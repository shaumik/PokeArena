// refresh.js — pulls Pokémon Showdown's canonical data via @pkmn/sim and
// writes a frozen snapshot to ../upstream/. Run rarely. The Go ETL in
// cmd/data-sync consumes the snapshot; this script does not touch
// data/*.json directly.
//
// The snapshot scope is "Gen 1 roster, modern mechanics": species num
// 1-151 (base forms only) but base stats, types, abilities, moves, and
// learnsets come from the current generation (GEN below). This is the
// "vintage roster + modern fight feel" intent from the engine
// modernization plan (issue #30). Movesets are the cumulative learnset
// across all gens up to GEN (i.e. every move legal on the species in
// the current metagame).

'use strict';

const fs = require('fs');
const path = require('path');
const {Dex} = require('@pkmn/sim');

const OUT_DIR = path.resolve(__dirname, '..', 'upstream');
const GEN = 9;
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
  // We iterate dex.data.Species (the raw table) rather than dex.species.all()
  // because the latter silently drops species flagged `isNonstandard: 'Past'`
  // — i.e. mons cut from the current gen's playable metagame. ~50 of the
  // original 151 are in that bucket (Pidgey line, Caterpie line, Nidoran
  // lines, etc.). We still want them, with their modern stats/types/abilities
  // — the "vintage roster, modern feel" intent (issue #30). We only filter
  // CAP and Pokestar fake-species.
  //
  // evos in @pkmn/sim is the union across all generations, so Rhydon shows
  // Rhyperior (Gen 4). Filter to evolutions that themselves exist within our
  // 1-151 scope; otherwise the Go NotPreEvolution filter would wrongly drop
  // final-form Gen 1 species (Rhydon, Onix, Chansey, Lickitung, ...) for
  // having later-gen evolutions that wouldn't ship.
  const out = [];
  for (const id of Object.keys(dex.data.Species)) {
    const sp = dex.species.get(id);
    if (!sp.exists) continue;
    if (sp.num < 1 || sp.num > 151) continue;
    if (sp.isNonstandard === 'CAP' || sp.isNonstandard === 'Pokestar') continue;
    if (sp.forme) continue;          // skip alternative formes (Mega, regional, etc.)
    const evosInScope = (sp.evos || []).filter((evoName) => {
      const evo = dex.species.get(evoName);
      return evo.exists && evo.num >= 1 && evo.num <= 151;
    });
    // sp.abilities is shaped {0: "Overgrow", 1?: "...", H?: "Chlorophyll"};
    // 0 is always present, 1 is the second normal slot (may be absent),
    // and H is the hidden ability. We pass it through unchanged so the Go
    // side can pick "slot 0 by default, picker may select 1 or H."
    const abilities = {0: sp.abilities[0] || ''};
    if (sp.abilities[1]) abilities[1] = sp.abilities[1];
    if (sp.abilities.H) abilities.H = sp.abilities.H;
    out.push({
      num: sp.num,
      id: slugify(sp.name),
      name: sp.name,
      types: sp.types.slice(),
      baseStats: {...sp.baseStats},
      abilities,
      prevo: sp.prevo || '',
      evos: evosInScope,
    });
  }
  out.sort((a, b) => a.num - b.num);
  return out;
}

// dumpMoves emits every move referenced by the scoped species's learnsets
// (struggle is synthetic in the engine; we don't need it from upstream).
//
// We capture every field Showdown stores statically. Behavior encoded only
// as JS callbacks (onBeforeMove, damageCallback, etc.) is not capturable
// here — moves that depend on those go on manualMoveFlags in the Go
// transform. The audit step (issue #30 step 2) walks unmapped statics and
// either maps them to engine flags, files a sub-ticket, or denylists the
// move.
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
      // Modern-mechanics statics — these did not exist (or were rare) in
      // the Gen-1 snapshot. The Go side decides what to map to engine
      // flags vs. denylist vs. defer.
      breaksProtect:    m.breaksProtect || false,
      forceSwitch:      m.forceSwitch || false,
      selfSwitch:       m.selfSwitch || false,    // true, "copyvolatile", "shedtail", etc.
      sleepUsable:      m.sleepUsable || false,
      multihit:         m.multihit || null,        // number or [min, max]
      thawsTarget:      m.thawsTarget || false,
      ohko:             m.ohko || false,           // true, "Ice", etc.
      willCrit:         m.willCrit || false,
      ignoreAbility:    m.ignoreAbility || false,
      ignoreDefensive:  m.ignoreDefensive || false,
      ignoreEvasion:    m.ignoreEvasion || false,
      ignoreImmunity:   m.ignoreImmunity || false,
      noPPBoosts:       m.noPPBoosts || false,
      weather:          m.weather || '',
      terrain:          m.terrain || '',
      pseudoWeather:    m.pseudoWeather || '',
      sideCondition:    m.sideCondition || '',
      slotCondition:    m.slotCondition || '',
      stallingMove:     m.stallingMove || false,
    });
  }
  return out;
}

