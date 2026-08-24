package engine

import "testing"

// Wonder Room swaps the stored Defense and Special Defense and nothing else.
// This engine used to swap the *slug* the formula reads and then take the raw
// stat, the stage and the stat-modifying item off the swapped name, which came
// out exactly inverted: Defense Curl's protection and an Assault Vest's
// protection traded places. These are the untagged checks; the port covers the
// same ground against upstream's own figures.

// TestWonderRoomSwapsRawStatsOnly is the three-legged case: the raw swap
// happens, the stage stays on the stat the move named, and so does the item.
func TestWonderRoomSwapsRawStatsOnly(t *testing.T) {
	d := loadDex(t)
	room := &PseudoWeather{WonderRoom: &PWTimer{TurnsLeft: 5}}

	// Chansey: 5 Def, 105 SpD — the widest split in the dex, so a swap is
	// unmissable. Brick Break is physical, so it reads Defense.
	hit := func(setup func(*Pokemon), pw *PseudoWeather) int {
		atk := buildPokemon(d, d.Species[143])
		atk.Ability = AbilityNone
		def := buildPokemon(d, d.Species[113])
		def.Ability = AbilityNone
		if setup != nil {
			setup(&def)
		}
		return computeDamage(d, &atk, &def, d.Moves["brick-break"], nil, nil, nil, pw, NewRNG(4)).Damage
	}

	plain := hit(nil, nil)
	swapped := hit(nil, room)
	if swapped >= plain {
		t.Fatalf("a physical hit under Wonder Room should read Chansey's 105 Sp. Def instead of "+
			"its 5 Defense and hurt less: %d vs %d", swapped, plain)
	}

	// Defense Curl raises the Def stage. Under Wonder Room a physical hit still
	// names Defense, so the stage still applies.
	curled := hit(func(p *Pokemon) { p.Stages.Def = 1 }, room)
	if curled >= swapped {
		t.Errorf("Defense Curl's +1 Def should still protect against a physical hit under "+
			"Wonder Room: %d, want less than %d", curled, swapped)
	}
	// An Assault Vest raises Sp. Def. A physical hit does not read Sp. Def,
	// under Wonder Room or otherwise, so it must do nothing here.
	vested := hit(func(p *Pokemon) { p.Item = ItemAssaultVest }, room)
	if vested != swapped {
		t.Errorf("an Assault Vest should not blunt a physical hit under Wonder Room: "+
			"%d, want the unvested %d", vested, swapped)
	}
	// The mirror: an Assault Vest still blunts a special hit under Wonder Room,
	// and Defense Curl still does not.
	special := func(setup func(*Pokemon)) int {
		atk := buildPokemon(d, d.Species[143])
		atk.Ability = AbilityNone
		def := buildPokemon(d, d.Species[113])
		def.Ability = AbilityNone
		if setup != nil {
			setup(&def)
		}
		return computeDamage(d, &atk, &def, d.Moves["hyper-voice"], nil, nil, nil, room, NewRNG(4)).Damage
	}
	bare := special(nil)
	if got := special(func(p *Pokemon) { p.Item = ItemAssaultVest }); got >= bare {
		t.Errorf("an Assault Vest should still blunt a special hit under Wonder Room: %d vs %d",
			got, bare)
	}
	if got := special(func(p *Pokemon) { p.Stages.Def = 1 }); got != bare {
		t.Errorf("Defense Curl should not blunt a special hit under Wonder Room: %d, want %d",
			got, bare)
	}
}

// TestWonderRoomFlipsBodyPressToSpDefStages: the one thing that does move by
// name. Upstream's condition carries an onModifyMove that flips an offensive
// override of `def` to `spd`, so Body Press keeps reading the raw Defense
// number — the swap has put it in the spd slot — and picks up the Sp. Def
// stage on the way.
func TestWonderRoomFlipsBodyPressToSpDefStages(t *testing.T) {
	d := loadDex(t)
	room := &PseudoWeather{WonderRoom: &PWTimer{TurnsLeft: 5}}
	press := d.Moves["body-press"]

	press4 := func(defStage, spdStage int, pw *PseudoWeather) int {
		atk := buildPokemon(d, d.Species[143])
		atk.Ability = AbilityNone
		atk.Stages.Def = defStage
		atk.Stages.SpD = spdStage
		def := buildPokemon(d, d.Species[143])
		def.Ability = AbilityNone
		return computeDamage(d, &atk, &def, press, nil, nil, nil, pw, NewRNG(6)).Damage
	}

	base := press4(0, 0, room)
	if got := press4(0, 2, room); got <= base {
		t.Errorf("under Wonder Room, Body Press should read the user's +2 Sp. Def stage: "+
			"%d, want more than %d", got, base)
	}
	if got := press4(2, 0, room); got != base {
		t.Errorf("under Wonder Room, Body Press should ignore the user's Def stage: %d, want %d",
			got, base)
	}
	// Without the room it is the other way round, which is the control.
	plain := press4(0, 0, nil)
	if got := press4(2, 0, nil); got <= plain {
		t.Errorf("without Wonder Room, Body Press should read the Def stage: %d vs %d", got, plain)
	}
	if got := press4(0, 2, nil); got != plain {
		t.Errorf("without Wonder Room, Body Press should ignore the Sp. Def stage: %d, want %d",
			got, plain)
	}
}

// TestDownloadReadsStagesAndIgnoresTheRawSwap. Download compares the target's
// two defenses with stages applied and no item or ability modifiers. Wonder
// Room reaches it only through the stages, because upstream's getStat renames
// the stat after reading the raw value — so a +2 Sp. Def under the room reads
// to Download as a Defense buff.
func TestDownloadReadsStagesAndIgnoresTheRawSwap(t *testing.T) {
	d := loadDex(t)
	// Venusaur: 83 Def, 100 SpD. Def < SpD, so bare Download picks Attack.
	pick := func(spdStage int, pw *PseudoWeather) string {
		s, err := NewBattle(d, "b", "P1", []int{137, 137}, "P2", []int{3}, 1) // Porygon leads
		if err != nil {
			t.Fatalf("new battle: %v", err)
		}
		s.Active(0).Ability = "download"
		s.Active(1).Ability = AbilityNone
		s.Active(1).Stages.SpD = spdStage
		if pw != nil {
			s.PseudoWeather = *pw
		}
		var log []LogLine
		applyOnSwitchIn(s, 0, &log)
		switch {
		case s.Active(0).Stages.Atk > 0:
			return "atk"
		case s.Active(0).Stages.SpA > 0:
			return "spa"
		}
		return "none"
	}
	room := &PseudoWeather{WonderRoom: &PWTimer{TurnsLeft: 5}}

	if got := pick(0, nil); got != "atk" {
		t.Errorf("bare Download vs Venusaur (83 Def / 100 SpD) raised %q, want atk", got)
	}
	if got := pick(0, room); got != "atk" {
		t.Errorf("Download under Wonder Room should still read the raw stats unswapped, "+
			"so still atk; raised %q", got)
	}
	if got := pick(2, nil); got != "atk" {
		t.Errorf("a +2 Sp. Def only widens the gap Download already saw; raised %q, want atk", got)
	}
	if got := pick(2, room); got != "spa" {
		t.Errorf("under Wonder Room a +2 Sp. Def should read to Download as a Defense buff and "+
			"flip it to spa; raised %q", got)
	}
}
