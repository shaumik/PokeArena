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
func applyPsychUp(s *BattleState, side int, log *[]LogLine) {
	user, foe := s.Active(side), s.Active(1-side)
	if foe == nil || foe.Fainted {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	user.Stages = foe.Stages
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
// The volatile clears when the *target* switches out, which it can't do while
// trapped — so in practice it lasts until one of them faints, matching canon.
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

// perishSongTurns is how many end-of-turn ticks a Pokémon gets before the
// song takes it. Canon counts 3, 2, 1, 0 with the faint on 0, which is four
// end-of-turn announcements including the one that kills.
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
func applyPerishSong(s *BattleState, side int, log *[]LogLine) {
	started := false
	for i := range s.Sides {
		p := s.Active(i)
		if p == nil || p.Fainted || p.Volatiles.PerishSong != nil {
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
// number each turn — the count is public information and the whole point is
// that both trainers can see the clock.
func tickPerishSong(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	if p == nil || p.Fainted || p.Volatiles.PerishSong == nil {
		return
	}
	p.Volatiles.PerishSong.TurnsLeft--
	left := p.Volatiles.PerishSong.TurnsLeft
	*log = append(*log, LogLine{
		Type: "perishsong", Side: side,
		Text: fmt.Sprintf("%s's perish count fell to %d!", p.Name, left),
	})
	if left <= 0 {
		// faint clears HP and the whole volatile bag, the countdown with it.
		faint(p, side, log)
	}
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

// statusDoublingMoves double their base power against a target carrying the
// right condition: Hex against any non-volatile status, Venoshock against
// poison specifically. Without this both were flat 65 BP and there was no
// reason to run either over an ordinary STAB.
var statusDoublingMoves = map[string]func(def *Pokemon) bool{
	"hex": func(def *Pokemon) bool {
		return def != nil && def.Status != StatusNone
	},
	"venoshock": func(def *Pokemon) bool {
		return def != nil && (def.Status == StatusPoison || def.Status == StatusToxic)
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
	if doubles, ok := statusDoublingMoves[m.ID]; ok && doubles(def) {
		m.Power *= 2
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
	if abilityBlocksSecondaries(def) || itemBlocksSecondaries(def) ||
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
