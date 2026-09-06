"""Exception types for the PokéArena environment.

Every failure mode gets its own class so a caller can react to it rather than
matching on a message. The one that matters most is
:class:`BinaryNotFoundError`: the Go engine binary is a real prerequisite, and
a wrapper that failed silently — or fell back to some fake environment — would
be worse than one that refuses to start.
"""

from __future__ import annotations

from typing import Any, Dict, Optional

__all__ = [
    "PokeArenaError",
    "BinaryNotFoundError",
    "EngineError",
    "IllegalActionError",
    "ProtocolError",
    "EngineTimeoutError",
    "EngineClosedError",
]


class PokeArenaError(Exception):
    """Base class for everything this package raises."""


class BinaryNotFoundError(PokeArenaError):
    """The ``pokearena-env`` engine binary could not be located."""


class ProtocolError(PokeArenaError):
    """The engine said something that is not a valid protocol message.

    This is a bug on one side of the wire — a version mismatch, a corrupted
    pipe, or a stray write to stdout — never something a caller can fix by
    retrying.
    """


class EngineTimeoutError(PokeArenaError):
    """The engine did not answer within the configured timeout."""


class EngineClosedError(PokeArenaError):
    """The engine subprocess is gone (closed, crashed, or never started)."""


class EngineError(PokeArenaError):
    """The engine rejected a request and said why.

    Attributes:
        code: the stable machine-readable code (``bad_request``,
            ``illegal_action``, ``no_episode``, ``episode_over``,
            ``unknown_command``, ``internal``).
        details: structured context, when the engine attached any. An
            ``illegal_action`` carries ``legal_actions`` and ``action_mask``.
    """

    def __init__(self, code: str, message: str, details: Optional[Dict[str, Any]] = None):
        super().__init__(f"{code}: {message}")
        self.code = code
        self.message = message
        self.details: Dict[str, Any] = details or {}


class IllegalActionError(EngineError):
    """The submitted action was not in the legal set.

    The episode is untouched — the engine validates before it resolves — so a
    caller may simply pick a legal action and step again. ``details`` carries
    ``legal_actions`` and ``action_mask`` for that side.
    """

    @property
    def legal_actions(self):
        """The legal action records the engine offered instead."""
        return self.details.get("legal_actions", [])

    @property
    def action_mask(self):
        """A 0/1 mask over the discrete action space."""
        return self.details.get("action_mask", [])
