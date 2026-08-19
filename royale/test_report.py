#!/usr/bin/env python3
"""Tests for the report generator's parsing of referee reports.

The generator reads markdown written by agents, which is the least trustworthy
input in the whole pipeline: it is prose, it is not validated anywhere, and a
parsing slip turns into a factual claim on a published page. Both tests below
pin bugs that actually shipped.

Run: python3 royale/test_report.py   (also wired into `make test-royale`)
"""
import importlib.util
import os
import unittest

ROOT = os.path.dirname(os.path.abspath(__file__))
_spec = importlib.util.spec_from_file_location("build_report", os.path.join(ROOT, "build_report.py"))
br = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(br)


class BugVerdicts(unittest.TestCase):
    """bug_verdicts counts the findings a referee filed.

    It used to count the bare word CONFIRMED anywhere in the section, so a
    referee's prose scored as a finding. r1m1 of the second tournament filed
    "NONE OBSERVED" and then wrote "Confirmed." at the end of a suspicion it
    had just run to ground — which gave the match one confirmed bug and
    painted a clean audit as a dirty one on the published page.
    """

    def test_prose_is_not_a_verdict(self):
        md = """## BUGS

**NONE OBSERVED.**

**NOT-A-BUG — Close Combat did 65, which I thought was unreachable.** My
constant was wrong, not the engine: with 5325 and the half-down bias, roll 88
gives exactly 65. Confirmed.
"""
        v = br.bug_verdicts(md)
        self.assertEqual(v["confirmed"], 0, "the word 'Confirmed.' in prose is not a filed finding")
        self.assertEqual(v["not_a_bug"], 1)
        self.assertTrue(v["clean"], "a NONE OBSERVED audit with no confirmed findings is clean")

    def test_counts_labels_in_every_shape_referees_used(self):
        md = """## BUGS

**CONFIRMED: Fake Out has no first-turn restriction.** Details.

- **NOT-A-BUG** — Trick Room duration. Details.
- UNCERTAIN — something I could not settle.

NOT-A-BUG — Sheer Force is a final multiplier. Details.
"""
        v = br.bug_verdicts(md)
        self.assertEqual((v["confirmed"], v["not_a_bug"], v["uncertain"]), (1, 2, 1))
        self.assertFalse(v["clean"])

    def test_no_section_is_not_a_clean_audit(self):
        v = br.bug_verdicts("## Verdict\n\nSomebody won.\n")
        self.assertFalse(v["filed"], "a missing BUGS section must not read as a filed report")
        self.assertFalse(v["clean"], "not filing an audit is not the same as filing a clean one")


class SectionParsing(unittest.TestCase):
    def test_section_of_stops_at_the_next_heading(self):
        md = "## Verdict\n\nA won.\n\n## MVP\n\nB.\n"
        self.assertEqual(br.section_of(md, "Verdict"), "A won.")
        self.assertEqual(br.section_of(md, "MVP"), "B.")

    def test_section_of_is_case_insensitive_and_absent_is_empty(self):
        self.assertEqual(br.section_of("## bugs\n\nnone\n", "BUGS"), "none")
        self.assertEqual(br.section_of("## Verdict\n\nx\n", "Scorecard"), "")


class Markdown(unittest.TestCase):
    def test_html_is_escaped_before_inline_formatting(self):
        """Reports are agent-authored text rendered into a page; a stray angle
        bracket must not become markup."""
        out = br.md_to_html("A **bold** claim about <script>alert(1)</script>")
        self.assertIn("<strong>bold</strong>", out)
        self.assertNotIn("<script>", out)
        self.assertIn("&lt;script&gt;", out)

    def test_lists_and_headings_close_properly(self):
        out = br.md_to_html("## Head\n\n- one\n- two\n\nAfter.")
        self.assertEqual(out.count("<ul>"), out.count("</ul>"))
        self.assertIn("<li>one</li>", out)
        self.assertIn("<p>After.</p>", out)


if __name__ == "__main__":
    unittest.main(verbosity=2)
