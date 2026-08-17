#!/usr/bin/env python3
"""Render the tournament report: royale/tournament.json -> a standalone HTML page.

Everything the page needs is inlined — no CDN, no fetch. The digest is embedded
as one JSON blob and the page's replay scrubber reads it client-side, so the
reader can step through any match turn by turn.

Usage: python3 royale/digest.py && python3 royale/build_report.py
"""
import html
import json
import os
import re
import sys

ROOT = os.path.dirname(os.path.abspath(__file__))
TEMPLATE = os.path.join(ROOT, "report_template.html")
OUT = os.path.join(ROOT, "tournament-report.html")

# Team colours live in report_template.html, which needs both the light and the
# dark step of each hue to hand CSS. Keeping a second copy here only invited the
# two to drift.

TEAM_TAG = {
    "The Bulwark": "STALL",
    "Spike Cartel": "HAZARD",
    "Hairtrigger": "GLASS",
    "Solaris": "SUN",
    "Deep Room": "TRICK ROOM",
    "The Apothecary": "STATUS",
}


def md_to_html(md):
    """Small markdown renderer for the judge and player reports.

    Deliberately minimal: headings, bullets, bold, inline code, paragraphs.
    Those are the only constructs the report prompts ask for, and a full parser
    would be more surface than the job needs.
    """
    if not md:
        return ""
    out, in_ul = [], False

    def inline(s):
        s = html.escape(s)
        s = re.sub(r"\*\*(.+?)\*\*", r"<strong>\1</strong>", s)
        s = re.sub(r"`(.+?)`", r"<code>\1</code>", s)
        s = re.sub(r"(?<!\*)\*([^*]+?)\*(?!\*)", r"<em>\1</em>", s)
        return s

    for raw in md.splitlines():
        line = raw.rstrip()
        if line.startswith("```"):
            continue
        m = re.match(r"^(#{1,6})\s+(.*)$", line)
        if m:
            if in_ul:
                out.append("</ul>")
                in_ul = False
            lvl = min(len(m.group(1)) + 2, 6)
            out.append(f"<h{lvl}>{inline(m.group(2))}</h{lvl}>")
            continue
        m = re.match(r"^\s*[-*]\s+(.*)$", line)
        if m:
            if not in_ul:
                out.append("<ul>")
                in_ul = True
            out.append(f"<li>{inline(m.group(1))}</li>")
            continue
        if not line.strip():
            if in_ul:
                out.append("</ul>")
                in_ul = False
            continue
        if in_ul:
            out.append("</ul>")
            in_ul = False
        out.append(f"<p>{inline(line)}</p>")
    if in_ul:
        out.append("</ul>")
    return "\n".join(out)


def section_of(md, heading):
    """Pull one '## Heading' section out of a report, as raw markdown."""
    if not md:
        return ""
    pat = re.compile(r"^##\s+" + re.escape(heading) + r"\s*$(.*?)(?=^##\s|\Z)",
                     re.M | re.S | re.I)
    m = pat.search(md)
    return m.group(1).strip() if m else ""


def bug_verdicts(md):
    """Count CONFIRMED / NOT-A-BUG / UNCERTAIN calls in a judge's BUGS section."""
    body = section_of(md, "BUGS")
    if not body:
        # No section filed is not the same as a clean audit — say so.
        return {"confirmed": 0, "not_a_bug": 0, "uncertain": 0, "clean": False, "filed": False}
    up = body.upper()
    return {
        "confirmed": len(re.findall(r"\bCONFIRMED\b", up)),
        "not_a_bug": len(re.findall(r"NOT[- ]A[- ]BUG", up)),
        "uncertain": len(re.findall(r"\bUNCERTAIN\b", up)),
        "clean": "NONE OBSERVED" in up and "CONFIRMED" not in up,
        "filed": True,
    }


def enrich(data):
    """Add everything the page renders but the digest doesn't compute."""
    for m in data["matches"]:
        for t in m["trainers"]:
            t["tag"] = TEAM_TAG.get(t["name"], "")
        judge = m["reports"].get("judge", "")
        m["judge_sections"] = {
            k: md_to_html(section_of(judge, k))
            for k in ("Verdict", "The story", "Scorecard", "MVP", "Notable turns", "BUGS")
        }
        m["judge_raw"] = judge
        m["bugs"] = bug_verdicts(judge)
        for slot in ("p1", "p2"):
            m["reports"][slot + "_html"] = md_to_html(m["reports"].get(slot, ""))
        # Total-HP curve per side: one point per resolution, for the swing chart.
        curves = [[], []]
        for t in m["turns_log"]:
            for s in range(2):
                team = t["sides"][s]["team"]
                tot = sum(x["hp"] for x in team)
                mx = sum(x["max_hp"] for x in team)
                curves[s].append(round(100.0 * tot / mx, 1) if mx else 0)
        m["curves"] = curves
    return data


def build_ladder(data, standings):
    """Standings are authored by the organiser (bracket placement is a tournament
    fact, not something derivable from match rows alone) and enriched here."""
    for row in standings:
        row["tag"] = TEAM_TAG.get(row["name"], "")
    return standings


def main():
    data = json.load(open(os.path.join(ROOT, "tournament.json")))
    data = enrich(data)

    meta_path = os.path.join(ROOT, "tournament-meta.json")
    meta = json.load(open(meta_path)) if os.path.exists(meta_path) else {}
    data["bracket"] = meta.get("bracket", [])
    # The digest walks the battles directory, which is alphabetical — "final"
    # sorts first. Read the page in bracket order instead.
    order = [mid for col in data["bracket"] for mid in col.get("matches", [])]
    data["matches"].sort(key=lambda m: order.index(m["id"]) if m["id"] in order else 99)
    data["standings"] = build_ladder(data, meta.get("standings", []))
    data["champion"] = meta.get("champion", "")
    data["headline"] = meta.get("headline", "")
    data["god_notes"] = md_to_html(meta.get("god_notes", ""))
    data["rules"] = meta.get("rules", [])
    data["team_tags"] = TEAM_TAG

    tpl = open(TEMPLATE).read()
    blob = json.dumps(data, separators=(",", ":"))
    # Keep the JSON from terminating the script element it lives in.
    blob = blob.replace("</", "<\\/")
    out = tpl.replace("/*__DATA__*/null", blob)
    with open(OUT, "w") as f:
        f.write(out)
    print(f"wrote {OUT} ({len(out)/1024:.0f} KB, {len(data['matches'])} matches)")


if __name__ == "__main__":
    sys.exit(main())
