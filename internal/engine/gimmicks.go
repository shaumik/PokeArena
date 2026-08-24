package engine

import (
	"fmt"
	"math"

	"pokearena/internal/domain"
	"pokearena/internal/specs"
)

// gimmicks.go owns eight one-offs that don't cluster with any of the
// other mechanic files. All are volatiles; most are timer-shaped:
//
//   Magnet Rise   — 5-turn Ground immunity on user
//   Smack Down    — physical damage + grounds target (cancels Magnet
//                   Rise, Telekinesis, Levitate, Flying immunity to
//                   Ground; persists until switch)
//   Telekinesis   — 3-turn auto-hit vs target + Ground immunity
//   Snatch        — +4 priority, steals foe's next self-targeted
//                   status move
//   Magic Coat    — +4 priority, blocks foe's next foe-targeted
//                   status move (bounceback degraded to "blocked")
//   Stockpile     — counter 1..3, +1 Def / +1 SpD per use
//   Grudge        — register-only (PP drain not modeled)
//   Gastro Acid   — register-only (ability suppression not threaded)
//
// computeDamage's Ground-type immunity gate (Magnet Rise gives
// immunity; Smack Down lifts Flying / Levitate) is the only damage-
// path hook this file adds. Snatch / Magic Coat gate inside
// applyStatusMove via the helpers below. Stockpile's +1 / +1 ride
// through applyStages directly. tickGimmicks runs the end-of-turn
// decrement for Magnet Rise and Telekinesis; Snatch / Magic Coat
// clear in the transient sweep alongside Flinch / Protect.

func init() {
	specs.RegisterVolatile("magnetrise")
	specs.RegisterVolatile("smackdown")
	specs.RegisterVolatile("telekinesis")
	specs.RegisterVolatile("snatch")
	specs.RegisterVolatile("magiccoat")
	specs.RegisterVolatile("stockpile")
	specs.RegisterVolatile("grudge")
	specs.RegisterVolatile("gastroacid")
	registerVolatile("magnetrise", applyMagnetRiseVolatile)
	registerVolatile("smackdown", applySmackDownVolatile)
	registerVolatile("telekinesis", applyTelekinesisVolatile)
	registerVolatile("snatch", applySnatchVolatile)
	registerVolatile("magiccoat", applyMagicCoatVolatile)
	registerVolatile("stockpile", applyStockpileVolatile)
	registerVolatile("grudge", applyGrudgeVolatile)
	registerVolatile("gastroacid", applyGastroAcidVolatile)
}

// MagnetRiseState counts down 5 end-of-turn ticks of Ground immunity.
type MagnetRiseState struct {
	TurnsLeft int `json:"turns_left"`
}

// TelekinesisState counts down 3 end-of-turn ticks of auto-hit-vs-
// target + Ground immunity.
type TelekinesisState struct {
	TurnsLeft int `json:"turns_left"`
}

// StockpileState counts stacks 1..3. Each stack riders +1 Def /
// +1 SpD on apply via applyStages — the live stack count is
// surfaced so future Swallow / Spit Up moves can read it.
//
// Def / SpD are the separate tallies of how many of those boosts actually
// landed, which is not always one per stack: a stat already at +6 (or one
// something refused) never moved, and Spit Up / Swallow must not hand back a
// stage the stockpile never took. Canon carries exactly this pair —
// `this.effectState.def` / `.spd`, decremented only when `target.boosts.def`
// is observed to have changed — and its onEnd restores those numbers rather
// than the layer count, to the point of emitting a hint when the two disagree.
//
// Both are counts of landed boosts, so 0 <= Def, SpD <= Count. There is no
// legacy fallback: a state written before these fields existed reads 0/0 and
// gives nothing back. That is a real (if narrow) divergence rather than a safe
// default — the pre-change engine always reversed the layer count — and it is
// left as-is because this engine already treats a behavior change as
// invalidating recorded battles, which is what re-recording
// testdata/fullgame-golden.json on every such change is for. Anything that
// hand-builds a StockpileState has to fill the tallies in, and the tests in
// stockpile_consume_test.go do.
type StockpileState struct {
	Count int `json:"count"`
	Def   int `json:"def_boosts,omitempty"`
	SpD   int `json:"spd_boosts,omitempty"`
}

