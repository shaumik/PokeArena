package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// maxTurns caps a battle so two defensive teams cannot loop forever; at the
// cap the winner is decided on remaining team HP.
const maxTurns = 300

// struggleMove is the typeless fallback used when a Pokémon has no PP left.
// 25% recoil rides on the user via the standard self-effect block.
var struggleMove = domain.Move{
	Name: "Struggle", Type: "", Category: domain.CatPhysical, Power: 50, Accuracy: 100,
	Self: &domain.Effect{Recoil: 0.25},
}

// ResolveTurn advances the battle by one turn given both sides' actions, and
// returns the turn log. It mutates s in place; callers that need the prior
// state should Clone first. The RNG state travels inside s, so resolving the
// same turn from the same state always produces the identical result — which
// is what makes turn resolution safely idempotent under message redelivery.
func ResolveTurn(dex *domain.Dex, s *BattleState, actions [2]Action) []LogLine {
	var log []LogLine
	if s.Phase != PhaseChoosing {
		return log
	}
	rng := NewRNG(s.RNGState)
	defer func() { s.RNGState = rng.State() }()

	s.Turn++
	log = append(log, LogLine{Type: "turn", Side: -1, Text: fmt.Sprintf("— Turn %d —", s.Turn)})

	// Switches always resolve before moves.
	for i := 0; i < 2; i++ {
		if actions[i].Kind == ActionSwitch {
			doSwitch(s, i, actions[i].Index, &log)
		}
	}

	// Movers act in priority-then-speed order.
	var movers []int
	for i := 0; i < 2; i++ {
		if actions[i].Kind == ActionMove {
			movers = append(movers, i)
		}
	}
	for _, side := range orderMovers(dex, s, movers, actions, rng) {
		if s.Active(side).Fainted {
			continue
		}
		executeMove(dex, s, side, actions[side].Index, rng, &log)
	}

	// End-of-turn residual damage (burn, poison).
	for i := 0; i < 2; i++ {
		applyResidual(s, i, &log)
	}

	updatePhase(s, &log)
	return log
}

// ResolveReplace applies forced switches after faints. sw[i] is the switch
// chosen for side i (nil if that side does not need to replace).
func ResolveReplace(s *BattleState, sw [2]*Action) []LogLine {
	var log []LogLine
	if s.Phase != PhaseReplace {
		return log
	}
	for i := 0; i < 2; i++ {
		if s.Replace[i] && sw[i] != nil && sw[i].Kind == ActionSwitch {
			doSwitch(s, i, sw[i].Index, &log)
			s.Replace[i] = false
		}
	}
	if !s.Replace[0] && !s.Replace[1] {
		s.Phase = PhaseChoosing
	}
	return log
}

// orderMovers returns the (at most two) move-takers in execution order.
func orderMovers(dex *domain.Dex, s *BattleState, movers []int, actions [2]Action, rng *RNG) []int {
	if len(movers) < 2 {
		return movers
	}
	a, b := movers[0], movers[1]
	if goesFirst(dex, s, b, a, actions, rng) {
		return []int{b, a}
	}
	return []int{a, b}
}

// goesFirst reports whether side x acts before side y.
func goesFirst(dex *domain.Dex, s *BattleState, x, y int, actions [2]Action, rng *RNG) bool {
	px, py := movePriority(dex, s, x, actions[x].Index), movePriority(dex, s, y, actions[y].Index)
	if px != py {
		return px > py
	}
	sx, sy := effectiveSpeed(s.Active(x)), effectiveSpeed(s.Active(y))
	if sx != sy {
		return sx > sy
	}
	return rng.IntN(2) == 0 // speed tie broken by the seeded RNG
}

func movePriority(dex *domain.Dex, s *BattleState, side, idx int) int {
	act := s.Active(side)
	if idx < 0 || idx >= len(act.Moves) {
		return 0
	}
	return dex.Moves[act.Moves[idx].MoveID].Priority
}

