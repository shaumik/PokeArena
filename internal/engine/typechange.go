package engine

import (
	"fmt"
	"sort"
	"strings"

	"pokearena/internal/domain"
)

// typechange.go lifts the moves that rewrite a Pokémon's typing mid-battle:
// Soak, Reflect Type, Conversion and Conversion 2.
//
// All four arrive from data-sync as bare status shells — the whole of each one
// lives in a JS `onHit` upstream, so there is no effect block to carry — which
// is the same shape the ability-setting moves next door arrived in, and the
// same reason they need a hand-written gate. Without one they fall through
// applyStatusMoveFrom's declarative tail and narrate a clean success.
//
// The change is field-scoped in canon: `setType` writes the Pokémon's own
// `types` array, and clearVolatile discards it by re-running setSpecies. So it
// lasts exactly as long as the Pokémon is out, and Pokemon.BaseTypes is the
// memo that makes leaving put the real typing back — the same shape BaseAbility
// and BaseStats already use, and installSwitchIn restores all three together.
//
// Nothing in this engine caches or derives from a Pokémon's typing: every one
// of the two dozen readers — STAB, the type chart, groundedness, hazard
// effectiveness, status immunity, Curse's routing, Magnet Pull's hold — is a
// live read of Type1/Type2. So writing the two fields is the whole job, and
// Roost composes on top for free, because roostTypes is a read-time override
// that never writes them back.
//
// Two divergences worth stating, neither introduced here:
//
//   - Magic Coat bounces on shape rather than on canon's `reflectable` flag,
//     which the transform drops. Of these four only Soak carries it upstream,
//     so Reflect Type and Conversion 2 are bounced here and are not in canon.
//     Fixing that properly means resurrecting the flag for every status move,
//     which is a wider change than this one.
//   - Snatch intercepts on shape too, gating on a self-targeted status move.
//     Conversion is the only self-targeted move of the four and the only one
//     carrying `snatch` upstream, so that one lines up exactly — by accident,
//     but exactly.

// rememberBaseTypes takes the one-time snapshot that lets a type rewrite be
// undone on switch-out. First writer wins, so a Pokémon Soaked and then
// Conversion-2'd reverts to what it was built with rather than to the Water it
// wore in between — the same rule BaseAbility follows, and for the same reason.
func rememberBaseTypes(p *Pokemon) {
	if p.BaseTypes == nil {
		p.BaseTypes = &[2]domain.Type{p.Type1, p.Type2}
	}
}

// setTypes writes a new typing and announces it. The log line is emitted even
// when the new typing equals the old one, because canon emits `-start
// typechange` on every success and a silent no-op would read to
// TestNoCuratedMoveIsInert as a move that did nothing — which, in the audit's
// same-species fixture, a Reflect Type genuinely would.
func setTypes(p *Pokemon, side int, t1, t2 domain.Type, log *[]LogLine) {
	rememberBaseTypes(p)
	p.Type1, p.Type2 = t1, t2
	*log = append(*log, LogLine{
		Type: "typechange", Side: side,
		Text: fmt.Sprintf("%s's type changed to %s!", p.Name, typeListName(t1, t2)),
	})
}

// typeListName renders a typing for a log line: "Water", "Grass/Poison", or
// "???" for the typeless state nothing in this dataset can currently produce.
func typeListName(t1, t2 domain.Type) string {
	switch {
	case t1 == "" && t2 == "":
		return "???"
	case t2 == "" || t2 == t1:
		return titleType(t1)
	case t1 == "":
		return titleType(t2)
	}
	return titleType(t1) + "/" + titleType(t2)
}

// titleType renders a type slug for display. The dataset stores them
// lower-case; log lines have always shown them capitalized.
func titleType(t domain.Type) string {
	if t == "" {
		return "???"
	}
	return strings.ToUpper(string(t[:1])) + string(t[1:])
}