const (
	defaultMagnetRiseTurns  = 5
	defaultTelekinesisTurns = 3
	maxStockpileStacks      = 3
)

func applyMagnetRiseVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.MagnetRise != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	if p.Volatiles.SmackDown {
		// Already grounded — Magnet Rise can't lift off canon.
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.MagnetRise = &MagnetRiseState{TurnsLeft: defaultMagnetRiseTurns}
	*log = append(*log, LogLine{
		Type: "magnetrise", Side: side,
		Text: fmt.Sprintf("%s levitated with electromagnetism!", p.Name),
	})
}

// applySmackDownVolatile is the damage-primary handler for Smack Down.
// Sets the persistent grounding flag, cancels any active Magnet Rise /
// Telekinesis on the target, and knocks a target that is mid-Fly or mid-Bounce
// out of the move it was about to land.
//
// Canon's condition is a list of four things the volatile can be *taking away*
// (Flying typing or Levitate, a Fly / Bounce charge, Magnet Rise, Telekinesis)
// and it refuses to apply at all — `if (!applies) return false` — when none of
// them is there. That refusal is the half this engine was missing: Smack Down
// used to stick its flag on anything it hit, which is a permanent grounding
// handed out for free to a Pokemon that was already standing on the ground,
// and which then blocks the Magnet Rise it should not have blocked. The three
// unconditional grounders (Gravity, Ingrain, an Iron Ball) are canon's
// override in the other direction: they are already holding the target down,
// so there is nothing for the move to do.
//
// groundedness (terrain.go) answers the whole of that first list in one call,
// which is the point of it being one predicate — the alternative is a second,
// slightly different notion of "in the air" living here, which is the bug this
// engine already had once.
//
// One divergence to know about, and it is not fixable from this file: canon
// pairs the charge cancellation with `this.queue.cancelMove(pokemon)`, so a
// target smacked out of Fly on its *strike* turn loses the action entirely.
// There is no queued-action cancellation in this engine — the move loop reads
// the submitted action, and with the charge gone LegalActions no longer pins
// it — so that target instead starts the two-turn move over. The turn is lost
// either way; which is spent differs.
func applySmackDownVolatile(p *Pokemon, side int, _ domain.Move, s *BattleState, _ *RNG, log *[]LogLine) {
	fell := LogLine{
		Type: "smackdown", Side: side,
		Text: fmt.Sprintf("%s fell straight down!", p.Name),
	}
	// Canon's onRestart. The flag is already set and there is nothing to
	// re-ground, but a target that took off again since is still brought down.
	if p.Volatiles.SmackDown {
		if cancelAirborneCharge(p) {
			*log = append(*log, fell)
		}
		return
	}

	var pw *PseudoWeather
	if s != nil {
		pw = &s.PseudoWeather
	}
	applies := false
	switch groundedness(p, pw, false) {
	case airborne:
		applies = true
	case airborneByAbility:
		// Levitate. Canon asks `pokemon.hasAbility('levitate')`, which reads
		// false while a mold breaker is resolving — so a mold-breaking Smack
		// Down grounds nothing, because as far as the move can see nothing was
		// holding the target up. Same question isGroundedOnEntry asks.
		applies = !abilitySuppressed(s, p)
	}
	// Each of these three sets `applies` back to true in canon even when one of
	// the unconditional grounders above said no, so the order is kept: an Iron
	// Ball holder mid-Fly is still knocked out of the Fly.
	if cancelAirborneCharge(p) {
		applies = true
	}
	if p.Volatiles.MagnetRise != nil {
		applies = true
		p.Volatiles.MagnetRise = nil
	}
	if p.Volatiles.Telekinesis != nil {
		applies = true
		p.Volatiles.Telekinesis = nil
	}
	if !applies {
		return
	}
	p.Volatiles.SmackDown = true
	*log = append(*log, fell)
}

// airborneChargeMoves are the two-turn moves that spend the charge turn in the
// air. Smack Down cancels exactly these upstream — Dig and Dive are the other
// direction and Solar Beam / Skull Bash / Razor Wind charge with their feet on
// the ground — so it is the only distinction between two-turn moves this
// engine needs to draw.
var airborneChargeMoves = map[string]bool{"fly": true, "bounce": true}