// dumpItems emits the full standard item catalog (id + display name). Unlike
// species/moves there's no learnset to scope against — items are universal —
// so we dump every existing item and let the Go transform's curated allowlist
// pick the ones the engine models. Only CAP/Pokestar fakes are filtered, the
// same fake-content exclusion dumpSpecies applies.
function dumpItems(dex) {
  const out = [];
  for (const id of Object.keys(dex.data.Items)) {
    const item = dex.items.get(id);
    if (!item.exists) continue;
    if (item.isNonstandard === 'CAP' || item.isNonstandard === 'Pokestar') continue;
    out.push({id: slugify(item.name), name: item.name});
  }
  out.sort((a, b) => a.id.localeCompare(b.id));
  return out;
}

// dumpNatures emits all 25 natures with the stat each one raises and lowers,
// in Showdown's stat ids (atk/def/spa/spd/spe). The five neutral natures
// (Hardy, Docile, Serious, Bashful, Quirky) carry neither key — absence is
// the signal, so the Go transform never has to special-case a name list.
//
// Natures are gen-independent (unchanged since Gen 3), so this reads the
// latest-gen table like dumpTypechart does rather than the GEN-scoped dex.
function dumpNatures() {
  const out = [];
  for (const nature of Dex.natures.all()) {
    const entry = {id: slugify(nature.name), name: nature.name};
    if (nature.plus) entry.plus = nature.plus;
    if (nature.minus) entry.minus = nature.minus;
    out.push(entry);
  }
  out.sort((a, b) => a.id.localeCompare(b.id));
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

// dumpLearnsets emits the cumulative movepool for each scoped species —
// every move reachable via level-up, TM/HM, tutor, etc. in any gen up to
// GEN. Each tag in Showdown's learnset table is of the form
// "<gen><method><level?>", e.g. "9L21" (Gen 9, level-up at 21), "8M"
// (Gen 8, TM), "3T" (Gen 3, tutor). We keep any move with at least one
// tag from gens 1..GEN.
//
// Output order is "natural progression first": moves are sorted by the
// lowest level-up level they appear at across any gen, so picker defaults
// like "first 4" still feel like sensible early-game picks rather than an
// alphabetical jumble. Non-level-up moves (TMs, tutors) come after,
// sorted alphabetically.
function dumpLearnsets(dex, species) {
  const out = {};
  for (const sp of species) {
    const key = dex.species.get(sp.id).id; // Showdown's stripped id, e.g. "mrmime"
    const entry = dex.data.Learnsets[key];
    const learnset = (entry && entry.learnset) || {};
    const scored = [];
    for (const [moveId, tags] of Object.entries(learnset)) {
      // tag[0] is the gen-prefix character ('1'..'9'). Lexicographic
      // compare works because they're single digits 1..9.
      const inScope = (tags || []).filter((t) => t && t[0] >= '1' && t[0] <= String(GEN));
      if (inScope.length === 0) continue;
      let minLevel = Infinity;
      for (const t of inScope) {
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
  const items = dumpItems(dex);
  const natures = dumpNatures();

  const meta = {
    gen: GEN,
    sim_version: SIM_VERSION,
    refreshed_at: new Date().toISOString(),
    species_count: species.length,
    moves_count: moves.length,
    items_count: items.length,
    natures_count: natures.length,
  };

  writeJSON('species.json', species);
  writeJSON('moves.json', moves);
  writeJSON('typechart.json', typechart);
  writeJSON('learnsets.json', learnsets);
  writeJSON('items.json', items);
  writeJSON('natures.json', natures);
  writeJSON('_meta.json', meta);

  console.log(
    `wrote snapshot: ${species.length} species, ${moves.length} moves, ` +
      `${items.length} items, ${Object.keys(typechart).length} types, ` +
      `${natures.length} natures — gen ${GEN}, @pkmn/sim ${SIM_VERSION}`
  );
}

main();
