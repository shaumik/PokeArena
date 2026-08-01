package engine

import (
	"testing"
)

// items_reactive_test.go covers the event-reaction family plus the accuracy and
// turn-order items. The recurring risk here is a trigger that is too broad — an
// Absorb Bulb that answers any hit, a Blunder Policy that answers any failure —
// so every test pairs the firing case with the case that must be ignored.

// --- reactive stat boosts ---

func TestTypeReactBoostsAnswerOnlyTheirType(t *testing.T) {
	cases := []struct {
		item    ItemKind
		fires   string // move of the matching type
		ignores string
		read    func(*Stages) int
	}{
		{ItemAbsorbBulb, "water-gun", "body-slam", func(g *Stages) int { return g.SpA }},
		{ItemCellBattery, "thunderbolt", "body-slam", func(g *Stages) int { return g.Atk }},
		{ItemLuminousMoss, "water-gun", "body-slam", func(g *Stages) int { return g.SpD }},
		{ItemSnowball, "ice-beam", "body-slam", func(g *Stages) int { return g.Atk }},
	}
	d := loadDex(t)
	for _, tc := range cases {
		run := func(moveID string) *BattleState {
			s, err := NewBattle(d, "b", "Holder", []int{143}, "Foe", []int{143}, 5)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
			s.Active(0).Item = tc.item
			s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			s.Active(1).Moves = []MoveSlot{{MoveID: moveID, PP: 25, MaxPP: 25}}
			splashTurn(d, s)
			return s
		}
		t.Run(string(tc.item), func(t *testing.T) {
			s := run(tc.fires)
			if got := tc.read(&s.Active(0).Stages); got != 1 {
				t.Errorf("stage after a matching hit = %d, want 1", got)
			}
			if s.Active(0).Item != ItemNone {
				t.Errorf("%s not consumed", tc.item)
			}
		})
		t.Run(string(tc.item)+"/wrong-type", func(t *testing.T) {
			s := run(tc.ignores)
			if got := tc.read(&s.Active(0).Stages); got != 0 {
				t.Errorf("%s fired on a %s move: stage = %d", tc.item, tc.ignores, got)
			}
			if s.Active(0).Item != tc.item {
				t.Errorf("%s consumed on the wrong type", tc.item)
			}
		})
	}
}

// TestWeaknessPolicyAnswersSuperEffectiveOnly, and raises both offenses.
func TestWeaknessPolicyAnswersSuperEffectiveOnly(t *testing.T) {
	d := loadDex(t)
	run := func(holderDex, foeDex int, moveID string) *BattleState {
		s, err := NewBattle(d, "b", "Holder", []int{holderDex}, "Foe", []int{foeDex}, 5)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		s.Active(0).Item = ItemWeaknessPolicy
		s.Active(0).HP = s.Active(0).MaxHP // survive the hit
		s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Moves = []MoveSlot{{MoveID: moveID, PP: 15, MaxPP: 15}}
		splashTurn(d, s)
		return s
	}
	// Venusaur (Grass/Poison) takes 2x Fire from Charizard.
	se := run(3, 6, "flamethrower")
	if se.Active(0).Stages.Atk != 2 || se.Active(0).Stages.SpA != 2 {
		t.Errorf("Weakness Policy stages = Atk %d / SpA %d, want 2 / 2",
			se.Active(0).Stages.Atk, se.Active(0).Stages.SpA)
	}
	if se.Active(0).Item != ItemNone {
		t.Errorf("Weakness Policy not consumed")
	}
	// Neutral hit: untouched.
	n := run(143, 143, "body-slam")
	if n.Active(0).Stages.Atk != 0 || n.Active(0).Stages.SpA != 0 {
		t.Errorf("Weakness Policy fired on a neutral hit")
	}
	if n.Active(0).Item != ItemWeaknessPolicy {
		t.Errorf("Weakness Policy consumed on a neutral hit")
	}
}

// TestThroatSprayAnswersSoundMovesOnly. The "and only when the move actually
// resolved" half of the contract lives in TestThroatSprayDoesNotFireThroughProtect.
func TestThroatSprayAnswersSoundMovesOnly(t *testing.T) {
	d := loadDex(t)
	run := func(moveID string) *BattleState {
		_, s := berryBattle(t, ItemThroatSpray)
		s.Active(0).Moves = []MoveSlot{{MoveID: moveID, PP: 20, MaxPP: 20}}
		splashTurn(d, s)
		return s
	}
	if _, ok := d.Moves["hyper-voice"]; !ok {
		t.Skip("hyper-voice not in the curated move set")
	}
	sound := run("hyper-voice")
	if sound.Active(0).Stages.SpA != 1 {
		t.Errorf("Throat Spray did not fire on a sound move: SpA = %d", sound.Active(0).Stages.SpA)
	}
	if sound.Active(0).Item != ItemNone {
		t.Errorf("Throat Spray not consumed")
	}
	quiet := run("body-slam")
	if quiet.Active(0).Stages.SpA != 0 {
		t.Errorf("Throat Spray fired on a non-sound move")
	}
	if quiet.Active(0).Item != ItemThroatSpray {
		t.Errorf("Throat Spray consumed on a non-sound move")
	}
}

