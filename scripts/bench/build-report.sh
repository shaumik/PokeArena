#!/usr/bin/env bash
# Build the benchmark report end-to-end — no code, no hand-picked battle ids.
#
# For each live model it reconstructs one won battle's replay from the battle id
# that model's run recorded (see _bench_helpers.py: every result line now carries
# bid=<id>), then renders the standard report with every model's game watchable:
# leaderboard, Elo, head-to-head matrix, per-team, momentum, rosters, replays.
#
# Usage: build-report.sh [agentic-dir] [baseline-trace] [out-html]
#   build-report.sh
#     -> /tmp/pk-agentic + runs/arm1-baseline.jsonl -> reports/benchmark.html
#   build-report.sh /tmp/pk-agentic runs/arm1-baseline.jsonl reports/benchmark.html
#
# Idempotent and mostly offline: a replay already under <agentic-dir>/replays is
# reused as-is; only a model missing its replay is reconstructed, which needs the
# stack up (postgres holds the turns). A model with no recorded won-battle id is
# left replayless (its matrix cell still shows the stats) and logged, so nothing
# is silently dropped.
set -uo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$DIR/../.." && pwd)"
cd "$REPO" || exit 1

AGENTIC="${1:-/tmp/pk-agentic}"
BASELINE="${2:-$REPO/runs/arm1-baseline.jsonl}"
OUT="${3:-$REPO/reports/benchmark.html}"

go build -o "$REPO/bin/bench-report" ./cmd/bench-report || exit 1
go build -o "$REPO/bin/db-replay" ./cmd/db-replay || exit 1
mkdir -p "$AGENTIC/replays" "$(dirname "$OUT")"

# The distinct live-model keys present (cc-haiku, agy-gemini, ...), each backing
# one contestant on the board regardless of how many teams it played.
keys="$(for d in "$AGENTIC"/*/; do
  b="$(basename "$d")"; k="${b%-*}"
  case "$k" in cc-*|agy-*) echo "$k" ;; esac
done | sort -u)"

for key in $keys; do
  stem="${key##*-}"   # cc-haiku -> haiku, agy-gemini -> gemini
  if [ -f "$AGENTIC/replays/$stem.json" ]; then
    echo "[build-report] $key: replay present, reusing"
    continue
  fi

  # First team (alphabetical) where this model recorded a won battle with an id.
  found=""
  for d in "$AGENTIC/$key"-*/; do
    [ -d "$d" ] || continue
    res="$d/results.txt"
    [ -f "$res" ] || continue
    team="$(basename "$d")"; team="${team##*-}"
    bid="$(awk '/winner=0/ && /bid=/{for(i=1;i<=NF;i++) if($i ~ /^bid=/){sub(/^bid=/,"",$i); print $i; exit}}' "$res")"
    [ -n "$bid" ] || continue
    echo "[build-report] $key: reconstructing replay from battle $bid (team $team)"
    if bash "$DIR/reconstruct-replay.sh" "$bid" "$key" "$team" "$stem" "$AGENTIC"; then
      found=1
      break
    fi
    echo "[build-report] WARN: could not reconstruct $key from $bid (is the stack up?)"
  done
  [ -n "$found" ] || echo "[build-report] $key: no won-battle id recorded yet -> replayless cell"
done

"$REPO/bin/bench-report" -baseline "$BASELINE" -agentic "$AGENTIC" -ref heuristic -out "$OUT"
echo "[build-report] wrote $OUT"
