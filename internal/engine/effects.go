package engine

import (
	"fmt"
	"math"

	"pokearena/internal/domain"
)

// volatileHandler is the contract a mechanic file fulfills to claim a
// `Move.Primary.Volatile` slug. The signature is wide so every existing
// case can register without an adapter — handlers that don't need rng or
// the battle state simply ignore those args. Registration happens at
// package init via registerVolatile from the mechanic's own file.
type volatileHandler func(p *Pokemon, side int, source domain.Move, s *BattleState, rng *RNG, log *[]LogLine)

// volatileHandlers is the dispatch table applyVolatile consults. Populated
// by registerVolatile calls in init() functions across the package. Each
// mechanic owns its slugs; the spine only routes the call.
var volatileHandlers = map[string]volatileHandler{}

func registerVolatile(slug string, h volatileHandler) {
	volatileHandlers[slug] = h
}

// sideConditionSetter is the contract a mechanic fulfills to claim a
// `Move.SideCondition` slug. Screens (Reflect / Light Screen / Aurora
// Veil) and hazards (Stealth Rock / Spikes / Toxic Spikes) register
// closures that bind their own ScreenKind / HazardKind constants and
// call applyScreenSetter / applyHazardSetter under the hood.
type sideConditionSetter func(s *BattleState, side int, log *[]LogLine)

// sideConditionSetters is the dispatch table applyStatusMove consults
// when a status move declares a side-condition. Same registration pattern
// as volatileHandlers.
var sideConditionSetters = map[string]sideConditionSetter{}

func registerSideCondition(slug string, h sideConditionSetter) {
	sideConditionSetters[slug] = h
}

// applyStatusMove handles the guaranteed primary effect of a status-category
// move. The primary applies to the move's declared target.
//
// Returns whether the move actually resolved for `side`. False only for the two
// interceptions below — Snatch (the move resolved for the thief) and Magic Coat
// (it never resolved at all). Both are the same category as Protect, so a
// caller hanging post-move hooks off this must skip them; a move that resolved
// and then failed to accomplish anything still returns true, which is canon.
//
// Weather and terrain setters (Move.Weather / Move.Terrain != "") are
// dispatched here too: if the move names one, the new condition takes effect
// for its default-turn duration. A setter that names the *currently active*
// weather / terrain fails (matches Showdown — Rain Dance in rain is a
// wasted PP; same for Electric Terrain in electric terrain).
func applyStatusMove(dex *domain.Dex, s *BattleState, side int, m domain.Move, rng *RNG, log *[]LogLine) (resolved bool) {
	return applyStatusMoveFrom(dex, s, side, m, false, rng, log)
}

