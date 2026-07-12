#!/usr/bin/env bash
# Build the benchmark report end-to-end — no code, no hand-picked battle ids.
#
# For each live model it reconstructs a per-team WIN and LOSS replay from the
# battle ids that model's run recorded (see _bench_helpers.py: every result line
# now carries bid=<id>), then renders the standard report — each model's row
# expands to a strip of its sample battles (Genesis W, Genesis L, Keystone W, …).
# Leaderboard, Elo, head-to-head matrix, per-team, momentum, rosters all included.
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

# Reconstruct one WIN and one LOSS replay per model per team, wherever a battle
# id was recorded, so each model's row expands to a per-team win/loss strip.
# Files are named <stem>-<team>-<win|loss>.json and reused if already present.
# Teams from older runs that predate id recording contribute nothing here.
for d in "$AGENTIC"/cc-*/ "$AGENTIC"/agy-*/; do
  [ -d "$d" ] || continue
  b="$(basename "$d")"
  key="${b%-*}"; team="${b##*-}"
  stem="${key##*-}"   # cc-haiku -> haiku, agy-gemini -> gemini
  res="$d/results.txt"
  [ -f "$res" ] || continue

  win_bid="$(awk '/winner=0/ && /bid=/{for(i=1;i<=NF;i++) if($i ~ /^bid=/){sub(/^bid=/,"",$i); print $i; exit}}' "$res")"
  loss_bid="$(awk '/winner=1/ && /bid=/{for(i=1;i<=NF;i++) if($i ~ /^bid=/){sub(/^bid=/,"",$i); print $i; exit}}' "$res")"

  if [ -n "$win_bid" ] && [ ! -f "$AGENTIC/replays/$stem-$team-win.json" ]; then
    echo "[build-report] $stem/$team: reconstructing WIN from $win_bid"
    bash "$DIR/reconstruct-replay.sh" "$win_bid" "$key" "$team" "$stem-$team-win" "$AGENTIC" \
      || echo "[build-report] WARN: win reconstruct failed ($stem/$team)"
  fi
  if [ -n "$loss_bid" ] && [ ! -f "$AGENTIC/replays/$stem-$team-loss.json" ]; then
    echo "[build-report] $stem/$team: reconstructing LOSS from $loss_bid"
    bash "$DIR/reconstruct-replay.sh" "$loss_bid" "$key" "$team" "$stem-$team-loss" "$AGENTIC" \
      || echo "[build-report] WARN: loss reconstruct failed ($stem/$team)"
  fi
done

# A model that now has per-team sample files no longer needs an older single
# <stem>.json (it would just duplicate one team's win); drop it. A model with no
# per-team files (older runs, no recorded ids) keeps its single fallback.
for plain in "$AGENTIC"/replays/*.json; do
  [ -e "$plain" ] || continue
  base="$(basename "$plain" .json)"
  case "$base" in *-*) continue ;; esac   # per-team files contain a dash; skip
  if ls "$AGENTIC/replays/$base-"*.json >/dev/null 2>&1; then
    echo "[build-report] $base: dropping single replay (superseded by per-team samples)"
    rm -f "$plain"
  fi
done

"$REPO/bin/bench-report" -baseline "$BASELINE" -agentic "$AGENTIC" -ref heuristic -out "$OUT"
echo "[build-report] wrote $OUT"
