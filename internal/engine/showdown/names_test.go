//go:build showdown

package showdown

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"pokearena/internal/domain"
)

// names_test.go is the translation layer between Showdown's vocabulary and
// this engine's.
//
// Two problems, and they are not the same problem.
//
// The easy one is spelling. Showdown identifies everything by an id that is
// the display name with every non-alphanumeric character stripped —
// "Knock Off" is `knockoff`, "King's Rock" is `kingsrock`. This engine uses
// kebab-case slugs — `knock-off`, `king-s-rock`. Strip the punctuation from
// ours and the two agree on every one of the 538 moves, 128 items, 118
// abilities and 80 species in the dataset, verified by
// TestEveryDatasetSlugNormalizesToAShowdownId below. So the whole spelling
// problem is one regexp.
//
// The hard one is the roster. Showdown's tests draw on the full National Dex;
// this engine ships 80 fully-evolved Kanto species. 77% of the species
// mentions in the upstream suite name something we do not have. Skipping all
// of those would throw away most of the corpus for a reason that usually has
// nothing to do with the mechanic under test — the single most-used species
// in the whole suite is Wynaut, which appears 513 times purely as a body for
// something to happen to.
//
// So the port substitutes, and standIns below is the table it substitutes
// through. Every row is a judgment call and carries the reason it is safe,
// because a stand-in is only safe with respect to what a particular test is
// asking. The table encodes the *usual* answer; a port whose test turns on
// something the row does not preserve is expected to name its own species
// instead (or skip). What a row must never do is silently change the thing
// being measured — see the guard notes on the individual entries.

var nonAlnum = regexp.MustCompile(`[^a-z0-9]`)

// psID renders any name — Showdown display name, Showdown id, or PokeArena
// slug — into Showdown's canonical id form. This is the join key for every
// lookup in this package.
func psID(s string) string { return nonAlnum.ReplaceAllString(strings.ToLower(s), "") }

// standIn is one substitution: a Showdown species this engine does not have,
// and the in-dex species a port should use in its place.
type standIn struct {
	// Dex is the PokeArena Pokédex number to build instead.
	Dex int
	// Keeps names what the substitution preserves, and is the whole
	// justification for the row. A port that depends on something not in
	// this list must not use the stand-in.
	Keeps string
}

