package engine

import (
	"testing"

	"github.com/shaumik/PokeArena/internal/domain"
)

// items_modifiers_test.go covers the always-on family. The shape of the risk is
// different from the berries: nothing is consumed, so there is no "did it get
// spent" question — what goes wrong instead is a multiplier applied to the
// wrong thing (the wrong type, the wrong category, the wrong side, the wrong
// species). Every test here therefore checks a positive case *and* a negative
// one, because a booster wired to "always 1.2" passes any positive-only test.

// damageWith reports the expected damage atk deals to def with the given item
// held. ExpectedDamage is the deterministic estimator (average roll, no crit),
// which is what makes an exact multiplier assertion possible at all.
func damageWith(t *testing.T, d *domain.Dex, atkDex, defDex int, moveID string, item ItemKind) int {
	t.Helper()
	atk := buildPokemon(d, d.Species[atkDex])
	def := buildPokemon(d, d.Species[defDex])
	atk.Ability, def.Ability = AbilityNone, AbilityNone
	atk.Item = item
	m, ok := d.Moves[moveID]
	if !ok {
		t.Fatalf("move %q not in the dex", moveID)
	}
	return ExpectedDamage(d, &atk, &def, m, nil, nil, nil)
}

// assertRatio checks got ≈ base × num/den, allowing one point of integer slack
// in either direction (the formula floors once at the end).
func assertRatio(t *testing.T, label string, base, got, num, den int) {
	t.Helper()
	want := base * num / den
	if got < want-1 || got > want+1 {
		t.Errorf("%s: %d → %d, want ~%d (%d/%d of base)", label, base, got, want, num, den)
	}
}

// --- type boosters ---

// TestTypeBoostersRaiseOnlyTheirType walks all eighteen: each must boost a move
// of its own type by 1.2x and leave every other type alone. The "leaves alone"
// half is what catches a booster wired to the wrong type — a mistake that a
// per-item positive test would happily confirm.
func TestTypeBoostersRaiseOnlyTheirType(t *testing.T) {
	d := loadDex(t)
	// Snorlax has a wide learnset, so one attacker can throw moves of many
	// types; Snorlax also defends neutrally against most of them.
	const atkDex, defDex = 143, 143
	cases := []struct {
		item   ItemKind
		typ    domain.Type
		moveID string
	}{
		{ItemSilkScarf, "normal", "body-slam"},
		{ItemCharcoal, "fire", "fire-blast"},
		{ItemMysticWater, "water", "surf"},
		{ItemMagnet, "electric", "thunderbolt"},
		{ItemMiracleSeed, "grass", "solar-beam"},
		{ItemNeverMeltIce, "ice", "ice-beam"},
		{ItemBlackBelt, "fighting", "brick-break"},
		{ItemPoisonBarb, "poison", "sludge-bomb"},
		{ItemSoftSand, "ground", "earthquake"},
		{ItemSharpBeak, "flying", "fly"},
		{ItemTwistedSpoon, "psychic", "psychic"},
		{ItemSilverPowder, "bug", "bug-bite"},
		{ItemHardStone, "rock", "rock-slide"},
		{ItemSpellTag, "ghost", "shadow-ball"},
		{ItemDragonFang, "dragon", "outrage"},
		{ItemBlackGlasses, "dark", "crunch"},
		{ItemMetalCoat, "steel", "iron-head"},
		{ItemFairyFeather, "fairy", "play-rough"},
	}
	for _, tc := range cases {
		t.Run(string(tc.item), func(t *testing.T) {
			if m, ok := d.Moves[tc.moveID]; !ok {
				t.Skipf("%s not in the curated move set", tc.moveID)
			} else if m.Type != tc.typ {
				t.Fatalf("fixture error: %s is %s, not %s", tc.moveID, m.Type, tc.typ)
			}
			base := damageWith(t, d, atkDex, defDex, tc.moveID, ItemNone)
			if base <= 0 {
				t.Skipf("%s deals no damage in this matchup", tc.moveID)
			}
			assertRatio(t, string(tc.item)+" on its own type",
				base, damageWith(t, d, atkDex, defDex, tc.moveID, tc.item), 12, 10)

			// An off-type move must be untouched. Tackle is Normal, so Silk
			// Scarf uses Surf as its foil instead.
			off := "tackle"
			if tc.typ == "normal" {
				off = "surf"
			}
			offBase := damageWith(t, d, atkDex, defDex, off, ItemNone)
			if got := damageWith(t, d, atkDex, defDex, off, tc.item); got != offBase {
				t.Errorf("%s changed off-type (%s) damage: %d → %d", tc.item, off, offBase, got)
			}
		})
	}
}

