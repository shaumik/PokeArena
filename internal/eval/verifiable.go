package eval

import (
	"encoding/json"
	"fmt"
	"sort"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
)

// Verifiable errors: decision quality without a judge.
//
// Scoring a choice against a reference agent measures agreement with that
// agent, and two references of different families rank the same policies in
// opposite orders (see docs/decision-quality.md). So "blunder rate vs an
// oracle" cannot be published as a quality score.
//
// This is the other approach: count only mistakes that are *provably* mistakes.
// Not "a strong player would have done better" — that is an opinion — but "this
// action could not possibly have accomplished anything," which is a fact about
// the rules that no reviewer can argue with and no choice of reference can
// flip.
//
// Two rules keep it honest, and both are load-bearing:
//
//  1. **Certainty.** A category is only included when the action is a guaranteed
//     no-op. Anything that merely *usually* wastes a turn is left out. A false
//     positive here is far more damaging than a missed one — the whole value of
//     this metric is that every count survives scrutiny.
//
//  2. **Knowability.** A mistake only counts if the player could have known it
//     was one from the fog-of-war view it was handed. Attacking into a Levitate
//     immunity is a wasted turn, but the ability may be hidden, so it is not
//     charged here; attacking a Ghost with a Normal move is charged, because the
//     defender's types are public. Penalizing unknowable information would make
//     this a luck metric.
//
// The result is a floor, not a full account of skill: a policy can score zero
// verifiable errors and still play badly. That is the intended trade. It is a
// number that means exactly what it says.

// ErrKind identifies a category of provable error. Kept as stable string
// constants because they are reported per category and end up in published
// tables.
type ErrKind string

const (
	// ErrImmuneAttack is a damaging move whose type does nothing to the
	// defender's types. Zero damage is guaranteed by the type chart, and both
	// of the defender's types are visible.
	ErrImmuneAttack ErrKind = "immune-attack"
	// ErrHealAtFull is a pure healing move used at full HP. It restores
	// nothing; the turn is spent for no effect.
	ErrHealAtFull ErrKind = "heal-at-full"
	// ErrRedundantStatus is a status move that inflicts the status the target
	// already has. A Pokémon cannot hold two statuses, so it cannot land.
	ErrRedundantStatus ErrKind = "redundant-status"
	// ErrBoostAtCap is a self-boosting move whose every stat is already at the
	// +6 ceiling. Nothing can move.
	ErrBoostAtCap ErrKind = "boost-at-cap"
)

// VerifiableError is one provable mistake, with enough context to audit it.
// Detail is written to be readable on its own in a report row, because the
// point of this metric is that a skeptic can check any single count by hand.
type VerifiableError struct {
	Turn   int     `json:"turn"`
	Side   int     `json:"side"`
	Kind   ErrKind `json:"kind"`
	Detail string  `json:"detail"`
}

// maxStage is the stat-stage ceiling; a boost move cannot move a stat past it.
const maxStage = 6

