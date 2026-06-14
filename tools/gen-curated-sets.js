'use strict';
// gen-curated-sets.js — derives a static curated-moveset table from the
// canonical dataset (data/pokedex.json + data/moves.json) and writes it to
// web/curated-sets.js as a hardcoded `CURATED_SETS` map (dex_no -> [move_id]).
//
// Every emitted move is guaranteed to be in that species' learnset, so the
// table can never go illegal against the engine. Re-run after a data-sync:
//   node tools/gen-curated-sets.js
//
// The heuristic, per species:
//   1. Pick the best STAB attacking move for each of the mon's types,
//      biased toward its stronger attacking stat (physical vs special).
//   2. Add one utility move appropriate to its role (recovery for bulky
//      mons, setup for strong attackers, else a status/hazard move).
//   3. Fill the rest with the highest-power coverage moves of new types.
// It is intentionally conservative — these are sane defaults a human can
// then tweak in the builder, not optimal competitive sets.

const fs = require('fs');
const path = require('path');

const ROOT = path.join(__dirname, '..');
const species = JSON.parse(fs.readFileSync(path.join(ROOT, 'data/pokedex.json'), 'utf8'));
const moves = JSON.parse(fs.readFileSync(path.join(ROOT, 'data/moves.json'), 'utf8'));
const M = {};
moves.forEach((m) => { M[m.id] = m; });

// Flagless conditional/gimmick moves whose raw power lies about their value:
// they need a setup turn, a sleeping target, a prior KO, etc. Penalized hard
// so the heuristic only reaches for them when a species has nothing better.
const CONDITIONAL = new Set([
  'focus-punch', 'last-resort', 'dream-eater', 'belch', 'steel-roller',
  'swallow', 'spit-up', 'natural-gift', 'fake-out', 'first-impression',
  'sky-drop', 'beat-up', 'present', 'return', 'frustration', 'hidden-power',
]);

// effPower scores an attacking move: power discounted by accuracy, then by
// any drawback — recharge turns, two-turn charges, self-KO, recoil, and
// gimmick conditions — so a clean 90/100 beats a flashy 150 with strings.
function effPower(m) {
  const acc = m.accuracy && m.accuracy > 0 ? m.accuracy : 100;
  let p = m.power * (acc / 100);
  const flags = m.flags || [];
  if (flags.includes('recharge')) p *= 0.45;       // lose the next turn
  if (flags.includes('two-turn')) p *= 0.5;        // telegraphed charge
  if (flags.includes('selfdestruct')) p *= 0.05;   // user faints
  if (m.self && m.self.recoil) p *= 1 - m.self.recoil * 0.5; // recoil bite
  if (CONDITIONAL.has(m.id)) p *= 0.25;
  return p;
}

// classifyUtility tags a status move with a role, or returns null if it's
// not something we'd ever auto-pick (e.g. Growl, Flash).
function classifyUtility(m) {
  if (m.category !== 'status') return null;
  const p = m.primary || {};
  // Recovery: heals the user.
  if (m.flags && m.flags.includes('heal') && (m.target === 'self' || m.id === 'rest')) return 'recovery';
  if (typeof p.heal === 'number' && p.heal > 0) return 'recovery';
  // Setup: raises the user's own stats.
  if (p.boosts && m.target === 'self') {
    const sum = Object.values(p.boosts).reduce((a, b) => a + b, 0);
    if (sum > 0) return 'setup';
  }
  // Hazards.
  if (m.side_condition && ['stealthrock', 'spikes', 'toxicspikes'].includes(m.side_condition)) return 'hazard';
  // Status infliction on the foe.
  if (p.status && m.target === 'foe') return 'status';
  // Sticky utility volatiles.
  if (p.volatile && ['leechseed', 'substitute'].includes(p.volatile)) return 'pivot';
  return null;
}

// setupMatchesBias keeps a setup move only if it pumps the stat the mon
// actually attacks with — no Calm Mind on a physical sweeper.
function setupMatchesBias(m, physical) {
  const b = (m.primary && m.primary.boosts) || {};
  if (physical) return (b.attack || 0) > 0;
  return (b.spatk || 0) > 0;
}

