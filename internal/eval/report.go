package eval

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
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
	// Reference marks a yardstick contestant with no record of its own (0 games).
	// It is not expandable — its "matchups" would just invert every other row.
	Reference bool
	// CI bar geometry, in percent of the 0..100% win-rate axis.
	CILeft  float64
	CIWidth float64
	Mark    float64
}

type reportView struct {
	Rec     RunRecord
	Rows    []reportRow
	HasCost bool
	// RulesetParts is the ruleset string split into pills for the masthead.
	RulesetParts []string
	// ReplaysJSON is the embedded battle data the in-page replayer reads. It is
	// template.JS so it drops into a <script> as a literal; json.Marshal escapes
	// <, >, and & to \u00xx by default, so it can't break out of the tag.
	HasReplays  bool
	ReplaysJSON template.JS
	HasMatrix   bool
	MatrixJSON  template.JS
	HasRosters  bool
	RostersJSON template.JS
	// Sprites maps national-dex number (as a string, for JSON) to a base64 data:
	// URI of that Pokémon's vendored sprite, for every mon on a revealed roster.
	// Inlined so the report shows sprites while fetching nothing. Empty when no
	// sprite is embedded for any roster mon.
	HasSprites  bool
	SpritesJSON template.JS
	// Samples are the per-model sample battles: each model-backed contestant's
	// row expands to its reconstructed per-team win/loss replays instead of the
	// single vs-reference chip. Keyed by contestant name; values are chips a
	// viewer can click to load. Empty for a run with no model replays.
	HasSamples  bool
	SamplesJSON template.JS
	// DQRows is the per-model decision-quality table, sorted best (fewest
	// blunders) first. Empty when the run carries no decision-quality data.
	HasDecisionQuality bool
	DQRows             []dqRow
	// Human-facing run summary for the masthead — meaningful to a viewer, unlike
	// the raw run id.
	GeneratedHuman string
	TotalGames     int
	NAgents        int
	NTeams         int
	RepoURL        string
}

// dqRow is one model's decision-quality line, preformatted for the template: the
// share of choices that blundered (the headline, with a bar), how typical a
// shortfall looked (median regret), how often it matched the oracle's top pick,
// and win rate for context.
type dqRow struct {
	Model        string
	Games        int
	WinRate      string
	Decisions    int
	BlunderRate  string
	BlunderBar   float64 // 0–100, for the inline bar
	MatchRate    string
	MedianRegret string
	Best         bool // lowest blunder rate — the cleanest decision-maker
}

// sampleChip is one clickable battle in a model's sample strip: the team it was
// played on, whether the model won or lost, and the index of the embedded replay
// to load. Derived from the replays themselves, so richer coverage (more
// reconstructed per-team battles) shows up with no extra plumbing.
type sampleChip struct {
	Team    string `json:"team"`
	Outcome string `json:"outcome"` // "win" | "loss", from the model's point of view
	Replay  int    `json:"replay"`
}

// repoURL is the project's canonical source, linked from the report so a
// published page points back at the code and data behind its numbers.
const repoURL = "https://github.com/shaumik/PokeArena"

