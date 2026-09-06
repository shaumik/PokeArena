package ai

import (
	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/engine"
)

// MakeViewDex includes authoritative legal actions and public move metadata.
// Use it when publishing a battle view; legality is computed on the real
// state before redaction, never reconstructed from an opponent's hidden data.
func MakeViewDex(dex *domain.Dex, s *engine.BattleState, side int) View {
	return makeView(dex, s, side)
}

func viewLegalActions(dex *domain.Dex, s *engine.BattleState, side int) []engine.Action {
	if s.Ended() || (s.Phase == engine.PhaseReplace && !s.Replace[side]) {
		return []engine.Action{}
	}
	actions := engine.LegalActionsDex(dex, s, side)
	if actions == nil {
		return []engine.Action{}
	}
	return actions
}

// MoveMetadata describes the public dex entry, not situation-dependent power
// or hit probability. Accuracy zero denotes a move that bypasses accuracy checks.
type MoveMetadata struct {
	BP       int             `json:"bp"`
	Accuracy int             `json:"accuracy"`
	Type     domain.Type     `json:"type"`
	Category domain.Category `json:"category"`
}

func moveMetadata(dex *domain.Dex, id string) *MoveMetadata {
	if dex == nil || id == "" {
		return nil
	}
	m, ok := dex.Moves[id]
	if !ok {
		return nil
	}
	return &MoveMetadata{BP: m.Power, Accuracy: m.Accuracy, Type: m.Type, Category: m.Category}
}

type ownMoveWire struct {
	engine.MoveSlot
	*MoveMetadata
}

type pokemonMovesWire struct {
	engine.Pokemon
	Moves []ownMoveWire `json:"moves"`
}

type sideMovesWire struct {
	engine.Side
	Team []pokemonMovesWire `json:"team"`
}

func selfMovesWire(side engine.Side, dex *domain.Dex) sideMovesWire {
	out := sideMovesWire{Side: side, Team: make([]pokemonMovesWire, len(side.Team))}
	for i, p := range side.Team {
		moves := make([]ownMoveWire, len(p.Moves))
		for j, m := range p.Moves {
			moves[j] = ownMoveWire{MoveSlot: m, MoveMetadata: moveMetadata(dex, m.MoveID)}
		}
		out.Team[i] = pokemonMovesWire{Pokemon: p, Moves: moves}
	}
	return out
}
