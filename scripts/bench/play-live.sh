#!/usr/bin/env bash
# Play ONE live (vs the server-side programmatic AI) battle with a chosen
# agentic harness + model, then print the authoritative winner from the gateway.
#
# The agent plays a named team from the competitive library (data/benchmark-teams.json)
# against the live expectimax opponent, driving the battle through the pokearena
# MCP tools. This is the "agentic harness" arm of the benchmark — the same model
# in a full agent runtime (Claude Code, Antigravity) rather than the thin one-shot
# harness that `cmd/bench -llm` measures.
#
# Prereqs: the stack is up (`make run`), `bin/pokearena-mcp` is built (`make mcp`),
# and the chosen CLI is installed and authenticated (claude, or agy for Antigravity).
#
# Usage: play-live.sh <claude|agy> <model> <team-name> <label> [outdir]
#   play-live.sh claude sonnet Genesis cs1
#   play-live.sh agy "Gemini 3.1 Pro (High)" Genesis ag1
set -uo pipefail

HARNESS="${1:?harness: claude or agy}"
MODEL="${2:?model, e.g. sonnet or Gemini 3.1 Pro (High)}"
TEAM="${3:?team name from data/benchmark-teams.json}"
LABEL="${4:?a short label for this game}"
OUTDIR="${5:-/tmp/pokearena-agentic}"

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$DIR/../.." && pwd)"
HELP="$DIR/_bench_helpers.py"
GW_HTTP="${POKEARENA_GATEWAY_HTTP:-http://localhost:8080}"
GW_WS="${POKEARENA_GATEWAY_URL:-ws://localhost:8080}"
# A hung agent session (a stalled model/streaming connection reads forever at 0%
# CPU) would otherwise block a whole batch indefinitely — Opus in particular
# walls out this way. Cap each game's wall clock and kill a session that blows
# past it, so the game just lands as unfinished instead of wedging the batch.
# The `agy` harness already self-limits via --print-timeout; this gives `claude`
# the same guarantee. macOS ships no timeout(1), so we watchdog it ourselves.
GAME_TIMEOUT="${POKEARENA_GAME_TIMEOUT:-1200}"
mkdir -p "$OUTDIR"

# run_capped runs a command with a wall-clock cap. On expiry it TERMs (then
# KILLs) the command AND its children — claude spawns the MCP server as a child,
# so killing only the parent would orphan it. Returns 124 on timeout, else the
# command's own exit code (matching timeout(1)'s convention).
run_capped() {
  local secs="$1"; shift
  local flag; flag="$(mktemp)"; rm -f "$flag"  # exists only once the cap fires
  "$@" &
  local cpid=$!
  (
    local waited=0
    while kill -0 "$cpid" 2>/dev/null; do
      sleep 10; waited=$((waited + 10))
      if [ "$waited" -ge "$secs" ]; then
        : > "$flag"
        pkill -TERM -P "$cpid" 2>/dev/null; kill -TERM "$cpid" 2>/dev/null
        sleep 5
        pkill -KILL -P "$cpid" 2>/dev/null; kill -KILL "$cpid" 2>/dev/null
        exit 0
      fi
    done
  ) &
  local wpid=$!
  wait "$cpid" 2>/dev/null; local rc=$?
  if [ -f "$flag" ]; then wait "$wpid" 2>/dev/null; rm -f "$flag"; return 124; fi
  kill "$wpid" 2>/dev/null; wait "$wpid" 2>/dev/null
  return "$rc"
}

# The team's picks, straight from the committed library — the agent plays a real
# tuned team, not an improvised one.
PICKS=$(python3 "$HELP" picks "$REPO/data/benchmark-teams.json" "$TEAM") || exit 1

# A fresh live (vs-AI) battle.
BID=$(python3 "$HELP" newbattle "$GW_HTTP") || { echo "$LABEL FAILED: could not create battle (is the stack up?)"; exit 1; }

read -r -d '' PROMPT <<EOF
You are a Pokemon battle client playing ONE battle to WIN against a programmatic AI opponent.
You have ONLY the pokearena MCP tools (the pokearena tools). Call them directly.
Do NOT use shell/bash or any other tool. Do NOT ask questions — play autonomously to the end.

battle_id: $BID

Steps:
1. join_battle with ONLY battle_id="$BID" (no slot, no join_token). Live vs-AI battle; you are p1.
2. submit_team with these exact picks:
$PICKS
3. Play loop: call wait (timeout_seconds: 30). When it returns ready with a view and it's your turn, read the view and call act:
   - act kind="move", index=0..3  -> your active Pokemon's move at that slot.
   - act kind="switch", index=0..5 -> switch to that team member.
   Only moves with PP>0 are legal; only non-fainted bench can be switched to. If an action is rejected, pick another legal one.
   In the view: "self" is your full side (exact HP, move PP). "foe" is the opponent (hp_pct = their HP%; moves show move_id once revealed). "foe_bench_alive" = how many they have left.
4. Repeat wait->act until wait returns terminal=true. Then stop.

Play to WIN: type effectiveness, prioritize KOs, switch out of bad matchups, set up stat boosts when safe, exploit status.
EOF

case "$HARNESS" in
  claude)
    MCPCFG="$OUTDIR/mcp.json"
    printf '{"mcpServers":{"pokearena":{"command":"%s/bin/pokearena-mcp","env":{"POKEARENA_GATEWAY_URL":"%s"}}}}\n' "$REPO" "$GW_WS" > "$MCPCFG"
    run_capped "$GAME_TIMEOUT" \
      claude -p "$PROMPT" --model "$MODEL" \
      --mcp-config "$MCPCFG" --strict-mcp-config \
      --permission-mode bypassPermissions \
      --disallowedTools "Bash,Edit,Write,Read,Glob,Grep,WebFetch,WebSearch,Task,TodoWrite,NotebookEdit" \
      --max-turns 400 > "$OUTDIR/$LABEL.log" 2>&1
    [ $? -eq 124 ] && echo "[play-live] $LABEL: killed after ${GAME_TIMEOUT}s wall-clock cap" >> "$OUTDIR/$LABEL.log"
    ;;
  agy)
    # Antigravity reads its MCP servers from ~/.gemini/antigravity-cli/mcp_config.json;
    # register pokearena there once (see docs/running-the-benchmark.md).
    agy -p "$PROMPT" --model "$MODEL" \
      --dangerously-skip-permissions --print-timeout 20m \
      > "$OUTDIR/$LABEL.log" 2>&1
    ;;
  *)
    echo "$LABEL FAILED: unknown harness $HARNESS -- want claude or agy"; exit 1 ;;
esac

# Authoritative result from the gateway — never trust the agent's self-report.
sleep 1
python3 "$HELP" winner "$GW_HTTP" "$BID" "$LABEL"