// executeMove runs one Pokémon's move: status gating, PP, accuracy, damage,
// and the move's rider effect.
func executeMove(dex *domain.Dex, s *BattleState, side, moveIdx int, rng *RNG, log *[]LogLine) {
	atk := s.Active(side)
	def := s.Active(1 - side)

	if !canAct(atk, side, rng, log) {
		return
	}

	m := struggleMove
	if moveIdx >= 0 && moveIdx < len(atk.Moves) && atk.Moves[moveIdx].PP > 0 {
		atk.Moves[moveIdx].PP--
		m = dex.Moves[atk.Moves[moveIdx].MoveID]
	}
	*log = append(*log, LogLine{Type: "move", Side: side, Text: fmt.Sprintf("%s used %s!", atk.Name, m.Name)})

	if m.Accuracy > 0 && m.Accuracy < 100 && rng.IntN(100) >= m.Accuracy {
		*log = append(*log, LogLine{Type: "miss", Side: side, Text: fmt.Sprintf("%s's attack missed!", atk.Name)})
		return
	}

	if m.Category == domain.CatStatus {
		applyStatusMove(s, side, m, rng, log)
		return
	}

	res := computeDamage(dex, atk, def, m, rng)
	if res.Effectiveness == 0 {
		*log = append(*log, LogLine{Type: "immune", Side: side, Text: fmt.Sprintf("It doesn't affect %s...", def.Name)})
		return
	}
	dmg := res.Damage
	if dmg > def.HP {
		dmg = def.HP
	}
	def.HP -= dmg
	*log = append(*log, LogLine{Type: "damage", Side: 1 - side, Text: fmt.Sprintf("%s took %d damage.", def.Name, dmg)})
	if res.Crit {
		*log = append(*log, LogLine{Type: "crit", Side: side, Text: "A critical hit!"})
	}
	if res.Effectiveness > 1 {
		*log = append(*log, LogLine{Type: "effective", Side: side, Text: "It's super effective!"})
	} else if res.Effectiveness < 1 {
		*log = append(*log, LogLine{Type: "resisted", Side: side, Text: "It's not very effective..."})
	}

	applyDamageEffects(s, side, m, dmg, rng, log)

	if def.HP <= 0 {
		faint(def, 1-side, log)
	}
	if atk.HP <= 0 {
		faint(atk, side, log)
	}
}

// canAct applies pre-move status checks and reports whether the Pokémon moves.
func canAct(p *Pokemon, side int, rng *RNG, log *[]LogLine) bool {
	switch p.Status {
	case StatusFreeze:
		if rng.Chance(20) {
			p.Status = StatusNone
			*log = append(*log, LogLine{Type: "status", Side: side, Text: p.Name + " thawed out!"})
			return true
		}
		*log = append(*log, LogLine{Type: "status", Side: side, Text: p.Name + " is frozen solid!"})
		return false
	case StatusSleep:
		if p.SleepTurns > 0 {
			p.SleepTurns--
		}
		if p.SleepTurns <= 0 {
			p.Status = StatusNone
			*log = append(*log, LogLine{Type: "status", Side: side, Text: p.Name + " woke up!"})
			return true
		}
		*log = append(*log, LogLine{Type: "status", Side: side, Text: p.Name + " is fast asleep."})
		return false
	case StatusParalysis:
		if rng.Chance(25) {
			*log = append(*log, LogLine{Type: "status", Side: side, Text: p.Name + " is paralyzed! It can't move!"})
			return false
		}
	}
	return true
}

// applyStatusMove handles the guaranteed primary effect of a status-category
// move. The primary applies to the move's declared target.
func applyStatusMove(s *BattleState, side int, m domain.Move, rng *RNG, log *[]LogLine) {
	if m.Primary == nil {
		return
	}
	atk := s.Active(side)
	def := s.Active(1 - side)
	tgt, tside := def, 1-side
	if m.Target == domain.TargetSelf {
		tgt, tside = atk, side
	}
	if failed := applyEffectFields(m.Primary, atk, side, tgt, tside, 0, rng, log); failed {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
	}
}

// applyDamageEffects runs the post-damage effects of a damaging move: the
// guaranteed Self block on the user and each rolled Secondary on the foe.
func applyDamageEffects(s *BattleState, side int, m domain.Move, dmg int, rng *RNG, log *[]LogLine) {
	atk := s.Active(side)
	def := s.Active(1 - side)
	if m.Self != nil {
		applyEffectFields(m.Self, atk, side, atk, side, dmg, rng, log)
	}
	for i := range m.Secondaries {
		sec := &m.Secondaries[i]
		if rng.Chance(sec.Chance) {
			applyEffectFields(sec, atk, side, def, 1-side, dmg, rng, log)
		}
	}
}