// applyTypeChangeMove dispatches the four. Called from applyStatusMoveFrom
// beside the other JS-callback moves; every path logs, so the caller can treat
// the move as resolved either way.
func applyTypeChangeMove(dex *domain.Dex, s *BattleState, side int, m domain.Move, rng *RNG, log *[]LogLine) {
	user, foe := s.Active(side), s.Active(1-side)
	fail := func() {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
	}
	switch m.ID {
	case "soak":
		// Soak carries no bypass-sub flag upstream, and the doll check that
		// serves the declarative moves lives inside applyEffectFields — which a
		// move with no effect block never reaches. So it is made here.
		if foe == nil || foe.Fainted || (hasSubstitute(foe) && !bypassesSubstitute(m, user)) {
			fail()
			return
		}
		// Canon compares the target's whole current typing against "Water", so
		// a Water/Flying target is soakable and a pure Water one is not.
		if foe.Type1 == "water" && foe.Type2 == "" {
			fail()
			return
		}
		setTypes(foe, 1-side, "water", "", log)
	case "reflect-type":
		// Reflect Type does carry bypass-sub, so the doll is transparent to it.
		if foe == nil || foe.Fainted {
			fail()
			return
		}
		// A target with no typing at all leaves nothing to copy. Unreachable
		// today — no move in this dataset makes a Pokémon typeless — and kept
		// because it is canon's own refusal rather than a guess about the dex.
		if foe.Type1 == "" && foe.Type2 == "" {
			fail()
			return
		}
		// The *current* typing, not the built-in one: a foe that has itself
		// been Soaked hands over Water. And a mono-typed target makes a
		// mono-typed copier, because canon assigns the whole list.
		setTypes(user, side, foe.Type1, foe.Type2, log)
	case "conversion":
		t, ok := conversionType(dex, user)
		if !ok {
			fail()
			return
		}
		setTypes(user, side, t, "", log)
	case "conversion-2":
		t, ok := conversion2Type(dex, user, foe, rng)
		if !ok {
			fail()
			return
		}
		setTypes(user, side, t, "", log)
	}
}

// conversionType is Conversion's choice: the type of the user's *first move
// slot*, unconditionally.
//
// Not the first damaging move, not a random one, not a filtered one — canon is
// literally `moveSlots[0]`, and a Conversion sitting in slot 0 therefore tries
// to make its user Normal. That is why the move fails so often in practice, and
// a "smarter" version would be a different move: it would stop failing where
// canon fails.
func conversionType(dex *domain.Dex, p *Pokemon) (domain.Type, bool) {
	if len(p.Moves) == 0 {
		return "", false
	}
	t := dex.Moves[p.Moves[0].MoveID].Type
	if t == "" || isType(p, t) {
		return "", false
	}
	return t, true
}

// allTypes is the type chart's own vocabulary, in the order Showdown's
// `dex.types.names()` yields it — alphabetical, which is what the file it is
// read from happens to be sorted by. Conversion 2 draws one index into a list
// built by walking this in order, so the order is part of the answer: a
// different one is still random, but it is not the same random, and every
// replay pinned to this engine's seed would move.
var allTypes = []domain.Type{
	"bug", "dark", "dragon", "electric", "fairy", "fighting", "fire", "flying",
	"ghost", "grass", "ground", "ice", "normal", "poison", "psychic", "rock",
	"steel", "water",
}

// conversion2Type is Conversion 2's choice: a type picked uniformly from those
// that resist or are immune to the foe's last move, excluding types the user
// already has.
//
// It fails in three ways, and the first is the interesting one. The foe must
// have used something, and that something must have had a type — canon reads
// `lastMoveUsed.type`, and a Struggle is typed `???`, so a Conversion 2 that
// follows one has no attack to answer and fails. This engine records that
// through Volatiles.LastMoveType, which is written for Struggle where the slug
// is not; reading the dex entry instead would find Struggle's static "Normal"
// and produce the opposite answer.
func conversion2Type(dex *domain.Dex, user, foe *Pokemon, rng *RNG) (domain.Type, bool) {
	if foe == nil {
		return "", false
	}
	attack := foe.Volatiles.LastMoveType
	if attack == "" {
		return "", false
	}
	candidates := make([]domain.Type, 0, len(allTypes))
	for _, t := range allTypes {
		if isType(user, t) {
			continue
		}
		// Canon tests the chart entry for "resists" or "immune", which is
		// exactly a multiplier at or below a half.
		if dex.Multiplier(attack, t) <= 0.5 {
			candidates = append(candidates, t)
		}
	}
	if len(candidates) == 0 {
		return "", false
	}
	// One uniform draw over the list, canon's `this.sample`. Sorted for the
	// same reason the list above is ordered: the index has to mean the same
	// thing on every run.
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	return candidates[rng.IntN(len(candidates))], true
}
