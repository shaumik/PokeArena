#!/usr/bin/env bash
# Run N live agentic battles (one harness+model+team) concurrently, pool the
# outcomes, and print the win rate with a Wilson 95% confidence interval — the
# same statistic cmd/bench reports, so agentic and thin-harness numbers line up.
#
# Usage: run-batch.sh <claude|agy> <model> <team> <N> <concurrency> <tag>
#   run-batch.sh claude sonnet Genesis 20 3 claude-sonnet
#   run-batch.sh agy "Gemini 3.1 Pro (High)" Genesis 20 2 agy-gemini
#
# Each battle is a full agent session, so keep concurrency modest (2-3): every
# live battle also runs an expectimax opponent server-side.
set -uo pipefail

export HARNESS="${1:?harness: claude or agy}"
export MODEL="${2:?model}"
export TEAM="${3:?team name}"
N="${4:-20}"
CONC="${5:-3}"
TAG="${6:?tag naming the output dir for this run}"

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export OUTDIR="${POKEARENA_OUT:-/tmp/pokearena-agentic}/$TAG"
mkdir -p "$OUTDIR"
export RES="$OUTDIR/results.txt"
export PLAY="$DIR/play-live.sh"
: > "$RES"

seq 1 "$N" | xargs -P "$CONC" -I {} bash -c 'bash "$PLAY" "$HARNESS" "$MODEL" "$TEAM" "g$1" "$OUTDIR" | tee -a "$RES"' _ {}

echo "=== $TAG COMPLETE ==="
python3 "$DIR/_bench_helpers.py" tally "$RES" "$TAG"