// TestBlunderPolicyAnswersOnlyAMissableMiss: a move that cannot miss and was
// blocked some other way is not a blunder.
func TestBlunderPolicyAnswersOnlyAMissableMiss(t *testing.T) {
	d := loadDex(t)
	for seed := uint64(1); seed <= 60; seed++ {
		_, s := berryBattle(t, ItemBlunderPolicy)
		s.RNGState, s.Seed = seed, seed
		s.Active(0).Moves = []MoveSlot{{MoveID: "focus-blast", PP: 5, MaxPP: 5}}

		log := splashTurn(d, s)
		if !logHas(log, "attack missed") {
			continue
		}
		if got := s.Active(0).Stages.Spe; got != 2 {
			t.Fatalf("seed %d: Blunder Policy Speed = %d, want 2; log: %v", seed, got, log)
		}
		if s.Active(0).Item != ItemNone {
			t.Fatalf("seed %d: Blunder Policy not consumed", seed)
		}
		// And it must not fire on a landed move.
		_, hit := berryBattle(t, ItemBlunderPolicy)
		hit.Active(0).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
		splashTurn(d, hit)
		if hit.Active(0).Stages.Spe != 0 {
			t.Errorf("Blunder Policy fired on a move that landed")
		}
		return
	}
	t.Fatal("no miss occurred across 60 seeds — the fixture stopped exercising the path")
}

// --- herbs ---

// TestWhiteHerbRestoresOnlyLoweredStats: it must not touch boosts, and must
// stay in reserve when nothing is negative.
func TestWhiteHerbRestoresOnlyLoweredStats(t *testing.T) {
	d, s := berryBattle(t, ItemWhiteHerb)
	holder := s.Active(0)
	holder.Stages.Atk = 2  // a boost — must survive
	holder.Stages.Def = -2 // a drop — must be cleared
	holder.Stages.Spe = -1

	splashTurn(d, s)

	got := s.Active(0)
	if got.Stages.Def != 0 || got.Stages.Spe != 0 {
		t.Errorf("White Herb left drops in place: %+v", got.Stages)
	}
	if got.Stages.Atk != 2 {
		t.Errorf("White Herb cleared a boost: Atk = %d, want 2", got.Stages.Atk)
	}
	if got.Item != ItemNone {
		t.Errorf("White Herb not consumed")
	}
}

// TestWhiteHerbStaysInReserveWithNothingToFix.
func TestWhiteHerbStaysInReserveWithNothingToFix(t *testing.T) {
	d, s := berryBattle(t, ItemWhiteHerb)
	s.Active(0).Stages.Atk = 2 // boosts only

	splashTurn(d, s)

	if s.Active(0).Item != ItemWhiteHerb {
		t.Errorf("White Herb spent with no lowered stat to restore")
	}
	if s.Active(0).Stages.Atk != 2 {
		t.Errorf("White Herb touched a boost")
	}
}

// TestMentalHerbFreesTheHolder covers each restriction it lifts, and the
// nothing-to-fix case.
func TestMentalHerbFreesTheHolder(t *testing.T) {
	d := loadDex(t)
	for _, tc := range []struct {
		name string
		set  func(*Pokemon)
		ok   func(*Pokemon) bool
	}{
		{"attract", func(p *Pokemon) { p.Volatiles.Attract = true }, func(p *Pokemon) bool { return !p.Volatiles.Attract }},
		{"taunt", func(p *Pokemon) { p.Volatiles.Taunt = &TauntState{Turns: 3} }, func(p *Pokemon) bool { return p.Volatiles.Taunt == nil }},
		{"encore", func(p *Pokemon) { p.Volatiles.Encore = &EncoreState{MoveID: "splash", Turns: 3} }, func(p *Pokemon) bool { return p.Volatiles.Encore == nil }},
		{"disable", func(p *Pokemon) { p.Volatiles.Disable = &DisableState{MoveID: "splash", Turns: 4} }, func(p *Pokemon) bool { return p.Volatiles.Disable == nil }},
		{"torment", func(p *Pokemon) { p.Volatiles.Torment = true }, func(p *Pokemon) bool { return !p.Volatiles.Torment }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, s := berryBattle(t, ItemMentalHerb)
			holder := s.Active(0)
			tc.set(holder)
			splashTurn(d, s)
			if !tc.ok(s.Active(0)) {
				t.Errorf("Mental Herb did not lift %s", tc.name)
			}
			if s.Active(0).Item != ItemNone {
				t.Errorf("Mental Herb not consumed")
			}
		})
	}
	t.Run("nothing to fix", func(t *testing.T) {
		_, s := berryBattle(t, ItemMentalHerb)
		splashTurn(d, s)
		if s.Active(0).Item != ItemMentalHerb {
			t.Errorf("Mental Herb spent with no restriction to lift")
		}
	})
}