func buildReportView(rec RunRecord) reportView {
	v := reportView{Rec: rec, RepoURL: repoURL}
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
		// A contestant with no games is a reference/yardstick (it has no record of
		// its own), so it reads as such rather than as a literal "0–0–0" at 0%.
		winRate := fmt.Sprintf("%.1f%%", 100*c.WinRate)
		record := fmt.Sprintf("%d–%d–%d", c.Wins, c.Losses, c.Draws)
		if c.Games == 0 {
			winRate, record = "ref", "reference"
		}
		v.Rows = append(v.Rows, reportRow{
			Rank:        i + 1,
			Name:        c.Name,
			Model:       model,
			Condition:   c.Condition,
			Elo:         fmt.Sprintf("%.0f", c.Elo),
			WinRate:     winRate,
			Record:      record,
			Games:       c.Games,
			Cost:        cost,
			CostPerGame: perGame,
			Top:         i == 0,
			Reference:   c.Games == 0,
			CILeft:      100 * c.CILow,
			CIWidth:     100 * (c.CIHigh - c.CILow),
			Mark:        100 * c.WinRate,
		})
	}
	if rec.Header.Ruleset != "" {
		v.RulesetParts = strings.Split(rec.Header.Ruleset, ", ")
	}
	if len(rec.Replays) > 0 {
		if b, err := json.Marshal(rec.Replays); err == nil {
			v.ReplaysJSON = template.JS(b)
			v.HasReplays = true
		}
	}

	// Group each model-backed contestant's reconstructed replays into a sample
	// strip. A replay whose side 0 is a model contestant is one of that model's
	// samples; the outcome is read from its winner, so a win and a loss on the
	// same team sit side by side. Indices match the marshaled rec.Replays above.
	modelName := map[string]bool{}
	for _, c := range rec.Contestants {
		if c.Model != "" {
			modelName[c.Name] = true
		}
	}
	samples := map[string][]sampleChip{}
	for idx, rep := range rec.Replays {
		if !modelName[rep.Side0] {
			continue
		}
		outcome := "loss"
		if rep.Winner == rep.Side0 {
			outcome = "win"
		}
		samples[rep.Side0] = append(samples[rep.Side0], sampleChip{Team: rep.Team, Outcome: outcome, Replay: idx})
	}
	// Order each strip by team, win before loss, so it reads as a tidy grid.
	for name := range samples {
		s := samples[name]
		sort.SliceStable(s, func(i, j int) bool {
			if s[i].Team != s[j].Team {
				return s[i].Team < s[j].Team
			}
			return s[i].Outcome == "win" && s[j].Outcome == "loss"
		})
	}
	if len(samples) > 0 {
		if b, err := json.Marshal(samples); err == nil {
			v.SamplesJSON = template.JS(b)
			v.HasSamples = true
		}
	}
	if rec.Matrix != nil && len(rec.Matrix.Agents) > 0 {
		if b, err := json.Marshal(rec.Matrix); err == nil {
			v.MatrixJSON = template.JS(b)
			v.HasMatrix = true
		}
	}
	if len(rec.Rosters) > 0 {
		if b, err := json.Marshal(rec.Rosters); err == nil {
			v.RostersJSON = template.JS(b)
			v.HasRosters = true
		}
		// Inline the sprite for every distinct roster mon (keyed by dex number so
		// the same species shared across teams is embedded once). Missing sprites
		// are simply skipped — the medallion falls back to a monogram.
		sprites := map[string]string{}
		for _, tr := range rec.Rosters {
			for _, m := range tr.Members {
				key := strconv.Itoa(m.DexNo)
				if m.DexNo == 0 || sprites[key] != "" {
					continue
				}
				if uri := spriteDataURI(m.DexNo); uri != "" {
					sprites[key] = uri
				}
			}
		}
		if len(sprites) > 0 {
			if b, err := json.Marshal(sprites); err == nil {
				v.SpritesJSON = template.JS(b)
				v.HasSprites = true
			}
		}
	}

	// Decision-quality table: fewest blunders first (AggregateByModel already
	// sorts this way; re-sort defensively in case the data was hand-assembled).
	if len(rec.DecisionQuality) > 0 {
		dq := append([]ModelStats(nil), rec.DecisionQuality...)
		sort.SliceStable(dq, func(i, j int) bool { return dq[i].BlunderRate < dq[j].BlunderRate })
		for i, s := range dq {
			v.DQRows = append(v.DQRows, dqRow{
				Model:        s.Model,
				Games:        s.Games,
				WinRate:      fmt.Sprintf("%.0f%%", 100*s.WinRate),
				Decisions:    s.Decisions,
				BlunderRate:  fmt.Sprintf("%.0f%%", 100*s.BlunderRate),
				BlunderBar:   100 * s.BlunderRate,
				MatchRate:    fmt.Sprintf("%.0f%%", 100*s.MatchRate),
				MedianRegret: fmt.Sprintf("%.0f", s.MedianRegret),
				Best:         i == 0 && len(dq) > 1,
			})
		}
		v.HasDecisionQuality = len(v.DQRows) > 0
	}

	// Human run summary: readable date, and the run's scale from the header.
	v.NAgents = len(rec.Header.Contestants)
	v.NTeams = len(rec.Header.Teams)
	if v.NAgents > 1 {
		pairs := v.NAgents * (v.NAgents - 1) / 2
		v.TotalGames = rec.Header.GamesPerPairing * rec.Header.Orientations * pairs * v.NTeams
	}
	v.GeneratedHuman = rec.GeneratedAt
	if t, err := time.Parse(time.RFC3339, rec.GeneratedAt); err == nil {
		v.GeneratedHuman = t.Format("January 2, 2006")
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
  .masthead { display: flex; align-items: center; justify-content: space-between; gap: 1rem 1.5rem; flex-wrap: wrap;
    padding: 1.4rem 1.6rem; border-radius: 16px; margin-bottom: .75rem;
    background: linear-gradient(115deg, #eceffe 0%, #f7f7fb 55%); border: 1px solid var(--line); }
  .mh-title { display: flex; align-items: center; gap: 1rem; }
  .mh-title h1 { font-size: 1.6rem; margin: 0; letter-spacing: -.02em; font-weight: 800; }
  .mh-title h1 span { color: var(--accent); }
  .mh-title .tag { margin: .2rem 0 0; color: var(--muted); font-size: .86rem; max-width: 42ch; }
  .mh-side { text-align: right; }
  .mh-when { font-size: 1rem; font-weight: 700; color: var(--ink); }
  .mh-scale { font-size: .82rem; color: var(--accent); font-weight: 600; margin-top: .1rem; }
  .mh-repo { display: inline-flex; align-items: center; gap: .35rem; margin-bottom: .45rem; font-size: .76rem;
    color: var(--muted); text-decoration: none; font-weight: 600; }
  .mh-repo:hover { color: var(--ink); }
  .mh-repo svg { opacity: .85; }
  .pokeball { flex: none; width: 38px; height: 38px; border-radius: 50%; border: 3px solid #16181d; position: relative;
    background: linear-gradient(#ef4444 0 50%, #fff 50% 100%); box-shadow: 0 3px 8px rgba(0,0,0,.15); }
  .pokeball::before { content: ""; position: absolute; left: -3px; right: -3px; top: 50%; transform: translateY(-50%); height: 3px; background: #16181d; }
  .pokeball::after { content: ""; position: absolute; top: 50%; left: 50%; transform: translate(-50%,-50%); width: 11px; height: 11px; border-radius: 50%; background: #fff; border: 3px solid #16181d; }
  .rules { display: flex; flex-wrap: wrap; gap: .35rem; margin: 0 0 1.5rem; }
  .rule { font-size: .72rem; color: #4338ca; background: #eef2ff; border: 1px solid #e0e7ff; padding: .16rem .55rem; border-radius: 999px; }
  .abstract { margin: 0 0 2rem; }
  .abstract .lead { font-size: 1.04rem; line-height: 1.62; color: #2b2b40; margin: .2rem 0 1.1rem; max-width: 72ch; }
  .abstract .lead b { color: var(--ink); font-weight: 700; }
  .facets { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: .7rem; }
  .facet { background: var(--card); border: 1px solid var(--line); border-radius: 12px; padding: .85rem 1rem; }
  .facet h3 { margin: 0 0 .35rem; font-size: .68rem; text-transform: uppercase; letter-spacing: .07em; color: var(--accent); }
  .facet p { margin: 0; font-size: .84rem; color: #4b5563; line-height: 1.5; }
  .abstract .eyebrow { font-size: .68rem; text-transform: uppercase; letter-spacing: .12em; color: var(--accent); font-weight: 700; }
  .abstract .probes { margin: .9rem 0 0; font-size: .82rem; color: var(--muted); line-height: 1.5; }
  .abstract .probes b { color: var(--ink); font-weight: 600; }
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
  .name .cond.cleanest { background: #234a3a; color: #8fe0bf; }
  .elo { font-weight: 700; }
  .bar { position: relative; height: 22px; min-width: 160px; background: #f1f2f6; border-radius: 5px; }
  .bar .ci { position: absolute; top: 0; bottom: 0; background: var(--ci); border-radius: 5px; }
  .bar .mark { position: absolute; top: -2px; bottom: -2px; width: 2px; background: var(--mark); }
  .bar .lbl { position: absolute; right: 6px; top: 50%; transform: translateY(-50%); font-size: .72rem; color: #3730a3; }
  .dq-lead { font-size: .95rem; line-height: 1.6; color: #2b2b40; margin: .1rem 0 1rem; max-width: 76ch; }
  .dq-note { font-size: .8rem; line-height: 1.55; color: var(--muted); margin: .9rem 0 0; max-width: 76ch; }
  table.dq td.name { font-weight: 600; }
  .dq-bar .fill { position: absolute; top: 0; bottom: 0; left: 0; background: linear-gradient(90deg, #f0a24d, #e0603f); border-radius: 5px; opacity: .85; }
  .dq-bar .lbl { color: #7a3b1e; }
  .free { color: var(--free); }
  .unknown { color: var(--warn); }
  .teams { display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); gap: .6rem; }
  .team { border: 1px solid var(--line); border-radius: 10px; padding: .6rem .8rem; background: var(--card); }
  .team .t { font-weight: 600; font-size: .85rem; margin-bottom: .35rem; }
  .chip { display: inline-block; margin: 0 .35rem .3rem 0; padding: .1rem .45rem; border-radius: 999px;
          background: #eef2ff; color: #3730a3; font-size: .78rem; }
  .chip.win { background: var(--accent); color: #fff; }
  /* leaderboard as the battle picker */
  .lrow.clickable { cursor: pointer; }
  .lrow.clickable:hover td { background: #f4f5ff; }
  .lrow.open td { background: #f0f1ff; }
  .hh-toggle { display: inline-block; margin-left: .55rem; font-size: .66rem; font-weight: 600; color: var(--accent);
    text-transform: uppercase; letter-spacing: .04em; vertical-align: middle; opacity: .8; }
  .lrow.open .hh-toggle { opacity: 1; }
  .hh-row td { padding: 0; background: #fbfbff; border-bottom: 1px solid var(--line); }
  .hh { display: flex; flex-wrap: wrap; gap: .4rem; padding: .7rem .95rem; }
  .hchip { display: inline-flex; align-items: center; gap: .4rem; font-size: .76rem; border: 1px solid var(--line);
    border-radius: 999px; padding: .22rem .65rem; background: #fff; transition: border-color .12s, box-shadow .12s; }
  .hchip.playable { cursor: pointer; }
  .hchip.playable:hover { border-color: var(--accent); box-shadow: 0 0 0 2px #e0e7ff; }
  .hchip .wr { font-weight: 700; font-variant-numeric: tabular-nums; }
  .hchip.win .wr { color: var(--free); }
  .hchip.lose .wr { color: #dc2626; }
  .hchip .play { color: var(--accent); font-size: .7rem; }
  .hh-empty { color: var(--muted); font-size: .78rem; padding: .4rem; }
  /* per-team roster reveal */
  .team .t { cursor: default; }
  .team .reveal { float: right; font-size: .66rem; font-weight: 600; color: var(--accent); text-transform: uppercase;
    letter-spacing: .04em; cursor: pointer; opacity: .85; }
  .team .roster { margin-top: .5rem; border-top: 1px dashed var(--line); padding-top: .5rem; }
  .rmon { display: flex; align-items: center; gap: .45rem; font-size: .8rem; padding: .12rem 0; }
  .rmon .rspr { width: 42px; height: 42px; image-rendering: pixelated; flex: none; margin: -.2rem 0; }
  .rmon .rn { font-weight: 600; min-width: 6.5rem; }
  .rmon .rtype { font-size: .58rem; text-transform: uppercase; letter-spacing: .03em; font-weight: 700; color: #fff; padding: .06rem .35rem; border-radius: 999px; }
  .rmon .bst { margin-left: auto; color: var(--muted); font-size: .72rem; font-variant-numeric: tabular-nums; }
  /* replay — cinematic battle stage */
  .stage { position: relative; margin-top: .5rem; border-radius: 16px; overflow: hidden; color: #e6ecff;
    background: radial-gradient(130% 100% at 50% -25%, #1b2748 0%, #0d1223 55%, #080b14 100%);
    border: 1px solid #1e2a44; box-shadow: 0 24px 70px -28px rgba(0,0,0,.7); }
  .stage-top { display: flex; align-items: center; justify-content: space-between; gap: .75rem;
    padding: .85rem 1.1rem; border-bottom: 1px solid #17223b; }
  .stage-title { font-weight: 700; font-size: .95rem; }
  .stage-title small { display: block; color: #8394bd; font-weight: 400; font-size: .72rem; margin-top: .1rem; }
  .stage-pick { background: #101a2e; color: #dbe4ff; border: 1px solid #263453; border-radius: 8px;
    padding: .4rem .6rem; font-size: .82rem; max-width: 60%; }
  .arena { display: grid; grid-template-columns: 1fr auto 1fr; align-items: start; gap: .5rem; padding: 1.5rem 1.2rem 1rem; }
  .cbt { display: flex; flex-direction: column; min-width: 0; }
  .cbt.c1 { align-items: flex-end; text-align: right; }
  .cbt .trn { font-size: .66rem; text-transform: uppercase; letter-spacing: .13em; color: #7c8cb8; }
  .cbt .med { width: 62px; height: 62px; border-radius: 50%; margin: .4rem 0 .1rem; display: flex; align-items: center;
    justify-content: center; font-weight: 800; font-size: 1rem; color: #fff;
    background: radial-gradient(circle at 34% 28%, rgba(255,255,255,.4), transparent 60%), var(--tc, #4a5a86);
    box-shadow: 0 0 0 2px rgba(255,255,255,.08), 0 0 26px -2px var(--tc, #4a5a86);
    transition: box-shadow .35s, background .35s, filter .35s; }
  .cbt .med img { width: 54px; height: 54px; image-rendering: pixelated; }
  .cbt.c1 .med img { transform: scaleX(-1); }
  .cbt.faint .med { filter: grayscale(1) brightness(.55); box-shadow: none; }
  .cbt .nm { font-weight: 700; font-size: 1.05rem; letter-spacing: -.01em; }
  .cbt .ty { display: flex; gap: .25rem; margin: .3rem 0; }
  .cbt.c1 .ty { justify-content: flex-end; }
  .typ { font-size: .58rem; text-transform: uppercase; letter-spacing: .04em; font-weight: 700; color: #fff; padding: .1rem .42rem; border-radius: 999px; }
  .hprow { display: flex; align-items: center; gap: .5rem; width: 100%; margin-top: .1rem; }
  .cbt.c1 .hprow { flex-direction: row-reverse; }
  .hpbar { position: relative; flex: 1; height: 14px; background: #0a0f1c; border-radius: 8px; overflow: hidden; box-shadow: inset 0 0 0 1px #1c2740; }
  .hpbar > i { position: absolute; top: 0; bottom: 0; left: 0; width: 0; border-radius: 8px;
    background: linear-gradient(90deg, #16a34a, #4ade80); box-shadow: 0 0 12px -1px #22c55e; transition: width .5s cubic-bezier(.4,0,.2,1); }
  .cbt.c1 .hpbar > i { left: auto; right: 0; }
  .hpbar.low > i { background: linear-gradient(90deg, #d97706, #fbbf24); box-shadow: 0 0 12px -1px #f59e0b; }
  .hpbar.crit > i { background: linear-gradient(90deg, #dc2626, #f87171); box-shadow: 0 0 12px -1px #ef4444; }
  .hpbar.hit { animation: hitflash .5s ease; }
  @keyframes hitflash { 0% { box-shadow: inset 0 0 0 1px #1c2740, 0 0 0 4px rgba(239,68,68,.55); } 100% { box-shadow: inset 0 0 0 1px #1c2740; } }
  .hpnum { font-size: .72rem; font-variant-numeric: tabular-nums; color: #aeb9dc; min-width: 2.7rem; }
  .cbt.c1 .hpnum { text-align: right; }
  .tags { min-height: 1.15rem; margin-top: .35rem; display: flex; gap: .25rem; }
  .cbt.c1 .tags { justify-content: flex-end; }
  .tag { font-size: .62rem; padding: .07rem .4rem; border-radius: 5px; font-weight: 600; }
  .tag.st { background: #3a2a12; color: #fbbf24; text-transform: uppercase; letter-spacing: .05em; }
  .tag.bo { background: #241a3a; color: #c4b5fd; }
  .tray { display: flex; gap: .3rem; margin-top: .55rem; }
  .cbt.c1 .tray { flex-direction: row-reverse; }
  .tray i { width: 12px; height: 12px; border-radius: 50%; background: #3a6d4e; box-shadow: inset 0 0 0 1px rgba(255,255,255,.12); transition: transform .3s, background .3s; }
  .tray i.low { background: #9a6a1e; }
  .tray i.ko { background: #2a3350; animation: kopop .4s ease; }
  .tray i.act { box-shadow: 0 0 0 2px var(--tc, #6f7fb0), 0 0 8px -1px var(--tc, #6f7fb0); }
  @keyframes kopop { 0% { transform: scale(1.6); } 60% { transform: scale(.8); } 100% { transform: scale(1); } }
  .vs { display: flex; flex-direction: column; align-items: center; gap: .45rem; padding: .5rem .3rem 0; }
  .vs .b { font-weight: 800; font-size: .78rem; letter-spacing: .16em; color: #6274a4; border: 1px solid #253251; border-radius: 999px; padding: .2rem .55rem; background: #0d1424; }
  .vs .turn { font-size: .7rem; color: #9fb0d8; font-variant-numeric: tabular-nums; white-space: nowrap; }
  .comm { margin: .1rem 1.2rem .5rem; min-height: 2.5rem; padding: .6rem .9rem; border-left: 3px solid var(--tc, #4a5a86);
    background: #0b1322; border-radius: 0 8px 8px 0; font-size: .9rem; color: #dbe4ff; }
  .comm.swap { animation: slidein .35s ease; }
  @keyframes slidein { from { opacity: 0; transform: translateY(5px); } to { opacity: 1; transform: none; } }
  .comm .sub { display: block; color: #8fa0c9; font-size: .78rem; margin-top: .2rem; }
  .comm .start { color: #6b7aa3; }
  .momentum { padding: .1rem 1.1rem 0; }
  .momentum .mhd { display: flex; justify-content: space-between; align-items: baseline; font-size: .66rem; text-transform: uppercase; letter-spacing: .12em; color: #7c8cb8; margin-bottom: .1rem; }
  .momentum svg { width: 100%; height: 150px; display: block; cursor: pointer; touch-action: none; }
  .momentum svg.draw { animation: reveal .85s ease; }
  @keyframes reveal { from { clip-path: inset(0 100% 0 0); } to { clip-path: inset(0 0 0 0); } }
  .transport { display: flex; align-items: center; gap: .5rem; padding: .3rem 1.2rem 1.1rem; flex-wrap: wrap; }
  .transport button { background: #101c33; color: #dbe4ff; border: 1px solid #26375a; border-radius: 9px; padding: .4rem .7rem; font-size: .85rem; cursor: pointer; transition: background .15s; }
  .transport button:hover { background: #172647; }
  .transport .play { font-weight: 700; padding: .4rem 1.05rem; }
  .transport .sp { flex: 1; }
  .transport .rate { font-size: .78rem; color: #8fa0c9; }
  .winbanner { text-align: center; font-weight: 700; color: #fde68a; padding: 0 1rem .9rem; min-height: 1.2rem; }
  @media (max-width: 560px) { .arena { grid-template-columns: 1fr; } .vs { flex-direction: row; } .cbt.c1 { align-items: flex-start; text-align: left; } .cbt.c1 .ty, .cbt.c1 .tags, .cbt.c1 .tray { justify-content: flex-start; flex-direction: row; } .cbt.c1 .hprow { flex-direction: row; } }
</style>
</head>
<body>
<div class="wrap">
  <header class="masthead">
    <div class="mh-title">
      <span class="pokeball"></span>
      <div>
        <h1>Poké<span>Arena</span></h1>
        <p class="tag">Battle benchmark — deterministic Gen-1, every number reproduces from seed.</p>
      </div>
    </div>
    <div class="mh-side">
      <a class="mh-repo" href="{{.RepoURL}}" target="_blank" rel="noopener"><svg viewBox="0 0 16 16" width="14" height="14" fill="currentColor" aria-hidden="true"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"></path></svg>shaumik/PokeArena</a>
      <div class="mh-when">{{.GeneratedHuman}}</div>
      <div class="mh-scale num">{{if .TotalGames}}{{.TotalGames}} games · {{end}}{{.NTeams}} teams · {{.NAgents}} agents</div>
    </div>
  </header>
  <div class="rules">{{range .RulesetParts}}<span class="rule">{{.}}</span>{{end}}</div>

  <section class="abstract">
    <div class="eyebrow">Method</div>
    <p class="lead">PokéArena evaluates a decision policy in Gen-1 singles Pokémon. Each turn both sides commit an action before either is revealed, then a rules engine resolves it: the game is two-player, zero-sum, simultaneous-move, with imperfect information and stochastic outcomes. An agent's score is the fraction of games it wins.</p>
    <div class="facets">
      <div class="facet"><h3>Outcome</h3><p>The winner is decided by a deterministic Gen-1 rules engine. No judge, rubric, or model sits in the scoring path; the environment reports the result of play.</p></div>
      <div class="facet"><h3>Design</h3><p>Both sides are dealt the identical six Pokémon, and every seed is played in both seat assignments. Team strength and first-move order are held fixed, so the win rate reflects the policy, not the draw.</p></div>
      <div class="facet"><h3>Estimation</h3><p>Ratings are a Bradley-Terry fit over all pairwise games (MM iteration, order-independent), placed on the Elo scale. Win rates are reported with Wilson 95% intervals.</p></div>
      <div class="facet"><h3>Reproducibility</h3><p>Baseline agents are seeded and reproduce byte for byte. The fit depends on win-loss counts, not play order, so re-running the same configuration reproduces the table below.</p></div>
    </div>
    <p class="probes">The task exercises planning over a long horizon, action choice among moves and switches under stochastic damage and accuracy, and play against an adversary whose move is <b>hidden until resolution</b>.</p>
  </section>

  <div class="card">
    <table>
      <thead>
        <tr>
          <th class="rank">#</th><th>agent</th><th>elo</th>
          <th>win rate — 95% CI</th><th>W–L–D</th><th>$/game</th><th>cost</th>
        </tr>
      </thead>
      <tbody>
      {{range $i, $r := .Rows}}
        <tr class="lrow {{if $r.Top}}top{{end}}{{if and $.HasMatrix (not $r.Reference)}} clickable{{end}}" data-idx="{{$i}}" data-name="{{$r.Name}}">
          <td class="rank num">{{$r.Rank}}</td>
          <td class="name">{{$r.Name}}{{if $r.Condition}}<span class="cond {{$r.Condition}}">{{$r.Condition}}</span>{{end}}<span class="model">{{$r.Model}}</span>{{if and $.HasMatrix (not $r.Reference)}}<span class="hh-toggle">▾ matchups</span>{{end}}</td>
          <td class="elo num">{{$r.Elo}}</td>
          <td>
            <div class="bar">
              <div class="ci" style="left:{{$r.CILeft}}%;width:{{$r.CIWidth}}%"></div>
              <div class="mark" style="left:{{$r.Mark}}%"></div>
              <span class="lbl num">{{$r.WinRate}}</span>
            </div>
          </td>
          <td class="num">{{$r.Record}}</td>
          <td class="num">{{$r.CostPerGame}}</td>
          <td class="num {{if eq $r.Cost "free"}}free{{else if eq $r.Cost "unknown"}}unknown{{end}}">{{$r.Cost}}</td>
        </tr>
        {{if and $.HasMatrix (not $r.Reference)}}<tr class="hh-row" hidden><td colspan="7"><div class="hh"></div></td></tr>{{end}}
      {{end}}
      </tbody>
    </table>
  </div>

  {{if .HasDecisionQuality}}
  <h2>Decision quality — how well did each model choose?</h2>
  <p class="dq-lead">Win rate says who won; it can't say whether a model played well or got lucky. For every free choice in these battles we recover the exact move played, then ask a stronger expectimax oracle — deciding from the <em>identical</em> fog-of-war view — what it would have played, and measure the <strong>regret</strong> (the value the choice gave up). A <strong>blunder</strong> is a choice that gave up more than about a third of a Pokémon's worth of position. Fewer blunders is better.</p>
  <div class="card">
    <table class="dq">
      <thead>
        <tr><th>model</th><th>blunder rate</th><th>median regret</th><th>oracle match</th><th>decisions</th><th>win rate</th></tr>
      </thead>
      <tbody>
      {{range .DQRows}}
        <tr class="{{if .Best}}top{{end}}">
          <td class="name">{{.Model}}{{if .Best}}<span class="cond cleanest">cleanest</span>{{end}}</td>
          <td>
            <div class="bar dq-bar">
              <div class="fill" style="width:{{.BlunderBar}}%"></div>
              <span class="lbl num">{{.BlunderRate}}</span>
            </div>
          </td>
          <td class="num">{{.MedianRegret}}</td>
          <td class="num">{{.MatchRate}}</td>
          <td class="num">{{.Decisions}}</td>
          <td class="num">{{.WinRate}}</td>
        </tr>
      {{end}}
      </tbody>
    </table>
    <p class="dq-note">Regret is in the oracle's evaluation units (≈1000 = one Pokémon). It is heavy-tailed — a missed lethal scores off the chart — so the typical shortfall is reported as the <em>median</em>, not the mean. Match rate is coarse by design: two moves of equal value both count as “right,” so a low match with low regret just means many ties, not many mistakes.</p>
  </div>
  {{end}}

  {{if .Rec.PerTeam}}
  <h2>Per-team Elo — does the ranking hold across the library?</h2>
  <div class="teams">
    {{range .Rec.PerTeam}}
    <div class="team" data-team="{{.Team}}">
      <div class="t">{{.Team}}{{if $.HasRosters}}<span class="reveal">show team ▾</span>{{end}}</div>
      {{range $i, $r := .Ranks}}<span class="chip {{if eq $i 0}}win{{end}} num">{{$r.Name}} {{printf "%.0f" $r.Elo}}</span>{{end}}
      {{if $.HasRosters}}<div class="roster" hidden></div>{{end}}
    </div>
    {{end}}
  </div>
  {{end}}

  {{if or .HasReplays .HasRosters}}
  <script>
  window.POKE = {
    TC: {Normal:'#9099a1',Fire:'#ff6b3d',Water:'#4d90d5',Electric:'#f4c531',Grass:'#63bc5a',
      Ice:'#74cec0',Fighting:'#ce4069',Poison:'#ab5ac8',Ground:'#d97845',Flying:'#8caadc',
      Psychic:'#f85889',Bug:'#90c12c',Rock:'#c7b78b',Ghost:'#5269ac',Dragon:'#0a6dc4',
      Dark:'#5a5366',Steel:'#5a8ea1',Fairy:'#ec8fe6'},
    col: function(t){ if (!t) return '#5566aa'; return this.TC[t] || this.TC[t.charAt(0).toUpperCase() + t.slice(1).toLowerCase()] || '#5566aa'; },
    esc: function(s){ return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); },
    // sprites: dex-number -> inlined data: URI; dexByName is filled from ROSTERS.
    sprites: {{if .HasSprites}}{{.SpritesJSON}}{{else}}null{{end}},
    dexByName: {},
    spriteFor: function(name){ if (!this.sprites) return ''; var d = this.dexByName[name]; return (d && this.sprites[d]) || ''; }
  };
  </script>
  {{end}}

  {{if .HasRosters}}
  <script>
  const ROSTERS = {{.RostersJSON}};
  (function(){
    const byName = {};
    ROSTERS.forEach(function(t){ byName[t.name] = t;
      (t.members || []).forEach(function(m){ if (m.dex_no){ window.POKE.dexByName[m.name] = String(m.dex_no); } });
    });
    document.querySelectorAll('.team').forEach(function(card){
      const t = byName[card.getAttribute('data-team')];
      const panel = card.querySelector('.roster');
      const head = card.querySelector('.t');
      const toggle = card.querySelector('.reveal');
      if (!t || !panel || !head){ return; }
      head.style.cursor = 'pointer';
      let built = false;
      head.addEventListener('click', function(){
        if (!built){
          panel.innerHTML = (t.members || []).map(function(m){
            const types = (m.types || '').split('/').filter(Boolean).map(function(x){
              return '<span class="rtype" style="background:' + window.POKE.col(x) + '">' + window.POKE.esc(x) + '</span>';
            }).join('');
            const spr = window.POKE.spriteFor(m.name);
            const img = spr ? '<img class="rspr" src="' + spr + '" alt="" width="42" height="42">' : '';
            return '<div class="rmon">' + img + '<span class="rn">' + window.POKE.esc(m.name) + '</span> ' + types + '<span class="bst">BST ' + m.bst + '</span></div>';
          }).join('');
          built = true;
        }
        panel.hidden = !panel.hidden;
        if (toggle){ toggle.textContent = panel.hidden ? 'show team ▾' : 'hide team ▴'; }
      });
    });
  })();
  </script>
  {{end}}

  {{if .HasReplays}}
  <h2>Watch a battle{{if .HasMatrix}} — expand any agent above and pick a matchup{{if .HasSamples}} or sample battle{{end}}{{end}}</h2>
  <div class="stage">
    <div class="stage-top">
      <span class="stage-title"><span id="rtitle">—</span><small id="rsub"></small></span>
    </div>
    <div class="arena">
      <div class="cbt c0" id="c0"></div>
      <div class="vs"><span class="b">VS</span><span class="turn" id="rturn"></span></div>
      <div class="cbt c1" id="c1"></div>
    </div>
    <div class="comm" id="rcomm"></div>
    <div class="momentum">
      <div class="mhd"><span>Momentum — team vitality</span><span id="rmlbl"></span></div>
      <svg id="rspark" viewBox="0 0 1000 150" preserveAspectRatio="none"></svg>
    </div>
    <div class="transport">
      <button id="rfirst" title="restart">&#9198;</button>
      <button id="rprev" title="previous turn">&#9664;</button>
      <button id="rplay" class="play">&#9654; Play</button>
      <button id="rnext" title="next turn">&#9654;</button>
      <span class="sp"></span>
      <button id="rspeed" class="rate">1&#215;</button>
    </div>
    <div class="winbanner" id="rwin"></div>
  </div>
  <script>
  const REPLAYS = {{.ReplaysJSON}};
  const MATRIX = {{if .HasMatrix}}{{.MatrixJSON}}{{else}}null{{end}};
  const SAMPLES = {{if .HasSamples}}{{.SamplesJSON}}{{else}}{}{{end}};
  (function(){
    const esc = window.POKE.esc;
    const tcol = window.POKE.col.bind(window.POKE);
    function primary(mon){ return ((mon && mon.types) || '').split('/')[0]; }
    function $(id){ return document.getElementById(id); }

    const turnEl = $('rturn'), comm = $('rcomm'), winEl = $('rwin');
    const spark = $('rspark'), mlbl = $('rmlbl'), btnPlay = $('rplay'), btnSpeed = $('rspeed');
    const c = [$('c0'), $('c1')];
    let cur = 0, fi = 0, timer = null, dragging = false, rate = 1;
    let refs = [null, null], col = ['#888', '#888'];
    let pts = [[], []], N = 0, ph = {};

    // buildLeaderboardHH turns each leaderboard row into a battle picker: click a
    // row to reveal its head-to-head chips (win rate vs each opponent), click a
    // chip to load that matchup into the stage and scroll it into view.
    function buildLeaderboardHH(){
      if (!MATRIX){ return; }
      const A = MATRIX.agents, byRow = {};
      MATRIX.cells.forEach(function(x){ (byRow[x.row] = byRow[x.row] || []).push(x); });
      document.querySelectorAll('tr.lrow.clickable').forEach(function(tr){
        const idx = +tr.getAttribute('data-idx');
        const hhRow = tr.nextElementSibling;
        if (!hhRow || !hhRow.classList.contains('hh-row')){ return; }
        const hh = hhRow.querySelector('.hh');
        let built = false;
        tr.addEventListener('click', function(){
          if (!built){
            const samp = SAMPLES[tr.getAttribute('data-name')];
            if (samp && samp.length){
              // A model row expands to its sample battles: one chip per
              // reconstructed per-team win/loss, each loading that replay. Models
              // played only the reference, so this replaces the single vs-chip
              // with the fuller set a viewer can actually watch.
              hh.innerHTML = samp.map(function(s){
                const cls = s.outcome === 'win' ? 'win' : 'lose';
                const tag = s.outcome === 'win' ? 'W' : 'L';
                return '<span class="hchip ' + cls + ' playable" data-rep="' + s.replay + '">' +
                  esc(s.team) + ' <span class="wr">' + tag + '</span> <span class="play">&#9654;</span></span>';
              }).join('');
            } else {
              const cs = (byRow[idx] || []).slice().sort(function(a, b){ return b.win_rate - a.win_rate; });
              hh.innerHTML = cs.length ? cs.map(function(x){
                const wr = Math.round(x.win_rate * 100);
                // Only a matchup we captured a battle for gets a play affordance;
                // live model games have a win rate but no re-simulable replay, so
                // their chip is a plain stat, not a dead button.
                const playable = x.replay >= 0;
                return '<span class="hchip ' + (wr >= 50 ? 'win' : 'lose') + (playable ? ' playable' : '') + '" data-rep="' + x.replay + '">vs ' +
                  esc(A[x.col]) + ' <span class="wr">' + wr + '%</span>' + (playable ? ' <span class="play">&#9654;</span>' : '') + '</span>';
              }).join('') : '<span class="hh-empty">no matchups</span>';
            }
            hh.querySelectorAll('.hchip.playable').forEach(function(ch){
              ch.addEventListener('click', function(e){
                e.stopPropagation();
                const ri = +ch.getAttribute('data-rep');
                if (ri >= 0){ load(ri); const st = document.querySelector('.stage'); if (st){ st.scrollIntoView({ behavior: 'smooth', block: 'center' }); } }
              });
            });
            built = true;
          }
          hhRow.hidden = !hhRow.hidden;
          tr.classList.toggle('open', !hhRow.hidden);
        });
      });
    }

    function skeleton(el, name){
      el.innerHTML =
        '<div class="trn">' + esc(name) + '</div>' +
        '<div class="med"></div>' +
        '<div class="nm"></div>' +
        '<div class="ty"></div>' +
        '<div class="hprow"><div class="hpbar"><i></i></div><span class="hpnum"></span></div>' +
        '<div class="tags"></div>' +
        '<div class="tray"></div>';
      return { med: el.querySelector('.med'), nm: el.querySelector('.nm'), ty: el.querySelector('.ty'),
        bar: el.querySelector('.hpbar'), fill: el.querySelector('.hpbar > i'), num: el.querySelector('.hpnum'),
        tags: el.querySelector('.tags'), tray: el.querySelector('.tray') };
    }

    function pct(mon){ return (mon && mon.max_hp > 0) ? Math.round(100 * mon.hp / mon.max_hp) : 0; }
    function vit(side, f){ const t = (f.sides[side].tray) || []; if (!t.length) return 0;
      let s = 0; t.forEach(function(x){ s += Math.max(0, x.hp_pct || 0); }); return s / t.length; }

    function updateCbt(side, f, prev, animate){
      const R = refs[side], el = c[side], m = f.sides[side].active || {};
      const p = pct(m), t = tcol(primary(m));
      el.style.setProperty('--tc', t);
      const spr = window.POKE.spriteFor(m.name);
      if (spr){
        if (R.med.getAttribute('data-spr') !== spr){
          R.med.innerHTML = '<img src="' + spr + '" alt="">';
          R.med.setAttribute('data-spr', spr);
          if (animate){ R.med.style.animation = 'none'; void R.med.offsetWidth; R.med.style.animation = 'kopop .4s ease'; }
        }
      } else {
        R.med.removeAttribute('data-spr');
        const mono = (m.name || '?').replace(/[^A-Za-z]/g, '').slice(0, 4).toUpperCase();
        if (R.med.textContent !== mono){
          R.med.textContent = mono;
          if (animate){ R.med.style.animation = 'none'; void R.med.offsetWidth; R.med.style.animation = 'kopop .4s ease'; }
        }
      }
      R.med.style.background = 'radial-gradient(circle at 34% 28%, rgba(255,255,255,.4), transparent 60%), ' + t;
      R.nm.textContent = m.name || '—';
      R.ty.innerHTML = ((m.types || '').split('/')).filter(Boolean).map(function(x){
        return '<span class="typ" style="background:' + tcol(x) + '">' + esc(x) + '</span>'; }).join('');
      R.bar.className = 'hpbar ' + (p <= 20 ? 'crit' : (p <= 50 ? 'low' : ''));
      const same = prev && prev.active && prev.active.name === m.name;
      if (animate && same && p < pct(prev.active)){ void R.bar.offsetWidth; R.bar.classList.add('hit'); }
      R.fill.style.width = p + '%';
      R.num.textContent = (m.hp || 0) + '/' + (m.max_hp || 0);
      el.classList.toggle('faint', p <= 0);
      let tg = '';
      if (m.status) tg += '<span class="tag st">' + esc(m.status) + '</span>';
      if (m.boosts) tg += '<span class="tag bo">' + esc(m.boosts) + '</span>';
      R.tags.innerHTML = tg;
      R.tray.innerHTML = ((f.sides[side].tray) || []).map(function(x){
        const cl = x.fainted ? 'ko' : (x.hp_pct <= 50 ? 'low' : '');
        return '<i class="' + cl + (x.active ? ' act' : '') + '" title="' + esc(x.name) + ' ' + x.hp_pct + '%"></i>'; }).join('');
    }

    // smooth() turns a point list into a Catmull-Rom-derived cubic path — soft
    // curves instead of jagged polylines.
    function smooth(P){
      if (P.length < 2) return P.length ? ('M' + P[0][0] + ',' + P[0][1]) : '';
      let d = 'M' + P[0][0] + ',' + P[0][1];
      for (let i = 0; i < P.length - 1; i++){
        const a = P[i - 1] || P[i], b = P[i], e = P[i + 1], g = P[i + 2] || P[i + 1];
        d += 'C' + (b[0] + (e[0] - a[0]) / 6) + ',' + (b[1] + (e[1] - a[1]) / 6) + ' ' +
             (e[0] - (g[0] - b[0]) / 6) + ',' + (e[1] - (g[1] - b[1]) / 6) + ' ' + e[0] + ',' + e[1];
      }
      return d;
    }

    // buildSpark renders the momentum graph: two type-colored team-vitality
    // curves, KO ticks where a side cliffs, a glow at every lead change, and a
    // scrubbable playhead.
    function buildSpark(r){
      const F = r.frames; N = F.length;
      const W = 1000, top = 10, bot = 124;
      const xs = function(i){ return N <= 1 ? 0 : (i / (N - 1)) * W; };
      const ys = function(v){ return top + (1 - v / 100) * (bot - top); };
      pts = [[], []];
      for (let i = 0; i < N; i++){ pts[0].push([xs(i), ys(vit(0, F[i]))]); pts[1].push([xs(i), ys(vit(1, F[i]))]); }
      const area = function(P){ return smooth(P) + ' L' + W + ',' + bot + ' L0,' + bot + ' Z'; };
      let marks = '';
      for (let i = 1; i < N; i++){
        for (let side = 0; side < 2; side++){
          const kf = (F[i].sides[side].tray || []).filter(function(x){ return x.fainted; }).length;
          const kp = (F[i - 1].sides[side].tray || []).filter(function(x){ return x.fainted; }).length;
          if (kf > kp){
            const x = xs(i);
            marks += '<line x1="' + x + '" y1="' + top + '" x2="' + x + '" y2="' + bot + '" stroke="#33415f" stroke-width="1" stroke-dasharray="2 3"/>' +
              '<path d="M' + x + ',' + (bot + 2) + ' l-4,7 l8,0 z" fill="' + col[side] + '"><title>KO</title></path>';
          }
        }
        const a = vit(0, F[i - 1]) - vit(1, F[i - 1]), b = vit(0, F[i]) - vit(1, F[i]);
        if ((a < 0) !== (b < 0) && a !== 0){
          const x = xs(i);
          marks += '<line x1="' + x + '" y1="' + top + '" x2="' + x + '" y2="' + bot + '" stroke="#a78bfa" stroke-width="6" opacity=".16"/>';
        }
      }
      spark.innerHTML =
        '<defs>' +
        '<linearGradient id="g0" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="' + col[0] + '" stop-opacity=".5"/><stop offset="1" stop-color="' + col[0] + '" stop-opacity="0"/></linearGradient>' +
        '<linearGradient id="g1" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="' + col[1] + '" stop-opacity=".5"/><stop offset="1" stop-color="' + col[1] + '" stop-opacity="0"/></linearGradient>' +
        '</defs>' +
        '<line x1="0" y1="' + ys(50) + '" x2="' + W + '" y2="' + ys(50) + '" stroke="#1e2a44" stroke-dasharray="4 5"/>' +
        '<line x1="0" y1="' + bot + '" x2="' + W + '" y2="' + bot + '" stroke="#24314e"/>' +
        '<path d="' + area(pts[0]) + '" fill="url(#g0)"/>' +
        '<path d="' + area(pts[1]) + '" fill="url(#g1)"/>' +
        '<path d="' + smooth(pts[0]) + '" fill="none" stroke="' + col[0] + '" stroke-width="2.5"/>' +
        '<path d="' + smooth(pts[1]) + '" fill="none" stroke="' + col[1] + '" stroke-width="2.5"/>' +
        marks +
        '<line id="phln" x1="0" y1="' + top + '" x2="0" y2="' + bot + '" stroke="#eaf0ff" stroke-width="1.5" opacity=".8"/>' +
        '<circle id="phd0" r="4" fill="' + col[0] + '" stroke="#eaf0ff" stroke-width="1.5"/>' +
        '<circle id="phd1" r="4" fill="' + col[1] + '" stroke="#eaf0ff" stroke-width="1.5"/>';
      ph = { ln: $('phln'), d0: $('phd0'), d1: $('phd1') };
      spark.classList.remove('draw'); void spark.offsetWidth; spark.classList.add('draw');
    }

    function setHead(f){
      const x = N <= 1 ? 0 : (fi / (N - 1)) * 1000;
      ph.ln.setAttribute('x1', x); ph.ln.setAttribute('x2', x);
      ph.d0.setAttribute('cx', x); ph.d0.setAttribute('cy', pts[0][fi][1]);
      ph.d1.setAttribute('cx', x); ph.d1.setAttribute('cy', pts[1][fi][1]);
      const r = REPLAYS[cur];
      mlbl.textContent = 'T' + f.turn + ' · ' + r.side0 + ' ' + Math.round(vit(0, f)) + '% · ' + r.side1 + ' ' + Math.round(vit(1, f)) + '%';
    }

    function render(animate){
      const r = REPLAYS[cur], f = r.frames[fi], prev = fi > 0 ? r.frames[fi - 1] : null;
      updateCbt(0, f, prev ? prev.sides[0] : null, animate);
      updateCbt(1, f, prev ? prev.sides[1] : null, animate);
      turnEl.textContent = 'Turn ' + f.turn + ' · ' + (fi + 1) + '/' + r.frames.length;
      let acts = '', mover = 0;
      (f.actions || []).forEach(function(a, side){
        if (a){ if (!acts) mover = side; acts += '<div>' + esc(side === 0 ? r.side0 : r.side1) + "'s " + esc((f.sides[side].active || {}).name || '') + ' ' + esc(a) + '</div>'; }
      });
      const logs = (f.log || []);
      const sub = logs.length ? '<span class="sub">' + logs.map(esc).join(' · ') + '</span>' : '';
      comm.style.setProperty('--tc', col[mover]);
      comm.className = 'comm' + (animate ? ' swap' : '');
      comm.innerHTML = (acts || '<span class="start">— battle start —</span>') + sub;
      setHead(f);
      winEl.textContent = (fi === r.frames.length - 1) ? (r.winner === 'draw' ? 'Draw — double KO' : 'Winner — ' + r.winner) : '';
    }

    function stop(){ if (timer){ clearInterval(timer); timer = null; btnPlay.innerHTML = '&#9654; Play'; } }
    function step(d){ stop(); fi = Math.max(0, Math.min(N - 1, fi + d)); render(false); }
    function play(){
      if (timer){ stop(); return; }
      if (fi >= N - 1) fi = 0;
      btnPlay.innerHTML = '&#9208; Pause';
      timer = setInterval(function(){ if (fi >= N - 1){ stop(); return; } fi++; render(true); }, 1200 / rate);
    }
    function load(i){
      stop(); cur = i; fi = 0;
      const r = REPLAYS[i];
      $('rtitle').textContent = r.title;
      $('rsub').textContent = '  ' + r.match + ' · ' + r.team + ' · ' + r.turns + ' turns';
      col = [tcol(primary(r.frames[0].sides[0].active)), tcol(primary(r.frames[0].sides[1].active))];
      refs = [skeleton(c[0], r.side0), skeleton(c[1], r.side1)];
      buildSpark(r);
      render(false);
    }

    function seek(e){
      const rect = spark.getBoundingClientRect();
      const rel = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
      stop(); fi = Math.round(rel * (N - 1)); render(false);
    }
    spark.addEventListener('pointerdown', function(e){ dragging = true; try { spark.setPointerCapture(e.pointerId); } catch (x) {} seek(e); });
    spark.addEventListener('pointermove', function(e){ if (dragging) seek(e); });
    spark.addEventListener('pointerup', function(){ dragging = false; });
    spark.addEventListener('pointercancel', function(){ dragging = false; });

    $('rfirst').addEventListener('click', function(){ stop(); fi = 0; render(false); });
    $('rprev').addEventListener('click', function(){ step(-1); });
    $('rnext').addEventListener('click', function(){ step(1); });
    btnPlay.addEventListener('click', play);
    btnSpeed.addEventListener('click', function(){
      rate = rate === 1 ? 2 : (rate === 2 ? 0.5 : 1);
      btnSpeed.innerHTML = rate + '&#215;';
      if (timer){ stop(); play(); }
    });

    buildLeaderboardHH();
    load(0);
  })();
  </script>
  {{end}}

</div>
</body>
</html>
`
