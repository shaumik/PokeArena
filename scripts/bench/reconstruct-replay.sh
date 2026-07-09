#!/usr/bin/env bash
# Reconstruct a watchable Replay for one live model-vs-reference battle from its
# persisted turns in Postgres, and write it where the benchmark report picks it
# up (<agentic-out>/replays/<name>.json).
#
# A live model plays over the gateway, so its game can't be re-simulated from a
# seed like a baseline can — but every turn's engine state was stored, so the
# replay is rebuilt from that. Pick a representative battle (e.g. one the model
# WON) and label it with the model's display name and team.
#
# Usage: reconstruct-replay.sh <battle-id> <model-display-name> <team> <out-name> [agentic-out]
#   reconstruct-replay.sh d665b72e-... "Claude Haiku 4.5" Blitz haiku
#
# Prereqs: the stack is up (the postgres container holds the turns) and
# bin/db-replay is built (go build -o bin/db-replay ./cmd/db-replay).
set -uo pipefail

BID="${1:?battle id}"
DISP="${2:?model display name, e.g. \"Claude Haiku 4.5\"}"
TEAM="${3:?team name}"
NAME="${4:?output file stem, e.g. haiku}"
OUT="${5:-/tmp/pk-agentic}"

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$DIR/../.." && pwd)"
PG="${POKEARENA_PG_CONTAINER:-pk-bench-postgres-1}"
mkdir -p "$OUT/replays"

# One battle as {seed, winner, turns:[{state, log}]} — the shape db-replay reads.
QUERY="SELECT json_build_object(
  'seed', b.seed, 'winner', b.winner,
  'turns', (SELECT json_agg(json_build_object('state', t.state_digest, 'log', t.log) ORDER BY t.turn_no)
            FROM battle_turns t WHERE t.battle_id = b.id)
) FROM battles b WHERE b.id = '$BID';"

docker exec "$PG" psql -U pokearena -d pokearena -t -A -c "$QUERY" \
  | "$REPO/bin/db-replay" -side0 "$DISP" -side1 heuristic -team "$TEAM" -out "$OUT/replays/$NAME.json"
