package engine

import (
	"fmt"
	"strings"

	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/specs"
)

// lockrestrict.go owns the volatiles that restrict which move the holder
// may pick: Disable (bans one move), Encore (forces one move), Taunt
// (blocks status), Torment (blocks the same move twice in a row),
// Imprison (blocks the foe from using shared moves), Embargo (suppresses
// the holder's item). The first five gate at two
// sites: LegalActions filters disallowed slots for read paths (AI,
// picker, MCP), executeMove re-checks at resolution time so a controller
// that bypasses LegalActions still trips a fail line. Embargo is the odd
// one out — it restricts nothing about move choice; it lives here for the
// timer shape, and the suppression itself is enforced by itemSuppressed
// on the item-lookup path. Disable / Encore /
// Taunt / Embargo are counter-down timers; Torment / Imprison persist
// until switch-out.

func init() {
	specs.RegisterVolatile("disable")
	specs.RegisterVolatile("encore")
	specs.RegisterVolatile("taunt")
	specs.RegisterVolatile("torment")
	specs.RegisterVolatile("imprison")
	specs.RegisterVolatile("embargo")
	specs.RegisterVolatile("healblock")
	registerVolatile("healblock", applyHealBlockVolatile)
	registerVolatile("disable", applyDisableVolatile)
	registerVolatile("encore", applyEncoreVolatile)
	registerVolatile("taunt", applyTauntVolatile)
	registerVolatile("torment", applyTormentVolatile)
	registerVolatile("imprison", applyImprisonVolatile)
	registerVolatile("embargo", applyEmbargoVolatile)
}

const (
	defaultDisableTurns = 4
	defaultEncoreTurns  = 3
	defaultTauntTurns   = 3
	defaultEmbargoTurns = 5
)

// DisableState locks one of the target's move slots out for a few turns.
// MoveID is the slug of the disabled move; MoveName the display string
// captured at apply time so logs read "Tackle" rather than "tackle".
type DisableState struct {
	MoveID   string `json:"move_id"`
	MoveName string `json:"move_name"`
	Turns    int    `json:"turns"`
}

// EncoreState forces the target to repeat one move. Same shape as
// DisableState — MoveID is the locked slug and MoveName the display.
type EncoreState struct {
	MoveID   string `json:"move_id"`
	MoveName string `json:"move_name"`
	Turns    int    `json:"turns"`
}

// TauntState blocks status-category moves for a few turns. Single
// counter, no payload — categorization happens at the gate.
type TauntState struct {
	Turns int `json:"turns"`
}

// EmbargoState blocks item use for 5 turns. itemSuppressed reads it, so
// while it is set itemOf returns nil and the holder's item does nothing —
// no Leftovers tick, no Choice lock, no berry. The item is still *held*:
// it can be knocked off or stolen, and it still fills the slot for
// Acrobatics and Unburden.
type EmbargoState struct {
	Turns int `json:"turns"`
}

// ImprisonState lives on the imprisoner (Imprison targets self) and
// snapshots the move IDs shared with the foe at cast time. The foe's
// side queries this list to drop matching slots; the imprisoner side
// is unaffected. Indefinite — cleared only on switch-out.
type ImprisonState struct {
	MoveIDs []string `json:"move_ids"`
}

// applyDisableVolatile picks the target's last move and bans it for 4
// turns. Fails if the target has no last move, the last move isn't in
// its current slots, or Disable is already active.
func applyDisableVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Disable != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	last := p.Volatiles.LastMoveID
	if last == "" || !knowsMove(p, last) {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	name := p.Volatiles.LastMoveName
	if name == "" {
		name = prettyMoveName(last)
	}
	p.Volatiles.Disable = &DisableState{MoveID: last, MoveName: name, Turns: defaultDisableTurns}
	*log = append(*log, LogLine{
		Type: "disable", Side: side,
		Text: fmt.Sprintf("%s's %s was disabled!", p.Name, name),
	})
}