// applyStatusMoveFrom is applyStatusMove with the one piece of provenance a
// handler can need: whether `side` chose this move or stole it. Canon carries
// that as move.sourceEffect and two of Swallow's callbacks read it, so the
// only way to answer it here is to pass it down — the Snatch branch below is
// the sole caller that passes true, and it is the sole way a move can resolve
// for somebody who did not select it.
func applyStatusMoveFrom(dex *domain.Dex, s *BattleState, side int, m domain.Move, snatched bool, rng *RNG, log *[]LogLine) (resolved bool) {
	// Snatch: a foe's snatcher waiting for a self-target status move
	// intercepts this attempt. The snatcher's flag clears and the
	// status move re-routes through the snatcher's side (so target=
	// self means the snatcher boosts itself, etc.). Foe-target status
	// moves are NOT snatched — canonical Showdown behavior.
	if snatchInterceptsSelfStatus(s, side, m) {
		foe := s.Active(1 - side)
		foe.Volatiles.Snatch = false
		*log = append(*log, LogLine{
			Type: "snatch", Side: 1 - side,
			Text: fmt.Sprintf("%s snatched the move!", foe.Name),
		})
		applyStatusMoveFrom(dex, s, 1-side, m, true, rng, log)
		// The move resolved for the *snatcher*, not for `side` — the caller's
		// post-move hooks must not fire for a user whose move was taken.
		return false
	}
	// Magic Coat: a foe's coater intercepts a foe-target status move.
	// Bounceback is degraded — the move is blocked outright rather
	// than re-applied with reversed roles. The slug is still
	// registered so the audit clears.
	if magicCoatBouncesFoeStatus(s, side, m) {
		foe := s.Active(1 - side)
		foe.Volatiles.MagicCoat = false
		*log = append(*log, LogLine{
			Type: "magiccoat", Side: 1 - side,
			Text: fmt.Sprintf("%s bounced the move back!", foe.Name),
		})
		return false
	}
	if m.Weather != "" {
		applyWeatherSetter(s, side, WeatherKind(m.Weather), log)
		return true
	}
	if m.Terrain != "" {
		applyTerrainSetter(s, side, TerrainKind(m.Terrain), log)
		return true
	}
	if m.SideCondition != "" {
		if h, ok := sideConditionSetters[m.SideCondition]; ok {
			h(s, side, log)
		}
		return true
	}
	if m.PseudoWeather != "" {
		if h, ok := pseudoWeatherSetters[m.PseudoWeather]; ok {
			h(s, side, log)
		}
		return true
	}
	if m.SlotCondition != "" {
		if h, ok := slotConditionSetters[m.SlotCondition]; ok {
			h(s, side, log)
		}
		return true
	}
	// Defog: status move with no top-level effect block — Showdown encodes
	// its evasion drop and field-wipe in JS. Handled here by move ID rather
	// than via the SideCondition path (Defog's own sideCondition is "").
	if m.ID == "defog" {
		applyDefog(s, side, log)
		return true
	}
	// The JS-callback moves (see callbackmoves.go). Each arrives from
	// data-sync as a shell with no effect block, so without these gates the
	// `m.Primary == nil` fallthrough below reads them as clean successes.
	switch m.ID {
	case "haze":
		applyHaze(s, side, log)
		return true
	case "psych-up":
		applyPsychUp(s, side, log)
		return true
	case "mean-look", "block":
		applyMeanLook(s, side, m, log)
		return true
	case "heal-bell", "aromatherapy":
		applyTeamStatusCure(s, side, m, log)
		return true
	case "perish-song":
		applyPerishSong(s, side, log)
		return true
	case "spite":
		applySpite(s, side, log)
		return true
	case "guard-swap":
		swapStages(s, side, []string{"defense", "spdef"}, "Defense and Sp. Def", log)
		return true
	case "power-swap":
		swapStages(s, side, []string{"attack", "spatk"}, "Attack and Sp. Atk", log)
		return true
	case "speed-swap":
		applySpeedSwap(s, side, log)
		return true
	case "power-split":
		applyPowerSplit(s, side, log)
		return true
	case "heal-pulse":
		applyHealPulse(s, side, log)
		return true
	case "mimic":
		applyMimic(dex, s, side, log)
		return true
	case "soak", "reflect-type", "conversion", "conversion-2":
		applyTypeChangeMove(dex, s, side, m, rng, log)
		return true
	case "lock-on", "mind-reader":
		applyLockOn(s, side, m, log)
		return true
	case "belly-drum":
		applyBellyDrum(s, side, log)
		return true
	case "pain-split":
		applyPainSplit(s, side, log)
		return true
	case "refresh":
		applyRefresh(s, side, log)
		return true
	case "venom-drench":
		applyVenomDrench(s, side, log)
		return true
	case "acupressure":
		applyAcupressure(s, side, rng, log)
		return true
	case "rototiller":
		applyRototiller(s, side, log)
		return true
	case "celebrate":
		applyCelebrate(s, side, log)
		return true
	case "magnetic-flux":
		applyMagneticFlux(s, side, log)
		return true
	case "growth":
		// The +1/+1 rides the declarative Primary block; only the sun
		// doubling is lifted, so fall through when the sun is not up.
		if applyGrowthBoosts(s, side, log) {
			return true
		}
	}
	// The ability-rewriting moves (Worry Seed, Simple Beam, Role Play, Skill
	// Swap). Same shell problem as the callback moves above — all four ship
	// with no effect payload, so the fallthrough read them as successes and
	// they narrated a hit that changed nothing. See abilitysetting.go.
	if applyAbilitySettingMove(s, side, m, log) {
		return true
	}
	// Curse: split move whose behavior depends on the user's type
	// (Ghost vs not). The dataset captures the Ghost-target shape
	// only; the type-routed dispatch lives in applyCurse. Same
	// move-ID gate as Defog — encoded in JS upstream, lifted here.
	if m.ID == "curse" {
		applyCurse(s, side, m, rng, log)
		return true
	}
	// Moonlight / Synthesis / Morning Sun: self-heal whose amount scales with
	// the active weather. The heal lives in a JS callback upstream, so the
	// curated move has no Effect block — lifted here by ID like Defog / Curse.
	if isWeatherHealMove(m.ID) {
		applyWeatherHeal(s, side, log)
		return true
	}
	// Rest: Showdown encodes the whole move in JS (an onHit that sets sleep to
	// exactly 3 turns and heals to full), so the curated entry carries no Effect
	// block at all and the declarative path below would resolve it to nothing.
	// Lifted by ID like Defog / Curse / Swallow. Fails at full HP with no
	// status, matching canon — otherwise it is a free two-turn nap.
	if m.ID == "rest" {
		p := s.Active(side)
		if p.HP == p.MaxHP && p.Status == StatusNone {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
			return true
		}
		if !doRest(p, side, s, rng, log) {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		}
		return true
	}
	// Item-manipulation status moves (Trick, Switcheroo, Bestow, Corrosive Gas,
	// Recycle). All encoded in JS upstream, so none carries an Effect block;
	// items_moves.go owns them and reports whether it claimed this one.
	if applyItemStatusMove(s, side, m, log) {
		return true
	}
	// Swallow: heal scaled by the user's stockpile count (no declarative heal
	// block — the amount is dynamic). Consumes the stockpile. Gated by ID.
	if m.ID == "swallow" {
		applySwallow(s, side, snatched, log)
		return true
	}
	// Roost: the 50% heal rides the Primary block below; the side effect we
	// lift here is the one-turn loss of the Flying type. Non-returning so the
	// declarative heal still applies.
	//
	// The full-HP gate has to be *here*, above the volatile, rather than left to
	// the heal below. Canon's heal block does `continue` when the target is
	// already whole, which skips the move's self block along with the heal — so
	// a Roost that fails never costs the user its Flying type. Setting the
	// volatile first and then discovering the heal was a no-op made a full-HP
	// Aerodactyl Ground-vulnerable for nothing, which is the sibling case
	// upstream measures at 24 damage from a Mud-Slap it should be immune to.
	if m.ID == "roost" {
		p := s.Active(side)
		if p.HP >= p.MaxHP {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
			return true
		}
		p.Volatiles.Roost = true
	}
	// forceSwitch status variants (Roar, Whirlwind): no Primary,
	// no Weather/Terrain/etc — the whole point is the switch. A
	// foe with no live bench is a "But it failed!"; a successful
	// switch logs the drag-out line and runs hazards on the
	// incoming. The bypass-acc / sound flags from upstream are
	// already on the move; no extra accuracy work needed here.
	if m.ForceSwitch {
		if !applyForceSwitch(s, side, rng, log) {
			*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		}
		return true
	}
	if m.Primary == nil {
		return true
	}
	atk := s.Active(side)
	def := s.Active(1 - side)
	tgt, tside := def, 1-side
	if m.Target == domain.TargetSelf {
		tgt, tside = atk, side
	}
	if failed := applyEffectFields(m.Primary, m, atk, side, tgt, tside, 0, s, rng, log); failed {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		// Reported as unresolved, not merely announced. This path used to
		// return true regardless, so a move that printed "But it failed!" was
		// still recorded as having worked — which kept the Metronome streak
		// alive through an Attract into a genderless target and left Stomping
		// Tantrum with no failure to double off. The log line and the return
		// value now agree.
		return false
	}
	return true
}

