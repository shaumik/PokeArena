package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/shaumik/PokeArena/internal/ai"
	"github.com/shaumik/PokeArena/internal/domain"
)

// sideSalt derives a distinct-but-deterministic seed for the side-1 agent so
// two stochastic agents in a mirror match don't move in lockstep, while keeping
// the whole game a pure function of the episode seed.
//
// The value is copied from internal/eval/match.go, where it is unexported.
// Copying it is load-bearing rather than incidental: it is what makes a
// baseline-vs-baseline episode driven through this binary byte-identical to the
// same pairing run by cmd/bench, so a number measured here is comparable to a
// number on the published board. TestMatchesEvalRunGame pins that equality.
const sideSalt = 0xA5A5A5A5A5A5A5A5

// agentFactory builds a fresh agent for one episode, seeded from that episode's
// seed. Deterministic agents (heuristic, fixed-depth expectimax) ignore the
// seed; the stochastic one uses it, so an episode never depends on agent state
// carried over from the previous episode.
type agentFactory func(seed uint64) ai.Agent

// controllerSpec names who plays a side. "external" hands the side to the
// client over stdio; anything else is one of the built-in baselines, played
// in-process.
//
// The baseline names are exactly cmd/bench's -agents vocabulary, including the
// expectimax@N depth pin, so "which opponent did you train against" has the
// same answer in both tools.
const externalController = "external"

// makeController resolves a controller name. The returned factory is nil for
// the external controller.
func makeController(name string, dex *domain.Dex, defaultDepth int) (label string, newAgent agentFactory, err error) {
	switch {
	case name == "" || name == externalController:
		return externalController, nil, nil
	case name == "random":
		return "random", func(seed uint64) ai.Agent { return ai.NewRandomAgent(seed) }, nil
	case name == "heuristic":
		return "heuristic", func(uint64) ai.Agent { return ai.NewHeuristicAgent(dex) }, nil
	case name == "expectimax":
		return "expectimax", func(uint64) ai.Agent { return ai.NewExpectimaxAgentFixed(dex, defaultDepth) }, nil
	case strings.HasPrefix(name, "expectimax@"):
		d, err := strconv.Atoi(strings.TrimPrefix(name, "expectimax@"))
		if err != nil || d < 1 {
			return "", nil, fmt.Errorf("bad expectimax depth in %q (want expectimax@N, N>=1)", name)
		}
		label := fmt.Sprintf("expectimax-d%d", d)
		return label, func(uint64) ai.Agent { return ai.NewExpectimaxAgentFixed(dex, d) }, nil
	default:
		return "", nil, fmt.Errorf("unknown agent %q (known: external, random, heuristic, expectimax, expectimax@N)", name)
	}
}

// baselineNames lists the built-in opponents, for `handshake`.
func baselineNames() []string {
	return []string{externalController, "random", "heuristic", "expectimax", "expectimax@N"}
}
