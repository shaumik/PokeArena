// Command arena-ref is a headless referee for a two-agent exhibition battle.
// It drives the engine directly — no gateway, no queues, no database — so a
// battle can be played one turn at a time from a shell, with each side handed
// only its own fog-of-war view.
//
// The point is a battle that an external controller (a human, a script, a
// subagent in a chat session) can play move by move while a third party
// watches the whole board. Every mutating command re-runs the engine's state
// invariants and reports a violation loudly, which makes this a bug detector
// as much as a referee.
//
// Usage:
//
//	arena-ref new   -p1 red.json -p2 blue.json -n1 Red -n2 Blue -seed 7
//	arena-ref view  -side 0                # fog-of-war view + legal actions
//	arena-ref board                        # full-information judge's board
//	arena-ref turn  -a1 move:0 -a2 switch:3
//
// Team files are JSON arrays of engine.TeamPick: six species, 1–4 legal moves
// each, plus optional ability / item / EVs / IVs / nature.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

const defaultStatePath = "battle-state.json"

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
	case "board":
		err = cmdBoard(os.Args[2:])
	case "turn":
		err = cmdTurn(os.Args[2:])
	case "replay":
		err = cmdReplay(os.Args[2:])
	case "validate":
		err = cmdValidate(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "arena-ref: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `arena-ref — headless referee for a two-controller battle

  new       -p1 FILE -p2 FILE [-n1 NAME -n2 NAME -seed N -state FILE]
  validate  -team FILE
  view      -side 0|1 [-state FILE] [-json]
  board     [-state FILE]
  turn      -a1 ACTION -a2 ACTION [-state FILE]
  replay    -p1 FILE -p2 FILE -seed N -actions FILE [-out FILE]

ACTION is "move:N", "switch:N", or "struggle".
`)
}

// ---------- dex + state plumbing ----------

func loadDex(dir string) (*domain.Dex, error) { return domain.LoadDex(dir, "arena-ref") }

func loadState(path string) (*engine.BattleState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s engine.BattleState
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("decode state: %w", err)
	}
	return &s, nil
}

func saveState(path string, s *engine.BattleState) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func loadPicks(path string) ([]engine.TeamPick, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var picks []engine.TeamPick
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields() // a typo'd field is a silently-ignored strategy otherwise
	if err := dec.Decode(&picks); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return picks, nil
}

// ---------- commands ----------

func cmdValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ExitOnError)
	team := fs.String("team", "", "team JSON file")
	dataDir := fs.String("data", "data", "dataset directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dex, err := loadDex(*dataDir)
	if err != nil {
		return err
	}
	picks, err := loadPicks(*team)
	if err != nil {
		return err
	}
	if err := engine.ValidateTeam(picks, dex); err != nil {
		return err
	}
	fmt.Printf("✓ %s is a legal team\n\n", *team)
	printTeamSheet(dex, picks)
	return nil
}

// printTeamSheet renders the derived stats a spread actually buys, which is
// the only way to check an EV allocation without replaying the stat formula
// by hand.
func printTeamSheet(dex *domain.Dex, picks []engine.TeamPick) error {
	s, err := engine.NewBattleFromPicks(dex, "sheet", "T", picks, "T", picks, 1)
	if err != nil {
		return err
	}
	for i := range s.Sides[0].Team {
		p := &s.Sides[0].Team[i]
		fmt.Printf("%-12s %-16s %-14s %s\n", p.Name, typeLine(p), string(p.Ability), itemLabel(dex, string(p.Item)))
		fmt.Printf("  %-4s HP %3d | Atk %3d Def %3d SpA %3d SpD %3d Spe %3d\n",
			natureLabel(p.Nature), p.MaxHP, p.Stats.Atk, p.Stats.Def, p.Stats.SpA, p.Stats.SpD, p.Stats.Spe)
		fmt.Printf("  EVs %s\n", statLine(p.EVs))
		fmt.Printf("  moves: %s\n\n", strings.Join(moveNames(dex, p.Moves), ", "))
	}
	return nil
}

func cmdNew(args []string) error {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	p1 := fs.String("p1", "", "side 0 team JSON")
	p2 := fs.String("p2", "", "side 1 team JSON")
	n1 := fs.String("n1", "P1", "side 0 trainer name")
	n2 := fs.String("n2", "P2", "side 1 trainer name")
	seed := fs.Uint64("seed", 1, "RNG seed (the whole battle replays from it)")
	statePath := fs.String("state", defaultStatePath, "state file to write")
	dataDir := fs.String("data", "data", "dataset directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dex, err := loadDex(*dataDir)
	if err != nil {
		return err
	}
	picks1, err := loadPicks(*p1)
	if err != nil {
		return err
	}
	picks2, err := loadPicks(*p2)
	if err != nil {
		return err
	}
	if err := engine.ValidateTeam(picks1, dex); err != nil {
		return fmt.Errorf("%s: %w", *n1, err)
	}
	if err := engine.ValidateTeam(picks2, dex); err != nil {
		return fmt.Errorf("%s: %w", *n2, err)
	}
	s, err := engine.NewBattleFromPicks(dex, "exhibition", *n1, picks1, *n2, picks2, *seed)
	if err != nil {
		return err
	}
	if err := engine.ValidateStateInvariants(s); err != nil {
		return fmt.Errorf("INVARIANT VIOLATION at battle start: %w", err)
	}
	if err := saveState(*statePath, s); err != nil {
		return err
	}
	fmt.Printf("✓ battle created: %s vs %s, seed %d, level %d\n", *n1, *n2, *seed, engine.Level)
	return printBoard(dex, s)
}

func cmdView(args []string) error {
	fs := flag.NewFlagSet("view", flag.ExitOnError)
	side := fs.Int("side", -1, "side index (0 or 1)")
	statePath := fs.String("state", defaultStatePath, "state file")
	dataDir := fs.String("data", "data", "dataset directory")
	asJSON := fs.Bool("json", false, "emit the raw fog-of-war view JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *side != 0 && *side != 1 {
		return errors.New("-side must be 0 or 1")
	}
	dex, err := loadDex(*dataDir)
	if err != nil {
		return err
	}
	s, err := loadState(*statePath)
	if err != nil {
		return err
	}
	v := ai.MakeView(s, *side)
	if *asJSON {
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(raw))
		return nil
	}
	return printView(dex, s, v)
}

func cmdBoard(args []string) error {
	fs := flag.NewFlagSet("board", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "state file")
	dataDir := fs.String("data", "data", "dataset directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dex, err := loadDex(*dataDir)
	if err != nil {
		return err
	}
	s, err := loadState(*statePath)
	if err != nil {
		return err
	}
	return printBoard(dex, s)
}

func cmdTurn(args []string) error {
	fs := flag.NewFlagSet("turn", flag.ExitOnError)
	a1 := fs.String("a1", "", "side 0 action: move:N | switch:N | struggle")
	a2 := fs.String("a2", "", "side 1 action")
	statePath := fs.String("state", defaultStatePath, "state file")
	dataDir := fs.String("data", "data", "dataset directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dex, err := loadDex(*dataDir)
	if err != nil {
		return err
	}
	s, err := loadState(*statePath)
	if err != nil {
		return err
	}
	if s.Ended() {
		return errors.New("battle already ended")
	}

	replace := s.Phase == engine.PhaseReplace
	var acts [2]engine.Action
	var swp [2]*engine.Action
	for i, raw := range []string{*a1, *a2} {
		// In a replace phase only the side with a fainted active submits.
		if replace && !s.Replace[i] {
			if raw != "" {
				return fmt.Errorf("side %d does not need to replace, but got %q", i, raw)
			}
			continue
		}
		if raw == "" {
			return fmt.Errorf("side %d: no action given", i)
		}
		a, err := parseAction(raw)
		if err != nil {
			return fmt.Errorf("side %d: %w", i, err)
		}
		if err := checkLegal(dex, s, i, a); err != nil {
			return fmt.Errorf("side %d: %w", i, err)
		}
		acts[i] = a
		aa := a
		swp[i] = &aa
	}

	before := snapshotHP(s)
	var log []engine.LogLine
	if replace {
		log = engine.ResolveReplace(s, swp)
	} else {
		log = engine.ResolveTurn(dex, s, acts)
	}

	for _, l := range log {
		fmt.Println(renderLog(s, l))
	}
	fmt.Println()
	fmt.Println(hpDelta(s, before))

	if err := engine.ValidateStateInvariants(s); err != nil {
		fmt.Printf("\n!! INVARIANT VIOLATION after %s: %v\n", map[bool]string{true: "replace", false: "turn"}[replace], err)
	}
	if err := saveState(*statePath, s); err != nil {
		return err
	}
	fmt.Println()
	return printBoard(dex, s)
}

// ---------- action parsing ----------

func parseAction(raw string) (engine.Action, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "struggle" {
		return engine.Action{Kind: engine.ActionMove, Index: engine.StruggleMoveIndex}, nil
	}
	kind, idxRaw, ok := strings.Cut(raw, ":")
	if !ok {
		return engine.Action{}, fmt.Errorf("bad action %q (want move:N or switch:N)", raw)
	}
	idx, err := strconv.Atoi(strings.TrimSpace(idxRaw))
	if err != nil {
		return engine.Action{}, fmt.Errorf("bad index in %q", raw)
	}
	switch kind {
	case "move", "m":
		return engine.Action{Kind: engine.ActionMove, Index: idx}, nil
	case "switch", "s":
		return engine.Action{Kind: engine.ActionSwitch, Index: idx}, nil
	}
	return engine.Action{}, fmt.Errorf("bad action kind %q", kind)
}

// checkLegal refuses an action the engine would not have offered. The engine
// defends itself anyway, but a rejected illegal submission is a much clearer
// signal to a controller than a silently redirected one.
func checkLegal(dex *domain.Dex, s *engine.BattleState, side int, a engine.Action) error {
	legal := engine.LegalActionsDex(dex, s, side)
	for _, l := range legal {
		if l == a {
			return nil
		}
	}
	labels := make([]string, len(legal))
	for i, l := range legal {
		labels[i] = actionSlug(l)
	}
	return fmt.Errorf("%s is not legal right now; legal: %s", actionSlug(a), strings.Join(labels, " "))
}

func actionSlug(a engine.Action) string {
	if a.Kind == engine.ActionMove && a.Index == engine.StruggleMoveIndex {
		return "struggle"
	}
	return fmt.Sprintf("%s:%d", a.Kind, a.Index)
}

// ---------- rendering ----------

func printView(dex *domain.Dex, s *engine.BattleState, v ai.View) error {
	me := &v.Self
	act := &me.Team[me.Active]
	fmt.Printf("=== TURN %d — %s (side %d) ===\n", v.Turn, me.Trainer, v.Me)
	if v.Replace {
		fmt.Println("PHASE: replace — your active fainted, send in a teammate")
	}
	fmt.Println(fieldLine(s.Weather, s.Terrain, s.PseudoWeather))
	fmt.Printf("your side: %s\n", conditionsLine(me.Conditions, me.SlotConditions))
	fmt.Printf("foe side:  %s\n", conditionsLine(v.FoeConditions, engine.SlotConditions{}))

	fmt.Printf("\nYOUR ACTIVE  %s\n", pokeLine(dex, act, true))
	for i, m := range act.Moves {
		md := dex.Moves[m.MoveID]
		fmt.Printf("   move:%d  %-18s %-9s %-8s pow %3d acc %3d  pp %2d/%-2d%s\n",
			i, md.Name, md.Type, md.Category, md.Power, md.Accuracy, m.PP, m.MaxPP, priorityNote(md))
	}
	fmt.Printf("\nYOUR BENCH\n")
	for i := range me.Team {
		if i == me.Active {
			continue
		}
		fmt.Printf("   switch:%d %s\n", i, pokeLine(dex, &me.Team[i], true))
	}

	fmt.Printf("\nFOE ACTIVE   %s\n", pokeLine(dex, &v.Foe, false))
	fmt.Printf("   revealed moves: %s\n", revealedMoves(dex, v.Foe.Moves))
	fmt.Printf("   foe bench alive: %d\n", v.FoeBenchAlive)

	fmt.Printf("\nLEGAL ACTIONS: %s\n", strings.Join(legalLabels(dex, s, v.Me), "  "))
	return nil
}

func legalLabels(dex *domain.Dex, s *engine.BattleState, side int) []string {
	legal := engine.LegalActionsDex(dex, s, side)
	out := make([]string, 0, len(legal))
	for _, a := range legal {
		switch {
		case a.Kind == engine.ActionMove && a.Index == engine.StruggleMoveIndex:
			out = append(out, "struggle")
		case a.Kind == engine.ActionMove:
			mv := s.Sides[side].Team[s.Sides[side].Active].Moves
			name := "?"
			if a.Index >= 0 && a.Index < len(mv) {
				name = dex.Moves[mv[a.Index].MoveID].Name
			}
			out = append(out, fmt.Sprintf("move:%d(%s)", a.Index, name))
		default:
			out = append(out, fmt.Sprintf("switch:%d(%s)", a.Index, s.Sides[side].Team[a.Index].Name))
		}
	}
	return out
}

func printBoard(dex *domain.Dex, s *engine.BattleState) error {
	fmt.Printf("┌─ TURN %d — phase %s", s.Turn, s.Phase)
	if s.Winner >= 0 {
		switch s.Winner {
		case 2:
			fmt.Printf(" — DRAW")
		default:
			fmt.Printf(" — WINNER: %s", s.Sides[s.Winner].Trainer)
		}
	}
	fmt.Printf("\n│ %s\n", fieldLine(s.Weather, s.Terrain, s.PseudoWeather))
	for i := range s.Sides {
		sd := &s.Sides[i]
		fmt.Printf("│\n│ [%d] %-14s %s\n", i, sd.Trainer, conditionsLine(sd.Conditions, sd.SlotConditions))
		for j := range sd.Team {
			marker := "  "
			if j == sd.Active {
				marker = "▶ "
			}
			fmt.Printf("│  %s%d %s\n", marker, j, pokeLine(dex, &sd.Team[j], true))
		}
	}
	fmt.Println("└─")
	return nil
}

func pokeLine(dex *domain.Dex, p *engine.Pokemon, own bool) string {
	var b strings.Builder
	name := p.Name
	if p.Fainted {
		name += " (FNT)"
	}
	fmt.Fprintf(&b, "%-14s %-13s", name, typeLine(p))
	if own {
		pct := 0
		if p.MaxHP > 0 {
			pct = p.HP * 100 / p.MaxHP
		}
		fmt.Fprintf(&b, " %3d/%-3d (%3d%%)", p.HP, p.MaxHP, pct)
	} else {
		// Foe HP is already bucketed by the fog projection; show the ratio only.
		pct := 0
		if p.MaxHP > 0 {
			pct = p.HP * 100 / p.MaxHP
		}
		fmt.Fprintf(&b, " ~%d%%", pct)
	}
	if p.Status != engine.StatusNone {
		fmt.Fprintf(&b, " [%s]", strings.ToUpper(string(p.Status))[:3])
	}
	if st := stageLine(p.Stages); st != "" {
		fmt.Fprintf(&b, " %s", st)
	}
	if vol := volatileLine(p); vol != "" {
		fmt.Fprintf(&b, " {%s}", vol)
	}
	if own {
		if p.Item != "" {
			fmt.Fprintf(&b, " @%s", itemLabel(dex, string(p.Item)))
		}
		if p.Ability != "" {
			fmt.Fprintf(&b, " ·%s", p.Ability)
		}
	}
	return b.String()
}

func typeLine(p *engine.Pokemon) string {
	if p.Type2 == "" {
		return string(p.Type1)
	}
	return string(p.Type1) + "/" + string(p.Type2)
}

func stageLine(st engine.Stages) string {
	pairs := []struct {
		k string
		v int
	}{{"Atk", st.Atk}, {"Def", st.Def}, {"SpA", st.SpA}, {"SpD", st.SpD}, {"Spe", st.Spe}, {"Acc", st.Acc}, {"Eva", st.Eva}}
	var out []string
	for _, p := range pairs {
		if p.v != 0 {
			out = append(out, fmt.Sprintf("%s%+d", p.k, p.v))
		}
	}
	return strings.Join(out, ",")
}

func volatileLine(p *engine.Pokemon) string {
	v := p.Volatiles
	var out []string
	add := func(cond bool, s string) {
		if cond {
			out = append(out, s)
		}
	}
	add(v.Confusion != nil, "confused")
	add(v.Substitute != nil, "sub")
	add(v.LeechSeed != nil, "seeded")
	add(v.Charging != nil, "charging")
	add(v.LockedMove != nil, "locked")
	add(v.PartialTrap != nil, "trapped")
	add(v.Taunt != nil, "taunt")
	add(v.Encore != nil, "encore")
	add(v.Disable != nil, "disable")
	add(v.Attract, "attract")
	add(v.Yawn != nil, "yawn")
	add(v.Ingrain, "ingrain")
	add(v.AquaRing, "aqua-ring")
	add(v.MustRecharge, "recharge")
	add(v.Protect, "protected")
	add(v.Nightmare, "nightmare")
	add(v.Curse, "cursed")
	add(v.DestinyBond, "destiny-bond")
	add(v.MagnetRise != nil, "magnet-rise")
	add(v.SmackDown, "smacked-down")
	add(v.GastroAcid, "ability-suppressed")
	add(v.Unburden, "unburden")
	add(v.ChoiceLockMoveID != "", "choice-locked:"+v.ChoiceLockMoveID)
	return strings.Join(out, ",")
}

func fieldLine(w *engine.WeatherState, t *engine.TerrainState, pw engine.PseudoWeather) string {
	var out []string
	if w != nil {
		out = append(out, fmt.Sprintf("weather=%s(%d)", w.Kind, w.TurnsLeft))
	}
	if t != nil {
		out = append(out, fmt.Sprintf("terrain=%s(%d)", t.Kind, t.TurnsLeft))
	}
	if pw.TrickRoom != nil {
		out = append(out, fmt.Sprintf("trick-room(%d)", pw.TrickRoom.TurnsLeft))
	}
	if pw.Gravity != nil {
		out = append(out, fmt.Sprintf("gravity(%d)", pw.Gravity.TurnsLeft))
	}
	if pw.MagicRoom != nil {
		out = append(out, fmt.Sprintf("magic-room(%d)", pw.MagicRoom.TurnsLeft))
	}
	if pw.WonderRoom != nil {
		out = append(out, fmt.Sprintf("wonder-room(%d)", pw.WonderRoom.TurnsLeft))
	}
	if len(out) == 0 {
		return "field: clear"
	}
	return "field: " + strings.Join(out, " ")
}

func conditionsLine(c engine.SideConditions, sc engine.SlotConditions) string {
	var out []string
	if c.Hazards.StealthRock {
		out = append(out, "stealth-rock")
	}
	if c.Hazards.Spikes > 0 {
		out = append(out, fmt.Sprintf("spikes×%d", c.Hazards.Spikes))
	}
	if c.Hazards.ToxicSpikes > 0 {
		out = append(out, fmt.Sprintf("toxic-spikes×%d", c.Hazards.ToxicSpikes))
	}
	if c.Reflect != nil {
		out = append(out, fmt.Sprintf("reflect(%d)", c.Reflect.TurnsLeft))
	}
	if c.LightScreen != nil {
		out = append(out, fmt.Sprintf("light-screen(%d)", c.LightScreen.TurnsLeft))
	}
	if c.AuroraVeil != nil {
		out = append(out, fmt.Sprintf("aurora-veil(%d)", c.AuroraVeil.TurnsLeft))
	}
	if c.Safeguard != nil {
		out = append(out, fmt.Sprintf("safeguard(%d)", c.Safeguard.TurnsLeft))
	}
	if c.Mist != nil {
		out = append(out, fmt.Sprintf("mist(%d)", c.Mist.TurnsLeft))
	}
	if c.Tailwind != nil {
		out = append(out, fmt.Sprintf("tailwind(%d)", c.Tailwind.TurnsLeft))
	}
	if sc.Wish != nil {
		out = append(out, fmt.Sprintf("wish(%d)", sc.Wish.TurnsLeft))
	}
	if sc.HealingWish {
		out = append(out, "healing-wish")
	}
	if len(out) == 0 {
		return "no side conditions"
	}
	return strings.Join(out, " ")
}

func revealedMoves(dex *domain.Dex, ms []engine.MoveSlot) string {
	var out []string
	for _, m := range ms {
		if m.MoveID == "" {
			continue
		}
		md := dex.Moves[m.MoveID]
		name := md.Name
		if name == "" {
			name = m.MoveID
		}
		out = append(out, name)
	}
	if len(out) == 0 {
		return "(none seen yet)"
	}
	return strings.Join(out, ", ")
}

func moveNames(dex *domain.Dex, ms []engine.MoveSlot) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = dex.Moves[m.MoveID].Name
	}
	return out
}

func priorityNote(m domain.Move) string {
	if m.Priority == 0 {
		return ""
	}
	return fmt.Sprintf("  prio %+d", m.Priority)
}

func itemLabel(dex *domain.Dex, id string) string {
	if id == "" {
		return "(no item)"
	}
	if it, ok := dex.Items[id]; ok {
		return it.Name
	}
	return id
}

func natureLabel(n string) string {
	if n == "" {
		return "neutral"
	}
	return n
}

func statLine(s domain.Stats) string {
	parts := []string{}
	for _, kv := range []struct {
		k string
		v int
	}{{"HP", s.HP}, {"Atk", s.Atk}, {"Def", s.Def}, {"SpA", s.SpA}, {"SpD", s.SpD}, {"Spe", s.Spe}} {
		if kv.v != 0 {
			parts = append(parts, fmt.Sprintf("%d %s", kv.v, kv.k))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " / ")
}

func renderLog(s *engine.BattleState, l engine.LogLine) string {
	who := ""
	if l.Side >= 0 && l.Side < 2 {
		who = "[" + s.Sides[l.Side].Trainer + "] "
	}
	return "  " + who + l.Text
}

type hpKey struct{ side, slot int }

func snapshotHP(s *engine.BattleState) map[hpKey]int {
	out := map[hpKey]int{}
	for i := range s.Sides {
		for j := range s.Sides[i].Team {
			out[hpKey{i, j}] = s.Sides[i].Team[j].HP
		}
	}
	return out
}

// hpDelta reports every HP change the turn produced, which is the fastest way
// to spot damage the log forgot to mention.
func hpDelta(s *engine.BattleState, before map[hpKey]int) string {
	type row struct {
		key  hpKey
		text string
	}
	var rows []row
	for i := range s.Sides {
		for j := range s.Sides[i].Team {
			p := &s.Sides[i].Team[j]
			was := before[hpKey{i, j}]
			if was == p.HP {
				continue
			}
			pct := 0
			if p.MaxHP > 0 {
				pct = (p.HP - was) * 100 / p.MaxHP
			}
			rows = append(rows, row{hpKey{i, j}, fmt.Sprintf("  %s: %d → %d (%+d%%)", p.Name, was, p.HP, pct)})
		}
	}
	if len(rows) == 0 {
		return "  (no HP changed)"
	}
	sort.Slice(rows, func(a, b int) bool {
		if rows[a].key.side != rows[b].key.side {
			return rows[a].key.side < rows[b].key.side
		}
		return rows[a].key.slot < rows[b].key.slot
	})
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.text
	}
	return "HP changes:\n" + strings.Join(out, "\n")
}