// applyDamageEffects runs the post-damage effects of a damaging move: the
// guaranteed Self block on the user, the guaranteed Primary on the foe (e.g.
// partial-trap moves' volatileStatus), and each rolled Secondary on the foe.
// Primary effects bypass Shield Dust and Sheer Force the way Showdown's
// top-level effects do; only entries in m.Secondaries are gated by those.
func applyDamageEffects(s *BattleState, side int, m domain.Move, dmg int, rng *RNG, log *[]LogLine) {
	atk := s.Active(side)
	def := s.Active(1 - side)
	// This whole function runs inside the faint window (see turn.go): a
	// Pokémon killed by the hit we are resolving still has Fainted == false
	// and HP == 0, because faints are batched after the effects. So every
	// "is this Pokémon out of the fight?" question here has to go through
	// isDown(), which tests the HP — the guards below read Fainted alone and
	// were therefore never true in this window, letting a drain move heal a
	// corpse and a Primary effect land on a target already at zero.
	if m.Self != nil && !isDown(atk) {
		applyEffectFields(m.Self, m, atk, side, atk, side, dmg, s, rng, log)
	}
	if m.Primary != nil && !isDown(def) {
		applyEffectFields(m.Primary, m, atk, side, def, 1-side, dmg, s, rng, log)
	}
	// Sheer Force trades every secondary away for the damage boost, so it
	// gates the whole loop — including the user's own self-boosts.
	if !abilityBlocksOwnSecondaries(atk) {
		// Covert Cloak sits beside Shield Dust: both refuse the added effects
		// of an attack aimed at the holder. Neither reaches a secondary the
		// attacker points at itself — canon filters on the self flag, not on
		// the move — so this is checked per-entry rather than around the loop.
		foeRefuses := abilityBlocksSecondaries(s, def) || itemBlocksSecondaries(def)
		chanceMult := abilitySecondaryChanceMult(atk) // Serene Grace doubles
		for i := range m.Secondaries {
			sec := &m.Secondaries[i]
			tgt, tside := def, 1-side
			if sec.Self {
				tgt, tside = atk, side
			}
			if !sec.Self && foeRefuses {
				continue
			}
			chance := int(float64(sec.Chance) * chanceMult)
			if chance > 100 {
				chance = 100
			}
			if rng.Chance(chance) {
				// Rolled, then checked — deliberately in that order. A target
				// the same hit put at 0 HP takes no secondary (canon skips it;
				// Showdown guards each site with `if (!target.hp)`), but the
				// roll is still consumed so the RNG stream, and every replay
				// and fixture pinned to it, is unchanged by this guard.
				if isDown(tgt) {
					continue
				}
				applyEffectFields(sec, m, atk, side, tgt, tside, dmg, s, rng, log)
			}
		}
	}
	// Attacker-ability contact rider (Poison Touch): the holder's own move
	// effect, so it runs regardless of the target's Shield Dust / Sheer Force.
	applyOnDealDamage(s, side, m, rng, log)
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
				applyStagesFromFoe(tgt, tgtSide, stat, delta, s, log)
			} else {
				applyStages(tgt, tgtSide, stat, delta, log)
			}
		}
		// White Herb answers immediately, the way canon's onUpdate does —
		// waiting for the end of the turn would mean the holder attacks at the
		// lowered stat it is holding the herb specifically to avoid. It runs
		// after the whole boosts block, not per stat: Tickle drops Attack and
		// Defense in one effect and the herb has to see both before it fires.
		if fromFoe {
			applyItemStatCheck(tgt, tgtSide, log)
		}
	}
	if e.Status != "" {
		// Safeguard on the target's side blocks foe-induced non-volatile
		// status outright. Self-status (Rest is its own path; status moves
		// targeting self) bypasses since tgt==atk. Logged loud on a primary
		// (status-move dispatcher checks statusFailed), silent on a damage
		// secondary — same shape as the substitute block above.
		if tgt != atk && safeguardBlocksFoeStatus(s, tgtSide) {
			*log = append(*log, LogLine{
				Type: "safeguard", Side: tgtSide,
				Text: fmt.Sprintf("%s is protected by Safeguard!", tgt.Name),
			})
			statusFailed = true
		} else if !inflictStatusFrom(tgt, tgtSide, atkSide, StatusCond(e.Status), s, rng, log) {
			statusFailed = true
		}
	}
	if e.Volatile != "" {
		if tgt != atk && safeguardBlocksFoeVolatile(s, tgtSide, e.Volatile) {
			*log = append(*log, LogLine{
				Type: "safeguard", Side: tgtSide,
				Text: fmt.Sprintf("%s is protected by Safeguard!", tgt.Name),
			})
		} else {
			// A refused volatile is a failed move. The handlers announce their
			// own refusals — "But it failed!", or Oblivious's bespoke line — so
			// this only records the outcome and adds no second message.
			if applyVolatile(tgt, tgtSide, e.Volatile, source, s, rng, log) {
				statusFailed = true
			}
			// Mental Herb frees the holder the moment a restriction lands, so a
			// Taunt doesn't cost it the turn it was going to act on.
			if tgt != atk {
				applyItemStatCheck(tgt, tgtSide, log)
			}
		}
	}
	if e.Heal > 0 {
		// A declarative self-heal fails at full HP, and says so. Canon is
		// explicit about it — moveHit opens the heal block with
		// `if (target.hp >= target.maxhp) { add('-fail'); ... continue; }` —
		// and healPokemon caps at MaxHP and logs nothing when HP does not
		// move, so Recover, Soft-Boiled, Slack Off, Life Dew and Roost all
		// used to resolve silently at full HP: no heal line, no failure.
		//
		// Cosmetic for four of the five. Not for Roost: the `continue` above is
		// what stops canon reaching the move's self block, and this engine set
		// Volatiles.Roost *before* the heal was attempted, so a Roost that
		// should have failed still stripped the user's Flying type for the turn
		// — a full-HP Aerodactyl became Ground-vulnerable for free. Roost's
		// own gate is in applyStatusMove, above the volatile, for that reason.
		if atk.HP >= atk.MaxHP {
			statusFailed = true
		} else {
			amt := int(math.Round(float64(atk.MaxHP) * e.Heal))
			healPokemon(atk, atkSide, amt, log)
		}
	}
	if e.Drain > 0 && dmgDealt > 0 {
		// Big Root scales the recovery, not the damage — including the amount
		// Liquid Ooze turns back on the drainer below, which is canon.
		amt := int(math.Round(float64(dmgDealt) * e.Drain * itemDrainMult(atk)))
		// Liquid Ooze on the drained foe poisons the well: the drainer takes
		// the would-be-healed amount as damage instead of recovering it.
		if foe := s.Active(1 - atkSide); abilityDrainBackfires(foe) {
			revealAbility(foe)
			*log = append(*log, LogLine{
				Type: "ability", Side: 1 - atkSide,
				Text: fmt.Sprintf("%s sucked up the liquid ooze!", atk.Name),
			})
			applySelfDamage(atk, atkSide, amt, log)
		} else {
			healPokemon(atk, atkSide, amt, log)
		}
	}
	if e.Recoil > 0 && dmgDealt > 0 && !abilityBlocksIndirectDamage(atk) && !abilityBlocksRecoil(atk) {
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
		doRest(atk, atkSide, s, rng, log)
	}
	return statusFailed
}

