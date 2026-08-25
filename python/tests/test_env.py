"""Tests for the single-agent Gymnasium-style environment."""

from __future__ import annotations

import unittest

from _support import OTHER_TEAM, SMALL_TEAM, requires_engine, rollout

from pokearena import ACTION_SPACE_SIZE, IllegalActionError, PokeArenaEnv


@requires_engine
class ResetStepTest(unittest.TestCase):
    def setUp(self):
        self.env = PokeArenaEnv(team=SMALL_TEAM, opponent="heuristic")
        self.addCleanup(self.env.close)

    def test_reset_returns_obs_and_info(self):
        obs, info = self.env.reset(seed=0)
        self.assertIsInstance(obs, dict)
        self.assertIn("self", obs)
        self.assertIn("foe", obs)
        self.assertEqual(info["turn"], 0)
        self.assertTrue(info["legal_actions"])
        self.assertEqual(len(info["action_mask"]), ACTION_SPACE_SIZE)
        self.assertEqual(obs["me"], 0)

    def test_step_returns_the_five_tuple(self):
        _obs, info = self.env.reset(seed=0)
        out = self.env.step(info["legal_actions"][0]["index"])
        self.assertEqual(len(out), 5, "Gymnasium's step returns 5 values")
        obs, reward, terminated, truncated, info = out
        self.assertIsInstance(obs, dict)
        self.assertIsInstance(reward, float)
        self.assertIsInstance(terminated, bool)
        self.assertIsInstance(truncated, bool)
        self.assertIsInstance(info, dict)
        self.assertEqual(info["turn"], 1)
        self.assertTrue(info["events"], "a resolved turn produced no events")

    def test_full_episode_terminates_with_a_winner(self):
        steps, info = rollout(self.env, seed=2024)
        self.assertGreater(steps, 0)
        self.assertIn(info["winner"], (0, 1, 2))
        self.assertEqual(info["to_move"], [])

    def test_terminal_reward_agrees_with_the_winner(self):
        _obs, info = self.env.reset(seed=7)
        reward = 0.0
        terminated = truncated = False
        while not (terminated or truncated):
            _obs, reward, terminated, truncated, info = self.env.step(
                info["legal_actions"][0]["index"]
            )
        self.assertTrue(terminated, "expected a decided battle, not a truncation")
        self.assertEqual(reward, {0: 1.0, 1: -1.0, 2: 0.0}[info["winner"]])

    def test_illegal_action_raises_and_leaves_the_episode_alive(self):
        _obs, info = self.env.reset(seed=1)
        mask = list(info["action_mask"])
        illegal = next(i for i, m in enumerate(mask) if not m)
        with self.assertRaises(IllegalActionError):
            self.env.step(illegal)
        # Still on turn 0 and still playable.
        obs, _r, terminated, truncated, info = self.env.step(info["legal_actions"][0]["index"])
        self.assertFalse(terminated or truncated)
        self.assertEqual(info["turn"], 1)

    def test_out_of_range_action_raises_value_error(self):
        _obs, _info = self.env.reset(seed=1)
        with self.assertRaises(ValueError):
            self.env.step(ACTION_SPACE_SIZE + 3)

    def test_every_action_shape_a_caller_might_hold(self):
        """``env.step(env.legal_actions()[0])`` is the first line people write.

        All four shapes must work: the whole legal-action record, its nested
        engine object, its flat index, and a bare int. Passing the record used
        to reach the engine intact and come back as a confusing complaint about
        a missing JSON field the caller never typed.
        """
        for pick in (
            lambda la: la[0],                       # the record itself
            lambda la: la[0]["action"],             # the engine object
            lambda la: la[0]["index"],              # the flat index
            lambda _la: 0,                          # a bare int
        ):
            with self.subTest(shape=pick.__doc__ or pick):
                env = PokeArenaEnv(team=SMALL_TEAM, opponent="heuristic")
                self.addCleanup(env.close)
                _obs, info = env.reset(seed=13)
                legal = env.legal_actions()
                self.assertTrue(legal)
                _obs, _r, terminated, truncated, info = env.step(pick(legal))
                self.assertFalse(terminated or truncated)
                self.assertEqual(info["turn"], 1)

    def test_unusable_action_shape_raises_a_python_side_type_error(self):
        _obs, _info = self.env.reset(seed=1)
        with self.assertRaises(TypeError) as ctx:
            self.env.step({"label": "use Psystrike"})
        self.assertIn("legal_actions()", str(ctx.exception))

    def test_step_before_reset_raises(self):
        env = PokeArenaEnv(team=SMALL_TEAM)
        self.addCleanup(env.close)
        with self.assertRaises(Exception):
            env.step(0)