// TestEveryTypeHasABooster: eighteen types, eighteen boosters, no duplicates.
// The same completeness guard the resist berries get, for the same reason —
// a repetitive table is where an omission hides.
func TestEveryTypeHasABooster(t *testing.T) {
	d := loadDex(t)
	byType := map[domain.Type]ItemKind{}
	for _, ty := range []domain.Type{
		"normal", "fire", "water", "electric", "grass", "ice", "fighting", "poison",
		"ground", "flying", "psychic", "bug", "rock", "ghost", "dragon", "dark",
		"steel", "fairy",
	} {
		found := ItemNone
		for kind := range itemRegistry {
			it := itemRegistry[kind]
			if it.OutgoingDamageMult == nil || it.ResistType != "" {
				continue
			}
			// Probe the hook with a bare move of this type: a type booster
			// answers >1 for its own type and 1 for everything else.
			probe := domain.Move{Type: ty, Category: domain.CatPhysical, Power: 50}
			holder := buildPokemon(d, d.Species[143])
			holder.Item = kind
			if it.OutgoingDamageMult(&holder, probe, &holder, nil, 1.0) != typeBoostMult {
				continue
			}
			// Confirm it is type-scoped, not a blanket booster.
			other := domain.Type("normal")
			if ty == "normal" {
				other = "water"
			}
			probeOther := domain.Move{Type: other, Category: domain.CatPhysical, Power: 50}
			if it.OutgoingDamageMult(&holder, probeOther, &holder, nil, 1.0) != 1 {
				continue
			}
			if found != ItemNone {
				t.Errorf("two boosters claim %s: %q and %q", ty, found, kind)
			}
			found = kind
		}
		if found == ItemNone {
			t.Errorf("no type booster covers %s", ty)
		}
		byType[ty] = found
	}
}

// --- category and coverage ---

// TestExpertBeltOnlyBoostsSuperEffective: the whole item is the effectiveness
// gate, so the neutral case is the test that matters.
func TestExpertBeltOnlyBoostsSuperEffective(t *testing.T) {
	d := loadDex(t)
	// Charizard's Fire vs Venusaur (Grass/Poison) is 2x; vs Snorlax it's neutral.
	seBase := damageWith(t, d, 6, 3, "flamethrower", ItemNone)
	seBelt := damageWith(t, d, 6, 3, "flamethrower", ItemExpertBelt)
	assertRatio(t, "Expert Belt on a super-effective hit", seBase, seBelt, 12, 10)

	nBase := damageWith(t, d, 6, 143, "flamethrower", ItemNone)
	if nBelt := damageWith(t, d, 6, 143, "flamethrower", ItemExpertBelt); nBelt != nBase {
		t.Errorf("Expert Belt boosted a neutral hit: %d → %d", nBase, nBelt)
	}
}

