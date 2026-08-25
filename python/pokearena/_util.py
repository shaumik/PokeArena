"""Small shared helpers for the environment wrappers."""

from __future__ import annotations

from typing import Any, Dict, List, Optional, Sequence

from .spaces import ACTION_SPACE_SIZE

try:  # pragma: no cover - depends on whether numpy happens to be installed
    import numpy as _np
except Exception:  # pragma: no cover
    _np = None

__all__ = ["as_mask", "extract_action", "info_from", "UINT64_MASK"]

UINT64_MASK = (1 << 64) - 1


def as_mask(mask: Optional[Sequence[int]]):
    """Return an action mask in the most useful type available.

    NumPy's ``int8`` array is what PettingZoo's action-mask convention and most
    RL libraries expect. NumPy is not a dependency of this package, so a plain
    list is returned when it is absent — both are truthy, both index the same
    way, and ``np.asarray`` on the list gives the array if a caller wants one.
    """
    values = list(mask) if mask is not None else [0] * ACTION_SPACE_SIZE
    if _np is not None:
        return _np.asarray(values, dtype=_np.int8)
    return values


def extract_action(action: Any) -> Any:
    """Normalise a submitted action into something the protocol accepts.

    Every shape a caller plausibly has in hand is accepted, because the first
    line a new user writes is ``env.step(env.legal_actions()[0])`` and it has to
    work:

    * a plain ``int`` — the flat discrete index;
    * a NumPy scalar or anything with ``.item()``, so ``np.argmax(...)`` needs
      no cast;
    * a **legal-action record** as returned by ``legal_actions()`` /
      ``info["legal_actions"]``, i.e. ``{"index": 0, "action": {...},
      "label": "..."}`` — the envelope is unwrapped for you;
    * the engine's own action object ``{"kind": "move", "index": 2}``, which is
      the only encoding that can name a self-switch pivot target.

    Anything else raises :class:`TypeError` here, on the Python side, naming the
    shapes that do work. Passing it through to the engine instead would surface
    as a message about a JSON field the caller never typed.
    """
    if isinstance(action, dict):
        return _unwrap_action_dict(action)
    if isinstance(action, bool):  # bool is an int subclass; almost certainly a bug
        raise TypeError("action must be an int or an action dict, not a bool")
    if isinstance(action, int):
        return action
    item = getattr(action, "item", None)
    if callable(item):
        try:
            return int(item())
        except (TypeError, ValueError):
            pass
    try:
        return int(action)
    except (TypeError, ValueError) as exc:
        raise _action_shape_error(type(action).__name__) from exc


def _action_shape_error(got: str) -> TypeError:
    """The one message that lists every shape that does work.

    Built with %-formatting rather than str.format because the message itself is
    full of literal braces — the example dicts are the whole point of it.
    """
    return TypeError(
        "action must be one of: an int in [0,%d); a legal-action record from "
        "legal_actions() such as {'index': 0, 'action': {...}, 'label': ...}; "
        "or an engine action object such as {'kind': 'move', 'index': 0}. Got %s."
        % (ACTION_SPACE_SIZE, got)
    )


def _unwrap_action_dict(d: Dict[str, Any]) -> Any:
    """Reduce a dict-shaped action to what the protocol accepts."""
    inner = d.get("action")
    if isinstance(inner, dict):
        # A legal-action record: {"index": ..., "action": {...}, "label": ...}.
        # Recurse into the engine object rather than the flat index so a
        # self-switch pivot target survives.
        return _unwrap_action_dict(inner)

    kind = d.get("kind")
    if kind is not None:
        if kind not in ("move", "switch"):
            raise TypeError(
                f"action kind must be 'move' or 'switch', got {kind!r}"
            )
        if not isinstance(d.get("index"), int):
            raise TypeError(
                f"an action object needs an integer 'index', got {d.get('index')!r}"
            )
        return d

    index = d.get("index")
    if isinstance(index, int) and not isinstance(index, bool):
        return index

    raise _action_shape_error(f"a dict with keys {sorted(d)}")


def info_from(result: Dict[str, Any], side: int) -> Dict[str, Any]:
    """Build the per-side ``info`` dict from an engine step result.

    Everything in here is either public battle information (the event log, the
    turn number, the outcome) or the side's own legal-action set. No part of it
    is derived from the opponent's hidden state.
    """
    engine_info = result.get("info") or {}
    legal: List[Dict[str, Any]] = (result.get("legal_actions") or [None, None])[side] or []
    mask = (result.get("action_mask") or [None, None])[side]
    state_hash = (engine_info.get("state_hash") or ["", ""])[side]

    return {
        "turn": result.get("turn"),
        "phase": result.get("phase"),
        "to_move": result.get("to_move") or [],
        "legal_actions": legal,
        "action_mask": as_mask(mask),
        "events": result.get("events") or [],
        "winner": result.get("winner"),
        "state_hash": state_hash,
        "decision_index": engine_info.get("decision_index"),
        "seed": engine_info.get("seed"),
        "battle_id": engine_info.get("battle_id"),
        "teams": engine_info.get("teams"),
        "agents": engine_info.get("agents"),
        "turn_limit": engine_info.get("turn_limit"),
        "side": side,
    }
