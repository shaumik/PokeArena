package mcpserver

import (
	"fmt"
	"sort"

	pokearena "github.com/shaumik/PokeArena"
	"github.com/shaumik/PokeArena/internal/ai"
	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/engine"
)

// localdata.go: the dataset this binary carries, and the projection of it into
// the same shapes the gateway's REST endpoints return.
//
// Two things need it. start_battle needs a dex and an opponent roster to run a
// battle in-process. And find_pokemon / get_pokemon / list_items / list_natures
// need answers — they proxy GET /api/pokemon and friends (dexproxy.go), so
// without this they would be four tools that fail whenever no gateway is up,
// which is precisely the situation offline mode exists for.
//
// Rather than teach each tool a second code path, loading seeds the caches
// dexproxy.go already keeps. The fetch* helpers find them populated and never
// reach for HTTP. A gateway is then only needed for what genuinely needs one:
// joining somebody else's live battle.

// offlineDataVersion labels the dex built from the embedded dataset. It shows
// up wherever domain.Dex.Version is reported, so it says where the data came
// from rather than pretending to be a release version.
const offlineDataVersion = "embedded"

// aiTeamsFile is the opponent roster inside the embedded dataset.
const aiTeamsFile = "ai-teams.json"

// offlineData is everything needed to run a battle without a gateway.
type offlineData struct {
	dex  *domain.Dex
	pool *ai.TeamPool
}

// loadOfflineData builds the dex and the opponent team pool from the dataset
// compiled into this binary. It reads no files and opens no sockets, so it
// works from any working directory — which is the point: someone who installed
// pokearena-mcp from the MCP registry has a binary and nothing else.
func loadOfflineData() (*offlineData, error) {
	fsys := pokearena.DataFS()
	dex, err := domain.LoadDexFS(fsys, offlineDataVersion)
	if err != nil {
		return nil, fmt.Errorf("load embedded dex: %w", err)
	}
	pool, err := ai.LoadTeamPoolFS(dex, fsys, aiTeamsFile)
	if err != nil {
		return nil, fmt.Errorf("load embedded opponent teams: %w", err)
	}
	return &offlineData{dex: dex, pool: pool}, nil
}

// seedCaches fills the dexproxy caches from the local dex, so the four
// reference tools answer offline. Every projection below mirrors the gateway
// handler it stands in for (httpapi.handlePokemon / handleItems /
// handleNatures / handleRules) field for field — an agent must not be able to
// tell which one answered.
func (s *Server) seedCaches(d *offlineData) {
	s.dexMu.Lock()
	s.dexCache = localDexEntries(d.dex)
	s.dexMu.Unlock()

	s.itemMu.Lock()
	s.itemCache = localItemEntries(d.dex)
	s.itemMu.Unlock()

	s.natureMu.Lock()
	s.natureCache = localNatureEntries(d.dex)
	s.natureMu.Unlock()

	r := localRules()
	s.rulesMu.Lock()
	s.rulesCache = &r
	s.rulesMu.Unlock()
}

// localDexEntries projects the dex into the dexEntry shape GET /api/pokemon
// returns. Species order follows dex.AllSpecies, the same ordering the gateway
// serves, so find_pokemon's cap of 30 selects the same 30 either way.
func localDexEntries(dex *domain.Dex) []dexEntry {
	species := dex.AllSpecies()
	out := make([]dexEntry, 0, len(species))
	for _, sp := range species {
		moves := make([]dexMoveInfo, 0, len(sp.Moves))
		for _, mid := range sp.Moves {
			m, ok := dex.Moves[mid]
			if !ok {
				// A learnset naming a move the dataset doesn't carry. The
				// gateway drops it the same way; surfacing an unusable move
				// would only get the team rejected at submit time.
				continue
			}
			moves = append(moves, dexMoveInfo{
				ID:       m.ID,
				Name:     m.Name,
				Type:     string(m.Type),
				Category: string(m.Category),
				Power:    m.Power,
				PP:       m.PP,
			})
		}
		out = append(out, dexEntry{
			DexNo: sp.DexNo,
			Name:  sp.Name,
			Type1: string(sp.Type1),
			Type2: string(sp.Type2),
			Base: dexBaseStats{
				HP:    sp.Base.HP,
				Atk:   sp.Base.Atk,
				Def:   sp.Base.Def,
				SpAtk: sp.Base.SpA,
				SpDef: sp.Base.SpD,
				Speed: sp.Base.Spe,
			},
			Abilities: sp.Abilities,
			Moves:     moves,
		})
	}
	return out
}

// localItemEntries projects the held-item catalog. engine.ItemCatalog is the
// same call the gateway makes, so the descriptions come from the engine's item
// registry and the list cannot advertise an item the engine won't honor.
func localItemEntries(dex *domain.Dex) []itemEntry {
	cat := engine.ItemCatalog(dex)
	out := make([]itemEntry, 0, len(cat))
	for _, it := range cat {
		out = append(out, itemEntry{ID: it.ID, Name: it.Name, Desc: it.Desc})
	}
	return out
}

// localNatureEntries projects the nature table, sorted by id to match the
// gateway's stable ordering.
func localNatureEntries(dex *domain.Dex) []natureEntry {
	out := make([]natureEntry, 0, len(dex.Natures))
	for _, n := range dex.Natures {
		out = append(out, natureEntry{ID: n.ID, Name: n.Name, Plus: n.Plus, Minus: n.Minus})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// localRules reports the engine's own constants, exactly as
// httpapi.handleRules does. Read from the engine rather than restated, so a
// change to the EV budget cannot leave this telling agents the old number.
func localRules() formatRules {
	return formatRules{
		Level:        engine.Level,
		TeamSize:     engine.TeamSize,
		MovesMin:     engine.MovesMin,
		MovesMax:     engine.MovesMax,
		EVMaxPerStat: engine.MaxEVPerStat,
		EVMaxTotal:   engine.MaxEVTotal,
		IVMax:        engine.MaxIV,
	}
}