// cancelAirborneCharge drops the target out of a mid-air two-turn move,
// reporting whether there was one to drop. The Charging volatile is this
// engine's whole representation of the charge turn (there is no separate
// semi-invulnerability), so clearing it is canon's removeVolatile('fly') and
// removeVolatile('twoturnmove') at once.
func cancelAirborneCharge(p *Pokemon) bool {
	ch := p.Volatiles.Charging
	if ch == nil || ch.MoveIdx < 0 || ch.MoveIdx >= len(p.Moves) {
		return false
	}
	if !airborneChargeMoves[p.Moves[ch.MoveIdx].MoveID] {
		return false
	}
	p.Volatiles.Charging = nil
	return true
}

func applyTelekinesisVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Telekinesis != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	if p.Volatiles.SmackDown {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.Telekinesis = &TelekinesisState{TurnsLeft: defaultTelekinesisTurns}
	*log = append(*log, LogLine{
		Type: "telekinesis", Side: side,
		Text: fmt.Sprintf("%s was hurled into the air!", p.Name),
	})
}

// applySnatchVolatile arms the steal flag. Cleared either when a
// foe's self-target status move is intercepted or at end-of-turn.
func applySnatchVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	p.Volatiles.Snatch = true
	*log = append(*log, LogLine{
		Type: "snatch", Side: side,
		Text: fmt.Sprintf("%s waited for a target to make a move!", p.Name),
	})
}

// applyMagicCoatVolatile arms the block flag. Cleared either when a
// foe's foe-target status move is bounced or at end-of-turn.
func applyMagicCoatVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	p.Volatiles.MagicCoat = true
	*log = append(*log, LogLine{
		Type: "magiccoat", Side: side,
		Text: fmt.Sprintf("%s shrouded itself with Magic Coat!", p.Name),
	})
}

// applyStockpileVolatile stacks 1..3. Each stack rides +1 Def /
// +1 SpD via applyStages, and records which of the two actually
// moved — see StockpileState for why the tally is kept per stat
// rather than inferred from the stack count.
func applyStockpileVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Stockpile == nil {
		p.Volatiles.Stockpile = &StockpileState{Count: 0}
	}
	st := p.Volatiles.Stockpile
	if st.Count >= maxStockpileStacks {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	st.Count++
	*log = append(*log, LogLine{
		Type: "stockpile", Side: side,
		Text: fmt.Sprintf("%s stockpiled %d!", p.Name, st.Count),
	})
	if applyStagesTracked(p, side, "defense", 1, log) {
		st.Def++
	}
	if applyStagesTracked(p, side, "spdef", 1, log) {
		st.SpD++
	}
}

// applyStagesTracked is applyStages plus the answer to "did that move the
// stat?", which applyStages itself only says in the log ("won't go higher!").
// Canon asks the same question the same way — it reads target.boosts before
// and after its own boost call rather than having boost() report back — and
// Stockpile is the one mechanic here that has to undo precisely what it did.
func applyStagesTracked(p *Pokemon, side int, stat string, delta int, log *[]LogLine) bool {
	ptr := stagePtr(p, stat)
	if ptr == nil {
		return false
	}
	before := *ptr
	applyStages(p, side, stat, delta, log)
	return *ptr != before
}

// hpRatioPowerMoves scale base power with the user's remaining HP
// (floor(150 × curHP ÷ maxHP), min 1). Only water-spout is in the current
// Gen-1-scoped learnset; eruption and dragon-energy are listed so the mechanic
// is correct the moment they're ever synced in.
var hpRatioPowerMoves = map[string]bool{
	"water-spout":   true,
	"eruption":      true,
	"dragon-energy": true,
}

func isHPRatioPowerMove(id string) bool { return hpRatioPowerMoves[id] }

// stockpileCount returns the user's live stockpile stacks (0 if none). Read by
// Spit Up (dynamic base power) and Swallow (heal fraction).
func stockpileCount(p *Pokemon) int {
	if p.Volatiles.Stockpile == nil {
		return 0
	}
	return p.Volatiles.Stockpile.Count
}

