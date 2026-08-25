"""Protocol-level tests against a live engine subprocess."""

from __future__ import annotations

import unittest

from _support import OTHER_TEAM, SMALL_TEAM, requires_engine

from pokearena import (
    ACTION_SPACE_SIZE,
    PROTOCOL_VERSION,
    EngineClient,
    EngineError,
    IllegalActionError,
)


@requires_engine
class HandshakeTest(unittest.TestCase):
    def test_reports_provenance(self):
        with EngineClient() as engine:
            hs = engine.handshake
            self.assertEqual(
                hs["protocol_version"].split(".")[0],
                PROTOCOL_VERSION.split(".")[0],
                "engine and client disagree on the protocol major version",
            )
            for key in ("engine_revision", "ruleset", "dataset", "team_library", "action_space"):
                self.assertIn(key, hs)
            self.assertEqual(hs["action_space"]["n"], ACTION_SPACE_SIZE)
            self.assertEqual(hs["level"], 50)
            data = hs["dataset"]
            self.assertTrue(data["sim_version"], "dataset sim_version is empty")
            self.assertTrue(data["curation_sha"], "dataset curation_sha is empty")
            self.assertGreater(data["species"], 0)
            self.assertTrue(hs["team_library"]["teams"], "no curated teams reported")


@requires_engine
class ProtocolTest(unittest.TestCase):
    def setUp(self):
        self.engine = EngineClient()
        self.addCleanup(self.engine.close)

    def test_reset_returns_observation_and_legal_actions(self):
        result = self.engine.request(
            "reset", {"seed": 1, "team": SMALL_TEAM, "agents": ["external", "heuristic"]}
        )
        self.assertEqual(result["turn"], 0)
        self.assertEqual(result["phase"], "choosing")
        self.assertFalse(result["terminated"])
        self.assertIsNotNone(result["observations"][0])
        self.assertIsNone(result["observations"][1], "the baseline side's view was returned")
        self.assertTrue(result["legal_actions"][0])
        self.assertEqual(len(result["action_mask"][0]), ACTION_SPACE_SIZE)

    def test_illegal_action_is_recoverable(self):
        self.engine.request("reset", {"seed": 1, "team": SMALL_TEAM})
        with self.assertRaises(IllegalActionError) as ctx:
            # Flat index 5 switches to team slot 0, which is already active.
            self.engine.request("step", {"action": 5})
        self.assertTrue(ctx.exception.legal_actions)
        self.assertEqual(len(ctx.exception.action_mask), ACTION_SPACE_SIZE)

        # The rejection left the episode alone, so a legal action still works
        # and the battle is still on turn 0.
        after = self.engine.request("step", {"action": 0})
        self.assertEqual(after["turn"], 1)
        self.assertEqual(after["info"]["decision_index"], 1)

    def test_errors_have_codes(self):
        fresh = EngineClient()
        self.addCleanup(fresh.close)
        with self.assertRaises(EngineError) as ctx:
            fresh.request("step", {"action": 0})
        self.assertEqual(ctx.exception.code, "no_episode")

        with self.assertRaises(EngineError) as ctx:
            fresh.request("teleport")
        self.assertEqual(ctx.exception.code, "unknown_command")

        with self.assertRaises(EngineError) as ctx:
            fresh.request("reset", {"seed": 1, "team": "NoSuchTeam"})
        self.assertEqual(ctx.exception.code, "bad_request")

    def test_object_action_form_is_accepted(self):
        self.engine.request("reset", {"seed": 3, "team": SMALL_TEAM})
        result = self.engine.request("step", {"action": {"kind": "move", "index": 0}})
        self.assertEqual(result["turn"], 1)

    def test_asymmetric_teams(self):
        result = self.engine.request(
            "reset",
            {
                "seed": 5,
                "team": SMALL_TEAM,
                "opponent_team": OTHER_TEAM,
                "agents": ["external", "heuristic"],
            },
        )
        own = [p["name"] for p in result["observations"][0]["self"]["team"]]
        self.assertEqual(len(own), len(SMALL_TEAM))
        self.assertNotIn(result["observations"][0]["foe"]["name"], own)


@requires_engine
class LifecycleTest(unittest.TestCase):
    def test_close_is_idempotent_and_terminates_the_process(self):
        engine = EngineClient()
        proc = engine._proc  # noqa: SLF001 - asserting on the subprocess is the point
        self.assertIsNotNone(proc)
        engine.close()
        engine.close()
        self.assertIsNotNone(proc.poll(), "the engine subprocess is still running after close()")
        self.assertTrue(engine.closed)

    def test_context_manager_closes(self):
        with EngineClient() as engine:
            proc = engine._proc  # noqa: SLF001
        self.assertIsNotNone(proc.poll(), "the engine subprocess outlived its context manager")

    def test_request_after_close_raises(self):
        engine = EngineClient()
        engine.close()
        with self.assertRaises(Exception):
            engine.request("handshake")


if __name__ == "__main__":
    unittest.main()
