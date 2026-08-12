'use strict';

// ---- constants ----
const TYPE_COLORS = {
  normal: '#9099a1', fire: '#ff6b3d', water: '#3b91f0', electric: '#f3c92f',
  grass: '#54b35a', ice: '#74cec0', fighting: '#d3425f', poison: '#b566ce',
  ground: '#d97845', flying: '#8fa9df', psychic: '#fa7179', bug: '#92bc2c',
  rock: '#c5b88c', ghost: '#5269ac', dragon: '#0c6cdc', dark: '#5a5366',
  steel: '#5a8ea1', fairy: '#ec8fe6',
};
const spriteUrl = (dex) =>
  `https://raw.githubusercontent.com/PokeAPI/sprites/master/sprites/pokemon/${dex}.png`;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
// Move-animation tuning. FX_SPEED scales every effect duration (lower = snappier);
// REDUCED_MOTION honors the OS accessibility setting and skips all motion.
const FX_SPEED = 1;
const REDUCED_MOTION = !!(window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)').matches);
// Element types that read as a "ray/stream" get a beam; other specials get a
// thrown projectile. Physical moves lunge; status moves pulse a ring.
const RAY_TYPES = new Set(['fire', 'electric', 'ice', 'water', 'dragon', 'psychic', 'grass']);
const esc = (s) => String(s).replace(/[&<>"]/g, (c) =>
  ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));

// ---- app state ----
// A builder state holds one editable team: the dex numbers (team), the chosen
// moves, ability, held item and training spread per species, plus transient
// editor UI (which Pokémon is open, which move slot is armed, search/filter
// text). The same shape backs all three builder surfaces — the two setup-page
// teams and the picker. item[dexNo] absent or "" means "holds nothing", which
// the backend treats identically to omitting the item field.
//
// The spread maps follow the same absent-means-default contract as the
// backend: nature[dexNo] absent or "" is neutral, evs[dexNo] absent is no
// EVs, ivs[dexNo] absent is perfect IVs. Nothing is written until the user
// touches a control, so a team built without opening the Training section
// submits exactly the payload it did before spreads existed.
function newBuilderState() {
  return {
    team: [], moves: {}, ability: {}, item: {},
    nature: {}, evs: {}, ivs: {},
    sel: null, mslot: 0, q: '', mq: '', iq: '', typeFilter: null,
  };
}

const App = {
  pokedex: [], dexByNo: {}, moveById: {}, moveByName: {},
  // items is the held-item catalog from GET /api/items: [{id,name,desc}],
  // already sorted by id. itemById indexes it for the slot summaries and the
  // battle log. An item with an empty desc is one the catalog ships but the
  // engine does not model — the builder labels it so a pick is never a lie.
  items: [], itemById: {},
  // natures is GET /api/natures: [{id,name,plus?,minus?}] sorted by id. A
  // nature with no plus/minus is neutral — the five neutral ones are
  // identified by that absence, never by name.
  natures: [], natureById: {},
  // rules is GET /api/rules — the engine's own constants for level and the
  // EV/IV caps. Defaults below match today's engine and exist only so a
  // gateway that can't serve the endpoint still renders a usable builder;
  // the server revalidates every submission regardless of what we show.
  rules: { level: 50, team_size: 6, moves_min: 1, moves_max: 4, ev_max_per_stat: 252, ev_max_total: 510, iv_max: 31 },
  // Setup page (quicksim) edits both sides; setupSide picks the active one.
  setupSide: 'your',
  your: newBuilderState(),
  opp: newBuilderState(),
  // Picker state. Independent from the setup-page teams — the picker view
  // starts empty regardless of what's drafted on setup. ability[dexNo] absent
  // or "" means "use slot 0" (default), which the backend treats identically
  // to omitting the ability field. pickerSubmitted flips optimistically on
  // send and is reconciled against the room frame's you.submitted.
  pick: newBuilderState(),
  pickerSubmitted: false, pickerDeadlineTimer: null,
  battle: null,
};

// ---- api ----
async function api(path, opts) {
  const res = await fetch(path, opts);
  if (!res.ok) {
    let msg = res.statusText;
    try { msg = (await res.json()).error || msg; } catch (e) { /* ignore */ }
    throw new Error(msg);
  }
  return res.status === 204 ? null : res.json();
}

function toast(msg) {
  const t = document.getElementById('toast');
  t.textContent = msg;
  t.classList.remove('hidden');
  clearTimeout(toast._t);
  toast._t = setTimeout(() => t.classList.add('hidden'), 3800);
}

// ---- views ----
function showView(name) {
  document.querySelectorAll('.view').forEach((v) => v.classList.toggle('hidden', v.id !== name));
  document.querySelectorAll('.nav-btn').forEach((b) => b.classList.toggle('active', b.dataset.view === name));
  if (name === 'leaderboard') loadLeaderboard();
}

// ---- bootstrap ----
async function init() {
  document.querySelectorAll('.nav-btn').forEach((b) => { b.onclick = () => showView(b.dataset.view); });
  // Setup-page side tabs swap which team the shared builder edits. Each
  // side keeps its own builder state, so the open editor / search persists
  // when toggling back and forth.
  document.querySelectorAll('.side-tab').forEach((b) => {
    b.onclick = () => {
      App.setupSide = b.dataset.side;
      document.querySelectorAll('.side-tab').forEach((x) => x.classList.toggle('active', x === b));
      renderSetup();
    };
  });
  document.getElementById('setup-random').onclick = () => randomizeBuilder(setupCtx());
  document.getElementById('setup-smart').onclick = () => smartFillBuilder(setupCtx());
  document.getElementById('start-battle').onclick = startBattle;
  document.getElementById('refresh-lb').onclick = loadLeaderboard;
  document.getElementById('leave-arena').onclick = leaveArena;
  document.getElementById('leave-picker').onclick = leavePicker;
  document.getElementById('picker-submit').onclick = submitPicker;
  document.getElementById('picker-randomize').onclick = randomizePicker;
  document.getElementById('picker-smart').onclick = () => smartFillBuilder(pickerCtx());

  // syncMode owns the setup-page team builder visibility: it's only
  // authoritative for quicksim, where both teams must be present at POST.
  // Live + live_pvp use the dedicated picker view, so the builder here
  // would be misleading. Hide it.
  const modeSel = document.getElementById('mode');
  const syncMode = () => {
    const m = modeSel.value;
    const showTeams = m === 'quicksim';
    document.getElementById('side-tabs').classList.toggle('hidden', !showTeams);
    document.getElementById('setup-random').classList.toggle('hidden', !showTeams);
    document.getElementById('setup-smart').classList.toggle('hidden', !showTeams);
    document.getElementById('setup-builder').classList.toggle('hidden', !showTeams);
  };
  modeSel.onchange = syncMode;
  syncMode();

  try {
    App.pokedex = await api('/api/pokemon');
  } catch (e) {
    toast('Could not load the Pokédex: ' + e.message);
    return;
  }
  // The item catalog is optional garnish: a gateway that can't serve it still
  // gives a fully playable builder (every Pokémon just holds nothing), so a
  // failure here is a toast, not a bailout.
  try {
    App.items = await api('/api/items');
    App.items.forEach((it) => { App.itemById[it.id] = it; });
  } catch (e) {
    toast('Could not load the item catalog: ' + e.message);
  }
  // Natures and the format rules are optional for the same reason: without
  // them every Pokémon is neutral / untrained, which is a legal team. A
  // failure costs the Training section, not the builder.
  try {
    App.natures = await api('/api/natures');
    App.natures.forEach((n) => { App.natureById[n.id] = n; });
  } catch (e) {
    toast('Could not load the nature table: ' + e.message);
  }
  try {
    App.rules = Object.assign({}, App.rules, await api('/api/rules'));
  } catch (e) {
    // Silent: the built-in defaults are correct for the shipped engine, and
    // the server validates the real thing on submit.
  }
  App.pokedex.forEach((p) => {
    App.dexByNo[p.dex_no] = p;
    (p.moves || []).forEach((m) => {
      App.moveById[m.id] = m;
      // Index by display name too so we can re-attach the type chip when the
      // backend emits a log line like "Pikachu used Thunderbolt!" — the engine
      // builds the sentence in Go and we only get the rendered string back.
      if (m.name) App.moveByName[m.name.toLowerCase()] = m;
    });
  });
  renderPokedex();
  randomizeBuilder({ rail: 'setup-rail', pane: 'setup-pane', state: App.your, locked: () => false, onChange: syncSetupBar });
  // Seed the opponent team's state without rendering it (the "your" tab is
  // active on load); switching tabs renders it on demand.
  App.opp.team = [];
  App.pokedex.map((p) => p.dex_no).sort(() => 0.5 - Math.random()).slice(0, 6)
    .forEach((dex) => { App.opp.team.push(dex); seedMon(App.opp, dex); });
  renderSetup();

  // If the page URL carries join params (?battle=…&slot=…&token=…), skip
  // setup and connect straight to the arena. This is the path the
  // opponent takes when they open the share link.
  tryAutoJoin();
}

// =====================================================================
// Shared team builder
//
// One component drives all three builder surfaces — the two setup-page
// teams (quicksim) and the picker. A surface is described by a ctx:
//   { rail, pane }  element ids to render into
//   state           a newBuilderState() the component reads and mutates
//   locked()        true when edits are frozen (picker after submit)
//   onChange()      run after any mutation, to refresh outer chrome
// The left rail shows the six team slots; the right pane swaps between a
// searchable roster grid and a focused per-Pokémon editor.
// =====================================================================

const STAT_KEYS = [
  ['hp', 'HP'], ['atk', 'Atk'], ['def', 'Def'],
  ['spatk', 'SpA'], ['spdef', 'SpD'], ['speed', 'Spe'],
];
const STAT_SHORT = {
  attack: 'Atk', defense: 'Def', spatk: 'SpA', 'sp-atk': 'SpA',
  spdef: 'SpD', 'sp-def': 'SpD', speed: 'Spe', accuracy: 'Acc', evasion: 'Eva',
};
const STATUS_LABELS = {
  burn: 'burn', paralysis: 'paralyze', poison: 'poison', toxic: 'badly poison',
  sleep: 'sleep', freeze: 'freeze',
};
const VOLATILE_LABELS = {
  flinch: 'flinch', confusion: 'confuse', leechseed: 'leech seed',
  substitute: 'Substitute', protect: 'Protect', trapped: 'trap',
};
const SIDECOND_LABELS = {
  stealthrock: 'Stealth Rock', spikes: 'Spikes', toxicspikes: 'Toxic Spikes',
  reflect: 'Reflect', lightscreen: 'Light Screen', auroraveil: 'Aurora Veil',
  tailwind: 'Tailwind', safeguard: 'Safeguard', mist: 'Mist',
};

// ABILITY_INFO carries a one-line description for every ability the engine
// actually models. Anything NOT in this map is flavor-only in our engine —
// the builder labels it "no battle effect yet" so a pick is never a lie
// (Blaze, Torrent, Overgrow, Moxie, etc. are currently no-ops here).
const ABILITY_INFO = {
  analytic: 'Boosts move power when moving last.',
  'battle-armor': 'Blocks critical hits.',
  'big-pecks': 'Defense cannot be lowered.',
  chlorophyll: 'Doubles Speed in harsh sunlight.',
  'clear-body': 'Stats cannot be lowered by the foe.',
  'cloud-nine': 'Negates all weather effects.',
  competitive: 'Sharply raises Sp. Atk when a stat is lowered.',
  'compound-eyes': 'Raises move accuracy.',
  defiant: 'Sharply raises Attack when a stat is lowered.',
  drizzle: 'Summons rain on entry.',
  drought: 'Summons harsh sunlight on entry.',
  'dry-skin': 'Heals in rain, hurt by sun; absorbs Water, weak to Fire.',
  'effect-spore': 'Contact may poison, paralyze, or sleep the attacker.',
  filter: 'Reduces damage from super-effective hits.',
  'flame-body': 'Contact may burn the attacker.',
  'flash-fire': 'Immune to Fire; powers up own Fire moves when hit.',
  guts: 'Boosts Attack when statused (ignores burn cut).',
  hustle: 'Raises Attack but lowers physical accuracy.',
  'hyper-cutter': 'Attack cannot be lowered.',
  'ice-body': 'Heals each turn in hail/snow.',
  immunity: 'Cannot be poisoned.',
  'inner-focus': 'Cannot flinch.',
  insomnia: 'Cannot fall asleep.',
  intimidate: "Lowers the foe's Attack on entry.",
  'iron-fist': 'Boosts punching moves.',
  'keen-eye': 'Accuracy cannot be lowered.',
  levitate: 'Immune to Ground moves.',
  'lightning-rod': 'Draws in Electric moves; immune and raises Sp. Atk.',
  limber: 'Cannot be paralyzed.',
  'magic-guard': 'Only takes damage from direct attacks.',
  'magma-armor': 'Cannot be frozen.',
  'motor-drive': 'Electric moves miss and raise Speed instead.',
  multiscale: 'Halves damage taken at full HP.',
  'natural-cure': 'Heals status when switching out.',
  'own-tempo': 'Cannot be confused.',
  'poison-point': 'Contact may poison the attacker.',
  'quick-feet': 'Raises Speed when statused.',
  'rain-dish': 'Heals each turn in rain.',
  reckless: 'Boosts recoil and crash moves.',
  regenerator: 'Heals ⅓ HP when switching out.',
  'sand-rush': 'Doubles Speed in a sandstorm.',
  'sand-stream': 'Summons a sandstorm on entry.',
  'sap-sipper': 'Grass moves miss and raise Attack instead.',
  'sheer-force': 'Drops secondary effects for more power.',
  'shell-armor': 'Blocks critical hits.',
  'shield-dust': 'Blocks added effects of attacks.',
  'slush-rush': 'Doubles Speed in hail/snow.',
  sniper: 'Critical hits deal even more damage.',
  'snow-warning': 'Summons snow on entry.',
  'solar-power': 'Raises Sp. Atk in sun but loses HP each turn.',
  soundproof: 'Immune to sound-based moves.',
  'speed-boost': 'Raises Speed every turn.',
  static: 'Contact may paralyze the attacker.',
  steadfast: 'Raises Speed each time it flinches.',
  'storm-drain': 'Water moves miss and raise Sp. Atk instead.',
  sturdy: 'Survives a one-hit KO from full HP; blocks OHKO moves.',
  'sweet-veil': 'Cannot fall asleep.',
  'swift-swim': 'Doubles Speed in rain.',
  technician: 'Boosts moves of 60 power or less.',
  'thick-fat': 'Halves damage from Fire and Ice moves.',
  'tinted-lens': 'Doubles damage of not-very-effective hits.',
  'vital-spirit': 'Cannot fall asleep.',
  'volt-absorb': 'Heals when hit by an Electric move.',
  'water-absorb': 'Heals when hit by a Water move.',
  'water-veil': 'Cannot be burned.',
};

