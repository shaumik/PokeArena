#!/usr/bin/env python3
"""Helpers for the agentic-battle runner scripts.

Kept out of the shell so the bash stays simple and portable (macOS ships
bash 3.2, which mishandles heredocs-in-$() and nested quoting). Uses only the
standard library.

Subcommands:
  picks     <library.json> <team-name>   -> the team's picks as compact JSON
  newbattle <gateway-http>               -> create a live vs-AI battle, print its id
  winner    <gateway-http> <bid> <label> -> print the authoritative result line
  tally     <results-file> <tag>         -> pooled win rate + Wilson 95% interval
"""
import json
import math
import sys
import urllib.request


def picks(lib_path, team):
    lib = json.load(open(lib_path))
    t = next((x for x in lib["teams"] if x["name"] == team), None)
    if t is None:
        sys.exit(f"team {team!r} not found in {lib_path}")
    print(json.dumps(t["picks"], separators=(",", ":")))


def newbattle(gw):
    body = json.dumps({"mode": "live", "p1_name": "Agent", "p2_name": "AI"}).encode()
    req = urllib.request.Request(
        gw + "/api/battles", data=body, headers={"content-type": "application/json"}
    )
    print(json.load(urllib.request.urlopen(req, timeout=5))["battle_id"])


def winner(gw, bid, label):
    d = json.load(urllib.request.urlopen(gw + f"/api/battles/{bid}", timeout=5))
    b = d.get("battle", {})
    w = b.get("winner")
    who = {0: "AGENT", 1: "AI", -1: "unfinished"}.get(w, str(w))
    # The battle id is recorded so the report builder can reconstruct this exact
    # game's replay from its persisted turns — no time-window guesswork.
    print(f"{label} winner={w} -> {who} (status={b.get('status')}) bid={bid}")


def tally(res_path, tag):
    w = l = o = 0
    for line in open(res_path):
        if "winner=0" in line:
            w += 1
        elif "winner=1" in line:
            l += 1
        elif "winner=" in line:
            o += 1
    n = w + l + o
    if not n:
        print(f"{tag}: no results")
        return
    p, z = w / n, 1.96
    d = 1 + z * z / n
    c = (p + z * z / (2 * n)) / d
    h = z / d * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n))
    print(
        f"{tag}: {w}-{l} (+{o} unfinished), n={n}  "
        f"win {100*p:.1f}%  95% CI [{100*(c-h):.1f}, {100*(c+h):.1f}]"
    )


if __name__ == "__main__":
    {"picks": picks, "newbattle": newbattle, "winner": winner, "tally": tally}[
        sys.argv[1]
    ](*sys.argv[2:])