function buildSet(sp) {
  const types = [sp.type1, sp.type2].filter(Boolean);
  const physical = sp.base.atk >= sp.base.spatk;
  const preferred = physical ? 'physical' : 'special';
  const learn = sp.moves.map((id) => M[id]).filter(Boolean);

  const attackers = learn.filter((m) => (m.category === 'physical' || m.category === 'special') && m.power > 0);
  const picked = [];
  const usedTypes = new Set();

  // 1. Best STAB per type, biased to the mon's attacking category.
  for (const t of types) {
    const cands = attackers.filter((m) => m.type === t).sort((a, b) => {
      const ab = (a.category === preferred ? 1000 : 0) + effPower(a);
      const bb = (b.category === preferred ? 1000 : 0) + effPower(b);
      return bb - ab;
    });
    if (cands.length && !picked.includes(cands[0].id)) {
      picked.push(cands[0].id);
      usedTypes.add(cands[0].type);
    }
  }

  // 2. One role-appropriate utility move.
  const utils = learn.map((m) => ({ m, role: classifyUtility(m) })).filter((x) => x.role);
  const bulk = sp.base.hp + sp.base.def + sp.base.spdef;
  const roleOrder = bulk >= 270
    ? ['recovery', 'hazard', 'status', 'setup', 'pivot']
    : ['setup', 'hazard', 'status', 'recovery', 'pivot'];
  let util = null;
  for (const role of roleOrder) {
    const c = utils.filter((x) => x.role === role && (role !== 'setup' || setupMatchesBias(x.m, physical)));
    if (c.length) { util = c[0].m; break; }
  }
  if (util && !picked.includes(util.id) && picked.length < 4) picked.push(util.id);

  // 3. Fill remaining slots with the strongest coverage of new types,
  //    then any strongest attacker left.
  const coverage = attackers
    .filter((m) => !picked.includes(m.id))
    .sort((a, b) => {
      const an = (usedTypes.has(a.type) ? 0 : 200) + (a.category === preferred ? 100 : 0) + effPower(a);
      const bn = (usedTypes.has(b.type) ? 0 : 200) + (b.category === preferred ? 100 : 0) + effPower(b);
      return bn - an;
    });
  for (const m of coverage) {
    if (picked.length >= 4) break;
    if (picked.includes(m.id)) continue;
    picked.push(m.id);
    usedTypes.add(m.type);
  }

  // Last resort (move-poor species): pad from the rest of the learnset.
  for (const m of learn) {
    if (picked.length >= 4) break;
    if (!picked.includes(m.id)) picked.push(m.id);
  }
  return picked.slice(0, 4);
}

const out = {};
species
  .slice()
  .sort((a, b) => a.dex_no - b.dex_no)
  .forEach((sp) => { out[sp.dex_no] = buildSet(sp); });

const header = `'use strict';
// curated-sets.js — GENERATED by tools/gen-curated-sets.js. Do not edit by
// hand; re-run the generator after a data-sync. Maps dex_no -> a sane default
// 4-move set (all moves guaranteed legal for that species). The builder seeds
// a freshly-picked Pokémon from this, and "Smart-fill" resets to it.
const CURATED_SETS = `;
const body = JSON.stringify(out, null, 0)
  .replace(/\],/g, '],\n  ')
  .replace(/^\{/, '{\n  ')
  .replace(/\}$/, ',\n}'); // dangling comma is harmless and keeps diffs clean... actually drop it
const json = '{\n' + Object.keys(out).map((k) => `  ${k}: ${JSON.stringify(out[k])},`).join('\n') + '\n}';

fs.writeFileSync(
  path.join(ROOT, 'web/curated-sets.js'),
  header + json + ';\n',
);

// Print a quick audit so a human can eyeball quality.
let total = 0, short = 0;
species.forEach((sp) => {
  const s = out[sp.dex_no];
  total++;
  if (s.length < 4) short++;
  if (process.env.VERBOSE) {
    console.log(String(sp.dex_no).padStart(3), sp.name.padEnd(12), s.map((id) => M[id].name).join(', '));
  }
});
console.log(`wrote web/curated-sets.js — ${total} species, ${short} with <4 moves`);