function prettyName(slug) {
  return String(slug).split('-').map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(' ');
}

// defaultMovesFor returns a curated, sane 4-move set for a species (STAB +
// coverage + a utility move). Falls back to the first four learnset moves
// only when the generated table is missing the species.
function defaultMovesFor(dex) {
  if (typeof CURATED_SETS !== 'undefined' && CURATED_SETS[dex]) {
    const curated = CURATED_SETS[dex].filter((id) => App.moveById[id]);
    if (curated.length) return curated.slice(0, 4);
  }
  const sp = App.dexByNo[dex];
  if (!sp || !sp.moves) return [];
  return sp.moves.slice(0, 4).map((m) => m.id);
}

function firstEmptySlot(state, dex) {
  const mv = state.moves[dex] || [];
  for (let i = 0; i < 4; i++) if (!mv[i]) return i;
  return Math.min(mv.length, 3);
}

// ---- training spread ----
//
// The helpers below mirror the backend's absent-means-default contract so the
// UI never has to write a value just to read one back.

const ZERO_EVS = { hp: 0, atk: 0, def: 0, spatk: 0, spdef: 0, speed: 0 };

function evsOf(state, dex) { return Object.assign({}, ZERO_EVS, state.evs[dex] || {}); }
function ivsOf(state, dex) {
  const max = App.rules.iv_max;
  const dflt = { hp: max, atk: max, def: max, spatk: max, spdef: max, speed: max };
  return Object.assign(dflt, state.ivs[dex] || {});
}
function natureOf(state, dex) { return App.natureById[state.nature[dex] || ''] || null; }
function evTotal(evs) { return STAT_KEYS.reduce((n, [k]) => n + (evs[k] || 0), 0); }

// natureRatio returns [numerator, denominator] for a nature's effect on one
// stat — the same exact-integer ratio the engine uses, so this preview and the
// battle agree. See docs/battle-state.md.
function natureRatio(nature, key) {
  if (!nature || !nature.plus || nature.plus === nature.minus) return [1, 1];
  if (nature.plus === key) return [11, 10];
  if (nature.minus === key) return [9, 10];
  return [1, 1];
}

// derivedStat mirrors engine.calcStat / engine.calcHP. Every division floors,
// and the nature applies last — reordering it is the classic way to be off by
// one. This is a preview; the server derives the real numbers from the
// submitted pick. tools/check-stat-preview.js and
// engine.TestStatPreviewChecksum guard the two against drifting apart.
function derivedStat(key, base, iv, ev, nature) {
  const L = App.rules.level;
  const raw = Math.floor((2 * base + iv + Math.floor(ev / 4)) * L / 100);
  if (key === 'hp') return raw + L + 10;
  const [num, den] = natureRatio(nature, key);
  return Math.floor((raw + 5) * num / den);
}

// spreadSummary renders a one-line description of a Pokémon's training for the
// roster card: nature plus the invested stats, biggest first.
function spreadSummary(state, dex) {
  const nature = natureOf(state, dex);
  const evs = evsOf(state, dex);
  const invested = STAT_KEYS
    .filter(([k]) => evs[k] > 0)
    .sort((a, b) => evs[b[0]] - evs[a[0]])
    .map(([k, label]) => `${evs[k]} ${label}`);
  const parts = [];
  if (nature && nature.plus) parts.push(nature.name);
  if (invested.length) parts.push(invested.join(' / '));
  return parts.length ? parts.join(' · ') : 'untrained';
}

// seedMon attaches the curated moveset and the default ability the first
// time a species joins a team.
function seedMon(state, dex) {
  if (!state.moves[dex]) state.moves[dex] = defaultMovesFor(dex);
  if (!state.ability[dex]) {
    const sp = App.dexByNo[dex];
    state.ability[dex] = sp && sp.abilities && sp.abilities[0] ? sp.abilities[0] : '';
  }
}

function boostsText(b) {
  return Object.entries(b)
    .map(([k, v]) => `${v > 0 ? '+' : ''}${v} ${STAT_SHORT[k] || prettyName(k)}`)
    .join('/');
}

// moveEffectText distills a move's mechanical rider into one short clause —
// the thing a dropdown never told you. Empty for a vanilla attack.
function moveEffectText(m) {
  if (!m) return '';
  const parts = [];
  if (m.priority > 0) parts.push(`+${m.priority} priority`);
  if (m.priority < 0) parts.push(`${m.priority} priority`);
  const flags = m.flags || [];
  if (flags.includes('two-turn')) parts.push('charges a turn');
  if (flags.includes('recharge')) parts.push('then must recharge');
  if (flags.includes('selfdestruct')) parts.push('user faints');
  const p = m.primary;
  if (p) {
    if (p.status) parts.push(STATUS_LABELS[p.status] || p.status);
    if (p.volatile) parts.push(VOLATILE_LABELS[p.volatile] || prettyName(p.volatile));
    if (p.boosts) parts.push(boostsText(p.boosts));
    if (p.heal) parts.push(`heal ${Math.round(p.heal * 100)}%`);
  }
  if (m.side_condition) parts.push(SIDECOND_LABELS[m.side_condition] || prettyName(m.side_condition));
  if (m.pseudo_weather) parts.push(prettyName(m.pseudo_weather));
  if (m.weather) parts.push(prettyName(m.weather));
  if (m.terrain) parts.push(`${prettyName(m.terrain)} Terrain`);
  if (m.slot_condition) parts.push(prettyName(m.slot_condition));
  if (m.self) {
    if (m.self.recoil) parts.push(`${Math.round(m.self.recoil * 100)}% recoil`);
    if (m.self.drain) parts.push(`drains ${Math.round(m.self.drain * 100)}%`);
    if (m.self.boosts) parts.push(boostsText(m.self.boosts));
  }
  (m.secondaries || []).forEach((s) => {
    const bits = [];
    if (s.status) bits.push(STATUS_LABELS[s.status] || s.status);
    if (s.volatile) bits.push(VOLATILE_LABELS[s.volatile] || prettyName(s.volatile));
    if (s.boosts) bits.push(boostsText(s.boosts));
    if (bits.length) parts.push(`${s.chance || 100}% ${bits.join('/')}`);
  });
  if (m.min_hits) parts.push(`${m.min_hits}-${m.max_hits || m.min_hits} hits`);
  if (m.self_switch) parts.push('switches out');
  if (m.force_switch) parts.push('forces switch');
  if (m.ohko) parts.push('one-hit KO');
  if (m.thaws_target) parts.push('thaws target');
  return parts.join(', ');
}

// ---- ctx factories ----
function setupCtx() {
  const state = App.setupSide === 'your' ? App.your : App.opp;
  return { rail: 'setup-rail', pane: 'setup-pane', state, locked: () => false, onChange: syncSetupBar };
}
function pickerCtx() {
  return {
    rail: 'picker-rail', pane: 'picker-pane', state: App.pick,
    locked: () => App.pickerSubmitted, onChange: updatePickerSubmitButton,
  };
}

function renderBuilder(ctx) {
  buildRail(ctx);
  if (ctx.state.sel != null && App.dexByNo[ctx.state.sel]) buildEditor(ctx);
  else buildRoster(ctx);
  if (ctx.onChange) ctx.onChange();
}

// ---- left rail: the six team slots ----
function buildRail(ctx) {
  const { state } = ctx;
  const locked = ctx.locked();
  const rail = document.getElementById(ctx.rail);
  let html = state.team.map((dex) => {
    const sp = App.dexByNo[dex];
    const mv = state.moves[dex] || [];
    const ab = state.ability[dex] || (sp.abilities && sp.abilities[0]) || '';
    const itemName = itemLabel(state.item[dex]);
    const ready = mv.filter(Boolean).length >= 1;
    const chips = mv.filter(Boolean).map((id) => {
      const m = App.moveById[id];
      const c = m ? TYPE_COLORS[m.type] : 'var(--border)';
      return `<span class="mv-chip" style="border-color:${c};color:${c}">${esc(m ? m.name : id)}</span>`;
    }).join('') || '<span class="muted" style="font-size:11px">no moves</span>';
    return `<div class="slot-card ${state.sel === dex ? 'selected' : ''}" data-dex="${dex}">
      <img src="${spriteUrl(dex)}" alt=""/>
      <div class="who">
        <div class="nm">${esc(sp.name)}
          <span class="chip" style="background:${TYPE_COLORS[sp.type1]}">${sp.type1}</span>
          ${sp.type2 ? `<span class="chip" style="background:${TYPE_COLORS[sp.type2]}">${sp.type2}</span>` : ''}
        </div>
        <div class="ab">Ability: <b>${esc(prettyName(ab))}</b></div>
        <div class="ab">Item: <b>${esc(itemName)}</b></div>
        <div class="ab">Training: <b>${esc(spreadSummary(state, dex))}</b></div>
        <div class="slot-moves">${chips}</div>
      </div>
      <span class="slot-status ${ready ? 'ok' : 'warn'}">${ready ? '✓' : '…'}</span>
      ${locked ? '' : `<button class="rm" data-rm="${dex}" title="Remove">✕</button>`}
    </div>`;
  }).join('');
  for (let i = state.team.length; i < 6; i++) {
    html += locked
      ? '<div class="slot-empty muted">empty</div>'
      : `<button class="slot-empty" data-add="1">＋ Add Pokémon (${i + 1}/6)</button>`;
  }
  rail.innerHTML = html;
  if (locked) return;
  rail.querySelectorAll('.slot-card').forEach((c) => {
    c.onclick = (e) => {
      if (e.target.dataset.rm) return;
      state.sel = +c.dataset.dex;
      state.mslot = firstEmptySlot(state, state.sel);
      state.mq = '';
      renderBuilder(ctx);
    };
  });
  rail.querySelectorAll('[data-rm]').forEach((b) => {
    b.onclick = () => {
      const dex = +b.dataset.rm;
      state.team = state.team.filter((d) => d !== dex);
      if (state.sel === dex) state.sel = null;
      renderBuilder(ctx);
    };
  });
  rail.querySelectorAll('[data-add]').forEach((b) => {
    b.onclick = () => { state.sel = null; renderBuilder(ctx); };
  });
}

// ---- right pane: roster grid ----
// buildRoster paints the search shell once; typing only repaints the results
// via renderRosterList, so the search box element survives and keeps focus.
function buildRoster(ctx) {
  const { state } = ctx;
  const pane = document.getElementById(ctx.pane);
  const types = Object.keys(TYPE_COLORS);
  pane.innerHTML = `
    <div class="filter-row">
      <input type="search" id="${ctx.pane}-q" placeholder="Search Pokémon…" value="${esc(state.q)}"/>
      <span class="muted" id="${ctx.pane}-count"></span>
    </div>
    <div class="type-filters">${types.map((t) =>
      `<button class="tchip ${state.typeFilter === t ? 'on' : ''}" data-t="${t}" style="background:${TYPE_COLORS[t]}">${t}</button>`).join('')}
    </div>
    <div class="roster" id="${ctx.pane}-roster"></div>`;
  const q = pane.querySelector(`#${ctx.pane}-q`);
  q.oninput = () => { state.q = q.value.toLowerCase(); renderRosterList(ctx); };
  pane.querySelectorAll('.tchip').forEach((b) => {
    b.onclick = () => { state.typeFilter = state.typeFilter === b.dataset.t ? null : b.dataset.t; buildRoster(ctx); };
  });
  renderRosterList(ctx);
}

