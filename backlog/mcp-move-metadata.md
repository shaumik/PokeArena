# MCP: include move metadata in the view

**Status:** not started

**Why:** `engine.MoveSlot` exposes only `{move_id, pp, max_pp}` to the
agent. To decide between, say, `outrage` and `dragon-claw`, the agent has
to *recall from training data* the base power, type, accuracy, and
physical/special category of each move. This is fragile in two ways:

1. The agent's memory of move stats is generation-dependent. Crunch is
   special in gen 3, physical in gen 4+ — get the gen wrong and damage
   models are wildly off.
2. As the move dex evolves (custom moves, tweaks for balance), the
   agent's recall silently goes stale and there's no fix short of
   prompt-engineering.

The data lives in `domain.Dex.Moves` and is already used server-side
during damage resolution. The fog-of-war argument doesn't apply: move
metadata for *your own* Pokémon is something the human UI shows on
hover, and even the *foe's* revealed moves have public metadata
(everyone can look up Thunderbolt's BP).

**Scope:**
- Extend the JSON shape returned for each move in a `View` (both
  `Self.Team[*].Moves` and `Foe.Moves`) to include:
  - `bp int` (base power)
  - `accuracy int` (0..100; 0 for moves that bypass accuracy checks)
  - `type string` (matches `domain.Type`)
  - `category string` (`"physical"` | `"special"` | `"status"`)
- The natural place is a new wire struct in `internal/protocol` that
  embeds `engine.MoveSlot` plus the resolved metadata, populated in
  `ai.MakeView` from `domain.Dex.Moves[move_id]`.
- Engine internals (`MoveSlot`) stay as-is — the metadata is a *view*
  concern, denormalized at projection time.

**Open question:** include effect descriptors (e.g. `flinch_chance`,
`status_chance`, `recoil_fraction`) too? Probably yes, but they're not
wired into `domain.Move` uniformly today; punt to a follow-up once
[[attacks-secondary-effects]] lands.

**Acceptance:** An agent that has never played Pokémon can pick the
strongest available legal move purely from the JSON view, without any
external knowledge of move stats.

**Depends on:** nothing in core; the dex is already loaded server-side.
**Required by:** [[mcp-damage-prediction]] (the damage estimator needs
this metadata too — but exposing it raw is independently useful and
shippable first).
