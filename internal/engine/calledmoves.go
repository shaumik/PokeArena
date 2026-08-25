package engine

import (
	"sort"

	"pokearena/internal/domain"
)

// calledmoves.go implements the moves whose whole content is "resolve some
// other move instead of me": Sleep Talk, Copycat, Metronome, Mirror Move and
// Me First.
//
// It is the general form of the seam Nature Power already used, and the note
// there argued at length that building the general form would be the wrong
// trade — that these five need "to call a move the *user* did not choose,
// mid-resolution and re-entrantly", where Nature Power only needs a
// substitution. Half of that is right and half of it is not, and the difference
// is what this file rests on.
//
// Re-entrancy is what canon's useMove gives you. But re-entering executeMove is
// not how you get it here, because that function is one long sequence of things
// that are true of *an action* rather than of a move: it counts a move action
// for Fake Out, it rolls paralysis and ticks sleep, it deducts PP, it commits a
// Choice item, it ticks a rampage, and it resets a stall chain. A called move is
// none of those things. Every one would have to be parameterized away, and what
// would be left is precisely the substitution.
//
// So a called move is a substitution, and the two places that would notice the
// difference are handled explicitly instead:
//
//   - The caller is still what the user "last used", for Disable, Encore,
//     Torment and Mirror Move. Canon agrees: moveUsed is called from runMove
//     for the chosen move and never for a called one.
//   - The *called* move is what the battle last saw, for Copycat and for
//     Conversion 2. Canon agrees there too, by writing lastMoveUsed from
//     useMoveInner, which every route reaches.
//
// The one real cost is that a called move cannot be a two-turn move or a
// rampage. Both of those arm a volatile that points at a *slot index*, and a
// called move has no slot — a Metronome that rolled Solar Beam would arm a
// charge aimed at Metronome's own slot and resolve Metronome again next turn.
// Rather than model a slotless charge for the sake of a random roll, callable
// excludes them and the caller fails visibly when nothing else is left. Canon
// excludes charge moves from Sleep Talk for its own reasons and excludes them
// from nothing else, so this is a real divergence, stated here and visible in
// play.

// callsAnotherMove reports whether m resolves by picking some other move.
func callsAnotherMove(m domain.Move) bool {
	switch m.ID {
	case "sleep-talk", "copycat", "metronome", "mirror-move", "me-first":
		return true
	}
	return false
}

// callable reports whether a move can stand in as a called move. The two
// exclusions are the slot-indexed ones — see the file header.
func callable(m domain.Move) bool {
	return m.ID != "" && !m.HasFlag("two-turn") && !isLockedMove(m.ID)
}

// chooseCalledMove picks what a caller resolves as, reporting false when there
// is nothing to pick and the caller should fail.
//
// foeAction and foeMoved are Me First's alone: it is the one caller that reads
// what the *other* side has queued rather than what somebody already used.
func chooseCalledMove(dex *domain.Dex, s *BattleState, side int, m domain.Move,
	foeAction Action, foeMoved bool, rng *RNG,
) (domain.Move, bool) {
	switch m.ID {
	case "sleep-talk":
		return sleepTalkMove(dex, s.Active(side), rng)
	case "copycat":
		return copycatMove(dex, s)
	case "metronome":
		return metronomeMove(dex, rng)
	case "mirror-move":
		return mirrorMove(dex, s.Active(1-side))
	case "me-first":
		return meFirstMove(dex, s, side, foeAction, foeMoved)
	}
	return domain.Move{}, false
}

// sleepTalkMove picks uniformly from the user's own slots, minus the ones canon
// refuses: everything flagged no-sleep-talk (which is every caller including
// Sleep Talk itself, plus Focus Punch, Uproar and the charge moves) and, here,
// the rampages as well.
//
// There is no PP filter. Canon has none either — a Sleep Talk can call a move
// whose slot is empty, because the called move never pays.
func sleepTalkMove(dex *domain.Dex, p *Pokemon, rng *RNG) (domain.Move, bool) {
	pool := make([]domain.Move, 0, len(p.Moves))
	for _, slot := range p.Moves {
		mv := dex.Moves[slot.MoveID]
		if mv.HasFlag("no-sleep-talk") || !callable(mv) {
			continue
		}
		pool = append(pool, mv)
	}
	if len(pool) == 0 {
		return domain.Move{}, false
	}
	return pool[rng.IntN(len(pool))], true
}

// copycatMove repeats whatever the battle last saw anyone use — including a
// move that some other caller called, and including one that announced and then
// failed. Canon's register is the battle's, not the Pokémon's, and it is
// written from the inside of the call rather than the outside.
func copycatMove(dex *domain.Dex, s *BattleState) (domain.Move, bool) {
	mv, ok := dex.Moves[s.LastMoveUsedID]
	if !ok || mv.HasFlag("fail-copycat") || !callable(mv) {
		return domain.Move{}, false
	}
	return mv, true
}

