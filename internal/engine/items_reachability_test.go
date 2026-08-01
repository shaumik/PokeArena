package engine

import (
	"strings"
	"testing"
)

// items_reachability_test.go answers a question none of the other item tests
// ask: does each catalog item ever actually change a battle?
//
// The behavior tests drive each item's hook directly, so they prove the hook is
// correct given that it runs — not that anything in a real battle ever runs it.
// A dispatcher deleted, a registration dropped, a trigger predicate narrowed to
// a condition the engine can no longer produce: all of those leave every
// existing test green while the item silently becomes a cosmetic entry in the
// picker. That is a user-visible lie (the builder offers it, the battle ignores
// it) and it is invisible to structural coverage checks, which only compare the
// catalog against the registry.
//
// A 49k-battle random differential sweep covers most of the catalog on its own,
// but 19 items never fire under random play — they need a species (Farfetch'd,
// Marowak, Chansey), a move type, a weather setter, a hazard or a binding move
// that random rosters rarely roll together. Those are exactly the items most
// likely to rot unnoticed, so each gets the scenario it needs, spelled out.
//
// The assertion is deliberately weak — "the battle came out different" — because
// a strong one would duplicate the per-item behavior tests and would have to be
// rewritten every time a damage formula moves. This test only asks whether the
// item is still wired to anything at all.
func TestEverySituationalItemStillChangesABattle(t *testing.T) {
	d := loadDex(t)

	type scenario struct {
		item     ItemKind
		why      string
		holder   int
		holderMv []string
		bench    int
		foe      int
		foeMv    []string
		turns    int
		switchAt []int
	}
	scenarios := []scenario{
		// Species-locked relics. Random rosters rarely include the one species
		// each of these keys on, so nothing else exercises the dex-number gate.
		{ItemLeek, "Farfetch'd crit boost", 83, []string{"peck"}, 143, 143, []string{"splash"}, 6, nil},
		{ItemLuckyPunch, "Chansey crit boost", 113, []string{"pound"}, 143, 143, []string{"splash"}, 6, nil},
		{ItemThickClub, "Marowak Attack double", 105, []string{"bone-club"}, 143, 143, []string{"splash"}, 6, nil},

		// Type-boost items: the holder has to actually use that type.
		{ItemFairyFeather, "Fairy move boost", 36, []string{"moonblast"}, 143, 143, []string{"splash"}, 4, nil},
		{ItemPoisonBarb, "Poison move boost", 15, []string{"poison-jab"}, 143, 143, []string{"splash"}, 4, nil},

		// Resist berries: the holder has to *take* a super-effective hit of
		// that type, which needs a specific attacker on the other side.
		{ItemKebiaBerry, "halves a super-effective Poison hit", 36, []string{"splash"}, 143, 15, []string{"sludge-bomb"}, 4, nil},
		{ItemRoseliBerry, "halves a super-effective Fairy hit", 149, []string{"splash"}, 143, 36, []string{"moonblast"}, 4, nil},

		// Duration extenders: set the condition, then outlast its default
		// duration so the extra turns are observable.
		{ItemDampRock, "extends rain", 9, []string{"rain-dance", "splash"}, 143, 143, []string{"splash"}, 12, nil},
		{ItemHeatRock, "extends sun", 3, []string{"sunny-day", "splash"}, 143, 143, []string{"splash"}, 12, nil},
		{ItemIcyRock, "extends hail", 9, []string{"hail", "splash"}, 143, 143, []string{"splash"}, 12, nil},
		{ItemSmoothRock, "extends sandstorm", 6, []string{"sandstorm", "splash"}, 143, 143, []string{"splash"}, 12, nil},
		{ItemLightClay, "extends screens", 3, []string{"reflect", "splash"}, 143, 143, []string{"tackle"}, 12, nil},
		{ItemTerrainExtender, "extends terrain", 3, []string{"grassy-terrain", "splash"}, 143, 143, []string{"splash"}, 12, nil},

		// Weather/powder defenses.
		// Defender side: canon's onWeatherModifyDamage reads the *defender's*
		// effectiveWeather, so the umbrella keeps a rain-boosted Water hit off
		// its holder rather than damping the holder's own attack.
		{ItemUtilityUmbrella, "takes an unboosted hit in rain", 143, []string{"splash"}, 9, 9, []string{"rain-dance", "water-gun"}, 6, nil},
		{ItemSafetyGoggles, "refuses a powder move", 143, []string{"splash"}, 9, 12, []string{"sleep-powder"}, 6, nil},

		// Partial-trap tuning, set by the holder.
		{ItemBindingBand, "raises partial-trap chip", 6, []string{"fire-spin"}, 143, 143, []string{"splash"}, 8, nil},
		{ItemGripClaw, "extends a partial trap", 6, []string{"fire-spin"}, 143, 143, []string{"splash"}, 8, nil},

		// The two that only show up when the holder tries to leave the field.
		// Shed Shell edits the legal-action set rather than the battle state,
		// so the holder has to actually attempt the switch the trap refuses.
		{ItemShedShell, "escapes a partial trap", 143, []string{"splash"}, 9, 6, []string{"fire-spin"}, 8, []int{2, 3, 4}},
		{ItemHeavyDutyBoots, "ignores entry hazards", 143, []string{"splash"}, 9, 28, []string{"stealth-rock", "splash"}, 8, []int{2, 3}},
	}

	// play runs a fixed script so the trigger is guaranteed rather than hoped
	// for: both sides use move 0, switching to move 1 from turn 2 where they
	// have one, and switch on the scripted turns. Returns the whole log.
	play := func(sc scenario, item ItemKind) string {
		s, err := NewBattle(d, "b", "H", []int{sc.holder, sc.bench}, "F", []int{sc.foe, 143}, 7)
		if err != nil {
			t.Fatalf("%s: new battle: %v", sc.item, err)
		}
		set := func(p *Pokemon, ids []string) {
			slots := make([]MoveSlot, 0, len(ids))
			for _, id := range ids {
				if _, ok := d.Moves[id]; !ok {
					t.Skipf("%s: move %q is not in the curated set", sc.item, id)
				}
				slots = append(slots, MoveSlot{MoveID: id, PP: 32, MaxPP: 32})
			}
			p.Moves = slots
		}
		// Every slot, not just the leads: a scenario that switches must not
		// hand the bench mon its natural moveset and let it run the battle.
		for i := range s.Sides[0].Team {
			set(&s.Sides[0].Team[i], sc.holderMv)
		}
		for i := range s.Sides[1].Team {
			set(&s.Sides[1].Team[i], sc.foeMv)
		}
		s.Active(0).Item = item

		var b strings.Builder
		for turn := 0; turn < sc.turns; turn++ {
			if s.Phase == PhaseReplace {
				var acts [2]*Action
				for i := 0; i < 2; i++ {
					if !s.Replace[i] {
						continue
					}
					for j := range s.Sides[i].Team {
						if !s.Sides[i].Team[j].Fainted && j != s.Sides[i].Active {
							a := Action{Kind: ActionSwitch, Index: j}
							acts[i] = &a
							break
						}
					}
				}
				for _, l := range ResolveReplace(s, acts) {
					b.WriteString(l.Text)
					b.WriteByte('\n')
				}
				continue
			}
			if s.Phase != PhaseChoosing {
				break
			}
			idx := 0
			if turn > 0 && len(sc.holderMv) > 1 {
				idx = 1
			}
			a0 := Action{Kind: ActionMove, Index: idx}
			for _, w := range sc.switchAt {
				if w != turn {
					continue
				}
				want := 1 - s.Sides[0].Active
				if s.Sides[0].Team[want].Fainted {
					continue
				}
				// Ask the legality gate rather than assuming: a refused switch
				// is exactly what a trap produces, and the legal-action set is
				// the thing Shed Shell edits.
				for _, la := range LegalActionsDex(d, s, 0) {
					if la.Kind == ActionSwitch && la.Index == want {
						a0 = Action{Kind: ActionSwitch, Index: want}
					}
				}
			}
			idxFoe := 0
			if turn > 0 && len(sc.foeMv) > 1 {
				idxFoe = 1
			}
			for _, l := range ResolveTurn(d, s, [2]Action{a0, {Kind: ActionMove, Index: idxFoe}}) {
				b.WriteString(l.Text)
				b.WriteByte('\n')
			}
		}
		return b.String()
	}

	for _, sc := range scenarios {
		t.Run(string(sc.item), func(t *testing.T) {
			if sc.bench == sc.holder {
				t.Fatalf("scenario error: bench species must differ from the holder")
			}
			if play(sc, sc.item) == play(sc, ItemNone) {
				t.Errorf("%s (%s) played a byte-identical battle to holding nothing — "+
					"it is registered and cataloged but no longer wired to anything",
					sc.item, sc.why)
			}
		})
	}
}
