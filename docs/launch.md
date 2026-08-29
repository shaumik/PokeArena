# Launch checklist

Everything here is copy-paste. The order matters: 1 and 2 gate everything else,
and 3–5 are worth nothing until the project is installable.

Release mechanics (tagging, the registry publish, troubleshooting) live in
[publishing.md](publishing.md). This file is the distribution half.

---

## 1. Merge and tag  ·  ~10 min  ·  blocks everything below

Nothing is installable until the module rename is on `main`, and nothing is
listed anywhere until a `v*` tag exists. `go install …@latest` resolves through
`main`, so it fails today with *"module declares its path as: pokearena"*.

- Merge the module-rename PR.
- **Actions → Release → Run workflow**, `version: 0.1.0`, no leading `v`.

One-shot, and deliberately so: the MCP Registry refuses to re-publish a version
number. See [publishing.md §1](publishing.md).

---

## 2. Repo description  ·  1 min  ·  highest read-count of anything you own

Settings → the ⚙ beside **About**. Paste:

```
An MCP server that lets LLM agents play Pokémon battles.
```

That is the shape that works for this category — category, audience, verb,
famous thing, no adjectives. Compare `lmwilki/civ6-mcp` (151★): *"An MCP server
that lets LLM agents play Civilization VI."*

Website field: `https://github.com/shaumik/PokeArena#play-in-two-commands`

Topics are already good — 20 of them, including `mcp`,
`model-context-protocol`, `ai-agents`, `llm-agents`, `agent-benchmark`. Leave
them.

---

## 3. Directories  ·  ~45 min  ·  the slow, permanent channel

The MCP Registry publishes itself from the release workflow. These do not.

| Where | How | Notes |
|---|---|---|
| [punkpeye/awesome-mcp-servers](https://github.com/punkpeye/awesome-mcp-servers) | Pull request | Read its README header for the exact line format before opening the PR — it changes. |
| [mcpservers.org](https://mcpservers.org/submit) | Web form | Does not take PRs. |
| [Glama](https://glama.ai/mcp/servers) | Indexes public repos automatically | Claim the listing once it appears. |
| [Smithery](https://smithery.ai/new) | Connect the GitHub repo | |
| [PulseMCP](https://www.pulsemcp.com/submit) | Web form | |
| [Claude plugin directory](https://clau.de/plugin-directory-submission) | Web form | `.claude-plugin/` already exists in the repo. |

Line to submit, wherever a one-liner is asked for:

> **PokéArena** — An MCP server that lets LLM agents play Pokémon battles. Runs
> entirely in-process: no server, no API key, no setup. Also a reproducible
> benchmark, with variance-controlled mirror matches on a deterministic engine.

---

## 4. A demo people can see  ·  ~1 hr

[`demo.svg`](demo.svg) is already at the top of the README — a real session,
animated. Beyond that:

- GitHub Pages is **already enabled** on this repo and unused. A static replay
  viewer — feed it a run's JSONL trace, step through the turns — would be the
  single most shareable thing here, and it needs no backend.
- The battle video (`README` § *Watch: two agents battle*) is real footage and
  sits too far down. If Pages ships, link it from the hero instead.

---

## 5. Post it once, somewhere with an audience  ·  ~2 hr  ·  highest variance

Registries trickle. This is what produces the first hundred stars.

**Lead with the transcript, not the architecture.** The thing that makes people
look is *an agent got its team rejected and fixed it in one pass* — not the
distributed session tier.

### Show HN

Title (76 chars, under the 80 limit):

```
Show HN: PokéArena – an MCP server that lets LLM agents play Pokémon battles
```

First comment:

> I wanted an environment where I could prove an agent's win rate was the agent
> and not the dice, so I wrote the battle engine rather than wrapping Pokémon
> Showdown. Because the RNG stream is ours and seeded, every match is a mirror
> match on an identical seed — same team, both sides — so the only free variable
> left is the policy.
>
> It installs in two commands and needs nothing running: the battle happens
> inside the MCP server, on a dataset compiled into the binary. Three tool calls
> reach the first move.
>
> The part I'd most like feedback on is the team-building surface. An agent
> writing a team from memory gets it wrong in ways that are specific and
> fixable, so a rejection returns every problem at once with the legal
> alternatives, plus warnings for teams that are legal and still bad. A 22-turn
> battle costs 26 tool calls end to end.
>
> Honest limits are in the README, including one that cost me a metric: deeper
> expectimax search plays *worse* on this format, so per-move regret against it
> is not a valid measure and was cut.

### r/LocalLLaMA

Title: `I built a Pokémon battle environment for agents — no server, runs in-process, and every match is reproducible`

Open with the demo SVG, then the transcript. This audience cares about *runs
locally, no API key*, so lead with that and mention the Ollama contestant
support in `cmd/bench`.

### X / Bluesky

> Your agent can now lose at Pokémon.
>
> `go install github.com/shaumik/PokeArena/cmd/pokearena-mcp@latest`
>
> 6v6, fog of war, own deterministic engine. No server, no key, no clone. Three
> tool calls to the first move — and the same seed replays the same battle.

Attach `docs/demo.svg` rendered to a video or a screen recording; the SVG
animates on GitHub but most social embeds want a real video file.

---

## 6. Triage the issue list  ·  ~30 min

Fifteen open issues against a handful of stars reads as abandoned, whatever the
truth is. Several are already done and just need closing — see the audit in the
session notes, particularly the EV spread work (`engine.MaxEVPerStat`,
`TeamPick.EVs`) and the `wait()`/`act()` semantics, which changed when `act`
started returning the next view.

---

## What this does not fix

Stars, forks and citations come from other people over time. Everything above
buys you the *chance* at them; none of it manufactures them. Do 1 and 2 today —
they are twelve minutes and they unblock the rest.
