"""PokéArena — a deterministic Pokémon battle environment for Python.

The engine is a pure function ``(state, action_p1, action_p2) -> (state,
events)`` written in Go. This package drives it as a subprocess over a
line-oriented JSON protocol on stdin/stdout: no server, no ports, no database,
no data files to download.

What that buys you:

* **Determinism.** ``reset(seed=k)`` plays battle ``k``, byte for byte, every
  time — same events, same damage rolls, same outcome. The seeding path is the
  one the project's own benchmark uses, so a number measured here is comparable
  to a number on its published board.
* **Fog of war.** Each side sees its own team in full and only the opponent's
  *active* Pokémon — with its exact HP, ability, item, stats, EVs/IVs, nature
  and move PP redacted. The hidden information is not in the bytes at all, so
  an agent cannot accidentally read it.
* **Variance control.** The default is a mirror match: identical rosters on both
  sides, so the only free variable in a result is the policy.

Quick start::

    from pokearena import PokeArenaEnv

    with PokeArenaEnv(team="Genesis", opponent="heuristic") as env:
        obs, info = env.reset(seed=0)
        done = False
        while not done:
            action = info["legal_actions"][0]["index"]   # your policy here
            obs, reward, terminated, truncated, info = env.step(action)
            done = terminated or truncated
        print("winner:", info["winner"], "after", info["turn"], "turns")

The engine binary is a prerequisite; see :func:`pokearena.find_binary` for how
it is located and what the error tells you if it is missing.
"""

from ._binary import BINARY_NAME, ENV_VAR, GO_INSTALL_COMMAND, binary_version, find_binary
from ._client import DEFAULT_TIMEOUT, EngineClient
from .env import GYMNASIUM_ENV_BASE, PokeArenaEnv
from .errors import (
    BinaryNotFoundError,
    EngineClosedError,
    EngineError,
    EngineTimeoutError,
    IllegalActionError,
    PokeArenaError,
    ProtocolError,
)
from .parallel_env import (
    AGENTS,
    PETTINGZOO_ENV_BASE,
    PokeArenaParallelEnv,
    aec_env,
    parallel_env,
)
from .spaces import (
    ACTION_SPACE_SIZE,
    GYMNASIUM_AVAILABLE,
    MOVE_SLOTS,
    STRUGGLE_INDEX,
    SWITCH_BASE,
    TEAM_SIZE,
    describe_action,
)

__version__ = "0.1.0"

#: The stdio protocol version this client is written against. The engine
#: reports its own in the handshake; a differing major number means the two are
#: not compatible.
PROTOCOL_VERSION = "1.0"

__all__ = [
    "__version__",
    "PROTOCOL_VERSION",
    # environments
    "PokeArenaEnv",
    "PokeArenaParallelEnv",
    "parallel_env",
    "aec_env",
    "AGENTS",
    # low level
    "EngineClient",
    "DEFAULT_TIMEOUT",
    # binary discovery
    "find_binary",
    "binary_version",
    "BINARY_NAME",
    "ENV_VAR",
    "GO_INSTALL_COMMAND",
    # action space
    "ACTION_SPACE_SIZE",
    "MOVE_SLOTS",
    "STRUGGLE_INDEX",
    "SWITCH_BASE",
    "TEAM_SIZE",
    "describe_action",
    # capability flags
    "GYMNASIUM_AVAILABLE",
    "GYMNASIUM_ENV_BASE",
    "PETTINGZOO_ENV_BASE",
    # errors
    "PokeArenaError",
    "BinaryNotFoundError",
    "EngineError",
    "IllegalActionError",
    "ProtocolError",
    "EngineTimeoutError",
    "EngineClosedError",
]


def register_gymnasium_envs() -> bool:
    """Register ``PokeArena-v0`` with Gymnasium's registry, if it is installed.

    Returns ``True`` when registration happened, ``False`` when Gymnasium is not
    available. Calling it twice is harmless.

    This is opt-in rather than an import side effect: a library that mutates a
    global registry the moment it is imported is a library that is hard to
    reason about, and ``PokeArenaEnv(...)`` works perfectly well without it.
    """
    try:
        from gymnasium.envs.registration import register, registry  # type: ignore
    except Exception:
        return False
    if "PokeArena-v0" not in registry:
        register(id="PokeArena-v0", entry_point="pokearena.env:PokeArenaEnv")
    return True
