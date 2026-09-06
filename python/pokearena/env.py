"""Single-agent Gymnasium-style environment.

The agent plays one side of a PokéArena battle; a built-in baseline plays the
other, in-process inside the Go binary. That placement matters: the opponent is
part of the environment, seeded from the same battle seed, so the whole episode
stays a pure function of ``(seed, teams, opponent)`` and two runs of the same
seed produce the same game down to the byte.
"""

from __future__ import annotations

import random
from typing import Any, Dict, List, Optional, Tuple

from ._client import DEFAULT_TIMEOUT, EngineClient
from ._util import UINT64_MASK, extract_action, info_from
from .errors import EngineError
from .spaces import ACTION_SPACE_SIZE, action_space, describe_action, observation_space

__all__ = ["PokeArenaEnv", "GYMNASIUM_ENV_BASE"]

try:  # pragma: no cover - depends on the installed extras
    import gymnasium as _gym  # type: ignore

    _EnvBase = _gym.Env
    GYMNASIUM_ENV_BASE = True
except Exception:  # pragma: no cover - the dependency-free path
    _EnvBase = object  # type: ignore[assignment,misc]
    GYMNASIUM_ENV_BASE = False


class PokeArenaEnv(_EnvBase):  # type: ignore[misc,valid-type]
    """A PokéArena battle as a single-agent environment.

    Follows the current Gymnasium convention: ``reset`` returns ``(obs, info)``
    and ``step`` returns the 5-tuple ``(obs, reward, terminated, truncated,
    info)``. It subclasses ``gymnasium.Env`` when Gymnasium is installed and
    stands alone when it is not — the API is identical either way.

    Example::

        from pokearena import PokeArenaEnv

        with PokeArenaEnv(team="Genesis", opponent="heuristic") as env:
            obs, info = env.reset(seed=0)
            terminated = truncated = False
            while not (terminated or truncated):
                action = info["legal_actions"][0]["index"]
                obs, reward, terminated, truncated, info = env.step(action)
            print("winner:", info["winner"])

    Args:
        team: the agent's team — a library name (``"Genesis"``), a list of
            Pokédex numbers (``[150, 149, 143]``), or ``{"picks": [...]}`` for
            full control over moves, EVs, natures and items.
        opponent: the built-in baseline on the other side: ``"heuristic"``
            (strongest, and the benchmark's reference opponent), ``"random"``,
            ``"expectimax"``, or ``"expectimax@N"`` for a pinned search depth.
        opponent_team: the opponent's team. Defaults to ``team``, which is the
            variance-controlled **mirror match**: identical rosters on both
            sides, so the only free variable left is the policy.
        agent_side: which board side the agent occupies, 0 or 1.
        reward: ``"win_loss"`` (default: 0 every step, ±1 at the end) or
            ``"hp_delta"`` (dense shaping on team-HP difference; see the note
            in :ref:`rewards <rewards>` below).
        max_turns: truncate the episode after this many turns. 0 leaves only
            the engine's own 300-turn cap, which *terminates* with a winner
            decided on remaining HP rather than truncating.
        binary: explicit path to ``pokearena-env``. Normally unnecessary.
        data_dir: read the dataset from this directory instead of the copy
            embedded in the binary.
        teams_file: use this team library instead of the embedded one.
        expectimax_depth: default search depth for expectimax opponents.
        timeout: per-request read timeout in seconds.
        seed: seeds the sequence of battle seeds used by ``reset()`` calls that
            do not pass one of their own.

    .. _rewards:

    **Rewards.** ``win_loss`` is the honest default: the battle's only real
    objective, ±1 at the terminal step. ``hp_delta`` adds per-step shaping from
    the change in (own team HP fraction − opponent team HP fraction). That
    reads privileged state — the opponent's exact team HP is deliberately *not*
    in any observation — which is normal for a training signal and would be
    dishonest in an observation. It is opt-in for exactly that reason.
    """

    metadata = {"render_modes": ["ansi"], "name": "pokearena-v0"}

    def __init__(
        self,
        team: Any = "Genesis",
        opponent: str = "heuristic",
        *,
        opponent_team: Any = None,
        agent_side: int = 0,
        reward: str = "win_loss",
        max_turns: int = 0,
        binary: Optional[str] = None,
        data_dir: Optional[str] = None,
        teams_file: Optional[str] = None,
        expectimax_depth: Optional[int] = None,
        timeout: float = DEFAULT_TIMEOUT,
        render_mode: Optional[str] = None,
        seed: Optional[int] = None,
    ):
        if agent_side not in (0, 1):
            raise ValueError(f"agent_side must be 0 or 1, got {agent_side!r}")
        if render_mode not in (None, "ansi"):
            raise ValueError(f"render_mode must be None or 'ansi', got {render_mode!r}")

        self.team = team
        self.opponent = opponent
        self.opponent_team = opponent_team
        self.agent_side = agent_side
        self.reward_mode = reward
        self.max_turns = int(max_turns)
        self.render_mode = render_mode

        self.action_space = action_space()
        self.observation_space = observation_space()

        self._engine = EngineClient(
            binary,
            data_dir=data_dir,
            teams=teams_file,
            expectimax_depth=expectimax_depth,
            timeout=timeout,
        )
        #: The engine's handshake: protocol version, engine revision, dataset
        #: sim-version and curation SHA, ruleset, team library. This is the
        #: provenance record — quote it when you publish a number.
        self.engine_info: Dict[str, Any] = self._engine.handshake

        self._seed_rng = random.Random(seed)
        self._last: Dict[str, Any] = {}
        self._events: List[Dict[str, Any]] = []
        self._started = False

    # -- introspection ----------------------------------------------------

    @property
    def unwrapped(self) -> "PokeArenaEnv":
        return self

    @property
    def team_library(self) -> List[str]:
        """Names of the curated teams this binary can play."""
        return list((self.engine_info.get("team_library") or {}).get("teams") or [])

    def legal_actions(self) -> List[Dict[str, Any]]:
        """The agent's legal actions right now, each with ``index``, ``action``
        and a human-readable ``label``."""
        self._require_episode()
        out = self._engine.request("legal_actions", {"side": self.agent_side})
        return out.get("legal_actions") or []

    def action_mask(self):
        """A 0/1 mask over the discrete action space (NumPy array if NumPy is
        installed, otherwise a list)."""
        from ._util import as_mask

        self._require_episode()
        out = self._engine.request("legal_actions", {"side": self.agent_side})
        return as_mask(out.get("action_mask"))

    def observe(self) -> Dict[str, Any]:
        """Re-fetch the agent's current fog-of-war observation."""
        self._require_episode()
        out = self._engine.request("observe", {"side": self.agent_side})
        return out.get("observation") or {}

    # -- gym API ----------------------------------------------------------

    def reset(
        self,
        *,
        seed: Optional[int] = None,
        options: Optional[Dict[str, Any]] = None,
    ) -> Tuple[Dict[str, Any], Dict[str, Any]]:
        """Start a new battle.

        ``reset(seed=k)`` always plays battle ``k`` — the same rosters, the same
        RNG stream, the same game. ``reset()`` without a seed draws the next one
        from the generator seeded at construction (or at the last seeded reset),
        so an unseeded sequence is still reproducible from its starting point.
        """
        if GYMNASIUM_ENV_BASE:  # keep gymnasium's own np_random in step
            try:
                super().reset(seed=seed)  # type: ignore[misc]
            except TypeError:  # pragma: no cover - older signatures
                pass

        if seed is not None:
            self._seed_rng = random.Random(seed)
            battle_seed = int(seed) & UINT64_MASK
        else:
            battle_seed = self._seed_rng.getrandbits(64)

        options = dict(options or {})
        team = options.pop("team", self.team)
        opponent = options.pop("opponent", self.opponent)
        opponent_team = options.pop("opponent_team", self.opponent_team)
        reward = options.pop("reward", self.reward_mode)
        max_turns = int(options.pop("max_turns", self.max_turns))
        if options:
            raise ValueError(f"unknown reset options: {sorted(options)}")

        # The protocol indexes teams by board side; the agent may sit on
        # either. Default the opponent to the agent's own roster, which is the
        # mirror match the benchmark runs.
        sides: List[Any] = [None, None]
        sides[self.agent_side] = team
        sides[1 - self.agent_side] = team if opponent_team is None else opponent_team

        agents = ["external", "external"]
        agents[1 - self.agent_side] = opponent

        args: Dict[str, Any] = {
            "seed": battle_seed,
            "team": sides[0],
            "opponent_team": sides[1],
            "agents": agents,
            "reward": reward,
        }
        if max_turns > 0:
            args["max_turns"] = max_turns

        result = self._engine.request("reset", args)
        self._started = True
        obs, _reward, _terminated, _truncated = self._unpack(result)
        return obs, self._info(result)

    def step(self, action: Any) -> Tuple[Dict[str, Any], float, bool, bool, Dict[str, Any]]:
        """Submit the agent's action and advance to its next decision point.

        The opponent's move, and any forced replacement it has to make on its
        own, are resolved inside this call — the agent is only ever asked when
        it actually has a choice.

        Raises:
            IllegalActionError: the action was not legal. The episode is
                untouched, so picking a legal action and stepping again is a
                complete recovery.
        """
        self._require_episode()
        payload = extract_action(action)
        if isinstance(payload, int) and not 0 <= payload < ACTION_SPACE_SIZE:
            raise ValueError(
                f"action {payload} is outside the discrete space [0,{ACTION_SPACE_SIZE}); "
                f"see pokearena.spaces for the layout"
            )
        result = self._engine.request("step", {"action": payload})
        obs, reward, terminated, truncated = self._unpack(result)
        return obs, reward, terminated, truncated, self._info(result)

    def render(self) -> Optional[str]:
        """Return the most recent turn's event log as text (``ansi`` mode)."""
        if self.render_mode != "ansi":
            return None
        return self.text_log()

    def text_log(self) -> str:
        """The most recent step's battle log, one event per line."""
        return "\n".join(e.get("text", "") for e in self._events)

    def close(self) -> None:
        """Shut the engine subprocess down. Idempotent."""
        self._started = False
        self._engine.close()

    def __enter__(self) -> "PokeArenaEnv":
        return self

    def __exit__(self, *exc_info) -> None:
        self.close()

    # -- internals --------------------------------------------------------

    def _require_episode(self) -> None:
        if not self._started:
            raise EngineError("no_episode", "call reset() before stepping this environment")

    def _unpack(self, result: Dict[str, Any]) -> Tuple[Dict[str, Any], float, bool, bool]:
        self._last = result
        self._events = result.get("events") or []
        observations = result.get("observations") or [None, None]
        obs = observations[self.agent_side] or {}
        rewards = result.get("rewards") or [0.0, 0.0]
        reward = float(rewards[self.agent_side])
        terminated = bool(result.get("terminated"))
        truncated = bool(result.get("truncated"))
        if terminated or truncated:
            self._started = False
        return obs, reward, terminated, truncated

    def _info(self, result: Dict[str, Any]) -> Dict[str, Any]:
        info = info_from(result, self.agent_side)
        info["opponent"] = self.opponent
        info["action_labels"] = {
            la["index"]: la.get("label", describe_action(la["index"]))
            for la in info["legal_actions"]
            if isinstance(la, dict) and isinstance(la.get("index"), int)
        }
        return info
