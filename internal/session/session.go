// Package session is the battle-session tier: it owns the coordinator for live
// battles (mode=live and live_pvp). A battle is handed off by the gateway
// publishing a live.session.jobs work item; competing consumers plus a Redis
// ownership lease elect exactly one owner. The owner runs the pure engine over
// inbound player actions (durable per-battle queue) and publishes per-slot
// frames plus the usual domain events.
//
// Because the engine is a pure function and ownership is a lease — not
// co-tenancy with the socket — any instance can own any battle, and a dead
// owner's battle can be taken over via the lease.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"pokearena/internal/cache"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/livebattle"
	"pokearena/internal/messages"
	"pokearena/internal/mq"
	"pokearena/internal/protocol"
	"pokearena/internal/store"
)

const (
	// DefaultLeaseTTL is how long an ownership lease survives without a renewal —
	// a few multiples of DefaultLeaseRenew so a transient Redis hiccup doesn't
	// drop a live battle, but short enough that a dead owner is detected promptly.
	DefaultLeaseTTL     = 30 * time.Second
	DefaultLeaseRenew   = 10 * time.Second
	DefaultScanInterval = 12 * time.Second // < lease TTL: reclaim within one window
	// SessionPrefetch bounds how many session-start jobs one instance buffers.
	// We ack-and-spawn, so this caps burst, not concurrent battles.
	SessionPrefetch = 16
)

// Service owns live-battle coordination for one battle-session instance.
type Service struct {
	instanceID string
	dex        *domain.Dex
	store      *store.Store
	cache      *cache.Cache
	broker     *mq.Broker
	ai         livebattle.AIDecider

	leaseTTL     time.Duration
	leaseRenew   time.Duration
	scanInterval time.Duration
}

// Config wires a Service. AI decides AI-side actions (the production wiring runs
// the agent harness in-process; tests inject a deterministic decider). The
// lease/scan durations default to the package constants when left zero.
type Config struct {
	InstanceID   string
	Dex          *domain.Dex
	Store        *store.Store
	Cache        *cache.Cache
	Broker       *mq.Broker
	AI           livebattle.AIDecider
	LeaseTTL     time.Duration
	LeaseRenew   time.Duration
	ScanInterval time.Duration
}

// New builds a Service.
func New(cfg Config) *Service {
	return &Service{
		instanceID:   cfg.InstanceID,
		dex:          cfg.Dex,
		store:        cfg.Store,
		cache:        cfg.Cache,
		broker:       cfg.Broker,
		ai:           cfg.AI,
		leaseTTL:     orDur(cfg.LeaseTTL, DefaultLeaseTTL),
		leaseRenew:   orDur(cfg.LeaseRenew, DefaultLeaseRenew),
		scanInterval: orDur(cfg.ScanInterval, DefaultScanInterval),
	}
}

func orDur(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

// InstanceID returns this owner's id (stamped on its leases).
func (svc *Service) InstanceID() string { return svc.instanceID }

// Run consumes session-start jobs until ctx is cancelled, and runs the failover
// scan that takes over battles whose owner has died.
func (svc *Service) Run(ctx context.Context) error {
	go svc.runFailoverScan(ctx)
	log.Printf("instance %s consuming %s", svc.instanceID, messages.QueueLiveSession)
	return svc.broker.ConsumeLiveSession(ctx, SessionPrefetch, svc.handleSession)
}

// handleSession claims ownership of a newly-created live battle and starts its
// coordinator in the background, acking the job immediately. We ack-and-spawn
// rather than blocking the consumer for the battle's whole lifetime (minutes of
// human think-time) so one instance coordinates many concurrent battles. The
// lease — not the job — is what guards against a double owner.
func (svc *Service) handleSession(ctx context.Context, body []byte) error {
	var start messages.LiveSessionStart
	if err := json.Unmarshal(body, &start); err != nil {
		log.Printf("dropping malformed session job: %v", err)
		return nil
	}
	won, err := svc.cache.ClaimBattleOwner(ctx, start.BattleID, svc.instanceID, svc.leaseTTL)
	if err != nil {
		return err // transient (Redis) — requeue
	}
	if !won {
		log.Printf("battle %s already owned — skipping", start.BattleID)
		return nil
	}
	go svc.runSession(ctx, start)
	return nil
}

// runSession coordinates a freshly-created live battle from its picker phase.
func (svc *Service) runSession(parent context.Context, start messages.LiveSessionStart) {
	kinds, teams := decodeSession(start)
	m := livebattle.NewMatch(livebattle.Config{
		BattleID: start.BattleID, P1Name: start.P1Name, P2Name: start.P2Name, Seed: start.Seed,
		Kinds:   kinds,
		AITeams: teams,
		Sink:    &brokerSink{broker: svc.broker, battleID: start.BattleID, ctx: parent},
		Deps:    svc.deps(svc.aiFor(kinds)),
	})
	log.Printf("owning battle %s (mode=%s kinds=%v)", start.BattleID, start.Mode, start.Kinds)
	svc.coordinate(parent, start.BattleID, m, kinds)
}

// coordinate runs a built match to completion under an ownership lease: the
// heartbeat keeps the lease alive, the action pump feeds inbound player
// messages, and on exit the lease is released and the action queue removed.
// Shared by the fresh and resumed paths.
func (svc *Service) coordinate(parent context.Context, battleID string, m *livebattle.Match, kinds [2]livebattle.SideKind) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	hbWait := svc.startHeartbeat(ctx, battleID, cancel)

	pump := livebattle.NewPump(m, kinds)
	go svc.consumeActions(ctx, pump, battleID)

	m.Run() // blocks until the battle ends or a slot disconnects

	cancel() // stop the action pump + heartbeat
	hbWait() // wait for the heartbeat goroutine to exit before releasing
	svc.cleanup(battleID)
	log.Printf("released battle %s", battleID)
}