// TestCategoryBands: Muscle Band answers physical, Wise Glasses special, each
// ignoring the other.
func TestCategoryBands(t *testing.T) {
	d := loadDex(t)
	for _, tc := range []struct {
		item          ItemKind
		boosts, other string
	}{
		{ItemMuscleBand, "body-slam", "surf"},
		{ItemWiseGlasses, "surf", "body-slam"},
	} {
		t.Run(string(tc.item), func(t *testing.T) {
			base := damageWith(t, d, 143, 143, tc.boosts, ItemNone)
			assertRatio(t, string(tc.item), base,
				damageWith(t, d, 143, 143, tc.boosts, tc.item), 11, 10)

			otherBase := damageWith(t, d, 143, 143, tc.other, ItemNone)
			if got := damageWith(t, d, 143, 143, tc.other, tc.item); got != otherBase {
				t.Errorf("%s touched the other category: %d → %d", tc.item, otherBase, got)
			}
		})
	}
}

// TestPunchingGloveBoostsPunchesAndDropsContact covers both halves. The contact
// half is the reason the item exists over Muscle Band, and it is invisible to a
// damage-only test.
func TestPunchingGloveBoostsPunchesAndDropsContact(t *testing.T) {
	d := loadDex(t)
	if _, ok := d.Moves["fire-punch"]; !ok {
		t.Skip("fire-punch not in the curated move set")
	}
	base := damageWith(t, d, 143, 143, "fire-punch", ItemNone)
	assertRatio(t, "Punching Glove on a punch", base,
		damageWith(t, d, 143, 143, "fire-punch", ItemPunchingGlove), 11, 10)

	// A non-punch contact move gets no boost.
	slamBase := damageWith(t, d, 143, 143, "body-slam", ItemNone)
	if got := damageWith(t, d, 143, 143, "body-slam", ItemPunchingGlove); got != slamBase {
		t.Errorf("Punching Glove boosted a non-punch move: %d → %d", slamBase, got)
	}

	// Contact suppression: a gloved punch must not wake Rocky Helmet.
	helmetChip := func(atkItem ItemKind) int {
		s, err := NewBattle(d, "b", "Puncher", []int{107}, "Helmet", []int{143}, 5)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		s.Active(0).Item = atkItem
		s.Active(0).Moves = []MoveSlot{{MoveID: "fire-punch", PP: 15, MaxPP: 15}}
		s.Active(1).Item = ItemRockyHelmet
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		before := s.Active(0).HP
		splashTurn(d, s)
		return before - s.Active(0).HP
	}
	bare := helmetChip(ItemNone)
	if bare <= 0 {
		t.Fatalf("setup: Rocky Helmet did not chip a bare puncher")
	}
	if gloved := helmetChip(ItemPunchingGlove); gloved != 0 {
		t.Errorf("a gloved punch still tripped Rocky Helmet for %d (bare: %d)", gloved, bare)
	}
}

// TestMetronomeRampsOnRepeatsAndResets: the streak is the item. Both the ramp
// and the reset are asserted, plus the 2x ceiling.
func TestMetronomeRampsOnRepeatsAndResets(t *testing.T) {
	d := loadDex(t)
	atk := buildPokemon(d, d.Species[143])
	atk.Ability = AbilityNone
	atk.Item = ItemMetronome
	m := d.Moves["body-slam"]

	// The first use is unboosted — the streak counts *prior* consecutive uses,
	// so the ramp starts on the second.
	tickMetronome(&atk, m, false)
	if got := metronomeMult(&atk, m); got != 1 {
		t.Errorf("first use multiplier = %v, want 1", got)
	}
	for i, want := range []float64{1.2, 1.4, 1.6, 1.8, 2.0} {
		tickMetronome(&atk, m, false)
		if got := metronomeMult(&atk, m); got != want {
			t.Errorf("use %d (repeat %d): multiplier = %v, want %v", i+2, i+1, got, want)
		}
	}
	// Capped: more repeats don't push past 2.0.
	for i := 0; i < 5; i++ {
		tickMetronome(&atk, m, false)
	}
	if got := metronomeMult(&atk, m); got != metronomeMax {
		t.Errorf("multiplier past the cap = %v, want %v", got, metronomeMax)
	}
	// A different move resets the streak, and the old move no longer carries it.
	other := d.Moves["surf"]
	tickMetronome(&atk, other, false)
	if got := metronomeMult(&atk, other); got != 1 {
		t.Errorf("switching moves did not reset the streak: %v", got)
	}
	if got := metronomeMult(&atk, m); got != 1 {
		t.Errorf("the abandoned move kept its streak: %v", got)
	}
}

