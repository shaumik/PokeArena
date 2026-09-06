#!/usr/bin/env bash
# Build the per-model decision-quality table from a fresh attributed batch.
#
# Attribution lives only in the run dirs: each results.txt line carries the
# game's winner and its battle_id as "bid=<uuid>". The dir name's leading key
# ("cc-haiku", "agy-gemini", ...) maps to a model via eval.ModelDisplay. Postgres
# holds the turns but no model identity, so this script joins the two: for every
# COMPLETED game it exports the battle JSON from Postgres, writes a
# "model<TAB>export.json" manifest line, then hands the whole manifest to
# `decision-eval -manifest` which scores each battle and rolls up the table.
#
# Usage: decision-report.sh [results_root] [--json]
#   results_root defaults to /tmp/pk-agentic-v2
#
# Unfinished games (winner=-1, no completed battle) are skipped and counted —
# they have no scoreable decisions.
set -uo pipefail

ROOT="${1:-/tmp/pk-agentic-v2}"
JSON_FLAG=""
[ "${2:-}" = "--json" ] && JSON_FLAG="-json"

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$DIR/../.." && pwd)"
PG="pk-bench-postgres-1"
WORK="$(mktemp -d)"
MANIFEST="$WORK/manifest.tsv"
: > "$MANIFEST"

# key -> model display name, mirroring eval.ModelDisplay (kept in lockstep).
model_for() {
  case "$1" in
    cc-haiku)   echo "Claude Haiku 4.5" ;;
    cc-sonnet)  echo "Claude Sonnet 4.6" ;;
    cc-opus)    echo "Claude Opus 4.8" ;;
    agy-*)      echo "Gemini 3.1 Pro" ;;
    cc-*)       echo "Claude ${1#cc-}" ;;
    *)          echo "$1" ;;
  esac
}

skipped=0 exported=0
for res in "$ROOT"/*/results.txt; do
  [ -f "$res" ] || continue
  dir="$(basename "$(dirname "$res")")"        # e.g. cc-haiku-Genesis
  key="${dir%-*}"                               # strip trailing -<Team>
  model="$(model_for "$key")"
  while IFS= read -r line; do
    case "$line" in *winner=-1*) skipped=$((skipped+1)); continue ;; esac
    bid="${line##*bid=}"; bid="${bid%% *}"
    [ -n "$bid" ] || continue
    out="$WORK/$bid.json"
    docker exec "$PG" psql -U pokearena -d pokearena -tAX -c \
      "select json_build_object('seed', b.seed, 'winner', b.winner,
         'turns', (select json_agg(json_build_object('state', t.state_digest, 'log', t.log)
                   order by t.turn_no) from battle_turns t where t.battle_id=b.id))
       from battles b where b.id='$bid' and b.status='completed'" > "$out" 2>/dev/null
    if [ ! -s "$out" ] || [ "$(head -c 4 "$out")" = "" ]; then
      skipped=$((skipped+1)); rm -f "$out"; continue
    fi
    printf '%s\t%s\n' "$model" "$out" >> "$MANIFEST"
    exported=$((exported+1))
  done < "$res"
done

echo "# battles exported=$exported skipped(unfinished/missing)=$skipped" >&2
if [ "$exported" -eq 0 ]; then
  echo "no completed battles to score under $ROOT" >&2
  exit 1
fi

( cd "$REPO" && go run ./cmd/decision-eval -manifest "$MANIFEST" -data data $JSON_FLAG )
rc=$?
rm -rf "$WORK"
exit $rc
