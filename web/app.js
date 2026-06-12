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
const App = {
  pokedex: [], dexByNo: {}, moveById: {}, moveByName: {},
  yourTeam: [], oppTeam: [], editing: 'your',
  // yourMoves[dexNo] = [move_id, ...] (1-4 entries). Populated when a
  // Pokémon enters the team (defaulting to its first 4 learnset moves)
  // and editable from the setup builder. Used for quicksim only.
  yourMoves: {},
  // Picker state. Independent from the setup-page team — the picker view
  // starts empty regardless of what's drafted on setup. submitted flips
  // optimistically on send and is confirmed by the next room frame.
  // pickerAbility[dexNo] = slug; absent or "" means "use slot 0" (default),
  // which the backend treats identically to omitting the ability field.
  pickerTeam: [], pickerMoves: {}, pickerAbility: {}, pickerSubmitted: false, pickerDeadlineTimer: null,
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
  document.getElementById('leave-picker').onclick = leavePicker;
  document.getElementById('picker-submit').onclick = submitPicker;
  document.getElementById('picker-randomize').onclick = randomizePicker;

  // syncMode owns two things that depend on the mode select:
  //   - Difficulty is meaningless for live_pvp (no AI on either side); hide it.
  //   - The setup-page team builder is only authoritative for quicksim, where
  //     both teams must be present at POST. Live + live_pvp use the dedicated
  //     picker view, so the builder here would be misleading. Hide it.
  const modeSel = document.getElementById('mode');
  const syncMode = () => {
    const m = modeSel.value;
    document.getElementById('difficulty-label').style.display =
      (m === 'live_pvp' || m === 'agent_vs_agent') ? 'none' : '';
    const showTeams = m === 'quicksim';
    document.querySelector('.teams').style.display = showTeams ? '' : 'none';
    document.querySelector('.editing-row').style.display = showTeams ? '' : 'flex';
    document.getElementById('roster').style.display = showTeams ? '' : 'none';
    // The editing-row also contains the Start button — keep that visible.
    // Hide only the per-side chooser (the .seg widget) and the helper text.
    document.querySelectorAll('.editing-row > .seg, .editing-row > span').forEach((e) => {
      e.style.display = showTeams ? '' : 'none';
    });
  };
  modeSel.onchange = syncMode;
  syncMode();

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

  // If the page URL carries join params (?battle=…&slot=…&token=…), skip
  // setup and connect straight to the arena. This is the path the
  // opponent takes when they open the share link.
  tryAutoJoin();
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
  else if (team.length < 6) {
    team.push(dex);
    if (App.editing === 'your' && !App.yourMoves[dex]) {
      App.yourMoves[dex] = defaultMovesFor(dex);
    }
  }
  else { toast('A team can hold at most 6 Pokémon'); return; }
  renderRoster();
}

// defaultMovesFor returns the first up-to-4 moves from a species'
// learn list — the default moveset the picker uses unless the user
// edits it via the moveset panel.
function defaultMovesFor(dex) {
  const sp = App.dexByNo[dex];
  if (!sp || !sp.moves) return [];
  return sp.moves.slice(0, 4).map((m) => m.id);
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
  renderMoveset();
}

