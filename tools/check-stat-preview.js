'use strict';
// check-stat-preview.js — proves the builder's stat preview agrees with the
// engine.
//
// web/app.js:derivedStat is a hand-written mirror of engine.calcStat /
// engine.calcHP, so the team builder can show what a spread does before the
// server ever sees it. Two implementations of one formula drift; this script
// and TestStatPreviewChecksum in internal/engine/spread_test.go both reduce
// the same 216,000-value sweep (every species × every nature × six EV values
// × three IV values × six stats) to one number, and both assert the constants
// below.
//
// Change the formula and both fail. Change only one implementation and only
// that one fails, naming the other.
//
//   node tools/check-stat-preview.js
//
// Exits non-zero on mismatch.

const fs = require('fs');
const path = require('path');

const ROOT = path.join(__dirname, '..');
const species = JSON.parse(fs.readFileSync(path.join(ROOT, 'data/pokedex.json'), 'utf8'));
const natures = JSON.parse(fs.readFileSync(path.join(ROOT, 'data/natures.json'), 'utf8'));

// Must match engine.Level and the constants in internal/engine/spread_test.go.
const LEVEL = 50;
const EXPECTED_COUNT = 216000;
const EXPECTED_SUM = 23809332;

const KEYS = ['hp', 'atk', 'def', 'spatk', 'spdef', 'speed'];
const EV_SET = [0, 3, 4, 8, 100, 252]; // 3 and 4 straddle the floor(EV/4) step
const IV_SET = [0, 15, 31];

// --- the mirror under test: keep in step with web/app.js ---
function natureRatio(nature, key) {
  if (!nature || !nature.plus || nature.plus === nature.minus) return [1, 1];
  if (nature.plus === key) return [11, 10];
  if (nature.minus === key) return [9, 10];
  return [1, 1];
}

function derivedStat(key, base, iv, ev, nature) {
  const raw = Math.floor((2 * base + iv + Math.floor(ev / 4)) * LEVEL / 100);
  if (key === 'hp') return raw + LEVEL + 10;
  const [num, den] = natureRatio(nature, key);
  return Math.floor((raw + 5) * num / den);
}
// --- end mirror ---

let count = 0;
let sum = 0;
for (const sp of species) {
  for (const nature of natures) {
    for (const ev of EV_SET) {
      for (const iv of IV_SET) {
        for (const key of KEYS) {
          sum += derivedStat(key, sp.base[key], iv, ev, nature);
          count += 1;
        }
      }
    }
  }
}

if (count !== EXPECTED_COUNT || sum !== EXPECTED_SUM) {
  console.error(
    `stat preview MISMATCH: got count=${count} sum=${sum}, ` +
      `want count=${EXPECTED_COUNT} sum=${EXPECTED_SUM}\n` +
      'Either the dataset changed, or web/app.js:derivedStat no longer matches\n' +
      'engine.calcStat. If the engine formula changed on purpose, update the\n' +
      'mirror in web/app.js, the copy in this file, and the constants in both\n' +
      'this file and internal/engine/spread_test.go.'
  );
  process.exit(1);
}
console.log(`stat preview OK — ${count} values, checksum ${sum}`);
