package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// callbackmoves.go implements the moves Showdown encodes as JS callbacks
// rather than as static data. The data-sync snapshot can only carry statics,
// so every move in here arrives in data/moves.json as a shell — a status move
// with no `primary` block, or a damaging move whose power is a lie — and
// resolves to nothing unless the engine hand-codes it.
//
// That failure mode is quiet in the worst way: applyStatusMove's
// `m.Primary == nil → return true` reads the shell as a clean success, so a
// Haze pays its PP, logs nothing unusual, and does not reset a single stage.
// Twelve moves were shipping like that (issue #130 §2). The pattern the file
// follows is the one Defog, Curse, Rest and Swallow already established:
// gate on the move ID at the dispatch site and lift the behavior here.
//
// Grouped by what breaks without them:
//
//	stat reset      Haze, Clear Smog, Psych Up   — see below
//	trapping        Mean Look, Block
//	team status     Heal Bell, Aromatherapy
//	countdown       Perish Song
//	PP drain        Spite
//	dynamic power   Hex, Venoshock, Weather Ball
//	sun scaling     Growth
//	rolled status   Tri Attack
//
// The stat-reset cluster was the load-bearing one. With no way to remove a
// stat boost anywhere in the engine, phazing (Roar / Whirlwind) was the only
// counterplay to setup in the entire game, which made setup sweepers
// materially stronger here than in canon. Both trainers in the match that
// produced #130 independently built a setup sweeper and independently
// carried a phazer, because there was no other answer.

// --- stat reset ---

// applyHaze zeroes every stat stage on both actives — the user's own included,
// which is what makes it a reset rather than a removal and why it is a
// defensive move rather than a free tempo gain.
func applyHaze(s *BattleState, side int, log *[]LogLine) {
	for i := range s.Sides {
		p := s.Active(i)
		if p != nil && !p.Fainted {
			p.Stages = Stages{}
		}
	}
	*log = append(*log, LogLine{
		Type: "haze", Side: side,
		Text: "All stat changes were eliminated!",
	})
}

// applyClearSmog is the post-damage hook for clear-smog: it resets the
// target's stages only, and only when the hit connected. Unlike Haze it
// leaves the user's own boosts alone.
func applyClearSmog(s *BattleState, side int, log *[]LogLine) {
	def := s.Active(1 - side)
	if def == nil || def.Fainted {
		return
	}
	if def.Stages == (Stages{}) {
		return // nothing to clear; stay quiet rather than log a no-op
	}
	def.Stages = Stages{}
	*log = append(*log, LogLine{
		Type: "haze", Side: 1 - side,
		Text: fmt.Sprintf("%s's stat changes were removed!", def.Name),
	})
}