// cureStatus clears the user's non-volatile status. No-op if none.
func cureStatus(p *Pokemon, side int, log *[]LogLine) {
	if p.Status == StatusNone {
		return
	}
	prev := p.Status
	clearStatus(p)
	*log = append(*log, LogLine{
		Type: "status", Side: side,
		Text: fmt.Sprintf("%s was cured of its %s!", p.Name, prev),
	})
}

// doRest implements Rest: replace any status with a forced sleep and, if that
// sleep lands, heal to full. Reports whether it went through, so the caller can
// log "But it failed!".
//
// It used to write the status and the heal directly, which is the whole defect:
// the only terrainBlocksStatus call site is inside inflictStatus, so Electric
// Terrain and Misty Terrain did not stop a Rest, and neither did Insomnia or
// Vital Spirit. The Chesto Berry check was re-made locally, which is exactly
// what made the omission look considered — one of the checks the bypassed path
// performs was remembered and the rest were not.
//
// The order matters and is canon's: `const result = target.setStatus('slp', ...);
// if (!result) return result;` — the heal is downstream of the status. A Rest
// refused by the terrain heals nothing.
//
// Two things are deliberately *not* re-made here. Safeguard is one:
// inflictStatus does not consult it, and must not, because upstream's Safeguard
// only refuses foe-sourced status (its onSetStatus returns early on `!source`)
// — a Pokemon may Rest behind its own Safeguard. And the Sleep Clause is the
// other: it lives in inflictStatusFrom, the foe-induced path, so routing Rest
// through inflictStatus keeps the canonical carve-out falling out of the call
// graph rather than needing a flag. Both were already right; the point is that
// they stay right.
//
// The status is cleared before the attempt because canon's setStatus refuses
// only the *same* status, not a different one — the one-status-at-a-time rule
// lives in trySetStatus, which Rest does not go through. If the sleep is
// refused, the old status goes back.
func doRest(p *Pokemon, side int, s *BattleState, rng *RNG, log *[]LogLine) bool {
	prevStatus, prevSleep, prevToxic := p.Status, p.SleepTurns, p.ToxicCounter
	p.Status = StatusNone
	p.SleepTurns = 0
	p.ToxicCounter = 0
	if !inflictStatus(p, side, StatusSleep, s, rng, log) {
		p.Status, p.SleepTurns, p.ToxicCounter = prevStatus, prevSleep, prevToxic
		return false
	}
	// Rest's sleep is exactly two turns rather than inflictStatus's 2..4 roll:
	// canon sets it to 3 and counts differently, and this engine's two turns are
	// the same number of missed actions. Overwritten after the fact because the
	// guards, not the duration, are what inflictStatus is being asked for.
	//
	// A Chesto Berry may already have cleared the sleep inside inflictStatus, in
	// which case there is nothing to overwrite — that is the canonical combo, so
	// the duration is only set if the status actually stuck.
	if p.Status == StatusSleep {
		p.SleepTurns = 2
	}
	p.HP = p.MaxHP
	*log = append(*log, LogLine{
		Type: "status", Side: side,
		Text: fmt.Sprintf("%s became healthy!", p.Name),
	})
	return true
}

