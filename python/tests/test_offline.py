"""Tests that need no engine binary.

These cover the parts of the package that must work regardless: the action
space, the binary-discovery error message, and the promise that importing
``pokearena`` needs no third-party dependency.
"""

from __future__ import annotations

import os
import subprocess
import sys
import unittest
from pathlib import Path

import _support  # noqa: F401  - puts the package on sys.path

from pokearena import (
    ACTION_SPACE_SIZE,
    ENV_VAR,
    GO_INSTALL_COMMAND,
    MOVE_SLOTS,
    STRUGGLE_INDEX,
    SWITCH_BASE,
    TEAM_SIZE,
    BinaryNotFoundError,
    describe_action,
    find_binary,
)
from pokearena._util import extract_action
from pokearena.spaces import BattleObservationSpace, action_space, observation_space


class ActionSpaceTest(unittest.TestCase):
    def test_layout(self):
        self.assertEqual(ACTION_SPACE_SIZE, MOVE_SLOTS + 1 + TEAM_SIZE)
        self.assertEqual(ACTION_SPACE_SIZE, 11)
        self.assertEqual(STRUGGLE_INDEX, MOVE_SLOTS)
        self.assertEqual(SWITCH_BASE, MOVE_SLOTS + 1)

    def test_discrete_space(self):
        space = action_space(seed=0)
        self.assertEqual(space.n, ACTION_SPACE_SIZE)
        for _ in range(20):
            sample = space.sample()
            self.assertTrue(space.contains(sample))
        self.assertFalse(space.contains(ACTION_SPACE_SIZE))
        self.assertFalse(space.contains(-1))

    def test_observation_space_accepts_dicts(self):
        space = observation_space()
        self.assertIsInstance(space, BattleObservationSpace)
        self.assertTrue(space.contains({"me": 0}))
        self.assertFalse(space.contains([1, 2, 3]))
        with self.assertRaises(NotImplementedError):
            space.sample()

    def test_describe_action(self):
        self.assertEqual(describe_action(0), "use move slot 0")
        self.assertEqual(describe_action(STRUGGLE_INDEX), "Struggle / forced move")
        self.assertEqual(describe_action(SWITCH_BASE + 2), "switch to team slot 2")
        self.assertIn("invalid", describe_action(99))


class ExtractActionTest(unittest.TestCase):
    """Every action shape a caller plausibly holds, normalised without a round trip."""

    RECORD = {
        "index": 6,
        "action": {"kind": "switch", "index": 1},
        "label": "switch to Dragonite",
    }

    def test_legal_action_record_is_unwrapped(self):
        self.assertEqual(extract_action(self.RECORD), {"kind": "switch", "index": 1})

    def test_engine_object_passes_through(self):
        obj = {"kind": "move", "index": 1, "switch_target": 3}
        self.assertEqual(extract_action(obj), obj)

    def test_flat_index_and_int(self):
        self.assertEqual(extract_action(self.RECORD["index"]), 6)
        self.assertEqual(extract_action(0), 0)

    def test_numpy_style_scalar(self):
        class Scalar:
            def item(self):
                return 3

        self.assertEqual(extract_action(Scalar()), 3)

    def test_unusable_shapes_raise_here_not_over_the_wire(self):
        for bad in ({"label": "use Psystrike"}, {}, {"kind": "forfeit", "index": 0}, None, True):
            with self.subTest(bad=bad):
                with self.assertRaises(TypeError):
                    extract_action(bad)

    def test_error_names_the_shapes_that_work(self):
        with self.assertRaises(TypeError) as ctx:
            extract_action({"label": "nope"})
        message = str(ctx.exception)
        self.assertIn("legal_actions()", message)
        self.assertIn("kind", message)


class BinaryDiscoveryTest(unittest.TestCase):
    def test_missing_binary_error_is_actionable(self):
        with self.assertRaises(BinaryNotFoundError) as ctx:
            find_binary("/nonexistent/path/to/pokearena-env")
        self.assertIn("pokearena-env", str(ctx.exception))

    def test_error_when_nothing_is_installed_names_both_routes(self):
        env = {
            k: v
            for k, v in os.environ.items()
            if k not in ("PATH", ENV_VAR, "GOBIN", "GOPATH")
        }
        env["PATH"] = "/nonexistent-bin"
        env["HOME"] = "/nonexistent-home"
        script = (
            "import sys; sys.path.insert(0, %r)\n"
            "from pokearena import find_binary\n"
            "try:\n"
            "    find_binary()\n"
            "except Exception as exc:\n"
            "    print(exc)\n"
            "else:\n"
            "    print('FOUND')\n"
        ) % str(Path(__file__).resolve().parents[1])
        out = subprocess.run(
            [sys.executable, "-c", script], capture_output=True, text=True, env=env
        ).stdout
        if "FOUND" in out:
            self.skipTest("a pokearena-env binary is reachable even with PATH cleared")
        self.assertIn(GO_INSTALL_COMMAND, out)
        self.assertIn(ENV_VAR, out)
        self.assertIn("releases", out)


class NoRequiredDependenciesTest(unittest.TestCase):
    def test_import_without_third_party_packages(self):
        """Importing the package must not require gymnasium, pettingzoo or numpy."""
        script = (
            "import sys\n"
            "sys.path.insert(0, %r)\n"
            "for name in ('gymnasium', 'pettingzoo', 'numpy', 'gym'):\n"
            "    sys.modules[name] = None\n"
            "import pokearena\n"
            "env = pokearena.PokeArenaEnv\n"
            "assert pokearena.ACTION_SPACE_SIZE == 11\n"
            "print('OK', pokearena.__version__)\n"
        ) % str(Path(__file__).resolve().parents[1])
        proc = subprocess.run([sys.executable, "-c", script], capture_output=True, text=True)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn("OK", proc.stdout)


if __name__ == "__main__":
    unittest.main()
