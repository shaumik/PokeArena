package engine

// items_mirrorherb.go copies the *foe's* stat boosts onto its holder, once,
// and then goes away.
//
// The mechanism is Eject Pack's — accumulate now, spend later, because the
// boost that triggers it lands in the middle of somebody else's move and the
// field is in no state to do anything about it there — but the bookkeeping runs
// the other way round, and that is the part worth explaining.
//
// Upstream hangs the accumulator on the *herb*: onFoeAfterBoost walks the boost
// object and adds every positive entry into the item's own effectState. That
// needs the herb to be able to see a boost applied to somebody else, which in
// this engine it cannot: applyStages takes a Pokémon and a stat and has no
// BattleState, and threading one through the single write point every stage
// change in the engine goes through, to serve one item, is the wrong trade —
// the same trade abilitysuppression.go's header refuses for the same reason.
//
// So the accumulator lives on the Pokémon that *received* the boost instead,
// and the herb reads its opposite number's record at the moment it spends. The
// two are equivalent as long as the record is drained on the same schedule the
// herb is, which is what drainGainedBoosts guarantees: every firing point
// clears both sides whether or not a herb was there to copy them. A record that
// outlived its window would let a herb picked up two turns later copy a Swords
// Dance from the turn before last.
//
// Only positive entries are recorded. Canon's `if (boost[i]! > 0)` is the whole
// filter, and it is why a Superpower's own drops are not handed to the foe.

const ItemMirrorHerb ItemKind = "mirror-herb"

func init() {
	registerItem(&Item{
		Kind: ItemMirrorHerb,
		Name: "Mirror Herb",
		Desc: "Copies the opposing Pokémon's stat boosts onto the holder, then is used up.",
	})
}

// recordGainedBoost adds one positive stage change to the receiver's record.
// Called from applyStages with the delta that actually landed, so a boost
// clamped by the +6 ceiling contributes only what it moved — which is canon,
// since its boost object is trimmed the same way before onFoeAfterBoost sees it.
func recordGainedBoost(p *Pokemon, stat string, gained int) {
	if p == nil || gained <= 0 {
		return
	}
	if p.Volatiles.GainedBoosts == nil {
		p.Volatiles.GainedBoosts = &Stages{}
	}
	if ptr := stagePtrIn(p.Volatiles.GainedBoosts, stat); ptr != nil {
		*ptr += gained
	}
}

// stagePtrIn is stagePtr against a bare Stages rather than a Pokémon's own.
func stagePtrIn(st *Stages, stat string) *int {
	switch stat {
	case "attack":
		return &st.Atk
	case "defense":
		return &st.Def
	case "spatk":
		return &st.SpA
	case "spdef":
		return &st.SpD
	case "speed":
		return &st.Spe
	case "accuracy":
		return &st.Acc
	case "evasion":
		return &st.Eva
	}
	return nil
}

// boostEntry is one stat and the amount to raise it by.
type boostEntry struct {
	stat  string
	delta int
}

// gainedBoostList flattens a record into applyStages' argument order, skipping
// the zeroes. Ordered rather than ranged over a map so the log reads the same
// on every run.
func gainedBoostList(st *Stages) []boostEntry {
	all := []boostEntry{
		{"attack", st.Atk},
		{"defense", st.Def},
		{"spatk", st.SpA},
		{"spdef", st.SpD},
		{"speed", st.Spe},
		{"accuracy", st.Acc},
		{"evasion", st.Eva},
	}
	out := make([]boostEntry, 0, len(all))
	for _, e := range all {
		if e.delta > 0 {
			out = append(out, e)
		}
	}
	return out
}

// fireMirrorHerbs pays out both sides' herbs and then clears both records.
//
// Called from the same three points Eject Pack uses. The clear happens
// unconditionally — see the file header for why a record must not outlive its
// window.
func fireMirrorHerbs(s *BattleState, log *[]LogLine) {
	for side := 0; side < 2; side++ {
		fireMirrorHerb(s, side, log)
	}
	drainGainedBoosts(s)
}

func fireMirrorHerb(s *BattleState, side int, log *[]LogLine) {
	p, foe := s.Active(side), s.Active(1-side)
	if p == nil || foe == nil || p.Fainted || p.HP <= 0 {
		return
	}
	it := itemOf(p)
	if it == nil || it.Kind != ItemMirrorHerb {
		return
	}
	gained := foe.Volatiles.GainedBoosts
	if gained == nil {
		return
	}
	list := gainedBoostList(gained)
	if len(list) == 0 {
		return
	}
	consumeItemAnnounced(p, side, it, log)
	for _, e := range list {
		// applyStages, not applyStagesFromFoe: this is the holder's own item
		// raising its own stats, so nothing that guards against a foe's drops
		// has anything to say — and they are raises regardless.
		applyStages(p, side, e.stat, e.delta, log)
	}
}

// drainGainedBoosts clears both actives' records.
func drainGainedBoosts(s *BattleState) {
	for side := 0; side < 2; side++ {
		if p := s.Active(side); p != nil {
			p.Volatiles.GainedBoosts = nil
		}
	}
}
