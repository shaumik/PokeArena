//go:build showdown

package showdown

import "testing"

// Ported from test/sim/team-validator/custom-rules.js.
//
// Nothing came across. Every case in the file is about Showdown's custom-rule
// syntax — the `@@@` suffix on a format id that adds, removes or rewrites
// rules for one battle: `-Pikachu`, `+Baton Pass`, `!Standard`,
// `-Gravity ++ Grass Whistle > 2`, `-BST > 600`. There is no rule string, no
// rule table and no banlist in this engine at all. Its clauses are Species,
// Item, Evasion, OHKO and Sleep, they are fixed by StandardClauses, and a
// format cannot ask for a different set, so no case here has anything to be
// translated against.
//
// Several cases also lean on `assert.throws(() => Dex.formats.validate(...))`,
// which asserts that a rule string is rejected at parse time. That has no
// counterpart either — there is nothing to parse.
//
// The species are a second, independent reason: Kitsunoh, Crucibelle, Greninja
// forms, Giratina, Tyrantrum, Zygarde, Moltres-Galar, Shaymin, Eternatus,
// Blaziken, Absol, Abomasnow, Cacturne and Charizard-Mega-Y have no dex entry,
// and the stand-ins that do exist for Pikachu, Mewtwo, Smeargle, Eevee,
// Cloyster, Wobbuffet and Azumarill preserve typing, not tier membership.

func TestTeamValidatorCustomRules(t *testing.T) {
	describe(t, "Custom Rules", func(g *psg) {
		g.skip("should support legality tags",
			"team validator: legality tags are not a rule this engine has")
		g.skip("should allow Pokemon to be banned",
			"team validator: species banlists are not a rule this engine has")
		g.skip("should allow Pokemon to be unbanned",
			"team validator: species banlists are not a rule this engine has")
		g.skip("should allow Pokemon to be whitelisted",
			"team validator: species banlists are not a rule this engine has")
		g.skip("should allow Pokemon to be force-whitelisted",
			"team validator: species banlists are not a rule this engine has")
		g.skip("should warn when rules do nothing",
			"team validator: custom rule strings are not a thing this engine parses")
		g.skip("should support banning/unbanning tag combinations",
			"team validator: species tags are not a rule this engine has")
		g.skip("should support banning generic tags from items and moves",
			"team validator: item and move tags are not a rule this engine has")
		g.skip("should support banning Gigantamax tags",
			"team validator: Gigantamax tags are not a rule this engine has")
		g.skip("should support banning move tags",
			"team validator: move tags are not a rule this engine has")
		g.skip("should support numeric tag filters",
			"team validator: numeric tag filters are not a rule this engine has")
		g.skip("should support restrictions",
			"team validator: restricted-legendary limits are not a rule this engine has")
		g.skip("should allow moves to be banned",
			"team validator: move banlists are not a rule this engine has")
		g.skip("should allow moves to be unbanned",
			"team validator: move banlists are not a rule this engine has")
		g.skip("should allow items to be banned",
			"team validator: item banlists are not a rule this engine has")
		g.skip("should allow items to be unbanned",
			"team validator: item banlists are not a rule this engine has")
		g.skip("should allow abilities to be banned",
			"team validator: ability banlists are not a rule this engine has")
		g.skip("should allow abilities to be unbanned",
			"team validator: ability banlists are not a rule this engine has")
		g.skip("should allow complex bans to be added",
			"team validator: complex bans are not a rule this engine has")
		g.skip("should allow complex bans to be altered",
			"team validator: complex bans are not a rule this engine has")
		g.skip("should allow complex bans to be removed",
			"team validator: complex bans are not a rule this engine has")
		g.skip("should allow rule bundles to be removed",
			"team validator: rule bundles are not a thing this engine has")
		g.skip("should allow rule bundles to be overridden",
			"team validator: rule bundles are not a thing this engine has")
	})
}
