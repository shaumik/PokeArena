package engine

import (
	"fmt"

	"pokearena/internal/domain"
)

// Not here, deliberately: Ability Shield. Nothing in this engine can suppress
// an ability — Gastro Acid sets a volatile no lookup reads, and Neutralizing
// Gas is registered inert — so the shield would guard against nothing. Shipping
// it would mean advertising an item that provably does nothing, which is the
// same "don't ship what we can't honor" line the move denylist draws. It lands
// the day ability suppression does.
//
// Also absent: Eviolite (the dataset carries no evolution data, so there is no
// way to tell which holders qualify) and Float Stone (weight isn't modeled).
//
// items_field.go is the third item family: the ones that change the *rules*
// rather than a damage number. Self-inflicted status (the orbs), residual chip
// and heal that keys off typing (Black Sludge, Sticky Barb), field-duration
// extenders (Light Clay, the weather rocks, Terrain Extender), and the utility
// items that grant or remove an immunity (Heavy-Duty Boots, Safety Goggles,
// Shed Shell, Air Balloon, Iron Ball, Ring Target, Utility Umbrella,
// Protective Pads, Covert Cloak, Clear Amulet, Ability Shield).
//
// The through-line is that almost none of these live in the damage formula, so
// each one's integration point is somewhere the engine makes a *decision*: does
// this hazard apply, does this status land, may this Pokémon switch, is it
// grounded. Every one of those gates now asks the item layer, and each gate has
// a dispatcher here rather than reading the registry inline.

const (
	// Self-inflicted status.
	ItemFlameOrb ItemKind = "flame-orb"
	ItemToxicOrb ItemKind = "toxic-orb"

	// Typing-keyed residuals.
	ItemBlackSludge ItemKind = "black-sludge"
	ItemStickyBarb  ItemKind = "sticky-barb"

	// Field-duration extenders.
	ItemLightClay       ItemKind = "light-clay"
	ItemDampRock        ItemKind = "damp-rock"
	ItemHeatRock        ItemKind = "heat-rock"
	ItemSmoothRock      ItemKind = "smooth-rock"
	ItemIcyRock         ItemKind = "icy-rock"
	ItemTerrainExtender ItemKind = "terrain-extender"

	// Partial-trap modifiers (held by the trapper, read by the trap).
	ItemBindingBand ItemKind = "binding-band"
	ItemGripClaw    ItemKind = "grip-claw"

	// Immunity grants and removals.
	ItemHeavyDutyBoots  ItemKind = "heavy-duty-boots"
	ItemSafetyGoggles   ItemKind = "safety-goggles"
	ItemShedShell       ItemKind = "shed-shell"
	ItemAirBalloon      ItemKind = "air-balloon"
	ItemIronBall        ItemKind = "iron-ball"
	ItemRingTarget      ItemKind = "ring-target"
	ItemUtilityUmbrella ItemKind = "utility-umbrella"
	ItemProtectivePads  ItemKind = "protective-pads"
	ItemCovertCloak     ItemKind = "covert-cloak"
	ItemClearAmulet     ItemKind = "clear-amulet"
)

// extendedFieldTurns is how long an extender item makes a field condition last,
// up from the default five. One constant for all of them, because canon uses
// the same eight for screens, weather, and terrain.
const extendedFieldTurns = 8

// Partial-trap tuning. A trapper holding Binding Band chips for a sixth instead
// of an eighth; one holding Grip Claw pins the target for the full seven turns
// instead of the usual four-to-five roll.
const (
	bindingBandDenom = 6
	gripClawTurns    = 7
)

func init() {
	registerOrbItems()
	registerResidualItems()
	registerFieldExtenders()
	registerTrapModifiers()
	registerImmunityItems()
}

// --- self-inflicted status ---

// statusOrb builds a Flame Orb / Toxic Orb: at the end of the turn it inflicts
// its status on the holder. The infliction goes through inflictStatus, so every
// existing guard applies — a Fire-type can't be burned by a Flame Orb, a Steel
// or Poison type can't be badly poisoned by a Toxic Orb, Safeguard and the
// status-blocking abilities all still refuse it, and a holder that already has
// a status is unaffected. The orb is not consumed, so it retries every turn
// until something sticks.
func statusOrb(kind ItemKind, name string, st StatusCond) *Item {
	return &Item{
		Kind: kind, Name: name,
		Desc: fmt.Sprintf("At the end of each turn, the holder becomes %s.", statusVerb(st)),
		// Late slot: canon puts the orbs at the very end of the residual order,
		// well after the heals and chips, so a holder takes no status damage on
		// the turn the orb fires.
		EndOfTurnLate: func(s *BattleState, side int, rng *RNG, log *[]LogLine) {
			p := s.Active(side)
			if p.Status != StatusNone {
				return
			}
			inflictStatus(p, side, st, s, rng, log)
		},
	}
}

