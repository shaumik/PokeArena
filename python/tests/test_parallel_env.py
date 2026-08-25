"""Tests for the two-agent PettingZoo-style environment."""

from __future__ import annotations

import unittest

from _support import OTHER_TEAM, SMALL_TEAM, requires_engine

from pokearena import AGENTS, PokeArenaParallelEnv


def _first_legal(infos, agent):
    legal = infos[agent]["legal_actions"]
    assert legal, f"{agent} has no legal actions while it is to move"
    return legal[0]["index"]


@requires_engine
class ParallelEnvTest(unittest.TestCase):
    def setUp(self):
        self.env = PokeArenaParallelEnv(team=SMALL_TEAM)
        self.addCleanup(self.env.close)

    def test_reset_gives_every_agent_an_observation(self):
        obs, infos = self.env.reset(seed=0)
        self.assertEqual(sorted(obs), sorted(AGENTS))
        self.assertEqual(sorted(infos), sorted(AGENTS))
        self.assertEqual(self.env.agents, list(AGENTS))
        self.assertEqual(self.env.agents_to_move, list(AGENTS))
        self.assertEqual(obs["player_0"]["me"], 0)
        self.assertEqual(obs["player_1"]["me"], 1)

    def test_full_episode(self):
        obs, infos = self.env.reset(seed=11)
        steps = 0
        while self.env.agents:
            self.assertLess(steps, 1000, "episode did not finish")
            actions = {a: _first_legal(infos, a) for a in self.env.agents_to_move}
            obs, rewards, terms, truncs, infos = self.env.step(actions)
            steps += 1
        self.assertGreater(steps, 0)
        self.assertTrue(all(terms.values()) or all(truncs.values()))
        self.assertEqual(sum(rewards.values()), 0.0, "rewards are not zero-sum")
        self.assertEqual(self.env.agents, [], "agents must empty out when the episode ends")

    def test_replace_phase_asks_only_one_side(self):
        """After a single faint only one side chooses a replacement.

        Both agents still get an observation and stay in ``env.agents``; the
        engine's ``to_move`` is what says whose action is consumed.
        """
        obs, infos = self.env.reset(seed=11)
        saw_single = False
        while self.env.agents:
            if len(self.env.agents_to_move) == 1:
                saw_single = True
                self.assertEqual(sorted(obs), sorted(AGENTS))
                self.assertEqual(self.env.agents, list(AGENTS))
            actions = {a: _first_legal(infos, a) for a in self.env.agents_to_move}
            obs, _r, _t, _tr, infos = self.env.step(actions)
        self.assertTrue(saw_single, "no forced-replacement step occurred in this battle")

    def test_every_action_shape_a_caller_might_hold(self):
        """The same unwrapping seam exists on the two-agent path."""
        for pick in (
            lambda la: la[0],
            lambda la: la[0]["action"],
            lambda la: la[0]["index"],
            lambda _la: 0,
        ):
            with self.subTest(shape=pick):
                env = PokeArenaParallelEnv(team=SMALL_TEAM)
                self.addCleanup(env.close)
                _obs, infos = env.reset(seed=13)
                actions = {a: pick(infos[a]["legal_actions"]) for a in env.agents_to_move}
                _obs, _r, terms, truncs, infos = env.step(actions)
                self.assertFalse(any(terms.values()) or any(truncs.values()))
                self.assertEqual(infos["player_0"]["turn"], 1)

    def test_missing_action_for_a_moving_agent_raises(self):
        _obs, _infos = self.env.reset(seed=1)
        with self.assertRaises(KeyError):
            self.env.step({"player_0": 0})

    def test_extra_actions_are_ignored(self):
        """The usual ``{a: policy(a) for a in env.agents}`` loop must be safe."""
        _obs, infos = self.env.reset(seed=11)
        while self.env.agents:
            actions = {a: _first_legal(infos, a) for a in self.env.agents_to_move}
            for a in self.env.agents:
                actions.setdefault(a, 0)  # a stale action for a non-moving side
            _obs, _r, _t, _tr, infos = self.env.step(actions)

    def test_spaces(self):
        for agent in AGENTS:
            self.assertEqual(self.env.action_space(agent).n, 11)
            self.assertTrue(self.env.observation_space(agent).contains({}))
        with self.assertRaises(KeyError):
            self.env.action_space("player_2")

    def test_fog_of_war_per_agent(self):
        env = PokeArenaParallelEnv(team=SMALL_TEAM, opponent_team=OTHER_TEAM)
        self.addCleanup(env.close)
        obs, infos = env.reset(seed=9)
        rosters = {a: {p["name"] for p in obs[a]["self"]["team"]} for a in AGENTS}
        checked = 0
        while env.agents:
            for agent in AGENTS:
                foe = obs[agent]["foe"]
                for key in ("hp", "max_hp", "stats", "evs", "ivs", "nature", "ability", "item"):
                    self.assertNotIn(key, foe, f"{agent} sees foe.{key}")
                other = AGENTS[1 - AGENTS.index(agent)]
                hidden = rosters[other] - {foe["name"]} - rosters[agent]
                text = repr(obs[agent])
                for name in hidden:
                    self.assertNotIn(name, text, f"{agent} sees benched opponent {name}")
                checked += 1
            actions = {a: _first_legal(infos, a) for a in env.agents_to_move}
            obs, _r, _t, _tr, infos = env.step(actions)
        self.assertGreater(checked, 10)

    def test_context_manager(self):
        with PokeArenaParallelEnv(team=SMALL_TEAM) as env:
            env.reset(seed=0)
        self.assertTrue(env._engine.closed)  # noqa: SLF001


if __name__ == "__main__":
    unittest.main()
