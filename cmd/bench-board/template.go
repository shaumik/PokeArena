package main

// boardHTML is the standalone, script-free board. Dynamic geometry (bar widths,
// CI whisker offsets) is inlined as CSS custom properties per row; everything
// else is static. One external font and the PokéAPI sprite CDN carry the theme.
//
// Layout is a fixed 3-column grid — [who | plot | score] — so the win% and its
// CI read down a clean right-hand gutter and never collide with the whiskers.
const boardHTML = `{{define "row"}}
    <div class="row">
      <div class="who">
        <span class="rank">{{.Rank}}</span>
        <img class="sprite" loading="lazy" alt="" src="{{.SpriteURL}}">
        <span class="txt">
          <div class="name">{{.Name}}</div>
          <div class="arm">{{.ArmLabel}}</div>
        </span>
      </div>
      <div class="plot" style="--bar:{{printf "%.1f" .BarPct}}%; --cil:{{printf "%.1f" .CILeftPct}}%; --ciw:{{printf "%.1f" .CIWidthPct}}%; --c1:{{.Color}}; --c2:{{.Color}}">
        <div class="gl" style="left:25%"></div>
        <div class="gl" style="left:50%"></div>
        <div class="gl" style="left:75%"></div>
        <div class="ref"></div>
        <div class="track"></div>
        <div class="bar"></div>
        <div class="ci"></div>
      </div>
      <div class="score">
        <span class="p">{{.Pct}}</span>
        <span class="band">95% CI [{{printf "%.0f" .CILowPctNum}}–{{printf "%.0f" .CIHighPctNum}}]</span>
        <span class="rec">{{.Record}} · n={{.N}}</span>
      </div>
    </div>
{{end}}<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Press+Start+2P&family=Inter:wght@400;600;800&display=swap" rel="stylesheet">
<style>
  :root{
    --bg:#0c1018; --panel:#141b2b; --panel2:#0f1524; --ink:#eef3fb; --muted:#8ea0bd;
    --line:#243044; --grid:#1b2536; --accent:#ffcb05; --accent2:#3d7dca;
    --track:#0c111c;
    --colL:236px; --colR:118px;      /* who column / score column widths */
  }
  *{box-sizing:border-box}
  body{
    margin:0; background:
      radial-gradient(1200px 500px at 80% -10%, #1a2b4d 0, transparent 60%),
      radial-gradient(900px 500px at -10% 10%, #17213a 0, transparent 55%),
      var(--bg);
    color:var(--ink); font-family:Inter,system-ui,sans-serif; line-height:1.4;
    -webkit-font-smoothing:antialiased;
  }
  .wrap{max-width:1000px; margin:0 auto; padding:32px 20px 64px}

  /* ---- header ---- */
  header{display:flex; align-items:center; gap:18px; margin-bottom:6px}
  .pokeball{
    width:46px;height:46px;border-radius:50%;flex:0 0 auto;
    background:linear-gradient(#ef5350 0 49%, #111 49% 51%, #f7f7f7 51% 100%);
    border:3px solid #111; position:relative; box-shadow:0 4px 18px rgba(239,83,80,.35);
  }
  .pokeball::after{content:"";position:absolute;top:50%;left:50%;width:14px;height:14px;
    transform:translate(-50%,-50%);background:#f7f7f7;border:3px solid #111;border-radius:50%}
  h1{font-family:"Press Start 2P",monospace; font-size:20px; margin:0; letter-spacing:.5px;
    text-shadow:0 2px 0 #0008, 3px 3px 0 var(--accent2)}
  .sub{color:var(--muted); font-size:14px; margin:8px 0 2px}
  .sub b{color:var(--ink)}
  .metric{display:inline-block;margin-top:12px;padding:7px 12px;border-radius:999px;
    background:linear-gradient(90deg,#3d7dca22,#ffcb0522);border:1px solid var(--line);
    font-size:12.5px;color:var(--muted)}
  .metric b{color:var(--accent)}

  /* ---- legend ---- */
  .legend{display:flex;flex-wrap:wrap;gap:14px;margin:18px 0 10px}
  .legend .item{display:flex;align-items:center;gap:7px;font-size:12.5px;color:var(--muted)}
  .legend .swatch{width:13px;height:13px;border-radius:3px;box-shadow:0 0 0 1px #0006 inset}

  /* ---- chart ---- */
  .board{background:linear-gradient(180deg,var(--panel),var(--panel2));
    border:1px solid var(--line); border-radius:16px; padding:10px 18px 22px;
    box-shadow:0 20px 60px -30px #000}

  /* every band shares one grid so axis, rows and score gutter align */
  .axis,.row{display:grid;grid-template-columns:var(--colL) 1fr var(--colR);align-items:center}
  .axis{height:22px;margin:4px 0 2px}
  .scale{position:relative;height:22px;grid-column:2}
  .scale .tick{position:absolute;top:2px;transform:translateX(-50%);font-size:11px;color:var(--muted)}

  .row{padding:10px 0;border-top:1px solid var(--grid)}
  .row:first-of-type{border-top:none}

  /* col 1 — who */
  .who{display:flex;align-items:center;gap:11px;min-width:0}
  .rank{font-family:"Press Start 2P",monospace;font-size:11px;color:var(--muted);width:20px;text-align:right}
  .sprite{width:46px;height:46px;image-rendering:pixelated;flex:0 0 auto;
    filter:drop-shadow(0 3px 4px #0007)}
  .who .txt{min-width:0}
  .name{font-weight:800;font-size:15px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
  .arm{font-size:11px;color:var(--muted);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;margin-top:1px}

  /* col 2 — plot */
  .plot{position:relative;height:34px}
  .gl{position:absolute;top:-2px;bottom:-2px;width:1px;background:var(--grid)}
  .ref{position:absolute;top:-2px;bottom:-2px;left:50%;width:2px;
    background:repeating-linear-gradient(#ffcb0577 0 6px,transparent 6px 12px)}
  .track{position:absolute;left:0;right:0;top:9px;height:16px;background:var(--track);
    border-radius:8px;overflow:hidden;box-shadow:inset 0 0 0 1px #0006}
  .bar{position:absolute;left:0;top:9px;height:16px;border-radius:8px;
    width:var(--bar);background:linear-gradient(90deg,var(--c1),var(--c2));
    box-shadow:0 0 14px -2px var(--c2)}
  /* Wilson 95% CI whisker, drawn over the bar */
  .ci{position:absolute;top:17px;height:0;left:var(--cil);width:var(--ciw);
    border-top:2px solid #eef3fbe0}
  .ci::before,.ci::after{content:"";position:absolute;top:-4px;height:8px;width:2px;background:#eef3fbe0}
  .ci::before{left:0}.ci::after{right:0}

  /* col 3 — score gutter */
  .score{text-align:right;padding-left:16px;white-space:nowrap}
  .score .p{font-weight:800;font-size:18px;letter-spacing:.3px}
  .score .band{display:block;color:var(--muted);font-size:11px;font-weight:600;margin-top:2px}
  .score .rec{display:block;color:var(--muted);font-size:10.5px;font-weight:600;opacity:.8;margin-top:1px}

  /* section head + divider between the two (non-comparable) arms */
  .section{display:flex;align-items:baseline;gap:10px;margin:14px 0 4px;padding:0 2px}
  .section .tag{font-family:"Press Start 2P",monospace;font-size:10px;color:var(--ink);
    padding:5px 9px;border-radius:6px;background:#ffffff10;border:1px solid var(--line)}
  .section .note{font-size:11.5px;color:var(--muted)}
  .section.showcase .tag{color:#0c111c;background:var(--accent)}
  .divider{height:1px;background:linear-gradient(90deg,transparent,var(--line) 12%,var(--line) 88%,transparent);margin:16px 0 2px}

  footer{margin-top:22px;color:var(--muted);font-size:12px;line-height:1.6}
  footer .caveat{border-left:3px solid var(--accent);padding:8px 0 8px 12px;margin:12px 0;background:#ffcb050a}
  code{background:#0009;padding:1px 6px;border-radius:5px;color:#cfe0ff;font-size:11.5px}

  @media (max-width:720px){
    :root{--colL:150px;--colR:96px}
    .sprite{width:36px;height:36px}
    .name{font-size:13px}
  }
</style>
</head>
<body>
<div class="wrap">
  <header>
    <div class="pokeball"></div>
    <div>
      <h1>{{.Title}}</h1>
    </div>
  </header>
  <div class="sub">One question, two arms: <b>how often each trainer beats a strong expectimax bot</b>. 6v6, Gen-1 dex, Level 50, no items. <b>The two sections are ranked separately</b> — they face different opponents and teams (see below).</div>
  <div class="metric">metric: <b>win rate</b> &nbsp;·&nbsp; bar = win% &nbsp;·&nbsp; whisker = Wilson 95% CI</div>

  <div class="legend">
    {{range .Legend}}<div class="item"><span class="swatch" style="background:{{.Color}}"></span>{{.Label}}</div>{{end}}
    <div class="item"><span class="swatch" style="background:repeating-linear-gradient(90deg,#ffcb05 0 3px,transparent 3px 6px)"></span>even with the AI (50%)</div>
  </div>

  <div class="board">
    <div class="axis">
      <div class="scale">
        <span class="tick" style="left:0%">0%</span>
        <span class="tick" style="left:25%">25</span>
        <span class="tick" style="left:50%">50</span>
        <span class="tick" style="left:75%">75</span>
        <span class="tick" style="left:100%">100%</span>
      </div>
    </div>
    {{if .Baselines}}
    <div class="section">
      <span class="tag">BASELINES</span>
      <span class="note">reproducible round-robin · vs fixed <code>{{.Ref}}</code> · mirror matches · n≈240</span>
    </div>
    {{range .Baselines}}{{template "row" .}}{{end}}
    {{end}}

    {{if .Agentic}}
    <div class="divider"></div>
    <div class="section showcase">
      <span class="tag">AGENTIC SHOWCASE</span>
      <span class="note">live harness · vs the in-engine AI (adaptive expectimax d3) · non-mirror teams · small n — ranked separately, not comparable to baselines</span>
    </div>
    {{range .Agentic}}{{template "row" .}}{{end}}
    {{end}}
  </div>

  <footer>
    <div class="caveat">
      <b>Two arms, not one axis.</b> The sections are ranked separately because they are
      different measurements. <b>Baselines</b> play their head-to-head vs a <b>fixed
      depth-2</b> <code>{{.Ref}}</code> in <b>mirror matches</b> (both sides the same team),
      n≈240. <b>Agentic</b> harnesses play the live in-engine AI — an <b>adaptive expectimax
      (depth-3, time-bounded)</b> — on <b>non-mirror</b> curated teams, n=20–58. Different
      opponent, different teams, different sample size: read the agentic strip as a
      showcase, not a bit-for-bit continuation of the baseline board.
    </div>
    <div class="caveat">
      <b>Contamination.</b> Models carry Pokémon knowledge from pretraining, but the
      <b>format is custom</b> (Gen-1 pool, full modern movepools, no items, level 50) so the
      metagame can't be memorized, and the task is <b>tactical play under fog-of-war</b>, not
      trivia — a strong prior on species doesn't hand you the right switch.
    </div>
    Generated {{.GeneratedAt}} · win rate with Wilson 95% CI · unfinished agentic battles
    (<i>dnf</i>) are excluded from the decided denominator, shown for transparency.
    Sprites © Nintendo/Game Freak via PokéAPI — non-commercial fan project.
  </footer>
</div>
</body>
</html>`
