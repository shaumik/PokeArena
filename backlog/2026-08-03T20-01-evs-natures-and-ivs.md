# EVs, natures, and IVs — making the stat spread a decision

Every Pokémon in this sim currently has the same stat spread: IV 31, EV 0,
neutral nature, level 50. That is one line of arithmetic in
[`damage.go`](../internal/engine/damage.go):

```go
func calcStat(base int) int { return (2*base+31)*Level/100 + 5 }
func calcHP(base int) int   { return (2*base+31)*Level/100 + Level + 10 }
```

It was the right call when the engine was bare bones — a fixed spread means
base stats are the only thing that varies, so a benchmark measures *policy*
and nothing else. But `docs/benchmark.md` already admits the cost out loud:
"with no items, no EVs, and a neutral nature, **base stats dominate**." Items
landed. This entry is about closing the other half.

## What we're committing to this round

- **All three knobs at once: EVs, natures, IVs.** Not staged. The plumbing is
  identical for all three — same wire field, same validation pass, same UI
  panel — and splitting them means touching `TeamPick`, `ValidateTeam`,
  `foeWire`, the MCP schema, and the builder three separate times.
- **Spread rides on `TeamPick`, not just `Pokemon`.** Non-negotiable:
  [`eval/replay.go`](../internal/eval/replay.go) reconstructs a battle from
  picks via `NewBattleFromPicks`. A spread that only exists on the built
  `Pokemon` makes every replay a lie.
- **Pointer-or-nil for the optional fields.** `EVs *domain.Stats` (nil = all
  zero), `IVs *domain.Stats` (nil = all 31), `Nature string` (empty =
  neutral). Same convention the codebase already uses for weather, terrain,
  and confusion.
- **252 per stat / 510 total.** Modern rule, what Showdown enforces, what any
  competitive player expects.
- **`data/natures.json` through the data-sync pipeline.** Not a hardcoded Go
  table.
- **The default path stays byte-identical.** IV 31 / EV 0 / neutral reduces
  to exactly today's arithmetic. Existing tests, team files, and benchmark
  results do not move.

## The formula, and the one place it can go wrong

```
raw  = floor((2·B + IV + floor(EV/4)) · L / 100)
HP   = raw + L + 10                      // nature never touches HP
Stat = floor((raw + 5) · N)              // N ∈ {0.9, 1.0, 1.1}
```

The nature multiplier is applied **last**, after the `+5`, and floors. It must
be integer math — `s*11/10` and `s*9/10`, not `float64(s)*1.1`. 1.1 is not
representable in binary floating point, and `math.Floor(float64(s)*1.1)` lands
one below canon on a spread of real stat values. This is the single most
likely place for a silent off-by-one that only shows up as a damage roll
disagreeing with Showdown three months from now.

HP ignores nature entirely. Shedinja's 1-HP special case is not a concern —
it isn't in the Gen-1 roster.

## Decisions that took the longest to land

**Where the nature table lives.** Twenty-five rows that have not changed since
Gen 3, so a hardcoded Go map is genuinely tempting — no pipeline stage, no
dataset version coupling. Went with `data/natures.json` through data-sync
anyway, on the `typechart.json` precedent: the type chart is *also* effectively
immutable and *also* goes through extract → transform → stage → validate →
swap. The rule this project actually runs on is "the dataset is the dataset,"
not "the dataset is the volatile parts of the dataset." The payoff is that the
web builder and the MCP dexproxy fetch natures from the same place they fetch
items, instead of each carrying a hand-maintained copy that can drift.

The wrinkle: `@pkmn/sim` exposes `Dex.data.Natures`, so `refresh.js` grows a
`dumpNatures()`, but regenerating the snapshot needs a `make sync-upstream`
(Node, network). We can hand-author `tools/data-sync/upstream/natures.json`
once in the shape `dumpNatures()` will emit and wire the Go side against it;
the next real upstream refresh then regenerates it identically. Wiring the
pipeline shouldn't be blocked on a network round-trip to Smogon.

**Whether the benchmark adopts spreads in the same round.** No. The mechanic
lands with every existing team at EV 0 / neutral / IV 31, which makes the
engine change *provably inert* — same seeds, same results, byte for byte. An
EV'd team library comes later as its own versioned change, so the benchmark
discontinuity is explicit and dated. Landing both together would let a
regression in the stat formula hide behind a legitimately changed meta, and we
would have no way to tell the two apart after the fact.