// CheckAction reports every provable error in playing act from view v. It
// returns nil for a switch, for a replacement, and for any action it cannot
// prove wasteful — which is the overwhelming majority.
func CheckAction(dex *domain.Dex, v ai.View, act engine.Action) []VerifiableError {
	if act.Kind != engine.ActionMove || act.Index < 0 {
		return nil // switches are never provably wasted from the view alone
	}
	me := v.Self.Team[v.Self.Active]
	if act.Index >= len(me.Moves) {
		return nil
	}
	mv, ok := dex.Moves[me.Moves[act.Index].MoveID]
	if !ok {
		return nil
	}
	foe := v.Foe

	var out []VerifiableError
	add := func(k ErrKind, format string, args ...any) {
		out = append(out, VerifiableError{
			Turn: v.Turn, Side: v.Me, Kind: k, Detail: fmt.Sprintf(format, args...),
		})
	}

	// 1. Attacking into a type immunity. Only the type chart is consulted:
	// ability-granted immunities (Levitate, Flash Fire) are deliberately not
	// charged because the attacker may not know the ability.
	if mv.Category != domain.CatStatus && mv.Power > 0 {
		if dex.Effectiveness(mv.Type, foe.Type1, foe.Type2) == 0 {
			add(ErrImmuneAttack, "%s (%s) vs %s (%s): 0x by type",
				mv.Name, mv.Type, foe.Name, typeLabel(foe))
		}
	}

	// The rest are status moves that cannot do their one job. A move that also
	// deals damage is never counted, because the damage is a real effect.
	if mv.Category != domain.CatStatus {
		return out
	}

	// 2. Healing at full HP. Rest is excluded when the user is statused: it
	// cures, so the turn buys something even with the heal wasted.
	if selfHeal(mv) > 0 && me.HP >= me.MaxHP {
		if !isRest(mv) || me.Status == engine.StatusNone {
			add(ErrHealAtFull, "%s at %d/%d HP", mv.Name, me.HP, me.MaxHP)
		}
	}

	// 3. Inflicting a status the target already has.
	if st := primaryStatus(mv); st != "" && foe.Status != engine.StatusNone {
		if engine.StatusCond(st) != engine.StatusToxic || foe.Status != engine.StatusPoison {
			add(ErrRedundantStatus, "%s targets %s which is already %s",
				mv.Name, foe.Name, foe.Status)
		}
	}

	// 4. Boosting stats that are all pinned at +6.
	if boosts := selfBoosts(mv); len(boosts) > 0 && allAtCap(boosts, me.Stages) {
		add(ErrBoostAtCap, "%s with every boosted stat already at +%d", mv.Name, maxStage)
	}

	return out
}

func typeLabel(p engine.Pokemon) string {
	if p.Type2 == "" || p.Type2 == p.Type1 {
		return string(p.Type1)
	}
	return string(p.Type1) + "/" + string(p.Type2)
}

// selfHeal returns the fraction of max HP the move restores to its user, or 0.
// Drain is excluded on purpose: a draining move deals damage, so it is doing
// something even when the heal is capped.
func selfHeal(m domain.Move) float64 {
	if m.Self != nil && m.Self.Heal > 0 {
		return m.Self.Heal
	}
	// A self-targeting status move carries its heal on Primary.
	if m.Primary != nil && m.Primary.Heal > 0 && m.Target == domain.TargetSelf {
		return m.Primary.Heal
	}
	return 0
}

func isRest(m domain.Move) bool {
	return (m.Self != nil && m.Self.Rest) || (m.Primary != nil && m.Primary.Rest)
}

// primaryStatus returns the status a move inflicts as its guaranteed primary
// effect. Secondaries are ignored — they are chance-gated, so a move carrying
// one still has its main effect to deliver.
func primaryStatus(m domain.Move) string {
	if m.Primary == nil || m.Primary.Status == "" {
		return ""
	}
	// Chance < 100 means it can miss its own effect; not a guaranteed no-op.
	if m.Primary.Chance != 0 && m.Primary.Chance < 100 {
		return ""
	}
	return m.Primary.Status
}

// selfBoosts returns the stat boosts a move applies to its user. Only positive
// boosts count: a move that lowers its own stats as a cost is not "capped."
func selfBoosts(m domain.Move) map[string]int {
	var src map[string]int
	switch {
	case m.Self != nil && len(m.Self.Boosts) > 0:
		src = m.Self.Boosts
	case m.Primary != nil && len(m.Primary.Boosts) > 0 && m.Target == domain.TargetSelf:
		src = m.Primary.Boosts
	default:
		return nil
	}
	out := map[string]int{}
	for stat, n := range src {
		if n > 0 {
			out[stat] = n
		}
	}
	return out
}

// allAtCap reports whether every stat the move would raise is already at +6.
// If even one has room, the move does something and is not charged.
func allAtCap(boosts map[string]int, st engine.Stages) bool {
	for stat := range boosts {
		cur, known := stageOf(st, stat)
		if !known {
			return false // unrecognized stat key — never charge on a guess
		}
		if cur < maxStage {
			return false
		}
	}
	return true
}