// applyVolatile inflicts a volatile condition on the target. Routes the
// slug through volatileHandlers — each mechanic file registers its own
// handler at init() time, so adding a new volatile is one file edit
// (plus the state field on Volatiles). A fainted target short-circuits
// before any handler runs; an unknown slug is a silent no-op (matches
// the previous switch's default).
// It reports whether the volatile was refused — by the handler's own rules
// (Attract into a genderless target, Oblivious, a volatile already present) or
// because the target was in no state to receive it.
//
// Refusal is detected by comparing the target's volatile set across the call
// rather than by asking the handler, and the reason is proportion: the
// volatileHandler signature is shared by 36 registrations, and widening it to
// return a bool would touch every one of them to answer a question the state
// already answers. A handler that succeeds always writes something into
// Volatiles; one that refuses writes nothing.
//
// The one case that comparison cannot see is a handler that *restarts* an
// existing volatile by mutating through its pointer without replacing it —
// there is no such handler today, and canon generally treats a restart as a
// failure anyway, which is the same answer this returns.
func applyVolatile(p *Pokemon, side int, name string, source domain.Move, s *BattleState, rng *RNG, log *[]LogLine) (refused bool) {
	if p.Fainted {
		return true
	}
	h, ok := volatileHandlers[name]
	if !ok {
		return true
	}
	before := p.Volatiles
	h(p, side, source, s, rng, log)
	return p.Volatiles == before
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

// inflictStatus applies a non-volatile status, respecting status-condition type
// immunities and the one-status-at-a-time rule. Status-condition immunities are
// the ones keyed on the *status* — Electric can't be paralyzed, Fire can't be
// burned, Steel and Poison can't be poisoned — and are a different axis from
// whether the delivering move's type reaches the target at all. That second
// question is settled before this is ever called, by
// resolveStatusMoveTypeImmunity. It reports whether the status took hold.
// s is the battle state, consulted for terrain guards (Misty blocks all
// status, Electric blocks Sleep, both only on grounded targets).
// inflictStatusFrom applies a status caused by an identifiable source Pokémon,
// then honors Synchronize: if the freshly-statused Pokémon has that ability and
// the status is one it bounces (burn / poison / toxic / paralysis), the source
// catches the same status. srcSide == targetSide means a self-inflicted status,
// which never bounces. Sourceless statuses (hazards, Rest) call inflictStatus
// directly instead.
func inflictStatusFrom(target *Pokemon, targetSide, srcSide int, st StatusCond, s *BattleState, rng *RNG, log *[]LogLine) bool {
	// Sleep Clause: one Pokémon asleep per side at a time. Checked here
	// rather than in inflictStatus because this is the foe-induced path —
	// Rest and the other self-inflicted sleeps go straight to inflictStatus
	// and are exempt, which is the canonical carve-out and falls out of the
	// call graph rather than needing a flag.
	if st == StatusSleep && srcSide != targetSide && sleepClauseBlocks(s, targetSide, target) {
		*log = append(*log, LogLine{
			Type: "fail", Side: targetSide,
			Text: fmt.Sprintf("%s stayed awake! (Sleep Clause)", target.Name),
		})
		return false
	}
	if !inflictStatus(target, targetSide, st, s, rng, log) {
		return false
	}
	if srcSide != targetSide {
		applySynchronize(s, targetSide, st, rng, log)
	}
	return true
}

// applySynchronize bounces a just-applied status back onto the opposing active
// when the newly-statused Pokémon has Synchronize. Only the contact-status set
// (burn / poison / toxic / paralysis) reflects; the bounce runs through
// inflictStatus so the source's own typing and ability guards can still refuse
// it. It intentionally does not re-enter inflictStatusFrom — the reflection is
// terminal and must not chain back.
func applySynchronize(s *BattleState, statusedSide int, st StatusCond, rng *RNG, log *[]LogLine) {
	switch st {
	case StatusBurn, StatusPoison, StatusToxic, StatusParalysis:
	default:
		return
	}
	holder := s.Active(statusedSide)
	if a := abilityOf(holder); a == nil || !a.Synchronizes {
		return
	}
	src := s.Active(1 - statusedSide)
	if src.Fainted {
		return
	}
	revealAbility(holder)
	*log = append(*log, LogLine{
		Type: "ability", Side: statusedSide,
		Text: fmt.Sprintf("%s's Synchronize afflicted %s!", holder.Name, src.Name),
	})
	inflictStatus(src, 1-statusedSide, st, s, rng, log)
}

func inflictStatus(p *Pokemon, side int, st StatusCond, s *BattleState, rng *RNG, log *[]LogLine) bool {
	// isDown, not Fainted: this is reachable inside turn.go's faint window,
	// where a Pokémon killed by the hit being resolved is still at HP 0 with
	// the flag unset. Guarding at the sink means no caller can status a body
	// on its way out — a dying Synchronize holder bouncing a burn back onto
	// its killer being the case that actually bites.
	if p.Status != StatusNone || isDown(p) {
		return false
	}
	if abilityBlocksStatus(p, st) {
		return false
	}
	if s != nil && abilityBlocksStatusState(s, p, st) {
		return false
	}
	if s != nil && terrainBlocksStatus(s.Terrain, &s.PseudoWeather, p, st) {
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
		// Harsh sunlight has forbidden freeze since Gen 2 — it is a property
		// of the weather, not of the target, so it sits here rather than with
		// the ability and terrain guards above. Read through effectiveWeather
		// so Cloud Nine and Air Lock suppress the immunity along with the sun.
		if s != nil {
			if w := effectiveWeather(s); w != nil && w.Kind == WeatherSun {
				return false
			}
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
	// A status-cure berry fires the instant the condition lands. The status is
	// still reported as inflicted (this returns true): Synchronize and anything
	// else keyed on "the status happened" must still see it happen, the berry
	// just doesn't let it stick.
	applyItemStatusCure(s, p, side, log)
	return true
}

// applyStagesFromFoe is the foe-induced variant: it consults the target
// side's Mist shield first (blocks any drop outright), then ability
// guards (Clear Body, Hyper Cutter, Big Pecks, Keen Eye) that block
// specific drops, and finally fires reactor abilities (Defiant,
// Competitive) when a drop lands. Self-induced stat changes (Swords
// Dance, Curse on self, etc.) bypass this and call applyStages directly.
func applyStagesFromFoe(p *Pokemon, side int, stat string, delta int, s *BattleState, log *[]LogLine) {
	if mistBlocksFoeDrop(s, side) {
		*log = append(*log, LogLine{
			Type: "mist", Side: side,
			Text: fmt.Sprintf("%s is protected by the mist!", p.Name),
		})
		return
	}
	if abilityBlocksStatLowerByFoe(s, p, stat) {
		revealAbility(p)
		*log = append(*log, LogLine{
			Type: "ability", Side: side,
			Text: fmt.Sprintf("%s's ability prevented the stat drop!", p.Name),
		})
		return
	}
	// Clear Amulet is the item form of Clear Body: it refuses any foe-induced
	// drop. Self-inflicted drops (Close Combat, Overheat) reach applyStages
	// directly and are unaffected.
	if itemBlocksStatDrops(p) {
		revealItem(p)
		*log = append(*log, LogLine{
			Type: "item", Side: side,
			Text: fmt.Sprintf("%s's %s prevented the stat drop!", p.Name, itemOf(p).Name),
		})
		return
	}
	applyStages(p, side, stat, delta, log)
	// Reactor hooks fire only when the drop actually occurred. applyStages
	// doesn't currently return a "did clamp" signal, so we recompute by
	// checking that the stage moved off its previous floor.
	applyOnStatLoweredByFoe(p, side, stat, log)
	// The White Herb check deliberately does NOT live here. This function is
	// called once per stat, so a herb fired from inside it would answer the
	// first drop of a multi-stat effect and be gone before the rest landed —
	// Tickle would leave the holder at Atk 0 / Def −1. Callers run
	// applyItemStatCheck once the whole set of drops has been applied.
}

// applyStages changes a stat stage, clamped to -6..+6.
//
// The Simple doubling is applied here rather than at the call sites because
// canon doubles the *boost* and not the source: Showdown's simple.onChangeBoost
// runs on every boost the holder receives, from its own Swords Dance and from a
// foe's Charm alike, and the log then reports the doubled figure ("rose
// sharply" off a +1 move). Doing it at the top also means the ±6 clamp and the
// "won't go higher" line see the real magnitude.
func applyStages(p *Pokemon, side int, stat string, delta int, log *[]LogLine) {
	ptr := stagePtr(p, stat)
	if ptr == nil {
		return
	}
	delta *= abilityStageDeltaMult(p)
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
	// Canon records the direction inside boost() itself, on the Pokémon being
	// boosted and regardless of who caused it — which is why Lash Out (reads
	// the user's own drop) and Burning Jealousy (reads the target's rise) can
	// both be served from one pair of flags. Recorded only when the stage
	// actually moved: a change refused by Clear Body or clamped at ±6 did not
	// happen, and must not arm either move.
	if delta > 0 {
		p.Volatiles.StatsRaisedThisTurn = true
	} else if delta < 0 {
		p.Volatiles.StatsLoweredThisTurn = true
	}
	*log = append(*log, LogLine{
		Type: "stat", Side: side,
		Text: fmt.Sprintf("%s's %s %s!", p.Name, statName(stat), stageVerb(delta)),
	})
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