// deps builds the coordinator's host dependencies with the given AI decider.
func (svc *Service) deps(ai livebattle.AIDecider) livebattle.Deps {
	return livebattle.Deps{
		Dex: svc.dex, Cache: svc.cache, Store: svc.store,
		Publish: svc.publish, AI: ai,
	}
}

// aiFor returns the in-process AI decider when a slot needs it, else nil.
func (svc *Service) aiFor(kinds [2]livebattle.SideKind) livebattle.AIDecider {
	if kinds[0] == livebattle.SideAI || kinds[1] == livebattle.SideAI {
		return svc.ai
	}
	return nil
}

// runFailoverScan periodically reclaims live battles whose owner has died. A
// dead owner stops renewing its lease; the lease expires; the next scan on any
// instance finds the battle "running" in Postgres but unowned in Redis and takes
// it over, rehydrating the coordinator from the persisted state.
func (svc *Service) runFailoverScan(ctx context.Context) {
	t := time.NewTicker(svc.scanInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			svc.scanForOrphans(ctx)
		}
	}
}

func (svc *Service) scanForOrphans(ctx context.Context) {
	ids, err := svc.store.ListRunningLiveBattleIDs(ctx)
	if err != nil {
		log.Printf("failover scan: list running: %v", err)
		return
	}
	for _, id := range ids {
		_, err := svc.cache.GetBattleOwner(ctx, id)
		if err == nil {
			continue // still owned — healthy
		}
		if !errors.Is(err, cache.ErrNotFound) {
			continue // Redis hiccup; try this one again next scan
		}
		svc.tryTakeover(ctx, id)
	}
}

// tryTakeover claims an orphaned battle and resumes its coordinator. The claim
// is exclusive (SET NX), so if several instances scan at once only one wins; the
// rest no-op. If there's no live state to resume (the battle actually finished),
// the lease is dropped again.
func (svc *Service) tryTakeover(parent context.Context, battleID string) {
	won, err := svc.cache.ClaimBattleOwner(parent, battleID, svc.instanceID, svc.leaseTTL)
	if err != nil || !won {
		return
	}
	state, err := svc.cache.LoadState(parent, battleID)
	if err != nil {
		// No live state: the battle finished (state deleted) between the scan
		// and now, or was never persisted. Don't hold a lease on nothing.
		_ = svc.cache.ReleaseBattleOwner(parent, battleID, svc.instanceID)
		return
	}
	b, err := svc.store.GetBattle(parent, battleID)
	if err != nil || b.Status != "running" {
		_ = svc.cache.ReleaseBattleOwner(parent, battleID, svc.instanceID)
		return
	}
	log.Printf("taking over orphaned battle %s at turn %d", battleID, state.Turn)
	go svc.resumeSession(parent, b, state)
}

