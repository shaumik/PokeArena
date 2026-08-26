package main

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/shaumik/PokeArena/internal/ai"
	"github.com/shaumik/PokeArena/internal/domain"
	"github.com/shaumik/PokeArena/internal/engine"
	"github.com/shaumik/PokeArena/internal/eval"
)

// hardDecisionCap bounds a single episode so a non-terminating battle (an
// engine bug, or two controllers that stall forever) fails loudly instead of
// hanging the client. Same value and same reasoning as eval.maxDecisions.
const hardDecisionCap = 20000

// Reward modes.
const (
	rewardWinLoss = "win_loss" // 0 every step; +1/-1/0 at the terminal step
	rewardHPDelta = "hp_delta" // dense shaping; see episode.rewards
)

// controller is one side's pilot for an episode.
type controller struct {
	label    string
	external bool
	agent    ai.Agent
}

// episode is one battle in progress. There is at most one per process: the
// binary is a single environment instance, and parallelism is the client's job
// (run N processes), which keeps every episode's RNG stream trivially isolated.
type episode struct {
	dex   *domain.Dex
	state *engine.BattleState

	ctrl       [2]controller
	seed       uint64
	battleID   string
	teamLabels [2]string

	reward       string
	budget       time.Duration
	maxTurns     int // client-imposed truncation; 0 = only the engine's own 300-turn cap
	maxDecisions int

	decisions int
	truncated bool

	// hpFrac is each side's team HP fraction as of the last emitted step, the
	// baseline the hp_delta shaping differences against.
	hpFrac [2]float64
	// pendingEvents accumulates the engine log across every decision point
	// resolved inside one client-visible step (a step may auto-advance through
	// decision points that need no external input).
	pendingEvents []engine.LogLine
	// fallback records, per side, whether a built-in baseline's proposal had to
	// be replaced by a legal one at the most recent decision point.
	fallback [2]bool
}

// --- request payloads -----------------------------------------------------

type resetArgs struct {
	Seed         *uint64  `json:"seed,omitempty"`
	Team         TeamSpec `json:"team"`
	OpponentTeam TeamSpec `json:"opponent_team,omitempty"`
	// Agents names the pilot for each side, index 0 and 1. Defaults to
	// ["external","heuristic"] — the single-agent Gymnasium shape, against the
	// strongest programmatic baseline on the board.
	Agents          []string `json:"agents,omitempty"`
	ExpectimaxDepth int      `json:"expectimax_depth,omitempty"`
	Reward          string   `json:"reward,omitempty"`
	MaxTurns        int      `json:"max_turns,omitempty"`
	MaxDecisions    int      `json:"max_decisions,omitempty"`
	BudgetMs        int      `json:"budget_ms,omitempty"`
	BattleID        string   `json:"battle_id,omitempty"`
}

type stepArgs struct {
	// Action is the shorthand for the common case: exactly one external side
	// has to move.
	Action *ActionInput `json:"action,omitempty"`
	// Actions is the general form: a 2-element array indexed by side, with
	// null for a side that is not external or does not have to move.
	Actions []*ActionInput `json:"actions,omitempty"`
}

type sideArgs struct {
	Side *int `json:"side,omitempty"`
}

// --- response payloads ----------------------------------------------------

// StepResult is what reset and step return. Every per-side field is a
// 2-element array indexed by side, with null for a side this call says nothing
// about — which is the mechanism that keeps fog-of-war honest in the
// single-agent case: side 1's observation is simply not in the bytes.
type StepResult struct {
	Turn         int                `json:"turn"`
	Phase        engine.Phase       `json:"phase"`
	ToMove       []int              `json:"to_move"`
	Observations [2]json.RawMessage `json:"observations"`
	LegalActions [2][]LegalAction   `json:"legal_actions"`
	ActionMask   [2][]int           `json:"action_mask"`
	Rewards      [2]float64         `json:"rewards"`
	Terminated   bool               `json:"terminated"`
	Truncated    bool               `json:"truncated"`
	Winner       int                `json:"winner"` // -1 ongoing, 0/1 side, 2 draw
	Events       []engine.LogLine   `json:"events"`
	Info         StepInfo           `json:"info"`
}