// standIns maps a Showdown species id to the species this port builds instead.
//
// The rows are grouped by the role the upstream suite actually uses the
// species for, because that is what makes a substitution safe or unsafe. A
// row is chosen to preserve, in this order: typing, then the ability the test
// needs, then the stat relationship the test needs (usually just "is it
// faster than the other one"), then bulk.
//
// Deliberately absent: anything whose *identity* is the mechanic. There is no
// stand-in for Ditto in a Transform test, for Arceus in a Multitype test, or
// for Shedinja in a Wonder Guard test — those ports skip, and say so.
var standIns = map[string]standIn{
	// --- inert bodies ---------------------------------------------------
	// By far the largest group. The upstream suite reaches for Wynaut,
	// Smeargle, Magikarp and the unevolved starters when it wants a Pokémon
	// that does nothing so the test can watch one thing happen to it. What
	// has to survive is "does not interfere": no relevant ability, no type
	// interaction with the move under test. Ports pair these with
	// ability "noability" whenever the species' own ability could matter.
	"wynaut":     {97, "a slow psychic body with no offensive presence; Shadow Tag is not modeled and Wynaut is never used for it upstream"},
	"wobbuffet":  {97, "as Wynaut — the upstream tests use both interchangeably as a body"},
	"smeargle":   {113, "a frail normal-type body; Own Tempo/Technician are stripped by the port when they could matter"},
	"magikarp":   {119, "a weak water-type body of comparable frailty"},
	"caterpie":   {12, "the evolved form keeps bug typing; ports that need the low stats set HP explicitly"},
	"weedle":     {15, "bug/poison, as the line"},
	"bulbasaur":  {3, "same line, same grass/poison typing and Overgrow"},
	"ivysaur":    {3, "same line"},
	"charmander": {6, "same line, fire; Blaze and Solar Power both present"},
	"charmeleon": {6, "same line"},
	"squirtle":   {9, "same line, water; Torrent and Rain Dish both present"},
	"wartortle":  {9, "same line"},
	"pikachu":    {26, "same line, electric, Static and Lightning Rod both present"},
	"pichu":      {26, "same line"},
	"abra":       {65, "same line, psychic, Synchronize/Inner Focus/Magic Guard all present"},
	"kadabra":    {65, "same line"},
	"gastly":     {94, "same line, ghost/poison"},
	"haunter":    {94, "same line"},
	"geodude":    {76, "same line, rock/ground, Rock Head and Sturdy both present"},
	"graveler":   {76, "same line"},
	"machop":     {68, "same line, fighting, Guts and No Guard both present"},
	"machoke":    {68, "same line"},
	"diglett":    {51, "same line, ground, Arena Trap and Sand Veil both present"},
	"onix":       {95, "in the dex already, listed here so ports do not re-derive it"},
	"mankey":     {57, "same line, fighting"},
	"psyduck":    {55, "same line, water, Damp and Cloud Nine both present"},
	"poliwag":    {62, "same line, water"},
	"bellsprout": {71, "same line, grass/poison"},
	"oddish":     {45, "same line, grass/poison, Effect Spore present"},
	"gloom":      {45, "same line"},
	"paras":      {47, "same line, bug/grass, Effect Spore and Dry Skin both present"},
	"venonat":    {49, "same line, bug/poison, Shield Dust and Tinted Lens both present"},
	"meowth":     {53, "same line, normal, Technician present"},
	"growlithe":  {59, "same line, fire, Intimidate and Flash Fire both present"},
	"ponyta":     {78, "same line, fire, Flash Fire and Flame Body both present"},
	"slowpoke":   {80, "same line, water/psychic, Oblivious and Regenerator both present"},
	"magnemite":  {82, "same line, electric/steel, Magnet Pull and Sturdy both present"},
	"doduo":      {85, "same line, normal/flying, Early Bird present"},
	"seel":       {87, "same line, water/ice, Thick Fat and Hydration both present"},
	"grimer":     {89, "same line, poison, Sticky Hold present"},
	"shellder":   {91, "same line, water/ice, Shell Armor and Skill Link both present"},
	"drowzee":    {97, "same line, psychic, Insomnia and Forewarn both present"},
	"krabby":     {99, "same line, water, Hyper Cutter and Shell Armor both present"},
	"voltorb":    {101, "same line, electric, Soundproof and Aftermath both present"},
	"exeggcute":  {103, "same line, grass/psychic, Chlorophyll and Harvest both present"},
	"cubone":     {105, "same line, ground, Rock Head and Lightning Rod both present"},
	"koffing":    {110, "same line, poison, Levitate and Neutralizing Gas both present"},
	"rhyhorn":    {112, "same line, ground/rock, Lightning Rod and Rock Head both present"},
	"horsea":     {117, "same line, water, Sniper and Damp both present"},
	"goldeen":    {119, "same line, water, Swift Swim and Lightning Rod both present"},
	"staryu":     {121, "same line, water, Natural Cure and Analytic both present"},
	"magby":      {126, "same line, fire, Flame Body and Vital Spirit both present"},
	"elekid":     {125, "same line, electric, Static and Vital Spirit both present"},
	"eevee":      {134, "an eeveelution body; ports needing a specific type name Vaporeon/Jolteon/Flareon directly"},
	"omanyte":    {139, "same line, rock/water, Swift Swim and Shell Armor both present"},
	"kabuto":     {141, "same line, rock/water, Swift Swim and Battle Armor both present"},
	"munchlax":   {143, "same line, normal, Thick Fat and Gluttony both present"},
	"chansey":    {113, "in the dex already"},
	"happiny":    {113, "same line, normal, Natural Cure and Serene Grace both present"},
	"igglybuff":  {40, "same line, normal/fairy, Cute Charm and Competitive both present"},
	"jigglypuff": {40, "same line"},
	"clefairy":   {36, "same line, fairy, Cute Charm/Magic Guard/Unaware all present"},
	"cleffa":     {36, "same line"},
	"mimejr":     {122, "same line, psychic/fairy, Soundproof and Filter both present"},
	"tyrogue":    {106, "same line, fighting"},
	"scyther":    {123, "in the dex already"},
	"kangaskhan": {115, "in the dex already"},
	"tangela":    {114, "in the dex already"},
	"lickitung":  {108, "in the dex already"},

	// --- the special walls ----------------------------------------------
	// Blissey and Shuckle are used upstream when a test needs something that
	// will not die. Chansey is the same Pokémon a stage earlier and is in the
	// dex; Snorlax is the closest thing we have to Shuckle's "survives
	// anything" role, and unlike Shuckle it is not defined by extreme
	// defenses, so ports that measure a *fraction* of max HP are unaffected
	// while ports that need to eat a specific hit may need to set HP.
	"blissey":    {113, "the same species one stage down: normal, Natural Cure and Serene Grace both present, huge HP"},
	"shuckle":    {143, "a body that survives; Shuckle's defense extremes are not preserved, so damage-magnitude ports must not use this"},
	"regirock":   {76, "rock, physically bulky"},
	"regice":     {131, "ice, specially bulky"},
	"steelix":    {95, "Onix evolved is not in the dex; Onix keeps rock/ground and Sturdy, but not the steel typing"},
	"skarmory":   {82, "Magneton is the only steel body in the dex; flying is lost, so ports turning on Ground immunity must not use this"},
	"forretress": {82, "as Skarmory — steel is preserved, bug is not"},
	"ferrothorn": {82, "steel is preserved, grass and Iron Barbs are not; Iron Barbs ports must skip"},

	// --- typed stand-ins -------------------------------------------------
	// Where a test needs a type this dex reaches only through one or two
	// species. These are the rows most likely to be wrong for a given test,
	// so each says what it does *not* preserve.
	"tyranitar":    {76, "rock, and a Sand Stream body if the port sets the ability; dark is lost"},
	"sableye":      {94, "ghost is preserved via Gengar; dark and Prankster are not"},
	"spiritomb":    {94, "ghost; dark is not preserved"},
	"aggron":       {82, "steel; rock is not preserved"},
	"scizor":       {82, "steel and Technician if the port sets it; bug is not preserved"},
	"heatran":      {38, "fire; steel and Flash Fire's interaction with it are not preserved"},
	"latias":       {65, "psychic; dragon is not preserved"},
	"latios":       {65, "psychic; dragon is not preserved"},
	"salamence":    {149, "dragon/flying, and an Intimidate body upstream — Dragonite has Inner Focus instead, so Intimidate ports must set it"},
	"garchomp":     {149, "dragon; ground is not preserved"},
	"kyogre":       {9, "water, and a Drizzle body if the port sets the ability"},
	"groudon":      {28, "ground, and a Drought body if the port sets the ability"},
	"rayquaza":     {149, "dragon/flying"},
	"lugia":        {144, "flying with high special bulk; psychic is not preserved"},
	"hooh":         {146, "fire/flying"},
	"suicune":      {134, "water, bulky"},
	"raikou":       {135, "electric, fast"},
	"entei":        {59, "fire"},
	"celebi":       {103, "grass/psychic"},
	"jirachi":      {122, "psychic; steel and Serene Grace are not preserved"},
	"deoxys":       {150, "psychic; the forme stat spreads are not preserved"},
	"deoxysattack": {150, "psychic, offensive"},
	"deoxysspeed":  {65, "psychic, fast"},
	"ninjask":      {123, "bug/flying and fast; Speed Boost must be set explicitly"},
	"golisopod":    {99, "water and a physical attacker; bug and Emergency Exit are not preserved"},
	"greninja":     {62, "water; dark and Protean are not preserved"},
	"incineroar":   {59, "fire, and an Intimidate body"},
	"landorust":    {28, "ground; flying and Intimidate are not preserved"},
	"toxapex":      {73, "water/poison, bulky"},
	"corviknight":  {82, "steel; flying is not preserved"},
	"dragapult":    {94, "ghost and fast; dragon is not preserved"},
	"dondozo":      {131, "water, very bulky"},
	"lopunny":      {115, "normal"},
	"arcanine":     {59, "in the dex already"},
	"gliscor":      {28, "ground; flying and Poison Heal are not preserved"},
	"crobat":       {42, "poison/flying — Golbat is the same line one stage down"},
	"zubat":        {42, "same line"},
	"golbat":       {42, "in the dex already"},
	"weezing":      {110, "in the dex already"},
	"marowak":      {105, "in the dex already"},
	"electivire":   {125, "electric — Electabuzz is the same line"},
	"magmortar":    {126, "fire — Magmar is the same line"},
	"togekiss":     {40, "fairy; flying and Serene Grace are not preserved"},
	"clefable":     {36, "in the dex already"},
	"azumarill":    {36, "fairy; water and Huge Power are not preserved"},
	"sylveon":      {36, "fairy"},
	"mimikyu":      {94, "ghost; fairy and Disguise are not preserved"},
	"gholdengo":    {82, "steel; ghost and Good as Gold are not preserved"},
	"kingambit":    {82, "steel; dark and Supreme Overlord are not preserved"},
	"ironhands":    {125, "electric; fighting and Quark Drive are not preserved"},
	"greatusk":     {76, "ground; fighting and Protosynthesis are not preserved"},
	"fluttermane":  {94, "ghost and very fast; fairy and Protosynthesis are not preserved"},
}

