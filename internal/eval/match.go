package eval

import (
	"fmt"

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
	Team   string     `json:"team"`
	Seed   uint64     `json:"seed"`
	Side0  string     `json:"side0"`
	Side1  string     `json:"side1"`
	Winner string     `json:"winner"` // contestant name, or "draw"
	Result GameResult `json:"-"`      // full trace; streamed separately, not re-embedded
}

// MatchResult aggregates a head-to-head between two contestants on one team.
type MatchResult struct {
	A, B  string
	Team  string
	AWins int
	BWins int
	Draws int
	Games []GameRecord
}

// seat is one side's assignment for a single game: who's playing (name), the
// factory that builds their agent, and the team they pilot. RunMatch and
// TeamTournament fill seats differently — RunMatch swaps the agents over
// side-pinned teams, TeamTournament swaps the teams under one fixed pilot — but
// both hand two seats to resolvedGame, so the seat salting and the
// winner→name mapping live in exactly one place.
type seat struct {
	name     string
	newAgent AgentFactory
	picks    []engine.TeamPick
}

// gameOutcome is one played game with the winner already resolved to a
// contestant name ("" for a draw), so callers tally by identity rather than by
// board side.
type gameOutcome struct {
	S0, S1 string
	Winner string
	Turns  int
	Result GameResult
}

// resolvedGame plays one game with s0 on side 0 and s1 on side 1, salting the
// side-1 agent (seed ^ sideSalt) so a mirror doesn't move in lockstep while the
// game stays a pure function of the seed, and resolves the board-side winner to
// a name. This is the shared core of both match runners; the differing seat
// assignment (which side each agent/team takes per orientation) stays with each
// caller.
func resolvedGame(dex *domain.Dex, s0, s1 seat, seed uint64, budget Budget) (gameOutcome, error) {
	agents := [2]ai.Agent{s0.newAgent(seed), s1.newAgent(seed ^ sideSalt)}
	teams := [2][]engine.TeamPick{s0.picks, s1.picks}
	res, err := RunGame(dex, agents, teams, seed, budget)
	if err != nil {
		return gameOutcome{}, err
	}
	oc := gameOutcome{S0: s0.name, S1: s1.name, Turns: res.Turns, Result: res}
	switch res.Winner {
	case 0:
		oc.Winner = s0.name
	case 1:
		oc.Winner = s1.name
	}
	return oc, nil
}

// RunMatch plays a and b against each other across every seed, in BOTH side
// orientations per seed (a on side 0 then a on side 1). Playing both sides of
// the same seed cancels any first-mover / side-0 advantage, so the win rate
// measures the policy rather than the seat. The teams stay pinned to their
// board sides while the agents swap. Fresh agents are built per game, so every
// game is independently reproducible.
func RunMatch(dex *domain.Dex, a, b Contestant, teamName string, teams [2][]engine.TeamPick, seeds []uint64, budget Budget) (MatchResult, error) {
	// Two contestants sharing a name would collapse win attribution: the
	// per-side result is correct, but the `switch rec.Winner { case a.Name;
	// case b.Name }` below always matches the first case, so every win books to
	// A. Standings would likewise fold both into one record and write a self-edge
	// into the Elo fit. Names are the identity here — reject the collision.
	if a.Name == b.Name {
		return MatchResult{}, fmt.Errorf("RunMatch: both contestants are named %q; names must be unique", a.Name)
	}
	mr := MatchResult{A: a.Name, B: b.Name, Team: teamName}
	matchName := a.Name + "-vs-" + b.Name

	for _, seed := range seeds {
		// Two orientations so each contestant plays both seats on this seed.
		// The teams are pinned to the board sides; only the pilots swap.
		for _, o := range [2][2]Contestant{{a, b}, {b, a}} {
			s0 := seat{name: o[0].Name, newAgent: o[0].New, picks: teams[0]}
			s1 := seat{name: o[1].Name, newAgent: o[1].New, picks: teams[1]}
			oc, err := resolvedGame(dex, s0, s1, seed, budget)
			if err != nil {
				return mr, err
			}

			winner := oc.Winner
			if winner == "" {
				winner = "draw"
			}
			mr.Games = append(mr.Games, GameRecord{
				Match:  matchName,
				Team:   teamName,
				Seed:   seed,
				Side0:  oc.S0,
				Side1:  oc.S1,
				Winner: winner,
				Result: oc.Result,
			})

			switch oc.Winner {
			case a.Name:
				mr.AWins++
			case b.Name:
				mr.BWins++
			default:
				mr.Draws++
			}
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