// --- flinch ---

// TestFlinchItemsAddAChanceButDoNotStack: King's Rock adds 10% to a move with
// no flinch of its own, and leaves a move that already flinches alone (doubling
// Iron Head's rate would be a far bigger item than this one).
func TestFlinchItemsAddAChanceButDoNotStack(t *testing.T) {
	d := loadDex(t)
	flinches := func(item ItemKind, moveID string) int {
		n := 0
		for seed := uint64(1); seed <= 250; seed++ {
			s, err := NewBattle(d, "b", "Attacker", []int{143}, "Target", []int{143}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			// The attacker must move first for a flinch to be observable.
			s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
			s.Active(0).Stages.Spe = 6
			s.Active(0).Item = item
			s.Active(0).Moves = []MoveSlot{{MoveID: moveID, PP: 25, MaxPP: 25}}
			s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			if logHas(splashTurn(d, s), "flinched") {
				n++
			}
		}
		return n
	}
	// Water Gun carries no flinch secondary.
	bare := flinches(ItemNone, "water-gun")
	if bare != 0 {
		t.Fatalf("setup: a bare Water Gun flinched %d times", bare)
	}
	if rock := flinches(ItemKingsRock, "water-gun"); rock == 0 {
		t.Errorf("King's Rock never caused a flinch across 250 hits")
	}
	if fang := flinches(ItemRazorFang, "water-gun"); fang == 0 {
		t.Errorf("Razor Fang never caused a flinch across 250 hits")
	}

	// A move that already flinches must be untouched by the item.
	if _, ok := d.Moves["iron-head"]; ok {
		own := flinches(ItemNone, "iron-head")
		withRock := flinches(ItemKingsRock, "iron-head")
		if withRock != own {
			t.Errorf("King's Rock changed a move that already flinches: %d vs %d bare", withRock, own)
		}
	}
}

// --- accuracy ---

func TestAccuracyItemsShiftTheRoll(t *testing.T) {
	d := loadDex(t)
	hits := func(atkItem, defItem ItemKind) int {
		n := 0
		for seed := uint64(1); seed <= 250; seed++ {
			s, err := NewBattle(d, "b", "Attacker", []int{143}, "Target", []int{143}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
			s.Active(0).Item, s.Active(1).Item = atkItem, defItem
			s.Active(0).Moves = []MoveSlot{{MoveID: "focus-blast", PP: 5, MaxPP: 5}}
			s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			if !logHas(splashTurn(d, s), "attack missed") {
				n++
			}
		}
		return n
	}
	base := hits(ItemNone, ItemNone)
	if lens := hits(ItemWideLens, ItemNone); lens <= base {
		t.Errorf("Wide Lens did not raise the hit rate: %d vs %d bare (of 250)", lens, base)
	}
	if powder := hits(ItemNone, ItemBrightPowder); powder >= base {
		t.Errorf("Bright Powder did not lower the hit rate: %d vs %d bare (of 250)", powder, base)
	}
	if incense := hits(ItemNone, ItemLaxIncense); incense >= base {
		t.Errorf("Lax Incense did not lower the hit rate: %d vs %d bare (of 250)", incense, base)
	}
}

// TestZoomLensOnlyPaysOutWhenMovingSecond: the conditional is the item.
func TestZoomLensOnlyPaysOutWhenMovingSecond(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Holder", []int{143}, "Foe", []int{143}, 5)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	s.Active(0).Item = ItemZoomLens

	if got := itemAccuracyMult(s, 0); got != 1 {
		t.Errorf("Zoom Lens paid out before the target moved: %v", got)
	}
	s.Active(1).Volatiles.MovedThisTurn = true
	if got := itemAccuracyMult(s, 0); got != 1.2 {
		t.Errorf("Zoom Lens did not pay out when moving second: %v", got)
	}
}

// --- turn order ---

// TestQuickClawJumpsTheBracketSometimes: a chance-gated jump, so both the
// "sometimes" and the "not always" halves matter.
func TestQuickClawJumpsTheBracketSometimes(t *testing.T) {
	d := loadDex(t)
	firstMoves := 0
	const trials = 300
	for seed := uint64(1); seed <= trials; seed++ {
		// Snorlax (30 Speed) vs Jolteon (130): the slow side can only lead off
		// the claw.
		s, err := NewBattle(d, "b", "Slow", []int{143}, "Fast", []int{135}, seed)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		s.Active(0).Item = ItemQuickClaw
		s.Active(0).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}

		for _, l := range splashTurn(d, s) {
			if l.Type == "move" && logLineHas(l, " used ") {
				if l.Side == 0 {
					firstMoves++
				}
				break
			}
		}
	}
	if firstMoves == 0 {
		t.Errorf("Quick Claw never jumped the bracket across %d turns", trials)
	}
	if firstMoves == trials {
		t.Errorf("Quick Claw jumped every turn (%d/%d) — the chance gate is missing", firstMoves, trials)
	}
	if pct := firstMoves * 100 / trials; pct < 8 || pct > 34 {
		t.Errorf("Quick Claw led %d%% of turns, want roughly %d%%", pct, quickClawChance)
	}
}

// TestLaggingTailPushesTheHolderLast, even when it would otherwise be faster.
func TestLaggingTailPushesTheHolderLast(t *testing.T) {
	d := loadDex(t)
	for _, item := range []ItemKind{ItemLaggingTail, ItemFullIncense} {
		t.Run(string(item), func(t *testing.T) {
			// Jolteon is far faster, so only the tail can put it second.
			s, err := NewBattle(d, "b", "Fast", []int{135}, "Slow", []int{143}, 5)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
			s.Active(0).Item = item
			s.Active(0).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
			s.Active(1).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}

			first := -1
			for _, l := range splashTurn(d, s) {
				if l.Type == "move" && logLineHas(l, " used ") {
					first = l.Side
					break
				}
			}
			if first != 1 {
				t.Errorf("%s holder still moved first (side %d went first)", item, first)
			}
		})
	}
}