// renderRosterList repaints only the count + grid (not the search box) so the
// input keeps focus across keystrokes.
function renderRosterList(ctx) {
  const { state } = ctx;
  const pane = document.getElementById(ctx.pane);
  const list = App.pokedex.filter((sp) => {
    if (state.q && !sp.name.toLowerCase().includes(state.q)) return false;
    if (state.typeFilter && sp.type1 !== state.typeFilter && sp.type2 !== state.typeFilter) return false;
    return true;
  });
  pane.querySelector(`#${ctx.pane}-count`).textContent = `${list.length} of ${App.pokedex.length}`;
  const roster = pane.querySelector(`#${ctx.pane}-roster`);
  roster.innerHTML = list.map((sp) => {
    const bst = STAT_KEYS.reduce((n, [k]) => n + sp.base[k], 0);
    const picked = state.team.includes(sp.dex_no);
    return `<div class="mon ${picked ? 'picked' : ''}" data-dex="${sp.dex_no}" title="Base stat total ${bst}">
      <img src="${spriteUrl(sp.dex_no)}" loading="lazy" alt=""/>
      <div class="name">${esc(sp.name)}</div>
      <div class="types"><span class="chip" style="background:${TYPE_COLORS[sp.type1]}">${sp.type1}</span>
        ${sp.type2 ? `<span class="chip" style="background:${TYPE_COLORS[sp.type2]}">${sp.type2}</span>` : ''}</div>
      <div class="bst">BST <b>${bst}</b> · Spe ${sp.base.speed}</div>
    </div>`;
  }).join('');
  roster.querySelectorAll('.mon').forEach((c) => {
    c.onclick = () => {
      if (ctx.locked()) return;
      const dex = +c.dataset.dex;
      if (state.team.includes(dex)) { state.sel = dex; state.mslot = firstEmptySlot(state, dex); renderBuilder(ctx); return; }
      if (state.team.length >= 6) { toast('A team can hold at most 6 Pokémon'); return; }
      state.team.push(dex);
      seedMon(state, dex);
      state.sel = dex; state.mslot = 0; state.mq = '';
      renderBuilder(ctx);
    };
  });
}

// ---- right pane: per-Pokémon editor ----
function buildEditor(ctx) {
  const { state } = ctx;
  const locked = ctx.locked();
  const pane = document.getElementById(ctx.pane);
  const sp = App.dexByNo[state.sel];
  const mv = state.moves[state.sel] || [];
  const curAb = state.ability[state.sel] || (sp.abilities && sp.abilities[0]) || '';
  const curItem = state.item[state.sel] || '';
  const MAXSTAT = 170;

  const bars = STAT_KEYS.map(([k, label]) => {
    const v = sp.base[k];
    const hue = v >= 110 ? 'var(--good)' : v >= 80 ? '#eab308' : 'var(--bad)';
    return `<span>${label}</span><span class="sbar"><i style="width:${Math.min(100, v / MAXSTAT * 100)}%;background:${hue}"></i></span><span>${v}</span>`;
  }).join('');

  const curNature = natureOf(state, state.sel);
  const natureLabel = curNature ? curNature.name : '';

  const abilities = sp.abilities || [];
  const abilityCards = abilities.length ? abilities.map((a, i) => {
    const functional = !!ABILITY_INFO[a];
    const desc = functional ? ABILITY_INFO[a] : 'No battle effect in this engine yet.';
    const tag = i === 0 ? 'default' : (abilities.length >= 3 && i === abilities.length - 1 ? 'hidden' : '');
    return `<button class="ab-card ${curAb === a ? 'on' : ''} ${functional ? '' : 'cosmetic'}" data-ab="${esc(a)}"${locked ? ' disabled' : ''}>
      <div class="ab-nm">${esc(prettyName(a))}
        ${tag ? `<span class="ab-tag">${tag}</span>` : ''}
        ${functional ? '' : '<span class="ab-tag flat">cosmetic</span>'}</div>
      <div class="ab-desc">${esc(desc)}</div>
    </button>`;
  }).join('') : '<span class="muted">No abilities listed for this species.</span>';

  const slots = [0, 1, 2, 3].map((i) => {
    const id = mv[i];
    if (!id) return `<button class="mslot empty ${state.mslot === i ? 'active' : ''}" data-ms="${i}">＋ slot ${i + 1}</button>`;
    const m = App.moveById[id];
    return `<button class="mslot ${state.mslot === i ? 'active' : ''}" data-ms="${i}">
      <div class="ms-nm">${esc(m ? m.name : id)}</div>
      <div class="ms-meta">${m ? `<span class="chip" style="background:${TYPE_COLORS[m.type]}">${m.type}</span>
        <span class="cat ${m.category}">${m.category.slice(0, 4)}</span>${m.power ? ` ${m.power}` : ''}` : ''}</div>
    </button>`;
  }).join('');

  pane.innerHTML = `
    <button class="back-link" id="${ctx.pane}-back">← back to roster</button>
    <div class="ed-head">
      <img src="${spriteUrl(sp.dex_no)}" alt=""/>
      <div class="title">
        <h3>${esc(sp.name)}
          <span class="chip" style="background:${TYPE_COLORS[sp.type1]}">${sp.type1}</span>
          ${sp.type2 ? `<span class="chip" style="background:${TYPE_COLORS[sp.type2]}">${sp.type2}</span>` : ''}
        </h3>
        <div class="muted" style="margin-top:4px">#${String(sp.dex_no).padStart(3, '0')}</div>
      </div>
      <div class="statbars">${bars}</div>
    </div>
    <div class="ed-section">
      <h4>Ability</h4>
      <div class="ability-cards">${abilityCards}</div>
    </div>
    <div class="ed-section">
      <h4>Held item — one per Pokémon${curItem ? `, currently <b>${esc(itemLabel(curItem))}</b>` : ', currently none'}</h4>
      <div class="ed-search">
        <input type="search" id="${ctx.pane}-iq" placeholder="Filter items…" value="${esc(state.iq)}"${locked ? ' disabled' : ''}/>
      </div>
      <div class="item-cards" id="${ctx.pane}-itemlist"></div>
    </div>
    <div class="ed-section">
      <h4>Training — nature and EVs${natureLabel ? `, currently <b>${esc(natureLabel)}</b>` : ''}</h4>
      ${trainingHTML(ctx, sp, locked)}
    </div>
    <div class="ed-section">
      <h4>Moves — click a slot, then a move below (click a chosen move to remove)</h4>
      <div class="move-slots">${slots}</div>
    </div>
    <div class="ed-section">
      <div class="ed-search">
        <input type="search" id="${ctx.pane}-mq" placeholder="Filter learnset…" value="${esc(state.mq)}"/>
        ${locked ? '' : `<button class="mini" id="${ctx.pane}-smart">✨ Smart-fill</button>`}
      </div>
      <div class="move-list">
        <div class="move-list-head"><span>Move</span><span>Type</span><span>Cat</span><span style="text-align:right">Pwr</span><span style="text-align:right">Acc</span><span>Effect</span></div>
        <div id="${ctx.pane}-movelist"></div>
      </div>
    </div>`;

  pane.querySelector(`#${ctx.pane}-back`).onclick = () => { state.sel = null; renderBuilder(ctx); };
  const mq = pane.querySelector(`#${ctx.pane}-mq`);
  mq.oninput = () => { state.mq = mq.value.toLowerCase(); renderLearnList(ctx); };
  const iq = pane.querySelector(`#${ctx.pane}-iq`);
  if (iq) iq.oninput = () => { state.iq = iq.value.toLowerCase(); renderItemList(ctx); };
  renderItemList(ctx);
  renderLearnList(ctx);
  if (locked) return; // every Training control is rendered disabled too
  wireTraining(ctx, pane, sp);
  pane.querySelectorAll('[data-ab]').forEach((b) => {
    b.onclick = () => { state.ability[state.sel] = b.dataset.ab; renderBuilder(ctx); };
  });
  pane.querySelectorAll('[data-ms]').forEach((b) => {
    b.onclick = () => { state.mslot = +b.dataset.ms; buildEditor(ctx); };
  });
  const smart = pane.querySelector(`#${ctx.pane}-smart`);
  if (smart) smart.onclick = () => { state.moves[state.sel] = defaultMovesFor(state.sel); renderBuilder(ctx); };
}

// trainingHTML renders the nature picker, the EV allocator, and the IV
// disclosure for the open Pokémon. One row per stat so the base value, the
// investment, and the resulting number sit on the same line — the point of an
// EV editor is watching the last column move.
//
// IVs are behind a <details> because the honest default (31 everywhere) is
// right for almost everyone; the two real uses are minimising Speed for Trick
// Room and minimising Attack against confusion and Foul Play.
function trainingHTML(ctx, sp, locked) {
  const { state } = ctx;
  const dex = state.sel;
  if (!App.natures.length) {
    return '<span class="muted">Nature table unavailable — this Pokémon will battle untrained.</span>';
  }
  const evs = evsOf(state, dex);
  const ivs = ivsOf(state, dex);
  const nature = natureOf(state, dex);
  const dis = locked ? ' disabled' : '';

  const opts = ['<option value="">Neutral (no effect)</option>'].concat(
    App.natures.map((n) => {
      const eff = n.plus ? `+${statLabel(n.plus)} / −${statLabel(n.minus)}` : 'no effect';
      const sel = state.nature[dex] === n.id ? ' selected' : '';
      return `<option value="${esc(n.id)}"${sel}>${esc(n.name)} — ${esc(eff)}</option>`;
    })
  ).join('');

  const rows = STAT_KEYS.map(([k, label]) => {
    const [num] = natureRatio(nature, k);
    const mark = num === 11 ? '<span class="nat-up">▲</span>'
      : num === 9 ? '<span class="nat-down">▼</span>' : '<span class="nat-flat"></span>';
    return `<span class="tr-lbl">${label}${mark}</span>
      <span class="tr-base">${sp.base[k]}</span>
      <input type="range" class="tr-range" data-ev="${k}" min="0" max="${App.rules.ev_max_per_stat}" step="4" value="${evs[k]}"${dis}/>
      <input type="number" class="tr-num" data-ev="${k}" min="0" max="${App.rules.ev_max_per_stat}" step="4" value="${evs[k]}"${dis}/>
      <span class="tr-out" data-out="${k}">${derivedStat(k, sp.base[k], ivs[k], evs[k], nature)}</span>`;
  }).join('');

  const ivRows = STAT_KEYS.map(([k, label]) => `<label class="iv-cell">${label}
      <input type="number" class="tr-num" data-iv="${k}" min="0" max="${App.rules.iv_max}" value="${ivs[k]}"${dis}/>
    </label>`).join('');

  return `
    <div class="train-head">
      <label class="train-nature">Nature
        <select data-nature="1"${dis}>${opts}</select>
      </label>
      <span class="ev-budget" data-budget="1">${evBudgetText(evs)}</span>
    </div>
    <div class="train-grid">
      <span class="tr-head">Stat</span><span class="tr-head">Base</span>
      <span class="tr-head">EVs</span><span class="tr-head"></span><span class="tr-head" style="text-align:right">Total</span>
      ${rows}
    </div>
    <details class="iv-details">
      <summary>IVs — advanced, default ${App.rules.iv_max}</summary>
      <div class="iv-grid">${ivRows}</div>
    </details>`;
}

function statLabel(key) {
  const hit = STAT_KEYS.find(([k]) => k === key);
  return hit ? hit[1] : key;
}

function evBudgetText(evs) {
  const used = evTotal(evs);
  return `EVs ${used} / ${App.rules.ev_max_total}${used > App.rules.ev_max_total ? ' — over budget' : ''}`;
}

// wireTraining attaches the Training-section handlers. Inputs update state and
// then repaint only the derived numbers and the budget readout — never their
// own markup, which would drop focus mid-keystroke. Same split as
// renderItemList / renderLearnList.
function wireTraining(ctx, pane, sp) {
  const { state } = ctx;
  const dex = state.sel;
  const budgetEl = pane.querySelector('[data-budget]');
  if (!budgetEl) return; // nature table unavailable — nothing rendered

  const refresh = () => {
    const evs = evsOf(state, dex);
    const ivs = ivsOf(state, dex);
    const nature = natureOf(state, dex);
    STAT_KEYS.forEach(([k]) => {
      const out = pane.querySelector(`[data-out="${k}"]`);
      if (out) out.textContent = derivedStat(k, sp.base[k], ivs[k], evs[k], nature);
    });
    budgetEl.textContent = evBudgetText(evs);
    budgetEl.classList.toggle('over', evTotal(evs) > App.rules.ev_max_total);
  };

  const sel = pane.querySelector('[data-nature]');
  if (sel) {
    sel.onchange = () => {
      state.nature[dex] = sel.value;
      // A nature change also moves the ▲/▼ markers and the roster summary, so
      // this one repaints properly. A select doesn't lose a keystroke the way
      // a text input would.
      buildEditor(ctx);
      buildRail(ctx);
    };
  }

  // setEV clamps to the per-stat cap and to whatever is left of the budget,
  // then writes the clamped number back into both controls so the UI can never
  // show a value the server would reject.
  const setEV = (key, raw) => {
    const evs = evsOf(state, dex);
    let v = Number.isFinite(raw) ? Math.max(0, Math.min(App.rules.ev_max_per_stat, Math.trunc(raw))) : 0;
    const others = evTotal(evs) - evs[key];
    v = Math.min(v, Math.max(0, App.rules.ev_max_total - others));
    evs[key] = v;
    state.evs[dex] = evs;
    pane.querySelectorAll(`[data-ev="${key}"]`).forEach((el) => { el.value = v; });
    refresh();
  };

  pane.querySelectorAll('[data-ev]').forEach((el) => {
    el.oninput = () => setEV(el.dataset.ev, parseInt(el.value, 10));
    el.onchange = () => buildRail(ctx); // roster summary catches up on release
  });

  pane.querySelectorAll('[data-iv]').forEach((el) => {
    el.oninput = () => {
      const ivs = ivsOf(state, dex);
      const v = Math.max(0, Math.min(App.rules.iv_max, parseInt(el.value, 10) || 0));
      ivs[el.dataset.iv] = v;
      state.ivs[dex] = ivs;
      el.value = v;
      refresh();
    };
  });
}

// itemLabel renders a held-item slug for display. An unknown slug (a catalog
// that shrank under a running tab) falls back to the prettified slug rather
// than showing nothing, so a stale pick is visible instead of silent.
function itemLabel(id) {
  if (!id) return 'none';
  const it = App.itemById[id];
  return it ? it.name : prettyName(id);
}