// applyEffectFields applies an Effect block. atk/atkSide is the user; tgt/
// tgtSide is the block's target (foe for damage-move secondaries; the move's
// declared target for primaries; the user for self blocks). dmgDealt is the
// damage just dealt (for drain/recoil), zero for status moves. Returns true
// only if a status-infliction attempt failed (callers decide whether to log
// "But it failed!" — secondaries are silent, primaries are loud).
//
// Heal/Drain/Recoil/Cure/Rest always act on the user regardless of tgt; the
// other fields act on tgt. This matches canonical Pokémon mechanics: drain
// heals the attacker even though it's "on" a hit against the foe.
func applyEffectFields(e *domain.Effect, atk *Pokemon, atkSide int, tgt *Pokemon, tgtSide int, dmgDealt int, rng *RNG, log *[]LogLine) (statusFailed bool) {
	if len(e.Boosts) > 0 {
		for _, stat := range orderedBoostStats(e.Boosts) {
			applyStages(tgt, tgtSide, stat, e.Boosts[stat], log)
		}
	}
	if e.Status != "" {
		if !inflictStatus(tgt, tgtSide, StatusCond(e.Status), rng, log) {
			statusFailed = true
		}
	}
	if e.Volatile != "" {
		applyVolatile(tgt, tgtSide, e.Volatile, rng, log)
	}
	if e.Heal > 0 {
		amt := int(float64(atk.MaxHP) * e.Heal)
		healPokemon(atk, atkSide, amt, log)
	}
	if e.Drain > 0 && dmgDealt > 0 {
		amt := int(float64(dmgDealt) * e.Drain)
		healPokemon(atk, atkSide, amt, log)
	}
	if e.Recoil > 0 && dmgDealt > 0 {
		amt := int(float64(dmgDealt) * e.Recoil)
		applySelfDamage(atk, atkSide, amt, log)
	}
	// Cure and Rest are filled in by task #9 (bug fixes & polish).
	_ = e.Cure
	_ = e.Rest
	return statusFailed
}

// applyVolatile inflicts a volatile condition on the target. The body is
// filled in by task #7 (confusion) and task #8 (flinch); for now it is a stub
// so unknown volatiles in the dataset (validated by domain) do not silently
// crash.
func applyVolatile(p *Pokemon, side int, name string, rng *RNG, log *[]LogLine) {
	// task 7 / 8 will populate this switch.
	_ = p
	_ = side
	_ = name
	_ = rng
	_ = log
}

