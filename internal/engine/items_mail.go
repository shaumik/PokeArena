package engine

// items_mail.go is a marker rather than a mechanic. Mail does nothing in
// battle; the whole of its upstream entry is an onTakeItem that refuses to let
// the item go:
//
//	onTakeItem(item, source) {
//		if (!this.activeMove) return false;
//		if (this.activeMove.id !== 'knockoff' && this.activeMove.id !== 'thief' &&
//			this.activeMove.id !== 'covet') return false;
//	}
//
// Note the shape: a *deny*-list is not what it is. Two refusals, and the first
// one carries most of the weight — no active move at all means no, which is why
// an ability that lifts an item off a holder (Magician, Pickpocket) cannot take
// Mail either. Only the three named moves get through, and everything else
// bounces: Fling, Trick, Switcheroo, Corrosive Gas, Bug Bite.
//
// That makes it the only item in this engine whose removability depends on
// *which* move is asking, which is why itemIsRemovable grew a parameter for it
// rather than gaining another predicate alongside Sticky Hold's.
//
// It is registered with a Desc and no hooks on purpose. An item that does
// nothing would ordinarily be an inert hold and a coverage failure — see the
// drives, which are declined for exactly that — but Mail is not inert: the
// refusal below is its behavior, it is simply expressed in a predicate rather
// than in the Item struct's hook surface.

const ItemMail ItemKind = "mail"

func init() {
	registerItem(&Item{
		Kind: ItemMail,
		Name: "Mail",
		Desc: "Cannot be taken from the holder except by Knock Off, Thief or Covet.",
	})
}

// mailTakeableBy are the three moves upstream lets through by name.
var mailTakeableBy = map[string]bool{
	"knock-off": true,
	"thief":     true,
	"covet":     true,
}

// mailRefusesRemovalBy reports whether the holder's Mail blocks a removal
// attempt from byMove. An empty byMove means the attempt is not a move at all
// — canon's `if (!this.activeMove) return false` — and Mail refuses that too.
func mailRefusesRemovalBy(p *Pokemon, byMove string) bool {
	if p == nil || p.Item != ItemMail {
		return false
	}
	return !mailTakeableBy[byMove]
}