// TestMetronomeStreakSurvivesRealTurns drives it through ResolveTurn rather
// than calling the helper, so the tick's placement in executeMove is covered
// too — a streak that never advances in a real battle is the actual failure.
func TestMetronomeStreakSurvivesRealTurns(t *testing.T) {
	d, s := berryBattle(t, ItemMetronome)
	holder := s.Active(0)
	holder.Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}

	for i := 0; i < 3; i++ {
		ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 0}, {Kind: ActionMove, Index: 0}})
		if s.Ended() {
			break
		}
	}
	if got := s.Active(0).Volatiles.MetronomeCount; got < 2 {
		t.Errorf("streak after three repeats = %d, want at least 2", got)
	}
	if got := s.Active(0).Volatiles.MetronomeMoveID; got != "body-slam" {
		t.Errorf("streak tracked %q, want body-slam", got)
	}
}

// --- defensive ---

// TestAssaultVestBulksSpecialDefenceOnly: the boost must land on special
// defense and nowhere else.
func TestAssaultVestBulksSpecialDefence(t *testing.T) {
	d := loadDex(t)
	withVest := func(moveID string, vest bool) int {
		atk := buildPokemon(d, d.Species[143])
		def := buildPokemon(d, d.Species[143])
		atk.Ability, def.Ability = AbilityNone, AbilityNone
		if vest {
			def.Item = ItemAssaultVest
		}
		return ExpectedDamage(d, &atk, &def, d.Moves[moveID], nil, nil, nil)
	}
	specBase, specVest := withVest("surf", false), withVest("surf", true)
	assertRatio(t, "Assault Vest vs a special hit", specBase, specVest, 10, 15)

	physBase, physVest := withVest("body-slam", false), withVest("body-slam", true)
	if physVest != physBase {
		t.Errorf("Assault Vest changed physical damage taken: %d → %d", physBase, physVest)
	}
}

// TestAssaultVestBarsStatusMoves: both gates. LegalActions must not offer one,
// and executeMove must refuse it even if a controller ignores the legal set —
// which is the gate an external agent can actually reach.
func TestAssaultVestBarsStatusMoves(t *testing.T) {
	d, s := berryBattle(t, ItemAssaultVest)
	holder := s.Active(0)
	holder.Moves = []MoveSlot{
		{MoveID: "body-slam", PP: 15, MaxPP: 15},
		{MoveID: "swords-dance", PP: 20, MaxPP: 20},
	}

	for _, a := range LegalActionsDex(d, s, 0) {
		if a.Kind == ActionMove && a.Index == 1 {
			t.Errorf("LegalActions offered a status move to an Assault Vest holder")
		}
	}

	log := ResolveTurn(d, s, [2]Action{{Kind: ActionMove, Index: 1}, {Kind: ActionMove, Index: 0}})
	if s.Active(0).Stages.Atk != 0 {
		t.Errorf("Swords Dance resolved through the Assault Vest: Atk stage = %d", s.Active(0).Stages.Atk)
	}
	if !logHas(log, "cannot use status moves") {
		t.Errorf("no refusal line; log: %v", log)
	}
}