// orderedBoostStats returns the keys of a boost map in a stable order so the
// turn log is deterministic regardless of map iteration.
func orderedBoostStats(b map[string]int) []string {
	order := []string{"attack", "defense", "spatk", "spdef", "speed", "accuracy", "evasion"}
	out := make([]string, 0, len(b))
	for _, k := range order {
		if _, ok := b[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

// inflictStatus applies a non-volatile status, respecting type immunities and
// the one-status-at-a-time rule. It reports whether the status took hold.
func inflictStatus(p *Pokemon, side int, st StatusCond, rng *RNG, log *[]LogLine) bool {
	if p.Status != StatusNone || p.Fainted {
		return false
	}
	switch st {
	case StatusBurn:
		if isType(p, "fire") {
			return false
		}
	case StatusFreeze:
		if isType(p, "ice") {
			return false
		}
	case StatusParalysis:
		if isType(p, "electric") {
			return false
		}
	case StatusPoison:
		if isType(p, "poison") || isType(p, "steel") {
			return false
		}
	}
	p.Status = st
	if st == StatusSleep {
		p.SleepTurns = rng.Range(1, 3)
	}
	*log = append(*log, LogLine{Type: "status", Side: side, Text: fmt.Sprintf("%s was %s!", p.Name, statusVerb(st))})
	return true
}

// applyStages changes a stat stage, clamped to -6..+6.
func applyStages(p *Pokemon, side int, stat string, delta int, log *[]LogLine) {
	ptr := stagePtr(p, stat)
	if ptr == nil {
		return
	}
	old := *ptr
	*ptr += delta
	if *ptr > 6 {
		*ptr = 6
	}
	if *ptr < -6 {
		*ptr = -6
	}
	if *ptr == old {
		dir := "higher"
		if delta < 0 {
			dir = "lower"
		}
		*log = append(*log, LogLine{Type: "stat", Side: side, Text: fmt.Sprintf("%s's %s won't go %s!", p.Name, statName(stat), dir)})
		return
	}
	verb := "rose"
	if delta < 0 {
		verb = "fell"
	}
	if delta <= -2 || delta >= 2 {
		verb += " sharply"
	}
	*log = append(*log, LogLine{Type: "stat", Side: side, Text: fmt.Sprintf("%s's %s %s!", p.Name, statName(stat), verb)})
}

// applyResidual applies end-of-turn burn / poison damage.
func applyResidual(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	if p.Fainted {
		return
	}
	var dmg int
	switch p.Status {
	case StatusBurn:
		dmg = p.MaxHP / 16
	case StatusPoison:
		dmg = p.MaxHP / 8
	default:
		return
	}
	if dmg < 1 {
		dmg = 1
	}
	if dmg > p.HP {
		dmg = p.HP
	}
	p.HP -= dmg
	*log = append(*log, LogLine{Type: "status", Side: side,
		Text: fmt.Sprintf("%s is hurt by its %s! (-%d)", p.Name, p.Status, dmg)})
	if p.HP <= 0 {
		faint(p, side, log)
	}
}

// doSwitch brings in a teammate, resetting stat stages on both Pokémon.
func doSwitch(s *BattleState, side, idx int, log *[]LogLine) {
	sd := &s.Sides[side]
	if idx < 0 || idx >= len(sd.Team) || idx == sd.Active || sd.Team[idx].Fainted {
		return
	}
	out := &sd.Team[sd.Active]
	out.Stages = Stages{}
	if !out.Fainted {
		*log = append(*log, LogLine{Type: "switch", Side: side, Text: fmt.Sprintf("%s, come back!", out.Name)})
	}
	sd.Active = idx
	in := &sd.Team[idx]
	in.Stages = Stages{}
	*log = append(*log, LogLine{Type: "switch", Side: side, Text: fmt.Sprintf("Go, %s!", in.Name)})
}

// updatePhase recomputes the battle phase and winner after a turn.
func updatePhase(s *BattleState, log *[]LogLine) {
	l0, l1 := s.LiveCount(0), s.LiveCount(1)
	switch {
	case l0 == 0 && l1 == 0:
		endBattle(s, 2, log)
		return
	case l0 == 0:
		endBattle(s, 1, log)
		return
	case l1 == 0:
		endBattle(s, 0, log)
		return
	}
	if s.Turn >= maxTurns {
		f0, f1 := hpFraction(s, 0), hpFraction(s, 1)
		switch {
		case f0 > f1:
			endBattle(s, 0, log)
		case f1 > f0:
			endBattle(s, 1, log)
		default:
			endBattle(s, 2, log)
		}
		return
	}
	s.Replace[0] = s.Active(0).Fainted
	s.Replace[1] = s.Active(1).Fainted
	if s.Replace[0] || s.Replace[1] {
		s.Phase = PhaseReplace
	} else {
		s.Phase = PhaseChoosing
	}
}

func endBattle(s *BattleState, winner int, log *[]LogLine) {
	s.Phase = PhaseEnded
	s.Winner = winner
	s.Replace = [2]bool{}
	switch winner {
	case 2:
		*log = append(*log, LogLine{Type: "win", Side: -1, Text: "The battle ended in a draw!"})
	default:
		*log = append(*log, LogLine{Type: "win", Side: winner,
			Text: fmt.Sprintf("%s won the battle!", s.Sides[winner].Trainer)})
	}
}

func hpFraction(s *BattleState, side int) float64 {
	var cur, max float64
	for i := range s.Sides[side].Team {
		cur += float64(s.Sides[side].Team[i].HP)
		max += float64(s.Sides[side].Team[i].MaxHP)
	}
	if max == 0 {
		return 0
	}
	return cur / max
}

func faint(p *Pokemon, side int, log *[]LogLine) {
	if p.Fainted {
		return
	}
	p.HP = 0
	p.Fainted = true
	p.Status = StatusNone
	*log = append(*log, LogLine{Type: "faint", Side: side, Text: p.Name + " fainted!"})
}

func healPokemon(p *Pokemon, side, amt int, log *[]LogLine) {
	if amt < 1 {
		amt = 1
	}
	before := p.HP
	p.HP += amt
	if p.HP > p.MaxHP {
		p.HP = p.MaxHP
	}
	if p.HP > before {
		*log = append(*log, LogLine{Type: "heal", Side: side,
			Text: fmt.Sprintf("%s restored %d HP.", p.Name, p.HP-before)})
	}
}

func applySelfDamage(p *Pokemon, side, amt int, log *[]LogLine) {
	if amt < 1 {
		amt = 1
	}
	if amt > p.HP {
		amt = p.HP
	}
	p.HP -= amt
	*log = append(*log, LogLine{Type: "recoil", Side: side,
		Text: fmt.Sprintf("%s is hit with recoil! (-%d)", p.Name, amt)})
}

func isType(p *Pokemon, t domain.Type) bool { return p.Type1 == t || p.Type2 == t }

func stagePtr(p *Pokemon, stat string) *int {
	switch stat {
	case "attack":
		return &p.Stages.Atk
	case "defense":
		return &p.Stages.Def
	case "spatk":
		return &p.Stages.SpA
	case "spdef":
		return &p.Stages.SpD
	case "speed":
		return &p.Stages.Spe
	case "accuracy":
		return &p.Stages.Acc
	case "evasion":
		return &p.Stages.Eva
	}
	return nil
}

func statName(stat string) string {
	switch stat {
	case "attack":
		return "Attack"
	case "defense":
		return "Defense"
	case "spatk":
		return "Sp. Atk"
	case "spdef":
		return "Sp. Def"
	case "speed":
		return "Speed"
	case "accuracy":
		return "accuracy"
	case "evasion":
		return "evasion"
	}
	return stat
}

func statusVerb(st StatusCond) string {
	switch st {
	case StatusBurn:
		return "burned"
	case StatusPoison:
		return "poisoned"
	case StatusToxic:
		return "badly poisoned"
	case StatusParalysis:
		return "paralyzed"
	case StatusSleep:
		return "put to sleep"
	case StatusFreeze:
		return "frozen solid"
	}
	return string(st)
}