**IVs, given that they barely matter here.** At level 50 with no Hidden Power,
IVs buy you two things: minimizing Speed for Trick Room, and dropping Attack to
reduce confusion self-hit and Foul Play damage. Both are niche. Included them
anyway because the marginal cost on top of EVs is one more nil-able field and
one more range check, whereas adding them in a later round means a second pass
over every surface in the table below. The UI can hide them behind a disclosure
so the common case stays a nature dropdown and an EV allocator.

## The trap: foe stat leakage

[`ai/agent.go`](../internal/ai/agent.go) already nils out the foe's `stats` on
the wire, with the comment "the exact spread is a free damage calculator (exact
Speed alone decides move order)." That redaction is currently *aspirational* —
with a fixed spread, anyone can derive the foe's exact stats from the species.
EVs and natures are what finally make it load-bearing, which is a real
gameplay win.

It is also a live footgun. `foeWire` **embeds** `engine.Pokemon`, so three new
public fields serialize straight to the opponent unless explicitly shadowed —
and `evs` + `nature` together *are* the stat spread the `stats` shadow exists
to protect. Handing those over would be strictly worse than the status quo:
today the foe's stats are public-but-uniform, and after this change they would
be public-but-informative.

This needs shadows on all three fields plus an enumeration test in the style of
the existing `TestView_FoeVolatilesNameNoItem` — one that fails when someone
adds a fourth field to `Pokemon` without deciding whether it is hidden
information.

## Surfaces

| Layer | Work |
|---|---|
| `tools/data-sync/refresh-upstream` | `dumpNatures()` → `upstream/natures.json` |
| `cmd/data-sync` | extract / transform / stage / swap / validate for natures |
| `internal/domain` | `Nature` type, nature table loading, `natures.json` in `LoadDexFS` |
| `internal/engine` | `calcStat`/`calcHP` signatures, `pokemonShell`, `TeamPick`, `Pokemon` |
| `internal/engine` | `ValidateTeam`: 252/510 EV caps, 0–31 IV range, nature slug legality |
| `internal/ai` | `foeWire` shadows + fog enumeration test; `teams.go` passthrough |
| `internal/eval` | `Ruleset()` string, team library schema |
| `internal/httpapi` | serve the nature catalog (alongside `/api/items`) |
| `internal/mcpserver` | `submit_team` schema prose, dexproxy, nature listing |
| `web/` | builder: nature dropdown + EV allocator + IV disclosure, across all three builder surfaces |
| `tools/gen-curated-sets.js` | optionally emit a default spread per species |
| `docs/` | `battle-state.md`, `ARCHITECTURE.md`, `benchmark.md`, `team-picker-room.md` |

Nothing in the damage engine changes. `Pokemon.Stats` is the only read path —
`damage.go`, `turn.go`'s confusion self-hit, and Download in `abilities.go` all
read the derived struct, and nothing recomputes stats mid-battle. If the spread
only changes what `pokemonShell` writes, the fight math is untouched.

## Staging

Three PRs:

1. **Engine core** — nature/EV/IV on `TeamPick` and `Pokemon`, the formula,
   validation, fog shadows, tests. Zero behavior change at defaults.
2. **Surfaces** — data-sync pipeline, API, MCP, web builder.
3. **Content** — EV'd team libraries, `Ruleset()` bump, benchmark docs.

## A wart worth naming

`domain.Stats` tags its fields `atk` / `def` / `spatk` / `spdef` / `speed`, but
the boost vocabulary in `internal/specs` uses `attack` / `defense` / `spatk` /
`spdef` / `speed`. Reusing `domain.Stats` for EVs and IVs means the EV wire keys
won't match the boost keys — `{"evs": {"atk": 252}}` next to
`{"boosts": {"attack": 2}}`.

Reusing `domain.Stats` anyway. A fourth stat-naming convention invented to
paper over an inconsistency between the existing two is a worse outcome than
documenting the seam. Noted here so the next person to trip on it finds the
decision instead of re-deriving it.

## Scope cuts

- **Level stays fixed at 50.** `engine.Level` is a const read by the damage
  formula in four places; making it per-Pokémon is a separate change with its
  own accuracy and OHKO implications, and no one has asked for it.
- **No spread presets / "Smart-fill" for EVs** in round one. The curated-set
  generator can grow a default spread later; a nature dropdown defaulting to
  neutral and an allocator defaulting to all-zero is a complete, honest
  starting state.
- **No Hyper Training, no Gen-3 255-cap legacy mode.** One ruleset.