// renderItemList repaints only the item cards (not the filter box) so the
// filter input keeps focus across keystrokes — same split as renderLearnList.
// The currently held item is always rendered, even when the filter excludes
// it, so the selection can never scroll out of reach of the "deselect" click.
function renderItemList(ctx) {
  const { state } = ctx;
  const locked = ctx.locked();
  const pane = document.getElementById(ctx.pane);
  const listEl = pane.querySelector(`#${ctx.pane}-itemlist`);
  if (!listEl) return;
  const cur = state.item[state.sel] || '';
  if (!App.items.length) {
    listEl.innerHTML = '<span class="muted">Item catalog unavailable — this Pokémon will hold nothing.</span>';
    return;
  }
  const shown = App.items.filter((it) => it.id === cur || !state.iq
    || it.name.toLowerCase().includes(state.iq) || it.desc.toLowerCase().includes(state.iq));
  const none = `<button class="ab-card ${cur ? '' : 'on'}" data-item=""${locked ? ' disabled' : ''}>
    <div class="ab-nm">No item</div>
    <div class="ab-desc">This Pokémon holds nothing.</div>
  </button>`;
  const cards = shown.map((it) => {
    const modeled = !!it.desc;
    return `<button class="ab-card ${cur === it.id ? 'on' : ''} ${modeled ? '' : 'cosmetic'}" data-item="${esc(it.id)}"${locked ? ' disabled' : ''}>
      <div class="ab-nm">${esc(it.name)}${modeled ? '' : '<span class="ab-tag flat">cosmetic</span>'}</div>
      <div class="ab-desc">${esc(modeled ? it.desc : 'No battle effect in this engine yet.')}</div>
    </button>`;
  }).join('');
  listEl.innerHTML = none + (cards || '<span class="muted">No item matches that filter.</span>');
  if (locked) return;
  listEl.querySelectorAll('[data-item]').forEach((b) => {
    b.onclick = () => {
      // Clicking the held item again clears it, mirroring how clicking a
      // chosen move removes it from its slot.
      const id = b.dataset.item;
      state.item[state.sel] = (id && id !== cur) ? id : '';
      renderBuilder(ctx);
    };
  });
}

// renderLearnList repaints only the learnset rows (not the filter box) so the
// filter input keeps focus across keystrokes.
function renderLearnList(ctx) {
  const { state } = ctx;
  const pane = document.getElementById(ctx.pane);
  const sp = App.dexByNo[state.sel];
  const mv = state.moves[state.sel] || [];
  const learn = (sp.moves || [])
    .filter((m) => !state.mq || m.name.toLowerCase().includes(state.mq))
    .slice()
    .sort((a, b) => (b.power || 0) - (a.power || 0));
  const listEl = pane.querySelector(`#${ctx.pane}-movelist`);
  listEl.innerHTML = learn.map((m) => {
    const inset = mv.includes(m.id);
    return `<div class="move-row ${inset ? 'inset' : ''}" data-mid="${esc(m.id)}">
      <span class="mr-nm">${esc(m.name)}${inset ? ' ✓' : ''}</span>
      <span class="chip" style="background:${TYPE_COLORS[m.type]};justify-self:start">${m.type}</span>
      <span class="cat ${m.category}">${m.category}</span>
      <span class="num">${m.power || '—'}</span>
      <span class="num">${m.accuracy || '—'}</span>
      <span class="mr-eff">${esc(moveEffectText(m))}</span>
    </div>`;
  }).join('');
  if (ctx.locked()) return;
  listEl.querySelectorAll('[data-mid]').forEach((r) => {
    r.onclick = () => {
      const id = r.dataset.mid;
      const cur = (state.moves[state.sel] || []).slice();
      const at = cur.indexOf(id);
      if (at >= 0) cur.splice(at, 1);          // toggle a chosen move off
      else cur[state.mslot] = id;              // place into the armed slot
      const out = cur.filter(Boolean).slice(0, 4);
      state.moves[state.sel] = out;
      state.mslot = firstEmptySlot(state, state.sel);
      renderBuilder(ctx);
    };
  });
}

// randomizeBuilder fills a surface with six random species, each seeded
// with its curated set. smartFill only resets movesets of the current team.
function randomizeBuilder(ctx) {
  if (ctx.locked()) return;
  const pool = App.pokedex.map((p) => p.dex_no);
  const team = [];
  while (team.length < 6 && pool.length) {
    team.push(pool.splice(Math.floor(Math.random() * pool.length), 1)[0]);
  }
  ctx.state.team = team;
  team.forEach((dex) => { ctx.state.moves[dex] = defaultMovesFor(dex); seedMon(ctx.state, dex); });
  ctx.state.sel = null;
  renderBuilder(ctx);
}
function smartFillBuilder(ctx) {
  if (ctx.locked()) return;
  ctx.state.team.forEach((dex) => { ctx.state.moves[dex] = defaultMovesFor(dex); });
  renderBuilder(ctx);
}

// ---- setup-page builder wiring ----
function renderSetup() { renderBuilder(setupCtx()); }

// syncSetupBar refreshes the side tabs' counts and the Start button.
function syncSetupBar() {
  const yc = document.getElementById('your-count');
  const oc = document.getElementById('opp-count');
  if (yc) yc.textContent = `${App.your.team.length}/6`;
  if (oc) oc.textContent = `${App.opp.team.length}/6`;
}

// ---- pokedex view ----
function renderPokedex() {
  document.getElementById('pokedex-grid').innerHTML = App.pokedex.map((p) => {
    const t2 = p.type2
      ? `<span class="chip" style="background:${TYPE_COLORS[p.type2]}">${p.type2}</span>` : '';
    const b = p.base;
    const bst = b.hp + b.atk + b.def + b.spatk + b.spdef + b.speed;
    const abilities = (p.abilities && p.abilities.length)
      ? `<div class="dex">Ability: <b>${esc(p.abilities[0])}</b>${
          p.abilities.length > 1 ? ` <span class="muted">(alt: ${p.abilities.slice(1).map(esc).join(', ')})</span>` : ''
        }</div>`
      : '';
    return `<div class="mon" title="Base stat total ${bst}">
      <img src="${spriteUrl(p.dex_no)}" alt="${esc(p.name)}" loading="lazy"/>
      <div class="name">${esc(p.name)}</div>
      <div class="types"><span class="chip" style="background:${TYPE_COLORS[p.type1]}">${p.type1}</span>${t2}</div>
      <div class="dex">#${String(p.dex_no).padStart(3, '0')} · BST ${bst}</div>
      ${abilities}
    </div>`;
  }).join('');
}

// ---- leaderboard ----
async function loadLeaderboard() {
  const tbody = document.querySelector('#lb-table tbody');
  tbody.innerHTML = '<tr><td colspan="3" class="muted">Loading…</td></tr>';
  try {
    const rows = await api('/api/leaderboard');
    tbody.innerHTML = rows.length
      ? rows.map((r, i) => `<tr><td>${i + 1}</td><td>${esc(r.name)}</td><td>${r.rating}</td></tr>`).join('')
      : '<tr><td colspan="3" class="muted">No battles yet — go win some.</td></tr>';
  } catch (e) {
    tbody.innerHTML = `<tr><td colspan="3" class="muted">${esc(e.message)}</td></tr>`;
  }
}

