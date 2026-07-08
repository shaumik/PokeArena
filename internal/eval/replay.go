package eval

import (
	"fmt"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// A replay is a self-contained recording of one battle: a snapshot of the board
// after every resolved turn plus the engine's log lines for that turn. The
// report embeds a handful of these so a reader can watch a battle play out with
// no server, no trace file, and no network — the frames carry everything the
// in-browser replayer needs.
//
// Frames are captured live (RunGameCaptured), not reconstructed from the
// decision trace, because the trace stores only actions and state hashes, and
// because a non-deterministic (model-backed) game can't be re-derived after the
// fact. For the deterministic baseline run the capture is a byte-exact re-play
// of the original game.

// ReplayMon is one active Pokémon's public state at a frame.
type ReplayMon struct {
	Name   string `json:"name"`
	Types  string `json:"types"`
	HP     int    `json:"hp"`
	MaxHP  int    `json:"max_hp"`
	Status string `json:"status,omitempty"`
	Boosts string `json:"boosts,omitempty"`
}

// ReplaySlot is one party-tray slot: enough to draw a six-dot health tray.
type ReplaySlot struct {
	Name    string `json:"name"`
	HPPct   int    `json:"hp_pct"`
	Fainted bool   `json:"fainted"`
	Active  bool   `json:"active"`
}

// ReplaySide is one trainer's half of the board at a frame.
type ReplaySide struct {
	Trainer string       `json:"trainer"`
	Active  ReplayMon    `json:"active"`
	Tray    []ReplaySlot `json:"tray"`
}

// ReplayFrame is the board after one resolved turn, with that turn's actions and
// log. The lead frame (phase "lead") is the pre-turn-1 state with an empty log.
type ReplayFrame struct {
	Phase   string        `json:"phase"`
	Turn    int           `json:"turn"`
	Actions [2]string     `json:"actions"`
	Sides   [2]ReplaySide `json:"sides"`
	Field   string        `json:"field,omitempty"`
	Log     []string      `json:"log,omitempty"`
}

// Replay is a full recorded battle with its highlight caption.
type Replay struct {
	Title  string        `json:"title"`
	Match  string        `json:"match"`
	Team   string        `json:"team"`
	Seed   uint64        `json:"seed"`
	Side0  string        `json:"side0"`
	Side1  string        `json:"side1"`
	Winner string        `json:"winner"`
	Turns  int           `json:"turns"`
	Frames []ReplayFrame `json:"frames"`
}

// RunGameCaptured plays a game like RunGame but records a ReplayFrame after each
// resolved turn (capturing the engine log lines RunGame discards) plus a lead
// frame for the opening state. It is used only for the handful of highlight
// re-simulations, so the small duplication of RunGame's loop is deliberate — the
// shared, tricky part (fog-of-war projection, fallback, usage) stays in decide.
func RunGameCaptured(dex *domain.Dex, agents [2]ai.Agent, teams [2][]engine.TeamPick, seed uint64, budget Budget) (GameResult, Replay, error) {
	id := fmt.Sprintf("eval-%d", seed)
	s, err := engine.NewBattleFromPicks(dex, id, "P0", teams[0], "P1", teams[1], seed)
	if err != nil {
		return GameResult{}, Replay{}, fmt.Errorf("new battle: %w", err)
	}

	res := GameResult{Seed: seed}
	rep := Replay{Seed: seed, Frames: []ReplayFrame{frameFromState(s, nil, "lead", [2]string{})}}

	for !s.Ended() {
		if len(res.Decisions) > maxDecisions {
			return res, rep, fmt.Errorf("battle %s did not terminate within %d decisions", id, maxDecisions)
		}

		switch s.Phase {
		case engine.PhaseChoosing:
			var acts [2]engine.Action
			var labels [2]string
			for side := 0; side < 2; side++ {
				d := decide(dex, agents[side], s, side, budget)
				acts[side] = d.Action
				labels[side] = actionLabel(dex, s, side, d.Action) // before ResolveTurn mutates state
				res.Usage[side] = res.Usage[side].Add(d.Usage)
				res.Decisions = append(res.Decisions, d)
			}
			logs := engine.ResolveTurn(dex, s, acts)
			rep.Frames = append(rep.Frames, frameFromState(s, logs, "turn", labels))

		case engine.PhaseReplace:
			var sw [2]*engine.Action
			var labels [2]string
			for side := 0; side < 2; side++ {
				if !s.Replace[side] {
					continue
				}
				d := decide(dex, agents[side], s, side, budget)
				a := d.Action
				sw[side] = &a
				labels[side] = actionLabel(dex, s, side, a)
				res.Usage[side] = res.Usage[side].Add(d.Usage)
				res.Decisions = append(res.Decisions, d)
			}
			logs := engine.ResolveReplace(s, sw)
			rep.Frames = append(rep.Frames, frameFromState(s, logs, "replace", labels))

		default:
			return res, rep, fmt.Errorf("battle %s in unexpected phase %q", id, s.Phase)
		}
	}

	res.Winner = s.Winner
	res.Turns = s.Turn
	rep.Turns = s.Turn
	return res, rep, nil
}

// frameFromState snapshots the full (both-sides) board — a replay is post-hoc
// analysis, so there is no fog of war to preserve.
func frameFromState(s *engine.BattleState, logs []engine.LogLine, phase string, actions [2]string) ReplayFrame {
	f := ReplayFrame{Phase: phase, Turn: s.Turn, Actions: actions, Field: fieldSummary(s)}
	for side := 0; side < 2; side++ {
		sd := s.Sides[side]
		rs := ReplaySide{Trainer: sd.Trainer}
		if sd.Active >= 0 && sd.Active < len(sd.Team) {
			a := sd.Team[sd.Active]
			rs.Active = ReplayMon{
				Name:   a.Name,
				Types:  typeStr(a.Type1, a.Type2),
				HP:     a.HP,
				MaxHP:  a.MaxHP,
				Status: statusStr(a.Status),
				Boosts: boostsStr(a.Stages),
			}
		}
		for i, p := range sd.Team {
			rs.Tray = append(rs.Tray, ReplaySlot{
				Name:    p.Name,
				HPPct:   pctOf(p.HP, p.MaxHP),
				Fainted: p.Fainted,
				Active:  i == sd.Active,
			})
		}
		f.Sides[side] = rs
	}
	for _, l := range logs {
		if l.Text != "" {
			f.Log = append(f.Log, l.Text)
		}
	}
	return f
}

// pctOf is HP as a rounded 0..100 percentage, guarding a zero max.
func pctOf(hp, max int) int {
	if max <= 0 {
		return 0
	}
	return (100*hp + max/2) / max
}

func typeStr(t1, t2 domain.Type) string {
	if t2 == "" {
		return string(t1)
	}
	return string(t1) + "/" + string(t2)
}

func statusStr(s engine.StatusCond) string {
	if s == engine.StatusNone {
		return ""
	}
	return string(s)
}

// boostsStr renders non-zero stat stages as "+2 Atk, -1 Spe" (empty if none),
// in the canonical Atk..Eva order.
func boostsStr(st engine.Stages) string {
	stages := []struct {
		n     int
		label string
	}{
		{st.Atk, "Atk"},
		{st.Def, "Def"},
		{st.SpA, "SpA"},
		{st.SpD, "SpD"},
		{st.Spe, "Spe"},
		{st.Acc, "Acc"},
		{st.Eva, "Eva"},
	}
	out := ""
	for _, s := range stages {
		if s.n == 0 {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += fmt.Sprintf("%+d %s", s.n, s.label)
	}
	return out
}

// fieldSummary renders the global field — weather, terrain, rooms, Gravity —
// with turns left. Empty when the field is clear.
func fieldSummary(s *engine.BattleState) string {
	parts := []string{}
	if s.Weather != nil && s.Weather.Kind != engine.WeatherClear {
		parts = append(parts, fmt.Sprintf("%s %dt", s.Weather.Kind, s.Weather.TurnsLeft))
	}
	if s.Terrain != nil && s.Terrain.Kind != engine.TerrainNone {
		parts = append(parts, fmt.Sprintf("%s terrain %dt", s.Terrain.Kind, s.Terrain.TurnsLeft))
	}
	pw := s.PseudoWeather
	for _, t := range []struct {
		timer *engine.PWTimer
		label string
	}{
		{pw.TrickRoom, "Trick Room"},
		{pw.WonderRoom, "Wonder Room"},
		{pw.MagicRoom, "Magic Room"},
		{pw.Gravity, "Gravity"},
	} {
		if t.timer != nil {
			parts = append(parts, fmt.Sprintf("%s %dt", t.label, t.timer.TurnsLeft))
		}
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

// highlight is one chosen game plus the caption explaining why it's worth
// watching.
type highlight struct {
	Title string
	Game  GameRecord
}

// gameKey identifies a specific played game so highlights don't double-pick the
// same battle under two captions.
func gameKey(g GameRecord) string {
	return fmt.Sprintf("%s|%s|%d|%s", g.Match, g.Team, g.Seed, g.Side0)
}

// selectHighlights chooses a few of the most watchable games from the completed
// run, using only the lightweight per-game record (turns, winner) and the final
// Elo — no frames needed, so selection is cheap over the whole field. It returns
// distinct games captioned by why they stand out: the longest grind, the biggest
// Elo upset, a draw (double-KO), and the quickest decisive stomp.
func selectHighlights(matches []MatchResult, standings []ContestantResult) []highlight {
	elo := make(map[string]float64, len(standings))
	for _, c := range standings {
		elo[c.Name] = c.Elo
	}

	var longest, shortest, draw GameRecord
	var haveLongest, haveShortest, haveDraw bool
	var upset GameRecord
	var haveUpset bool
	bestUpset := 0.0 // largest (loserElo - winnerElo) margin

	for _, m := range matches {
		for _, g := range m.Games {
			turns := g.Result.Turns
			if !haveLongest || turns > longest.Result.Turns {
				longest, haveLongest = g, true
			}
			if g.Winner == "draw" {
				if !haveDraw || turns > draw.Result.Turns {
					draw, haveDraw = g, true
				}
				continue // draws don't count toward decisive/upset picks
			}
			if !haveShortest || turns < shortest.Result.Turns {
				shortest, haveShortest = g, true
			}
			loser := g.Side0
			if loser == g.Winner {
				loser = g.Side1
			}
			if margin := elo[loser] - elo[g.Winner]; margin > bestUpset {
				bestUpset, upset, haveUpset = margin, g, true
			}
		}
	}

	var picks []highlight
	seen := map[string]bool{}
	add := func(title string, ok bool, g GameRecord) {
		if !ok {
			return
		}
		k := gameKey(g)
		if seen[k] {
			return
		}
		seen[k] = true
		picks = append(picks, highlight{Title: title, Game: g})
	}
	if haveLongest {
		add(fmt.Sprintf("Longest game — %d turns", longest.Result.Turns), true, longest)
	}
	if haveUpset {
		loser := upset.Side0
		if loser == upset.Winner {
			loser = upset.Side1
		}
		add(fmt.Sprintf("Biggest upset — %s beats %s (%.0f Elo below)", upset.Winner, loser, bestUpset), true, upset)
	}
	if haveDraw {
		add(fmt.Sprintf("A draw — %d turns to a double-KO", draw.Result.Turns), true, draw)
	}
	if haveShortest {
		add(fmt.Sprintf("Quickest finish — %s in %d turns", shortest.Winner, shortest.Result.Turns), true, shortest)
	}
	return picks
}

// CaptureHighlights selects the standout games from a finished run and
// re-simulates each one with full frame capture, returning ready-to-embed
// replays. Re-simulation reproduces the original game byte-for-byte for
// deterministic contestants; if a replay diverges from the recorded outcome
// (a model-backed, non-deterministic contestant), it is dropped rather than
// shown, so the report never displays a battle that didn't happen.
func CaptureHighlights(dex *domain.Dex, contestants []Contestant, teams []NamedTeam, matches []MatchResult, standings []ContestantResult, budget Budget) []Replay {
	byName := make(map[string]Contestant, len(contestants))
	for _, c := range contestants {
		byName[c.Name] = c
	}
	teamByName := make(map[string]NamedTeam, len(teams))
	for _, t := range teams {
		teamByName[t.Name] = t
	}

	var out []Replay
	for _, h := range selectHighlights(matches, standings) {
		g := h.Game
		s0, ok0 := byName[g.Side0]
		s1, ok1 := byName[g.Side1]
		nt, okT := teamByName[g.Team]
		if !ok0 || !ok1 || !okT {
			continue
		}
		agents := [2]ai.Agent{s0.New(g.Seed), s1.New(g.Seed ^ sideSalt)}
		res, rep, err := RunGameCaptured(dex, agents, nt.Mirror(), g.Seed, budget)
		if err != nil {
			continue
		}
		// Guard against a divergent re-simulation: only embed a replay whose
		// outcome matches what was recorded.
		if winnerName(res.Winner, g.Side0, g.Side1) != g.Winner || res.Turns != g.Result.Turns {
			continue
		}
		rep.Title = h.Title
		rep.Match = g.Match
		rep.Team = g.Team
		rep.Side0 = g.Side0
		rep.Side1 = g.Side1
		rep.Winner = g.Winner
		out = append(out, rep)
	}
	return out
}

// winnerName maps a board-side winner index (0/1, else draw) to a contestant
// name — the same mapping resolvedGame applies, spelled out here for the replay
// divergence check.
func winnerName(idx int, side0, side1 string) string {
	switch idx {
	case 0:
		return side0
	case 1:
		return side1
	default:
		return "draw"
	}
}

// actionLabel is the short human-readable form of a chosen action, resolved
// against the state at decision time (before the turn resolves).
func actionLabel(dex *domain.Dex, s *engine.BattleState, side int, a engine.Action) string {
	sd := s.Sides[side]
	if a.Kind == engine.ActionSwitch {
		if a.Index >= 0 && a.Index < len(sd.Team) {
			return "sent out " + sd.Team[a.Index].Name
		}
		return "switched"
	}
	if a.Index < 0 {
		return "used Struggle"
	}
	if sd.Active >= 0 && sd.Active < len(sd.Team) {
		mv := sd.Team[sd.Active].Moves
		if a.Index < len(mv) {
			if m, ok := dex.Moves[mv[a.Index].MoveID]; ok {
				return "used " + m.Name
			}
			return "used " + mv[a.Index].MoveID
		}
	}
	return "acted"
}
