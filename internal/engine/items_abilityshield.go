package engine

import "fmt"

// items_abilityshield.go is one item with five integration points, which is why
// it gets a file rather than a registry entry beside its neighbors.
//
// Ability Shield says "the holder's ability cannot be interfered with", and
// upstream spells that out in five separate places because there are five
// separate ways to interfere. Its own data/items.ts entry carries only two of
// them — `ignoreKlutz` and an `onSetAbility` — and the other three are comments
// pointing somewhere else:
//
//	Neutralizing Gas   Pokemon#ignoringAbility, sim/pokemon.ts
//	Mold Breaker       Battle#suppressingAbility, sim/battle.ts
//	Gastro Acid        neither — see below, it falls out of ignoringAbility's order
//
// Each is mirrored here onto whichever engine function already owns that
// question, so there is exactly one place per mechanic that knows about the
// shield and no dispatcher had to grow a special case.
//
// The interesting part is that the shield is *not* uniformly stronger than the
// things it blocks. Canon's ignoringAbility reads:
//
//	if (this.volatiles['gastroacid']) return true;
//	if (this.hasItem('Ability Shield') || this.ability === 'neutralizinggas') return false;
//	for (... a neutralizinggas holder is active ...) return true;
//
// so a Gastro Acid that has already landed beats a shield acquired afterwards,
// while a Neutralizing Gas already filling the field does not. That asymmetry
// is not a quirk to paper over — it is the difference between a sticky volatile
// on the victim and a live read of the field, and the ported suite has a
// matched pair of cases asserting each half. Blocking Gastro Acid at the point
// it would *apply* the volatile, and exempting from the gas at the point the
// field is *read*, is what reproduces both without either being special-cased.

// ItemAbilityShield is the slug. Named as a constant because four other files
// ask about this one item by identity, and a typo'd string literal would fail
// silently — the same reasoning AbilityNeutralizingGas records next door.
const ItemAbilityShield ItemKind = "ability-shield"

func init() {
	registerItem(&Item{
		Kind: ItemAbilityShield,
		Name: "Ability Shield",
		Desc: "The holder's Ability cannot be suppressed, ignored, or changed.",
		// Upstream's ignoreKlutz. The shield is one of the handful of items
		// that work in a Klutz holder's hands — which matters here more than
		// for most, since a Klutz holder's own ability is the thing being
		// protected.
		IgnoreKlutz: true,
	})
}

// holdsAbilityShield reports whether p is protected right now.
//
// Goes through itemOf, so Embargo and Magic Room both switch the shield off:
// canon's hasItem ends in `return !this.ignoringItem()`, and those two are
// unconditional there. Klutz is the exception, and it is handled inside
// itemSuppressed by the IgnoreKlutz flag rather than by a second predicate
// here.
//
// Tolerates a nil Pokémon because two of its callers are field-level questions
// that can be asked about an empty slot.
func holdsAbilityShield(p *Pokemon) bool {
	it := itemOf(p)
	return it != nil && it.Kind == ItemAbilityShield
}

// abilityShieldBlocks is the shared refusal for the three paths that would
// take the holder's ability away from it: the ability-setting moves, Trace, and
// Gastro Acid's volatile.
//
// Canon's onSetAbility adds `-block` and returns null, and `null` is not
// `false`: it stops the write without failing the move. A Worry Seed into a
// shield holder is a move that resolved and did nothing, not a move that
// missed, and Worry Seed says so by returning setAbility's `false | null`
// verbatim. Every caller here mirrors that — the block line is printed and no
// "But it failed!" follows.
//
// The item is revealed, because the block is visible to both players. The
// source's ability is not, which is a small divergence: canon's handler also
// emits `-ability` for the interfering ability except when it is Trace, and
// this engine's reveal bookkeeping is driven from the ability side rather than
// from the blocker, so the two Trace cases in the ported suite are the only
// ones that would notice and both assert the *absence* of an effect.
func abilityShieldBlocks(p *Pokemon, side int, log *[]LogLine) bool {
	if !holdsAbilityShield(p) {
		return false
	}
	*log = append(*log, LogLine{
		Type: "block", Side: side,
		Text: fmt.Sprintf("%s's Ability Shield protected its Ability!", p.Name),
	})
	return true
}

// abilityShieldBlocksVolatile reports whether the shield refuses a volatile
// aimed at its holder.
//
// One entry, and a list rather than an equality test because the question is
// "which volatiles are really ability removal wearing a volatile's clothes",
// and gastroacid is the only one today only because it is the only one this
// dex has. Simple Beam and Worry Seed go through setAbility and are handled on
// the write path; Mummy and Wandering Spirit would too.
func abilityShieldBlocksVolatile(p *Pokemon, volatileName string) bool {
	return volatileName == "gastroacid" && holdsAbilityShield(p)
}
