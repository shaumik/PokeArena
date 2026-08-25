"""Two-agent PettingZoo-style environment.

A PokéArena turn is **simultaneous** — both sides commit before either sees the
other's choice — so the parallel API is the faithful model, not a convenience.
An AEC view is available via :func:`aec_env` for tooling that wants one; it is
PettingZoo's own ``parallel_to_aec`` conversion, which imposes a turn order the
underlying game does not have.

Fog of war is per agent and enforced by the engine, not by this wrapper: each
agent's observation is projected separately and the other side's bench is never
in the bytes. Both observations pass through this process, so a single script
holding both can of course see both — that is the script's own information, not
a leak between the agents.
"""

from __future__ import annotations

import functools
import random
from typing import Any, Dict, List, Optional, Tuple

from ._client import DEFAULT_TIMEOUT, EngineClient
from ._util import UINT64_MASK, extract_action, info_from
from .errors import EngineError
from .spaces import ACTION_SPACE_SIZE, action_space, observation_space

__all__ = ["PokeArenaParallelEnv", "parallel_env", "aec_env", "AGENTS", "PETTINGZOO_ENV_BASE"]

#: The two agent ids, in board-side order.
AGENTS: Tuple[str, str] = ("player_0", "player_1")

try:  # pragma: no cover - depends on the installed extras
    from pettingzoo import ParallelEnv as _ParallelBase  # type: ignore

    PETTINGZOO_ENV_BASE = True
except Exception:  # pragma: no cover - the dependency-free path
    _ParallelBase = object  # type: ignore[assignment,misc]
    PETTINGZOO_ENV_BASE = False


def _side_of(agent: str) -> int:
    try:
        return AGENTS.index(agent)
    except ValueError:
        raise KeyError(f"unknown agent {agent!r}; expected one of {list(AGENTS)}") from None