// TestRockyHelmetChipsContactAttackersOnly.
func TestRockyHelmetChipsContactAttackersOnly(t *testing.T) {
	d := loadDex(t)
	chip := func(moveID string) (int, int) {
		s, err := NewBattle(d, "b", "Attacker", []int{143}, "Helmet", []int{143}, 5)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		s.Active(0).Moves = []MoveSlot{{MoveID: moveID, PP: 20, MaxPP: 20}}
		s.Active(1).Item = ItemRockyHelmet
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		before := s.Active(0).HP
		splashTurn(d, s)
		return before - s.Active(0).HP, s.Active(0).MaxHP / 6
	}
	got, want := chip("body-slam") // contact
	if got != want {
		t.Errorf("contact move: attacker lost %d, want %d", got, want)
	}
	if got, _ := chip("surf"); got != 0 { // non-contact
		t.Errorf("non-contact move: attacker lost %d, want 0", got)
	}
	// The helmet is permanent — a second contact hit chips again.
	s, _ := NewBattle(d, "b", "Attacker", []int{143}, "Helmet", []int{143}, 5)
	s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	s.Active(0).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
	s.Active(1).Item = ItemRockyHelmet
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	splashTurn(d, s)
	afterFirst := s.Active(0).HP
	splashTurn(d, s)
	if s.Active(0).HP >= afterFirst {
		t.Errorf("Rocky Helmet stopped chipping after one hit — it is not consumable")
	}
	if s.Active(1).Item != ItemRockyHelmet {
		t.Errorf("Rocky Helmet was consumed")
	}
}

// TestShellBellDrainsAFractionOfDamageDealt.
func TestShellBellDrainsAFractionOfDamageDealt(t *testing.T) {
	d := loadDex(t)
	s, err := NewBattle(d, "b", "Holder", []int{143}, "Target", []int{143}, 5)
	if err != nil {
		t.Fatalf("new battle: %v", err)
	}
	holder := s.Active(0)
	holder.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	holder.Item = ItemShellBell
	holder.Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
	holder.HP = holder.MaxHP / 2
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
	foeBefore, selfBefore := s.Active(1).HP, holder.HP

	splashTurn(d, s)

	dealt := foeBefore - s.Active(1).HP
	if dealt <= 0 {
		t.Fatalf("setup: no damage dealt")
	}
	if got, want := s.Active(0).HP-selfBefore, dealt/8; got != want {
		t.Errorf("Shell Bell restored %d, want %d (1/8 of %d dealt)", got, want, dealt)
	}
}

// TestShellBellDoesNothingAtFullHP: a full-HP holder has nothing to restore, so
// the log must stay quiet rather than reporting a zero heal.
func TestShellBellDoesNothingAtFullHP(t *testing.T) {
	d := loadDex(t)
	s, _ := NewBattle(d, "b", "Holder", []int{143}, "Target", []int{143}, 5)
	s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
	s.Active(0).Item = ItemShellBell
	s.Active(0).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
	s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}

	log := splashTurn(d, s)

	if logHas(log, "Shell Bell") {
		t.Errorf("Shell Bell logged a heal at full HP; log: %v", log)
	}
}

// TestBigRootBoostsDrainRecovery: the multiplier lands on the recovery, not the
// damage, so both are checked.
func TestBigRootBoostsDrainRecovery(t *testing.T) {
	d := loadDex(t)
	if _, ok := d.Moves["giga-drain"]; !ok {
		t.Skip("giga-drain not in the curated move set")
	}
	run := func(item ItemKind) (healed, dealt int) {
		s, err := NewBattle(d, "b", "Drainer", []int{3}, "Target", []int{9}, 5)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		s.Active(0).Item = item
		s.Active(0).Moves = []MoveSlot{{MoveID: "giga-drain", PP: 10, MaxPP: 10}}
		s.Active(0).HP = s.Active(0).MaxHP / 4
		s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		selfBefore, foeBefore := s.Active(0).HP, s.Active(1).HP
		splashTurn(d, s)
		return s.Active(0).HP - selfBefore, foeBefore - s.Active(1).HP
	}
	bareHeal, bareDealt := run(ItemNone)
	rootHeal, rootDealt := run(ItemBigRoot)
	if bareHeal <= 0 {
		t.Fatalf("setup: the drain healed nothing")
	}
	if rootDealt != bareDealt {
		t.Errorf("Big Root changed the damage dealt: %d → %d", bareDealt, rootDealt)
	}
	assertRatio(t, "Big Root recovery", bareHeal, rootHeal, 13, 10)
}

