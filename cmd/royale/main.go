// Command royale is the tournament broker for a PokéArena battle royale: a
// file-backed, two-seat match director that lets two independent agent
// processes play a full battle against the real engine with no server, no
// websocket, and no shared memory between them.
//
// The design constraint it solves: the two players are separate agents that
// cannot talk to each other, and neither may see the other's team. So the
// match lives on disk — state.json is the only source of truth, every
// read-modify-write is serialized by a lock file, and each side reaches it
// through exactly two commands:
//
//	royale view --id M --slot p1 --wait      # block until it is your turn
//	royale act  --id M --slot p1 --action move:0
//
// `view` renders nothing but ai.MakeView, which is the engine's own
// fog-of-war projection, so a player agent cannot see the opponent's bench
// even by accident. Identity is fogged the same way: each seat plays under a
// codename from its team file, that alias is what the engine carries as the
// side's trainer, and the real name and theme never reach the other player.
// The referee-only commands (`log`, `report`, `state`) show everything and are
// gated behind the judge token in meta.json.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shaumik/PokeArena/internal/ai"
	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/engine"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "new":
		err = cmdNew(os.Args[2:])
	case "view":
		err = cmdView(os.Args[2:])
	case "act":
		err = cmdAct(os.Args[2:])
	case "team":
		err = cmdTeam(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "log":
		err = cmdLog(os.Args[2:])
	case "report":
		err = cmdReport(os.Args[2:])
	case "validate":
		err = cmdValidate(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "royale: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `royale — PokéArena tournament broker

  new       create a match from two team files
  team      print your own roster in full
  view      print your fog-of-war view (--wait blocks until it is your turn)
  act       submit an action (blocks until the turn resolves, then prints it)
  status    one-line match status
  log       full referee log            (judge token required)
  report    machine-readable match dump (judge token required)
  validate  legality-check a team file (-strict also fails on inert mechanics)
`)
}

func loadDex(dir string) (*domain.Dex, error) { return domain.LoadDex(dir, "gen1-v1") }

// teamFile is the on-disk roster format: a name, a public codename, a declared
// theme, and six engine.TeamPicks.
//
// Name and Theme are the roster's identity and are private to its own pilot.
// Codename is what the other seat is shown instead. The split exists because
// the second tournament's names were themselves the archetype — Perish Row,
// The Caltrops, The Low Ceiling — and the champion said the foe's name told it
// Trick Room before turn one. That was the previous run's theme-string leak
// reopened at a layer the harness had not covered, and it was patched with a
// rule in the runbook asking the organizer for neutral names. A rule is only
// as good as the organizer's memory, so the codename is the mechanism: an
// absent one falls back to a neutral seat label rather than to the real name,
// which makes forgetting the safe outcome instead of the leaky one.
type teamFile struct {
	Name     string            `json:"name"`
	Codename string            `json:"codename"`
	Theme    string            `json:"theme"`
	Picks    []engine.TeamPick `json:"picks"`
}

// codenameFor resolves the public alias for a seat: the team file's codename
// if it declared one, and a neutral seat label otherwise. Never the real name
// — a missing codename must not degrade into the leak this replaces.
func codenameFor(tf teamFile, side int) string {
	if cn := strings.TrimSpace(tf.Codename); cn != "" {
		return cn
	}
	return defaultCodename(side)
}

func defaultCodename(side int) string {
	if side == 1 {
		return "Trainer P2"
	}
	return "Trainer P1"
}

// publicName is the only identity a player agent may be shown for a side —
// its own included, since the engine carries codenames and a pilot has to be
// able to recognize itself in a battle line. Every player-facing printer goes
// through here; meta.Trainers[i].Name is for the pilot's own briefing and for
// the judge-gated commands, and nowhere else.
func publicName(meta Meta, side int) string {
	if cn := strings.TrimSpace(meta.Trainers[side].Codename); cn != "" {
		return cn
	}
	return defaultCodename(side)
}

// checkCodename rejects a codename that gives the game away by repeating the
// identity it is supposed to stand in for. It cannot judge whether a codename
// is *evocative* of the archetype — that stays the organizer's job, and the
// runbook says so — but a codename equal to the team name is not a public
// alias at all, and that one is worth refusing outright.
func checkCodename(tf teamFile) error {
	cn := strings.TrimSpace(tf.Codename)
	if cn == "" {
		return nil
	}
	if strings.EqualFold(cn, strings.TrimSpace(tf.Name)) {
		return fmt.Errorf("team %s: codename repeats the team name — it has to be a "+
			"separate public alias, or be left empty for a neutral seat label", tf.Name)
	}
	return nil
}

func readTeamFile(path string) (teamFile, error) {
	var tf teamFile
	if err := readJSON(path, &tf); err != nil {
		return tf, fmt.Errorf("read team %s: %w", path, err)
	}
	if tf.Name == "" {
		return tf, fmt.Errorf("team %s: missing name", path)
	}
	return tf, nil
}

func cmdValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	data := fs.String("data", "data", "dataset directory")
	path := fs.String("team", "", "team JSON file")
	strict := fs.Bool("strict", false, "treat inert-mechanic warnings as failures")
	_ = fs.Parse(args)
	dex, err := loadDex(*data)
	if err != nil {
		return err
	}
	tf, err := readTeamFile(*path)
	if err != nil {
		return err
	}
	if err := engine.ValidateTeam(tf.Picks, dex); err != nil {
		return fmt.Errorf("%s (%s): %w", tf.Name, *path, err)
	}
	if err := checkCodename(tf); err != nil {
		return err
	}
	fmt.Printf("OK  %-14s %-22s %d Pokémon, legal under standard clauses\n", tf.Name, tf.Theme, len(tf.Picks))
	if cn := strings.TrimSpace(tf.Codename); cn != "" {
		fmt.Printf("    codename %q — the only identity the opponent is shown\n", cn)
	} else {
		fmt.Printf("    no codename declared — the opponent will see the neutral seat label " +
			"(\"Trainer P1\"/\"Trainer P2\") instead of this team's name\n")
	}
	inert := inertWarnings(dex, tf.Picks)
	for _, w := range inert {
		fmt.Printf("WARN %s\n", w)
	}
	fmt.Print(teamSummary(dex, Trainer{Name: tf.Name, Theme: tf.Theme, Picks: tf.Picks}))
	if len(inert) > 0 && *strict {
		return fmt.Errorf("%s: %d pick(s) depend on mechanics the engine does not model "+
			"(run without -strict to see the roster anyway)", tf.Name, len(inert))
	}
	return nil
}

// inertWarnings reports the picks whose declared ability or item does nothing
// in this engine. Legality and soundness are different questions: ValidateTeam
// happily passes a roster built on an ability the engine models as nothing,
// which is how a tournament team declared Harvest a pillar of its strategy and
// found out mid-match, by reading source, that the slug was registered inert.
// It played a Pokémon and a half short for the whole tournament. This is the
// check that would have said so beforehand.
func inertWarnings(dex *domain.Dex, picks []engine.TeamPick) []string {
	var out []string
	for i, p := range picks {
		name := fmt.Sprintf("slot %d", i+1)
		if sp, ok := dex.Species[p.DexNo]; ok {
			name = fmt.Sprintf("slot %d %s", i+1, sp.Name)
		}
		if why := engine.AbilityInertReason(p.Ability); why != "" {
			out = append(out, fmt.Sprintf("%s: ability %q — %s", name, p.Ability, why))
		}
		if why := engine.ItemInertReason(p.Item); why != "" {
			out = append(out, fmt.Sprintf("%s: item %q — %s", name, p.Item, why))
		}
	}
	return out
}

func cmdNew(args []string) error {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	root := fs.String("root", "royale", "tournament directory")
	data := fs.String("data", "data", "dataset directory")
	id := fs.String("id", "", "match id")
	round := fs.String("round", "", "round label")
	p1 := fs.String("p1", "", "team JSON for slot p1")
	p2 := fs.String("p2", "", "team JSON for slot p2")
	seed := fs.Uint64("seed", 1, "battle seed")
	maxTurns := fs.Int("max-turns", 120, "turn cap before the match goes to a judges' decision")
	token := fs.String("token", "", "judge token gating the referee commands")
	_ = fs.Parse(args)
	if *id == "" || *p1 == "" || *p2 == "" {
		return errors.New("new requires -id, -p1 and -p2")
	}
	dex, err := loadDex(*data)
	if err != nil {
		return err
	}
	t1, err := readTeamFile(*p1)
	if err != nil {
		return err
	}
	t2, err := readTeamFile(*p2)
	if err != nil {
		return err
	}
	for _, t := range []teamFile{t1, t2} {
		if err := engine.ValidateTeam(t.Picks, dex); err != nil {
			return fmt.Errorf("team %s illegal: %w", t.Name, err)
		}
		if err := checkCodename(t); err != nil {
			return err
		}
	}
	cn1, cn2 := codenameFor(t1, 0), codenameFor(t2, 1)
	if strings.EqualFold(cn1, cn2) {
		return fmt.Errorf("both seats claim the codename %q; the two sides have to be "+
			"tellable apart in the log", cn1)
	}
	dir := matchDir(*root, *id)
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err == nil {
		return fmt.Errorf("match %s already exists", *id)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Codenames, not names: engine log lines interpolate the side's trainer
	// ("The Low Ceiling's team became cloaked in a mystical veil!") and those
	// lines are printed to both players. Handing the engine the alias means no
	// battle text can carry the real name, which is a stronger guarantee than
	// redacting it on the way out.
	st, err := engine.NewBattleFromPicks(dex, *id, cn1, t1.Picks, cn2, t2.Picks, *seed)
	if err != nil {
		return err
	}
	if *token == "" {
		*token = fmt.Sprintf("judge-%s-%d", *id, *seed)
	}
	meta := Meta{
		ID:    *id,
		Round: *round,
		Trainers: [2]Trainer{
			{Name: t1.Name, Codename: cn1, Theme: t1.Theme, Team: *p1, Picks: t1.Picks},
			{Name: t2.Name, Codename: cn2, Theme: t2.Theme, Team: *p2, Picks: t2.Picks},
		},
		Seed:       *seed,
		MaxTurns:   *maxTurns,
		JudgeToken: *token,
		Created:    time.Now().UTC().Format(time.RFC3339),
	}
	if err := writeJSON(filepath.Join(dir, "meta.json"), meta); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "state.json"), st); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "pending.json"), Pending{}); err != nil {
		return err
	}
	// Organizer-facing: real names, and the codenames each side will be played
	// under so the operator can read the players' logs without decoding them.
	fmt.Printf("match %s created: %s as %q (%s) vs %s as %q (%s), seed %d, turn cap %d\n",
		*id, t1.Name, cn1, t1.Theme, t2.Name, cn2, t2.Theme, *seed, *maxTurns)
	return nil
}

func parseSlot(s string) (int, error) {
	switch strings.ToLower(s) {
	case "p1":
		return 0, nil
	case "p2":
		return 1, nil
	}
	return 0, fmt.Errorf("slot must be p1 or p2, got %q", s)
}

type matchCtx struct {
	dir   string
	dex   *domain.Dex
	meta  Meta
	state *engine.BattleState
	pend  Pending
}

func openMatch(root, data, id string) (*matchCtx, error) {
	dir := matchDir(root, id)
	dex, err := loadDex(data)
	if err != nil {
		return nil, err
	}
	m := &matchCtx{dir: dir, dex: dex}
	if err := readJSON(filepath.Join(dir, "meta.json"), &m.meta); err != nil {
		return nil, fmt.Errorf("open match %s: %w", id, err)
	}
	m.state = &engine.BattleState{}
	if err := readJSON(filepath.Join(dir, "state.json"), m.state); err != nil {
		return nil, err
	}
	if err := readJSON(filepath.Join(dir, "pending.json"), &m.pend); err != nil {
		return nil, err
	}
	return m, nil
}

// owesAction reports whether a side must submit something right now: every
// side acts in the choosing phase, but only a side with a fainted active
// acts during a replace.
func owesAction(s *engine.BattleState, side int) bool {
	switch s.Phase {
	case engine.PhaseChoosing:
		return true
	case engine.PhaseReplace:
		return s.Replace[side]
	}
	return false
}

func cmdTeam(args []string) error {
	fs := flag.NewFlagSet("team", flag.ExitOnError)
	root := fs.String("root", "royale", "tournament directory")
	data := fs.String("data", "data", "dataset directory")
	id := fs.String("id", "", "match id")
	slot := fs.String("slot", "", "p1 or p2")
	_ = fs.Parse(args)
	side, err := parseSlot(*slot)
	if err != nil {
		return err
	}
	m, err := openMatch(*root, *data, *id)
	if err != nil {
		return err
	}
	fmt.Printf("Match %s (%s), seed %d, turn cap %d.\n", m.meta.ID, m.meta.Round, m.meta.Seed, m.meta.MaxTurns)
	// Codename only. The foe's theme describes its roster, and so did its
	// name: printing either here handed over abilities, species and the whole
	// archetype before the first turn. See the teamFile doc comment.
	fmt.Printf("You are %s in slot %s, playing under the codename %q — that alias is all the "+
		"opponent sees of you, and it is the name the battle log uses for your side.\n",
		m.meta.Trainers[side].Name, *slot, publicName(m.meta, side))
	fmt.Printf("Your opponent plays as %q. Their name, theme, roster and archetype are all hidden.\n\n",
		publicName(m.meta, 1-side))
	fmt.Print(teamSummary(m.dex, m.meta.Trainers[side]))
	return nil
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	root := fs.String("root", "royale", "tournament directory")
	data := fs.String("data", "data", "dataset directory")
	id := fs.String("id", "", "match id")
	_ = fs.Parse(args)
	m, err := openMatch(*root, *data, *id)
	if err != nil {
		return err
	}
	recs, err := readRecords(m.dir)
	if err != nil {
		return err
	}
	fmt.Printf("match %s (%s) turn %d phase %s resolutions %d alive %d-%d\n",
		m.meta.ID, m.meta.Round, m.state.Turn, m.state.Phase, len(recs),
		m.state.LiveCount(0), m.state.LiveCount(1))
	if m.state.Ended() {
		fmt.Println(winnerLine(m.meta, m.state))
	} else {
		fmt.Printf("awaiting: p1=%v p2=%v (submitted: p1=%v p2=%v)\n",
			owesAction(m.state, 0), owesAction(m.state, 1),
			m.pend.Actions[0] != nil, m.pend.Actions[1] != nil)
	}
	return nil
}

func cmdView(args []string) error {
	fs := flag.NewFlagSet("view", flag.ExitOnError)
	root := fs.String("root", "royale", "tournament directory")
	data := fs.String("data", "data", "dataset directory")
	id := fs.String("id", "", "match id")
	slot := fs.String("slot", "", "p1 or p2")
	wait := fs.Bool("wait", false, "block until it is your turn (or the battle ends)")
	timeout := fs.Duration("timeout", 10*time.Minute, "how long --wait blocks before giving up")
	_ = fs.Parse(args)
	side, err := parseSlot(*slot)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(*timeout)
	for {
		m, err := openMatch(*root, *data, *id)
		if err != nil {
			return err
		}
		mine := owesAction(m.state, side) && m.pend.Actions[side] == nil
		if !*wait || mine || m.state.Ended() {
			v := ai.MakeView(m.state, side)
			legal := []engine.Action{}
			if owesAction(m.state, side) {
				legal = engine.LegalActionsDex(m.dex, m.state, side)
			}
			fmt.Print(renderView(m.dex, m.meta, v, legal, owesAction(m.state, side), mine, m.state))
			if !mine && !m.state.Ended() {
				fmt.Printf("(waiting on the opponent — re-run with --wait to block until your turn)\n")
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("waited %s and it is still not your turn (opponent may be stuck); run `status` and try again", *timeout)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// parseAction accepts "move:N", "switch:N", or "move:N@B" — the @B form
// names the bench slot a self-switch move (U-turn, Baton Pass) should bring
// in, which is otherwise the engine's deterministic choice.
func parseAction(s string) (engine.Action, error) {
	var a engine.Action
	s = strings.TrimSpace(s)
	kind, rest, ok := strings.Cut(s, ":")
	if !ok {
		return a, fmt.Errorf("action must look like move:0 or switch:3, got %q", s)
	}
	idxStr, tgtStr, hasTgt := strings.Cut(rest, "@")
	idx, err := strconv.Atoi(strings.TrimSpace(idxStr))
	if err != nil {
		return a, fmt.Errorf("action index %q is not a number", idxStr)
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "move":
		a = engine.Action{Kind: engine.ActionMove, Index: idx}
	case "switch":
		a = engine.Action{Kind: engine.ActionSwitch, Index: idx}
	default:
		return a, fmt.Errorf("action kind must be move or switch, got %q", kind)
	}
	if hasTgt {
		t, err := strconv.Atoi(strings.TrimSpace(tgtStr))
		if err != nil {
			return a, fmt.Errorf("switch target %q is not a number", tgtStr)
		}
		a.SwitchTarget = &t
	}
	return a, nil
}

func actionLabel(dex *domain.Dex, s *engine.BattleState, side int, a engine.Action) string {
	sd := &s.Sides[side]
	switch a.Kind {
	case engine.ActionMove:
		if a.Index == engine.StruggleMoveIndex {
			return "Struggle"
		}
		act := sd.Team[sd.Active]
		if a.Index >= 0 && a.Index < len(act.Moves) {
			id := act.Moves[a.Index].MoveID
			if m, ok := dex.Moves[id]; ok {
				return fmt.Sprintf("%s used %s", act.Name, m.Name)
			}
			return fmt.Sprintf("%s used %s", act.Name, id)
		}
	case engine.ActionSwitch:
		if a.Index >= 0 && a.Index < len(sd.Team) {
			return fmt.Sprintf("switched to %s", sd.Team[a.Index].Name)
		}
	}
	return fmt.Sprintf("%s:%d", a.Kind, a.Index)
}

func cmdAct(args []string) error {
	fs := flag.NewFlagSet("act", flag.ExitOnError)
	root := fs.String("root", "royale", "tournament directory")
	data := fs.String("data", "data", "dataset directory")
	id := fs.String("id", "", "match id")
	slot := fs.String("slot", "", "p1 or p2")
	action := fs.String("action", "", "move:N | switch:N | move:N@B")
	why := fs.String("why", "", "one-line reasoning, recorded for the match report")
	timeout := fs.Duration("timeout", 10*time.Minute, "how long to wait for the opponent")
	_ = fs.Parse(args)
	side, err := parseSlot(*slot)
	if err != nil {
		return err
	}
	act, err := parseAction(*action)
	if err != nil {
		return err
	}

	dir := matchDir(*root, *id)
	unlock, err := acquireLock(dir)
	if err != nil {
		return err
	}
	m, err := openMatch(*root, *data, *id)
	if err != nil {
		unlock()
		return err
	}
	before, err := readRecords(m.dir)
	if err != nil {
		unlock()
		return err
	}
	nBefore := len(before)

	if m.state.Ended() {
		unlock()
		return fmt.Errorf("match %s is already over — %s", *id, winnerLine(m.meta, m.state))
	}
	if !owesAction(m.state, side) {
		unlock()
		return fmt.Errorf("you do not owe an action right now (phase %s); run `view --wait`", m.state.Phase)
	}
	if m.pend.Actions[side] != nil {
		unlock()
		return fmt.Errorf("you already submitted %q this turn; run `view --wait` for the result", m.pend.Labels[side])
	}
	if !engine.ActionAllowed(m.dex, m.state, side, act) {
		legal := engine.LegalActionsDex(m.dex, m.state, side)
		var opts []string
		for _, l := range legal {
			opts = append(opts, fmt.Sprintf("%s:%d", l.Kind, l.Index))
		}
		unlock()
		return fmt.Errorf("illegal action %q — legal right now: %s", *action, strings.Join(opts, " "))
	}

	label := actionLabel(m.dex, m.state, side, act)
	if *why != "" {
		label += "  // " + *why
	}
	m.pend.Actions[side] = &act
	m.pend.Labels[side] = label

	ready := true
	for s := 0; s < 2; s++ {
		if owesAction(m.state, s) && m.pend.Actions[s] == nil {
			ready = false
		}
	}

	if !ready {
		if err := writeJSON(filepath.Join(dir, "pending.json"), m.pend); err != nil {
			unlock()
			return err
		}
		unlock()
		fmt.Printf("submitted: %s\nwaiting for the opponent…\n", label)
		return waitForResolution(*root, *data, *id, side, nBefore, *timeout)
	}

	rec, err := resolve(m)
	if err != nil {
		unlock()
		return err
	}
	rec.N = nBefore + 1
	if err := writeJSON(filepath.Join(dir, "state.json"), m.state); err != nil {
		unlock()
		return err
	}
	if err := writeJSON(filepath.Join(dir, "pending.json"), Pending{}); err != nil {
		unlock()
		return err
	}
	if err := appendRecord(dir, rec); err != nil {
		unlock()
		return err
	}
	unlock()
	fmt.Printf("submitted: %s\n", label)
	printRecords([]Record{rec}, m.meta, side)
	return afterResolution(*root, *data, *id, side)
}

// resolve advances the battle now that every side that owed an action has
// filed one, and records what happened.
func resolve(m *matchCtx) (Record, error) {
	rec := Record{
		Turn:  m.state.Turn,
		Phase: string(m.state.Phase),
	}
	for s := 0; s < 2; s++ {
		if m.pend.Actions[s] != nil {
			rec.Actions[s] = m.pend.Labels[s]
		}
	}
	switch m.state.Phase {
	case engine.PhaseChoosing:
		acts := [2]engine.Action{*m.pend.Actions[0], *m.pend.Actions[1]}
		rec.Lines = engine.ResolveTurn(m.dex, m.state, acts)
	case engine.PhaseReplace:
		sw := [2]*engine.Action{m.pend.Actions[0], m.pend.Actions[1]}
		rec.Lines = engine.ResolveReplace(m.state, sw)
	default:
		return rec, fmt.Errorf("cannot resolve in phase %s", m.state.Phase)
	}

	// Turn cap. A stall mirror can legitimately run forever, and a tournament
	// needs a result, so past the cap the match goes to a decision on
	// Pokémon remaining and then total HP — announced to both agents up front.
	if !m.state.Ended() && m.meta.MaxTurns > 0 && m.state.Turn >= m.meta.MaxTurns {
		w, why := adjudicate(m.state)
		m.state.Phase = engine.PhaseEnded
		m.state.Winner = w
		rec.Lines = append(rec.Lines, engine.LogLine{
			Type: "decision", Side: -1,
			Text: fmt.Sprintf("Turn cap %d reached — judges' decision: %s", m.meta.MaxTurns, why),
		})
		rec.Verdict = why
	}
	rec.After = snapshot(m.meta, m.state)
	rec.Winner = m.state.Winner
	return rec, nil
}

// adjudicate decides a match that hit the turn cap: most Pokémon standing,
// then the most total HP left, then a draw.
func adjudicate(s *engine.BattleState) (int, string) {
	a0, a1 := s.LiveCount(0), s.LiveCount(1)
	if a0 != a1 {
		w := 0
		if a1 > a0 {
			w = 1
		}
		return w, fmt.Sprintf("%s wins on Pokémon remaining (%d–%d)", s.Sides[w].Trainer, max(a0, a1), min(a0, a1))
	}
	h0, h1 := totalHP(s, 0), totalHP(s, 1)
	if h0 != h1 {
		w := 0
		if h1 > h0 {
			w = 1
		}
		return w, fmt.Sprintf("%s wins on total HP remaining (%.1f%% vs %.1f%%)",
			s.Sides[w].Trainer, 100*max(h0, h1), 100*min(h0, h1))
	}
	return 2, "dead heat on Pokémon and HP — draw"
}

func totalHP(s *engine.BattleState, side int) float64 {
	var sum float64
	for i := range s.Sides[side].Team {
		p := s.Sides[side].Team[i]
		if p.MaxHP > 0 {
			sum += float64(p.HP) / float64(p.MaxHP)
		}
	}
	return sum / float64(len(s.Sides[side].Team))
}

// waitForResolution blocks the submitting agent until the opponent files its
// action and the engine advances, then prints everything that happened.
func waitForResolution(root, data, id string, side, nBefore int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		m, err := openMatch(root, data, id)
		if err != nil {
			return err
		}
		recs, err := readRecords(m.dir)
		if err != nil {
			return err
		}
		if len(recs) > nBefore {
			printRecords(recs[nBefore:], m.meta, side)
			return afterResolution(root, data, id, side)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("opponent has not moved in %s — your action is still queued; re-run `view --wait` later", timeout)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// afterResolution tells the agent what it owes next, so a well-behaved loop
// is just: view --wait, act, repeat.
func afterResolution(root, data, id string, side int) error {
	m, err := openMatch(root, data, id)
	if err != nil {
		return err
	}
	switch {
	case m.state.Ended():
		fmt.Printf("\n*** BATTLE OVER — %s ***\n", winnerLine(m.meta, m.state))
	case owesAction(m.state, side):
		fmt.Printf("\nYOUR TURN (turn %d, phase %s). Run `view` then `act`.\n", m.state.Turn, m.state.Phase)
	default:
		fmt.Printf("\nOpponent owes an action (phase %s). Run `view --wait`.\n", m.state.Phase)
	}
	return nil
}

func printRecords(recs []Record, meta Meta, side int) {
	for _, r := range recs {
		fmt.Printf("\n─── turn %d resolved ───\n", r.Turn)
		for s := 0; s < 2; s++ {
			if r.Actions[s] == "" {
				continue
			}
			who := "YOU"
			if s != side {
				who = "FOE"
			}
			// Never leak the opponent's private reasoning to a player.
			label := r.Actions[s]
			if s != side {
				if i := strings.Index(label, "  // "); i >= 0 {
					label = label[:i]
				}
			}
			// Codename for both seats: the foe's real name is not the
			// reader's to see, and its own side reads back the way the
			// battle log names it.
			fmt.Printf("  %s (%s): %s\n", who, publicName(meta, s), label)
		}
		for _, l := range r.Lines {
			fmt.Printf("  | %s\n", l.Text)
		}
	}
}

func requireJudge(m *matchCtx, token string) error {
	if token != m.meta.JudgeToken {
		return errors.New("referee command: pass the judge token with -token (players must use `view`)")
	}
	return nil
}

func cmdLog(args []string) error {
	fs := flag.NewFlagSet("log", flag.ExitOnError)
	root := fs.String("root", "royale", "tournament directory")
	data := fs.String("data", "data", "dataset directory")
	id := fs.String("id", "", "match id")
	token := fs.String("token", "", "judge token")
	from := fs.Int("from", 0, "first resolution to print")
	wait := fs.Bool("wait", false, "block until a new resolution lands (or the battle ends)")
	timeout := fs.Duration("timeout", 10*time.Minute, "how long --wait blocks")
	_ = fs.Parse(args)
	deadline := time.Now().Add(*timeout)
	for {
		m, err := openMatch(*root, *data, *id)
		if err != nil {
			return err
		}
		if err := requireJudge(m, *token); err != nil {
			return err
		}
		recs, err := readRecords(m.dir)
		if err != nil {
			return err
		}
		if !*wait || len(recs) > *from || m.state.Ended() {
			// The judge sees both identities and the legend for the aliases:
			// battle lines below name each side by its codename, because that
			// is what the engine carries.
			fmt.Printf("match %s (%s): %s [%s] vs %s [%s], seed %d\n",
				m.meta.ID, m.meta.Round,
				m.meta.Trainers[0].Name, m.meta.Trainers[0].Theme,
				m.meta.Trainers[1].Name, m.meta.Trainers[1].Theme, m.meta.Seed)
			fmt.Printf("codenames: p1 %q = %s | p2 %q = %s\n",
				publicName(m.meta, 0), m.meta.Trainers[0].Name,
				publicName(m.meta, 1), m.meta.Trainers[1].Name)
			for _, r := range recs[min(*from, len(recs)):] {
				fmt.Printf("\n─── #%d · turn %d · %s ───\n", r.N, r.Turn, r.Phase)
				for s := 0; s < 2; s++ {
					if r.Actions[s] != "" {
						fmt.Printf("  %s: %s\n", m.meta.Trainers[s].Name, r.Actions[s])
					}
				}
				for _, l := range r.Lines {
					fmt.Printf("  | %s\n", l.Text)
				}
				for s := 0; s < 2; s++ {
					var mons []string
					for _, mo := range r.After.Sides[s].Team {
						tag := fmt.Sprintf("%s %d%%", mo.Name, pct(mo.HP, mo.MaxHP))
						if mo.Fainted {
							tag = mo.Name + " ✗"
						} else if mo.Status != "" {
							tag += "/" + mo.Status
						}
						if mo.Active {
							tag = "*" + tag
						}
						mons = append(mons, tag)
					}
					fmt.Printf("  %s: %s | hazards %s | screens %s\n",
						m.meta.Trainers[s].Name, strings.Join(mons, ", "),
						r.After.Sides[s].Hazards, r.After.Sides[s].Screens)
				}
			}
			fmt.Printf("\nstatus: turn %d phase %s | %s\n", m.state.Turn, m.state.Phase, statusTail(m))
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("no new resolution within %s", *timeout)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func statusTail(m *matchCtx) string {
	if m.state.Ended() {
		return winnerLine(m.meta, m.state)
	}
	return fmt.Sprintf("awaiting p1=%v p2=%v (submitted p1=%v p2=%v)",
		owesAction(m.state, 0), owesAction(m.state, 1),
		m.pend.Actions[0] != nil, m.pend.Actions[1] != nil)
}

// cmdReport dumps everything about a finished match as one JSON document —
// the input the tournament report is built from.
func cmdReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	root := fs.String("root", "royale", "tournament directory")
	data := fs.String("data", "data", "dataset directory")
	id := fs.String("id", "", "match id")
	token := fs.String("token", "", "judge token")
	_ = fs.Parse(args)
	m, err := openMatch(*root, *data, *id)
	if err != nil {
		return err
	}
	if err := requireJudge(m, *token); err != nil {
		return err
	}
	recs, err := readRecords(m.dir)
	if err != nil {
		return err
	}
	out := map[string]any{
		"meta":        m.meta,
		"turns":       m.state.Turn,
		"phase":       m.state.Phase,
		"winner":      m.state.Winner,
		"winner_name": nameOfWinner(m.meta, m.state),
		"alive":       []int{m.state.LiveCount(0), m.state.LiveCount(1)},
		"records":     recs,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func nameOfWinner(meta Meta, s *engine.BattleState) string {
	if s.Winner == 0 || s.Winner == 1 {
		return meta.Trainers[s.Winner].Name
	}
	if s.Winner == 2 {
		return "draw"
	}
	return ""
}
