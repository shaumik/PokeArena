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
	"sync"
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
	// DefaultOpenOrphanGrace is how long a live battle may sit in the "open"
	// picker state, unowned, before the failover scan abandons it. It must
	// comfortably exceed the normal claim latency (a session job is claimed
	// within milliseconds of creation) so a freshly created room that is merely
	// mid-claim is never abandoned, yet stay well under the picker room deadline.
	DefaultOpenOrphanGrace = 60 * time.Second
	// SessionPrefetch bounds how many session-start jobs one instance buffers.
	// We ack-and-spawn, so this caps burst, not concurrent battles.
	SessionPrefetch = 16

	// DefaultDisconnectGrace is how long a coordinator waits for a slot to
	// re-attach after its WS bridge signals disconnect before abandoning the
	// battle. Long enough to ride out a transient blip or a page refresh
	// reconnecting through the load balancer; short enough that a real departure
	// ends the battle promptly.
	DefaultDisconnectGrace = 10 * time.Second
	// DefaultTurnDeadline bounds how long a turn waits on a WS slot before
	// treating it as gone. It is the backstop for a gateway that crashes without
	// sending a disconnect: no message ever arrives, so the grace window never
	// opens. Generous enough not to cut off a deliberating human, while bounding a
	// silent battle to minutes rather than forever.
	DefaultTurnDeadline = 3 * time.Minute
)

// Service owns live-battle coordination for one battle-session instance.
type Service struct {
	instanceID string
	dex        *domain.Dex
	store      *store.Store
	cache      *cache.Cache
	broker     *mq.Broker
	ai         livebattle.AIDecider

	leaseTTL        time.Duration
	leaseRenew      time.Duration
	scanInterval    time.Duration
	disconnectGrace time.Duration
	turnDeadline    time.Duration

	// coordinators tracks every in-flight coordinate() goroutine so Run can wait
	// for them to release their leases and delete their action queues before it
	// returns — i.e. before main closes the broker and Redis connections those
	// teardown calls depend on.
	coordinators sync.WaitGroup
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
	LeaseTTL        time.Duration
	LeaseRenew      time.Duration
	ScanInterval    time.Duration
	DisconnectGrace time.Duration
	TurnDeadline    time.Duration
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
		leaseTTL:        orDur(cfg.LeaseTTL, DefaultLeaseTTL),
		leaseRenew:      orDur(cfg.LeaseRenew, DefaultLeaseRenew),
		scanInterval:    orDur(cfg.ScanInterval, DefaultScanInterval),
		disconnectGrace: orDur(cfg.DisconnectGrace, DefaultDisconnectGrace),
		turnDeadline:    orDur(cfg.TurnDeadline, DefaultTurnDeadline),
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
// scan that takes over battles whose owner has died. On shutdown it drains: it
// waits for the scan loop and every in-flight coordinator to finish releasing
// its lease and deleting its action queue before returning, so the caller's
// broker/Redis Close() (deferred right after Run) can't race those teardown
// calls and strand a lease held until its TTL (~30s), delaying failover.
func (svc *Service) Run(ctx context.Context) error {
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		svc.runFailoverScan(ctx)
	}()
	log.Printf("instance %s consuming %s", svc.instanceID, messages.QueueLiveSession)
	err := svc.broker.ConsumeLiveSession(ctx, SessionPrefetch, svc.handleSession)

	// The consumer has stopped accepting new jobs and the scan loop has been told
	// to stop; both stop spawning coordinators. Wait for the scan to return (so
	// all its takeover goroutines are registered) and then for every coordinator
	// to finish its cleanup before we let the caller tear down connections.
	<-scanDone
	svc.coordinators.Wait()
	return err
}

// spawnCoordinator runs fn (a coordinate() call) in its own goroutine, tracked
// so Run's shutdown drain waits for its lease release and queue deletion.
func (svc *Service) spawnCoordinator(fn func()) {
	svc.coordinators.Add(1)
	go func() {
		defer svc.coordinators.Done()
		fn()
	}()
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
	svc.spawnCoordinator(func() { svc.runSession(ctx, start) })
	return nil
}