// --- crit ratio ---

// TestCritItemsRaiseTheCritStage compares observed crit rates rather than
// reading the stage back, because the stage is what the item sets but the rate
// is what a player experiences — and the crit table is where an off-by-one in
// the stage actually shows up.
func TestCritItemsRaiseTheCritStage(t *testing.T) {
	d := loadDex(t)
	crits := func(dexNo int, item ItemKind) int {
		n := 0
		for seed := uint64(1); seed <= 300; seed++ {
			s, err := NewBattle(d, "b", "Attacker", []int{dexNo}, "Target", []int{143}, seed)
			if err != nil {
				t.Fatalf("new battle: %v", err)
			}
			s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
			s.Active(0).Item = item
			s.Active(0).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}
			s.Active(1).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
			if logHas(splashTurn(d, s), "critical hit") {
				n++
			}
		}
		return n
	}
	// Stage 0 is 1/24, stage 1 is 1/8 — a threefold jump over 300 samples.
	bare := crits(143, ItemNone)
	lens := crits(143, ItemScopeLens)
	if lens <= bare {
		t.Errorf("Scope Lens did not raise the crit rate: %d vs %d bare (of 300)", lens, bare)
	}
	// Razor Claw is the same +1.
	if claw := crits(143, ItemRazorClaw); claw <= bare {
		t.Errorf("Razor Claw did not raise the crit rate: %d vs %d bare", claw, bare)
	}
}

// TestSpeciesLockedCritItemsAreInertOnTheWrongHolder: Lucky Punch and Leek are
// +2 for exactly one species and dead weight otherwise. The wrong-holder case
// is the whole point of the lock.
func TestSpeciesLockedCritItemsAreInertOnTheWrongHolder(t *testing.T) {
	d := loadDex(t)
	for _, tc := range []struct {
		item    ItemKind
		species int
	}{
		{ItemLuckyPunch, dexChansey},
		{ItemLeek, dexFarfetchd},
	} {
		t.Run(string(tc.item), func(t *testing.T) {
			right := buildPokemon(d, d.Species[tc.species])
			right.Item = tc.item
			if got := itemCritStage(&right); got != 2 {
				t.Errorf("%s on its own species: crit stage = %d, want 2", tc.item, got)
			}
			wrong := buildPokemon(d, d.Species[143]) // Snorlax
			wrong.Item = tc.item
			if got := itemCritStage(&wrong); got != 0 {
				t.Errorf("%s on the wrong species: crit stage = %d, want 0", tc.item, got)
			}
		})
	}
}

// TestThickClubDoublesAttackForItsSpeciesOnly.
func TestThickClubDoublesAttackForItsSpeciesOnly(t *testing.T) {
	d := loadDex(t)
	marowakBase := damageWith(t, d, dexMarowak, 143, "body-slam", ItemNone)
	marowakClub := damageWith(t, d, dexMarowak, 143, "body-slam", ItemThickClub)
	assertRatio(t, "Thick Club on Marowak", marowakBase, marowakClub, 2, 1)

	otherBase := damageWith(t, d, 143, 143, "body-slam", ItemNone)
	if got := damageWith(t, d, 143, 143, "body-slam", ItemThickClub); got != otherBase {
		t.Errorf("Thick Club boosted the wrong species: %d → %d", otherBase, got)
	}
	// Special attacks are untouched even on the right holder.
	specBase := damageWith(t, d, dexMarowak, 143, "surf", ItemNone)
	if got := damageWith(t, d, dexMarowak, 143, "surf", ItemThickClub); got != specBase {
		t.Errorf("Thick Club boosted a special move: %d → %d", specBase, got)
	}
}

// --- Focus Band ---