// releaseStockpile empties the stockpile and reverses the boosts it actually
// granted (Spit Up and Swallow both spend the stockpile when they fire). No-op
// when nothing is stockpiled.
//
// The two stats are reversed by their own tallies, not by the stack count: a
// Stockpile used at +6 Defense stacked and boosted nothing, so releasing it
// must not drop the Defense it never raised. That is canon's rule (its onEnd
// boosts by the recorded `def` / `spd`, not by `layers`), and the difference is
// visible in one turn — two Stockpiles at +5 Sp. Def leave the stat at +6, and
// undoing two stages there lands on +4 instead of +5.
func releaseStockpile(p *Pokemon, side int, log *[]LogLine) {
	st := p.Volatiles.Stockpile
	if st == nil || st.Count == 0 {
		return
	}
	def, spd := st.Def, st.SpD
	p.Volatiles.Stockpile = nil
	if def > 0 {
		applyStages(p, side, "defense", -def, log)
	}
	if spd > 0 {
		applyStages(p, side, "spdef", -spd, log)
	}
}

// swallowHealFraction maps a stockpile count to the fraction of max HP Swallow
// restores: 1/4, 1/2, full for 1/2/3 stacks.
var swallowHealFraction = map[int]float64{1: 0.25, 2: 0.5, 3: 1.0}

// applySwallow heals the user by the stockpile-scaled fraction and empties the
// stockpile. With no stockpile it fails loudly — except for a Swallow the user
// did not choose, which canon exempts: swallow's onTry returns early
// `if (move.sourceEffect === 'snatch')` and its onHit reads
// `pokemon.volatiles['stockpile']?.layers || 1`, so a thief with nothing
// stockpiled heals the one-stack amount off a Swallow it stole.
// applyStatusMoveFrom threads that provenance down as `snatched`, which is what
// canon carries on the move itself. Gated by move ID in applyStatusMove —
// Swallow ships with no declarative heal block because the amount is dynamic.
func applySwallow(s *BattleState, side int, snatched bool, log *[]LogLine) {
	p := s.Active(side)
	n := stockpileCount(p)
	if n == 0 {
		if !snatched {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
			return
		}
		// Canon's onHit reads `pokemon.volatiles['stockpile']?.layers || 1`, so
		// a thief with no stockpile of its own heals the one-stack amount off a
		// Swallow it stole. It is the `|| 1` that this branch is.
		n = 1
	}
	amt := int(math.Round(float64(p.MaxHP) * swallowHealFraction[n]))
	healPokemon(p, side, amt, log)
	// A no-op when the stockpile was the synthetic one above: there is nothing
	// to empty and, more to the point, no boost to take back.
	releaseStockpile(p, side, log)
}

// applyGrudgeVolatile is register-only — PP drain on attacker faint
// isn't modeled. The flag is set so future PP-aware mechanics can
// read it, and the start-of-grudge flavor log fires.
func applyGrudgeVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	p.Volatiles.Grudge = true
	*log = append(*log, LogLine{
		Type: "grudge", Side: side,
		Text: fmt.Sprintf("%s wants its foe to bear a grudge!", p.Name),
	})
}

// applyGastroAcidVolatile suppresses the target's ability until it switches
// out (the volatile is cleared with the rest of the bag on the way off the
// field, which is canon's duration for it).
//
// The GastroAcid flag used to be set and read by nothing: the note here said
// suppression "isn't threaded into the ability hook layer", and the move's
// "ability was suppressed!" line was a claim the engine did not honor. It is
// honored now, and by the same one-bool gate on abilityOf that Neutralizing
// Gas uses — the two mechanics are the same question asked twice, so they
// share the answer. See abilitysuppression.go.
//
// Only the GastroAcid volatile is set here. The AbilitySuppressed mirror that
// abilityOf actually reads is left to syncAbilitySuppression, which runs
// immediately after this move resolves — writing it here as well was tried and
// removed: no mutation of either write could be told apart by any test, and a
// second writer of a mirror is exactly what desynced the Magic Room one.
func applyGastroAcidVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.GastroAcid {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.GastroAcid = true
	*log = append(*log, LogLine{
		Type: "gastroacid", Side: side,
		Text: fmt.Sprintf("%s's ability was suppressed!", p.Name),
	})
}

