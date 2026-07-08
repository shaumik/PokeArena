package eval

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
)

// A saved run is JSON — precise, but not something you hand to a person. This
// renders one RunRecord into a single self-contained HTML page: no external
// CSS, JS, fonts, or network, so the file can be opened offline, attached to an
// issue, or published as-is. It reads only what the record already holds, so the
// report can never disagree with the persisted numbers.

// reportRow is one contestant precomputed for display: strings for the eye and
// percentages for the confidence-interval bar (0..100, so they drop straight
// into CSS widths).
type reportRow struct {
	Rank        int
	Name        string
	Model       string
	Condition   string
	Elo         string
	WinRate     string
	Record      string
	Games       int
	Cost        string
	CostPerGame string
	Top         bool
	// CI bar geometry, in percent of the 0..100% win-rate axis.
	CILeft  float64
	CIWidth float64
	Mark    float64
}

type reportView struct {
	Rec     RunRecord
	Rows    []reportRow
	HasCost bool
	Seeds   string
	// ReplaysJSON is the embedded battle data the in-page replayer reads. It is
	// template.JS so it drops into a <script> as a literal; json.Marshal escapes
	// <, >, and & to \u00xx by default, so it can't break out of the tag.
	HasReplays  bool
	ReplaysJSON template.JS
}

func buildReportView(rec RunRecord) reportView {
	v := reportView{Rec: rec}
	for i, c := range rec.Contestants {
		cost, perGame := "free", "—"
		if !c.Usage.IsZero() {
			if c.CostKnown {
				cost = fmt.Sprintf("$%.4f", c.CostUSD)
				perGame = fmt.Sprintf("$%.5f", c.CostPerGameUSD)
			} else {
				cost, perGame = "unknown", "unknown"
			}
			v.HasCost = true
		}
		model := c.Model
		if model == "" {
			model = "deterministic"
		}
		v.Rows = append(v.Rows, reportRow{
			Rank:        i + 1,
			Name:        c.Name,
			Model:       model,
			Condition:   c.Condition,
			Elo:         fmt.Sprintf("%.0f", c.Elo),
			WinRate:     fmt.Sprintf("%.1f%%", 100*c.WinRate),
			Record:      fmt.Sprintf("%d–%d–%d", c.Wins, c.Losses, c.Draws),
			Games:       c.Games,
			Cost:        cost,
			CostPerGame: perGame,
			Top:         i == 0,
			CILeft:      100 * c.CILow,
			CIWidth:     100 * (c.CIHigh - c.CILow),
			Mark:        100 * c.WinRate,
		})
	}
	v.Seeds = rec.Header.Seeds
	if len(rec.Replays) > 0 {
		if b, err := json.Marshal(rec.Replays); err == nil {
			v.ReplaysJSON = template.JS(b)
			v.HasReplays = true
		}
	}
	return v
}

// RenderHTMLReport writes a standalone HTML report for one run to w.
func RenderHTMLReport(w io.Writer, rec RunRecord) error {
	return reportTmpl.Execute(w, buildReportView(rec))
}

var reportTmpl = template.Must(template.New("report").Parse(reportHTML))

const reportHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>PokéArena benchmark — {{.Rec.RunID}}</title>
<style>
  :root {
    --ink: #1a1a2e; --muted: #6b7280; --line: #e5e7eb; --bg: #f7f7fb;
    --card: #ffffff; --accent: #4f46e5; --ci: #c7d2fe; --mark: #4f46e5;
    --free: #059669; --warn: #b45309;
  }
  * { box-sizing: border-box; }
  body {
    margin: 0; padding: 2.5rem 1.25rem; background: var(--bg); color: var(--ink);
    font: 15px/1.55 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
  }
  .wrap { max-width: 940px; margin: 0 auto; }
  h1 { font-size: 1.4rem; margin: 0 0 .25rem; letter-spacing: -.01em; }
  h2 { font-size: 1rem; text-transform: uppercase; letter-spacing: .06em; color: var(--muted); margin: 2.25rem 0 .75rem; }
  .sub { color: var(--muted); margin: 0 0 1.5rem; font-size: .9rem; }
  .num { font-variant-numeric: tabular-nums; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
  .card { background: var(--card); border: 1px solid var(--line); border-radius: 12px; overflow: hidden; }
  table { width: 100%; border-collapse: collapse; }
  th, td { padding: .7rem .9rem; text-align: left; border-bottom: 1px solid var(--line); }
  th { font-size: .72rem; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); font-weight: 600; }
  tr:last-child td { border-bottom: none; }
  tr.top td { background: #fbfbff; }
  .rank { color: var(--muted); width: 2rem; }
  .name { font-weight: 600; }
  .name .model { display: block; font-weight: 400; font-size: .78rem; color: var(--muted); }
  .name .cond { display: inline-block; margin-left: .4rem; padding: .05rem .35rem; border-radius: 4px; font-size: .68rem; font-weight: 600; text-transform: uppercase; letter-spacing: .03em; vertical-align: middle; }
  .name .cond.raw { background: #23324a; color: #9db4d6; }
  .name .cond.cot { background: #3a2f52; color: #c3a8ee; }
  .name .cond.agentic { background: #2f4a37; color: #9ed6ac; }
  .elo { font-weight: 700; }
  .bar { position: relative; height: 22px; min-width: 160px; background: #f1f2f6; border-radius: 5px; }
  .bar .ci { position: absolute; top: 0; bottom: 0; background: var(--ci); border-radius: 5px; }
  .bar .mark { position: absolute; top: -2px; bottom: -2px; width: 2px; background: var(--mark); }
  .bar .lbl { position: absolute; right: 6px; top: 50%; transform: translateY(-50%); font-size: .72rem; color: #3730a3; }
  .free { color: var(--free); }
  .unknown { color: var(--warn); }
  .teams { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: .6rem; }
  .team { border: 1px solid var(--line); border-radius: 10px; padding: .6rem .8rem; background: var(--card); }
  .team .t { font-weight: 600; font-size: .85rem; margin-bottom: .35rem; }
  .chip { display: inline-block; margin: 0 .35rem .3rem 0; padding: .1rem .45rem; border-radius: 999px;
          background: #eef2ff; color: #3730a3; font-size: .78rem; }
  .chip.win { background: var(--accent); color: #fff; }
  .prov { margin-top: 2rem; padding: 1rem 1.1rem; border: 1px dashed var(--line); border-radius: 10px;
          background: #fafafe; font-size: .82rem; color: var(--muted); }
  .prov b { color: var(--ink); font-weight: 600; }
  .prov code { font-family: ui-monospace, Menlo, monospace; font-size: .8rem; }
  /* replay */
  .replay .pick { width: 100%; padding: .5rem .6rem; border: 1px solid var(--line); border-radius: 8px; font-size: .9rem; margin-bottom: .5rem; background: var(--card); color: var(--ink); }
  .replay .cap { color: var(--muted); font-size: .85rem; margin: 0 0 .75rem; }
  .rfield { text-align: center; color: var(--muted); font-size: .8rem; margin: .4rem 0; min-height: 1rem; }
  .rboard { display: grid; grid-template-columns: 1fr 1fr; gap: 1rem; }
  .rside { border: 1px solid var(--line); border-radius: 10px; padding: .8rem; background: var(--card); }
  .rside .trn { font-size: .72rem; text-transform: uppercase; letter-spacing: .05em; color: var(--muted); }
  .rmon { font-weight: 600; margin: .15rem 0; }
  .rmon .ty { font-weight: 400; color: var(--muted); font-size: .8rem; }
  .rtags { min-height: 1.15rem; }
  .rtag { display: inline-block; padding: .03rem .35rem; border-radius: 4px; font-size: .68rem; margin-right: .25rem; }
  .rtag.st { background: #fde68a; color: #92400e; text-transform: uppercase; letter-spacing: .03em; }
  .rtag.bo { background: #ddd6fe; color: #5b21b6; }
  .rhp { position: relative; height: 12px; background: #f1f2f6; border-radius: 6px; overflow: hidden; margin: .35rem 0 .2rem; }
  .rhp > i { position: absolute; left: 0; top: 0; bottom: 0; width: 0; background: var(--free); border-radius: 6px; transition: width .25s ease; }
  .rhp.low > i { background: var(--warn); }
  .rhp.crit > i { background: #dc2626; }
  .rhpn { font-size: .72rem; color: var(--muted); font-variant-numeric: tabular-nums; }
  .rtray { display: flex; gap: .3rem; margin-top: .55rem; }
  .rtray i { width: 14px; height: 14px; border-radius: 50%; background: var(--free); border: 2px solid transparent; }
  .rtray i.low { background: var(--warn); }
  .rtray i.ko { background: #d1d5db; }
  .rtray i.act { border-color: var(--accent); }
  .rwin { text-align: center; font-weight: 600; margin-top: .55rem; color: var(--accent); min-height: 1.2rem; }
  .rlog { background: #0f1222; color: #cbd5e1; border-radius: 8px; padding: .6rem .8rem; margin-top: .8rem;
          font: 12px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; min-height: 4.5rem; max-height: 8.5rem; overflow: auto; }
  .rlog .act { color: #93c5fd; }
  .rlog .start { color: #64748b; }
  .rctrl { display: flex; align-items: center; gap: .5rem; margin-top: .8rem; flex-wrap: wrap; }
  .rctrl button { border: 1px solid var(--line); background: var(--card); border-radius: 7px; padding: .3rem .7rem; font-size: .85rem; cursor: pointer; }
  .rctrl button:hover { background: #f3f4f6; }
  .rctrl input[type=range] { flex: 1; min-width: 120px; }
  .rturn { font-size: .8rem; color: var(--muted); font-variant-numeric: tabular-nums; min-width: 8rem; text-align: right; }
  @media (max-width: 560px) { .rboard { grid-template-columns: 1fr; } }
  .prov .row { margin: .15rem 0; }
  footer { margin-top: 1.5rem; color: var(--muted); font-size: .78rem; text-align: center; }
</style>
</head>
<body>
<div class="wrap">
  <h1>PokéArena battle benchmark</h1>
  <p class="sub">Run <span class="num">{{.Rec.RunID}}</span> · {{.Rec.GeneratedAt}} · {{.Rec.Header.Ruleset}}</p>

  <div class="card">
    <table>
      <thead>
        <tr>
          <th class="rank">#</th><th>agent</th><th>elo</th>
          <th>win rate — 95% CI</th><th>W–L–D</th><th>$/game</th><th>cost</th>
        </tr>
      </thead>
      <tbody>
      {{range .Rows}}
        <tr class="{{if .Top}}top{{end}}">
          <td class="rank num">{{.Rank}}</td>
          <td class="name">{{.Name}}{{if .Condition}}<span class="cond {{.Condition}}">{{.Condition}}</span>{{end}}<span class="model">{{.Model}}</span></td>
          <td class="elo num">{{.Elo}}</td>
          <td>
            <div class="bar">
              <div class="ci" style="left:{{.CILeft}}%;width:{{.CIWidth}}%"></div>
              <div class="mark" style="left:{{.Mark}}%"></div>
              <span class="lbl num">{{.WinRate}}</span>
            </div>
          </td>
          <td class="num">{{.Record}}</td>
          <td class="num">{{.CostPerGame}}</td>
          <td class="num {{if eq .Cost "free"}}free{{else if eq .Cost "unknown"}}unknown{{end}}">{{.Cost}}</td>
        </tr>
      {{end}}
      </tbody>
    </table>
  </div>

  {{if .Rec.PerTeam}}
  <h2>Per-team Elo — does the ranking hold across the library?</h2>
  <div class="teams">
    {{range .Rec.PerTeam}}
    <div class="team">
      <div class="t">{{.Team}}</div>
      {{range $i, $r := .Ranks}}<span class="chip {{if eq $i 0}}win{{end}} num">{{$r.Name}} {{printf "%.0f" $r.Elo}}</span>{{end}}
    </div>
    {{end}}
  </div>
  {{end}}

  {{if .HasReplays}}
  <h2>Watch a battle — highlight replays</h2>
  <div class="card" style="padding:1rem">
    <div class="replay">
      <select id="rpick" class="pick"></select>
      <p id="rcap" class="cap"></p>
      <div id="rfield" class="rfield"></div>
      <div class="rboard">
        <div id="rside0" class="rside"></div>
        <div id="rside1" class="rside"></div>
      </div>
      <div id="rwin" class="rwin"></div>
      <div id="rlog" class="rlog"></div>
      <div class="rctrl">
        <button id="rfirst" title="restart">&#9198;</button>
        <button id="rprev">&#8249; prev</button>
        <button id="rplay">&#9654; play</button>
        <button id="rnext">next &#8250;</button>
        <input id="rslider" type="range" min="0" value="0">
        <span id="rturn" class="rturn"></span>
      </div>
    </div>
  </div>
  <script>
  const REPLAYS = {{.ReplaysJSON}};
  (function(){
    const $ = function(id){ return document.getElementById(id); };
    const pick = $('rpick'), cap = $('rcap'), field = $('rfield');
    const sideEl = [$('rside0'), $('rside1')];
    const winEl = $('rwin'), logEl = $('rlog'), slider = $('rslider'), turnEl = $('rturn');
    const btnPlay = $('rplay');
    let cur = 0, fi = 0, timer = null;

    REPLAYS.forEach(function(r, i){
      const o = document.createElement('option');
      o.value = i;
      o.textContent = r.title + '  —  ' + r.match + ' on ' + r.team;
      pick.appendChild(o);
    });

    function esc(s){ return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }
    function hpClass(pct){ return pct <= 20 ? 'crit' : (pct <= 50 ? 'low' : ''); }

    function renderSide(el, s, name){
      const m = s.active || {};
      const pct = m.max_hp > 0 ? Math.round(100 * m.hp / m.max_hp) : 0;
      let tags = '';
      if (m.status) tags += '<span class="rtag st">' + esc(m.status) + '</span>';
      if (m.boosts) tags += '<span class="rtag bo">' + esc(m.boosts) + '</span>';
      let tray = '';
      (s.tray || []).forEach(function(t){
        const cls = t.fainted ? 'ko' : (t.hp_pct <= 50 ? 'low' : '');
        tray += '<i class="' + cls + (t.active ? ' act' : '') + '" title="' + esc(t.name) + ' ' + t.hp_pct + '%"></i>';
      });
      el.innerHTML =
        '<div class="trn">' + esc(name) + '</div>' +
        '<div class="rmon">' + esc(m.name || '—') + ' <span class="ty">' + esc(m.types || '') + '</span></div>' +
        '<div class="rtags">' + tags + '</div>' +
        '<div class="rhp ' + hpClass(pct) + '"><i style="width:' + pct + '%"></i></div>' +
        '<div class="rhpn">' + (m.hp || 0) + '/' + (m.max_hp || 0) + ' (' + pct + '%)</div>' +
        '<div class="rtray">' + tray + '</div>';
    }

    function render(){
      const r = REPLAYS[cur], f = r.frames[fi];
      const names = [r.side0, r.side1];
      field.textContent = f.field || '';
      renderSide(sideEl[0], f.sides[0], names[0]);
      renderSide(sideEl[1], f.sides[1], names[1]);
      let html = '';
      (f.actions || []).forEach(function(a, side){
        if (a) html += '<div class="act">' + esc(names[side]) + "'s " + esc((f.sides[side].active || {}).name || '') + ' ' + esc(a) + '</div>';
      });
      (f.log || []).forEach(function(l){ html += '<div>' + esc(l) + '</div>'; });
      logEl.innerHTML = html || '<div class="start">— battle start —</div>';
      logEl.scrollTop = logEl.scrollHeight;
      slider.value = fi;
      turnEl.textContent = 'turn ' + f.turn + ' · frame ' + (fi + 1) + '/' + r.frames.length;
      winEl.textContent = (fi === r.frames.length - 1)
        ? (r.winner === 'draw' ? 'Draw — double KO' : 'Winner: ' + r.winner) : '';
    }

    function stop(){ if (timer){ clearInterval(timer); timer = null; btnPlay.innerHTML = '&#9654; play'; } }
    function load(i){ stop(); cur = i; fi = 0; const r = REPLAYS[i]; cap.textContent = r.title; slider.max = r.frames.length - 1; render(); }
    function step(d){ stop(); const r = REPLAYS[cur]; fi = Math.max(0, Math.min(r.frames.length - 1, fi + d)); render(); }
    function play(){
      if (timer){ stop(); return; }
      const r = REPLAYS[cur];
      if (fi >= r.frames.length - 1) fi = 0;
      btnPlay.innerHTML = '&#9208; pause';
      timer = setInterval(function(){
        if (fi >= REPLAYS[cur].frames.length - 1){ stop(); return; }
        fi++; render();
      }, 1100);
    }

    pick.addEventListener('change', function(e){ load(+e.target.value); });
    $('rfirst').addEventListener('click', function(){ stop(); fi = 0; render(); });
    $('rprev').addEventListener('click', function(){ step(-1); });
    $('rnext').addEventListener('click', function(){ step(1); });
    btnPlay.addEventListener('click', play);
    slider.addEventListener('input', function(e){ stop(); fi = +e.target.value; render(); });

    load(0);
  })();
  </script>
  {{end}}

  <div class="prov">
    <div class="row"><b>Engine</b> <code>{{.Rec.Header.EngineRevision}}</code></div>
    <div class="row"><b>Dataset</b> sim <code>{{.Rec.Header.DataSimVersion}}</code>, gen {{.Rec.Header.DataSourceGen}}, curation <code>{{.Rec.Header.DataCurationSHA}}</code></div>
    <div class="row"><b>Library</b> {{.Rec.Header.TeamLibrary}} · {{len .Rec.Header.Teams}} teams · {{.Rec.Header.GamesPerPairing}} seeds × {{.Rec.Header.Orientations}} orientations · seeds {{.Seeds}}</div>
    <div class="row"><b>Total cost</b> <span class="num">${{printf "%.4f" .Rec.TotalCostUSD}}</span></div>
  </div>

  <footer>
    Deterministic contestants reproduce byte-for-byte from these seeds — re-run the same config to confirm every number.
  </footer>
</div>
</body>
</html>
`