// TestFocusBandSavesSometimesAndIsNotConsumed: the chance gate and the "not a
// one-shot" property are the two things that distinguish it from Focus Sash.
func TestFocusBandSavesSometimesAndIsNotConsumed(t *testing.T) {
	d := loadDex(t)
	saves, held := 0, 0
	const trials = 300
	for seed := uint64(1); seed <= trials; seed++ {
		s, err := NewBattle(d, "b", "Holder", []int{143}, "Attacker", []int{143}, seed)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		holder := s.Active(0)
		holder.Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		holder.Item = ItemFocusBand
		holder.HP = 2 // not full HP: a Focus Sash could never save here
		holder.Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "body-slam", PP: 15, MaxPP: 15}}

		splashTurn(d, s)
		if !s.Sides[0].Team[0].Fainted {
			saves++
			if s.Sides[0].Team[0].Item == ItemFocusBand {
				held++
			}
		}
	}
	if saves == 0 {
		t.Fatalf("Focus Band never saved across %d lethal hits", trials)
	}
	if saves == trials {
		t.Errorf("Focus Band saved every time (%d/%d) — the chance gate is missing", saves, trials)
	}
	if held != saves {
		t.Errorf("Focus Band was consumed on %d of %d saves; it is not a one-shot", saves-held, saves)
	}
	// Roughly 10%: a wide band, since this is a sanity check on the rate, not
	// a distribution test.
	if pct := saves * 100 / trials; pct < 3 || pct > 22 {
		t.Errorf("Focus Band saved %d%% of the time, want roughly %d%%", pct, focusBandChance)
	}
}

// TestFocusBandDrawsNoRNGWhenItCannotFire: the roll must happen only on a hit
// that would actually be lethal, or a Focus Band holder would shift the RNG
// stream on every exchange and battles would stop replaying against the same
// battle without one.
func TestFocusBandDrawsNoRNGWhenItCannotFire(t *testing.T) {
	d := loadDex(t)
	run := func(item ItemKind) uint64 {
		s, err := NewBattle(d, "b", "Holder", []int{143}, "Attacker", []int{143}, 9)
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability, s.Active(1).Ability = AbilityNone, AbilityNone
		s.Active(0).Item = item
		s.Active(0).Moves = []MoveSlot{{MoveID: "splash", PP: 40, MaxPP: 40}}
		s.Active(1).Moves = []MoveSlot{{MoveID: "tackle", PP: 35, MaxPP: 35}}
		splashTurn(d, s) // a full-HP Snorlax is nowhere near dying to Tackle
		return s.RNGState
	}
	if band, bare := run(ItemFocusBand), run(ItemNone); band != bare {
		t.Errorf("Focus Band consumed RNG on a non-lethal hit: state %d vs %d bare", band, bare)
	}
}

// TestPunchingGloveOnlyDecontactsPunches: the glove's scope has to match its
// boost. A blanket "this holder makes no contact" flag would silently protect
// a Punching Glove holder's Body Slam from Rocky Helmet and Rough Skin, which
// is a much larger effect than the item actually has.
func TestPunchingGloveOnlyDecontactsPunches(t *testing.T) {
	d := loadDex(t)
	holder := buildPokemon(d, d.Species[107]) // Hitmonchan
	holder.Item = ItemPunchingGlove

	punch, ok := d.Moves["fire-punch"]
	if !ok {
		t.Skip("fire-punch not in the curated move set")
	}
	if moveMakesContact(punch, &holder) {
		t.Errorf("a gloved punch still counts as contact")
	}

	slam := d.Moves["body-slam"]
	if !slam.HasFlag("contact") {
		t.Fatalf("fixture error: body-slam is not a contact move")
	}
	if !moveMakesContact(slam, &holder) {
		t.Errorf("Punching Glove decontacted a non-punch move — its scope is punches only")
	}

	// And the untouched baseline: a bare holder makes contact with both.
	bare := buildPokemon(d, d.Species[107])
	if !moveMakesContact(punch, &bare) || !moveMakesContact(slam, &bare) {
		t.Errorf("a bare holder should make contact with both moves")
	}
}
