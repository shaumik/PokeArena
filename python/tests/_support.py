"""Shared test helpers.

The tests are written with :mod:`unittest` so they run under a bare
interpreter (``python -m unittest discover -s tests``) as well as under pytest.
That matters here: the package has no required dependencies, and a test suite
that needed one to run would be quietly asserting the opposite of what the
package claims.

Everything that touches the engine skips — loudly, with an actionable reason —
when the ``pokearena-env`` binary is not installed.
"""

from __future__ import annotations

import os
import sys
import unittest
from pathlib import Path

# Make the package importable when the suite is run from anywhere.
_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from pokearena import GO_INSTALL_COMMAND, BinaryNotFoundError, find_binary  # noqa: E402

try:
    ENGINE_BINARY = find_binary(os.environ.get("POKEARENA_ENV_BIN"))
    SKIP_REASON = ""
except BinaryNotFoundError:
    ENGINE_BINARY = None
    SKIP_REASON = (
        "the pokearena-env engine binary is not installed, so the engine-backed "
        "tests cannot run. Install it with:\n"
        f"    {GO_INSTALL_COMMAND}\n"
        "or download a release binary and point POKEARENA_ENV_BIN at it. "
        "This is a skip, not a pass: nothing about the engine was verified."
    )

#: Decorator for every test that needs a live engine.
requires_engine = unittest.skipIf(ENGINE_BINARY is None, SKIP_REASON)

#: A small ad-hoc team, so tests do not depend on the curated library's contents.
SMALL_TEAM = [150, 149, 143]
OTHER_TEAM = [6, 9, 3]


def rollout(env, seed=0, policy=None, max_steps=1000):
    """Play one full single-agent episode and return ``(steps, info)``.

    The default policy is "first legal action", which is deterministic and
    therefore usable as a reproducibility probe.
    """
    obs, info = env.reset(seed=seed)
    steps = 0
    terminated = truncated = False
    while not (terminated or truncated):
        if steps >= max_steps:
            raise AssertionError(f"episode did not finish within {max_steps} steps")
        action = (policy or _first_legal)(obs, info)
        obs, _reward, terminated, truncated, info = env.step(action)
        steps += 1
    return steps, info


def _first_legal(_obs, info):
    legal = info["legal_actions"]
    assert legal, "no legal actions at a decision point"
    return legal[0]["index"]