class PokeArenaParallelEnv(_ParallelBase):  # type: ignore[misc,valid-type]
    """Agent versus agent, one action per side per step.

    Example::

        from pokearena import PokeArenaParallelEnv

        with PokeArenaParallelEnv(team="Blitz") as env:
            obs, infos = env.reset(seed=0)
            while env.agents:
                actions = {
                    a: infos[a]["legal_actions"][0]["index"]
                    for a in env.agents_to_move
                }
                obs, rewards, terms, truncs, infos = env.step(actions)

    **Who has to act.** Most steps are a simultaneous turn and both agents act.
    After a faint, though, only the side with the fainted Pokémon chooses a
    replacement. ``env.agents_to_move`` and ``infos[agent]["to_move"]`` name the
    agents whose action is actually consumed this step; an action supplied for
    any other agent is ignored rather than rejected, so the usual
    ``{a: policy(a) for a in env.agents}`` loop is safe. Both agents stay in
    ``env.agents`` until the episode ends, per the PettingZoo contract.

    Args:
        team: the team for side 0.
        opponent_team: the team for side 1. Defaults to ``team`` — the
            variance-controlled mirror match.
        max_turns: truncate after this many turns (0 = engine cap only).
        binary, data_dir, teams_file, expectimax_depth, timeout: as
            :class:`~pokearena.env.PokeArenaEnv`.
        seed: seeds the sequence of battle seeds for unseeded resets.
    """

    metadata = {"render_modes": ["ansi"], "name": "pokearena_parallel_v0", "is_parallelizable": True}

    def __init__(
        self,
        team: Any = "Genesis",
        *,
        opponent_team: Any = None,
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
        if render_mode not in (None, "ansi"):
            raise ValueError(f"render_mode must be None or 'ansi', got {render_mode!r}")

        self.team = team
        self.opponent_team = opponent_team
        self.reward_mode = reward
        self.max_turns = int(max_turns)
        self.render_mode = render_mode

        self.possible_agents: List[str] = list(AGENTS)
        self.agents: List[str] = []
        self.agents_to_move: List[str] = []

        self._engine = EngineClient(
            binary,
            data_dir=data_dir,
            teams=teams_file,
            expectimax_depth=expectimax_depth,
            timeout=timeout,
        )
        #: Provenance: protocol version, engine revision, dataset identity.
        self.engine_info: Dict[str, Any] = self._engine.handshake

        self._seed_rng = random.Random(seed)
        self._events: List[Dict[str, Any]] = []
        self._obs_cache: Dict[str, Dict[str, Any]] = {}

    # -- spaces -----------------------------------------------------------

    @functools.lru_cache(maxsize=None)  # noqa: B019 - PettingZoo's own idiom
    def observation_space(self, agent: str):
        _side_of(agent)
        return observation_space()

    @functools.lru_cache(maxsize=None)  # noqa: B019
    def action_space(self, agent: str):
        _side_of(agent)
        return action_space()

    # -- pettingzoo API ---------------------------------------------------

    def reset(
        self,
        seed: Optional[int] = None,
        options: Optional[Dict[str, Any]] = None,
    ) -> Tuple[Dict[str, Dict[str, Any]], Dict[str, Dict[str, Any]]]:
        """Start a new battle and return ``(observations, infos)``."""
        if seed is not None:
            self._seed_rng = random.Random(seed)
            battle_seed = int(seed) & UINT64_MASK
        else:
            battle_seed = self._seed_rng.getrandbits(64)

        options = dict(options or {})
        team = options.pop("team", self.team)
        opponent_team = options.pop("opponent_team", self.opponent_team)
        reward = options.pop("reward", self.reward_mode)
        max_turns = int(options.pop("max_turns", self.max_turns))
        if options:
            raise ValueError(f"unknown reset options: {sorted(options)}")

        args: Dict[str, Any] = {
            "seed": battle_seed,
            "team": team,
            "opponent_team": team if opponent_team is None else opponent_team,
            "agents": ["external", "external"],
            "reward": reward,
        }
        if max_turns > 0:
            args["max_turns"] = max_turns

        result = self._engine.request("reset", args)
        self.agents = list(self.possible_agents)
        obs, _rewards, _terms, _truncs, infos = self._unpack(result)
        return obs, infos

    def step(self, actions: Dict[str, Any]):
        """Submit both sides' actions and resolve one decision point.

        Returns the PettingZoo 5-tuple
        ``(observations, rewards, terminations, truncations, infos)``.
        """
        if not self.agents:
            raise EngineError("no_episode", "call reset() before stepping this environment")

        payload: List[Any] = [None, None]
        for agent in self.agents_to_move:
            if agent not in actions:
                raise KeyError(
                    f"{agent!r} must act this step (to_move={self.agents_to_move}) "
                    f"but no action was supplied"
                )
            value = extract_action(actions[agent])
            if isinstance(value, int) and not 0 <= value < ACTION_SPACE_SIZE:
                raise ValueError(
                    f"action {value} for {agent!r} is outside the discrete space [0,{ACTION_SPACE_SIZE})"
                )
            payload[_side_of(agent)] = value

        result = self._engine.request("step", {"actions": payload})
        return self._unpack(result)

    def render(self) -> Optional[str]:
        if self.render_mode != "ansi":
            return None
        return self.text_log()

    def text_log(self) -> str:
        """The most recent step's battle log, one event per line."""
        return "\n".join(e.get("text", "") for e in self._events)

    def close(self) -> None:
        self.agents = []
        self.agents_to_move = []
        self._engine.close()

    def __enter__(self) -> "PokeArenaParallelEnv":
        return self

    def __exit__(self, *exc_info) -> None:
        self.close()

    # -- internals --------------------------------------------------------

    def _unpack(self, result: Dict[str, Any]):
        self._events = result.get("events") or []
        to_move = result.get("to_move") or []
        self.agents_to_move = [AGENTS[s] for s in to_move if 0 <= s < len(AGENTS)]

        terminated = bool(result.get("terminated"))
        truncated = bool(result.get("truncated"))
        rewards_arr = result.get("rewards") or [0.0, 0.0]
        raw_obs = result.get("observations") or [None, None]

        observations: Dict[str, Dict[str, Any]] = {}
        infos: Dict[str, Dict[str, Any]] = {}
        for agent in self.possible_agents:
            side = _side_of(agent)
            obs = raw_obs[side]
            if obs is None and not (terminated or truncated):
                # Only one side replaces after a single faint, so the other has
                # no observation attached to this step. Fetch its current view
                # so the returned dict is complete — every agent always gets an
                # observation, which is what the PettingZoo API promises.
                obs = self._engine.request("observe", {"side": side}).get("observation")
            observations[agent] = obs if obs is not None else self._obs_cache.get(agent, {})
            self._obs_cache[agent] = observations[agent]
            infos[agent] = info_from(result, side)
            infos[agent]["to_move"] = list(self.agents_to_move)

        rewards = {a: float(rewards_arr[_side_of(a)]) for a in self.possible_agents}
        terminations = {a: terminated for a in self.possible_agents}
        truncations = {a: truncated for a in self.possible_agents}

        if terminated or truncated:
            # PettingZoo's contract: an agent that is done leaves `agents`, and
            # `while env.agents:` is the standard loop condition.
            self.agents = []
            self.agents_to_move = []

        return observations, rewards, terminations, truncations, infos


def parallel_env(**kwargs) -> PokeArenaParallelEnv:
    """PettingZoo's conventional constructor name."""
    return PokeArenaParallelEnv(**kwargs)


def aec_env(**kwargs):
    """An AEC view of the parallel environment.

    Requires PettingZoo (``pip install "pokearena[pettingzoo]"``), because the
    conversion is theirs. Note what the conversion costs: PokéArena turns are
    simultaneous, and AEC imposes a sequential order on them, so the AEC view is
    a re-presentation of the parallel one rather than a truer picture of the
    game. Prefer :class:`PokeArenaParallelEnv` unless a tool requires AEC.
    """
    try:
        from pettingzoo.utils.conversions import parallel_to_aec  # type: ignore
    except Exception as exc:  # pragma: no cover - depends on extras
        raise ImportError(
            "aec_env() needs PettingZoo: pip install 'pokearena[pettingzoo]'"
        ) from exc
    return parallel_to_aec(PokeArenaParallelEnv(**kwargs))
