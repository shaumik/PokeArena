// refresh.js — pulls Pokémon Showdown's canonical data via @pkmn/sim and
// @pkmn/randoms and writes a frozen snapshot to ../upstream/. Run rarely.
// The Go ETL in cmd/data-sync consumes the snapshot; this script does not
// touch data/*.json directly.
//
// The snapshot scope is Gen 1: species num 1-151, with moves and stats from
// the gen=1 dex (so values are internally consistent with that generation's
// mechanics). Movesets come from the gen1randombattle data, which mirrors
// the gen-1-era random battle pools.

'use strict';

const fs = require('fs');
const path = require('path');
const {Dex} = require('@pkmn/sim');
const {TeamGenerators} = require('@pkmn/randoms');

const OUT_DIR = path.resolve(__dirname, '..', 'upstream');
const GEN = 1;
function pkgVersion(name) {
  const p = path.join(__dirname, 'node_modules', name, 'package.json');
  return JSON.parse(fs.readFileSync(p, 'utf8')).version;
}
const SIM_VERSION = pkgVersion('@pkmn/sim');
const RANDOMS_VERSION = pkgVersion('@pkmn/randoms');

// slugify turns Showdown's human move/species name into our kebab-case ID:
//   "Fire Blast"  -> "fire-blast"
//   "Double-Edge" -> "double-edge"
//   "Will-O-Wisp" -> "will-o-wisp"
// We key off `name` (not Showdown's internal ID) because Showdown collapses
// to a stripped-lowercase form ("fireblast") that loses word boundaries.
function slugify(name) {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');
}

function dumpSpecies(dex) {
  const out = [];
  for (const sp of dex.species.all()) {
    if (sp.num < 1 || sp.num > 151) continue;
    if (sp.isNonstandard) continue; // CAP and Pokestar
    if (sp.forme) continue;          // skip alternative formes (Mega, etc.)
    out.push({
      num: sp.num,
      id: slugify(sp.name),
      name: sp.name,
      types: sp.types.slice(),
      baseStats: {...sp.baseStats},
      prevo: sp.prevo || '',
      evos: (sp.evos || []).slice(),
    });
  }
  out.sort((a, b) => a.num - b.num);
  return out;
}

// dumpMoves emits every move referenced by gen1randombattle movesets for the
// scoped species, plus a small set of always-include moves (struggle is
// synthetic in the engine; we don't need it from upstream).
function dumpMoves(dex, referencedIDs) {
  const out = [];
  for (const id of [...referencedIDs].sort()) {
    const m = dex.moves.get(id);
    if (!m.exists) {
      throw new Error(`move id "${id}" does not exist in Gen ${GEN}`);
    }
    out.push({
      id: slugify(m.name),
      name: m.name,
      type: m.type,
      category: m.category,
      basePower: m.basePower,
      accuracy: m.accuracy, // number, or true for always-hits
      pp: m.pp,
      priority: m.priority,
      target: m.target,
      flags: {...m.flags},
      secondary: m.secondary || null,
      secondaries: m.secondaries ? m.secondaries.map((s) => ({...s})) : null,
      self: m.self ? {...m.self} : null,
      boosts: m.boosts ? {...m.boosts} : null,
      status: m.status || '',
      volatileStatus: m.volatileStatus || '',
      recoil: m.recoil || null,
      drain: m.drain || null,
      heal: m.heal || null,
    });
  }
  return out;
}

// SKIP_TYPES filters out types that aren't real combat types — Stellar is the
// Gen 9 tera-type mechanic, not a damageable type, and "???" is the engine's
// internal placeholder used during prep moves like Curse.
const SKIP_TYPES = new Set(['Stellar', '???', 'Bird']);

// dumpTypechart emits the 18×18 effectiveness table. Showdown stores per
// defending-type with damageTaken[ATK] = {0,1,2,3}: 0=neutral, 1=resist,
// 2=weakness, 3=immune. We flatten to our atk→def→multiplier shape so
// domain.LoadDexFS reads it directly. We pull from the latest gen — the
// type chart is post-Fairy/post-Steel canonical even though our species are
// scoped to Gen 1.
function dumpTypechart() {
  const dex = Dex; // latest gen for full 18-type chart
  const codeToMult = {0: 1, 1: 0.5, 2: 2, 3: 0};
  const result = {};
  const types = dex.types.all().filter((t) => !SKIP_TYPES.has(t.name));
  for (const atk of types) {
    const row = {};
    for (const def of types) {
      const code = def.damageTaken[atk.name];
      if (code === undefined) continue;
      const mult = codeToMult[code];
      if (mult === 1) continue; // skip neutral; default 1.0 is implicit
      row[def.name] = mult;
    }
    result[atk.name] = row;
  }
  return result;
}

// dumpRandombattleSets pulls each scoped species's Gen 1 randombattle pool.
// We collect: moves, essentialMoves, exclusiveMoves, comboMoves. The Go ETL
// uses these (with a deterministic heuristic) to pick the 4-move set per
// species. We don't pick here so the upstream snapshot stays a pure data dump.
function dumpRandombattleSets(species) {
  const gen = TeamGenerators.getTeamGenerator('gen1randombattle', [0, 0, 0, 0]);
  const data = gen.randomData;
  const sets = {};
  for (const sp of species) {
    const key = sp.id.replace(/-/g, '');
    const entry = data[key] || data[sp.id];
    if (!entry) {
      // Some species (e.g. legendaries, certain Pokémon) may be absent from
      // the standard randombattle pool. Record an empty pool so the Go ETL
      // can decide how to handle them (skip the species, fall back to
      // learnset, etc.).
      sets[sp.id] = {level: null, moves: [], essentialMoves: [], exclusiveMoves: [], comboMoves: []};
      continue;
    }
    sets[sp.id] = {
      level: entry.level || null,
      moves: entry.moves || [],
      essentialMoves: entry.essentialMoves || [],
      exclusiveMoves: entry.exclusiveMoves || [],
      comboMoves: entry.comboMoves || [],
    };
  }
  return sets;
}

function writeJSON(filename, data) {
  const out = path.join(OUT_DIR, filename);
  fs.writeFileSync(out, JSON.stringify(data, null, 2) + '\n');
  return out;
}

function main() {
  fs.mkdirSync(OUT_DIR, {recursive: true});
  const dex = Dex.forGen(GEN);

  const species = dumpSpecies(dex);
  const sets = dumpRandombattleSets(species);

  // Collect every move referenced by any randombattle set, so the moves dump
  // covers exactly what we need (plus deterministic ordering via the set
  // intersection).
  const referenced = new Set();
  for (const sp of species) {
    const s = sets[sp.id];
    for (const lst of [s.moves, s.essentialMoves, s.exclusiveMoves, s.comboMoves]) {
      for (const id of lst) referenced.add(id);
    }
  }
  const moves = dumpMoves(dex, referenced);
  const typechart = dumpTypechart();

  const meta = {
    gen: GEN,
    sim_version: SIM_VERSION,
    randoms_version: RANDOMS_VERSION,
    refreshed_at: new Date().toISOString(),
    species_count: species.length,
    moves_count: moves.length,
  };

  writeJSON('species.json', species);
  writeJSON('moves.json', moves);
  writeJSON('typechart.json', typechart);
  writeJSON('randombattle-sets.json', sets);
  writeJSON('_meta.json', meta);

  console.log(
    `wrote snapshot: ${species.length} species, ${moves.length} moves, ` +
      `${Object.keys(typechart).length} types — gen ${GEN}, @pkmn/sim ${SIM_VERSION}`
  );
}

main();
