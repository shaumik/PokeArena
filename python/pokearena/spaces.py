"""Action and observation spaces, with or without Gymnasium installed.

``pokearena`` has zero required runtime dependencies, so the spaces are defined
against a duck type rather than a hard import. When Gymnasium *is* present the
real ``gymnasium.spaces`` classes are used, so wrappers and vector envs that
``isinstance``-check a space keep working; when it is not, a small local
stand-in with the same surface (``n``, ``sample``, ``contains``, ``seed``) takes
its place and nothing else in the package changes.

The action space is a fixed 11-way ``Discrete``, laid out by the engine and
reported in its handshake:

===========  ======================================================
index        meaning
===========  ======================================================
``0``–``3``  use move slot 0–3
``4``        Struggle / the forced move on a recharge or charge turn
``5``–``10`` switch to team slot 0–5
===========  ======================================================

It is deliberately fixed-size rather than "however many actions are legal right
now". Renumbering per turn would make the same integer mean different things at
different times, which quietly destroys any learned policy and any saved
trajectory. Legality is expressed as a **mask** instead — ``info["action_mask"]``
on every step — which is the convention RL libraries already understand.
"""

from __future__ import annotations

import random
from typing import Any, Optional, Sequence

__all__ = [
    "GYMNASIUM_AVAILABLE",
    "Discrete",
    "Space",
    "BattleObservationSpace",
    "ACTION_SPACE_SIZE",
    "MOVE_SLOTS",
    "STRUGGLE_INDEX",
    "SWITCH_BASE",
    "TEAM_SIZE",
    "action_space",
    "observation_space",
    "describe_action",
]

MOVE_SLOTS = 4
STRUGGLE_INDEX = 4
SWITCH_BASE = 5
TEAM_SIZE = 6
ACTION_SPACE_SIZE = MOVE_SLOTS + 1 + TEAM_SIZE  # 11

try:  # pragma: no cover - depends on the installed extras
    from gymnasium.spaces import Discrete, Space  # type: ignore

    GYMNASIUM_AVAILABLE = True
except Exception:  # pragma: no cover - the dependency-free path
    GYMNASIUM_AVAILABLE = False

    class Space:  # type: ignore[no-redef]
        """Minimal stand-in for ``gymnasium.spaces.Space``."""

        def __init__(self, shape=None, dtype=None, seed=None):
            self.shape = shape
            self.dtype = dtype
            self._rng = random.Random(seed)

        def seed(self, seed=None):
            self._rng = random.Random(seed)
            return [seed]

        def sample(self):
            raise NotImplementedError

        def contains(self, x: Any) -> bool:
            raise NotImplementedError

        def __contains__(self, x: Any) -> bool:
            return self.contains(x)

    class Discrete(Space):  # type: ignore[no-redef]
        """Minimal stand-in for ``gymnasium.spaces.Discrete``."""

        def __init__(self, n: int, seed=None, start: int = 0):
            super().__init__(shape=(), dtype="int64", seed=seed)
            self.n = int(n)
            self.start = int(start)

        def sample(self, mask: Optional[Sequence[int]] = None) -> int:
            choices = range(self.start, self.start + self.n)
            if mask is not None:
                choices = [i for i, m in zip(choices, mask) if m]
                if not choices:
                    return self.start
            return self._rng.choice(list(choices))

        def contains(self, x: Any) -> bool:
            try:
                i = int(x)
            except (TypeError, ValueError):
                return False
            return self.start <= i < self.start + self.n

        def __repr__(self) -> str:
            return f"Discrete({self.n})"


class BattleObservationSpace(Space):
    """The space of battle observations.

    An observation is the engine's fog-of-war ``View``, decoded from JSON: a
    nested dict with the viewer's own team in full and the opponent's *active*
    Pokémon only, already redacted. It is not a fixed-width vector and this
    package does not pretend it is one — flattening it is a modelling decision
    that belongs to the user, not to the environment, and every reasonable
    encoding (a text prompt for an LLM, a hand-built feature vector, a set
    encoder) wants different things.

    So the space's only claim is the true one: observations are JSON objects.
    """

    def __init__(self, seed=None):
        super().__init__(shape=None, dtype=None, seed=seed)

    def contains(self, x: Any) -> bool:
        return isinstance(x, dict)

    def sample(self):
        raise NotImplementedError(
            "battle observations cannot be sampled independently of a battle; "
            "call reset() to obtain one"
        )

    def __repr__(self) -> str:
        return "BattleObservationSpace()"


def action_space(seed=None) -> Discrete:
    """The fixed 11-way discrete action space."""
    return Discrete(ACTION_SPACE_SIZE, seed=seed)


def observation_space(seed=None) -> BattleObservationSpace:
    """The battle observation space."""
    return BattleObservationSpace(seed=seed)


def describe_action(index: int) -> str:
    """Render a flat action index in words, for logs and error messages."""
    i = int(index)
    if 0 <= i < MOVE_SLOTS:
        return f"use move slot {i}"
    if i == STRUGGLE_INDEX:
        return "Struggle / forced move"
    if SWITCH_BASE <= i < ACTION_SPACE_SIZE:
        return f"switch to team slot {i - SWITCH_BASE}"
    return f"invalid action {i}"
