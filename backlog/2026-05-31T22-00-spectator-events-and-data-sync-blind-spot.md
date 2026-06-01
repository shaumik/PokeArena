# Spectator events, live/live_pvp unification, and what we lose at the data-sync boundary

This entry covers a long session that started as a feature add (agent-vs-agent UI) and ended with us finding that the "event-based architecture" we'd been describing was only event-based for quicksim. Live and live_pvp resolved turns inline, broadcast to players over WS, persisted to Postgres, and emitted exactly one event in their entire lifecycle — `BattleCompleted` at the end. Spectator SSE was therefore blank for the whole battle.

PR [#43](https://github.com/shaumik/PokeArena/pull/43) shipped the fix-shaped-as-architecture: one coordinator for both live and live_pvp, with side-kind (`sideWS` / `sideAI`) deciding how each slot is driven. `persistLiveTurn` now publishes `EventTurnResolved`; `runOpenPhase` publishes `EventBattleStarted`. Single call site for both, exercised by every live-mode battle.

## Why we didn't just do the one-liner

The minimum fix was a single `s.broker.PublishEvent` line in `persistLiveTurn`. That would have closed the symptom — spectator sees turns — without touching the three-coordinator topology underneath (`battle-worker` for quicksim, `ws.go::runLiveLoop` for live, `pvp.go::pvpMatch.run` for live_pvp). The reason to do the larger refactor was the gap between what we'd been saying about the system ("it's event-based") and what was true ("quicksim is event-based; the other two are RPC-shaped with a different bus"). The minimum fix would have made the lie technically true while leaving the duplicated turn loops in place; the next person to touch live mode would have had to re-derive why these two loops exist and why they share helpers but not control flow. Better to collapse them now while the context is fresh.

After the refactor, the architectural story is honest: every live-mode turn — live, live_pvp, agent-vs-agent (which is just live_pvp with both joiners being MCP) — flows through the same coordinator, calls the same `persistLiveTurn`, and emits the same events. Quicksim stays in `battle-worker` because it's the batch-resolve path (no human in the loop); that distinction is real and worth keeping.

## The data-sync blind spot

Once spectator was working, we went looking for moves that don't behave right and filed five issues in a row — [#44](https://github.com/shaumik/PokeArena/issues/44), [#45](https://github.com/shaumik/PokeArena/issues/45), [#46](https://github.com/shaumik/PokeArena/issues/46), [#47](https://github.com/shaumik/PokeArena/issues/47). Every one of them has the same root cause:

**Showdown encodes mechanics as JavaScript callbacks, and our static data-sync dump can't capture callbacks.**

- Moonlight / Morning Sun / Synthesis (#44): `heal: null` upstream because the amount is computed by an `onHit` callback that reads weather.
- Outrage / Petal Dance / Thrash (#45): `self.volatileStatus: "lockedmove"` upstream, but the lock-in *behavior* (2–3 turn lock, end-of-streak confuse) lives in a `lockedmove` condition's JS handlers — none of which appear in the dump.
- U-turn / Volt Switch / Flip Turn (#46): `selfSwitch: true` is statically there, but the actual switch trigger is implemented as a callback the engine has to interpret. The transform doesn't even read the flag.
- Stored Power / Power Trip (#47): `basePowerCallback: (pokemon) => 20 + 20*positiveStages(pokemon)`. The static dump just sees `basePower: 20`.

The pattern: anything Showdown describes by *imperative code* gets lost; only the declarative fields survive the dump. Future bug-hunting in moves should start by asking "is this behavior in a Showdown callback?" — if yes, the bug is at the data layer, not the engine.

The structural fix is a small per-mechanic override table consulted by the transform (or by the engine, depending on the case). Inline overrides for moonlight/morning-sun/synthesis (clear-weather amount), a new `lockedmove` volatile in the engine + transform allowlist, a new `Move.SelfSwitch bool` plumbed through, and a per-move basePower scaler hook. Each is independent; none requires a new framework.

## Two architectural side findings

[#48](https://github.com/shaumik/PokeArena/issues/48) — spectator lag. After PR #43, spectators *get* events, but they're 30–60ms behind the players, sometimes more. Three independent causes:
1. RabbitMQ publishes use `DeliveryMode: amqp.Persistent`, which fsyncs each event to disk. Overkill for ephemeral turn events.
2. `persistLiveTurn` calls `store.AppendTurn` (Postgres) before `broker.PublishEvent`, so the publish waits on the DB roundtrip.
3. A spectator on the same gateway as the publisher still gets the event via Rabbit → external routing → Hub → SSE. Same-process events should fan out via the Hub directly.

[#49](https://github.com/shaumik/PokeArena/issues/49) — `AI_DIFFICULTY` env var. The ai-service's startup self-check validates one difficulty out of N it accepts at runtime. The gateway uses it as a default for live mode, but quicksim uses a hardcoded `easy` default — asymmetry with no documented reason. Probably should just delete it; the gateway default can be a constant.

## Priority order — my read

Highest:

1. **[#48](https://github.com/shaumik/PokeArena/issues/48) spectator lag.** This is unfinished business from PR #43. Until the lag is under ~5ms median, "event-based" is still half-true. The smallest single win — flipping events to transient — is a one-line change. Doing the full fix (transient + in-process Hub injection + persist off critical path) is maybe a day.
2. **[#46](https://github.com/shaumik/PokeArena/issues/46) U-turn / selfSwitch.** Highest-impact gameplay bug we filed. U-turn / Volt Switch are competitive staples; without them the metagame doesn't feel right. Engine work, but well-scoped — `Move.SelfSwitch bool` and a transition to `PhaseReplace` after damage resolves.

Medium:

3. **[#45](https://github.com/shaumik/PokeArena/issues/45) Outrage / lockedmove.** Second-most-impactful, and it teaches us the volatile-management pattern we'll need for the tier-1 moves anyway. If we sequence #45 right before [#31](https://github.com/shaumik/PokeArena/issues/31) (Substitute), the volatile infrastructure carries forward.
4. **[#44](https://github.com/shaumik/PokeArena/issues/44) Moonlight / Morning Sun / Synthesis.** Smallest fix in the batch (likely a manual-overrides JSON in `tools/data-sync/`), unblocks three commonly-used recovery moves. Could be a 30-minute side quest while waiting on something else.

Low / opportunistic:

5. **[#47](https://github.com/shaumik/PokeArena/issues/47) Stored Power.** Niche. Bundle with the broader variable-BP move work when we open [#37](https://github.com/shaumik/PokeArena/issues/37) (tier-2 mechanics).
6. **[#49](https://github.com/shaumik/PokeArena/issues/49) AI_DIFFICULTY cleanup.** Pure cleanup, no user impact. Do it while we're touching `cmd/ai-service/main.go` for something else.

What I'd want to *consider* before sequencing the next big block (tier-1 moves [#31](https://github.com/shaumik/PokeArena/issues/31)–[#35](https://github.com/shaumik/PokeArena/issues/35) and hazards [#36](https://github.com/shaumik/PokeArena/issues/36)): #45's lockedmove design will land a real engine-side "volatile with per-turn handler" abstraction. Substitute, Endure, Curse, Attract, Protect/Detect all want the same shape. If we land #45 first as a one-off, we'll re-do the abstraction for #31. If we wait and do it as part of #31, #45 becomes its first user. I lean toward doing #45 first because it's smaller and pressure-tests the abstraction with a single move before we use it for five at once — but it's a real trade-off, and the other answer is defensible.

## What we're not going to do this round

- The bigger live/quicksim unification (collapse quicksim into pvpMatch with both sides AI). Not blocking anything. Worth filing if we run out of higher-priority work but I don't want to add scope to the post-#43 follow-up.
- Cross-pod spectator fan-out. We're one gateway pod; cross-pod is a real concern at scale but not in the demo budget.
- The "hosted reference bot" ([#19](https://github.com/shaumik/PokeArena/issues/19)). Real demo value but it's a deployment exercise, not an engine exercise.
