# PokéArena

**A deterministic Pokémon battle environment for RL and LLM evaluation.**
Gymnasium and PettingZoo APIs, hidden information, no server.

```python
from pokearena import PokeArenaEnv

with PokeArenaEnv(team="Genesis", opponent="heuristic") as env:
    obs, info = env.reset(seed=42)                    # seed 42 is always this battle
    terminated = truncated = False
    while not (terminated or truncated):
        action = info["legal_actions"][0]["index"]    # your policy goes here
        obs, reward, terminated, truncated, info = env.step(action)
    print("winner:", info["winner"], "after", info["turn"], "turns")
```

---

## Why this one

Most game environments are stochastic in ways you cannot pin down, hand you
perfect information, and need a server, a container, or an emulator running
somewhere. This one is built the other way round.

**Determinism is the product.** `reset(seed=k)` plays battle *k* — the same
rosters, the same RNG stream, the same damage rolls, the same outcome — every
time, on every machine. Not "approximately reproducible": the engine emits a
state hash at every decision point, and two runs of the same seed produce the
same sequence of hashes. That is what lets you diff two policies, bisect a
regression, or publish a number someone else can re-derive.

**Variance is controlled by construction.** The default setup is a **mirror
match**: identical rosters on both sides, so the only free variable left in a
result is the policy. This is the setup the project's own benchmark runs, and
the seeding path here is the same one, so a number you measure is comparable to
a number on its published board.

**Hidden information is real.** Each side sees its own team in full and only the
opponent's *active* Pokémon — with its exact HP, ability, held item, stats,
EVs/IVs, nature and move PP redacted, exactly as Pokémon Showdown redacts them.
The hidden data is not in the bytes your agent receives, so it cannot be read by
accident. That makes this a genuine imperfect-information game: inference about
the opponent's set is part of playing well, not an afterthought.

**No server.** No ports, no Docker, no database, no ROM, no dataset download.
The environment is a single Go binary the Python package spawns as a subprocess
and talks to over stdin/stdout in line-delimited JSON. The dataset is compiled
into it. One process is one environment; run *N* for *N* parallel envs.

**Zero required Python dependencies.** Gymnasium, PettingZoo and NumPy are all
optional. Install any of them and the environments subclass and use them;
install none and the same API works standalone.

---

## Install

```bash
pip install pokearena
```

That gets the Python client. It does **not** get the engine — the engine is a Go
binary, and this package is honest about it rather than shipping a fake
pure-Python fallback:

```bash
go install github.com/shaumik/PokeArena/cmd/pokearena-env@latest
```

