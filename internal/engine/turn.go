package engine

import (
	"fmt"
	"math"

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

	// Lead on-switch-in: triggers like Intimidate that fire when a Pokémon
	// "enters the field" should also fire for the starting leads, who never
	// went through doSwitch. We piggyback on turn 1 rather than burdening
	// NewBattle/NewBattleFromPicks with a log channel.
	if s.Turn == 1 {
		applyOnSwitchIn(s, 0, &log)
		applyOnSwitchIn(s, 1, &log)
	}

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
	ordered := orderMovers(dex, s, movers, actions, rng)
	for i, side := range ordered {
		if s.Active(side).Fainted {
			continue
		}
		// Mark the last scheduled mover so Analytic can read it from inside
		// computeDamage. Set before executeMove so the hook sees true on the
		// same move it modifies.
		if i == len(ordered)-1 {
			s.Active(side).Volatiles.MovedLast = true
		}
		executeMove(dex, s, side, actions[side].Index, rng, &log)
	}

	// End-of-turn residual damage (burn, poison, toxic).
	for i := 0; i < 2; i++ {
		applyResidual(s, i, &log)
	}

	// Weather residual chip + counter tick. Sandstorm chips Side 0 then
	// Side 1 (stable order; speed ordering doesn't matter for a
	// non-interactive residual).
	applyWeatherResidual(s, &log)
	tickWeather(s, &log)

	// Terrain residual (Grassy heal) then counter tick. Same stable
	// side-0-then-side-1 order as weather. Cloud Nine does NOT suppress
	// terrain in Gen 8+, so we read s.Terrain directly without an
	// "effective" filter.
	applyTerrainResidual(s, &log)
	tickTerrain(s, &log)

	// Per-side screens (Reflect / Light Screen / Aurora Veil): no residual,
	// just count down and clear at zero. Side 0 then Side 1 for log
	// determinism.
	tickScreens(s, 0, &log)
	tickScreens(s, 1, &log)

	// Ability end-of-turn ticks (Speed Boost, Rain Dish, Ice Body, Dry Skin,
	// Solar Power). Side 0 then Side 1 — stable order matches weather.
	applyAbilityEndOfTurn(s, 0, &log)
	applyAbilityEndOfTurn(s, 1, &log)

	// Clear transient volatiles. Flinch is one-shot — if it wasn't consumed
	// this turn (e.g. because the flincher was slower, or the target fainted
	// before they could try to move), it must not leak into next turn.
	// MovedLast is per-turn scheduling state, also cleared here. Protect /
	// Endure are one-shot shields that expire at end of turn even if no
	// foe move tested them; ProtectCounter persists (the stall chain runs
	// across turns until broken by a non-stall action).
	for i := 0; i < 2; i++ {
		s.Active(i).Volatiles.Flinch = false
		s.Active(i).Volatiles.MovedLast = false
		s.Active(i).Volatiles.Protect = false
		s.Active(i).Volatiles.Endure = false
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
	w := effectiveWeather(s)
	sx, sy := effectiveSpeed(s.Active(x), w), effectiveSpeed(s.Active(y), w)
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

// executeMove runs one Pokémon's move. The phases are split into named helpers
// so future ability/item hooks can slot between them without rewriting the
// function: canAct → choosePP → announceMove → resolveAccuracy → dealDamage
// → applyDamageEffects, with applyResidual called separately at end-of-turn.
//
// Two extra gates wrap the normal flow:
//   - MustRecharge: the user spent its last move on Hyper Beam (or similar).
//     This turn is consumed recovering; no move resolves.
//   - Charging (two-turn): if the user isn't already charging, the first
//     hit of a two-turn move sets the Charging volatile and skips strike;
//     if it is, the strike resolves against the charged move regardless of
//     the submitted moveIdx.
func executeMove(dex *domain.Dex, s *BattleState, side, moveIdx int, rng *RNG, log *[]LogLine) {
	atk := s.Active(side)

	// Stall counter reset: any path through this function that does NOT
	// end in a successful Protect/Endure (which increments the counter
	// itself) means the user took a non-stall action — recharge, can't-
	// act, miss, damage, status, you name it — and the chain breaks.
	// applyProtectMove on success bumps the counter past counterBefore;
	// on a failed roll it explicitly zeroes it. So a defer that resets
	// only when the counter is unchanged covers every reset case without
	// special-casing each early return.
	counterBefore := atk.Volatiles.ProtectCounter
	defer func() {
		if atk.Volatiles.ProtectCounter == counterBefore {
			atk.Volatiles.ProtectCounter = 0
		}
	}()

	if atk.Volatiles.MustRecharge {
		atk.Volatiles.MustRecharge = false
		*log = append(*log, LogLine{Type: "status", Side: side,
			Text: fmt.Sprintf("%s must recharge!", atk.Name)})
		return
	}

	if !canAct(atk, side, rng, log) {
		return
	}

	var m domain.Move
	if ch := atk.Volatiles.Charging; ch != nil {
		// Strike turn of a two-turn move. PP was paid on the charge turn;
		// the moveIdx the controller submitted is ignored.
		atk.Volatiles.Charging = nil
		m = dex.Moves[atk.Moves[ch.MoveIdx].MoveID]
	} else {
		m = choosePP(dex, atk, moveIdx)
		if m.HasFlag("two-turn") && moveIdx >= 0 && moveIdx < len(atk.Moves) {
			atk.Volatiles.Charging = &ChargingState{MoveIdx: moveIdx}
			*log = append(*log, LogLine{Type: "move", Side: side,
				Text: fmt.Sprintf("%s began charging %s!", atk.Name, m.Name)})
			return
		}
	}

	announceMove(atk, side, m, log)

	// Psychic Terrain blocks priority moves aimed at a grounded foe. The
	// move announces but doesn't connect — Showdown emits a "protected"
	// flavour line; we lean on the generic terrain log type so the UI can
	// style it consistently with other terrain events.
	if m.Target != domain.TargetSelf {
		def := s.Active(1 - side)
		if terrainBlocksPriorityAgainst(s.Terrain, def, m.Priority) {
			*log = append(*log, LogLine{Type: "terrain", Side: side,
				Text: fmt.Sprintf("%s surrounds itself with Psychic Terrain!", def.Name)})
			return
		}
		// Protect / Detect: the foe's one-turn shield blocks every
		// foe-targeted move (damaging or status) unless the move carries
		// bypass-protect (Feint, Hyperspace Hole, ...). Returning here
		// suppresses the damage step, applyDamageEffects, contact riders,
		// and the m.Self / Primary / Secondary cascade — canonical
		// behavior for a fully absorbed attempt.
		if protectBlocksFoeMove(def, m) {
			*log = append(*log, LogLine{Type: "protect", Side: 1 - side,
				Text: fmt.Sprintf("%s protected itself!", def.Name)})
			return
		}
	}

	// OHKO immunity short-circuits fire before the accuracy roll: the
	// canonical log for Sheer Cold vs Ice or any OHKO vs Sturdy is
	// "doesn't affect" / "is unaffected", not "missed". (Normal type
	// immunity still happens inside computeDamage post-accuracy.)
	if m.OHKO != "" && resolveOHKOImmunity(s, side, m, log) {
		return
	}

	if !resolveAccuracy(s, side, m, rng, log) {
		applyMissOrEndEffects(s, side, m, log)
		return
	}

	if m.Category == domain.CatStatus {
		applyStatusMove(s, side, m, rng, log)
		applySelfSwitch(s, side, m, log)
		return
	}

	// Multi-strike moves (Bullet Seed, Bone Rush, Bonemerang, Triple Axel, ...)
	// loop the strike phase. The accuracy roll above gates the whole sequence;
	// each hit then rolls its own damage spread, crit, and secondary effects.
	// The loop stops early if either side faints (so a multi-hit move can't
	// continue against a 0-HP target, and Rough Skin can cut it short).
	planned := 1
	if m.IsMultihit() {
		planned = multihitCount(m, rng)
	}
	hits := 0
	for i := 0; i < planned; i++ {
		if s.Active(1-side).HP <= 0 || atk.HP <= 0 {
			break
		}
		dmg, ok := dealDamage(dex, s, side, m, rng, log)
		if !ok {
			// Type immunity also fires the post-move tail: a Ghost on the
			// receiving end of Explosion still takes no damage, but the
			// user still detonates (matches the canonical Gen-1 behavior).
			// For multi-hit moves immunity is type-based and deterministic,
			// so if the first strike is immune the rest would be too —
			// short-circuit the whole sequence.
			if i == 0 {
				applyMissOrEndEffects(s, side, m, log)
				return
			}
			break
		}
		applyDamageEffects(s, side, m, dmg, rng, log)
		hits++
	}
	if m.IsMultihit() && hits > 0 {
		*log = append(*log, LogLine{Type: "info", Side: side,
			Text: fmt.Sprintf("Hit %d time(s)!", hits)})
	}

	// Rapid Spin clears the user's side hazards on a successful hit. The
	// Speed +1 self-boost is already wired via the upstream secondary; only
	// the hazard sweep needs the hand-coded hook (Showdown encodes it in
	// JS). Triggered before faint resolution so a contact-faint counter
	// (Rough Skin) doesn't suppress the spin sweep — the move connected,
	// and that's what gates the clear.
	if hits > 0 && m.ID == "rapid-spin" {
		applyRapidSpin(s, side, log)
	}

	if m.HasFlag("recharge") {
		atk.Volatiles.MustRecharge = true
	}
	if m.HasFlag("selfdestruct") {
		applySelfDestruct(atk, side, log)
	}

	def := s.Active(1 - side)
	if def.HP <= 0 {
		faint(def, 1-side, log)
	}
	if atk.HP <= 0 {
		faint(atk, side, log)
	}

	// Damage-variant self-switch (U-turn, Volt Switch, Flip Turn) runs after
	// faint resolution so a contact-hit-reactive faint (Rocky Helmet, Rough
	// Skin) suppresses the switch the way it does in canon.
	applySelfSwitch(s, side, m, log)
}

// multihitCount returns the number of strikes for one use of a multi-hit
// move. Fixed counts (Bonemerang [2,2], Triple Axel [3,3]) return MinHits;
// the canonical [2,5] range follows the Gen-5+ weighted distribution
// (35% 2 hits, 35% 3 hits, 15% 4 hits, 15% 5 hits). Other ranges fall back
// to a uniform roll — no curated move uses one today, the branch exists for
// forward compatibility.
func multihitCount(m domain.Move, rng *RNG) int {
	if m.MinHits == m.MaxHits {
		return m.MinHits
	}
	if m.MinHits == 2 && m.MaxHits == 5 {
		roll := rng.IntN(100)
		switch {
		case roll < 35:
			return 2
		case roll < 70:
			return 3
		case roll < 85:
			return 4
		default:
			return 5
		}
	}
	return rng.Range(m.MinHits, m.MaxHits)
}

// applyMissOrEndEffects handles the post-resolution tail for cases where the
// damage step didn't fire (miss or type-immune): currently just selfdestruct,
// which detonates the user regardless of whether the move connected.
func applyMissOrEndEffects(s *BattleState, side int, m domain.Move, log *[]LogLine) {
	if !m.HasFlag("selfdestruct") {
		return
	}
	atk := s.Active(side)
	applySelfDestruct(atk, side, log)
	if atk.HP <= 0 {
		faint(atk, side, log)
	}
}

// applySelfDestruct drops the user to 0 HP. Used by Explosion / Self-Destruct
// after the move resolves (hit or miss); the caller is responsible for the
// subsequent faint() call.
func applySelfDestruct(atk *Pokemon, side int, log *[]LogLine) {
	if atk.HP <= 0 {
		return
	}
	atk.HP = 0
	*log = append(*log, LogLine{Type: "recoil", Side: side,
		Text: fmt.Sprintf("%s exploded!", atk.Name)})
}

// choosePP picks the move to use this turn and decrements its PP. If the
// requested slot is empty or out of range, Struggle is used instead.
func choosePP(dex *domain.Dex, atk *Pokemon, moveIdx int) domain.Move {
	if moveIdx >= 0 && moveIdx < len(atk.Moves) && atk.Moves[moveIdx].PP > 0 {
		atk.Moves[moveIdx].PP--
		return dex.Moves[atk.Moves[moveIdx].MoveID]
	}
	return struggleMove
}

// announceMove logs the "X used Y!" line that opens every move execution.
func announceMove(atk *Pokemon, side int, m domain.Move, log *[]LogLine) {
	*log = append(*log, LogLine{Type: "move", Side: side, Text: fmt.Sprintf("%s used %s!", atk.Name, m.Name)})
}

// resolveOHKOImmunity handles the two pre-accuracy immunity gates specific
// to OHKO moves and emits the canonical log line for each. Returns true if
// the move was absorbed (caller should stop processing the move).
//
//	(1) Sheer Cold's ohko="ice" makes Ice-types immune even though the type
//	    chart says Ice vs Ice is a normal 0.5× matchup.
//	(2) Sturdy (Gen 5+) blocks OHKO moves outright — distinct from the
//	    "leave at 1 HP" clamp the SurviveOHKO hook applies to normal hits.
//
// Normal type immunity (Ghost vs Horn Drill, Flying vs Fissure, Levitate
// vs Fissure) still runs inside computeDamage post-accuracy.
func resolveOHKOImmunity(s *BattleState, side int, m domain.Move, log *[]LogLine) bool {
	def := s.Active(1 - side)
	if m.OHKO != "any" && isType(def, domain.Type(m.OHKO)) {
		*log = append(*log, LogLine{Type: "immune", Side: side,
			Text: fmt.Sprintf("It doesn't affect %s...", def.Name)})
		return true
	}
	if a := abilityOf(def); a != nil && a.Kind == AbilitySturdy {
		*log = append(*log, LogLine{Type: "ability", Side: 1 - side,
			Text: fmt.Sprintf("%s is unaffected by the one-hit KO! (Sturdy)", def.Name)})
		return true
	}
	return false
}

// resolveAccuracy rolls the accuracy check and reports whether the move lands.
// Effective accuracy is move.Accuracy * accMult(clamp(atk.Acc - def.Eva, -6,
// +6)). The bypass-acc flag (Aerial Ace, Swift, Aura Sphere) skips the roll.
// Moves with Accuracy==0 are also unmissable (status-move convention).
func resolveAccuracy(s *BattleState, side int, m domain.Move, rng *RNG, log *[]LogLine) bool {
	if m.HasFlag("bypass-acc") || m.Accuracy == 0 {
		return true
	}
	atk := s.Active(side)
	def := s.Active(1 - side)
	// Soundproof: sound-flagged moves don't affect the holder at all. We
	// log "doesn't affect" rather than "missed" to match canon.
	if m.HasFlag("sound") {
		if a := abilityOf(def); a != nil && a.Kind == "soundproof" {
			*log = append(*log, LogLine{Type: "immune", Side: side,
				Text: fmt.Sprintf("It doesn't affect %s... (Soundproof)", def.Name)})
			return false
		}
	}
	// ignoreEvasion (Chip Away, Darkest Lariat): only positive evasion
	// boosts get zeroed; drops still help the attacker. Mirrors
	// canonical Showdown behavior — "ignore the buff, not the debuff".
	defEva := def.Stages.Eva
	if m.IgnoreEvasion && defEva > 0 {
		defEva = 0
	}
	combined := atk.Stages.Acc - defEva
	if combined > 6 {
		combined = 6
	}
	if combined < -6 {
		combined = -6
	}
	chance := int(float64(m.Accuracy) * accStageMultiplier(combined) * abilityAccuracyMult(atk))
	if chance > 100 {
		chance = 100
	}
	if chance < 100 && rng.IntN(100) >= chance {
		*log = append(*log, LogLine{Type: "miss", Side: side, Text: fmt.Sprintf("%s's attack missed!", atk.Name)})
		return false
	}
	return true
}

// dealDamage computes and applies damage for a non-status move, logging the
// damage/crit/effectiveness lines. Returns (dmg, true) on a normal hit, or
// (0, false) if the move was immune-blocked. A frozen target hit by a
// Fire-type damaging move thaws before damage applies; the move still lands.
func dealDamage(dex *domain.Dex, s *BattleState, side int, m domain.Move, rng *RNG, log *[]LogLine) (int, bool) {
	atk := s.Active(side)
	def := s.Active(1 - side)
	res := computeDamage(dex, atk, def, m, effectiveWeather(s), s.Terrain, &s.Sides[1-side].Conditions, rng)
	if res.Effectiveness == 0 {
		if res.AbilityImmune {
			// The ability's TypeMultOverride blocked it — let the ability's
			// own bonus hook describe what happened (Volt Absorb "absorbed
			// the electricity!", Flash Fire "warmed up!"). For pure
			// immunities (Levitate) the bonus hook is nil and we fall back
			// to the generic "doesn't affect" message.
			before := len(*log)
			abilityImmunityBonus(s, 1-side, m.Type, log)
			if len(*log) == before {
				*log = append(*log, LogLine{Type: "immune", Side: side,
					Text: fmt.Sprintf("It doesn't affect %s...", def.Name)})
			}
		} else {
			*log = append(*log, LogLine{Type: "immune", Side: side,
				Text: fmt.Sprintf("It doesn't affect %s...", def.Name)})
		}
		return 0, false
	}
	if def.Status == StatusFreeze && (m.Type == "fire" || m.ThawsTarget) {
		def.Status = StatusNone
		*log = append(*log, LogLine{Type: "status", Side: 1 - side,
			Text: fmt.Sprintf("%s was thawed by the heat!", def.Name)})
	}
	dmg := res.Damage
	// OHKO override: damage = full target HP. Sturdy was already filtered
	// upstream, so any OHKO that reaches here connects for a clean KO. The
	// formula's BP-0 crit/effectiveness lines are noise on a one-hit KO and
	// Showdown suppresses them — we follow suit and emit a single
	// "one-hit KO!" line instead.
	if m.OHKO != "" {
		dmg = def.HP
	}
	if dmg > def.HP {
		dmg = def.HP
	}
	// Substitute redirection: a doll on the defender soaks the hit before
	// def.HP changes. Sound and bypass-sub moves treat the doll as
	// transparent and damage the holder directly. Type effectiveness is
	// still computed against the holder (not the sub), so SE / resist
	// lines print either way. An OHKO move whose target has a sub simply
	// breaks the sub — it does NOT one-hit KO the holder, and the
	// "one-hit KO!" line is suppressed (Showdown's behavior).
	if hasSubstitute(def) && !bypassesSubstitute(m, atk) {
		if m.OHKO != "" {
			dmg = def.Volatiles.Substitute.HP
		}
		absorbed := applyDamageToSubstitute(def, 1-side, dmg, log)
		if res.Crit {
			*log = append(*log, LogLine{Type: "crit", Side: side, Text: "A critical hit!"})
		}
		if res.Effectiveness > 1 {
			*log = append(*log, LogLine{Type: "effective", Side: side, Text: "It's super effective!"})
		} else if res.Effectiveness < 1 {
			*log = append(*log, LogLine{Type: "resisted", Side: side, Text: "It's not very effective..."})
		}
		// Contact riders (Rough Skin, Static, Flame Body, Poison Point,
		// Effect Spore) still fire when a contact move hits the doll —
		// the attacker did touch the holder's body, the doll just stood
		// between them. Canonical.
		applyOnHit(s, 1-side, m, rng, log)
		return absorbed, true
	}
	// Endure: a lethal hit clamps to leave the target at 1 HP. Endure does
	// NOT block sub-routed damage (the doll already absorbed it) and does
	// NOT block status moves (those bypass dealDamage entirely). OHKO
	// moves are clamped too — Endure pays for that one-HP survival.
	enduredHit := false
	if def.Volatiles.Endure && dmg >= def.HP {
		dmg = def.HP - 1
		if dmg < 0 {
			dmg = 0
		}
		enduredHit = true
	}
	def.HP -= dmg
	*log = append(*log, LogLine{Type: "damage", Side: 1 - side, Text: fmt.Sprintf("%s took %d damage.", def.Name, dmg)})
	if enduredHit {
		*log = append(*log, LogLine{Type: "endure", Side: 1 - side,
			Text: fmt.Sprintf("%s endured the hit!", def.Name)})
	}
	if m.OHKO != "" && !enduredHit {
		*log = append(*log, LogLine{Type: "info", Side: side, Text: "It's a one-hit KO!"})
	} else if m.OHKO == "" {
		if res.Crit {
			*log = append(*log, LogLine{Type: "crit", Side: side, Text: "A critical hit!"})
		}
		if res.Effectiveness > 1 {
			*log = append(*log, LogLine{Type: "effective", Side: side, Text: "It's super effective!"})
		} else if res.Effectiveness < 1 {
			*log = append(*log, LogLine{Type: "resisted", Side: side, Text: "It's not very effective..."})
		}
		if res.Sturdy {
			*log = append(*log, LogLine{Type: "ability", Side: 1 - side,
				Text: fmt.Sprintf("%s hung on with Sturdy!", def.Name)})
		}
	}
	// On-hit hook for contact riders (Static, Flame Body, Poison Point,
	// Effect Spore). The hook itself checks the contact flag — keeping that
	// inside the ability avoids spreading move-flag inspection across
	// integration sites.
	applyOnHit(s, 1-side, m, rng, log)
	return dmg, true
}

// canAct applies pre-move status and volatile checks and reports whether the
// Pokémon moves. Order: flinch (one-shot, consumed) → confusion (may self-hit
// and preempt) → non-volatile status (freeze/sleep/para).
func canAct(p *Pokemon, side int, rng *RNG, log *[]LogLine) bool {
	if p.Volatiles.Flinch {
		p.Volatiles.Flinch = false
		*log = append(*log, LogLine{Type: "status", Side: side,
			Text: p.Name + " flinched and couldn't move!"})
		return false
	}
	if p.Volatiles.Confusion != nil {
		p.Volatiles.Confusion.Turns--
		if p.Volatiles.Confusion.Turns <= 0 {
			p.Volatiles.Confusion = nil
			*log = append(*log, LogLine{Type: "status", Side: side, Text: p.Name + " snapped out of confusion!"})
		} else {
			*log = append(*log, LogLine{Type: "status", Side: side, Text: p.Name + " is confused..."})
			if rng.Chance(33) {
				confusionSelfHit(p, side, rng, log)
				return false
			}
		}
	}
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

// confusionSelfHit deals self-damage when a confused Pokémon hits itself. The
// virtual move is 40-power typeless physical, using the user's own Atk/Def
// stages, no STAB, no crit, no type effectiveness. Burn does NOT halve this
// damage (Showdown convention).
func confusionSelfHit(p *Pokemon, side int, rng *RNG, log *[]LogLine) {
	a := float64(p.Stats.Atk) * stageMultiplier(p.Stages.Atk)
	d := float64(p.Stats.Def) * stageMultiplier(p.Stages.Def)
	base := (float64(2*Level)/5.0+2.0)*40.0*a/d/50.0 + 2.0
	randMult := float64(rng.Range(85, 100)) / 100.0
	dmg := int(base * randMult)
	if dmg < 1 {
		dmg = 1
	}
	if dmg > p.HP {
		dmg = p.HP
	}
	p.HP -= dmg
	*log = append(*log, LogLine{Type: "status", Side: side,
		Text: fmt.Sprintf("%s hurt itself in its confusion! (-%d)", p.Name, dmg)})
	if p.HP <= 0 {
		faint(p, side, log)
	}
}

// applyStatusMove handles the guaranteed primary effect of a status-category
// move. The primary applies to the move's declared target.
//
// Weather and terrain setters (Move.Weather / Move.Terrain != "") are
// dispatched here too: if the move names one, the new condition takes effect
// for its default-turn duration. A setter that names the *currently active*
// weather / terrain fails (matches Showdown — Rain Dance in rain is a
// wasted PP; same for Electric Terrain in electric terrain).
func applyStatusMove(s *BattleState, side int, m domain.Move, rng *RNG, log *[]LogLine) {
	if m.Weather != "" {
		applyWeatherSetter(s, side, WeatherKind(m.Weather), log)
		return
	}
	if m.Terrain != "" {
		applyTerrainSetter(s, side, TerrainKind(m.Terrain), log)
		return
	}
	if m.SideCondition != "" {
		if isHazardKind(m.SideCondition) {
			applyHazardSetter(s, side, HazardKind(m.SideCondition), log)
		} else {
			applyScreenSetter(s, side, ScreenKind(m.SideCondition), log)
		}
		return
	}
	// Defog: status move with no top-level effect block — Showdown encodes
	// its evasion drop and field-wipe in JS. Handled here by move ID rather
	// than via the SideCondition path (Defog's own sideCondition is "").
	if m.ID == "defog" {
		applyDefog(s, side, log)
		return
	}
	if m.Primary == nil {
		return
	}
	atk := s.Active(side)
	def := s.Active(1 - side)
	tgt, tside := def, 1-side
	if m.Target == domain.TargetSelf {
		tgt, tside = atk, side
	}
	if failed := applyEffectFields(m.Primary, m, atk, side, tgt, tside, 0, s, rng, log); failed {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
	}
}

// applyWeatherSetter spawns or refreshes the battle-level weather.
func applyWeatherSetter(s *BattleState, side int, kind WeatherKind, log *[]LogLine) {
	if s.Weather != nil && s.Weather.Kind == kind {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	s.Weather = &WeatherState{Kind: kind, TurnsLeft: defaultWeatherTurns}
	*log = append(*log, LogLine{Type: "weather", Side: -1, Text: weatherStartedText(kind)})
}

// applyTerrainSetter spawns or refreshes the battle-level terrain. Mirrors
// applyWeatherSetter — setting the same terrain that's already active fails.
func applyTerrainSetter(s *BattleState, side int, kind TerrainKind, log *[]LogLine) {
	if s.Terrain != nil && s.Terrain.Kind == kind {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	s.Terrain = &TerrainState{Kind: kind, TurnsLeft: defaultTerrainTurns}
	*log = append(*log, LogLine{Type: "terrain", Side: -1, Text: terrainStartedText(kind)})
}

// applyScreenSetter spawns a screen on the user's side. Re-setting an
// already-active screen fails (canonical Showdown — Reflect into Reflect
// is a wasted PP). Aurora Veil additionally fails unless hail/snow is
// active when used; once up, it persists even if the weather changes.
func applyScreenSetter(s *BattleState, side int, kind ScreenKind, log *[]LogLine) {
	if kind == ScreenAuroraVeil {
		w := effectiveWeather(s)
		if w == nil || w.Kind != WeatherSnow {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
			return
		}
	}
	slot := screenSlot(&s.Sides[side].Conditions, kind)
	if slot == nil {
		return
	}
	if *slot != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	*slot = &ScreenState{TurnsLeft: defaultScreenTurns}
	*log = append(*log, LogLine{Type: "screen", Side: side, Text: screenStartedText(kind)})
}

// applyDamageEffects runs the post-damage effects of a damaging move: the
// guaranteed Self block on the user, the guaranteed Primary on the foe (e.g.
// partial-trap moves' volatileStatus), and each rolled Secondary on the foe.
// Primary effects bypass Shield Dust and Sheer Force the way Showdown's
// top-level effects do; only entries in m.Secondaries are gated by those.
func applyDamageEffects(s *BattleState, side int, m domain.Move, dmg int, rng *RNG, log *[]LogLine) {
	atk := s.Active(side)
	def := s.Active(1 - side)
	if m.Self != nil {
		applyEffectFields(m.Self, m, atk, side, atk, side, dmg, s, rng, log)
	}
	if m.Primary != nil && !def.Fainted {
		applyEffectFields(m.Primary, m, atk, side, def, 1-side, dmg, s, rng, log)
	}
	if !abilityBlocksSecondaries(def) && !abilityBlocksOwnSecondaries(atk) {
		for i := range m.Secondaries {
			sec := &m.Secondaries[i]
			if rng.Chance(sec.Chance) {
				applyEffectFields(sec, m, atk, side, def, 1-side, dmg, s, rng, log)
			}
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
func applyEffectFields(e *domain.Effect, source domain.Move, atk *Pokemon, atkSide int, tgt *Pokemon, tgtSide int, dmgDealt int, s *BattleState, rng *RNG, log *[]LogLine) (statusFailed bool) {
	// Substitute on the target blocks foe-induced fields entirely: status
	// inflictions, volatile inflictions, boost drops, secondary riders. The
	// tgt==atk path (m.Self on damage moves, status moves with TargetSelf)
	// still applies because the doll doesn't sit between the user and its
	// own effects. Sound / bypass-sub moves treat the doll as transparent.
	// Returning true causes status-move dispatchers to log "But it failed!";
	// damage-move sites ignore the return value, so a sub-blocked secondary
	// is silent (canon).
	if tgt != atk && hasSubstitute(tgt) && !bypassesSubstitute(source, atk) {
		return true
	}
	if len(e.Boosts) > 0 {
		fromFoe := tgt != atk
		for _, stat := range orderedBoostStats(e.Boosts) {
			delta := e.Boosts[stat]
			if fromFoe && delta < 0 {
				applyStagesFromFoe(tgt, tgtSide, stat, delta, log)
			} else {
				applyStages(tgt, tgtSide, stat, delta, log)
			}
		}
	}
	if e.Status != "" {
		if !inflictStatus(tgt, tgtSide, StatusCond(e.Status), s, rng, log) {
			statusFailed = true
		}
	}
	if e.Volatile != "" {
		applyVolatile(tgt, tgtSide, e.Volatile, source, s, rng, log)
	}
	if e.Heal > 0 {
		amt := int(math.Round(float64(atk.MaxHP) * e.Heal))
		healPokemon(atk, atkSide, amt, log)
	}
	if e.Drain > 0 && dmgDealt > 0 {
		amt := int(math.Round(float64(dmgDealt) * e.Drain))
		healPokemon(atk, atkSide, amt, log)
	}
	if e.Recoil > 0 && dmgDealt > 0 && !abilityBlocksIndirectDamage(atk) {
		// Canonical Showdown rounds (round-half-up) rather than truncating
		// — truncation systematically under-reported recoil on every hit
		// where the fraction landed above .5 (issue #27). Magic Guard makes
		// the user immune to recoil.
		amt := int(math.Round(float64(dmgDealt) * e.Recoil))
		applySelfDamage(atk, atkSide, amt, log)
	}
	if e.Cure {
		cureStatus(atk, atkSide, log)
	}
	if e.Rest {
		doRest(atk, atkSide, log)
	}
	return statusFailed
}

// cureStatus clears the user's non-volatile status. No-op if none.
func cureStatus(p *Pokemon, side int, log *[]LogLine) {
	if p.Status == StatusNone {
		return
	}
	prev := p.Status
	p.Status = StatusNone
	p.SleepTurns = 0
	p.ToxicCounter = 0
	*log = append(*log, LogLine{Type: "status", Side: side,
		Text: fmt.Sprintf("%s was cured of its %s!", p.Name, prev)})
}

// doRest implements Rest: cure any status, fully heal, then force a 2-turn
// sleep. Unlike normal status infliction this bypasses the "already has a
// status" check, since Rest *replaces* any existing status with Sleep.
func doRest(p *Pokemon, side int, log *[]LogLine) {
	p.Status = StatusSleep
	p.SleepTurns = 2
	p.ToxicCounter = 0
	p.HP = p.MaxHP
	*log = append(*log, LogLine{Type: "status", Side: side,
		Text: fmt.Sprintf("%s went to sleep and became healthy!", p.Name)})
}

// applyVolatile inflicts a volatile condition on the target. No-op if the
// target is fainted or already has the volatile (with the exception of
// Flinch, which is overwritten by re-application). source carries the
// move that inflicted the volatile — used by partial-trap flavour text;
// other branches ignore it. s is the battle state, consulted for terrain
// guards (Misty refuses confusion on grounded targets).
func applyVolatile(p *Pokemon, side int, name string, source domain.Move, s *BattleState, rng *RNG, log *[]LogLine) {
	if p.Fainted {
		return
	}
	switch name {
	case "confusion":
		if p.Volatiles.Confusion != nil {
			return
		}
		if s != nil && terrainBlocksConfusion(s.Terrain, p) {
			return
		}
		p.Volatiles.Confusion = &ConfusionState{Turns: rng.Range(2, 5)}
		*log = append(*log, LogLine{Type: "status", Side: side,
			Text: fmt.Sprintf("%s became confused!", p.Name)})
	case "flinch":
		if abilityBlocksFlinch(p) {
			return
		}
		p.Volatiles.Flinch = true
		applyOnFlinched(p, side, log)
	case "partiallytrapped":
		if p.Volatiles.PartialTrap != nil {
			return
		}
		// Gen 5+: trap lasts 4 or 5 turns (equal probability without Grip
		// Claw — items aren't modeled). End-of-turn ticks the counter and
		// chips 1/8 max HP; switch-block is enforced in LegalActions.
		p.Volatiles.PartialTrap = &PartialTrapState{
			Turns:    4 + rng.IntN(2),
			MoveName: source.Name,
		}
		*log = append(*log, LogLine{Type: "status", Side: side,
			Text: fmt.Sprintf("%s was trapped by %s!", p.Name, source.Name)})
	case "substitute":
		applySubstituteSetup(p, side, log)
	case "protect":
		applyProtectMove(p, side, false, rng, log)
	case "endure":
		applyProtectMove(p, side, true, rng, log)
	}
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
// s is the battle state, consulted for terrain guards (Misty blocks all
// status, Electric blocks Sleep, both only on grounded targets).
func inflictStatus(p *Pokemon, side int, st StatusCond, s *BattleState, rng *RNG, log *[]LogLine) bool {
	if p.Status != StatusNone || p.Fainted {
		return false
	}
	if abilityBlocksStatus(p, st) {
		return false
	}
	if s != nil && terrainBlocksStatus(s.Terrain, p, st) {
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
	case StatusPoison, StatusToxic:
		if isType(p, "poison") || isType(p, "steel") {
			return false
		}
	}
	p.Status = st
	if st == StatusSleep {
		// Range is 2..4 (not 1..3) so a Pokémon inflicted on a turn it has
		// not yet moved doesn't immediately wake on the same turn's canAct.
		// Effective forced-skip turns are 1..3 either way (issue #24).
		p.SleepTurns = rng.Range(2, 4)
	}
	if st == StatusToxic {
		p.ToxicCounter = 1
	}
	*log = append(*log, LogLine{Type: "status", Side: side, Text: fmt.Sprintf("%s was %s!", p.Name, statusVerb(st))})
	return true
}

// applyStagesFromFoe is the foe-induced variant: it consults ability guards
// (Clear Body, Hyper Cutter, Big Pecks, Keen Eye) that block specific drops,
// and fires reactor abilities (Defiant, Competitive) when a drop lands.
// Self-induced stat changes (Swords Dance, Curse on self, etc.) bypass this
// and call applyStages directly.
func applyStagesFromFoe(p *Pokemon, side int, stat string, delta int, log *[]LogLine) {
	if abilityBlocksStatLowerByFoe(p, stat) {
		*log = append(*log, LogLine{Type: "ability", Side: side,
			Text: fmt.Sprintf("%s's ability prevented the stat drop!", p.Name)})
		return
	}
	applyStages(p, side, stat, delta, log)
	// Reactor hooks fire only when the drop actually occurred. applyStages
	// doesn't currently return a "did clamp" signal, so we recompute by
	// checking that the stage moved off its previous floor.
	applyOnStatLoweredByFoe(p, side, stat, log)
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
	*log = append(*log, LogLine{Type: "stat", Side: side,
		Text: fmt.Sprintf("%s's %s %s!", p.Name, statName(stat), stageVerb(delta))})
}

// stageVerb returns the canonical Pokémon log fragment for a stage change:
// ±1 "rose/fell", ±2 "rose sharply / harshly fell", ≥±3 "rose drastically /
// severely fell". The magnitude is based on the requested delta, not the
// clamped applied delta (canon convention).
func stageVerb(delta int) string {
	switch {
	case delta == 1:
		return "rose"
	case delta == 2:
		return "rose sharply"
	case delta >= 3:
		return "rose drastically"
	case delta == -1:
		return "fell"
	case delta == -2:
		return "harshly fell"
	case delta <= -3:
		return "severely fell"
	}
	return "changed"
}

// applyResidual applies end-of-turn residual damage: non-volatile status
// (burn / poison / toxic) and partial-trap chip. Toxic escalates each
// turn (1/16, 2/16, ... capped at 15/16) via p.ToxicCounter; the partial-
// trap counter ticks down here too and the volatile clears at zero.
func applyResidual(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	if p.Fainted {
		return
	}
	applyStatusResidual(p, side, log)
	if p.Fainted {
		return
	}
	applyPartialTrapResidual(p, side, log)
}

func applyStatusResidual(p *Pokemon, side int, log *[]LogLine) {
	var dmg int
	switch p.Status {
	case StatusBurn:
		dmg = p.MaxHP / 16
	case StatusPoison:
		dmg = p.MaxHP / 8
	case StatusToxic:
		dmg = p.MaxHP * p.ToxicCounter / 16
		if p.ToxicCounter < 15 {
			p.ToxicCounter++
		}
	default:
		return
	}
	if abilityBlocksIndirectDamage(p) {
		return // Magic Guard: skip the chip but the status still ticks for toxic.
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

// applyPartialTrapResidual chips 1/8 max HP and ticks the trap counter.
// The volatile clears when the counter reaches zero (or the holder faints).
// Magic Guard skips the chip but the counter still ticks — matching how
// burn/toxic still expire under Magic Guard.
func applyPartialTrapResidual(p *Pokemon, side int, log *[]LogLine) {
	pt := p.Volatiles.PartialTrap
	if pt == nil {
		return
	}
	if !abilityBlocksIndirectDamage(p) {
		dmg := p.MaxHP / 8
		if dmg < 1 {
			dmg = 1
		}
		if dmg > p.HP {
			dmg = p.HP
		}
		p.HP -= dmg
		*log = append(*log, LogLine{Type: "status", Side: side,
			Text: fmt.Sprintf("%s is hurt by %s! (-%d)", p.Name, pt.MoveName, dmg)})
		if p.HP <= 0 {
			faint(p, side, log)
			p.Volatiles.PartialTrap = nil
			return
		}
	}
	pt.Turns--
	if pt.Turns <= 0 {
		*log = append(*log, LogLine{Type: "status", Side: side,
			Text: fmt.Sprintf("%s was freed from %s!", p.Name, pt.MoveName)})
		p.Volatiles.PartialTrap = nil
	}
}

// applyWeatherResidual applies sandstorm chip damage to any active Pokémon
// that isn't Rock / Ground / Steel. Snow / Rain / Sun never chip; clear
// weather is a no-op. Faints fire here if the chip is lethal.
func applyWeatherResidual(s *BattleState, log *[]LogLine) {
	w := effectiveWeather(s) // honors Cloud Nine on either active
	if w == nil {
		return
	}
	for i := 0; i < 2; i++ {
		p := s.Active(i)
		if p.Fainted {
			continue
		}
		dmg := weatherResidual(w, p)
		if dmg == 0 {
			continue
		}
		if abilityBlocksIndirectDamage(p) {
			continue // Magic Guard: sandstorm chip is indirect damage.
		}
		if dmg > p.HP {
			dmg = p.HP
		}
		p.HP -= dmg
		*log = append(*log, LogLine{Type: "weather", Side: i,
			Text: fmt.Sprintf("%s is buffeted by the sandstorm! (-%d)", p.Name, dmg)})
		if p.HP <= 0 {
			faint(p, i, log)
		}
	}
}

// applyTerrainResidual fires Grassy Terrain's 1/16 max-HP end-of-turn heal
// on every grounded active. Other terrains don't have residual effects, so
// this is a no-op for them. Heals are not indirect damage — Magic Guard is
// irrelevant here.
func applyTerrainResidual(s *BattleState, log *[]LogLine) {
	t := s.Terrain
	if t == nil {
		return
	}
	for i := 0; i < 2; i++ {
		p := s.Active(i)
		if p.Fainted {
			continue
		}
		amt := terrainGrassyHeal(t, p)
		if amt == 0 {
			continue
		}
		if p.HP >= p.MaxHP {
			continue
		}
		before := p.HP
		p.HP += amt
		if p.HP > p.MaxHP {
			p.HP = p.MaxHP
		}
		*log = append(*log, LogLine{Type: "terrain", Side: i,
			Text: fmt.Sprintf("%s is healed by the Grassy Terrain! (+%d)", p.Name, p.HP-before)})
	}
}

// tickTerrain decrements the terrain's TurnsLeft. When it hits zero the
// terrain clears and a "<terrain> disappeared" line lands. Setters that
// name an already-active terrain are blocked at applyStatusMove, so a
// setter and a counter tick can't race here.
func tickTerrain(s *BattleState, log *[]LogLine) {
	if s.Terrain == nil {
		return
	}
	s.Terrain.TurnsLeft--
	if s.Terrain.TurnsLeft <= 0 {
		kind := s.Terrain.Kind
		s.Terrain = nil
		*log = append(*log, LogLine{Type: "terrain", Side: -1, Text: terrainClearedText(kind)})
		return
	}
	if txt := terrainContinuesText(s.Terrain.Kind); txt != "" {
		*log = append(*log, LogLine{Type: "terrain", Side: -1, Text: txt})
	}
}

// tickScreens decrements each active screen on side and clears any whose
// TurnsLeft hits zero. Screens have no per-turn flavour line — the log
// would be noisy on a Reflect+Light Screen team — only an expiry one.
func tickScreens(s *BattleState, side int, log *[]LogLine) {
	sc := &s.Sides[side].Conditions
	for _, kind := range []ScreenKind{ScreenReflect, ScreenLightScreen, ScreenAuroraVeil} {
		slot := screenSlot(sc, kind)
		if slot == nil || *slot == nil {
			continue
		}
		(*slot).TurnsLeft--
		if (*slot).TurnsLeft <= 0 {
			*slot = nil
			*log = append(*log, LogLine{Type: "screen", Side: side, Text: screenClearedText(kind)})
		}
	}
}

// tickWeather decrements the weather's TurnsLeft. When it hits zero the
// weather clears and a "<weather> stopped" line lands. Setters that name a
// weather already active are blocked at applyStatusMove, so a setter and a
// counter tick can't race here.
func tickWeather(s *BattleState, log *[]LogLine) {
	if s.Weather == nil {
		return
	}
	s.Weather.TurnsLeft--
	if s.Weather.TurnsLeft <= 0 {
		kind := s.Weather.Kind
		s.Weather = nil
		*log = append(*log, LogLine{Type: "weather", Side: -1, Text: weatherClearedText(kind)})
		return
	}
	if txt := weatherContinuesText(s.Weather.Kind); txt != "" {
		*log = append(*log, LogLine{Type: "weather", Side: -1, Text: txt})
	}
}

// doSwitch brings in a teammate. Stat stages and volatiles reset on both the
// outgoing and incoming Pokémon. The Sleep counter on the outgoing Pokémon
// resets too (Gen 5+ semantics — see docs/battle-state.md).
func doSwitch(s *BattleState, side, idx int, log *[]LogLine) {
	doSwitchWithCarry(s, side, idx, nil, log)
}

// batonCarry is the subset of the outgoing's state that Baton Pass copies
// onto the incoming. Stages always transfer; among volatiles, Confusion and
// Substitute do (Leech Seed / Encore aren't modeled yet). Flinch /
// MovedLast / Charging / MustRecharge are turn-scheduling state and never
// pass under canonical Showdown.
type batonCarry struct {
	Stages     Stages
	Confusion  *ConfusionState
	Substitute *SubstituteState
}

// doSwitchWithCarry performs a switch, optionally transferring the outgoing
// Pokémon's stat stages and select volatiles to the incoming (Baton Pass).
// carry == nil is the plain reset-on-switch path doSwitch uses.
func doSwitchWithCarry(s *BattleState, side, idx int, carry *batonCarry, log *[]LogLine) {
	sd := &s.Sides[side]
	if idx < 0 || idx >= len(sd.Team) || idx == sd.Active || sd.Team[idx].Fainted {
		return
	}
	out := &sd.Team[sd.Active]
	// Switch-out ability hook (Natural Cure, Regenerator) runs before the
	// outgoing's status / stages / volatiles are reset, so the hook can
	// observe what it's clearing.
	applyOnSwitchOut(out, side, log)
	out.Stages = Stages{}
	out.Volatiles = Volatiles{}
	if out.Status == StatusSleep {
		out.SleepTurns = 0
	}
	if !out.Fainted {
		*log = append(*log, LogLine{Type: "switch", Side: side, Text: fmt.Sprintf("%s, come back!", out.Name)})
	}
	sd.Active = idx
	in := &sd.Team[idx]
	in.Stages = Stages{}
	in.Volatiles = Volatiles{}
	if carry != nil {
		in.Stages = carry.Stages
		if carry.Confusion != nil {
			cc := *carry.Confusion
			in.Volatiles.Confusion = &cc
		}
		if carry.Substitute != nil {
			ss := *carry.Substitute
			in.Volatiles.Substitute = &ss
		}
	}
	*log = append(*log, LogLine{Type: "switch", Side: side, Text: fmt.Sprintf("Go, %s!", in.Name)})
	// Entry hazards fire before the ability switch-in hook: canon order is
	// Stealth Rock → Spikes → Toxic Spikes → Intimidate/Drizzle/etc. A
	// hazard KO short-circuits the rest (applyOnSwitchIn no-ops on a
	// fainted active).
	applyHazardsOnSwitchIn(s, side, log)
	applyOnSwitchIn(s, side, log)
}

// applySelfSwitch handles U-turn / Volt Switch / Flip Turn / Teleport (plain
// "normal") and Baton Pass ("copyvolatile"). Called at the tail of
// executeMove: if the user is alive and has a live bench member, the switch
// fires immediately so a same-turn slower foe sees (and can target) the
// replacement. The bench member is the lowest-indexed live teammate —
// deterministic across replays, matching how the AI / picker controllers
// already resolve faint replacements today.
func applySelfSwitch(s *BattleState, side int, m domain.Move, log *[]LogLine) {
	if m.SelfSwitch == "" {
		return
	}
	atk := s.Active(side)
	if atk.Fainted || atk.HP <= 0 {
		return
	}
	sd := &s.Sides[side]
	target := -1
	for i := range sd.Team {
		if i != sd.Active && !sd.Team[i].Fainted {
			target = i
			break
		}
	}
	if target == -1 {
		return
	}
	var carry *batonCarry
	if m.SelfSwitch == "copyvolatile" {
		c := batonCarry{Stages: atk.Stages}
		if atk.Volatiles.Confusion != nil {
			cc := *atk.Volatiles.Confusion
			c.Confusion = &cc
		}
		if atk.Volatiles.Substitute != nil {
			ss := *atk.Volatiles.Substitute
			c.Substitute = &ss
		}
		carry = &c
	}
	doSwitchWithCarry(s, side, target, carry, log)
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
	p.SleepTurns = 0
	p.ToxicCounter = 0
	p.Volatiles = Volatiles{}
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