@requires_engine
class FogOfWarTest(unittest.TestCase):
    """The observation must not carry the opponent's hidden information.

    The teams are asymmetric on purpose: in a mirror match every species on the
    board is on both teams, so a leaked bench Pokémon would be indistinguishable
    from one of your own and the test would pass for the wrong reason.
    """

    FORBIDDEN = ("hp", "max_hp", "stats", "evs", "ivs", "nature", "ability", "item")

    def test_no_hidden_foe_fields_and_no_foe_bench(self):
        env = PokeArenaEnv(team=SMALL_TEAM, opponent_team=OTHER_TEAM, opponent="heuristic")
        self.addCleanup(env.close)

        obs, info = env.reset(seed=9)
        checked = 0
        terminated = truncated = False
        while not (terminated or truncated):
            foe = obs["foe"]
            for key in self.FORBIDDEN:
                self.assertNotIn(key, foe, f"observation leaks foe.{key}")
            self.assertIn("hp_pct", foe, "the public HP percentage is missing")
            # Only the active opponent is present at all: there is no bench.
            self.assertNotIn("team", foe)
            self.assertIsInstance(obs["foe_bench_alive"], int)
            for slot in foe["moves"]:
                self.assertEqual(set(slot), {"move_id"}, "foe move slots carry more than an id")
            checked += 1
            obs, _r, terminated, truncated, info = env.step(info["legal_actions"][0]["index"])
        self.assertGreater(checked, 5, "the battle was too short to be evidence")

    def test_own_team_is_fully_visible(self):
        env = PokeArenaEnv(team=SMALL_TEAM, opponent="heuristic")
        self.addCleanup(env.close)
        obs, _info = env.reset(seed=4)
        own = obs["self"]["team"]
        self.assertEqual(len(own), len(SMALL_TEAM))
        for mon in own:
            for key in ("hp", "max_hp", "stats", "moves", "ability"):
                self.assertIn(key, mon, f"own Pokemon is missing {key}")


@requires_engine
class ConfigurationTest(unittest.TestCase):
    def test_agent_can_play_side_one(self):
        env = PokeArenaEnv(team=SMALL_TEAM, opponent="heuristic", agent_side=1)
        self.addCleanup(env.close)
        obs, info = env.reset(seed=3)
        self.assertEqual(obs["me"], 1)
        self.assertEqual(info["agents"], ["heuristic", "external"])

    def test_max_turns_truncates_rather_than_terminates(self):
        env = PokeArenaEnv(team=SMALL_TEAM, opponent="heuristic", max_turns=3)
        self.addCleanup(env.close)
        _obs, info = env.reset(seed=6)
        terminated = truncated = False
        while not (terminated or truncated):
            _obs, _r, terminated, truncated, info = env.step(info["legal_actions"][0]["index"])
        self.assertTrue(truncated)
        self.assertFalse(terminated)

    def test_library_team_by_name(self):
        env = PokeArenaEnv(team="Genesis", opponent="random")
        self.addCleanup(env.close)
        self.assertIn("Genesis", env.team_library)
        _obs, info = env.reset(seed=1)
        self.assertEqual(info["teams"], ["Genesis", "Genesis"])

    def test_hp_delta_reward_is_dense(self):
        env = PokeArenaEnv(team=SMALL_TEAM, opponent="heuristic", reward="hp_delta")
        self.addCleanup(env.close)
        _obs, info = env.reset(seed=31)
        saw_nonzero = False
        terminated = truncated = False
        while not (terminated or truncated):
            _obs, reward, terminated, truncated, info = env.step(info["legal_actions"][0]["index"])
            if reward != 0 and not (terminated or truncated):
                saw_nonzero = True
        self.assertTrue(saw_nonzero, "hp_delta produced no mid-episode reward")

    def test_helpers(self):
        env = PokeArenaEnv(team=SMALL_TEAM, opponent="heuristic", render_mode="ansi")
        self.addCleanup(env.close)
        env.reset(seed=1)
        self.assertTrue(env.legal_actions())
        self.assertEqual(len(env.action_mask()), ACTION_SPACE_SIZE)
        self.assertIn("self", env.observe())
        env.step(0)
        self.assertTrue(env.render(), "ansi render produced no text")

    def test_context_manager(self):
        with PokeArenaEnv(team=SMALL_TEAM) as env:
            env.reset(seed=0)
        self.assertTrue(env._engine.closed)  # noqa: SLF001


if __name__ == "__main__":
    unittest.main()