func registerOrbItems() {
	registerItem(statusOrb(ItemFlameOrb, "Flame Orb", StatusBurn))
	registerItem(statusOrb(ItemToxicOrb, "Toxic Orb", StatusToxic))
}

// --- typing-keyed residuals ---

func registerResidualItems() {
	// Black Sludge is Leftovers for one type and a liability for everything
	// else, so it shares Leftovers' early residual slot: the heal has to land
	// before the poison/burn chip to be worth anything, and the chip half is
	// ordered the same way for symmetry.
	registerItem(&Item{
		Kind: ItemBlackSludge, Name: "Black Sludge",
		Desc: "Restores 1/16 of max HP each turn to a Poison-type holder; costs any other holder 1/8.",
		EndOfTurn: func(s *BattleState, side int, log *[]LogLine) {
			p := s.Active(side)
			if isType(p, "poison") {
				itemHealFraction(p, side, 1.0/16, "Black Sludge", log)
				return
			}
			// Indirect damage: Magic Guard shrugs it off like any other chip.
			if abilityBlocksIndirectDamage(p) {
				return
			}
			itemDamage(p, side, p.MaxHP/8, "%s is hurt by its Black Sludge! (-%d)", log)
			if p.HP <= 0 {
				faint(p, side, log)
			}
		},
	})

	registerItem(&Item{
		Kind: ItemStickyBarb, Name: "Sticky Barb",
		Desc: "Costs the holder 1/8 of max HP each turn, and latches onto an attacker that makes contact and holds nothing.",
		// Late slot, like the orbs — the barb is a pure drawback and canon
		// ticks it after the recovery items.
		EndOfTurnLate: func(s *BattleState, side int, _ *RNG, log *[]LogLine) {
			p := s.Active(side)
			if abilityBlocksIndirectDamage(p) {
				return
			}
			itemDamage(p, side, p.MaxHP/8, "%s is hurt by the Sticky Barb! (-%d)", log)
			if p.HP <= 0 {
				faint(p, side, log)
			}
		},
		// The transfer is the interesting half: a contact attacker that is
		// holding nothing walks away with the barb, and the holder is free of
		// it. An attacker that already holds something keeps its own item.
		OnHitTakenPassive: func(s *BattleState, defSide int, m domain.Move, _ DamageResult, log *[]LogLine) {
			def := s.Active(defSide)
			atk := s.Active(1 - defSide)
			if !moveMakesContact(m, atk) || atk.Fainted || atk.Item != ItemNone {
				return
			}
			// loseItem, not consumeItem: the barb was taken, not used up, so
			// Unburden arms but Recycle must not be able to hand it back.
			loseItem(def)
			giveItem(atk, ItemStickyBarb)
			*log = append(*log, LogLine{
				Type: "item", Side: 1 - defSide,
				Text: fmt.Sprintf("%s was given the Sticky Barb!", atk.Name),
			})
		},
	})
}

// --- field-duration extenders ---

func registerFieldExtenders() {
	registerItem(&Item{
		Kind: ItemLightClay, Name: "Light Clay",
		Desc:           "Screens the holder sets last 8 turns instead of 5.",
		ExtendsScreens: true,
	})
	registerItem(&Item{
		Kind: ItemTerrainExtender, Name: "Terrain Extender",
		Desc:           "Terrain the holder sets lasts 8 turns instead of 5.",
		ExtendsTerrain: true,
	})

	for _, r := range []struct {
		kind    ItemKind
		name    string
		weather WeatherKind
		label   string
	}{
		{ItemDampRock, "Damp Rock", WeatherRain, "Rain"},
		{ItemHeatRock, "Heat Rock", WeatherSun, "Harsh sunlight"},
		{ItemSmoothRock, "Smooth Rock", WeatherSandstorm, "A sandstorm"},
		{ItemIcyRock, "Icy Rock", WeatherSnow, "Snow"},
	} {
		registerItem(&Item{
			Kind: r.kind, Name: r.name,
			Desc:           fmt.Sprintf("%s the holder summons lasts 8 turns instead of 5.", r.label),
			ExtendsWeather: r.weather,
		})
	}
}

