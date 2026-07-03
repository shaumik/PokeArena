package eval

import (
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
          <td class="name">{{.Name}}<span class="model">{{.Model}}</span></td>
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
