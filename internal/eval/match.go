package eval

import (
	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// sideSalt derives a distinct-but-deterministic seed for the side-1 agent so
// two RandomAgents in a mirror match don't move in lockstep, while keeping the
// whole game a pure function of the match seed.
const sideSalt = 0xA5A5A5A5A5A5A5A5

// AgentFactory builds a fresh agent for a single game, seeded from that game's
// seed. Deterministic agents (heuristic, fixed-depth expectimax) ignore the
// seed; stochastic ones (random) use it so the game stays reproducible in
// isolation — the Nth game never depends on agent state carried from game N-1.
type AgentFactory func(seed uint64) ai.Agent

// Contestant is a named strategy entered into a match.
type Contestant struct {
	Name string
	New  AgentFactory
}

// GameRecord is one played game with its participants resolved to names, so a
// win is attributed to an agent rather than to a board side.
type GameRecord struct {
	Match  string     `json:"match"`
	Seed   uint64     `json:"seed"`
	Side0  string     `json:"side0"`
	Side1  string     `json:"side1"`
	Winner string     `json:"winner"` // contestant name, or "draw"
	Result GameResult `json:"-"`      // full trace; streamed separately, not re-embedded
}

// MatchResult aggregates a head-to-head between two contestants.
type MatchResult struct {
	A, B    string
	AWins   int
	BWins   int
	Draws   int
	Games   []GameRecord
}

// RunMatch plays a and b against each other across every seed, in BOTH side
// orientations per seed (a on side 0 then a on side 1). Playing both sides of
// the same seed cancels any first-mover / side-0 advantage, so the win rate
// measures the policy rather than the seat. Fresh agents are built per game, so
// every game is independently reproducible.
func RunMatch(dex *domain.Dex, a, b Contestant, teams [2][]engine.TeamPick, seeds []uint64, budget Budget) (MatchResult, error) {
	mr := MatchResult{A: a.Name, B: b.Name}
	matchName := a.Name + "-vs-" + b.Name

	for _, seed := range seeds {
		// Two orientations so each contestant plays both seats on this seed.
		orientations := [2][2]Contestant{
			{a, b},
			{b, a},
		}
		for _, o := range orientations {
			s0, s1 := o[0], o[1]
			agents := [2]ai.Agent{s0.New(seed), s1.New(seed ^ sideSalt)}
			res, err := RunGame(dex, agents, teams, seed, budget)
			if err != nil {
				return mr, err
			}

			rec := GameRecord{
				Match:  matchName,
				Seed:   seed,
				Side0:  s0.Name,
				Side1:  s1.Name,
				Result: res,
			}
			switch res.Winner {
			case 0:
				rec.Winner = s0.Name
			case 1:
				rec.Winner = s1.Name
			default:
				rec.Winner = "draw"
			}

			switch rec.Winner {
			case a.Name:
				mr.AWins++
			case b.Name:
				mr.BWins++
			default:
				mr.Draws++
			}
			mr.Games = append(mr.Games, rec)
		}
	}
	return mr, nil
}

// SeedRange is the conventional seed set for a run of n games per orientation:
// 0, 1, ..., n-1. Naming the set (rather than randomizing it) is what makes a
// published run reproducible from the command line alone.
func SeedRange(n int) []uint64 {
	seeds := make([]uint64, n)
	for i := range seeds {
		seeds[i] = uint64(i)
	}
	return seeds
}
