# Claude as team drafter

**Status:** not started

**Why:** If both sides can be Claude, both sides need to be able to *build* a team
too — not just play one we hand them. "Blind drafting" (neither side sees the other
before lock-in) falls out for free if we don't broadcast the team.

**Surface:** MCP tool `propose_team(slots int) → Team` returning a legal team
(species in dex, moves in each species's learnset). Server validates and either
accepts or returns the validation error so the agent can retry.

**Open question:** does Claude get a *constraint* (e.g. "weak to fire-types is
banned", "no legendaries", "must include at least one psychic-type")? Defer until
the basic case works — start with "any legal team."

**Depends on:** [[team-builder-moves]] (for the schema), [[mcp-server]] (for the
tool surface).
