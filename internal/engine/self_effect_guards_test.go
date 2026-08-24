package engine

import "testing"

// Three effects reached from outside the path that guards them. The shape is
// the same each time: a hand-coded site re-made *some* of the checks the
// declarative path performs, which is exactly what made the omissions look
// considered.

// TestRestIsRefusedByEverythingThatRefusesSleep. doRest wrote the status and the
// heal directly, so the only terrainBlocksStatus call site — inside
// inflictStatus — never saw it. The Chesto Berry check was re-made locally,
// which is the tell.
func TestRestIsRefusedByEverythingThatRefusesSleep(t *testing.T) {
	d := loadDex(t)
	// Snorlax is grounded, so both terrains reach it.
	rest := func(setup func(*BattleState)) (healed bool, log []LogLine) {
		s := neutralBattle(t, d, 3, []int{143}, []int{143})
		teachMoves(t, d, &s.Sides[0].Team[0], "rest")
		teachMoves(t, d, &s.Sides[1].Team[0], "splash")
		s.Active(0).HP = s.Active(0).MaxHP / 2
		s.Active(0).Ability = AbilityNone
		if setup != nil {
			setup(s)
		}
		before := s.Active(0).HP
		log = ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
		return s.Active(0).HP > before, log
	}

	if ok, _ := rest(nil); !ok {
		t.Fatalf("baseline: Rest should heal a half-HP Snorlax")
	}
	for _, tc := range []struct {
		name  string
		setup func(*BattleState)
	}{
		{"electric terrain", func(s *BattleState) {
			s.Terrain = &TerrainState{Kind: TerrainElectric, TurnsLeft: 5}
		}},
		{"misty terrain", func(s *BattleState) {
			s.Terrain = &TerrainState{Kind: TerrainMisty, TurnsLeft: 5}
		}},
		{"insomnia", func(s *BattleState) { s.Active(0).Ability = "insomnia" }},
		{"vital spirit", func(s *BattleState) { s.Active(0).Ability = "vital-spirit" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			healed, log := rest(tc.setup)
			if healed {
				t.Errorf("Rest healed through %s — the heal is downstream of the status "+
					"landing; log %v", tc.name, logTexts(log))
			}
			if !logHas(log, "But it failed") {
				t.Errorf("a refused Rest should say so; log %v", logTexts(log))
			}
		})
	}
	// Safeguard is deliberately NOT in that list: upstream's Safeguard only
	// refuses foe-sourced status, so a Pokemon may Rest behind its own.
	t.Run("its own Safeguard does not stop it", func(t *testing.T) {
		healed, log := rest(func(s *BattleState) {
			s.Sides[0].Conditions.Safeguard = &SafeguardState{TurnsLeft: 5}
		})
		if !healed {
			t.Errorf("Rest under the user's own Safeguard should still work; log %v",
				logTexts(log))
		}
	})
}

// TestSelfHealMovesFailAtFullHP, and Roost keeps its Flying type when it does.
// healPokemon caps at MaxHP and logs nothing when HP does not move, so the whole
// family used to resolve in silence; for Roost that was not cosmetic, because the
// volatile was set before the heal was attempted.
func TestSelfHealMovesFailAtFullHP(t *testing.T) {
	d := loadDex(t)
	for _, move := range []string{"recover", "soft-boiled", "roost"} {
		t.Run(move, func(t *testing.T) {
			// Chansey learns Soft-Boiled; Pidgeot learns Roost. Snorlax covers
			// Recover. Pick a body that has the move and can also be dented.
			dexNo := 143
			switch move {
			case "soft-boiled":
				dexNo = 113
			case "roost":
				dexNo = 18
			}
			s := neutralBattle(t, d, 5, []int{dexNo}, []int{143})
			teachMoves(t, d, &s.Sides[0].Team[0], move)
			teachMoves(t, d, &s.Sides[1].Team[0], "splash")
			log := ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
			if !logHas(log, "But it failed") {
				t.Errorf("%s at full HP should fail out loud; log %v", move, logTexts(log))
			}
			if move == "roost" && s.Active(0).Volatiles.Roost {
				t.Errorf("a Roost that failed must not strip the user's Flying type — that is " +
					"a free Ground weakness")
			}
		})
	}
	// And a heal that actually has room still works.
	s := neutralBattle(t, d, 7, []int{143}, []int{143})
	teachMoves(t, d, &s.Sides[0].Team[0], "recover")
	teachMoves(t, d, &s.Sides[1].Team[0], "splash")
	s.Active(0).HP = s.Active(0).MaxHP - 1
	ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)})
	if s.Active(0).HP != s.Active(0).MaxHP {
		t.Errorf("a one-HP-short Recover should still heal: %d/%d",
			s.Active(0).HP, s.Active(0).MaxHP)
	}
}

// TestIntimidateAndDefogAreStoppedByASubstitute. The Substitute guard for
// foe-induced effects lives in applyEffectFields, the path a *move's* effects
// take. Neither of these takes it: Intimidate calls applyStagesFromFoe from
// applyOnSwitchIn and Defog calls it from its own handler, and both re-made the
// White Herb check and not this one. Canon puts the check in each effect rather
// than in the shared boost path, so it is checked at each site here too.
func TestIntimidateAndDefogAreStoppedByASubstitute(t *testing.T) {
	d := loadDex(t)
	t.Run("intimidate", func(t *testing.T) {
		s := neutralBattle(t, d, 9, []int{143, 59}, []int{143})
		teachMoves(t, d, &s.Sides[0].Team[0], "splash")
		teachMoves(t, d, &s.Sides[0].Team[1], "splash")
		teachMoves(t, d, &s.Sides[1].Team[0], "substitute", "splash")
		s.Sides[0].Team[1].Ability = "intimidate"

		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)}) // the doll goes up
		if !hasSubstitute(s.Active(1)) {
			t.Fatalf("setup: the Substitute should be up")
		}
		log := ResolveTurn(d, s, [2]Action{switchTo(1), moveAt(1)})
		if got := s.Active(1).Stages.Atk; got != 0 {
			t.Errorf("a doll should stop Intimidate outright: atk %d, want 0; log %v",
				got, logTexts(log))
		}
		if !logHas(log, "Intimidate cuts") {
			t.Errorf("canon announces the ability either way; log %v", logTexts(log))
		}
	})
	t.Run("defog", func(t *testing.T) {
		s := neutralBattle(t, d, 11, []int{18}, []int{143})
		teachMoves(t, d, &s.Sides[0].Team[0], "defog")
		teachMoves(t, d, &s.Sides[1].Team[0], "substitute", "splash")
		s.Sides[1].Conditions.Hazards.StealthRock = true

		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(0)}) // doll up, Defog once
		before := s.Active(1).Stages.Eva
		ResolveTurn(d, s, [2]Action{moveAt(0), moveAt(1)})
		if got := s.Active(1).Stages.Eva; got != before {
			t.Errorf("a doll should stop Defog's evasion drop: eva %d -> %d", before, got)
		}
		// The sweep is not blocked — only the drop is.
		if s.Sides[1].Conditions.Hazards.StealthRock {
			t.Errorf("Defog should still have cleared the field through the doll")
		}
	})
}
