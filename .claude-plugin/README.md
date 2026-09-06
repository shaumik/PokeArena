# PokéArena — Claude Code plugin

Installs the [`pokearena-mcp`](../cmd/pokearena-mcp) stdio MCP server and a
`play-battle` skill, so a Claude Code session can claim a trainer slot in a live
PokéArena battle and play it to completion.

This repository is also its own plugin marketplace
([`marketplace.json`](marketplace.json)):

```
/plugin marketplace add shaumik/PokeArena
/plugin install pokearena@pokearena
```

Or, for local development straight from a checkout:

```
claude --plugin-dir /path/to/PokeArena
```

## What it registers

| Component | What |
|---|---|
| MCP server `pokearena` | `join_battle`, `submit_team`, `wait`, `view`, `act`, `leave_battle`, plus the drafting helpers `find_pokemon`, `get_pokemon`, `list_natures`, `list_items` |
| Skill `pokearena:play-battle` | The battle loop and the fog-of-war contract, grounded in [`docs/mcp-protocol.md`](../docs/mcp-protocol.md) |

## The server binary

[`bin/pokearena-mcp`](bin/pokearena-mcp) is a launcher, not the server. It looks
for a prebuilt binary and falls back to `go run ./cmd/pokearena-mcp`, so a
checkout with the Go toolchain installed works with no build step (the first
start compiles and is slow). To avoid that, build it once:

```
go build -o ./bin/pokearena-mcp ./cmd/pokearena-mcp
```

The launcher will pick up `<plugin root>/bin/pokearena-mcp`, anything on `PATH`
named `pokearena-mcp`, or an explicit `POKEARENA_MCP_BIN`.

## Pointing at a gateway

The server defaults to a local stack at `ws://localhost:8080`. For a deployed
gateway, set the plugin's **Gateway URL** config (it is passed through as
`POKEARENA_GATEWAY_URL`), or export `POKEARENA_GATEWAY_URL=wss://your.host`
in the environment Claude Code starts from.

Then hand Claude a battle share URL — `http://…/?battle=ID&slot=p2&token=…` —
and it will join that slot and play.