// --- multi-hit ---

// TestLoadedDiceRaisesTheFloor: never below 4, never above the move's own max,
// and untouched for a fixed-count multi-hit.
func TestLoadedDiceRaisesTheFloor(t *testing.T) {
	d := loadDex(t)
	if _, ok := d.Moves["bullet-seed"]; !ok {
		t.Skip("bullet-seed not in the curated move set")
	}
	atk := buildPokemon(d, d.Species[143])
	atk.Ability = AbilityNone
	m := d.Moves["bullet-seed"]
	if m.MinHits != 2 || m.MaxHits != 5 {
		t.Skipf("bullet-seed is [%d,%d], not the expected [2,5]", m.MinHits, m.MaxHits)
	}

	sawBelowFloor, sawAny := false, false
	atk.Item = ItemLoadedDice
	for seed := uint64(1); seed <= 200; seed++ {
		n := multihitCount(m, &atk, NewRNG(seed))
		sawAny = true
		if n < loadedDiceMinHits {
			sawBelowFloor = true
		}
		if n > m.MaxHits {
			t.Fatalf("seed %d: %d hits exceeds the move's max of %d", seed, n, m.MaxHits)
		}
	}
	if !sawAny {
		t.Fatal("no rolls taken")
	}
	if sawBelowFloor {
		t.Errorf("Loaded Dice rolled below its floor of %d", loadedDiceMinHits)
	}

	// A bare holder still rolls the full range, so the floor is the item's
	// doing and not a change to the distribution itself.
	bare := buildPokemon(d, d.Species[143])
	bare.Ability = AbilityNone
	low := false
	for seed := uint64(1); seed <= 200; seed++ {
		if multihitCount(m, &bare, NewRNG(seed)) < loadedDiceMinHits {
			low = true
			break
		}
	}
	if !low {
		t.Errorf("a bare holder never rolled below %d — the fixture proves nothing", loadedDiceMinHits)
	}
}

// firstOf2 discards the second result of a two-value call. resolveAccuracy
// grew a "was it a genuine miss?" result that only executeMove cares about;
// the tests that predate it still ask the original yes/no question.
func firstOf2(landed, _ bool) bool { return landed }
