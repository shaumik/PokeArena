"""Determinism is the product; this is the test that says so.

Two independent engine processes, the same seed, the same policy — the
trajectories must match event for event and hash for hash. The state hashes are
the strongest part: each one fingerprints the exact observation bytes the agent
received at that decision point, so an equal sequence means the two runs saw
byte-identical states all the way down.
"""

from __future__ import annotations

import unittest

from _support import SMALL_TEAM, requires_engine

from pokearena import PokeArenaEnv, PokeArenaParallelEnv


def _trajectory(seed, team=SMALL_TEAM, opponent="heuristic"):
    """Play one episode in a fresh process and return a comparable record."""
    with PokeArenaEnv(team=team, opponent=opponent) as env:
        obs, info = env.reset(seed=seed)
        record = [(info["state_hash"], tuple(e["text"] for e in info["events"]))]
        rewards = []
        terminated = truncated = False
        while not (terminated or truncated):
            action = info["legal_actions"][0]["index"]
            obs, reward, terminated, truncated, info = env.step(action)
            record.append((info["state_hash"], tuple(e["text"] for e in info["events"])))
            rewards.append(reward)
        return {
            "record": record,
            "rewards": rewards,
            "winner": info["winner"],
            "turns": info["turn"],
        }


@requires_engine
class DeterminismTest(unittest.TestCase):
    def test_same_seed_same_trajectory(self):
        a = _trajectory(2026)
        b = _trajectory(2026)
        self.assertEqual(a["winner"], b["winner"])
        self.assertEqual(a["turns"], b["turns"])
        self.assertEqual(a["rewards"], b["rewards"])
        self.assertEqual(len(a["record"]), len(b["record"]))
        for i, (left, right) in enumerate(zip(a["record"], b["record"])):
            self.assertEqual(left, right, f"trajectories diverge at step {i}")
        self.assertGreater(len(a["record"]), 3, "the battle was too short to be evidence")

    def test_different_seeds_diverge(self):
        a = _trajectory(1)
        b = _trajectory(2)
        self.assertNotEqual(
            [h for h, _ in a["record"]],
            [h for h, _ in b["record"]],
            "two different seeds produced the same trajectory; the seed is not reaching the engine",
        )

    def test_reset_is_independent_of_history(self):
        """A reset after a partial episode must equal a reset in a fresh process."""
        with PokeArenaEnv(team=SMALL_TEAM, opponent="heuristic") as env:
            _obs, info = env.reset(seed=5)
            for _ in range(4):
                _obs, _r, terminated, truncated, info = env.step(info["legal_actions"][0]["index"])
                if terminated or truncated:
                    break
            _obs, restarted = env.reset(seed=77)

        with PokeArenaEnv(team=SMALL_TEAM, opponent="heuristic") as fresh_env:
            _obs, fresh = fresh_env.reset(seed=77)

        self.assertEqual(restarted["state_hash"], fresh["state_hash"])
        self.assertEqual(restarted["turn"], fresh["turn"])

    def test_parallel_env_is_deterministic_too(self):
        def run():
            with PokeArenaParallelEnv(team=SMALL_TEAM) as env:
                obs, infos = env.reset(seed=808)
                hashes = [tuple(infos[a]["state_hash"] for a in sorted(infos))]
                while env.agents:
                    actions = {
                        a: infos[a]["legal_actions"][0]["index"] for a in env.agents_to_move
                    }
                    obs, _r, _t, _tr, infos = env.step(actions)
                    hashes.append(tuple(infos[a]["state_hash"] for a in sorted(infos)))
                return hashes

        self.assertEqual(run(), run())


if __name__ == "__main__":
    unittest.main()
