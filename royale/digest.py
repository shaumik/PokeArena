#!/usr/bin/env python3
"""Fold every finished royale match into one tournament.json.

The HTML report should never have to re-parse engine log text. This walks each
match directory, reads meta.json + log.jsonl, and derives the things a reader
actually wants: an exact HP timeline per Pokemon (taken from the snapshots the
broker already records, not scraped from prose), a KO timeline, the moves each
side actually used, and both agents' private one-line reasoning.

Usage: python3 royale/digest.py [match-id ...]   # default: every match
"""
import json
import os
import sys

# ROYALE_ROOT lets a test point the digest at a tournament directory it built
# itself. Unset — the normal case — it is this file's own directory.
ROOT = os.environ.get("ROYALE_ROOT") or os.path.dirname(os.path.abspath(__file__))
BATTLES = os.path.join(ROOT, "battles")
REPORTS = os.path.join(ROOT, "reports")


def read_records(d):
    p = os.path.join(d, "log.jsonl")
    if not os.path.exists(p):
        return []
    out = []
    with open(p) as f:
        for line in f:
            line = line.strip()
            if line:
                out.append(json.loads(line))
    return out


def codename_map(meta):
    """codename -> real name, for the two seats of a match.

    Players see each other by codename only, and the engine is handed those
    codenames as the sides' trainer names so no battle line can carry a real
    one. That is the right answer during the match and the wrong one after it:
    the report is the referee's artifact, it labels everything else by real
    name, and a page that reads "Indigo won the battle!" under a heading that
    says The Low Ceiling is a page that makes the reader do the decoding.
    """
    out = {}
    for t in meta.get("trainers", []):
        cn, name = (t.get("codename") or "").strip(), (t.get("name") or "").strip()
        if cn and name and cn != name:
            out[cn] = name
    return out


def deanonymize(text, mapping):
    """Put the real names back into one line of engine text."""
    for cn, name in mapping.items():
        text = text.replace(cn, name)
    return text


def split_why(label):
    """Actions are stored as 'Alakazam used Psychic  // reason'."""
    if not label:
        return "", ""
    if "  // " in label:
        act, why = label.split("  // ", 1)
        return act.strip(), why.strip()
    return label.strip(), ""


def digest_match(mid):
    d = os.path.join(BATTLES, mid)
    meta = json.load(open(os.path.join(d, "meta.json")))
    state = json.load(open(os.path.join(d, "state.json")))
    recs = read_records(d)
    names = codename_map(meta)

    # Roster, resolved from the first snapshot so we get battle-instance names.
    rosters = [[], []]
    if recs:
        for s in range(2):
            rosters[s] = [m["name"] for m in recs[0]["after"]["sides"][s]["team"]]

    turns = []
    prev = None
    kos = []
    for r in recs:
        after = r["after"]
        entry = {
            "n": r["n"],
            # The record stamps the turn it was chosen on; the snapshot carries
            # the turn the engine had advanced to. Readers count the latter.
            "turn": after.get("turn", r["turn"]),
            "phase": r["phase"],
            "lines": [deanonymize(l["text"], names) for l in (r.get("lines") or [])],
            "weather": after.get("weather", ""),
            "terrain": after.get("terrain", ""),
            "actions": [],
            "sides": [],
            "verdict": deanonymize(r.get("verdict", ""), names),
        }
        for s in range(2):
            act, why = split_why(r["actions"][s])
            entry["actions"].append({"trainer": meta["trainers"][s]["name"], "action": act, "why": why})
            side = after["sides"][s]
            mons = []
            for i, m in enumerate(side["team"]):
                hp_pct = round(100.0 * m["hp"] / m["max_hp"]) if m["max_hp"] else 0
                delta = None
                if prev:
                    pm = prev["sides"][s]["team"][i]
                    pp = round(100.0 * pm["hp"] / pm["max_hp"]) if pm["max_hp"] else 0
                    delta = hp_pct - pp
                    if m.get("fainted") and not pm.get("fainted"):
                        kos.append({"turn": r["turn"], "n": r["n"], "side": s,
                                    "trainer": meta["trainers"][s]["name"], "mon": m["name"]})
                mons.append({
                    "name": m["name"], "hp": m["hp"], "max_hp": m["max_hp"], "pct": hp_pct,
                    "status": m.get("status", ""), "fainted": bool(m.get("fainted")),
                    "active": bool(m.get("active")), "delta": delta,
                })
            entry["sides"].append({
                "trainer": side["trainer"], "team": mons,
                "hazards": side.get("hazards", "none"), "screens": side.get("screens", "none"),
                "alive": sum(0 if m["fainted"] else 1 for m in mons),
            })
        turns.append(entry)
        prev = after

    # Move usage, per side.
    usage = [{}, {}]
    for t in turns:
        for s, a in enumerate(t["actions"]):
            act = a["action"]
            if " used " in act:
                mv = act.split(" used ", 1)[1]
                usage[s][mv] = usage[s].get(mv, 0) + 1

    final = turns[-1] if turns else None
    winner = state.get("winner", -1)
    result = {
        "id": mid,
        "round": meta.get("round", ""),
        "seed": meta.get("seed"),
        "max_turns": meta.get("max_turns"),
        "turns": state.get("turn", 0),
        "resolutions": len(recs),
        "winner_index": winner,
        "winner": meta["trainers"][winner]["name"] if winner in (0, 1) else ("draw" if winner == 2 else ""),
        "ended": state.get("phase") == "ended",
        "trainers": [
            {
                "name": meta["trainers"][s]["name"],
                "theme": meta["trainers"][s]["theme"],
                "slot": "p1" if s == 0 else "p2",
                "roster": rosters[s],
                "picks": meta["trainers"][s]["picks"],
                "alive": final["sides"][s]["alive"] if final else 6,
                "moves_used": usage[s],
            }
            for s in range(2)
        ],
        "kos": kos,
        "turns_log": turns,
        "reports": {},
    }
    for who in ("p1", "p2", "judge"):
        p = os.path.join(REPORTS, f"{mid}-{who}.md")
        if os.path.exists(p):
            result["reports"][who] = open(p).read()
    return result


def main():
    ids = sys.argv[1:] or sorted(os.listdir(BATTLES))
    matches = []
    for mid in ids:
        if not os.path.isdir(os.path.join(BATTLES, mid)):
            continue
        try:
            matches.append(digest_match(mid))
        except Exception as e:  # a half-written match should not kill the digest
            print(f"skip {mid}: {e}", file=sys.stderr)
    out = {"matches": matches}
    path = os.path.join(ROOT, "tournament.json")
    with open(path, "w") as f:
        json.dump(out, f, indent=1)
    for m in matches:
        print(f"{m['id']:6s} {m['round']:22s} {m['trainers'][0]['name']:16s} vs {m['trainers'][1]['name']:16s} "
              f"-> {m['winner'] or 'in progress':16s} {m['trainers'][0]['alive']}-{m['trainers'][1]['alive']} "
              f"in {m['turns']} turns ({m['resolutions']} resolutions)")
    print(f"\nwrote {path}")


if __name__ == "__main__":
    main()
