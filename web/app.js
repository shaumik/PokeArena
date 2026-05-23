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
const esc = (s) => String(s).replace(/[&<>"]/g, (c) =>
  ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));

// ---- app state ----
const App = {
  pokedex: [], dexByNo: {}, moveById: {}, moveByName: {},
  yourTeam: [], oppTeam: [], editing: 'your',
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
  document.querySelectorAll('.seg-btn').forEach((b) => {
    b.onclick = () => {
      App.editing = b.dataset.edit;
      document.querySelectorAll('.seg-btn').forEach((x) => x.classList.toggle('active', x === b));
      renderRoster();
    };
  });
  document.querySelectorAll('[data-randomize]').forEach((b) => {
    b.onclick = () => randomizeTeam(b.dataset.randomize);
  });
  document.getElementById('start-battle').onclick = startBattle;
  document.getElementById('refresh-lb').onclick = loadLeaderboard;
  document.getElementById('leave-arena').onclick = leaveArena;

  try {
    App.pokedex = await api('/api/pokemon');
  } catch (e) {
    toast('Could not load the Pokédex: ' + e.message);
    return;
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
  randomizeTeam('your');
  randomizeTeam('opp');
}

// ---- team builder ----
function monCard(p, picked) {
  const t2 = p.type2
    ? `<span class="chip" style="background:${TYPE_COLORS[p.type2]}">${p.type2}</span>` : '';
  return `<div class="mon ${picked ? 'picked' : ''}" data-dex="${p.dex_no}">
    <img src="${spriteUrl(p.dex_no)}" alt="${esc(p.name)}" loading="lazy"/>
    <div class="name">${esc(p.name)}</div>
    <div class="types"><span class="chip" style="background:${TYPE_COLORS[p.type1]}">${p.type1}</span>${t2}</div>
    <div class="dex">#${String(p.dex_no).padStart(3, '0')}</div>
  </div>`;
}

function renderRoster() {
  const el = document.getElementById('roster');
  const team = App.editing === 'your' ? App.yourTeam : App.oppTeam;
  el.innerHTML = App.pokedex.map((p) => monCard(p, team.includes(p.dex_no))).join('');
  el.querySelectorAll('.mon').forEach((c) => { c.onclick = () => toggleMon(+c.dataset.dex); });
  renderTrays();
}

function toggleMon(dex) {
  const team = App.editing === 'your' ? App.yourTeam : App.oppTeam;
  const i = team.indexOf(dex);
  if (i >= 0) team.splice(i, 1);
  else if (team.length < 6) team.push(dex);
  else { toast('A team can hold at most 6 Pokémon'); return; }
  renderRoster();
}

function renderTrays() {
  ['your', 'opp'].forEach((which) => {
    const team = which === 'your' ? App.yourTeam : App.oppTeam;
    document.getElementById(which + '-count').textContent = `${team.length}/6`;
    const tray = document.getElementById(which + '-tray');
    tray.innerHTML = team.map((dex, idx) => {
      const p = App.dexByNo[dex];
      return `<div class="slot" data-which="${which}" data-idx="${idx}">
        <img src="${spriteUrl(dex)}" alt=""/><span>${esc(p.name)}</span></div>`;
    }).join('') || '<span class="muted">empty — click Pokémon below</span>';
    tray.querySelectorAll('.slot').forEach((s) => {
      s.onclick = () => {
        (s.dataset.which === 'your' ? App.yourTeam : App.oppTeam).splice(+s.dataset.idx, 1);
        renderRoster();
      };
    });
  });
}

function randomizeTeam(which) {
  const pool = App.pokedex.map((p) => p.dex_no);
  const team = [];
  while (team.length < 6 && pool.length) {
    team.push(pool.splice(Math.floor(Math.random() * pool.length), 1)[0]);
  }
  if (which === 'your') App.yourTeam = team; else App.oppTeam = team;
  renderRoster();
}

// ---- pokedex view ----
function renderPokedex() {
  document.getElementById('pokedex-grid').innerHTML = App.pokedex.map((p) => {
    const t2 = p.type2
      ? `<span class="chip" style="background:${TYPE_COLORS[p.type2]}">${p.type2}</span>` : '';
    const b = p.base;
    const bst = b.hp + b.atk + b.def + b.spatk + b.spdef + b.speed;
    return `<div class="mon" title="Base stat total ${bst}">
      <img src="${spriteUrl(p.dex_no)}" alt="${esc(p.name)}" loading="lazy"/>
      <div class="name">${esc(p.name)}</div>
      <div class="types"><span class="chip" style="background:${TYPE_COLORS[p.type1]}">${p.type1}</span>${t2}</div>
      <div class="dex">#${String(p.dex_no).padStart(3, '0')} · BST ${bst}</div>
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
  if (!App.yourTeam.length) { toast('Pick at least one Pokémon for your team'); return; }
  if (!App.oppTeam.length) { toast('Pick at least one Pokémon for the opponent'); return; }

  const mode = document.getElementById('mode').value;
  const difficulty = document.getElementById('difficulty').value;
  const name = document.getElementById('player-name').value.trim() || 'Challenger';
  const body = {
    mode,
    p1_name: name,
    p2_name: mode === 'live' ? `AI (${difficulty})` : 'Rival',
    p1_team: App.yourTeam,
    p2_team: App.oppTeam,
    p1_difficulty: difficulty,
    p2_difficulty: difficulty,
  };

  const btn = document.getElementById('start-battle');
  btn.disabled = true;
  try {
    const res = await api('/api/battles', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    enterArena(res, mode, name);
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
  App.battle = {
    id: res.battle_id, mode, name,
    queue: [], playing: false, ended: false, state: null, ws: null, es: null,
  };
  document.getElementById('arena-label').textContent = mode === 'live'
    ? `Live Battle · ${name} vs AI`
    : `Quick Sim · ${name} vs Rival — spectating`;
  logLine({ type: 'turn', text: 'Battle starting…' });
  if (mode === 'live') connectWS(res.ws_url);
  else spectate(res.battle_id);
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
    case 'state':
      App.battle.state = msg.state;
      renderBattle(msg.state);
      updateControls(msg.state);
      break;
    case 'turn':
      App.battle.queue.push({ turn: msg });
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
    case 'end':
      App.battle.state = msg.state || App.battle.state;
      App.battle.queue.push({ end: msg });
      playLoop();
      break;
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
    await sleep(step);
  }
  await sleep(260);
  if (msg.state && msg.state.phase !== 'ended') updateControls(msg.state);
}

async function showResult(end) {
  if (!App.battle) return;
  App.battle.ended = true;
  const banner = document.getElementById('result-banner');
  const w = end.winner;
  let cls = 'win-draw';
  let text = '🤝 The battle ended in a draw.';
  if (App.battle.mode === 'live') {
    if (w === 0) { cls = 'win-you'; text = '🏆 Victory! You won the battle.'; }
    else if (w === 1) { cls = 'win-opp'; text = '💀 Defeat — the AI won this one.'; }
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

// ---- battle rendering ----
function renderBattle(state) {
  renderPlatform(state.sides[1], 'opp-platform', 'opp');
  renderPlatform(state.sides[0], 'you-platform', 'you');
}

function renderPlatform(side, elId, klass) {
  const p = side.team[side.active];
  const pct = Math.max(0, Math.round((p.hp / p.max_hp) * 100));
  const color = pct > 50 ? 'var(--good)' : pct > 20 ? '#eab308' : 'var(--bad)';
  const status = p.status
    ? `<span class="status-badge st-${p.status}">${p.status}</span>` : '';
  const dots = side.team.map((m) =>
    `<span class="dot ${m.fainted ? 'fainted' : ''}" title="${esc(m.name)}"></span>`).join('');
  // In live battles side 0 is always "you"; in quicksim there is no player, so
  // we label by trainer slot instead of identity.
  const isLive = App.battle && App.battle.mode === 'live';
  const tag = klass === 'you'
    ? (isLive ? 'YOU' : 'PLAYER 1')
    : (isLive ? 'OPPONENT' : 'PLAYER 2');
  const el = document.getElementById(elId);
  el.className = 'platform ' + klass;
  el.innerHTML = `
    <img class="sprite" src="${spriteUrl(p.dex_no)}" alt="${esc(p.name)}"/>
    <div class="pkmn-card">
      <span class="side-tag">${tag}</span>
      <div class="trainer">${esc(side.trainer)}</div>
      <div class="pname">${esc(p.name)} ${status} <span class="lvl">Lv50</span></div>
      <div class="hpbar"><div class="hpfill" style="width:${pct}%;background:${color}"></div></div>
      <div class="hp-num">${Math.max(0, p.hp)} / ${p.max_hp} HP</div>
      <div class="team-dots">${dots}</div>
    </div>`;
}

// ---- live controls ----
function updateControls(state) {
  const el = document.getElementById('controls');
  if (!App.battle || App.battle.mode !== 'live') {
    el.innerHTML = '<div class="muted">Spectating — sit back and watch the AI battle it out.</div>';
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

init();