Make sure the install directory (`go env GOPATH`/bin, usually `~/go/bin`) is on
your `PATH`. If you would rather not install a Go toolchain, download a prebuilt
`pokearena-env` from the
[releases page](https://github.com/shaumik/PokeArena/releases) and either put it
on your `PATH` or point `POKEARENA_ENV_BIN` at it.

If the binary is missing, the package raises `BinaryNotFoundError` with both of
those instructions and a list of everywhere it looked. It never fails silently.

Optional extras:

```bash
pip install "pokearena[gymnasium]"    # subclass gymnasium.Env
pip install "pokearena[pettingzoo]"   # subclass pettingzoo.ParallelEnv, enable aec_env()
pip install "pokearena[all]"          # both, plus numpy for array action masks
```

---

## Single agent (Gymnasium style)

The agent plays one side; a built-in baseline plays the other, in-process inside
the engine binary, seeded from the same battle seed. Current Gymnasium
conventions throughout: `reset(seed=...) -> (obs, info)` and
`step(action) -> (obs, reward, terminated, truncated, info)`.

```python
from pokearena import PokeArenaEnv

env = PokeArenaEnv(
    team="Genesis",          # a curated team, [150, 149, 143], or {"picks": [...]}
    opponent="heuristic",    # random | heuristic | expectimax | expectimax@3
    reward="win_loss",       # or "hp_delta" for dense shaping
)
obs, info = env.reset(seed=0)
obs, reward, terminated, truncated, info = env.step(0)
env.close()
```

**Actions** are a fixed `Discrete(11)`:

| index | meaning |
|-------|---------|
| 0–3   | use move slot 0–3 |
| 4     | Struggle, or the forced move on a charge/recharge turn |
| 5–10  | switch to team slot 0–5 |

The space is fixed rather than renumbered per turn, because renumbering would
make the same integer mean different things at different times. Legality comes
as a **mask** instead — `info["action_mask"]` on every step, a NumPy `int8`
array when NumPy is installed and a plain list otherwise. `info["legal_actions"]`
carries the same set with a human-readable `label` per entry, which is what you
want when the policy is an LLM:

```python
[{"index": 0, "action": {"kind": "move", "index": 0}, "label": "use Psystrike (10/10 PP)"},
 {"index": 6, "action": {"kind": "switch", "index": 1}, "label": "switch to Dragonite"}]
```

`step()` accepts every shape you might plausibly have in hand — a bare `int`, a
NumPy scalar from `argmax`, a whole legal-action record, or its nested engine
object — so `env.step(env.legal_actions()[0])` just works:

```python
la = env.legal_actions()
env.step(la[0])              # the record
env.step(la[0]["action"])    # the engine object — the only form that can aim a pivot
env.step(la[0]["index"])     # the flat index
env.step(0)                  # a bare int
```

Anything else raises `TypeError` from Python, naming the shapes that do work,
rather than surfacing as a confusing complaint about a JSON field you never
typed.

**Observations** are the engine's fog-of-war view, decoded from JSON: `self`
(your whole team, unredacted), `foe` (the opponent's active Pokémon, redacted),
`foe_bench_alive`, `turn`, `phase`, weather, terrain, and the opponent's public
side conditions. It is a nested dict, not a fixed-width vector — flattening it
is a modelling decision that belongs to you, and every reasonable encoding (a
text prompt, a hand-built feature vector, a set encoder) wants something
different.

**Rewards.** `win_loss` is the default and the honest one: 0 every step, ±1 at
the end. `hp_delta` adds per-step shaping from the change in team-HP difference;
it is opt-in because it reads privileged state — the opponent's exact team HP is
deliberately not in any observation. That is normal for a training signal and
would be dishonest in an observation, which is why the two are kept apart.

An illegal action raises `IllegalActionError` and leaves the episode exactly
where it was, so recovering is just picking a legal one and stepping again.

---

## Agent versus agent (PettingZoo style)

A PokéArena turn is **simultaneous**, so the parallel API is the faithful model:

```python
from pokearena import PokeArenaParallelEnv

with PokeArenaParallelEnv(team="Blitz") as env:
    obs, infos = env.reset(seed=0)
    while env.agents:
        actions = {a: my_policy(obs[a], infos[a]) for a in env.agents_to_move}
        obs, rewards, terminations, truncations, infos = env.step(actions)
```

Agents are `player_0` and `player_1`. Most steps ask both sides; after a faint,
only the side with the fainted Pokémon chooses a replacement — `env.agents_to_move`
and `infos[agent]["to_move"]` name whose action is actually consumed, and an
action supplied for anyone else is ignored, so the usual
`{a: policy(a) for a in env.agents}` loop is safe. Every agent always receives
its own observation, projected independently.

`pokearena.aec_env()` returns an AEC view via PettingZoo's own conversion, for
tooling that requires one. Prefer the parallel env: AEC imposes a turn order the
game does not actually have.

---

## Provenance

Every environment exposes the engine's handshake, which is the record you quote
when you publish a number:

```python
>>> env.engine_info
{'protocol_version': '1.0',
 'engine_revision': '5caa7fb4317c61a08991a4950d8329beafb082ce',
 'level': 50,
 'ruleset': 'L50, IVs 0-31, EVs 252 per stat / 510 total, 25 natures, held items, Species Clause, mirror match',
 'dataset': {'sim_version': '0.10.9', 'curation_sha': '55ded32e...', 'source_gen': 9, ...},
 'team_library': {'version': 'v2', 'teams': ['Bastion', 'Blitz', 'Bruiser', 'Genesis', 'Keystone', 'Spectrum'], ...},
 ...}
```

Engine revision plus dataset SHA plus ruleset plus seed pins a result
completely. A trajectory can never be silently reattributed to a different
engine or a different dataset.

---

## Teams

Three ways to specify one, all accepted by `team=` and `opponent_team=`:

```python
PokeArenaEnv(team="Genesis")            # a curated, legality-checked library team
PokeArenaEnv(team=[150, 149, 143])      # ad-hoc: Pokedex numbers, first 4 moves each
PokeArenaEnv(team={"picks": [           # full control, validated before play
    {"dex_no": 150, "moves": ["psystrike", "ice-beam", "recover", "aura-sphere"],
     "nature": "timid", "evs": {"spatk": 252, "speed": 252, "hp": 4}},
    # ...
]})
```

`opponent_team` defaults to `team` — the mirror match. Set it to make the
matchup asymmetric. Every team passes the engine's legality check (six or fewer
Pokémon, Species Clause, learnset-legal moves, the EV/IV budget) before a
battle starts; an illegal team is a clear error, never a silently corrected one.

`env.team_library` lists the curated teams the binary carries.

---

## Low-level protocol access

`EngineClient` is the raw JSONL client if you want to build something the two
env classes do not cover:

```python
from pokearena import EngineClient

with EngineClient() as engine:
    engine.request("reset", {"seed": 0, "team": "Genesis", "agents": ["external", "external"]})
    result = engine.request("step", {"actions": [0, 0]})
```

The full wire contract — every command, every field, the determinism guarantee
and the fog-of-war guarantee — is in
[`docs/python-env.md`](https://github.com/shaumik/PokeArena/blob/main/docs/python-env.md).

---

## Requirements & caveats

- Python 3.9+.
- **A Go toolchain (1.22+) or a release binary is required** for the engine.
  There is no pure-Python wheel and this package does not pretend otherwise.
- One engine process per environment instance. Vectorise by running several.
- LLM opponents are not part of this package; the built-in baselines are
  deterministic Go agents (`random`, `heuristic`, `expectimax`).

MIT licensed. Issues and contributions:
<https://github.com/shaumik/PokeArena>.