// metronomeMove rolls one of the moves upstream marks as reachable by it: 522
// of this dataset's 559 carry the flag, and 508 survive callable's exclusion of
// the two-turn moves and the rampages.
//
// A smaller pool than canon's 577, and the difference is in this engine's
// favor: every one of the 508 is curated, implemented, and carried by
// TestNoCuratedMoveIsInert, where canon's list names hundreds of moves this
// engine has never heard of.
//
// Sorted before the draw. The pool is built by walking a map, and an unsorted
// one would make the roll depend on Go's map iteration order — random in a way
// that is not the seed's, and so not reproducible in a replay.
func metronomeMove(dex *domain.Dex, rng *RNG) (domain.Move, bool) {
	ids := make([]string, 0, len(dex.Moves))
	for id, mv := range dex.Moves {
		if mv.HasFlag("metronome") && callable(mv) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return domain.Move{}, false
	}
	sort.Strings(ids)
	return dex.Moves[ids[rng.IntN(len(ids))]], true
}

// mirrorMove reflects the move the *target* last used, gated on the `mirror`
// flag — 447 of the curated set carry it.
//
// It reads the target's own last-move register rather than the battle's, which
// is the difference between it and Copycat: Mirror Move answers a particular
// opponent, Copycat answers the room.
func mirrorMove(dex *domain.Dex, foe *Pokemon) (domain.Move, bool) {
	if foe == nil {
		return domain.Move{}, false
	}
	mv, ok := dex.Moves[foe.Volatiles.LastMoveID]
	if !ok || !mv.HasFlag("mirror") || !callable(mv) {
		return domain.Move{}, false
	}
	return mv, true
}

// meFirstMove pre-empts the attack the target still has queued this turn, at
// one and a half times the power.
//
// It is the one caller that is not really a call: canon reads the *queue*, so
// the move has to still be pending. foeQueuedAttack is already exactly that
// question — Sucker Punch and Upper Hand ask it — and it answers false for a
// target that has already moved, is switching, is recharging, or picked a
// status move, which is four of Me First's five refusals. The fifth is the
// fail-me-first flag, which the moves that would answer a pre-emption with a
// pre-emption all carry.
func meFirstMove(dex *domain.Dex, s *BattleState, side int, foeAction Action, foeMoved bool) (domain.Move, bool) {
	mv, ok := foeQueuedAttack(dex, s, side, foeAction, foeMoved)
	if !ok || mv.HasFlag("fail-me-first") || !callable(mv) {
		return domain.Move{}, false
	}
	// The boost rides on the move rather than on a volatile, because the
	// substitution means there is only ever one move in flight to put it on.
	mv.Power = mv.Power * 3 / 2
	return mv, true
}

// --- Mimic ---
//
// Mimic is filed with the callers upstream and in the denylist, and it does not
// call anything. It overwrites its own slot with the move the target last used,
// which the user then owns for as long as it stays out.

// applyMimic is the onHit for mimic. Fails when the target has used nothing,
// when what it used is flagged fail-mimic, when the user already knows it, or
// when the user has no Mimic slot to overwrite — canon's four refusals, in
// canon's order.
//
// The copy arrives at the copied move's own full PP, which is modern canon:
// the five-PP rule people remember is from the handheld games and Showdown does
// not model it.
func applyMimic(dex *domain.Dex, s *BattleState, side int, log *[]LogLine) {
	user, foe := s.Active(side), s.Active(1-side)
	fail := func() {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
	}
	if foe == nil {
		fail()
		return
	}
	mv, ok := dex.Moves[foe.Volatiles.LastMoveID]
	if !ok || mv.HasFlag("fail-mimic") {
		fail()
		return
	}
	slot := -1
	for i, sl := range user.Moves {
		if sl.MoveID == mv.ID {
			fail() // already known
			return
		}
		if sl.MoveID == "mimic" {
			slot = i
		}
	}
	if slot < 0 {
		fail()
		return
	}
	rememberBaseMoves(user)
	user.Moves[slot] = MoveSlot{MoveID: mv.ID, PP: mv.PP, MaxPP: mv.PP}
	*log = append(*log, LogLine{
		Type: "mimic", Side: side,
		Text: user.Name + " learned " + mv.Name + "!",
	})
}

// rememberBaseMoves snapshots the move list before Mimic overwrites a slot, so
// switching out puts the real one back. The same first-writer-wins memo as
// BaseAbility, BaseStats and BaseTypes, restored in the same place; canon gets
// the revert from clearVolatile re-reading baseMoveSlots.
//
// The slice is copied rather than aliased. Sharing the backing array would make
// the memo change every time the slot did, which is the one thing a memo must
// not do.
func rememberBaseMoves(p *Pokemon) {
	if p.BaseMoves != nil {
		return
	}
	snap := make([]MoveSlot, len(p.Moves))
	copy(snap, p.Moves)
	p.BaseMoves = snap
}
