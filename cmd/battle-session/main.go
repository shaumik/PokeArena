// Command battle-session owns the coordinator for live battles (mode=live and
// mode=live_pvp). It is the dedicated tier the gateway hands a battle off to,
// mirroring how battle-worker owns Quick Sim execution.
//
// A live battle is created by the gateway publishing a live.session.jobs work
// item; competing consumers elect exactly one battle-session instance as the
// owner. The owner takes a Redis ownership lease (renewed on a heartbeat),
// consumes inbound player actions from a durable per-battle queue, runs the
// pure engine over them, and publishes per-slot frames plus the usual domain
// events. Because the engine is a pure function and ownership is a lease — not
// co-tenancy with the socket — any instance can own any battle, and a dead
// owner's battle can be taken over (see the failover scan).
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"pokearena/internal/ai"
	"pokearena/internal/cache"
	"pokearena/internal/config"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/livebattle"
	"pokearena/internal/messages"
	"pokearena/internal/mq"
	"pokearena/internal/protocol"
	"pokearena/internal/store"
)

const (
	// leaseTTL is how long an ownership lease survives without a renewal. Pick a
	// few multiples of leaseRenew so a transient Redis hiccup doesn't drop a
	// live battle, but short enough that a dead owner is detected promptly.
	leaseTTL        = 30 * time.Second
	leaseRenew      = 10 * time.Second
	sessionPrefetch = 16
)

type service struct {
	instanceID string
	dex        *domain.Dex
	store      *store.Store
	cache      *cache.Cache
	broker     *mq.Broker
	ai         livebattle.AIDecider
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[battle-session] ")

	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dex, err := domain.LoadDex(envOr("DATA_DIR", "data"), cfg.DataVersion)
	if err != nil {
		log.Fatalf("load dataset: %v", err)
	}
	st, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		log.Fatalf("apply schema: %v", err)
	}
	rc, err := cache.New(ctx, cfg.RedisURL)
	if err != nil {
		log.Fatalf("connect redis: %v", err)
	}
	defer rc.Close()
	broker, err := mq.Connect(ctx, cfg.RabbitURL)
	if err != nil {
		log.Fatalf("connect rabbitmq: %v", err)
	}
	defer broker.Close()

	svc := &service{
		instanceID: uuid.NewString(),
		dex:        dex,
		store:      st,
		cache:      rc,
		broker:     broker,
		ai:         &harnessAI{h: ai.NewHarness(dex, cfg.AITimeBudget)},
	}

	log.Printf("instance %s consuming %s", svc.instanceID, messages.QueueLiveSession)
	if err := broker.ConsumeLiveSession(ctx, sessionPrefetch, svc.handleSession); err != nil && ctx.Err() == nil {
		log.Fatalf("session consumer stopped: %v", err)
	}
}

// handleSession claims ownership of a newly-created live battle and starts its
// coordinator in the background, acking the job immediately. We ack-and-spawn
// rather than blocking the consumer for the battle's whole lifetime (minutes of
// human think-time) so one instance coordinates many concurrent battles. The
// lease — not the job — is what guards against a double owner.
func (svc *service) handleSession(ctx context.Context, body []byte) error {
	var start messages.LiveSessionStart
	if err := json.Unmarshal(body, &start); err != nil {
		log.Printf("dropping malformed session job: %v", err)
		return nil
	}
	won, err := svc.cache.ClaimBattleOwner(ctx, start.BattleID, svc.instanceID, leaseTTL)
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

// runSession coordinates one live battle from the session-start job through to
// completion, then releases the lease and cleans up the action queue. The
// heartbeat keeps the lease alive; the action pump feeds inbound player
// messages into the coordinator; outbound frames are published per slot.
func (svc *service) runSession(parent context.Context, start messages.LiveSessionStart) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	kinds, teams := decodeSession(start)
	var aiDecider livebattle.AIDecider
	if kinds[0] == livebattle.SideAI || kinds[1] == livebattle.SideAI {
		aiDecider = svc.ai
	}

	sink := &brokerSink{broker: svc.broker, battleID: start.BattleID, ctx: ctx}
	m := livebattle.NewMatch(livebattle.Config{
		BattleID: start.BattleID, P1Name: start.P1Name, P2Name: start.P2Name, Seed: start.Seed,
		Kinds:   kinds,
		AITeams: teams,
		Sink:    sink,
		Deps: livebattle.Deps{
			Dex: svc.dex, Cache: svc.cache, Store: svc.store,
			Publish: svc.publish, AI: aiDecider,
		},
	})

	hbWait := svc.startHeartbeat(ctx, start.BattleID, cancel)

	pump := livebattle.NewPump(m, kinds)
	go svc.consumeActions(ctx, pump, start.BattleID)

	log.Printf("owning battle %s (mode=%s kinds=%v)", start.BattleID, start.Mode, start.Kinds)
	m.Run() // blocks until the battle ends or a slot disconnects

	cancel() // stop the action pump + heartbeat
	hbWait() // wait for the heartbeat goroutine to exit before releasing
	svc.cleanup(start.BattleID)
	log.Printf("released battle %s", start.BattleID)
}

// consumeActions drains the per-battle action queue into the pump until the
// session context is cancelled (battle over).
func (svc *service) consumeActions(ctx context.Context, pump *livebattle.Pump, battleID string) {
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
// lease no longer ours (it expired and was taken over), it cancels the session
// so we stop coordinating a battle we no longer own. Returns a wait func that
// blocks until the heartbeat goroutine has exited.
func (svc *service) startHeartbeat(ctx context.Context, battleID string, onLost func()) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(leaseRenew)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				ok, err := svc.cache.RenewBattleOwner(ctx, battleID, svc.instanceID, leaseTTL)
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
func (svc *service) cleanup(battleID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := svc.cache.ReleaseBattleOwner(ctx, battleID, svc.instanceID); err != nil {
		log.Printf("release lease %s: %v", battleID, err)
	}
	if err := svc.broker.DeleteLiveActionQueue(battleID); err != nil {
		log.Printf("delete action queue %s: %v", battleID, err)
	}
}

// publish fans a domain event out to the broker. Unlike the gateway, the
// session service has no local Hub — spectators live on the gateway and receive
// these events over Rabbit.
func (svc *service) publish(ctx context.Context, eventType, battleID string, msg any) {
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

// harnessAI implements livebattle.AIDecider in-process, running the agent
// harness directly (like battle-worker) rather than offloading to ai-service.
// For a dedicated worker tier this is strictly better than a per-turn broker
// round-trip: lower latency, no correlation bookkeeping, and the harness's own
// time budget guarantees the turn never stalls.
type harnessAI struct{ h *ai.Harness }

func (a *harnessAI) Start(context.Context) {}
func (a *harnessAI) Decide(_ context.Context, st *engine.BattleState, side int) (engine.Action, string) {
	return a.h.Decide(st, side), ""
}
func (a *harnessAI) DecideReplace(_ context.Context, st *engine.BattleState, side int) engine.Action {
	return a.h.Decide(st, side)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