// renderMoveset paints the per-Pokémon move dropdowns under the
// "your" tray for the quicksim setup builder. Picker mode has its own
// rendering path via renderPickerMoveset.
function renderMoveset() {
  const panel = document.getElementById('your-moveset');
  if (!panel) return;
  if (!App.yourTeam.length) {
    panel.innerHTML = '<div class="muted">Pick a team to choose moves.</div>';
    return;
  }
  panel.innerHTML = App.yourTeam.map((dex, slotIdx) => {
    const sp = App.dexByNo[dex];
    const selected = App.yourMoves[dex] || defaultMovesFor(dex);
    App.yourMoves[dex] = selected;
    const learnset = sp.moves || [];
    const slots = [0, 1, 2, 3].map((mi) => {
      const cur = selected[mi] || '';
      const opts = learnset.map((m) => {
        const mark = m.id === cur ? ' selected' : '';
        return `<option value="${esc(m.id)}"${mark}>${esc(m.name)}</option>`;
      }).join('');
      const blank = cur ? '' : '<option value="" selected>— empty —</option>';
      return `<select class="mvsel" data-slot="${slotIdx}" data-mi="${mi}">${blank}${opts}</select>`;
    }).join('');
    return `<div class="mv-row">
      <img src="${spriteUrl(dex)}" alt=""/>
      <span class="mv-name">${esc(sp.name)}</span>
      <div class="mv-slots">${slots}</div>
    </div>`;
  }).join('');
  panel.querySelectorAll('.mvsel').forEach((sel) => {
    sel.onchange = () => {
      const dex = App.yourTeam[+sel.dataset.slot];
      const mi = +sel.dataset.mi;
      const moves = (App.yourMoves[dex] || defaultMovesFor(dex)).slice();
      moves[mi] = sel.value;
      // Strip empties, dedupe, clamp to 4. The server validates strictly;
      // we shape the array before send so the wire stays clean.
      const out = [];
      for (const m of moves) {
        if (m && !out.includes(m)) out.push(m);
        if (out.length === 4) break;
      }
      if (out.length === 0) {
        toast(`${App.dexByNo[dex].name} needs at least one move.`);
        return;
      }
      App.yourMoves[dex] = out;
    };
  });
}