// runSession coordinates a freshly-created live battle from its picker phase.
func (svc *Service) runSession(parent context.Context, start messages.LiveSessionStart) {
	kinds, teams := decodeSession(start)
	m := livebattle.NewMatch(livebattle.Config{
		BattleID: start.BattleID, P1Name: start.P1Name, P2Name: start.P2Name, Seed: start.Seed,
		Kinds:           kinds,
		AITeams:         teams,
		Sink:            &brokerSink{broker: svc.broker, battleID: start.BattleID, ctx: parent},
		Deps:            svc.deps(svc.aiFor(kinds)),
		DisconnectGrace: svc.disconnectGrace,
		TurnDeadline:    svc.turnDeadline,
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

	// Declare the durable action queue before consuming. PublishLiveSession also
	// declares it at create time (covering the pre-owner window); this re-asserts
	// it on the takeover path, where the orphan may predate that or the queue may
	// have x-expired. Idempotent. Note this alone does NOT guarantee a WS bridge's
	// reply to the first (resync) frame isn't lost to a bind/consume race — the
	// coordinator's resync re-prompt (see collectActions) is what makes that
	// self-healing.
	if err := svc.broker.DeclareLiveActionQueue(battleID); err != nil {
		log.Printf("declare action queue %s: %v", battleID, err)
	}

	pump := livebattle.NewPump(m, kinds)
	go svc.consumeActions(ctx, pump, battleID)

	// Run is bound to ctx: a lost-lease yield (heartbeat → cancel) or shutdown
	// stops the turn loop instead of leaking it. The Reason drives cleanup.
	reason := m.Run(ctx)

	cancel() // stop the action pump + heartbeat
	hbWait() // wait for the heartbeat goroutine to exit before releasing
	svc.cleanup(battleID, reason)
	log.Printf("released battle %s (%s)", battleID, reason)
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
	svc.reclaimRunningOrphans(ctx)
	svc.abandonStaleOpenOrphans(ctx)
}

// reclaimRunningOrphans takes over running battles whose owner has died.
func (svc *Service) reclaimRunningOrphans(ctx context.Context) {
	ids, err := svc.store.ListRunningLiveBattleIDs(ctx)
	if err != nil {
		log.Printf("failover scan: list running: %v", err)
		return
	}
	for _, id := range ids {
		if svc.isOwned(ctx, id) {
			continue
		}
		svc.tryTakeover(ctx, id)
	}
}

// abandonStaleOpenOrphans retires picker rooms whose owner died during the OPEN
// phase. Unlike a running battle there is nothing to take over: the picker state
// (attaches, submissions, the room-deadline timer) lived only in the dead
// owner's memory and was never persisted. Left alone these sit "open" forever —
// the running scan never sees them and no deadline timer survives to expire them.
func (svc *Service) abandonStaleOpenOrphans(ctx context.Context) {
	cutoff := time.Now().Add(-DefaultOpenOrphanGrace)
	ids, err := svc.store.ListStaleOpenLiveBattleIDs(ctx, cutoff)
	if err != nil {
		log.Printf("failover scan: list open: %v", err)
		return
	}
	for _, id := range ids {
		if svc.isOwned(ctx, id) {
			continue
		}
		svc.abandonOpenOrphan(ctx, id)
	}
}

// isOwned reports whether a battle currently has a live ownership lease. A Redis
// hiccup is treated as "owned" so a transient error never triggers a takeover or
// an abandon — the next scan retries.
func (svc *Service) isOwned(ctx context.Context, battleID string) bool {
	_, err := svc.cache.GetBattleOwner(ctx, battleID)
	if err == nil {
		return true
	}
	return !errors.Is(err, cache.ErrNotFound)
}

// abandonOpenOrphan claims an orphaned picker room and marks it abandoned. The
// claim is exclusive (SET NX): if it loses, another instance is already handling
// the room; if it wins but the row has since advanced past "open", it releases
// the lease and lets the running scan take over instead.
func (svc *Service) abandonOpenOrphan(ctx context.Context, battleID string) {
	won, err := svc.cache.ClaimBattleOwner(ctx, battleID, svc.instanceID, svc.leaseTTL)
	if err != nil || !won {
		return
	}
	b, err := svc.store.GetBattle(ctx, battleID)
	if err != nil || b.Status != "open" {
		_ = svc.cache.ReleaseBattleOwner(ctx, battleID, svc.instanceID)
		return
	}
	log.Printf("abandoning orphaned open battle %s (owner died during picker)", battleID)
	svc.cleanup(battleID, livebattle.ReasonDeadlineExpired)
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
	svc.spawnCoordinator(func() { svc.resumeSession(parent, b, state) })
}

// resumeSession rebuilds a coordinator for a battle already in progress and runs
// it to completion. The reconnected gateway bridges are still publishing to the
// same durable action queue and bound to the same frame keys, so they reattach
// to the new owner transparently — they never learn ownership changed.
func (svc *Service) resumeSession(parent context.Context, b store.Battle, state *engine.BattleState) {
	kinds := kindsForMode(b.Mode)
	m := livebattle.NewResumedMatch(livebattle.Config{
		BattleID: b.ID, P1Name: b.P1Name, P2Name: b.P2Name, Seed: uint64(b.Seed),
		Kinds:           kinds,
		Sink:            &brokerSink{broker: svc.broker, battleID: b.ID, ctx: parent},
		Deps:            svc.deps(svc.aiFor(kinds)),
		DisconnectGrace: svc.disconnectGrace,
		TurnDeadline:    svc.turnDeadline,
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

// cleanup tears a coordinator down according to why it stopped.
//
//   - Yielded: we lost the lease (another instance now owns this battle) or we're
//     shutting down (the failover scan will hand it to a survivor). Either way,
//     release our lease (a CAS no-op once it's been re-taken) but leave the
//     durable action queue AND the persisted state untouched — the next owner is
//     mid-takeover and needs both.
//   - Disconnected / DeadlineExpired: the battle is abandoned. Mark the row
//     terminal and drop its live state so the failover scan stops seeing a
//     "running" battle to reclaim — otherwise it resurrects a coordinator that
//     blocks forever on players who are gone.
//   - Completed: Run already recorded the result and cleared state.
//
// All non-yield paths then release the lease and delete the now-idle action queue.
func (svc *Service) cleanup(battleID string, reason livebattle.Reason) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if reason == livebattle.ReasonYielded {
		// CAS release: frees the lease only if somehow still ours; never the new
		// owner's. Crucially does NOT delete the action queue or the state.
		if err := svc.cache.ReleaseBattleOwner(ctx, battleID, svc.instanceID); err != nil {
			log.Printf("release lease %s: %v", battleID, err)
		}
		return
	}

	if reason == livebattle.ReasonDisconnected || reason == livebattle.ReasonDeadlineExpired {
		if err := svc.store.SetBattleStatus(ctx, battleID, "abandoned"); err != nil {
			log.Printf("mark abandoned %s: %v", battleID, err)
		}
		if err := svc.cache.DeleteState(ctx, battleID); err != nil {
			log.Printf("delete state %s: %v", battleID, err)
		}
		// Drop the pvp slot-token hash too. The natural-completion path clears it
		// (Run → deleteTokensBest), but an abandoned battle never reaches that, so
		// without this the tokens linger for the battle's TTL. Combined with a
		// gateway crash skipping releaseSlotBest, that leaves the slot marked
		// claimed and refuses a legitimate reconnect with "slot is not available".
		if err := svc.cache.DeletePvPTokens(ctx, battleID); err != nil {
			log.Printf("delete pvp tokens %s: %v", battleID, err)
		}
	}

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