// StepInfo is the non-observation metadata. Nothing here is hidden state: the
// per-side state hashes fingerprint the *already redacted* observation bytes,
// and the rest is configuration the client supplied itself.
type StepInfo struct {
	DecisionIndex int       `json:"decision_index"`
	Seed          uint64    `json:"seed"`
	BattleID      string    `json:"battle_id"`
	Teams         [2]string `json:"teams"`
	Agents        [2]string `json:"agents"`
	StateHash     [2]string `json:"state_hash"`
	// Fallback[i] is true when side i's built-in baseline proposed something
	// illegal at the last decision point and was replaced by the first legal
	// action — the same measurable failure mode cmd/bench records.
	Fallback [2]bool `json:"fallback"`
	// TurnLimit is the effective truncation cap in turns (client cap, or the
	// engine's own maxTurns when the client set none).
	TurnLimit int `json:"turn_limit"`
}

// --- construction ---------------------------------------------------------

// newEpisode builds a battle from a reset request. It is a pure function of the
// arguments plus the dataset: same args, same episode, byte for byte.
func newEpisode(dex *domain.Dex, lib *eval.TeamLibrary, defaultDepth int, a resetArgs) (*episode, *ErrorObject) {
	seed := uint64(0)
	if a.Seed != nil {
		seed = *a.Seed
	}

	if a.Team.IsZero() {
		return nil, errorf(ErrBadRequest, `reset needs a "team" (a library name, a list of dex numbers, or {"picks":[…]})`)
	}
	own, ownLabel, err := a.Team.resolve(dex, lib)
	if err != nil {
		return nil, errorf(ErrBadRequest, "team: %v", err)
	}
	foe, foeLabel := own, ownLabel
	if !a.OpponentTeam.IsZero() {
		foe, foeLabel, err = a.OpponentTeam.resolve(dex, lib)
		if err != nil {
			return nil, errorf(ErrBadRequest, "opponent_team: %v", err)
		}
	}

	depth := defaultDepth
	if a.ExpectimaxDepth > 0 {
		depth = a.ExpectimaxDepth
	}
	names := []string{externalController, "heuristic"}
	if len(a.Agents) > 0 {
		if len(a.Agents) != 2 {
			return nil, errorf(ErrBadRequest, `"agents" must have exactly 2 entries (one per side), got %d`, len(a.Agents))
		}
		names = a.Agents
	}

	reward := a.Reward
	if reward == "" {
		reward = rewardWinLoss
	}
	if reward != rewardWinLoss && reward != rewardHPDelta {
		return nil, errorf(ErrBadRequest, `"reward" must be %q or %q, got %q`, rewardWinLoss, rewardHPDelta, reward)
	}

	id := a.BattleID
	if id == "" {
		// The same battle id cmd/bench uses. The id is cosmetic to the engine,
		// but matching it keeps a trajectory recorded here textually identical
		// to the same game recorded by the benchmark.
		id = fmt.Sprintf("eval-%d", seed)
	}

	st, berr := engine.NewBattleFromPicks(dex, id, "P0", own, "P1", foe, seed)
	if berr != nil {
		return nil, errorf(ErrBadRequest, "new battle: %v", berr)
	}

	ep := &episode{
		dex:          dex,
		state:        st,
		seed:         seed,
		battleID:     id,
		teamLabels:   [2]string{ownLabel, foeLabel},
		reward:       reward,
		budget:       time.Duration(a.BudgetMs) * time.Millisecond,
		maxTurns:     a.MaxTurns,
		maxDecisions: a.MaxDecisions,
	}
	if ep.maxDecisions <= 0 || ep.maxDecisions > hardDecisionCap {
		ep.maxDecisions = hardDecisionCap
	}

	// Side 1's agent is salted exactly the way eval.resolvedGame salts it, so a
	// baseline-vs-baseline episode reproduces the benchmark's game.
	seeds := [2]uint64{seed, seed ^ sideSalt}
	for i := 0; i < 2; i++ {
		label, factory, err := makeController(names[i], dex, depth)
		if err != nil {
			return nil, errorf(ErrBadRequest, "agents[%d]: %v", i, err)
		}
		ep.ctrl[i] = controller{label: label, external: factory == nil}
		if factory != nil {
			ep.ctrl[i].agent = factory(seeds[i])
		}
	}
	ep.hpFrac = [2]float64{ep.teamHP(0), ep.teamHP(1)}
	return ep, nil
}

// --- driving --------------------------------------------------------------

// toMove lists the sides that owe an action at the current decision point.
func (ep *episode) toMove() []int {
	if ep.done() {
		return nil
	}
	var out []int
	switch ep.state.Phase {
	case engine.PhaseChoosing:
		out = []int{0, 1}
	case engine.PhaseReplace:
		for side := 0; side < 2; side++ {
			if ep.state.Replace[side] {
				out = append(out, side)
			}
		}
	}
	return out
}