// stageOf maps a move's boost key to the matching stat stage. The two
// vocabularies differ (see the note in backlog/2026-08-03T20-01), so this is
// deliberately explicit and returns known=false rather than defaulting.
func stageOf(st engine.Stages, stat string) (int, bool) {
	switch stat {
	case "atk", "attack":
		return st.Atk, true
	case "def", "defense":
		return st.Def, true
	case "spa", "spatk":
		return st.SpA, true
	case "spd", "spdef":
		return st.SpD, true
	case "spe", "speed":
		return st.Spe, true
	case "accuracy", "acc":
		return st.Acc, true
	case "evasion", "eva":
		return st.Eva, true
	}
	return 0, false
}

// VerifiableStats is one contestant's provable-error record over a batch. Rate
// is the headline: errors per hundred decisions, so it is comparable across
// contestants that played different numbers of games.
type VerifiableStats struct {
	Contestant string          `json:"contestant"`
	Games      int             `json:"games"`
	Decisions  int             `json:"decisions"`
	Errors     int             `json:"errors"`
	Per100     float64         `json:"errors_per_100_decisions"`
	ByKind     map[ErrKind]int `json:"by_kind"`
	// CleanGames is the count of games containing no provable error at all.
	// Reported because a rate can hide a distribution: ten errors spread over
	// ten games is a different failure from ten in one.
	CleanGames int `json:"clean_games"`
}

// ScoreVerifiable walks a battle's stored turns and returns every provable
// error the given side committed, plus the number of decisions examined.
//
// It reuses recoverActions, so it inherits the same exactness guarantee as the
// oracle path: the action it checks is the action that was actually played,
// re-derived by re-simulating the turn until the stored next state reproduces
// byte-for-byte. skipped counts turns whose action could not be recovered.
func ScoreVerifiable(dex *domain.Dex, side int, turns []StoredTurn) (errs []VerifiableError, decisions, skipped int, err error) {
	for i := 1; i < len(turns); i++ {
		var prev engine.BattleState
		if e := json.Unmarshal(turns[i-1].State, &prev); e != nil {
			return nil, decisions, skipped, e
		}
		if prev.Phase != engine.PhaseChoosing {
			continue
		}
		var next engine.BattleState
		if e := json.Unmarshal(turns[i].State, &next); e != nil {
			return nil, decisions, skipped, e
		}
		acts, ok := recoverActions(dex, turns[i-1].State, &next)
		if !ok {
			skipped++
			continue
		}
		// The view is the one the player actually decided from, so a mistake is
		// only charged on information it held.
		v := ai.MakeView(&prev, side)
		decisions++
		errs = append(errs, CheckAction(dex, v, acts[side])...)
	}
	return errs, decisions, skipped, nil
}

// AggregateVerifiable folds per-battle results into one row per contestant,
// sorted by error rate ascending (cleanest first).
func AggregateVerifiable(results []VerifiableBattle) []VerifiableStats {
	type acc struct {
		games, decisions, clean int
		errs                    []VerifiableError
	}
	byName := map[string]*acc{}
	var order []string
	for _, r := range results {
		a := byName[r.Contestant]
		if a == nil {
			a = &acc{}
			byName[r.Contestant] = a
			order = append(order, r.Contestant)
		}
		a.games++
		a.decisions += r.Decisions
		a.errs = append(a.errs, r.Errors...)
		if len(r.Errors) == 0 {
			a.clean++
		}
	}
	out := make([]VerifiableStats, 0, len(order))
	for _, name := range order {
		a := byName[name]
		s := VerifiableStats{
			Contestant: name, Games: a.games, Decisions: a.decisions,
			Errors: len(a.errs), ByKind: map[ErrKind]int{}, CleanGames: a.clean,
		}
		for _, e := range a.errs {
			s.ByKind[e.Kind]++
		}
		if a.decisions > 0 {
			s.Per100 = 100 * float64(len(a.errs)) / float64(a.decisions)
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Per100 != out[j].Per100 {
			return out[i].Per100 < out[j].Per100
		}
		return out[i].Contestant < out[j].Contestant
	})
	return out
}

// VerifiableBattle is one scored battle attributed to a contestant.
type VerifiableBattle struct {
	Contestant string
	Decisions  int
	Errors     []VerifiableError
}
