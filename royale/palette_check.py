#!/usr/bin/env python3
"""Validate the team colours in report_template.html.

Two things must hold, and the first assignment of the previous run failed the
second one by pairing yellow against orange in the final:

  1. every colour is readable on its own ground — the light step on the light
     ground, the dark step on the dark ground (WCAG contrast >= 3.0, the
     large-text/graphical-object threshold, since team colour is only ever used
     on headings, names and chart strokes);
  2. the two sides of every actual match are far enough apart to tell apart —
     CIE76 dE >= 25 in both themes, checked on the pairs the bracket really
     produced rather than on all fifteen combinations.

Usage: python3 royale/palette_check.py [match-pairs.json]
       (defaults to the pairs in royale/tournament-meta.json)
"""
import json
import os
import re
import sys

ROOT = os.path.dirname(os.path.abspath(__file__))
LIGHT_GROUND = "#F4F4FA"
DARK_GROUND = "#0A0D17"


def srgb(h):
    h = h.lstrip("#")
    return [int(h[i:i + 2], 16) / 255 for i in (0, 2, 4)]


def lin(c):
    return c / 12.92 if c <= 0.04045 else ((c + 0.055) / 1.055) ** 2.4


def luminance(h):
    r, g, b = (lin(c) for c in srgb(h))
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def contrast(a, b):
    la, lb = luminance(a), luminance(b)
    hi, lo = max(la, lb), min(la, lb)
    return (hi + 0.05) / (lo + 0.05)


def lab(h):
    r, g, b = (lin(c) for c in srgb(h))
    x = (0.4124 * r + 0.3576 * g + 0.1805 * b) / 0.95047
    y = (0.2126 * r + 0.7152 * g + 0.0722 * b) / 1.00000
    z = (0.0193 * r + 0.1192 * g + 0.9505 * b) / 1.08883
    f = lambda t: t ** (1 / 3) if t > 0.008856 else 7.787 * t + 16 / 116
    fx, fy, fz = f(x), f(y), f(z)
    return (116 * fy - 16, 500 * (fx - fy), 200 * (fy - fz))


def de(a, b):
    la, lb = lab(a), lab(b)
    return sum((x - y) ** 2 for x, y in zip(la, lb)) ** 0.5


def colours():
    src = open(os.path.join(ROOT, "report_template.html")).read()
    block = re.search(r"const COLORS = \{(.*?)\};", src, re.S).group(1)
    return {m.group(1): [m.group(2), m.group(3)]
            for m in re.finditer(r'"([^"]+)":\s*\["(#\w{6})",\s*"(#\w{6})"\]', block)}


def main():
    cols = colours()
    ok = True
    print("readability — light step on light ground, dark step on dark ground")
    for name, (tl, td) in cols.items():
        cl, cd = contrast(tl, LIGHT_GROUND), contrast(td, DARK_GROUND)
        bad = cl < 3.0 or cd < 3.0
        ok &= not bad
        print(f"  {'FAIL' if bad else 'ok  '} {name:<18} light {tl} {cl:5.2f}:1   dark {td} {cd:5.2f}:1")

    pairs_path = sys.argv[1] if len(sys.argv) > 1 else os.path.join(ROOT, "tournament-meta.json")
    pairs = []
    if os.path.exists(pairs_path):
        meta = json.load(open(pairs_path))
        pairs = [tuple(p) for p in meta.get("colour_pairs", [])]
    if not pairs:
        print("\nno match pairs given — checking all combinations instead")
        names = list(cols)
        pairs = [(a, b) for i, a in enumerate(names) for b in names[i + 1:]]

    print("\nseparation — the two sides of each pairing, in both themes")
    for a, b in pairs:
        if a not in cols or b not in cols:
            print(f"  SKIP {a} vs {b}: not in the palette")
            ok = False
            continue
        dl, dd = de(cols[a][0], cols[b][0]), de(cols[a][1], cols[b][1])
        bad = min(dl, dd) < 25
        ok &= not bad
        print(f"  {'FAIL' if bad else 'ok  '} {a:<18} vs {b:<18} dE light {dl:5.1f}  dark {dd:5.1f}")

    print("\nPALETTE OK" if ok else "\nPALETTE FAILS")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