// externalToMove is the subset of toMove the client has to answer for.
func (ep *episode) externalToMove() []int {
	var out []int
	for _, side := range ep.toMove() {
		if ep.ctrl[side].external {
			out = append(out, side)
		}
	}
	return out
}

func (ep *episode) done() bool { return ep.state.Ended() || ep.truncated }

// start advances a freshly built episode to its first client-visible decision
// point. With at least one external side that is a no-op (the opening choosing
// phase already needs the client); with none, it plays the whole battle, which
// is the baseline-vs-baseline reproduction mode.
func (ep *episode) start() *ErrorObject {
	return ep.autoAdvance()
}

// step consumes the client's actions for the current decision point, resolves
// it, and then auto-advances through any following decision points that need
// no external input (a lone baseline replacement after a faint, for instance).
func (ep *episode) step(supplied map[int]engine.Action) *ErrorObject {
	if ep.done() {
		return errorf(ErrEpisodeOver, "episode already finished (terminated=%v truncated=%v); call reset", ep.state.Ended(), ep.truncated)
	}
	ep.pendingEvents = nil

	need := ep.externalToMove()
	// Validate everything before mutating anything, so a rejected action leaves
	// the episode exactly where it was and the client can simply retry.
	for _, side := range need {
		act, ok := supplied[side]
		if !ok {
			return errorf(ErrBadRequest, "side %d must act this step but no action was supplied (to_move=%v)", side, ep.toMove())
		}
		if e := ep.checkLegal(side, act); e != nil {
			return e
		}
	}
	for side := range supplied {
		if side < 0 || side > 1 {
			return errorf(ErrBadRequest, "action for side %d: sides are 0 and 1", side)
		}
		if !contains(need, side) {
			return errorf(ErrBadRequest, "action supplied for side %d, which is not an external side to move (to_move=%v)", side, ep.externalToMove())
		}
	}

	if e := ep.resolveOnce(supplied); e != nil {
		return e
	}
	return ep.autoAdvance()
}

// autoAdvance resolves decision points for as long as no external side has to
// act, stopping at termination or truncation.
func (ep *episode) autoAdvance() *ErrorObject {
	for !ep.done() && len(ep.externalToMove()) == 0 {
		if e := ep.resolveOnce(nil); e != nil {
			return e
		}
	}
	return nil
}

// resolveOnce advances the battle by exactly one decision point. Sides listed
// in supplied use the client's action; every other side that owes one is asked
// of its built-in baseline, in side order, which is the order eval.RunGame uses
// and therefore the order a stochastic baseline's RNG stream expects.
func (ep *episode) resolveOnce(supplied map[int]engine.Action) *ErrorObject {
	if ep.decisions >= ep.maxDecisions {
		ep.truncated = true
		return nil
	}
	s := ep.state
	ep.fallback = [2]bool{}

	switch s.Phase {
	case engine.PhaseChoosing:
		var acts [2]engine.Action
		for side := 0; side < 2; side++ {
			a, e := ep.actionFor(side, supplied)
			if e != nil {
				return e
			}
			acts[side] = a
		}
		ep.pendingEvents = append(ep.pendingEvents, engine.ResolveTurn(ep.dex, s, acts)...)

	case engine.PhaseReplace:
		var sw [2]*engine.Action
		for side := 0; side < 2; side++ {
			if !s.Replace[side] {
				continue
			}
			a, e := ep.actionFor(side, supplied)
			if e != nil {
				return e
			}
			chosen := a
			sw[side] = &chosen
		}
		ep.pendingEvents = append(ep.pendingEvents, engine.ResolveReplace(s, sw)...)

	default:
		return errorf(ErrInternal, "battle %s in unexpected phase %q", ep.battleID, s.Phase)
	}

	ep.decisions++
	if !s.Ended() && ep.maxTurns > 0 && s.Turn >= ep.maxTurns {
		ep.truncated = true
	}
	if ep.decisions >= ep.maxDecisions && !s.Ended() {
		ep.truncated = true
	}
	return nil
}

// actionFor produces one side's action at the current decision point.
func (ep *episode) actionFor(side int, supplied map[int]engine.Action) (engine.Action, *ErrorObject) {
	if ep.ctrl[side].external {
		a, ok := supplied[side]
		if !ok {
			return engine.Action{}, errorf(ErrBadRequest, "no action supplied for external side %d", side)
		}
		return a, nil
	}
	return ep.baselineAction(side), nil
}