// applyPsychUp copies the target's stat stages onto the user, replacing the
// user's own — including negatives, which is the risk that makes it a read
// rather than a free steal. The target keeps its boosts.
//
// The crit-ratio volatiles copy with the stages, and they copy the same way:
// what the user had before is gone whether or not the target has anything to
// replace it with. Canon spells that out as two loops over
// ['dragoncheer','focusenergy','gmaxchistrike','laserfocus'] — the first
// removes every one of them from the user, the second re-adds only the ones the
// target actually carries — and this engine models two of the four, so the
// straight assignment below is those two loops in one statement.
//
// Copying the stages alone (which is all this did) reads as a smaller bug than
// it is: the *removal* is the half that makes Psych Up a read. A Focus Energy
// user that Psychs Up a foe with none keeps its own +2 crit stages here and
// loses them in canon, which is a free effect on a move whose whole cost is
// that you get the target's numbers and not your own.
func applyPsychUp(s *BattleState, side int, log *[]LogLine) {
	user, foe := s.Active(side), s.Active(1-side)
	if foe == nil || foe.Fainted {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	user.Stages = foe.Stages
	user.Volatiles.FocusEnergy = foe.Volatiles.FocusEnergy
	user.Volatiles.LaserFocus = foe.Volatiles.LaserFocus
	*log = append(*log, LogLine{
		Type: "psychup", Side: side,
		Text: fmt.Sprintf("%s copied %s's stat changes!", user.Name, foe.Name),
	})
}

// --- trapping ---

// applyMeanLook roots the target until it faints or the trapper leaves. Move-
// based trapping didn't exist at all before this: only Arena Trap, Magnet
// Pull, the partial-trap moves and Ingrain held anything in place.
//
// The hold ends when the *trapper* leaves the field, not when the victim does —
// canon links the two volatiles (addVolatile's fourth argument) so
// clearVolatile's removeLinkedVolatiles deletes the victim's on switch-out and
// on faint alike. releaseTrapsSetBy is the engine's version.
//
// This comment used to say the opposite: that the volatile clears when the
// *target* switches out, "which it can't do while trapped — so in practice it
// lasts until one of them faints, matching canon". Every clause of that was
// wrong, and "matching canon" was the load-bearing one, because it is what
// would have stopped anyone checking.
//
// Ghost-types walk out regardless, the same immunity that beats Arena Trap.
func applyMeanLook(s *BattleState, side int, m domain.Move, log *[]LogLine) {
	def := s.Active(1 - side)
	if def == nil || def.Fainted || def.Volatiles.Trapped {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	if isType(def, "ghost") {
		*log = append(*log, LogLine{
			Type: "immune", Side: side,
			Text: fmt.Sprintf("It doesn't affect %s...", def.Name),
		})
		return
	}
	def.Volatiles.Trapped = true
	*log = append(*log, LogLine{
		Type: "trapped", Side: 1 - side,
		Text: fmt.Sprintf("%s can no longer escape!", def.Name),
	})
}

// --- team status ---

// applyTeamStatusCure is Heal Bell / Aromatherapy: every non-fainted member
// of the user's team, bench included, loses its non-volatile status. The
// sleep clock and toxic counter go with it.
//
// Upstream targets this at "allyTeam", a target shape this engine has no
// vocabulary for in singles, so the move arrives with no effect block at all.
func applyTeamStatusCure(s *BattleState, side int, m domain.Move, log *[]LogLine) {
	team := s.Sides[side].Team
	cured := false
	for i := range team {
		p := &team[i]
		if p.Fainted || p.Status == StatusNone {
			continue
		}
		clearStatus(p)
		cured = true
	}
	if !cured {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	*log = append(*log, LogLine{
		Type: "heal", Side: side,
		Text: fmt.Sprintf("A soothing aroma wafted through the area! (%s)", m.Name),
	})
}

// --- countdown ---

// perishSongTurns is the count the song starts at, and the number of turns of
// counterplay the victim gets. The end-of-turn on the turn it lands announces
// this number without spending it; the three that follow announce 2, 1 and 0,
// and the 0 is the one that kills. Four announcements, three turns to switch.
const perishSongTurns = 3

// PerishState is the countdown Perish Song leaves on a Pokémon. TurnsLeft
// ticks down at end of turn and the holder faints when it reaches zero.
// Cleared by switching out with the rest of the volatile bag, which is the
// move's entire counterplay: it is a switch-forcing move, not a kill.
type PerishState struct {
	TurnsLeft int `json:"turns_left"`
}

// applyPerishSong starts the count on *both* actives, the user included.
// A user with no bench has signed its own death warrant, which is canon and
// is what keeps the move from being a free win condition.
//
// Soundproof is checked here, per target, because that is what it is: an
// onTryHit on the holder, not a veto on the move. resolveAccuracy exempts the
// field-wide sound moves from its own Soundproof gate for exactly this reason —
// a Soundproof foe used to cancel the song for both sides, so the user's own
// count never started either.
func applyPerishSong(s *BattleState, side int, log *[]LogLine) {
	started := false
	for i := range s.Sides {
		p := s.Active(i)
		if p == nil || p.Fainted || p.Volatiles.PerishSong != nil {
			continue
		}
		// Soundproof does not deafen its holder to its *own* sound move —
		// upstream's onTryHit is gated on `target !== source`, so a Soundproof
		// Perish Song user starts its own count. That is the half of this the
		// upstream case is really about.
		if a := abilityOf(p); i != side && a != nil && a.Kind == "soundproof" &&
			!abilityBreaksMold(s.Active(side)) {
			revealAbility(p)
			*log = append(*log, LogLine{
				Type: "immune", Side: i,
				Text: fmt.Sprintf("It doesn't affect %s... (Soundproof)", p.Name),
			})
			continue
		}
		p.Volatiles.PerishSong = &PerishState{TurnsLeft: perishSongTurns}
		started = true
	}
	if !started {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	*log = append(*log, LogLine{
		Type: "perishsong", Side: side,
		Text: "All Pokémon that heard the song will faint in three turns!",
	})
}

// tickPerishSong runs one end-of-turn count on side's active. Announces the
// number each turn — the count is public information and the whole point of
// the move is that both trainers can see the clock.
//
// Announce first, then spend. The turn the song lands, its own end-of-turn
// announces the starting count and takes nothing off it, so the victim gets
// the full three turns canon promises. Decrementing first instead costs it a
// turn and makes the first announcement read one lower than the deadline
// actually is — which is worse than the missing turn, because the number on
// screen is what a player counts switches against.
func tickPerishSong(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	if p == nil || p.Fainted || p.Volatiles.PerishSong == nil {
		return
	}
	left := p.Volatiles.PerishSong.TurnsLeft
	*log = append(*log, LogLine{
		Type: "perishsong", Side: side,
		Text: fmt.Sprintf("%s's perish count fell to %d!", p.Name, left),
	})
	if left <= 0 {
		// faint clears HP and the whole volatile bag, the countdown with it.
		faint(p, side, log)
		return
	}
	p.Volatiles.PerishSong.TurnsLeft = left - 1
}

// --- PP drain ---

// spitePPLoss is how much PP Spite takes off the target's last move.
const spitePPLoss = 4

// applySpite drains PP from the move the target used most recently. Fails
// when the target hasn't moved yet, when that move is no longer in its slots,
// or when the slot is already empty — all three are canon, and all three are
// reachable, which is why Spite is a read rather than a filler move.
func applySpite(s *BattleState, side int, log *[]LogLine) {
	def := s.Active(1 - side)
	if def == nil || def.Fainted || def.Volatiles.LastMoveID == "" {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	for i := range def.Moves {
		slot := &def.Moves[i]
		if slot.MoveID != def.Volatiles.LastMoveID || slot.PP <= 0 {
			continue
		}
		lost := spitePPLoss
		if lost > slot.PP {
			lost = slot.PP
		}
		slot.PP -= lost
		*log = append(*log, LogLine{
			Type: "spite", Side: 1 - side,
			Text: fmt.Sprintf("%s's %s lost %d PP!", def.Name, def.Volatiles.LastMoveName, lost),
		})
		return
	}
	*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
}

// --- dynamic power ---

// statusDoublingMoves double their base power off a status condition. Hex and
// Venoshock read the *target* — any non-volatile status, and poison
// specifically. Facade reads the *user*: it is the move you run precisely
// because you intend to be burned or poisoned yourself, which is why the
// predicate takes both sides rather than just the defender.
//
// Facade excludes sleep and freeze, per canon — a Pokémon that cannot act
// gets nothing out of a power bonus. It also ignores burn's Attack halve
// (burnHalvesAttack in damage.go), so the burn is pure upside for it.
var statusDoublingMoves = map[string]func(atk, def *Pokemon) bool{
	// The three "hit them while they're down" moves. Each doubles against the
	// status it then cures, which is the joke: the doubled hit is the last one
	// that gets the bonus.
	"smelling-salts": func(_, def *Pokemon) bool { return def != nil && def.Status == StatusParalysis },
	"wake-up-slap":   func(_, def *Pokemon) bool { return def != nil && def.Status == StatusSleep },
	"hex": func(_, def *Pokemon) bool {
		return def != nil && def.Status != StatusNone
	},
	"venoshock": func(_, def *Pokemon) bool {
		return def != nil && (def.Status == StatusPoison || def.Status == StatusToxic)
	},
	"facade": func(atk, _ *Pokemon) bool {
		if atk == nil {
			return false
		}
		switch atk.Status {
		case StatusBurn, StatusParalysis, StatusPoison, StatusToxic:
			return true
		}
		return false
	},
}

// weatherBallType is the type Weather Ball takes on in each weather. Absent
// weather leaves it Normal at its base power.
var weatherBallType = map[WeatherKind]domain.Type{
	WeatherSun:       "fire",
	WeatherRain:      "water",
	WeatherSandstorm: "rock",
	WeatherSnow:      "ice",
}

// applyCallbackPower rewrites a move's power and type where Showdown's
// basePowerCallback would. Called from executeMove on the working copy of the
// move, beside the other dynamic-power adjustments, and returns the move so
// the caller reads as a transformation rather than a hidden mutation.
//
// Weather is read through weatherFor, so a Utility Umbrella holder throws a
// plain Normal-type ball out of the rain — the same rule every other
// weather-keyed effect follows.
func applyCallbackPower(s *BattleState, atk, def *Pokemon, m domain.Move) domain.Move {
	if doubles, ok := statusDoublingMoves[m.ID]; ok && doubles(atk, def) {
		m.Power *= 2
		return m
	}
	// The history-keyed doublings. Each reads one thing the engine records
	// about a turn that has already happened, which is why they were all
	// inert together: none of them is difficult, and none of them could work
	// until something wrote the state down.
	switch m.ID {
	case "assurance":
		// Doubles if the target has already been hurt this turn — the reason
		// Assurance is a partner move rather than a lead move.
		if def != nil && def.Volatiles.HurtThisTurn {
			m.Power *= 2
		}
		return m
	case "stomping-tantrum":
		// Doubles after the user's *previous* move failed. Strictly failed:
		// canon compares moveLastTurnResult against false, so a move that
		// merely accomplished nothing does not arm it.
		if atk.Volatiles.MoveLastTurnFailed {
			m.Power *= 2
		}
		return m
	case "lash-out":
		// Doubles if the user's own stats were dropped this turn — including
		// by its own move, and including a drop that happened before it acted.
		if atk.Volatiles.StatsLoweredThisTurn {
			m.Power *= 2
		}
		return m
	case "rage-fist":
		// +50 per hit taken across the whole battle, capped at 350. The counter
		// rides the Pokémon rather than its volatiles, so pivoting out does not
		// reset it.
		p := 50 + 50*atk.TimesAttacked
		if p > 350 {
			p = 350
		}
		m.Power = p
		return m
	case "trump-card":
		m.Power = trumpCardPower(atk, m.ID)
		return m
	case "fury-cutter":
		m.Power = furyCutterPower(atk, m.Power)
		return m
	case "rollout":
		m.Power = rolloutPower(atk, m.Power)
		return m
	}
	if m.ID == "weather-ball" {
		w := weatherFor(atk, effectiveWeather(s))
		if w == nil {
			return m
		}
		if t, ok := weatherBallType[w.Kind]; ok {
			m.Type = t
			m.Power *= 2
		}
	}
	return m
}

// --- sun scaling ---

// applyGrowthBoosts is Growth: +1 Attack and +1 Sp. Atk, doubled to +2 each
// under the sun. The base boosts ride the move's declarative Primary block;
// only the sun doubling is hand-coded, so this reports whether it took over.
//
// Read through weatherFor like Weather Ball, so a Utility Umbrella holder
// grows at the ordinary rate.
func applyGrowthBoosts(s *BattleState, side int, log *[]LogLine) bool {
	p := s.Active(side)
	w := weatherFor(p, effectiveWeather(s))
	if w == nil || w.Kind != WeatherSun {
		return false
	}
	applyStages(p, side, "attack", 2, log)
	applyStages(p, side, "spatk", 2, log)
	applyItemStatCheck(p, side, log)
	return true
}

// --- rolled status ---

// triAttackStatuses are the three conditions Tri Attack picks between on its
// 20% roll. Upstream ships the chance with no payload — Showdown decides which
// one in an onHit callback — so the move was rolling a 20% chance of nothing.
var triAttackStatuses = []StatusCond{StatusBurn, StatusFreeze, StatusParalysis}

// applyTriAttack is the post-damage hook for tri-attack: a 20% roll, and on
// success one of burn / freeze / paralysis chosen uniformly. The infliction
// goes through inflictStatusFrom so type immunities, Safeguard, abilities and
// the Sleep Clause all still get their say.
func applyTriAttack(s *BattleState, side int, rng *RNG, log *[]LogLine) {
	def := s.Active(1 - side)
	if def == nil || def.Fainted {
		return
	}
	// Shield Dust and Covert Cloak refuse added effects aimed at the holder,
	// and Sheer Force trades them away — the same gates the declarative
	// secondaries loop applies, checked here because this rider bypasses it.
	if abilityBlocksSecondaries(s, def) || itemBlocksSecondaries(def) ||
		abilityBlocksOwnSecondaries(s.Active(side)) {
		return
	}
	chance := int(20 * abilitySecondaryChanceMult(s.Active(side))) // Serene Grace doubles
	if chance > 100 {
		chance = 100
	}
	if !rng.Chance(chance) {
		return
	}
	inflictStatusFrom(def, 1-side, side, triAttackStatuses[rng.IntN(len(triAttackStatuses))], s, rng, log)
}

// moveTrapsSwitch reports whether a move-based trap is holding side's active in
// place — the partial traps (Bind, Wrap, Fire Spin, ...) and Mean Look / Block.
//
// Two refusals, and canon reaches both through Pokemon#tryTrap:
//
// A Ghost is never held. tryTrap opens with runImmunity('trapped'), and the
// type chart gives Ghost `trapped: 3`. Note what this does *not* do: a partial
// trap still lands on a Ghost and still chips it 1/8 a turn, because
// `partiallytrapped` is not a name in the type chart and addVolatile's own
// immunity check therefore passes. Only the *hold* is refused. Refusing the
// volatile instead would silently delete the chip damage, which is the wrong
// fix wearing the right shape.
//
// The trapper leaving ends the hold immediately: the partial trap's
// onTrapPokemon re-reads effectState.source live, and Battle#go re-runs the
// TrapPokemon event before every request. releaseTrapsSetBy handles the
// volatile itself; this covers the window where the trapper has fainted but not
// yet been replaced.
//
// The foe-is-the-trapper assumption: with releaseTrapsSetBy in place the engine
// holds the invariant "while a move trap is set, its setter is the foe's
// active", which is what lets this ask about s.Active(1-side) rather than store
// a source on the trap. Same shape abilityTrapsSwitch already uses. It has one
// false positive — with a mutual trap (A partial-traps B while B Mean Looks A),
// B leaving would clear A's hold too — which is unreachable in this dex and is
// the price of not carrying a trapper identity on two volatiles.
func moveTrapsSwitch(s *BattleState, side int) bool {
	act := s.Active(side)
	if act.Volatiles.PartialTrap == nil && !act.Volatiles.Trapped {
		return false
	}
	if isType(act, "ghost") {
		return false
	}
	foe := s.Active(1 - side)
	return foe != nil && !foe.Fainted
}

// releaseTrapsSetBy frees the victim on victimSide from any move-based trap,
// called when the Pokémon that set it leaves the field.
//
// Canon reaches the same place by two different routes, which is worth knowing
// because the engine's single function stands in for both. Mean Look and Block
// are *linked*: addVolatile's fourth argument cross-references trapper and
// victim, and clearVolatile → removeLinkedVolatiles deletes the victim's
// `trapped` — on switch-out and on faint alike. The partial traps are not
// linked at all; their condition re-reads effectState.source on every residual
// and deletes itself, silently and without chipping, once the source is gone.
//
// Modeled as an immediate release for both. The observable difference is one
// residual's worth of chip on the turn the trapper leaves, which canon skips
// anyway — its onResidual deletes the volatile *before* dealing the damage.
func releaseTrapsSetBy(s *BattleState, victimSide int) {
	v := s.Active(victimSide)
	if v == nil {
		return
	}
	v.Volatiles.PartialTrap = nil
	v.Volatiles.Trapped = false
}

// --- the second sweep: moves that narrated success and did nothing ---
//
// Everything below arrived the same way the twelve above did — a status move
// with no `primary` block, resolving through applyStatusMove's
// `m.Primary == nil → return true` as a clean success. The difference is how
// they were found. The first twelve were enumerated by hand in issue #130; the
// Showdown port later caught four more (the ability-setting moves, see
// abilitysetting.go), and only because it happened to ship cases for them.
//
// These twelve came out of an audit instead — TestNoCuratedMoveIsInert plays
// every curated move in a fixture built to give it something to act on, and
// fails on any move that neither changes the battle nor says anything beyond
// its own name. That test is the durable half of this work: the hand-written
// list was always going to be as complete as whoever wrote it.
//
// Two are implemented as refusals rather than effects, and both refuse for a
// reason that is about this dex rather than about the move — see
// applyMagneticFlux and applyNaturePower.

// swapStages exchanges a named set of stat stages between the user and the
// foe. Written as direct assignment rather than through applyStages because
// canon's setBoost is a write, not a boost: no Simple doubling, no Clear Body
// refusal, no "won't go higher" line. The two callers are Guard Swap (Def /
// Sp. Def) and Power Swap (Attack / Sp. Atk).
func swapStages(s *BattleState, side int, stats []string, label string, log *[]LogLine) {
	user, foe := s.Active(side), s.Active(1-side)
	if foe == nil || foe.Fainted {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	for _, stat := range stats {
		up, fp := stagePtr(user, stat), stagePtr(foe, stat)
		if up == nil || fp == nil {
			continue
		}
		*up, *fp = *fp, *up
	}
	*log = append(*log, LogLine{
		Type: "swapboost", Side: side,
		Text: fmt.Sprintf("%s switched its %s changes with %s!", user.Name, label, foe.Name),
	})
}

// rememberBaseStats takes the one-time snapshot that lets a stat rewrite be
// undone on switch-out. First writer wins, so a Pokémon hit by both Speed Swap
// and Power Split still reverts to the spread it was built with.
func rememberBaseStats(p *Pokemon) {
	if p.BaseStats == nil {
		snap := p.Stats
		p.BaseStats = &snap
	}
}

// applySpeedSwap exchanges the two actives' Speed *stats* — not their stages.
// The distinction is the move: swapping stages would be a much weaker effect
// that Speed Swap's users (slow, bulky Pokémon wanting a fast body's legs)
// would have no reason to run.
func applySpeedSwap(s *BattleState, side int, log *[]LogLine) {
	user, foe := s.Active(side), s.Active(1-side)
	if foe == nil || foe.Fainted {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	rememberBaseStats(user)
	rememberBaseStats(foe)
	user.Stats.Spe, foe.Stats.Spe = foe.Stats.Spe, user.Stats.Spe
	*log = append(*log, LogLine{
		Type: "swapstat", Side: side,
		Text: fmt.Sprintf("%s switched Speed with %s!", user.Name, foe.Name),
	})
}

// applyPowerSplit averages both actives' Attack and Sp. Atk. Floor division on
// each, matching canon's Math.floor — the pair does not conserve the total when
// the sum is odd, and reproducing that is cheaper than explaining a divergence.
func applyPowerSplit(s *BattleState, side int, log *[]LogLine) {
	user, foe := s.Active(side), s.Active(1-side)
	if foe == nil || foe.Fainted {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	rememberBaseStats(user)
	rememberBaseStats(foe)
	atk := (user.Stats.Atk + foe.Stats.Atk) / 2
	spa := (user.Stats.SpA + foe.Stats.SpA) / 2
	user.Stats.Atk, foe.Stats.Atk = atk, atk
	user.Stats.SpA, foe.Stats.SpA = spa, spa
	*log = append(*log, LogLine{
		Type: "activate", Side: side,
		Text: fmt.Sprintf("%s shared its power with %s!", user.Name, foe.Name),
	})
}

// applyHealPulse heals its target for half its maximum HP, rounded up. In
// singles the only legal target is the foe — canon's target is "any", which
// excludes the user — so this move restores the *opponent*. That reads like a
// bug and is not one: Heal Pulse is a doubles support move, and running it in
// singles is simply a mistake the rules allow.
func applyHealPulse(s *BattleState, side int, log *[]LogLine) {
	foe := s.Active(1 - side)
	if foe == nil || foe.Fainted {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	if foe.HP >= foe.MaxHP {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	healPokemon(foe, 1-side, (foe.MaxHP+1)/2, log)
}

// applyRefresh cures the user's burn, poison or paralysis. Sleep and freeze are
// explicitly not curable by it (canon's onHit returns false on ”, 'slp' and
// 'frz'), which is the whole reason the move is not simply a worse Rest.
func applyRefresh(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	switch p.Status {
	case StatusNone, StatusSleep, StatusFreeze:
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	cureStatus(p, side, log)
}

// applyVenomDrench drops a poisoned target's Attack, Sp. Atk and Speed by one
// stage each, and fails against anything that is not poisoned. The drops are
// foe-induced, so they route through applyStagesFromFoe and are answerable by
// Clear Body, Mist and the rest.
func applyVenomDrench(s *BattleState, side int, log *[]LogLine) {
	foe := s.Active(1 - side)
	if foe == nil || foe.Fainted ||
		(foe.Status != StatusPoison && foe.Status != StatusToxic) {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	for _, stat := range []string{"attack", "spatk", "speed"} {
		applyStagesFromFoe(foe, 1-side, stat, -1, s, log)
	}
	applyItemStatCheck(foe, 1-side, log)
}

// applyAcupressure raises one randomly chosen stat of the user by two stages,
// picking only from the stats that are not already at +6 — and failing when
// every one of them is. Accuracy and evasion are in the pool: canon iterates
// the whole boosts table, not just the five battle stats.
//
// In singles the target is always the user (canon's adjacentAllyOrSelf).
func applyAcupressure(s *BattleState, side int, rng *RNG, log *[]LogLine) {
	p := s.Active(side)
	var pool []string
	for _, stat := range []string{"attack", "defense", "spatk", "spdef", "speed", "accuracy", "evasion"} {
		if ptr := stagePtr(p, stat); ptr != nil && *ptr < 6 {
			pool = append(pool, stat)
		}
	}
	if len(pool) == 0 {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	applyStages(p, side, pool[rng.IntN(len(pool))], 2, log)
}

// applyRototiller raises the Attack and Sp. Atk of every grounded Grass-type on
// the field — both sides, since canon's target is "all". It fails only when
// there is nothing grounded and Grass *and* nothing airborne to announce an
// immunity for; an airborne Pokémon alone is enough to make the move "work",
// which is a genuine canon quirk rather than a simplification.
func applyRototiller(s *BattleState, side int, log *[]LogLine) {
	var targets []int
	airborne := false
	for i := 0; i < 2; i++ {
		p := s.Active(i)
		if p == nil || p.Fainted {
			continue
		}
		if !isGrounded(p, &s.PseudoWeather) {
			airborne = true
			*log = append(*log, LogLine{
				Type: "immune", Side: i,
				Text: fmt.Sprintf("It doesn't affect %s...", p.Name),
			})
			continue
		}
		if isType(p, "grass") {
			targets = append(targets, i)
		}
	}
	if len(targets) == 0 && !airborne {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	for _, i := range targets {
		applyStages(s.Active(i), i, "attack", 1, log)
		applyStages(s.Active(i), i, "spatk", 1, log)
	}
}

// applyCelebrate does nothing, and says so. That is the entire move upstream:
// its onTryHit adds an activation line and returns. It is in this file rather
// than left to the declarative fallthrough because "resolves to nothing
// silently" and "resolves to nothing loudly" are different behaviors, and only
// one of them is Celebrate.
func applyCelebrate(s *BattleState, side int, log *[]LogLine) {
	*log = append(*log, LogLine{
		Type: "activate", Side: side,
		Text: fmt.Sprintf("Congratulations, %s!", s.Sides[side].Trainer),
	})
}

// applyMagneticFlux raises the Defense and Sp. Def of every Pokémon on the
// user's side that has Plus or Minus. In singles the user's side is the user,
// so this is a self-boost gated on one of two abilities — and neither Plus nor
// Minus is on any species in this dex, so today the move always refuses.
//
// Written against the abilities anyway rather than hard-coded to fail: the
// refusal is a fact about the roster, not about the move, and a dex that gains
// a Plus holder should not also need this file edited.
func applyMagneticFlux(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	// The slug is read off the Pokémon rather than through abilityOf, which
	// would answer nil: Plus and Minus have no registry entry, because nothing
	// in the engine implements them and no species carries them. Suppression is
	// still honored — a Pokémon whose ability is switched off by Neutralizing
	// Gas does not have Plus for this purpose either.
	if abilitySuppressed(s, p) || (p.Ability != "plus" && p.Ability != "minus") {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	applyStages(p, side, "defense", 1, log)
	applyStages(p, side, "spdef", 1, log)
}

// naturePowerMove is the move Nature Power becomes. Tri Attack on bare ground;
// one move per terrain otherwise. All five ship in this dataset, which is the
// only reason the substitution in executeMove is worth making — a mapping to
// moves that were not curated would resolve to an empty move and be worse than
// the refusal it replaced.
//
// Falls back to Tri Attack if the terrain names a move the dex does not carry,
// so a terrain added later cannot silently turn Nature Power into nothing.
func naturePowerMove(dex *domain.Dex, s *BattleState) domain.Move {
	id := "tri-attack"
	if t := s.Terrain; t != nil {
		switch t.Kind {
		case TerrainElectric:
			id = "thunderbolt"
		case TerrainGrassy:
			id = "energy-ball"
		case TerrainMisty:
			id = "moonblast"
		case TerrainPsychic:
			id = "psychic"
		}
	}
	if m, ok := dex.Moves[id]; ok {
		return m
	}
	return dex.Moves["tri-attack"]
}

// FuryCutterState is the consecutive-use counter behind Fury Cutter's ramp.
// Multiplier is what the base power is scaled by (1, 2, 4 — canon caps the
// doubling at four); TurnsLeft is canon's two-turn duration, which is what
// makes the chain break on any turn the move does not connect.
type FuryCutterState struct {
	Multiplier int `json:"multiplier"`
	TurnsLeft  int `json:"turns_left"`
}

// trumpCardPower is Trump Card's inverted PP curve: the rarer the move, the
// harder it hits. Read off the slot the user is about to spend, and read
// *before* the PP is deducted — canon's basePowerCallback runs inside
// getDamage, which is well after deductPP, so the figure it sees is the PP
// remaining *after* this use. The engine calls applyCallbackPower after
// choosePP for the same reason, and this function therefore wants the
// post-payment number exactly as it finds it.
//
// A move not in the user's list (there is no such caller today, but Metronome
// or Sleep Talk would create one) falls to the 40 floor, matching canon's
// missing-moveSlot branch.
func trumpCardPower(p *Pokemon, moveID string) int {
	for i := range p.Moves {
		if p.Moves[i].MoveID != moveID {
			continue
		}
		switch p.Moves[i].PP {
		case 0:
			return 200
		case 1:
			return 80
		case 2:
			return 60
		case 3:
			return 50
		default:
			return 40
		}
	}
	return 40
}

// furyCutterPower applies the consecutive-use multiplier and arms the next
// one. Canon caps the multiplier at 4 (160 base power from a 40 BP move) and
// gives the volatile a two-turn duration, so a single turn without a
// connecting Fury Cutter drops the chain back to the start.
//
// The tick happens here, at power calculation, because that is where canon
// puts it — basePowerCallback both reads the multiplier and adds the volatile.
// A move that goes on to miss still counted, and the expiry below is what
// takes the boost away again.
func furyCutterPower(p *Pokemon, base int) int {
	fc := p.Volatiles.FuryCutter
	if fc == nil {
		p.Volatiles.FuryCutter = &FuryCutterState{Multiplier: 1, TurnsLeft: 2}
		return base
	}
	if fc.Multiplier < 4 {
		fc.Multiplier *= 2
	}
	fc.TurnsLeft = 2
	power := base * fc.Multiplier
	if power > 160 {
		power = 160
	}
	return power
}

// tickFuryCutter expires the chain. Called once per end of turn on each active:
// the counter is refreshed to two turns by every use, so it survives the tick
// that follows its own turn and dies on the next one.
func tickFuryCutter(p *Pokemon) {
	fc := p.Volatiles.FuryCutter
	if fc == nil {
		return
	}
	fc.TurnsLeft--
	if fc.TurnsLeft <= 0 {
		p.Volatiles.FuryCutter = nil
	}
}

// applyBurningJealousy burns a target whose stats were raised this turn, and
// does nothing to one that merely arrived boosted. That distinction is the
// move: it answers setup as it happens rather than punishing a Pokémon for
// having set up at some point in the past.
//
// targetWasRaised is read before the strike rather than here — see the snapshot
// in executeMove for why a Weakness Policy triggered by this very hit does not
// count.
//
// Modeled as a secondary rather than as a plain on-hit effect, because that is
// what it is upstream — so Shield Dust, Covert Cloak and Sheer Force all refuse
// it, and inflictStatusFrom applies the usual immunities (a Fire-type target
// cannot be burned by it any more than by anything else).
func applyBurningJealousy(s *BattleState, side int, targetWasRaised bool, rng *RNG, log *[]LogLine) {
	def := s.Active(1 - side)
	if isDown(def) || !targetWasRaised {
		return
	}
	if abilityBlocksSecondaries(s, def) || itemBlocksSecondaries(def) ||
		abilityBlocksOwnSecondaries(s.Active(side)) {
		return
	}
	inflictStatusFrom(def, 1-side, side, StatusBurn, s, rng, log)
}

// RolloutState is the consecutive-use counter behind Rollout's ramp. HitCount
// is how many times the chain has already connected, so the power of the next
// use is 30 × 2^HitCount.
type RolloutState struct {
	HitCount int `json:"hit_count"`
}

// rolloutPower doubles Rollout's base power for each consecutive connecting
// use, doubles it again for a user that has curled up, and starts the chain
// over after the fifth. Canon expresses the cap by simply not refreshing the
// volatile's duration on the fifth hit, so the sixth use finds nothing and
// begins again at 30.
//
// **The choice-lock is deliberately not modeled.** Canon's rollout condition
// carries onLockMove, so a player mid-chain cannot choose anything else; here
// they can, and choosing something else breaks the chain (see tickRollout).
// The power ramp — which is the whole of what makes Rollout interesting, and
// all either ported case measures — is faithful; the commitment that is meant
// to pay for it is not. That is a real divergence and it favors the Rollout
// user, so it is written down rather than left to be discovered: it sits in
// the same category as the rampage degradations listed in lockedmove.go.
//
// Upstream's own Defense Curl case curls *first* and rolls second, precisely
// because a locked user could not do it the other way round. The port has to
// measure damage rather than read a BasePower hook, so it rolls, curls, and
// rolls again — a sequence that only exists because this engine allows it.
func rolloutPower(p *Pokemon, base int) int {
	r := p.Volatiles.Rollout
	if r == nil {
		r = &RolloutState{}
		p.Volatiles.Rollout = r
	}
	power := base
	for i := 0; i < r.HitCount; i++ {
		power *= 2
	}
	if p.Volatiles.DefenseCurl {
		power *= 2
	}
	r.HitCount++
	if r.HitCount >= rolloutChainLength {
		// The fifth use is the last of the chain; the next one starts over.
		p.Volatiles.Rollout = nil
	}
	return power
}

// rolloutChainLength is how many consecutive uses the ramp runs for before
// resetting to base power.
const rolloutChainLength = 5

// tickRollout breaks the chain at the end of any turn the user did not connect
// with Rollout — a different move, a miss, an immunity, a refusal. Canon
// reaches the same place by only refreshing the volatile's duration from
// inside Rollout's own base-power callback, so any turn that does not run it
// lets the volatile expire.
func tickRollout(p *Pokemon) {
	if p.Volatiles.Rollout == nil {
		return
	}
	if p.Volatiles.LastMoveID != "rollout" || p.Volatiles.MoveThisTurnFailed {
		p.Volatiles.Rollout = nil
	}
}

// --- crash damage ---

// crashMoveIDs are the moves that hurt their user when they fail to connect
// (upstream's hasCrashDamage plus the matching onMoveFail). Both are in this
// dataset; neither carries the fact in data/moves.json, because it lives in a
// JS callback.
var crashMoveIDs = map[string]bool{
	"jump-kick":      true,
	"high-jump-kick": true,
}

func hasCrashDamage(m domain.Move) bool { return crashMoveIDs[m.ID] }

// applyCrashDamage charges the user half its maximum HP for a jump kick that
// did not connect. Canon is `this.damage(source.baseMaxhp / 2, ...)`, floored
// and clamped to a minimum of 1 — note there is no rounding, unlike Struggle's
// recoil, which canon does put through Math.round.
//
// The damage is attributed to a *condition* named after the move rather than to
// the move's recoil field, and that is exactly why Rock Head does not cover it:
// the ability's onDamage only intercepts `effect.id === 'recoil'`. Magic Guard
// does cover it, because it refuses every damage whose effect is not a Move.
// Gating on abilityBlocksIndirectDamage alone and never on abilityBlocksRecoil
// reproduces that split for free.
func applyCrashDamage(atk *Pokemon, side int, log *[]LogLine) {
	if atk == nil || atk.HP <= 0 || abilityBlocksIndirectDamage(atk) {
		return
	}
	amt := atk.MaxHP / 2
	if amt < 1 {
		amt = 1
	}
	applySelfDamage(atk, side, amt, log)
	if atk.HP <= 0 {
		faint(atk, side, log)
	}
}

// --- status cures on a damaging hit ---

// cureStatusIf clears the target's non-volatile status, but only when it is the
// one the move is keyed on. Smelling Salts (paralysis), Sparkling Aria (burn)
// and Wake-Up Slap (sleep) are the three; all are engine branches rather than
// data because domain.Effect.Cure clears the *user's* status, not the target's.
func cureStatusIf(p *Pokemon, side int, want StatusCond, log *[]LogLine) {
	if p == nil || p.HP <= 0 || p.Status != want {
		return
	}
	cureStatus(p, side, log)
}

// sharesAType reports whether the two Pokémon have a type in common —
// Synchronoise's onTryImmunity, and nothing else reads it.
//
// Reads Type1/Type2 directly rather than going through roostTypes: that helper
// is deliberately damage-path-only, and using it here would make a roosting
// Flying-type untargetable by a Flying Synchronoise, which is not what canon's
// getTypes() does for this check.
func sharesAType(a, b *Pokemon) bool {
	if a == nil || b == nil {
		return false
	}
	for _, x := range []domain.Type{a.Type1, a.Type2} {
		if x == "" {
			continue
		}
		if x == b.Type1 || x == b.Type2 {
			return true
		}
	}
	return false
}