// applyEncoreVolatile forces the target into using its last move for 3
// turns. Fails if no last move, the move can't be encored, or it has no
// PP left.
func applyEncoreVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Encore != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	last := p.Volatiles.LastMoveID
	if last == "" || encoreDenylisted(last) || !knowsMoveWithPP(p, last) {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	name := p.Volatiles.LastMoveName
	if name == "" {
		name = prettyMoveName(last)
	}
	p.Volatiles.Encore = &EncoreState{MoveID: last, MoveName: name, Turns: defaultEncoreTurns}
	*log = append(*log, LogLine{
		Type: "encore", Side: side,
		Text: fmt.Sprintf("%s received an encore!", p.Name),
	})
}

// applyTauntVolatile sets a 3-turn block on status-category moves.
// Reapply while already tainted is a no-op (canon — the move fails).
func applyTauntVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if abilityBlocksTaunt(p) {
		revealAbility(p)
		*log = append(*log, LogLine{
			Type: "ability", Side: side,
			Text: fmt.Sprintf("%s's Oblivious keeps it from being taunted!", p.Name),
		})
		return
	}
	if p.Volatiles.Taunt != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.Taunt = &TauntState{Turns: defaultTauntTurns}
	*log = append(*log, LogLine{
		Type: "taunt", Side: side,
		Text: fmt.Sprintf("%s fell for the taunt!", p.Name),
	})
}

// applyHealBlockVolatile stops the target healing, and stops it using a healing
// move at all. Five turns normally; two from Psychic Noise, which is the only
// source in this dataset — canon puts that split in a durationCallback keyed on
// the source move, which is why the handler reads the move it came from.
//
// The move ban is the half worth stating: Gen 6+ refuses a heal-flagged move
// outright rather than letting it resolve and heal nothing, so a blocked
// Recover costs the user its turn but not its PP.
func applyHealBlockVolatile(p *Pokemon, side int, source domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.HealBlock != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	turns := defaultHealBlockTurns
	if source.ID == "psychic-noise" {
		turns = psychicNoiseHealBlockTurns
	}
	p.Volatiles.HealBlock = &HealBlockState{Turns: turns}
	*log = append(*log, LogLine{
		Type: "healblock", Side: side,
		Text: fmt.Sprintf("%s was prevented from healing!", p.Name),
	})
}

// applyTormentVolatile sets an indefinite block on using the same move
// twice in a row. Cleared only on switch-out (Volatiles wipe). Bag is a
// bool — the comparison with LastMoveID happens at the gate.
func applyTormentVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Torment {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.Torment = true
	*log = append(*log, LogLine{
		Type: "torment", Side: side,
		Text: fmt.Sprintf("%s was subjected to torment!", p.Name),
	})
}