// baselineAction asks a built-in agent for its move, over the same fog-of-war
// View an external client gets. An error or an illegal proposal is replaced by
// the first legal action and flagged — identical to eval.RunGame's behavior,
// which is what keeps the two drivers' trajectories equal.
func (ep *episode) baselineAction(side int) engine.Action {
	v := ai.MakeView(ep.state, side)
	legal := ai.LegalActions(v)

	ctx := context.Background()
	if ep.budget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, ep.budget)
		defer cancel()
	}

	act, err := ep.ctrl[side].agent.Decide(ctx, v)
	if err != nil || !isLegal(legal, act) {
		if len(legal) > 0 {
			act = legal[0]
		}
		ep.fallback[side] = true
	}
	return act
}

// checkLegal rejects an action the engine would not accept. Rejection is an
// error rather than a silent substitution: a client that sends an illegal
// action has a bug, and quietly playing something else for it would corrupt
// both its training signal and its trajectory.
func (ep *episode) checkLegal(side int, act engine.Action) *ErrorObject {
	if engine.ActionAllowed(ep.dex, ep.state, side, act) {
		return nil
	}
	legal := ep.legalFor(side)
	return &ErrorObject{
		Code: ErrIllegalAction,
		Message: fmt.Sprintf("action %s is not legal for side %d at turn %d (phase %s)",
			describeAction(act), side, ep.state.Turn, ep.state.Phase),
		Details: map[string]any{
			"side":          side,
			"legal_actions": legal,
			"action_mask":   maskOf(legal),
		},
	}
}

func isLegal(legal []engine.Action, a engine.Action) bool {
	for _, l := range legal {
		if l.Equal(a) {
			return true
		}
	}
	return false
}

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// --- projection -----------------------------------------------------------

// observation renders one side's fog-of-war view as wire bytes.
//
// This does not reimplement the projection: it marshals ai.View, whose
// MarshalJSON is the single redaction path the MCP server and the PvP
// WebSocket also serialize through. The opponent's bench is absent by
// construction (MakeView only ever copies the foe's active Pokémon), and the
// active foe loses exact HP, ability, item, stats, EVs/IVs/nature and move PP
// on the way out. Reusing it is deliberate: a second implementation of
// fog-of-war is a second thing that can silently stop matching.
func (ep *episode) observation(side int) (json.RawMessage, error) {
	b, err := json.Marshal(ai.MakeView(ep.state, side))
	if err != nil {
		return nil, fmt.Errorf("marshal view for side %d: %w", side, err)
	}
	return json.RawMessage(b), nil
}

// legalFor enumerates a side's legal actions from that side's own View, so the
// set is exactly what a client holding only the observation could have derived.
func (ep *episode) legalFor(side int) []LegalAction {
	v := ai.MakeView(ep.state, side)
	acts := ai.LegalActionsDex(ep.dex, v)
	out := make([]LegalAction, 0, len(acts))
	for _, a := range acts {
		out = append(out, LegalAction{
			Index:  encodeFlat(a),
			Action: a,
			Label:  ep.labelAction(v, a),
		})
	}
	return out
}

// maskOf renders a legal set as a fixed-width 0/1 mask over the discrete action
// space — the shape RL libraries and PettingZoo's action-mask convention want.
func maskOf(legal []LegalAction) []int {
	mask := make([]int, FlatActionCount)
	for _, la := range legal {
		if la.Index >= 0 && la.Index < FlatActionCount {
			mask[la.Index] = 1
		}
	}
	return mask
}

// labelAction renders an action the way a human (or an LLM) reads it, using
// only what is in that side's own View.
func (ep *episode) labelAction(v ai.View, a engine.Action) string {
	switch a.Kind {
	case engine.ActionSwitch:
		if a.Index >= 0 && a.Index < len(v.Self.Team) {
			return "switch to " + v.Self.Team[a.Index].Name
		}
		return fmt.Sprintf("switch to slot %d", a.Index)
	case engine.ActionMove:
		if a.Index == engine.StruggleMoveIndex {
			return "Struggle (no usable move)"
		}
		act := v.Self.Team[v.Self.Active]
		if a.Index >= 0 && a.Index < len(act.Moves) {
			slot := act.Moves[a.Index]
			name := slot.MoveID
			if m, ok := ep.dex.Moves[slot.MoveID]; ok && m.Name != "" {
				name = m.Name
			}
			label := fmt.Sprintf("use %s (%d/%d PP)", name, slot.PP, slot.MaxPP)
			if a.SwitchTarget != nil && *a.SwitchTarget < len(v.Self.Team) {
				label += ", pivot to " + v.Self.Team[*a.SwitchTarget].Name
			}
			return label
		}
	}
	return describeAction(a)
}

