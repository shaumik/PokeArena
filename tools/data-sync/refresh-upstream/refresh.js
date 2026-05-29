// refresh.js — pulls Pokémon Showdown's canonical data via @pkmn/sim and
// writes a frozen snapshot to ../upstream/. Run rarely. The Go ETL in
// cmd/data-sync consumes the snapshot; this script does not touch
// data/*.json directly.
//
// The snapshot scope is Gen 1: species num 1-151, with moves and stats
// from the gen=1 dex (so values are internally consistent with that
// generation's mechanics). Movesets come from each species's full Gen-1
// learnset (every move reachable via level-up / TM / HM / tutor in Gen 1).

'use strict';

const fs = require('fs');
const path = require('path');
const {Dex} = require('@pkmn/sim');

const OUT_DIR = path.resolve(__dirname, '..', 'upstream');
const GEN = 1;
function pkgVersion(name) {
  const p = path.join(__dirname, 'node_modules', name, 'package.json');
  return JSON.parse(fs.readFileSync(p, 'utf8')).version;
}
const SIM_VERSION = pkgVersion('@pkmn/sim');

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
  // evos in @pkmn/sim is the union across all generations, so Rhydon shows
  // Rhyperior (Gen 4) even when querying Dex.forGen(1). Filter the evos list
  // to evolutions that themselves exist within our generation scope (≤151);
  // otherwise the Go NotPreEvolution filter wrongly drops final-form Gen 1
  // species (Rhydon, Onix, Chansey, Lickitung, ...) for having later-gen
  // evolutions that wouldn't ship.
  const out = [];
  for (const sp of dex.species.all()) {
    if (sp.num < 1 || sp.num > 151) continue;
    if (sp.isNonstandard) continue; // CAP and Pokestar
    if (sp.forme) continue;          // skip alternative formes (Mega, etc.)
    const evosInScope = (sp.evos || []).filter((evoName) => {
      const evo = dex.species.get(evoName);
      return evo.exists && evo.num >= 1 && evo.num <= 151;
    });
    out.push({
      num: sp.num,
      id: slugify(sp.name),
      name: sp.name,
      types: sp.types.slice(),
      baseStats: {...sp.baseStats},
      prevo: sp.prevo || '',
      evos: evosInScope,
    });
  }
  out.sort((a, b) => a.num - b.num);
  return out;
}

// dumpMoves emits every move referenced by the scoped species's Gen-1
// learnsets (struggle is synthetic in the engine; we don't need it from
// upstream).
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
// defending-type with damageTaken[ATK] = {0,1,2,3}: 0=neutral, 1=weakness
// (defender takes 2x), 2=resistance (defender takes 0.5x), 3=immunity. We
// flatten to our atk→def→multiplier shape so domain.LoadDexFS reads it
// directly. We pull from the latest gen — the type chart is post-Fairy
// canonical even though our species are scoped to Gen 1.
function dumpTypechart() {
  const dex = Dex; // latest gen for full 18-type chart
  const codeToMult = {0: 1, 1: 2, 2: 0.5, 3: 0};
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

// dumpLearnsets emits the full Gen-1 movepool for each scoped species —
// every move reachable via level-up, TM/HM, or tutor in Gen 1. Each tag in
// Showdown's learnset table is of the form "<gen><method><level?>", e.g.
// "1L21" (Gen 1, level-up at 21), "1M" (Gen 1, TM/HM), "1T" (Gen 1, tutor).
// We keep any move with at least one gen-1 tag.
//
// Output order is "natural progression first": moves are sorted by the
// lowest gen-1 level-up level they appear at (so Charizard's first picks
// are scratch/growl/leer/ember, not an alphabetical jumble), and non-
// level-up moves (TMs, tutors) come after sorted alphabetically. This
// keeps the picker UI's "first 4 = sensible default" property meaningful
// when the user doesn't customize the picks.
function dumpLearnsets(dex, species) {
  const out = {};
  for (const sp of species) {
    const key = dex.species.get(sp.id).id; // Showdown's stripped id, e.g. "mrmime"
    const entry = dex.data.Learnsets[key];
    const learnset = (entry && entry.learnset) || {};
    const scored = [];
    for (const [moveId, tags] of Object.entries(learnset)) {
      const gen1 = (tags || []).filter((t) => t && t[0] === '1');
      if (gen1.length === 0) continue;
      let minLevel = Infinity;
      for (const t of gen1) {
        if (t[1] === 'L') {
          const n = parseInt(t.slice(2), 10);
          if (!isNaN(n) && n < minLevel) minLevel = n;
        }
      }
      scored.push({moveId, minLevel});
    }
    scored.sort((a, b) => (a.minLevel - b.minLevel) || a.moveId.localeCompare(b.moveId));
    out[sp.id] = scored.map((s) => s.moveId);
  }
  return out;
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
  const learnsets = dumpLearnsets(dex, species);

  // Collect every move referenced by any learnset, so the moves dump
  // covers exactly what we need (no orphans, no unused moves).
  const referenced = new Set();
  for (const sp of species) {
    for (const id of learnsets[sp.id] || []) referenced.add(id);
  }
  const moves = dumpMoves(dex, referenced);
  const typechart = dumpTypechart();

  const meta = {
    gen: GEN,
    sim_version: SIM_VERSION,
    refreshed_at: new Date().toISOString(),
    species_count: species.length,
    moves_count: moves.length,
  };

  writeJSON('species.json', species);
  writeJSON('moves.json', moves);
  writeJSON('typechart.json', typechart);
  writeJSON('learnsets.json', learnsets);
  writeJSON('_meta.json', meta);

  console.log(
    `wrote snapshot: ${species.length} species, ${moves.length} moves, ` +
      `${Object.keys(typechart).length} types — gen ${GEN}, @pkmn/sim ${SIM_VERSION}`
  );
}

main();