// ---- start a battle ----
async function startBattle() {
  const mode = document.getElementById('mode').value;
  // Quicksim is the only mode where teams are required at create time —
  // both sides are AIs, so the engine state is built before any picker
  // round-trip. Live and live_pvp pick in the dedicated picker view.
  if (mode === 'quicksim') {
    if (!App.your.team.length) { toast('Pick at least one Pokémon for your team'); return; }
    if (!App.opp.team.length) { toast('Pick at least one Pokémon for the opponent'); return; }
  }

  const name = document.getElementById('player-name').value.trim() || 'Challenger';
  // agent_vs_agent is a UI framing on top of live_pvp — backend has no separate
  // mode because the protocol is identical (two external joiners, no AI). We
  // just present the URLs differently and drop the user into spectate.
  const backendMode = mode === 'agent_vs_agent' ? 'live_pvp' : mode;
  const body = {
    mode: backendMode,
    p1_name: mode === 'agent_vs_agent' ? 'Agent 1' : name,
    p2_name: mode === 'live' ? 'AI'
      : mode === 'live_pvp' ? 'Opponent'
      : mode === 'agent_vs_agent' ? 'Agent 2'
      : 'Rival',
  };
  if (mode === 'quicksim') {
    // Dex-number arrays remain the persisted battle record; the *_picks
    // carry the per-Pokémon movesets and abilities the user chose so the
    // quicksim honors them (the worker uses picks when present, else falls
    // back to default movesets from the bare dex list).
    body.p1_team = App.your.team;
    body.p2_team = App.opp.team;
    body.p1_picks = picksFromState(App.your);
    body.p2_picks = picksFromState(App.opp);
  }

  const btn = document.getElementById('start-battle');
  btn.disabled = true;
  try {
    const res = await api('/api/battles', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (mode === 'agent_vs_agent') {
      enterAgentVsAgent(res);
    } else if (mode === 'live' || mode === 'live_pvp') {
      enterPicker(res, mode, name);
    } else {
      enterArena(res, mode, name);
    }
  } catch (e) {
    toast('Could not start battle: ' + e.message);
  }
  btn.disabled = false;
}

function enterArena(res, mode, name) {
  showView('arena');
  document.getElementById('battle-log').innerHTML = '';
  document.getElementById('controls').innerHTML = '';
  document.getElementById('opp-platform').innerHTML = '';
  document.getElementById('you-platform').innerHTML = '';
  document.getElementById('result-banner').classList.add('hidden');
  document.getElementById('share-banner').classList.add('hidden');
  App.battle = {
    id: res.battle_id, mode, name, view: 'arena',
    queue: [], playing: false, ended: false, state: null, ws: null, es: null,
  };
  document.getElementById('arena-label').textContent = `Quick Sim · ${name} vs Rival — spectating`;
  logLine({ type: 'turn', text: 'Battle starting…' });
  spectate(res.battle_id);
}

// enterAgentVsAgent kicks off a live_pvp battle whose two slots are both
// filled by external MCP agents. The human running the SPA isn't a player —
// they paste each share URL into a separate MCP client (Claude Code, etc.)
// and watch via the spectator view. The agents pick teams, choose moves,
// and run to completion entirely through MCP tools (join_battle → submit_team
// → wait/view/act loop).
function enterAgentVsAgent(res) {
  const battleId = res.battle_id;
  const share = (wsUrl, slot) => {
    const u = new URL(wsUrl, location.origin);
    const tok = u.searchParams.get('token');
    return `${location.origin}/?battle=${battleId}&slot=${slot}&token=${encodeURIComponent(tok)}`;
  };
  const p1Share = share(res.p1_url, 'p1');
  const p2Share = share(res.p2_url, 'p2');

  enterSpectate(battleId).then(() => {
    const banner = document.getElementById('share-banner');
    banner.classList.remove('hidden');
    banner.innerHTML = `
      <div class="agent-pair">
        <div class="agent-share">
          <span class="share-title">Agent 1 (slot p1)</span>
          <code class="share-url" id="ava-p1-url">${esc(p1Share)}</code>
          <button class="mini" id="ava-copy-p1">Copy</button>
        </div>
        <div class="agent-share">
          <span class="share-title">Agent 2 (slot p2)</span>
          <code class="share-url" id="ava-p2-url">${esc(p2Share)}</code>
          <button class="mini" id="ava-copy-p2">Copy</button>
        </div>
        <div class="share-hint">
          Paste each URL into a separate MCP client (e.g. Claude Code) and ask it to
          play that slot. Both agents will draft teams and battle while you watch below.
        </div>
      </div>`;
    const copy = (url) => navigator.clipboard.writeText(url).then(
      () => toast('Copied!'),
      () => toast('Copy failed — select the URL and copy manually'),
    );
    document.getElementById('ava-copy-p1').onclick = () => copy(p1Share);
    document.getElementById('ava-copy-p2').onclick = () => copy(p2Share);
  });
}

// enterSpectate is the read-only entry point: any battle (live, live_pvp, or
// quicksim) can be watched by deeplinking with ?spectate=<id>. We reuse the
// existing arena view; updateControls already renders a non-playable message
// for modes that aren't 'live' or 'live_pvp', so the action panel stays empty.
async function enterSpectate(battleId) {
  showView('arena');
  document.getElementById('battle-log').innerHTML = '';
  document.getElementById('controls').innerHTML = '';
  document.getElementById('opp-platform').innerHTML = '';
  document.getElementById('you-platform').innerHTML = '';
  document.getElementById('result-banner').classList.add('hidden');
  document.getElementById('share-banner').classList.add('hidden');
  App.battle = {
    id: battleId, mode: 'spectate', name: 'Spectator', view: 'arena',
    queue: [], playing: false, ended: false, state: null, ws: null, es: null,
  };
  let label = `Spectating · battle ${battleId.slice(0, 8)}`;
  try {
    const data = await api('/api/battles/' + battleId);
    if (data && data.battle) {
      label = `Spectating · ${data.battle.p1_name} vs ${data.battle.p2_name}`;
    }
  } catch (e) { /* keep default label if metadata fetch fails */ }
  document.getElementById('arena-label').textContent = label;
  logLine({ type: 'turn', text: 'Connecting to battle…' });
  spectate(battleId);
}

function leaveArena() {
  if (App.battle) {
    App.battle.ended = true;
    if (App.battle.ws) try { App.battle.ws.close(); } catch (e) { /* */ }
    if (App.battle.es) try { App.battle.es.close(); } catch (e) { /* */ }
  }
  App.battle = null;
  showView('setup');
}

// ---- picker view ----
//
// Both live (vs AI) and live_pvp enter here after Start (and when joining
// via share URL). The picker owns its own team state — setup-page picks
// do not flow in. User assembles a team, clicks Submit, and the WS sends
// {type:"submit_team",picks}. On the next "state" frame, the picker
// transitions to the arena view.

// picksFromState projects a builder state into the wire shape both submit
// paths use: [{dex_no, moves:[id], ability?, item?, nature?, evs?, ivs?}].
// Every optional field is sent only when it differs from the backend's
// default — the ability when it isn't slot 0, the item when one is held, the
// nature when it isn't neutral, EVs when any are invested, IVs when any are
// below the maximum. A team nobody trained therefore serializes exactly the
// way it did before spreads existed.
function picksFromState(state) {
  return state.team.map((dex) => {
    const sp = App.dexByNo[dex];
    const moves = (state.moves[dex] && state.moves[dex].filter(Boolean).length)
      ? state.moves[dex].filter(Boolean).slice(0, 4)
      : defaultMovesFor(dex);
    const pick = { dex_no: dex, moves };
    const ab = state.ability[dex];
    if (ab && sp && sp.abilities && ab !== sp.abilities[0]) pick.ability = ab;
    const item = state.item[dex];
    if (item) pick.item = item;
    if (state.nature[dex]) pick.nature = state.nature[dex];
    const evs = evsOf(state, dex);
    if (evTotal(evs) > 0) pick.evs = evs;
    const ivs = ivsOf(state, dex);
    if (STAT_KEYS.some(([k]) => ivs[k] !== App.rules.iv_max)) pick.ivs = ivs;
    return pick;
  });
}

function enterPicker(res, mode, name) {
  // Reset picker state — start empty per the agreed UX.
  App.pick = newBuilderState();
  App.pickerSubmitted = false;
  if (App.pickerDeadlineTimer) { clearInterval(App.pickerDeadlineTimer); App.pickerDeadlineTimer = null; }

  App.battle = {
    id: res.battle_id, mode, name, view: 'picker',
    queue: [], playing: false, ended: false, state: null, ws: null, es: null,
  };

  showView('picker');
  document.getElementById('picker-label').textContent =
    mode === 'live_pvp' ? `Pv-Player · ${name} · drafting team` : `Live Battle · ${name} vs AI · drafting team`;
  document.querySelector('.picker-main').classList.remove('picker-locked');
  document.getElementById('picker-share-banner').classList.add('hidden');
  document.getElementById('picker-opp').innerHTML =
    '<h4>Opponent</h4><div class="muted">Connecting…</div>';
  renderBuilder(pickerCtx());

  if (mode === 'live_pvp') {
    showPickerShareBanner(res.battle_id, res.p2_url);
    connectPvPWS(res.p1_url);
  } else {
    connectWS(res.ws_url);
  }
}

function leavePicker() {
  if (App.battle) {
    App.battle.ended = true;
    if (App.battle.ws) try { App.battle.ws.close(); } catch (e) { /* */ }
  }
  if (App.pickerDeadlineTimer) { clearInterval(App.pickerDeadlineTimer); App.pickerDeadlineTimer = null; }
  App.battle = null;
  showView('setup');
}

// renderPicker re-renders the picker's shared builder. Kept as a thin named
// wrapper because the WS handlers call it after room/state frames.
function renderPicker() { renderBuilder(pickerCtx()); }

function updatePickerSubmitButton() {
  const count = document.getElementById('picker-count');
  if (count) count.textContent = `${App.pick.team.length}/6`;
  const btn = document.getElementById('picker-submit');
  if (App.pickerSubmitted) {
    btn.disabled = true; btn.textContent = '✓ Submitted'; return;
  }
  btn.disabled = App.pick.team.length !== 6;
  btn.textContent = App.pick.team.length === 6 ? 'Submit team ▶' : `Pick ${6 - App.pick.team.length} more`;
}

function submitPicker() {
  if (App.pickerSubmitted || App.pick.team.length !== 6) return;
  const ws = App.battle && App.battle.ws;
  if (!ws || ws.readyState !== WebSocket.OPEN) { toast('Not connected'); return; }
  const picks = picksFromState(App.pick);
  try {
    ws.send(JSON.stringify({ type: 'submit_team', picks }));
  } catch (e) {
    toast('Could not submit team: ' + e.message); return;
  }
  // Optimistic lock. The next room frame confirms (or a FrameError unlocks).
  App.pickerSubmitted = true;
  document.querySelector('.picker-main').classList.add('picker-locked');
  renderBuilder(pickerCtx()); // re-render read-only and refresh the button
}

function randomizePicker() { randomizeBuilder(pickerCtx()); }

// renderPickerOpp paints the opponent status card from a room frame.
// "them.attached=false" → "waiting to join"; attached but !submitted → "drafting";
// submitted → green ✓. The deadline countdown is driven by a setInterval that
// computes its own remainder from the room frame's deadline_ms — we don't trust
// the room frame to arrive every second.
function renderPickerOpp(room) {
  const them = room.them || { attached: false, submitted: false };
  let dotCls = 'opp-dot detached';
  let stateText = 'Waiting to join…';
  if (them.attached) {
    if (them.submitted) { dotCls = 'opp-dot submitted'; stateText = 'Submitted ✓'; }
    else { dotCls = 'opp-dot'; stateText = 'Drafting team…'; }
  }
  const trainer = them.trainer || 'Opponent';
  const el = document.getElementById('picker-opp');
  el.innerHTML = `
    <h4>Opponent</h4>
    <div class="opp-row"><span class="${dotCls}"></span><span>${esc(trainer)}</span></div>
    <div class="opp-status">${stateText}</div>
    <div class="opp-deadline" id="picker-deadline">—</div>
    <div class="opp-status">picker deadline</div>
  `;
  if (App.pickerDeadlineTimer) clearInterval(App.pickerDeadlineTimer);
  const endAt = Date.now() + (room.deadline_ms || 0);
  const tick = () => {
    const left = Math.max(0, endAt - Date.now());
    const node = document.getElementById('picker-deadline');
    if (!node) { clearInterval(App.pickerDeadlineTimer); App.pickerDeadlineTimer = null; return; }
    const mm = Math.floor(left / 60000);
    const ss = Math.floor((left % 60000) / 1000);
    node.textContent = `${mm}:${String(ss).padStart(2, '0')}`;
    node.classList.toggle('urgent', left <= 30000);
  };
  tick();
  App.pickerDeadlineTimer = setInterval(tick, 1000);
}

function showPickerShareBanner(battleId, p2WsUrl) {
  const u = new URL(p2WsUrl, location.origin);
  const token = u.searchParams.get('token');
  const pageUrl = `${location.origin}/?battle=${battleId}&slot=p2&token=${encodeURIComponent(token)}`;
  const banner = document.getElementById('picker-share-banner');
  banner.innerHTML = `
    <span class="share-title">🔗 Send this URL to your opponent:</span>
    <code class="share-url">${esc(pageUrl)}</code>
    <button class="mini" id="picker-copy-share">📋 Copy</button>
    <div class="share-hint">They join slot 2 and draft their own team alongside you.</div>`;
  banner.classList.remove('hidden');
  document.getElementById('picker-copy-share').onclick = () => {
    navigator.clipboard.writeText(pageUrl).then(
      () => toast('Copied!'),
      () => toast('Copy failed — select the URL and copy manually'),
    );
  };
}

// transitionPickerToArena flips the active view when the first "state"
// frame arrives — the gateway sends it once both teams are submitted and
// the engine state is built. The state frame's view is rendered by the
// caller via the existing renderBattle path.
function transitionPickerToArena() {
  if (!App.battle || App.battle.view === 'arena') return;
  if (App.pickerDeadlineTimer) { clearInterval(App.pickerDeadlineTimer); App.pickerDeadlineTimer = null; }
  App.battle.view = 'arena';
  showView('arena');
  document.getElementById('battle-log').innerHTML = '';
  document.getElementById('controls').innerHTML = '';
  document.getElementById('opp-platform').innerHTML = '';
  document.getElementById('you-platform').innerHTML = '';
  document.getElementById('result-banner').classList.add('hidden');
  document.getElementById('share-banner').classList.add('hidden');
  const mode = App.battle.mode;
  const label = mode === 'live_pvp'
    ? `Pv-Player · ${App.battle.name}`
    : `Live Battle · ${App.battle.name} vs AI`;
  document.getElementById('arena-label').textContent = label;
  logLine({ type: 'turn', text: 'Battle starting…' });
}

// ---- live battle over WebSocket ----
function connectWS(wsUrl) {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const ws = new WebSocket(`${proto}://${location.host}${wsUrl}`);
  App.battle.ws = ws;
  ws.onmessage = (ev) => {
    let msg;
    try { msg = JSON.parse(ev.data); } catch (e) { return; }
    handleWSMessage(msg);
  };
  ws.onerror = () => toast('Battle connection error');
}

function handleWSMessage(msg) {
  if (!App.battle) return;
  switch (msg.type) {
    case 'room': {
      // Live picker room. The server-side AI has already auto-submitted;
      // we render its status here and let the user click Submit when
      // their own team is ready.
      if (msg.room) renderPickerOpp(msg.room);
      break;
    }
    case 'state': {
      if (App.battle.view === 'picker') transitionPickerToArena();
      App.battle.state = viewToRenderableState(msg.view);
      renderBattle(App.battle.state);
      updateControls(App.battle.state);
      break;
    }
    case 'turn':
      App.battle.queue.push({ turn: { log: msg.log, state: viewToRenderableState(msg.view) } });
      playLoop();
      break;
    case 'ai':
      App.battle.queue.push({ ai: msg.reasoning });
      playLoop();
      break;
    case 'info':
      App.battle.queue.push({ info: msg.message });
      playLoop();
      break;
    case 'error':
      toast(msg.message);
      if (App.battle.state) updateControls(App.battle.state);
      break;
    case 'end': {
      if (!msg.view) { endAbandoned(); break; }
      App.battle.state = viewToRenderableState(msg.view);
      // Live mode: the human is always side 0, so winner=0 maps to
      // "you" without further normalization.
      App.battle.queue.push({ end: { winner: msg.winner, state: App.battle.state } });
      playLoop();
      break;
    }
  }
}

function sendAction(kind, index) {
  const b = App.battle;
  if (!b || !b.ws || b.ws.readyState !== WebSocket.OPEN) { toast('Not connected'); return; }
  // Echo the chosen action into the log immediately as INTENT, not outcome.
  // The controls panel is about to be replaced by "Resolving turn…" — without
  // this, the user's last action vanishes from the screen for ~400ms while
  // the engine resolves. Crucially we say "chose to use" (an intent), not
  // "used" (an outcome) — the move might still be cancelled by paralysis,
  // sleep, or freeze, and the engine's resolution will tell the true story.
  if (b.state) {
    const me = b.state.sides[0];
    const active = me.team[me.active];
    if (kind === 'move' && active && active.moves[index]) {
      const ms = active.moves[index];
      const mv = App.moveById[ms.move_id] || { name: ms.move_id, type: 'normal' };
      logLine({ type: 'choice', side: 0, text: '🎯 chose', chip: { name: mv.name, type: mv.type } });
    } else if (kind === 'switch' && me.team[index]) {
      logLine({ type: 'choice', side: 0, text: `🔄 sending in ${me.team[index].name}…` });
    }
  }
  b.ws.send(JSON.stringify({ type: 'action', kind, index }));
  document.getElementById('controls').innerHTML = '<div class="muted">Resolving turn…</div>';
}

// ---- quick sim spectating ----
function spectate(battleId) {
  App.battle.seen = new Set();
  let finished = false;

  const reconcile = async () => {
    if (finished || !App.battle) return;
    finished = true;
    if (App.battle.es) try { App.battle.es.close(); } catch (e) { /* */ }
    try {
      const data = await api('/api/battles/' + battleId);
      for (const t of data.turns || []) {
        if (!App.battle.seen.has(t.turn_no)) {
          App.battle.seen.add(t.turn_no);
          App.battle.queue.push({ turn: { log: t.log, state: t.state_digest } });
        }
      }
      App.battle.queue.push({ end: { winner: data.battle.winner } });
      playLoop();
    } catch (e) {
      toast('Could not load battle result: ' + e.message);
    }
  };

  const es = new EventSource('/api/battles/' + battleId + '/stream');
  App.battle.es = es;
  es.addEventListener('turn-resolved', (ev) => {
    if (!App.battle) return;
    let d;
    try { d = JSON.parse(ev.data); } catch (e) { return; }
    if (App.battle.seen.has(d.turn)) return;
    App.battle.seen.add(d.turn);
    App.battle.queue.push({ turn: { log: d.log, state: d.state } });
    playLoop();
  });
  es.addEventListener('battle-completed', reconcile);
  // EventSource fires onerror on a normal stream close too — reconcile via GET.
  es.onerror = () => setTimeout(reconcile, 400);
}

// ---- turn playback ----
function playLoop() {
  const b = App.battle;
  if (!b || b.playing) return;
  b.playing = true;
  (async () => {
    while (b.queue.length) {
      if (App.battle !== b) return; // user left the arena
      const item = b.queue.shift();
      if (item.ai !== undefined) {
        logLine({ type: 'ai', text: '🤖 ' + item.ai });
        await sleep(700);
      } else if (item.info !== undefined) {
        logLine({ type: 'turn', text: item.info });
        await sleep(450);
      } else if (item.end !== undefined) {
        await showResult(item.end);
      } else if (item.turn !== undefined) {
        await playTurn(item.turn);
      }
    }
    b.playing = false;
  })();
}

async function playTurn(msg) {
  if (msg.state) {
    App.battle.state = msg.state;
    renderBattle(msg.state);
  }
  const step = App.battle.mode === 'live' ? 260 : 470;
  for (const line of msg.log || []) {
    logLine(line);
    if (line.type === 'move') {
      // Play the attack animation, then a shortened beat so the effect (not a
      // full extra step) sets the turn's pacing.
      await playMoveEffect(line);
      await sleep(step * 0.4);
    } else {
      playLineDrama(line);
      await sleep(step);
    }
  }
  await sleep(260);
  if (msg.state && msg.state.phase !== 'ended') updateControls(msg.state);
}

// endAbandoned handles a terminal frame that carries no battle view — the room
// was abandoned before it ever became ACTIVE (the opponent never joined, or left
// the picker, so the deadline lapsed). There is no result to show; we move off
// the picker into the arena's terminal banner so the user isn't stranded waiting
// for an opponent who is never coming.
function endAbandoned() {
  if (!App.battle) return;
  if (App.battle.view === 'picker') transitionPickerToArena();
  App.battle.queue.push({ end: { abandoned: true } });
  playLoop();
}

async function showResult(end) {
  if (!App.battle) return;
  App.battle.ended = true;
  const banner = document.getElementById('result-banner');
  if (end.abandoned) {
    banner.className = 'win-draw';
    banner.textContent = '⚠️ The battle was abandoned — your opponent never joined.';
    banner.classList.remove('hidden');
    document.getElementById('controls').innerHTML =
      '<div class="muted">Battle closed. Head back to setup for another round.</div>';
    return;
  }
  const w = end.winner;
  let cls = 'win-draw';
  let text = '🤝 The battle ended in a draw.';
  if (App.battle.mode === 'live') {
    if (w === 0) { cls = 'win-you'; text = '🏆 Victory! You won the battle.'; }
    else if (w === 1) { cls = 'win-opp'; text = '💀 Defeat — the AI won this one.'; }
  } else if (App.battle.mode === 'live_pvp') {
    // For pvp the queue normalizer maps winner so 0 = you, 1 = opponent —
    // regardless of which slot you actually claimed on the server.
    if (w === 0) { cls = 'win-you'; text = '🏆 Victory! You won the battle.'; }
    else if (w === 1) { cls = 'win-opp'; text = '💀 Defeat — your opponent won.'; }
  } else if (w === 0 || w === 1) {
    const name = App.battle.state ? App.battle.state.sides[w].trainer : 'Side ' + w;
    cls = w === 0 ? 'win-you' : 'win-opp';
    text = '🏆 ' + name + ' wins!';
  }
  banner.className = cls;
  banner.textContent = text;
  banner.classList.remove('hidden');
  document.getElementById('controls').innerHTML =
    '<div class="muted">Battle complete. Head back to setup for another round.</div>';
}

// ---- move animations ----
// Effects are absolutely-positioned overlays appended to .battlefield (which is
// position:relative). renderBattle only runs at the top of a turn, so overlays
// spawned while the log narrates are never wiped mid-turn.

// fxGeom resolves the attacker/target sprites and their centers relative to the
// battlefield, so a projectile can travel from one to the other regardless of
// the you/opp layout (vertical stack, row-reverse, mobile column).
function fxGeom(attackerSide) {
  const bf = document.querySelector('.battlefield');
  const atkEl = document.querySelector(attackerSide === 0 ? '#you-platform .sprite' : '#opp-platform .sprite');
  const tgtEl = document.querySelector(attackerSide === 0 ? '#opp-platform .sprite' : '#you-platform .sprite');
  if (!bf || !atkEl || !tgtEl) return null;
  const b = bf.getBoundingClientRect();
  const center = (el) => {
    const r = el.getBoundingClientRect();
    return { x: r.left + r.width / 2 - b.left, y: r.top + r.height / 2 - b.top };
  };
  // The opponent sprite is CSS-mirrored (scaleX(-1)); any transform we animate
  // on it must keep that flip as a suffix or it un-mirrors mid-effect. The flip
  // goes LAST so the translate stays in screen space (transforms apply R→L).
  return { bf, atkEl, tgtEl, atk: center(atkEl), tgt: center(tgtEl),
    atkFlip: attackerSide === 1, tgtFlip: attackerSide === 0 };
}

function fxSpawn(g, cls, x, y, color) {
  const el = document.createElement('div');
  el.className = 'fx ' + cls;
  el.style.left = x + 'px';
  el.style.top = y + 'px';
  el.style.setProperty('--c', color);
  g.bf.appendChild(el);
  return el;
}

// fxImpact: white-hot burst at the target plus a short horizontal shake.
async function fxImpact(g, color) {
  const burst = fxSpawn(g, 'fx-impact', g.tgt.x, g.tgt.y, color);
  const s = g.tgtFlip ? ' scaleX(-1)' : '';
  g.tgtEl.animate([
    { transform: `translate(0,0)${s}` }, { transform: `translate(6px,0)${s}` },
    { transform: `translate(-5px,0)${s}` }, { transform: `translate(4px,0)${s}` },
    { transform: `translate(-2px,0)${s}` }, { transform: `translate(0,0)${s}` },
  ], { duration: 300 * FX_SPEED, easing: 'ease-out' });
  await burst.animate([
    { transform: 'translate(-50%,-50%) scale(0.2)', opacity: 0.95 },
    { transform: 'translate(-50%,-50%) scale(1.5)', opacity: 0 },
  ], { duration: 340 * FX_SPEED, easing: 'ease-out' }).finished;
  burst.remove();
}

// fxContact: attacker lunges ~40% of the way to the target; impact lands at the
// midpoint while the attacker recoils back.
async function fxContact(g, color) {
  const dx = (g.tgt.x - g.atk.x) * 0.4, dy = (g.tgt.y - g.atk.y) * 0.4;
  const s = g.atkFlip ? ' scaleX(-1)' : '';
  const lunge = g.atkEl.animate([
    { transform: `translate(0,0)${s}` },
    { transform: `translate(${dx}px, ${dy}px)${s}`, offset: 0.45 },
    { transform: `translate(0,0)${s}` },
  ], { duration: 360 * FX_SPEED, easing: 'ease-in-out' }).finished;
  await sleep(150 * FX_SPEED);
  await fxImpact(g, color);
  await lunge;
}

// fxProjectile: a glowing type-colored orb flies from attacker to target.
async function fxProjectile(g, color) {
  const orb = fxSpawn(g, 'fx-orb', g.atk.x, g.atk.y, color);
  const dx = g.tgt.x - g.atk.x, dy = g.tgt.y - g.atk.y;
  await orb.animate([
    { transform: 'translate(-50%,-50%) scale(0.5)', opacity: 0.3 },
    { transform: 'translate(-50%,-50%) scale(1)', opacity: 1, offset: 0.18 },
    { transform: `translate(calc(-50% + ${dx}px), calc(-50% + ${dy}px)) scale(1)`, opacity: 1 },
  ], { duration: 340 * FX_SPEED, easing: 'ease-in' }).finished;
  orb.remove();
  await fxImpact(g, color);
}

// fxBeam: a stretched gradient beam fired from attacker toward target.
async function fxBeam(g, color) {
  const dx = g.tgt.x - g.atk.x, dy = g.tgt.y - g.atk.y;
  const len = Math.hypot(dx, dy);
  const ang = Math.atan2(dy, dx) * 180 / Math.PI;
  const beam = fxSpawn(g, 'fx-beam', g.atk.x, g.atk.y, color);
  beam.style.width = len + 'px';
  await beam.animate([
    { transform: `rotate(${ang}deg) scaleX(0)`, opacity: 0.9 },
    { transform: `rotate(${ang}deg) scaleX(1)`, opacity: 1, offset: 0.55 },
    { transform: `rotate(${ang}deg) scaleX(1)`, opacity: 0 },
  ], { duration: 360 * FX_SPEED, easing: 'ease-out' }).finished;
  beam.remove();
  await fxImpact(g, color);
}

// fxStatus: a type-colored ring pulses out from the target.
async function fxStatus(g, color) {
  const ring = fxSpawn(g, 'fx-ring', g.tgt.x, g.tgt.y, color);
  await ring.animate([
    { transform: 'translate(-50%,-50%) scale(0.3)', opacity: 0.9 },
    { transform: 'translate(-50%,-50%) scale(1.3)', opacity: 0 },
  ], { duration: 520 * FX_SPEED, easing: 'ease-out' }).finished;
  ring.remove();
}

// playMoveEffect dispatches a "X used Move!" log line to the right effect family,
// tinted by the move's element. Failures never interrupt turn playback.
async function playMoveEffect(line) {
  if (REDUCED_MOTION) return;
  const side = Number.isInteger(line.side) ? line.side : -1;
  if (side !== 0 && side !== 1) return;
  const g = fxGeom(side);
  if (!g) return;
  const parsed = /^(.+?) used (.+?)!$/.exec(line.text || '');
  const mv = parsed ? App.moveByName[parsed[2].toLowerCase()] : null;
  const type = (mv && mv.type) || 'normal';
  const color = TYPE_COLORS[type] || 'var(--accent)';
  const cat = mv ? mv.category : 'physical';
  try {
    if (cat === 'status' || (mv && mv.power === 0)) {
      await fxStatus(g, color);
    } else if (cat === 'special') {
      if (RAY_TYPES.has(type)) await fxBeam(g, color);
      else await fxProjectile(g, color);
    } else {
      await fxContact(g, color);
    }
  } catch (_) { /* an effect must never break the battle log */ }
}

// screenShake jolts the battlefield (platforms + banner, not the log, which
// lives outside .battlefield). The battlefield has no base transform, so this
// is safe to animate directly.
function screenShake(px) {
  if (REDUCED_MOTION) return;
  const bf = document.querySelector('.battlefield');
  if (!bf) return;
  bf.animate([
    { transform: 'translate(0,0)' },
    { transform: `translate(${px}px, ${-px * 0.6}px)` },
    { transform: `translate(${-px * 0.8}px, ${px * 0.5}px)` },
    { transform: `translate(${px * 0.5}px, ${px * 0.3}px)` },
    { transform: `translate(${-px * 0.3}px, 0)` },
    { transform: 'translate(0,0)' },
  ], { duration: 360 * FX_SPEED, easing: 'ease-out' });
}

// screenFlash overlays a brief color wash across the battlefield.
function screenFlash(color, maxOpacity) {
  if (REDUCED_MOTION) return;
  const bf = document.querySelector('.battlefield');
  if (!bf) return;
  const f = document.createElement('div');
  f.className = 'fx-flash';
  f.style.background = color;
  bf.appendChild(f);
  f.animate([
    { opacity: 0 }, { opacity: maxOpacity, offset: 0.18 }, { opacity: 0 },
  ], { duration: 300 * FX_SPEED, easing: 'ease-out' }).finished.then(() => f.remove());
}

// playLineDrama adds punctuation to the iconic combat callouts the engine emits
// as plain log text. Effectiveness/crit lines arrive just after the move, so the
// shake lands as emphasis on the hit.
function playLineDrama(line) {
  const t = line.text || '';
  if (/critical hit/i.test(t)) { screenShake(9); screenFlash('#ffffff', 0.5); }
  else if (/super effective/i.test(t)) { screenShake(7); }
}

// ---- battle rendering ----
function renderBattle(state) {
  renderScene(state);
  renderFieldStrip(state);
  renderPlatform(state.sides[1], 'opp-platform', 'opp');
  renderPlatform(state.sides[0], 'you-platform', 'you');
  renderParty(state.sides[1], 'opp-party', 'opp');
  renderParty(state.sides[0], 'you-party', 'you');
}

// renderScene tints the battlefield backdrop with the active weather and
// terrain so those conditions are *felt*, not just read off a chip. The CSS
// keys entirely off the data-weather / data-terrain attributes (rain streaks,
// sun wash, sand haze, snow, plus a colored terrain floor).
function renderScene(state) {
  const scene = document.getElementById('bf-scene');
  if (!scene) return;
  scene.dataset.weather = (state.weather && state.weather.kind) ? state.weather.kind : '';
  scene.dataset.terrain = (state.terrain && state.terrain.kind) ? state.terrain.kind : '';
  const pw = state.pseudo_weather || {};
  scene.dataset.room = pw.trick_room ? 'trick' : '';
}

// ---- party trays (the six-Pokémon roster per side) ----
// Showdown shows benched Pokémon as a row of Poké Balls that turn into sprites
// once revealed. We go further: every party member is a live mini-card carrying
// its own HP sliver and status tag, so "what's on the bench and how hurt is it"
// is legible at a glance. Our own side is always full; the foe's is fog-gated —
// only Pokémon that have actually appeared are shown, the rest stay as balls.
const STATUS_ABBR = {
  burn: 'BRN', poison: 'PSN', toxic: 'TOX', paralysis: 'PAR',
  sleep: 'SLP', freeze: 'FRZ',
};

function partySlotHTML(m, isActive) {
  const isPct = m.hp_pct !== undefined;
  const pct = isPct
    ? Math.max(0, Math.min(100, m.hp_pct))
    : Math.max(0, Math.round((m.hp / m.max_hp) * 100));
  const color = pct > 50 ? 'var(--good)' : pct > 20 ? '#eab308' : 'var(--bad)';
  const st = m.status
    ? `<span class="pt-status st-${m.status}">${STATUS_ABBR[m.status] || ''}</span>` : '';
  const foot = m.fainted
    ? '<span class="pt-faint">✕</span>'
    : `<span class="pt-hp"><i style="width:${pct}%;background:${color}"></i></span>`;
  return `<div class="pt-slot ${m.fainted ? 'fainted' : ''} ${isActive ? 'active' : ''}"
      title="${esc(m.name)}${m.status ? ' · ' + (STATUS_ABBR[m.status] || m.status) : ''}${m.fainted ? ' · fainted' : ` · ${pct}%`}">
    <img src="${spriteUrl(m.dex_no)}" alt="${esc(m.name)}"/>${st}${foot}
  </div>`;
}

function pokeballSlotHTML() {
  return '<div class="pt-slot unknown" title="Not yet revealed"><span class="pt-ball"></span></div>';
}

function renderParty(side, elId) {
  const el = document.getElementById(elId);
  if (!el) return;
  const active = side.team[side.active];
  const fog = side.team.some((m) => m && m._hidden)
    || (active && active.hp_pct !== undefined);
  let slots;
  if (!fog) {
    slots = side.team.map((m, i) => partySlotHTML(m, i === side.active));
  } else {
    // Fog: accumulate every foe Pokémon we've actually seen, keyed by dex.
    if (App.battle && !App.battle.foeSeen) App.battle.foeSeen = {};
    const seenMap = (App.battle && App.battle.foeSeen) || {};
    if (active && active.dex_no) {
      seenMap[active.dex_no] = {
        dex_no: active.dex_no, name: active.name,
        status: active.status, hp_pct: active.hp_pct, fainted: false,
      };
    }
    const activeDex = active ? active.dex_no : -1;
    const seen = Object.values(seenMap);
    slots = seen.map((m) => partySlotHTML(m, m.dex_no === activeDex));
    // Unfainted, still-hidden bench members surface as Poké Balls.
    const benchAlive = side.team.filter((m) => m && m._hidden).length;
    const unknown = Math.max(0, Math.min(6 - slots.length, benchAlive - (seen.length - 1)));
    for (let i = 0; i < unknown; i++) slots.push(pokeballSlotHTML());
  }
  el.innerHTML = slots.slice(0, 6).join('');
}

// ---- field-state indicators ----
// Weather, terrain, screens, and hazards are all public information — the
// engine announces every one of them — but the UI never surfaced any of it.
// The strip above the platforms shows the global pair (weather + terrain);
// each card shows the conditions sitting on its own side of the field.
const WEATHER_LABELS = {
  rain: '🌧 Rain', sun: '☀️ Sun', sandstorm: '🌪 Sandstorm', snow: '❄️ Snow',
};
const TERRAIN_LABELS = {
  electric: '⚡ Electric Terrain', grassy: '🌿 Grassy Terrain',
  misty: '🌫 Misty Terrain', psychic: '🔮 Psychic Terrain',
};
const PSEUDO_WEATHER_LABELS = {
  trick_room: '🔄 Trick Room', wonder_room: '🌀 Wonder Room',
  magic_room: '✨ Magic Room', gravity: '⬇️ Gravity',
};

function renderFieldStrip(state) {
  const el = document.getElementById('field-strip');
  if (!el) return;
  const chips = [];
  if (state.weather && state.weather.kind) {
    chips.push(`<span class="field-chip">${WEATHER_LABELS[state.weather.kind] || esc(state.weather.kind)} (${state.weather.turns_left})</span>`);
  }
  if (state.terrain && state.terrain.kind) {
    chips.push(`<span class="field-chip">${TERRAIN_LABELS[state.terrain.kind] || esc(state.terrain.kind)} (${state.terrain.turns_left})</span>`);
  }
  // Rooms and Gravity coexist (the bag holds independent timers).
  const pw = state.pseudo_weather || {};
  for (const [key, label] of Object.entries(PSEUDO_WEATHER_LABELS)) {
    if (pw[key]) chips.push(`<span class="field-chip">${label} (${pw[key].turns_left})</span>`);
  }
  el.innerHTML = chips.join('');
  el.classList.toggle('hidden', chips.length === 0);
}

// sideCondChipsHTML renders one side's field conditions: timed buffs
// (screens, Tailwind, Safeguard…) with their turns left, plus the entry
// hazards lying on that side's half of the field. sc is the slot bag —
// pending Wish / Healing Wish; for the foe it's the redacted projection
// (caster + countdown, never the heal amount).
function sideCondChipsHTML(c, sc) {
  const chips = [];
  if (sc) {
    if (sc.wish) chips.push(`<span class="cond-chip">Wish (${sc.wish.turns_left})</span>`);
    if (sc.healing_wish) chips.push('<span class="cond-chip">Healing Wish</span>');
  }
  if (!c) return chips.join('');
  const timed = [
    ['reflect', 'Reflect'], ['light_screen', 'Light Screen'], ['aurora_veil', 'Aurora Veil'],
    ['tailwind', 'Tailwind'], ['safeguard', 'Safeguard'], ['mist', 'Mist'],
    ['quick_guard', 'Quick Guard'], ['wide_guard', 'Wide Guard'],
  ];
  for (const [key, label] of timed) {
    const s = c[key];
    if (s) chips.push(`<span class="cond-chip">${label}${s.turns_left ? ` (${s.turns_left})` : ''}</span>`);
  }
  const h = c.hazards || {};
  if (h.stealth_rock) chips.push('<span class="cond-chip hazard">Stealth Rock</span>');
  if (h.spikes) chips.push(`<span class="cond-chip hazard">Spikes ×${h.spikes}</span>`);
  if (h.toxic_spikes) chips.push(`<span class="cond-chip hazard">Toxic Spikes ×${h.toxic_spikes}</span>`);
  return chips.join('');
}

// hazardGroundHTML renders the entry hazards sitting on one side's field as
// little emblems strewn on the ground beneath the sprite — Stealth Rock shards,
// a Spikes count, Toxic Spikes — so the hazard is a thing you see on the field,
// not only a chip on the card.
function hazardGroundHTML(c) {
  const h = (c && c.hazards) || {};
  const bits = [];
  if (h.stealth_rock) bits.push('<span class="hz hz-rock" title="Stealth Rock">◆</span>');
  for (let i = 0; i < (h.spikes || 0); i++) bits.push('<span class="hz hz-spike" title="Spikes">✦</span>');
  for (let i = 0; i < (h.toxic_spikes || 0); i++) bits.push('<span class="hz hz-tox" title="Toxic Spikes">☣</span>');
  return bits.join('');
}

// renderPlatform builds the platform skeleton ONCE per active Pokémon (keyed by
// data-dex) and then mutates the HP fill / name / dots in place on later updates.
// This is what lets the CSS HP-bar transition actually fire (the node persists)
// and lets us detect HP changes (floating damage numbers) and switches (slide-in)
// instead of nuking everything with innerHTML every frame.
function renderPlatform(side, elId, klass) {
  const p = side.team[side.active];
  // The foe arrives fog-redacted: a 0–100 hp_pct with no absolute hp/max_hp
  // (so clients can't read its exact HP). Our own team carries real hp/max_hp.
  const isPct = p.hp_pct !== undefined;
  const pct = isPct
    ? Math.max(0, Math.min(100, p.hp_pct))
    : Math.max(0, Math.round((p.hp / p.max_hp) * 100));
  // Unified HP value for switch-detection and damage-delta tracking: percentage
  // points for the foe, absolute HP for us.
  const hpVal = isPct ? pct : p.hp;
  const color = pct > 50 ? 'var(--good)' : pct > 20 ? '#eab308' : 'var(--bad)';
  const status = p.status
    ? `<span class="status-badge st-${p.status}">${STATUS_ABBR[p.status] || p.status}</span>` : '';
  const dots = side.team.map((m) =>
    `<span class="dot ${m.fainted ? 'fainted' : ''}" title="${esc(m.name)}"></span>`).join('');
  // In live battles side 0 is always "you"; in quicksim there is no player, so
  // we label by trainer slot instead of identity.
  const isLive = App.battle && App.battle.mode === 'live';
  const tag = klass === 'you'
    ? (isLive ? 'YOU' : 'PLAYER 1')
    : (isLive ? 'OPPONENT' : 'PLAYER 2');
  const el = document.getElementById(elId);

  // (Re)build the skeleton only when the active Pokémon changes — i.e. first
  // render or a switch-in. dataset.dex is our identity key; dataset.hp tracks
  // the last rendered HP so we can compute deltas for damage numbers.
  const dexKey = String(p.dex_no);
  const hadPrev = el.dataset.dex !== undefined;
  // Rebuild on a new active Pokémon OR when the skeleton is gone (the platform
  // is cleared with innerHTML='' between battles, which leaves data-dex behind).
  const isSwitch = el.dataset.dex !== dexKey || !el.querySelector('.hpfill');
  if (isSwitch) {
    el.className = 'platform ' + klass;
    el.dataset.dex = dexKey;
    el.dataset.hp = String(hpVal);
    el.innerHTML = `
      <div class="sprite-wrap">
        <div class="platform-pad"></div>
        <img class="sprite" src="${spriteUrl(p.dex_no)}" alt="${esc(p.name)}"/>
        <div class="ground-hazards"></div>
      </div>
      <div class="pkmn-card">
        <span class="side-tag">${tag}</span>
        <div class="trainer">${esc(side.trainer)}</div>
        <div class="pname"></div>
        <div class="hpbar"><div class="hpfill" style="width:${pct}%;background:${color}"></div></div>
        <div class="hp-num"></div>
        <div class="boosts"></div>
        <div class="side-conds"></div>
        <div class="team-dots"></div>
        <div class="fog-tip-slot"></div>
      </div>`;
    // Slide + fade the new sprite in (skip the very first paint of the battle,
    // where every platform "switches" from empty — that reads as a send-out).
    if (hadPrev && !REDUCED_MOTION) {
      const spr = el.querySelector('.sprite');
      const dir = klass === 'you' ? 22 : -22;
      spr.animate([
        { transform: `translateY(${dir}px)`, opacity: 0 },
        { transform: 'translateY(0)', opacity: 1 },
      ], { duration: 320 * FX_SPEED, easing: 'ease-out' });
    }
  }

  // Mutate the dynamic bits in place. Setting hpfill.style.width on the
  // persisted node is what triggers the CSS drain transition.
  el.classList.toggle('fainted', !!p.fainted || pct <= 0);
  el.querySelector('.pname').innerHTML =
    `${esc(p.name)} ${status} <span class="lvl">Lv50</span>`;
  const fill = el.querySelector('.hpfill');
  fill.style.width = pct + '%';
  fill.style.background = color;
  // Foe shows a percentage (its exact HP is hidden); our own team shows counts.
  el.querySelector('.hp-num').textContent = isPct
    ? `${pct}%`
    : `${Math.max(0, p.hp)} / ${p.max_hp} HP`;
  el.querySelector('.boosts').innerHTML = boostChipsHTML(p.stages);
  el.querySelector('.side-conds').innerHTML = sideCondChipsHTML(side.conditions, side.slot_conditions);
  const gh = el.querySelector('.ground-hazards');
  if (gh) gh.innerHTML = hazardGroundHTML(side.conditions);
  el.querySelector('.team-dots').innerHTML = dots;
  // Fog-of-war tooltip: only the fog-redacted foe gets one (a card with
  // hp_pct). Our own card has nothing hidden, so nothing to reveal.
  el.querySelector('.fog-tip-slot').innerHTML = isPct ? fogTipHTML(p) : '';
  el.classList.toggle('has-fog-tip', isPct);

  // Floating damage / heal number when HP changed (not on a switch/first paint).
  // For the foe the delta is in percentage points; for us, absolute HP.
  const prevHp = Number(el.dataset.hp);
  const delta = hpVal - prevHp;
  if (!isSwitch && delta !== 0 && !REDUCED_MOTION) spawnHpDelta(el, delta, isPct);
  el.dataset.hp = String(hpVal);
}

// fogTipHTML is the Showdown-style hover tooltip for the fog-redacted foe:
// what has been *revealed* so far — moves, by usage. The wire deliberately
// carries neither the foe's ability nor its move PP, and we don't hint at
// abilities here either: this panel shows revealed knowledge only.
function fogTipHTML(p) {
  const slots = p.moves || [];
  const revealed = slots.filter((m) => m && m.move_id);
  const names = revealed.map((m) => esc((App.moveById[m.move_id] || { name: m.move_id }).name));
  const movesLine = slots.length
    ? `<b>Moves</b> (${revealed.length}/${slots.length} revealed): ${names.join(', ') || '—'}`
    : '<b>Moves:</b> none revealed';
  return `
    <div class="fog-tip">
      <div class="fog-tip-row">${movesLine}</div>
    </div>`;
}

// boostChipsHTML renders a Pokémon's stat-stage modifiers as Showdown-style
// chips ("+2 Atk", "−1 Spe"). Stages are public information — every boost is
// announced when it happens — so both platforms show them, foe included.
const STAGE_LABELS = [
  ['atk', 'Atk'], ['def', 'Def'], ['spa', 'SpA'], ['spd', 'SpD'],
  ['spe', 'Spe'], ['acc', 'Acc'], ['eva', 'Eva'],
];
function boostChipsHTML(st) {
  if (!st) return '';
  return STAGE_LABELS
    .filter(([k]) => st[k])
    .map(([k, label]) => {
      const n = st[k];
      return `<span class="boost-chip ${n > 0 ? 'up' : 'down'}">${n > 0 ? '+' : '−'}${Math.abs(n)} ${label}</span>`;
    }).join('');
}

// spawnHpDelta floats a red "−N" (or green "+N" on heal) up from the sprite.
// pct=true renders the magnitude as percentage points (the foe's HP is hidden).
function spawnHpDelta(el, delta, pct) {
  const bf = document.querySelector('.battlefield');
  const spr = el.querySelector('.sprite');
  if (!bf || !spr) return;
  const b = bf.getBoundingClientRect();
  const r = spr.getBoundingClientRect();
  const heal = delta > 0;
  const n = document.createElement('div');
  n.className = 'hp-delta ' + (heal ? 'heal' : 'dmg');
  n.textContent = (heal ? '+' : '−') + Math.abs(delta) + (pct ? '%' : '');
  n.style.left = (r.left + r.width / 2 - b.left) + 'px';
  n.style.top = (r.top + r.height * 0.25 - b.top) + 'px';
  bf.appendChild(n);
  n.animate([
    { transform: 'translate(-50%,-50%)', opacity: 0 },
    { transform: 'translate(-50%,-50%) translateY(-8px)', opacity: 1, offset: 0.2 },
    { transform: 'translate(-50%,-50%) translateY(-38px)', opacity: 0 },
  ], { duration: 950 * FX_SPEED, easing: 'ease-out' }).finished.then(() => n.remove());
}

// ---- live controls ----
function updateControls(state) {
  const el = document.getElementById('controls');
  const playable = App.battle && (App.battle.mode === 'live' || App.battle.mode === 'live_pvp');
  if (!playable) {
    el.innerHTML = '<div class="muted">Spectating — watching the battle unfold.</div>';
    return;
  }
  if (state.phase === 'ended') { el.innerHTML = '<div class="muted">Battle over.</div>'; return; }

  const side = state.sides[0];
  const active = side.team[side.active];

  if (state.phase === 'replace') {
    if (state.replace[0]) {
      el.innerHTML = `<div class="ctrl-label">${esc(active.name)} fainted — send in a Pokémon:</div>`
        + switchHTML(side);
      wireControls();
    } else {
      el.innerHTML = '<div class="muted">Waiting for the opponent…</div>';
    }
    return;
  }

  const moves = active.moves.map((ms, i) => {
    const mv = App.moveById[ms.move_id]
      || { name: ms.move_id, type: 'normal', power: 0, category: 'physical' };
    return `<button class="move-btn" data-act="move" data-idx="${i}" ${ms.pp <= 0 ? 'disabled' : ''}>
      <div class="mv-name">${esc(mv.name)}</div>
      <div class="mv-meta"><span class="chip" style="background:${TYPE_COLORS[mv.type]}">${mv.type}</span>
        ${mv.category} · pow ${mv.power || '—'} · pp ${ms.pp}/${ms.max_pp}</div>
    </button>`;
  }).join('');
  el.innerHTML = `<div class="move-grid">${moves}</div>
    <div class="ctrl-label">…or switch Pokémon</div>${switchHTML(side)}`;
  wireControls();
}

function switchHTML(side) {
  const opts = side.team.map((m, i) => {
    if (i === side.active || m.fainted) return '';
    return `<button class="switch-btn" data-act="switch" data-idx="${i}">
      <img src="${spriteUrl(m.dex_no)}" alt=""/><span>${esc(m.name)}</span>
      <span class="muted">${Math.max(0, m.hp)}/${m.max_hp}</span></button>`;
  }).join('');
  return `<div class="switch-row">${opts || '<span class="muted">no Pokémon left to switch to</span>'}</div>`;
}

function wireControls() {
  document.querySelectorAll('#controls [data-act]').forEach((btn) => {
    btn.onclick = () => sendAction(btn.dataset.act, +btn.dataset.idx);
  });
}

// sideClass maps the engine's numeric Side (-1, 0, 1) into a stable CSS hook.
// In live battles side 0 is the player; in quicksim there is no player so we
// fall back to "p1" / "p2" semantics by reusing the same you/opp coloring.
function sideClass(side) {
  if (side === 0) return 'side-you';
  if (side === 1) return 'side-opp';
  return 'side-sys';
}
function sideLabel(side) {
  if (side === 0) return App.battle && App.battle.mode === 'live' ? 'YOU' : 'P1';
  if (side === 1) return App.battle && App.battle.mode === 'live' ? 'OPP' : 'P2';
  return '·';
}

// renderMoveLineHTML upgrades a "X used Y!" string into a typed chip. We split
// on " used " (the deterministic phrasing engine.go emits), and if the move
// name resolves in our moveByName index we render it with its type color —
// the same chip the player sees in the controls panel a moment earlier.
function renderMoveLineHTML(text) {
  const m = /^(.+?) used (.+?)!$/.exec(text);
  if (!m) return null;
  const [, actor, moveName] = m;
  const mv = App.moveByName[moveName.toLowerCase()];
  const bg = mv && TYPE_COLORS[mv.type] ? TYPE_COLORS[mv.type] : 'var(--accent)';
  return `<span>${esc(actor)} used</span>` +
    `<span class="log-move-chip" style="background:${bg}">${esc(moveName)}</span>`;
}

// moveChipHTML renders a single move name as a type-colored chip — the same
// visual primitive used in the controls panel, so the user's eye carries from
// "what I clicked" to "what I chose" to "what fired" along the same shape.
function moveChipHTML(name, type) {
  const bg = TYPE_COLORS[type] || 'var(--accent)';
  return `<span class="log-move-chip" style="background:${bg}">${esc(name)}</span>`;
}

function logLine(line) {
  const log = document.getElementById('battle-log');
  const div = document.createElement('div');
  const side = Number.isInteger(line.side) ? line.side : -1;
  div.className = `log-line log-${line.type || 'info'} ${sideClass(side)}`;

  // Emphasize the iconic combat callouts (works on any line, before the
  // system/turn early-return below).
  const lt = line.text || '';
  if (/critical hit/i.test(lt)) div.classList.add('log-crit');
  else if (/super effective/i.test(lt)) div.classList.add('log-super');
  else if (/not very effective/i.test(lt)) div.classList.add('log-resist');
  else if (/missed|no effect|immune/i.test(lt)) div.classList.add('log-miss');

  // System / turn-header lines stay plain — they belong to no side, and a
  // badge would be visual noise on every "— Turn N —" separator.
  if (line.type === 'turn' || side === -1) {
    div.innerHTML = `<span>${esc(line.text)}</span>`;
    log.appendChild(div);
    log.scrollTop = log.scrollHeight;
    return;
  }

  const badge = `<span class="log-side-badge">${sideLabel(side)}</span>`;
  let body;
  if (line.chip) {
    // Explicit structured chip (used by the client-side action echo, where we
    // know the move object directly and don't need to parse text).
    body = `<span>${esc(line.text)}</span>` + moveChipHTML(line.chip.name, line.chip.type);
  } else if (line.type === 'move') {
    // Engine-emitted line: parse "Actor used Move!" and re-attach the chip.
    body = renderMoveLineHTML(line.text) || `<span>${esc(line.text)}</span>`;
  } else {
    body = `<span>${esc(line.text)}</span>`;
  }
  div.innerHTML = badge + body;
  log.appendChild(div);
  log.scrollTop = log.scrollHeight;
}

// ---- live_pvp ----

// tryAutoJoin reads the join params off the page URL and, if all three are
// present, takes the user straight to the arena as a pvp client. Returns
// true iff it dispatched (so the caller can skip default-init behavior if
// it wants to — today, we keep setup rendered behind the arena so a user
// who clicks "Back to setup" lands somewhere sensible).
function tryAutoJoin() {
  const params = new URLSearchParams(location.search);
  const spec = params.get('spectate');
  if (spec) {
    enterSpectate(spec);
    return true;
  }
  const battle = params.get('battle');
  const slot = params.get('slot');
  const token = params.get('token');
  if (!battle || !slot || !token) return false;
  autoJoinPvP(battle, slot, token);
  return true;
}

function autoJoinPvP(battleId, slot, token) {
  // p2 joins straight into the picker. Their setup-page draft is irrelevant
  // here — the share URL is the entire context they have. The first room
  // frame populates the opponent panel; "state" later transitions to arena.
  App.pick = newBuilderState();
  App.pickerSubmitted = false;
  if (App.pickerDeadlineTimer) { clearInterval(App.pickerDeadlineTimer); App.pickerDeadlineTimer = null; }
  App.battle = {
    id: battleId, mode: 'live_pvp', name: 'Trainer', view: 'picker',
    queue: [], playing: false, ended: false, state: null, ws: null, es: null, slot,
  };
  showView('picker');
  document.getElementById('picker-label').textContent = `Pv-Player · joining slot ${slot} · drafting team`;
  document.querySelector('.picker-main').classList.remove('picker-locked');
  document.getElementById('picker-share-banner').classList.add('hidden');
  document.getElementById('picker-opp').innerHTML =
    '<h4>Opponent</h4><div class="muted">Connecting…</div>';
  renderPicker();
  const wsUrl = `/api/battles/${battleId}/play?slot=${slot}&token=${encodeURIComponent(token)}`;
  connectPvPWS(wsUrl);
}

// connectPvPWS opens the pvp WebSocket. The message shape is different
// from the legacy live mode (BattleView, not BattleState; "state" / "turn"
// / "end" / "info" / "error" — see internal/httpapi/pvp.go's matchUpdate),
// so we route incoming frames to a dedicated handler.
function connectPvPWS(wsUrl) {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const ws = new WebSocket(`${proto}://${location.host}${wsUrl}`);
  App.battle.ws = ws;
  ws.onmessage = (ev) => {
    let msg;
    try { msg = JSON.parse(ev.data); } catch (e) { return; }
    handlePvPWSMessage(msg);
  };
  ws.onerror = () => toast('Battle connection error');
  ws.onclose = () => {
    // A close while we still expect to be playing is a real disconnect.
    // After end-of-battle (App.battle.ended = true) it's normal cleanup.
    if (App.battle && !App.battle.ended) toast('Connection closed');
  };
}

function handlePvPWSMessage(msg) {
  if (!App.battle) return;
  switch (msg.type) {
    case 'room': {
      // Picker room. Render opponent status; the user's own submission
      // is gated behind the Submit button (no auto-submit). The optimistic
      // local "submitted" flag is reconciled against the server's
      // you.submitted on every frame in case our send raced a disconnect.
      if (msg.room) {
        renderPickerOpp(msg.room);
        if (msg.room.you && msg.room.you.submitted && !App.pickerSubmitted) {
          App.pickerSubmitted = true;
          document.querySelector('.picker-main').classList.add('picker-locked');
          updatePickerSubmitButton();
        }
      }
      break;
    }
    case 'state': {
      if (App.battle.view === 'picker') transitionPickerToArena();
      App.battle.state = viewToRenderableState(msg.view);
      const myName = App.battle.state.sides[0].trainer;
      if (myName) {
        document.getElementById('arena-label').textContent = `Pv-Player · ${myName}`;
        App.battle.name = myName;
      }
      renderBattle(App.battle.state);
      updateControls(App.battle.state);
      break;
    }
    case 'turn':
      App.battle.queue.push({ turn: { log: msg.log, state: viewToRenderableState(msg.view) } });
      playLoop();
      break;
    case 'info':
      App.battle.queue.push({ info: msg.message });
      playLoop();
      break;
    case 'error':
      toast(msg.message);
      // During the picker, an error usually means the server rejected our
      // submit_team. Roll back the optimistic lock so the user can fix
      // their picks and resubmit instead of staring at a stuck "✓".
      if (App.battle.view === 'picker' && App.pickerSubmitted) {
        App.pickerSubmitted = false;
        document.querySelector('.picker-main').classList.remove('picker-locked');
        updatePickerSubmitButton();
      }
      if (App.battle.state) updateControls(App.battle.state);
      break;
    case 'end': {
      if (!msg.view) { endAbandoned(); break; }
      App.battle.state = viewToRenderableState(msg.view);
      // The server reports the engine's winner side: 0, 1, or 2 for a draw.
      // Normalize so 0 = "you", 1 = "opponent", regardless of which slot we
      // actually claimed; anything else (a draw) becomes -1, which showResult
      // renders as the draw banner. It only needs "did I win" and "draw or no".
      const me = msg.view.me;
      const w = msg.winner;
      const normalized = (w === 0 || w === 1)
        ? (w === me ? 0 : 1)
        : -1;
      App.battle.queue.push({ end: { winner: normalized } });
      playLoop();
      break;
    }
  }
}

// viewToRenderableState adapts a BattleView (fog-of-war) into the
// state-shaped object the existing renderer expects. Conventions:
//   - sides[0] is always "you" in the UI, regardless of server slot.
//   - The opponent side gets a synthetic team: visible Foe at index 0,
//     plus foe_bench_alive opaque "?" placeholders so the dots render
//     the bench count. We never invent fainted opponent info — what we
//     don't know, we don't show.
function viewToRenderableState(view) {
  const bench = [];
  for (let i = 0; i < (view.foe_bench_alive || 0); i++) {
    bench.push({
      name: '?', dex_no: 0, fainted: false,
      hp: 1, max_hp: 1, moves: [], _hidden: true,
    });
  }
  const opp = {
    trainer: 'Opponent',  // BattleView doesn't carry the opponent's name.
    team: [view.foe, ...bench],
    active: 0,
    // The foe's side conditions (screens, hazards on their field) are
    // public and arrive as a dedicated field on the view, as does the
    // redacted slot bag (pending Wish — caster and countdown, no amount).
    conditions: view.foe_conditions,
    slot_conditions: view.foe_slot_conditions,
  };
  return {
    phase: view.phase,
    turn: view.turn,
    // We only know our own replace flag — the opponent's is private.
    replace: [view.replace === true, false],
    sides: [view.self, opp],
    weather: view.weather,
    terrain: view.terrain,
    pseudo_weather: view.pseudo_weather,
    winner: -1,
  };
}

init();
