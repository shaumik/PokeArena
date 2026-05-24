# Moved the backlog into GitHub Issues

The old `backlog/` was doing two jobs badly: tracking action items
(one stub file per "thing to build") *and* holding the design notes
that explained why those items mattered. It collected dead empty
stubs (`ability.md`, `animation.md`, …) that no one looked at, and
it duplicated work that GitHub Issues already does well — labels,
state, cross-referencing from commits, assignment.

So today I split them. **Action items → Issues. Diary → here.**

## What got filed

Fifteen issues, `shaumik/PokeArena#1`–`#15`. The bodies are the
original `.md` files verbatim (minus the H1 title), with `[[wikilink]]`
refs rewritten to `#N` cross-references so the in-issue navigation works.

| #    | Title                                                       | Label         |
|------|-------------------------------------------------------------|---------------|
| #1   | MCP: surface the per-turn event log to the agent            | `mcp`         |
| #2   | MCP: expose the legal-action list on the view               | `mcp`         |
| #3   | MCP: include move metadata in the view                      | `mcp`         |
| #4   | MCP: damage-range prediction per legal move                 | `mcp`         |
| #5   | MCP: clarify wait()/phase semantics for simultaneous turns  | `mcp`         |
| #6   | Disconnect detection (30s grace → forfeit)                  | `enhancement` |
| #7   | Team builder: explicit moves per Pokemon                    | `enhancement` |
| #8   | Claude as team drafter (MCP propose_team)                   | `enhancement` |
| #9   | Engine: Pokemon abilities                                   | `engine`      |
| #10  | Engine: battle animations                                   | `engine`      |
| #11  | Engine: secondary effects on attacks                        | `engine`      |
| #12  | Engine: background music and sound                          | `engine`      |
| #13  | Engine: EV (effort value) stat points                       | `engine`      |
| #14  | Engine: status conditions                                   | `engine`      |
| #15  | Engine: weather conditions                                  | `engine`      |

The 5 MCP-surface issues (`#1`–`#5`) are the highest leverage right
now — they were raised by a Claude playtest session that mis-tracked
turn state because the agent surface was thinner than the engine's
internal state. Worth shipping as a batch.

`#6` (disconnect detection) is the only remaining must-have before
strangers can play.

## What this folder is now

A diary. See `backlog/README.md` for the file-naming convention. Old
backlog stubs are gone — they're either in issues now or were never
populated past a filename.

## The next agent reading this

If you're looking for "what should I build" — read the issues, sorted
by label and reaction count. If you're looking for "why is the code
shaped this way" — read `docs/`. If you're looking for "what did past
sessions try and learn" — keep reading this folder, newest-to-oldest.