func describeAction(a engine.Action) string {
	if a.SwitchTarget != nil {
		return fmt.Sprintf("{kind=%s index=%d switch_target=%d}", a.Kind, a.Index, *a.SwitchTarget)
	}
	return fmt.Sprintf("{kind=%s index=%d}", a.Kind, a.Index)
}

// --- results --------------------------------------------------------------

// teamHP is a side's remaining team HP as a fraction of its maximum.
func (ep *episode) teamHP(side int) float64 {
	var hp, max int
	for _, p := range ep.state.Sides[side].Team {
		hp += p.HP
		max += p.MaxHP
	}
	if max == 0 {
		return 0
	}
	return float64(hp) / float64(max)
}

// rewards computes each side's reward for the step just taken.
//
// win_loss is the honest default: the battle's only real objective, +1/-1/0 at
// the terminal step and zero everywhere else. hp_delta adds dense shaping —
// the change in (own team HP fraction − foe team HP fraction) since the last
// step — and is opt-in because it reads privileged state: the foe's exact team
// HP is not in any observation. That asymmetry is normal for a training signal
// and dishonest in an observation, which is why it lives here and not there.
func (ep *episode) rewards() [2]float64 {
	var r [2]float64
	if ep.reward == rewardHPDelta {
		now := [2]float64{ep.teamHP(0), ep.teamHP(1)}
		d0 := (now[0] - ep.hpFrac[0]) - (now[1] - ep.hpFrac[1])
		r[0], r[1] = d0, -d0
		ep.hpFrac = now
	}
	if ep.state.Ended() {
		switch ep.state.Winner {
		case 0:
			r[0] += 1
			r[1] += -1
		case 1:
			r[0] += -1
			r[1] += 1
		}
	}
	return r
}

// result builds the client-visible snapshot. observeSides names which sides get
// an observation: the external sides that owe an action, or — once the episode
// is over — every external side, so each one sees its final state.
func (ep *episode) result() (*StepResult, *ErrorObject) {
	res := &StepResult{
		Turn:       ep.state.Turn,
		Phase:      ep.state.Phase,
		ToMove:     ep.toMove(),
		Terminated: ep.state.Ended(),
		Truncated:  ep.truncated,
		Winner:     ep.state.Winner,
		Rewards:    ep.rewards(),
		Events:     ep.pendingEvents,
		Info: StepInfo{
			DecisionIndex: ep.decisions,
			Seed:          ep.seed,
			BattleID:      ep.battleID,
			Teams:         ep.teamLabels,
			Agents:        [2]string{ep.ctrl[0].label, ep.ctrl[1].label},
			Fallback:      ep.fallback,
			TurnLimit:     ep.effectiveTurnLimit(),
		},
	}
	if res.Events == nil {
		res.Events = []engine.LogLine{}
	}
	if res.ToMove == nil {
		res.ToMove = []int{}
	}

	observe := ep.externalToMove()
	if ep.done() {
		observe = nil
		for side := 0; side < 2; side++ {
			if ep.ctrl[side].external {
				observe = append(observe, side)
			}
		}
	}
	for _, side := range observe {
		obs, err := ep.observation(side)
		if err != nil {
			return nil, errorf(ErrInternal, "%v", err)
		}
		res.Observations[side] = obs
		res.Info.StateHash[side] = hashBytes(obs)
		if !ep.done() {
			legal := ep.legalFor(side)
			res.LegalActions[side] = legal
			res.ActionMask[side] = maskOf(legal)
		}
	}
	return res, nil
}

// effectiveTurnLimit reports the cap that will actually truncate this episode:
// the client's, when it set one below the engine's own.
func (ep *episode) effectiveTurnLimit() int {
	const engineMaxTurns = 300 // engine/turn.go maxTurns; unexported there
	if ep.maxTurns > 0 && ep.maxTurns < engineMaxTurns {
		return ep.maxTurns
	}
	return engineMaxTurns
}

// hashBytes fingerprints observation bytes with FNV-1a, rendered the same way
// eval.hashView renders a decision-point fingerprint — and over the same input,
// since eval hashes the marshaled ai.View too. Equal hashes across two runs
// mean byte-identical decision states, which is the reproducibility check.
func hashBytes(b []byte) string {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return fmt.Sprintf("%016x", h.Sum64())
}