// applyImprisonVolatile snapshots the user's moves shared with the foe.
// Fails if no moves overlap or Imprison is already active. The list is
// queried from the foe's side at action time — the imprisoner itself is
// unaffected.
func applyImprisonVolatile(p *Pokemon, side int, _ domain.Move, s *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Imprison != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	foe := s.Active(1 - side)
	foeKnows := map[string]bool{}
	for _, m := range foe.Moves {
		foeKnows[m.MoveID] = true
	}
	var shared []string
	for _, m := range p.Moves {
		if foeKnows[m.MoveID] {
			shared = append(shared, m.MoveID)
		}
	}
	if len(shared) == 0 {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.Imprison = &ImprisonState{MoveIDs: shared}
	*log = append(*log, LogLine{
		Type: "imprison", Side: side,
		Text: fmt.Sprintf("%s sealed any moves its opponent shares with it!", p.Name),
	})
}

// applyEmbargoVolatile sets a 5-turn item-use block. The suppression itself
// lives in itemSuppressed, which every item lookup goes through; here we
// only arm the timer and emit the start log.
func applyEmbargoVolatile(p *Pokemon, side int, _ domain.Move, _ *BattleState, _ *RNG, log *[]LogLine) {
	if p.Volatiles.Embargo != nil {
		*log = append(*log, LogLine{Type: "fail", Side: side, Text: "But it failed!"})
		return
	}
	p.Volatiles.Embargo = &EmbargoState{Turns: defaultEmbargoTurns}
	*log = append(*log, LogLine{
		Type: "embargo", Side: side,
		Text: fmt.Sprintf("%s can't use items!", p.Name),
	})
}

// tickLockRestrict decrements the timer volatiles on side's active and
// clears any that expire. Called from ResolveTurn's end-of-turn block.
// Torment and Imprison are indefinite — not touched here.
//
// Encore also breaks early if the encored move is exhausted of PP
// (canonical): if knowsMoveWithPP returns false before the counter
// would otherwise expire, the encore ends with the same log line.
func tickLockRestrict(s *BattleState, side int, log *[]LogLine) {
	p := s.Active(side)
	if d := p.Volatiles.Disable; d != nil {
		d.Turns--
		if d.Turns <= 0 {
			p.Volatiles.Disable = nil
			*log = append(*log, LogLine{
				Type: "disable", Side: side,
				Text: fmt.Sprintf("%s's move is no longer disabled.", p.Name),
			})
		}
	}
	if e := p.Volatiles.Encore; e != nil {
		e.Turns--
		expired := e.Turns <= 0 || !knowsMoveWithPP(p, e.MoveID)
		if expired {
			p.Volatiles.Encore = nil
			*log = append(*log, LogLine{
				Type: "encore", Side: side,
				Text: fmt.Sprintf("%s's encore ended.", p.Name),
			})
		}
	}
	if h := p.Volatiles.HealBlock; h != nil {
		h.Turns--
		if h.Turns <= 0 {
			p.Volatiles.HealBlock = nil
			*log = append(*log, LogLine{
				Type: "healblock", Side: side,
				Text: fmt.Sprintf("%s's Heal Block wore off!", p.Name),
			})
		}
	}
	if t := p.Volatiles.Taunt; t != nil {
		t.Turns--
		if t.Turns <= 0 {
			p.Volatiles.Taunt = nil
			*log = append(*log, LogLine{
				Type: "taunt", Side: side,
				Text: fmt.Sprintf("%s shook off the taunt.", p.Name),
			})
		}
	}
	if eb := p.Volatiles.Embargo; eb != nil {
		eb.Turns--
		if eb.Turns <= 0 {
			p.Volatiles.Embargo = nil
			*log = append(*log, LogLine{
				Type: "embargo", Side: side,
				Text: fmt.Sprintf("%s can use items again.", p.Name),
			})
		}
	}
}

// lockRestrictBlocksMove reports whether the user's chosen move is
// blocked by any lock/restrict volatile (own or foe-imposed). Returns a
// canonical-shaped fail string the caller logs as a "cant" line. Called
// from executeMove right after choosePP and before announceMove so the
// move never lands and PP is still deducted (Disable/Taunt/etc. canon —
// the attempt costs a turn but the move doesn't resolve).
func lockRestrictBlocksMove(s *BattleState, side int, m domain.Move) (string, bool) {
	atk := s.Active(side)
	if d := atk.Volatiles.Disable; d != nil && d.MoveID == m.ID {
		return fmt.Sprintf("%s's %s is disabled!", atk.Name, d.MoveName), true
	}
	if e := atk.Volatiles.Encore; e != nil && e.MoveID != m.ID && m.ID != "" {
		return fmt.Sprintf("%s must use %s!", atk.Name, e.MoveName), true
	}
	if atk.Volatiles.Taunt != nil && m.Category == domain.CatStatus {
		return fmt.Sprintf("%s can't use %s after the taunt!", atk.Name, m.Name), true
	}
	if atk.Volatiles.HealBlock != nil && m.HasFlag("heal") {
		return fmt.Sprintf("%s can't use %s after the Heal Block!", atk.Name, m.Name), true
	}
	if atk.Volatiles.Torment && atk.Volatiles.LastMoveID == m.ID && m.ID != "" {
		return fmt.Sprintf("%s can't use the same move twice in a row!", atk.Name), true
	}
	if foeImp := s.Active(1 - side).Volatiles.Imprison; foeImp != nil && m.ID != "" {
		for _, id := range foeImp.MoveIDs {
			if id == m.ID {
				return fmt.Sprintf("%s can't use the sealed %s!", atk.Name, m.Name), true
			}
		}
	}
	return "", false
}

// lockRestrictBlocksSlot mirrors lockRestrictBlocksMove for the
// LegalActions read path: returns true if slot would be refused at
// resolution. Operates off the move ID alone (no dex lookup needed for
// Disable/Encore/Torment/Imprison). Taunt checks category and so needs
// the dex — handled by statusBlockedByTaunt below.
func lockRestrictBlocksSlot(s *BattleState, side, slot int) bool {
	atk := s.Active(side)
	if slot < 0 || slot >= len(atk.Moves) {
		return false
	}
	moveID := atk.Moves[slot].MoveID
	if d := atk.Volatiles.Disable; d != nil && d.MoveID == moveID {
		return true
	}
	if e := atk.Volatiles.Encore; e != nil && e.MoveID != moveID {
		return true
	}
	if atk.Volatiles.Torment && atk.Volatiles.LastMoveID == moveID && moveID != "" {
		return true
	}
	if foeImp := s.Active(1 - side).Volatiles.Imprison; foeImp != nil {
		for _, id := range foeImp.MoveIDs {
			if id == moveID {
				return true
			}
		}
	}
	return false
}

// statusBlockedByTaunt reports whether slot is a status-category move
// the holder is currently taunted out of. Split from lockRestrictBlocksSlot
// because Taunt is the only restrict-vol that needs the dex (every
// other gate works off the ID alone).
func statusBlockedByTaunt(dex *domain.Dex, atk *Pokemon, slot int) bool {
	if atk.Volatiles.Taunt == nil {
		return false
	}
	if slot < 0 || slot >= len(atk.Moves) {
		return false
	}
	m, ok := dex.Moves[atk.Moves[slot].MoveID]
	if !ok {
		return false
	}
	return m.Category == domain.CatStatus
}

// knowsMove reports whether the holder currently has the given move in
// any slot. Used by Disable's apply guard.
func knowsMove(p *Pokemon, moveID string) bool {
	for i := range p.Moves {
		if p.Moves[i].MoveID == moveID {
			return true
		}
	}
	return false
}

// knowsMoveWithPP reports whether the holder has the given move with
// remaining PP. Used by Encore's apply guard (Encore fails on a PP-0
// move) and by tickLockRestrict for the break-on-PP-exhaust path.
func knowsMoveWithPP(p *Pokemon, moveID string) bool {
	for i := range p.Moves {
		if p.Moves[i].MoveID == moveID && p.Moves[i].PP > 0 {
			return true
		}
	}
	return false
}

// encoreDenylisted is the canonical "can't be encored" set: Encore
// itself plus the calls-another-move family (Mimic, Sketch, Mirror Move,
// ...). Most are also data-sync denylisted so they can't appear as a
// user's last move today, but keeping the check here means a future
// un-denylisting won't accidentally lock the user into a re-roll move.
func encoreDenylisted(moveID string) bool {
	switch moveID {
	case "", "encore", "mimic", "mirror-move", "copycat", "metronome", "sketch", "assist", "me-first":
		return true
	}
	return false
}

// prettyMoveName converts a kebab-case slug ("fire-blast") to its
// title-cased display form ("Fire Blast"). Used by Disable's log when
// no captured name is on hand (defensive fallback — executeMove
// snapshots m.Name into LastMoveName, so the slug path should never
// actually fire in production).
func prettyMoveName(slug string) string {
	if slug == "" {
		return ""
	}
	parts := strings.Split(slug, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		rs := []rune(p)
		if rs[0] >= 'a' && rs[0] <= 'z' {
			rs[0] -= 32
		}
		parts[i] = string(rs)
	}
	return strings.Join(parts, " ")
}

// defaultHealBlockTurns / psychicNoiseHealBlockTurns are canon's two durations.
// Heal Block itself is not in this dataset — no curated species learns it — so
// the five-turn figure is unreachable today and is here because the split is
// the rule, not because a move needs it.
const (
	defaultHealBlockTurns      = 5
	psychicNoiseHealBlockTurns = 2
)

// HealBlockState is the Heal Block countdown. Turns ticks at end of turn with
// the other lock-restrict timers.
type HealBlockState struct {
	Turns int `json:"turns"`
}

// healBlocked reports whether p is currently barred from gaining HP.
//
// Consulted at every heal site rather than at one, because five of them do not
// go through healPokemon: the item heals, the ability heals (Rain Dish, Ice
// Body, Dry Skin), Regenerator, Grassy Terrain, the Leech Seed drain and the
// ring heals all add HP directly. Guarding only the choke point would leave a
// Heal Blocked Pokémon quietly topping itself up on Leftovers.
func healBlocked(p *Pokemon) bool {
	return p != nil && p.Volatiles.HealBlock != nil
}