// --- partial-trap modifiers ---

func registerTrapModifiers() {
	registerItem(&Item{
		Kind: ItemBindingBand, Name: "Binding Band",
		Desc: "Trapping moves the holder uses chip the target for 1/6 of max HP instead of 1/8.",
	})
	registerItem(&Item{
		Kind: ItemGripClaw, Name: "Grip Claw",
		Desc: "Trapping moves the holder uses last the full 7 turns.",
	})
}

// partialTrapTuning returns the chip denominator and turn count a trap set by
// this attacker should use. Read once, at the moment the trap lands, because
// the items live on the *trapper* while the residual runs on the target — and
// the trapper may have switched out (or lost the item) long before the trap
// expires. Snapshotting is also what canon does.
func partialTrapTuning(atk *Pokemon, rolledTurns int) (denom, turns int) {
	denom, turns = partialTrapDenom, rolledTurns
	switch it := itemOf(atk); {
	case it == nil:
	case it.Kind == ItemBindingBand:
		denom = bindingBandDenom
	case it.Kind == ItemGripClaw:
		turns = gripClawTurns
	}
	return denom, turns
}

// --- immunity grants and removals ---

func registerImmunityItems() {
	registerItem(&Item{
		Kind: ItemHeavyDutyBoots, Name: "Heavy-Duty Boots",
		Desc:           "The holder is unaffected by entry hazards.",
		IgnoresHazards: true,
	})

	registerItem(&Item{
		Kind: ItemSafetyGoggles, Name: "Safety Goggles",
		Desc:              "The holder is unaffected by sandstorm damage and powder moves.",
		ImmuneToSandstorm: true,
		BlocksPowder:      true,
	})

	registerItem(&Item{
		Kind: ItemShedShell, Name: "Shed Shell",
		Desc:            "The holder can always switch out, even when trapped.",
		AllowsSwitchOut: true,
	})

	registerItem(&Item{
		Kind: ItemIronBall, Name: "Iron Ball",
		Desc:      "Halves the holder's Speed and grounds it, so Ground-type moves can hit it.",
		Grounds:   true,
		SpeedMult: func(p *Pokemon, w *WeatherState) float64 { return 0.5 },
	})

	registerItem(&Item{
		Kind: ItemRingTarget, Name: "Ring Target",
		Desc:               "Removes the holder's type immunities — moves it would normally be immune to hit it.",
		LiftsOwnImmunities: true,
	})

	registerItem(&Item{
		Kind: ItemUtilityUmbrella, Name: "Utility Umbrella",
		Desc:           "The holder ignores the effects of rain and harsh sunlight.",
		IgnoresWeather: true,
	})

	registerItem(&Item{
		Kind: ItemProtectivePads, Name: "Protective Pads",
		Desc: "The holder's contact moves don't trigger contact-reactive effects.",
		// Unlike Punching Glove, the pads cover everything the holder throws.
		SuppressesContact: func(m domain.Move) bool { return true },
	})

	registerItem(&Item{
		Kind: ItemCovertCloak, Name: "Covert Cloak",
		Desc:              "The holder is unaffected by the added effects of attacks.",
		BlocksSecondaries: true,
	})

	registerItem(&Item{
		Kind: ItemClearAmulet, Name: "Clear Amulet",
		Desc:            "The holder's stats cannot be lowered by the opponent.",
		BlocksStatDrops: true,
	})

	// Air Balloon is the one item here with a lifecycle: it grants a Ground
	// immunity and then pops the first time any damaging move connects,
	// including one it made the holder immune to nothing about. Both halves
	// (the immunity and the pop) are one-shot in the sense that the item is
	// gone afterwards, but the immunity is not itself "consumed" — it simply
	// stops existing along with the balloon.
	registerItem(&Item{
		Kind:   ItemAirBalloon,
		Name:   "Air Balloon",
		Desc:   "The holder floats, dodging Ground-type moves, until it is hit by any attack.",
		Floats: true,
		TypeImmunity: func(atkType domain.Type) (float64, bool) {
			if atkType == "ground" {
				return 0, true
			}
			return 1, false
		},
		OnHitTakenPassive: func(s *BattleState, defSide int, _ domain.Move, _ DamageResult, log *[]LogLine) {
			def := s.Active(defSide)
			*log = append(*log, LogLine{
				Type: "item", Side: defSide,
				Text: fmt.Sprintf("%s's Air Balloon popped!", def.Name),
			})
			consumeItem(def)
		},
	})
}
