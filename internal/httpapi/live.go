package httpapi

import (
	"math/rand"

	"github.com/shaumik/PokeArena/internal/engine"
)

// pickAITeam draws a curated AI roster, seeded by the battle's seed so the same
// battle ID always faces the same opponent. It lives here (not server.go) to
// keep the v1 math/rand the TeamPool expects out of server.go, which uses
// math/rand/v2 for seed generation.
func (s *Server) pickAITeam(seed uint64) ([]engine.TeamPick, error) {
	return s.aiTeams.Pick(rand.New(rand.NewSource(int64(seed))))
}