// resumeSession rebuilds a coordinator for a battle already in progress and runs
// it to completion. The reconnected gateway bridges are still publishing to the
// same durable action queue and bound to the same frame keys, so they reattach
// to the new owner transparently — they never learn ownership changed.
func (svc *Service) resumeSession(parent context.Context, b store.Battle, state *engine.BattleState) {
	kinds := kindsForMode(b.Mode)
	m := livebattle.NewResumedMatch(livebattle.Config{
		BattleID: b.ID, P1Name: b.P1Name, P2Name: b.P2Name, Seed: uint64(b.Seed),
		Kinds: kinds,
		Sink:  &brokerSink{broker: svc.broker, battleID: b.ID, ctx: parent},
		Deps:  svc.deps(svc.aiFor(kinds)),
	}, state)
	svc.coordinate(parent, b.ID, m, kinds)
}

// kindsForMode reconstructs slot kinds from the battle mode for a takeover —
// live is human vs in-process AI, live_pvp is two remote sides.
func kindsForMode(mode string) [2]livebattle.SideKind {
	if mode == "live" {
		return [2]livebattle.SideKind{livebattle.SideWS, livebattle.SideAI}
	}
	return [2]livebattle.SideKind{livebattle.SideWS, livebattle.SideWS}
}

// consumeActions drains the per-battle action queue into the pump until the
// session context is cancelled (battle over).
func (svc *Service) consumeActions(ctx context.Context, pump *livebattle.Pump, battleID string) {
	err := svc.broker.ConsumeLiveActions(ctx, battleID, 1, func(_ context.Context, body []byte) error {
		var a messages.LiveAction
		if err := json.Unmarshal(body, &a); err != nil {
			return nil // drop malformed; don't requeue
		}
		pump.Route(a)
		return nil
	})
	if err != nil && ctx.Err() == nil {
		log.Printf("action consumer %s stopped: %v", battleID, err)
	}
}

// startHeartbeat renews the ownership lease on a ticker. If a renewal finds the
// lease no longer ours (it expired and was taken over), it cancels the session.
// Returns a wait func that blocks until the heartbeat goroutine has exited.
func (svc *Service) startHeartbeat(ctx context.Context, battleID string, onLost func()) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(svc.leaseRenew)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				ok, err := svc.cache.RenewBattleOwner(ctx, battleID, svc.instanceID, svc.leaseTTL)
				if err != nil {
					log.Printf("lease renew %s: %v", battleID, err)
					continue
				}
				if !ok {
					log.Printf("lease lost for %s — yielding", battleID)
					onLost()
					return
				}
			}
		}
	}()
	return func() { <-done }
}

// cleanup releases the lease (CAS — only if we still hold it) and deletes the
// now-idle action queue.
func (svc *Service) cleanup(battleID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := svc.cache.ReleaseBattleOwner(ctx, battleID, svc.instanceID); err != nil {
		log.Printf("release lease %s: %v", battleID, err)
	}
	if err := svc.broker.DeleteLiveActionQueue(battleID); err != nil {
		log.Printf("delete action queue %s: %v", battleID, err)
	}
}

// publish fans a domain event out to the broker. Unlike the gateway, the session
// service has no local Hub — spectators live on the gateway and receive these
// events over Rabbit.
func (svc *Service) publish(ctx context.Context, eventType, battleID string, msg any) {
	_ = svc.broker.PublishEvent(ctx, eventType, battleID, msg)
}

// decodeSession maps the wire job to coordinator inputs. The "ai" slot (live
// mode) gets the pre-picked roster carried in the job; "ws" slots are remote.
func decodeSession(start messages.LiveSessionStart) (kinds [2]livebattle.SideKind, teams [2][]engine.TeamPick) {
	for i := 0; i < 2; i++ {
		if start.Kinds[i] == "ai" {
			kinds[i] = livebattle.SideAI
			teams[i] = start.AITeam
		} else {
			kinds[i] = livebattle.SideWS
		}
	}
	return kinds, teams
}

// brokerSink implements livebattle.FrameSink by publishing each per-slot frame
// to the events topic. The gateway holding that slot's socket has bound the
// matching routing key and forwards the bytes to the WebSocket.
type brokerSink struct {
	broker   *mq.Broker
	battleID string
	ctx      context.Context
}

func (s *brokerSink) SendFrame(slot int, u protocol.MatchUpdate) {
	_ = s.broker.PublishFrame(s.ctx, s.battleID, livebattle.SlotName(slot), u)
}
func (s *brokerSink) Close() {}