function randomizeTeam(which) {
  const pool = App.pokedex.map((p) => p.dex_no);
  const team = [];
  while (team.length < 6 && pool.length) {
    team.push(pool.splice(Math.floor(Math.random() * pool.length), 1)[0]);
  }
  if (which === 'your') {
    App.yourTeam = team;
    // Re-seed default movesets for the new lineup; previously edited
    // movesets for species no longer on the team are kept but won't be
    // used until that species is re-added.
    team.forEach((dex) => { App.yourMoves[dex] = defaultMovesFor(dex); });
  } else {
    App.oppTeam = team;
  }
  renderRoster();
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
    if (!App.yourTeam.length) { toast('Pick at least one Pokémon for your team'); return; }
    if (!App.oppTeam.length) { toast('Pick at least one Pokémon for the opponent'); return; }
  }

  const difficulty = document.getElementById('difficulty').value;
  const name = document.getElementById('player-name').value.trim() || 'Challenger';
  // agent_vs_agent is a UI framing on top of live_pvp — backend has no separate
  // mode because the protocol is identical (two external joiners, no AI). We
  // just present the URLs differently and drop the user into spectate.
  const backendMode = mode === 'agent_vs_agent' ? 'live_pvp' : mode;
  const body = {
    mode: backendMode,
    p1_name: mode === 'agent_vs_agent' ? 'Agent 1' : name,
    p2_name: mode === 'live' ? `AI (${difficulty})`
      : mode === 'live_pvp' ? 'Opponent'
      : mode === 'agent_vs_agent' ? 'Agent 2'
      : 'Rival',
  };
  if (mode === 'quicksim') {
    body.p1_team = App.yourTeam;
    body.p2_team = App.oppTeam;
  }
  if (backendMode !== 'live_pvp') {
    body.p1_difficulty = difficulty;
    body.p2_difficulty = difficulty;
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

function enterPicker(res, mode, name) {
  // Reset picker state — start empty per the agreed UX.
  App.pickerTeam = [];
  App.pickerMoves = {};
  App.pickerAbility = {};
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
  renderPicker();

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

function renderPicker() {
  // Roster grid — clickable cards. Reuses monCard from the setup builder.
  const roster = document.getElementById('picker-roster');
  roster.innerHTML = App.pokedex.map((p) => monCard(p, App.pickerTeam.includes(p.dex_no))).join('');
  roster.querySelectorAll('.mon').forEach((c) => { c.onclick = () => togglePickerMon(+c.dataset.dex); });

  // Tray — six slots, click to remove.
  document.getElementById('picker-count').textContent = `${App.pickerTeam.length}/6`;
  const tray = document.getElementById('picker-tray');
  tray.innerHTML = App.pickerTeam.map((dex, idx) => {
    const p = App.dexByNo[dex];
    return `<div class="slot" data-idx="${idx}"><img src="${spriteUrl(dex)}" alt=""/><span>${esc(p.name)}</span></div>`;
  }).join('') || '<span class="muted">empty — click Pokémon below</span>';
  tray.querySelectorAll('.slot').forEach((s) => {
    s.onclick = () => { App.pickerTeam.splice(+s.dataset.idx, 1); renderPicker(); };
  });

  renderPickerMoveset();
  updatePickerSubmitButton();
}

function togglePickerMon(dex) {
  if (App.pickerSubmitted) return;
  const i = App.pickerTeam.indexOf(dex);
  if (i >= 0) {
    App.pickerTeam.splice(i, 1);
  } else if (App.pickerTeam.length < 6) {
    App.pickerTeam.push(dex);
    if (!App.pickerMoves[dex]) App.pickerMoves[dex] = defaultMovesFor(dex);
  } else {
    toast('A team can hold at most 6 Pokémon'); return;
  }
  renderPicker();
}

function renderPickerMoveset() {
  const panel = document.getElementById('picker-moveset');
  if (!App.pickerTeam.length) {
    panel.innerHTML = '<div class="muted">Pick a team to choose moves.</div>';
    return;
  }
  panel.innerHTML = App.pickerTeam.map((dex, slotIdx) => {
    const sp = App.dexByNo[dex];
    const selected = App.pickerMoves[dex] || defaultMovesFor(dex);
    App.pickerMoves[dex] = selected;
    const abilities = sp.abilities || [];
    const curAbility = App.pickerAbility[dex] || '';
    const abilityOpts = abilities.length
      ? abilities.map((a, i) => {
          const mark = (curAbility ? a === curAbility : i === 0) ? ' selected' : '';
          const label = i === 0 ? `${a} (default)` : a;
          return `<option value="${esc(a)}"${mark}>${esc(label)}</option>`;
        }).join('')
      : '';
    const abilitySel = abilities.length
      ? `<select class="absel" data-slot="${slotIdx}">${abilityOpts}</select>`
      : '<span class="muted">—</span>';
    const learnset = sp.moves || [];
    const slots = [0, 1, 2, 3].map((mi) => {
      const cur = selected[mi] || '';
      const opts = learnset.map((m) => {
        const mark = m.id === cur ? ' selected' : '';
        return `<option value="${esc(m.id)}"${mark}>${esc(m.name)}</option>`;
      }).join('');
      const blank = cur ? '' : '<option value="" selected>— empty —</option>';
      return `<select class="mvsel" data-slot="${slotIdx}" data-mi="${mi}">${blank}${opts}</select>`;
    }).join('');
    return `<div class="mv-row">
      <img src="${spriteUrl(dex)}" alt=""/>
      <span class="mv-name">${esc(sp.name)}</span>
      <div class="mv-slots">${slots}</div>
      <div class="ab-pick"><label class="muted">Ability</label>${abilitySel}</div>
    </div>`;
  }).join('');
  panel.querySelectorAll('.mvsel').forEach((sel) => {
    sel.onchange = () => {
      const dex = App.pickerTeam[+sel.dataset.slot];
      const mi = +sel.dataset.mi;
      const moves = (App.pickerMoves[dex] || defaultMovesFor(dex)).slice();
      moves[mi] = sel.value;
      const out = [];
      for (const m of moves) {
        if (m && !out.includes(m)) out.push(m);
        if (out.length === 4) break;
      }
      if (out.length === 0) { toast(`${App.dexByNo[dex].name} needs at least one move.`); return; }
      App.pickerMoves[dex] = out;
    };
  });
  panel.querySelectorAll('.absel').forEach((sel) => {
    sel.onchange = () => {
      const dex = App.pickerTeam[+sel.dataset.slot];
      App.pickerAbility[dex] = sel.value;
    };
  });
}

function updatePickerSubmitButton() {
  const btn = document.getElementById('picker-submit');
  if (App.pickerSubmitted) {
    btn.disabled = true; btn.textContent = '✓ Submitted'; return;
  }
  btn.disabled = App.pickerTeam.length !== 6;
  btn.textContent = App.pickerTeam.length === 6 ? 'Submit team ▶' : `Pick ${6 - App.pickerTeam.length} more`;
}

function submitPicker() {
  if (App.pickerSubmitted || App.pickerTeam.length !== 6) return;
  const ws = App.battle && App.battle.ws;
  if (!ws || ws.readyState !== WebSocket.OPEN) { toast('Not connected'); return; }
  const picks = App.pickerTeam.map((dex) => {
    const sp = App.dexByNo[dex];
    const pick = {
      dex_no: dex,
      moves: (App.pickerMoves[dex] && App.pickerMoves[dex].length)
        ? App.pickerMoves[dex].slice(0, 4)
        : defaultMovesFor(dex),
    };
    const ab = App.pickerAbility[dex];
    if (ab && sp && sp.abilities && ab !== sp.abilities[0]) {
      pick.ability = ab;
    }
    return pick;
  });
  try {
    ws.send(JSON.stringify({ type: 'submit_team', picks }));
  } catch (e) {
    toast('Could not submit team: ' + e.message); return;
  }
  // Optimistic lock. The next room frame confirms (or a FrameError unlocks).
  App.pickerSubmitted = true;
  document.querySelector('.picker-main').classList.add('picker-locked');
  updatePickerSubmitButton();
}

function randomizePicker() {
  if (App.pickerSubmitted) return;
  const pool = App.pokedex.map((p) => p.dex_no);
  App.pickerTeam = [];
  while (App.pickerTeam.length < 6 && pool.length) {
    App.pickerTeam.push(pool.splice(Math.floor(Math.random() * pool.length), 1)[0]);
  }
  App.pickerTeam.forEach((dex) => { App.pickerMoves[dex] = defaultMovesFor(dex); });
  renderPicker();
}

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
  renderPlatform(state.sides[1], 'opp-platform', 'opp');
  renderPlatform(state.sides[0], 'you-platform', 'you');
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
      <img class="sprite" src="${spriteUrl(p.dex_no)}" alt="${esc(p.name)}"/>
      <div class="pkmn-card">
        <span class="side-tag">${tag}</span>
        <div class="trainer">${esc(side.trainer)}</div>
        <div class="pname"></div>
        <div class="hpbar"><div class="hpfill" style="width:${pct}%;background:${color}"></div></div>
        <div class="hp-num"></div>
        <div class="boosts"></div>
        <div class="team-dots"></div>
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
  el.querySelector('.team-dots').innerHTML = dots;

  // Floating damage / heal number when HP changed (not on a switch/first paint).
  // For the foe the delta is in percentage points; for us, absolute HP.
  const prevHp = Number(el.dataset.hp);
  const delta = hpVal - prevHp;
  if (!isSwitch && delta !== 0 && !REDUCED_MOTION) spawnHpDelta(el, delta, isPct);
  el.dataset.hp = String(hpVal);
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
  App.pickerTeam = [];
  App.pickerMoves = {};
  App.pickerAbility = {};
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
      App.battle.state = viewToRenderableState(msg.view);
      // The server reports the engine's winner side (0 or 1). Normalize so
      // 0 = "you", 1 = "opponent", regardless of which slot we actually
      // claimed. showResult only needs to know "did I win" and "draw or no".
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
  };
  return {
    phase: view.phase,
    turn: view.turn,
    // We only know our own replace flag — the opponent's is private.
    replace: [view.replace === true, false],
    sides: [view.self, opp],
    winner: -1,
  };
}

init();