// tickGimmicks decrements the timer volatiles on side's active and
// clears any that expire. Called from ResolveTurn's end-of-turn block.
// Persistent volatiles (SmackDown, Snatch, MagicCoat, Stockpile,
// Grudge, GastroAcid) are NOT touched here — Snatch / MagicCoat
// clear in the transient sweep (one-turn flags); the rest persist
// until switch-out wipes the bag.
func tickGimmicks(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	if mr := p.Volatiles.MagnetRise; mr != nil {
		mr.TurnsLeft--
		if mr.TurnsLeft <= 0 {
			p.Volatiles.MagnetRise = nil
			*log = append(*log, LogLine{
				Type: "magnetrise", Side: side,
				Text: fmt.Sprintf("%s's electromagnetism wore off.", p.Name),
			})
		}
	}
	if tk := p.Volatiles.Telekinesis; tk != nil {
		tk.TurnsLeft--
		if tk.TurnsLeft <= 0 {
			p.Volatiles.Telekinesis = nil
			*log = append(*log, LogLine{
				Type: "telekinesis", Side: side,
				Text: fmt.Sprintf("%s was freed from the telekinesis!", p.Name),
			})
		}
	}
}

// Magnet Rise, Telekinesis and Smack Down used to have their own Ground-move
// predicates here, read by computeDamage and by nothing else — which is why a
// lifted Pokemon dodged Earthquake and still took Spikes. All three are legs of
// groundedness (terrain.go) now, so every rule that cares about the ground
// gets the same answer.

// telekinesisAutoHits reports whether moves against the target should
// auto-hit (Telekinesis lifts the target and makes it a sitting duck).
// Called from resolveAccuracy.
func telekinesisAutoHits(def *Pokemon) bool {
	return def.Volatiles.Telekinesis != nil
}

// snatchInterceptsSelfStatus reports whether the foe should steal
// this side's incoming self-targeted status move. On true, the
// caller logs the snatch line, clears the foe's flag, and routes the
// status move via the foe's side instead.
func snatchInterceptsSelfStatus(s *BattleState, side int, m domain.Move) bool {
	if m.Category != domain.CatStatus {
		return false
	}
	if m.Target != domain.TargetSelf {
		return false
	}
	// Canon runs the move's own onTry *before* the theft: battle-actions.ts
	// evaluates singleEvent('Try') and only then runEvent('PrepareHit'), which
	// is where the snatcher's condition hooks in. So a move that was going to
	// fail on its user's state is not stolen at all — it fails, and the
	// snatcher keeps its charge. Upstream tests both halves of that (a
	// stockpile-less Swallow does not activate Snatch; a full-HP Rest cannot be
	// stolen), and it matters here because the thief's Swallow no longer needs
	// a stockpile of its own to heal — without this gate a user with nothing
	// stockpiled would be handing the thief a free quarter of its HP.
	//
	// Only Swallow is gated, because Swallow is the only onTry this file owns.
	// Rest's failure condition (full HP, no status) and Stockpile's (already at
	// three stacks) are the same shape and are not consulted here; closing
	// those means lifting the predicate out of applyStatusMove's Rest branch.
	if m.ID == "swallow" && stockpileCount(s.Active(side)) == 0 {
		return false
	}
	foe := s.Active(1 - side)
	return foe.Volatiles.Snatch
}

// magicCoatBouncesFoeStatus reports whether the foe should bounce
// this side's incoming foe-targeted status move. Bounceback is
// degraded — the move is blocked outright rather than redirected,
// but the audit-cleaning slug is registered. On true the caller
// clears the foe's flag, logs the block line, and returns from
// applyStatusMove without applying any effect.
func magicCoatBouncesFoeStatus(s *BattleState, side int, m domain.Move) bool {
	if m.Category != domain.CatStatus {
		return false
	}
	if m.Target != domain.TargetFoe {
		return false
	}
	foe := s.Active(1 - side)
	return foe.Volatiles.MagicCoat
}