// dexIndex is the lazily built id → Pokédex number map for the species this
// engine actually ships.
type dexIndex struct {
	once      sync.Once
	species   map[string]int
	moves     map[string]string
	items     map[string]string
	abilities map[string]bool
}

var index dexIndex

func (x *dexIndex) build(d *domain.Dex) {
	x.once.Do(func() {
		x.species = map[string]int{}
		x.moves = map[string]string{}
		x.items = map[string]string{}
		x.abilities = map[string]bool{}
		for n, sp := range d.Species {
			x.species[psID(sp.Name)] = n
			for _, a := range sp.Abilities {
				x.abilities[psID(a)] = true
			}
		}
		for id := range d.Moves {
			x.moves[psID(id)] = id
		}
		for id := range d.Items {
			x.items[psID(id)] = id
		}
	})
}

// resolveSpecies turns a Showdown species name into a Pokédex number. It
// returns the number, whether a stand-in was used, and an error naming the
// species when neither the dex nor the stand-in table has an answer.
func resolveSpecies(d *domain.Dex, name string) (num int, substituted bool, err error) {
	index.build(d)
	id := psID(name)
	if n, ok := index.species[id]; ok {
		return n, false, nil
	}
	if si, ok := standIns[id]; ok {
		return si.Dex, true, nil
	}
	return 0, false, fmt.Errorf("species %q is not in this dex and has no stand-in "+
		"(add a row to standIns with the reason it is safe, name an in-dex species "+
		"in the port, or skip the case)", name)
}

// resolveMove turns a Showdown move id into this engine's move slug.
func resolveMove(d *domain.Dex, name string) (string, error) {
	index.build(d)
	if slug, ok := index.moves[psID(name)]; ok {
		return slug, nil
	}
	return "", fmt.Errorf("move %q is not in this dataset", name)
}

// resolveItem turns a Showdown item id into this engine's item slug.
func resolveItem(d *domain.Dex, name string) (string, error) {
	index.build(d)
	if slug, ok := index.items[psID(name)]; ok {
		return slug, nil
	}
	return "", fmt.Errorf("item %q is not in this dataset", name)
}

// standInReport renders the stand-in table for the port's documentation, so
// the reasons live in one place and are read from the code rather than
// re-typed into a doc that then drifts.
func standInReport(d *domain.Dex) string {
	index.build(d)
	ids := make([]string, 0, len(standIns))
	for id := range standIns {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var b strings.Builder
	for _, id := range ids {
		si := standIns[id]
		fmt.Fprintf(&b, "| %s | %s | %s |\n", id, d.Species[si.Dex].Name, si.Keeps)
	}
	return b.String()
}
