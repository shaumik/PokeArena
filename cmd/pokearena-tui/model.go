package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pokearena/internal/ai"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/protocol"
)

type screen int

const (
	screenConnecting screen = iota
	screenRoom
	screenBattle
	screenEnded
)

// model is the whole TUI state. It is a value type (bubbletea convention):
// Update returns a mutated copy.
//
// Ground-truth discipline: the latest *battleView is the authority for all
// rendered game state (HP, stages, field, whose turn) — we replace it wholesale
// every frame and never reconstruct it from deltas, because the gateway drops
// frames to slow readers, so any single view frame must stand on its own. The
// log is the one exception: it is a best-effort running transcript built by
// appending each frame's new lines (a turn can span several frames), so a
// dropped frame may leave a cosmetic gap. It is capped at maxLogLines so a long
// battle can't grow it without bound.
type model struct {
	cl       *wsClient
	send     func(protocol.WsClientMsg) error // cl.Send; injectable so tests can observe sends
	dex      *domain.Dex
	battleID string
	slot     string
	meSide   int // 0 for p1, 1 for p2; refined from view.Me once frames arrive

	screen screen

	// Battle state.
	view          *battleView
	log           []engine.LogLine
	needsAction   bool // true between an incoming state/turn frame and our next send
	autoActedTurn int  // last turn maybeAutoAct sent for, so a same-turn resync can't double-send
	spriteFrame   int  // foe idle-animation cursor; advanced by spriteTickMsg, modulo'd at render
	foeDexNo      int  // foe's dex number, resolved once per view (the foe is name-only on the wire)

	// Picker state.
	room       *protocol.RoomUpdate
	deadlineAt time.Time
	inviteURL  string // opponent invite (set only when we created the battle)
	team       []engine.TeamPick
	teamView   []teamMon
	submitting bool // a submit_team is in flight, awaiting room ack / error
	submitted  bool // the room reports our team as submitted
	submitSeq  int  // increments per submit; lets a stale timeout ignore a newer submit

	// Terminal / lifecycle.
	winner     *int
	ended      bool
	disconnErr error
	status     string // transient status / error line

	width, height int
}

func newModel(cl *wsClient, dex *domain.Dex, battleID, slot string) model {
	side := 0
	if slot == slotP2 {
		side = 1
	}
	return model{
		cl:       cl,
		send:     cl.Send,
		dex:      dex,
		battleID: battleID,
		slot:     slot,
		meSide:   side,
		screen:   screenConnecting,
		status:   "connecting…",
		// -1: "no turn auto-acted yet" without colliding with a turn number.
		autoActedTurn: -1,
	}
}

// maxLogLines bounds the running transcript so a long battle can't grow the
// log slice without limit. The screen only ever shows the tail anyway.
const maxLogLines = 500

// submitAckTimeout bounds how long we wait for the gateway to acknowledge a
// submit_team (a fresh FrameRoom with you.submitted, or a FrameError) before we
// let the user retry. Mirrors the MCP session's 5s submit ack window.
const submitAckTimeout = 5 * time.Second

// Messages injected into the bubbletea runtime.
type frameMsg frame
type disconnectMsg struct{ err error }
type tickMsg time.Time

// submitTimeoutMsg fires submitAckTimeout after a submit_team send. seq guards
// against a stale timeout clobbering a newer submit attempt.
type submitTimeoutMsg struct{ seq int }

// spriteTickMsg advances the foe's idle animation by one frame.
type spriteTickMsg struct{}

// spriteIdleDelayMs is the heartbeat of the sprite loop when there's nothing to
// animate (room/connecting screens): slow enough to be cheap, fast enough that
// the loop is warm when the battle starts.
const spriteIdleDelayMs = 250

// appendLog adds the frame's new log lines, trimming to maxLogLines.
func appendLog(log []engine.LogLine, lines []engine.LogLine) []engine.LogLine {
	log = append(log, lines...)
	if len(log) > maxLogLines {
		log = log[len(log)-maxLogLines:]
	}
	return log
}

func (m model) Init() tea.Cmd {
	return tea.Batch(waitForFrame(m.cl), tick(), spriteTickCmd(spriteIdleDelayMs))
}

// waitForFrame blocks on exactly one frame from the WS and turns it into a
// message. The Update handler re-issues it on every frameMsg, so there is
// always exactly one outstanding reader — never two racing on the channel.
func waitForFrame(cl *wsClient) tea.Cmd {
	return func() tea.Msg {
		f, ok := <-cl.Updates()
		if !ok {
			return disconnectMsg{err: <-cl.Closed()}
		}
		return frameMsg(f)
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		if m.ended {
			return m, nil
		}
		return m, tick()

	case submitTimeoutMsg:
		// Only act if this timeout belongs to the still-pending submit.
		if msg.seq == m.submitSeq && m.submitting && !m.submitted {
			m.submitting = false
			m.status = "⚠ no response to team submission — press enter to retry"
		}
		return m, nil

	case spriteTickMsg:
		// One self-rescheduling loop (started in Init), so exactly one is ever
		// outstanding. It only advances during a battle; otherwise it idles
		// cheaply, and it stops entirely once the battle ends.
		if m.ended {
			return m, nil
		}
		if m.screen == screenBattle {
			m.spriteFrame++
			return m, spriteTickCmd(m.foeFrameDelay())
		}
		return m, spriteTickCmd(spriteIdleDelayMs)

	case disconnectMsg:
		m.disconnErr = msg.err
		if !m.ended {
			m.ended = true
			m.screen = screenEnded
			if msg.err != nil {
				m.status = "connection closed: " + msg.err.Error()
			} else {
				m.status = "disconnected"
			}
		}
		return m, nil

	case frameMsg:
		return m.handleFrame(frame(msg))

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleFrame(f frame) (tea.Model, tea.Cmd) {
	next := waitForFrame(m.cl) // keep listening
	switch f.Type {
	case protocol.FrameRoom:
		if f.Room != nil {
			m.room = f.Room
			m.deadlineAt = time.Now().Add(time.Duration(f.Room.DeadlineMS) * time.Millisecond)
			if m.team == nil {
				m.team, m.teamView = autoTeam(m.dex)
			}
			if f.Room.You.Submitted {
				m.submitted = true
				m.submitting = false
			}
			if m.screen == screenConnecting {
				m.screen = screenRoom
			}
		}

	case protocol.FrameState, protocol.FrameTurn:
		if f.View != nil && !m.ended {
			if validView(f.View) {
				m.setView(f.View)
				m.needsAction = f.View.Phase != engine.PhaseEnded
				m.screen = screenBattle
				m.status = ""
				m = m.maybeAutoAct()
			} else {
				// A well-formed server view always has Active in range (engine
				// invariant); a malformed/empty frame would otherwise panic the
				// renderers, so drop it rather than promote it.
				m.status = "⚠ ignored a malformed battle view"
			}
		}
		m.log = appendLog(m.log, f.Log)

	case protocol.FrameEnd:
		if validView(f.View) {
			m.setView(f.View)
		}
		m.log = appendLog(m.log, f.Log)
		m.winner = f.Winner
		m.ended = true
		m.needsAction = false
		m.screen = screenEnded

	case protocol.FrameInfo:
		if f.Message != "" {
			m.status = f.Message
		}

	case protocol.FrameError:
		// The gateway sends a human-readable message; default it so an empty
		// error still gives the user feedback.
		msg := f.Message
		if msg == "" {
			msg = "rejected by server"
		}
		m.status = "⚠ " + msg
		switch {
		case m.submitting:
			// Our team was rejected: drop the in-flight flag so the user can
			// edit and resubmit. The stale submit timeout will no-op (it checks
			// m.submitting).
			m.submitting = false
		case m.view != nil && !m.ended:
			// An action was rejected mid-battle: it's still our turn, so
			// re-enable input rather than waiting for a resync frame.
			m.needsAction = true
		}
	}
	return m, next
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		m.cl.Close()
		return m, tea.Quit
	}
	switch m.screen {
	case screenRoom:
		return m.handleRoomKey(key)
	case screenBattle:
		return m.handleBattleKey(key)
	case screenEnded:
		if key == "q" || key == "enter" || key == "esc" {
			m.cl.Close()
			return m, tea.Quit
		}
	case screenConnecting:
		if key == "q" {
			m.cl.Close()
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m model) handleBattleKey(key string) (tea.Model, tea.Cmd) {
	if key == "q" {
		m.cl.Close()
		return m, tea.Quit
	}
	if m.view == nil || !m.needsAction {
		if key != "" {
			m.status = "waiting for the next turn…"
		}
		return m, nil
	}
	acts := ai.LegalActions(m.view.toAIView())

	switch key {
	case "1", "2", "3", "4":
		idx := int(key[0] - '1')
		if a, ok := findAction(acts, engine.ActionMove, idx); ok {
			m = m.sendAction(a)
		} else {
			m.status = "that move can't be used right now"
		}
	case "s":
		if a, ok := findAction(acts, engine.ActionMove, -1); ok { // Struggle
			m = m.sendAction(a)
		}
	default:
		if len(key) == 1 && key[0] >= 'a' && key[0] <= 'f' {
			idx := int(key[0] - 'a')
			if a, ok := findAction(acts, engine.ActionSwitch, idx); ok {
				m = m.sendAction(a)
			} else {
				m.status = "can't switch to that Pokémon"
			}
		}
	}
	return m, nil
}

func (m model) handleRoomKey(key string) (tea.Model, tea.Cmd) {
	if key == "q" {
		m.cl.Close()
		return m, tea.Quit
	}
	if m.submitted || m.submitting {
		return m, nil // locked in; nothing to change
	}
	switch key {
	case "r":
		m.team, m.teamView = autoTeam(m.dex)
		m.status = "re-rolled the whole team"
	case "1", "2", "3", "4", "5", "6":
		idx := int(key[0] - '1')
		m.team, m.teamView = rerollSlot(m.dex, m.team, m.teamView, idx)
		m.status = "re-rolled slot " + key
	case "enter":
		if err := engine.ValidateTeam(m.team, m.dex); err != nil {
			m.status = "⚠ invalid team: " + err.Error()
			return m, nil
		}
		if err := m.send(protocol.WsClientMsg{Type: protocol.MsgSubmitTeam, Picks: m.team}); err != nil {
			m.status = "⚠ submit failed: " + err.Error()
			return m, nil
		}
		m.submitting = true
		m.submitSeq++
		m.status = "submitting team…"
		return m, submitTimeoutCmd(m.submitSeq)
	}
	return m, nil
}

// sendAction writes a move/switch frame and latches input off until the next
// state frame arrives — mirroring the MCP session's needsAction discipline so
// a fast double-press can't fire two actions for one turn. It returns the
// updated model by value (matching the rest of Update) rather than mutating
// through a pointer, so there's no value/pointer-receiver subtlety.
func (m model) sendAction(a engine.Action) model {
	kind := protocol.ActionKindMove
	if a.Kind == engine.ActionSwitch {
		kind = protocol.ActionKindSwitch
	}
	if err := m.send(protocol.WsClientMsg{Type: protocol.MsgAction, Kind: kind, Index: a.Index}); err != nil {
		m.status = "⚠ send failed: " + err.Error()
		return m
	}
	m.needsAction = false
	m.status = ""
	return m
}

// forcedAction reports the turn's forced move when the player has no real
// choice: finishing a two-turn charge (Fly, Solar Beam), continuing a rampage
// (Outrage, Thrash, Petal Dance), or spending the Hyper Beam recharge turn
// (the engine's index -1 sentinel). The handheld plays these turns without a
// menu, and so do we (maybeAutoAct). A choice lock or Encore is NOT forced —
// the player may still switch out — and a replace prompt is a free pick.
func (m model) forcedAction() (int, string, bool) {
	v := m.view
	if v == nil || v.Replace {
		return 0, "", false
	}
	me := v.Self.Team[v.Self.Active]
	switch {
	case me.Volatiles.Charging != nil:
		i := me.Volatiles.Charging.MoveIdx
		return i, m.moveName(me, i) + " is mid-charge — finishing it automatically", true
	case me.Volatiles.LockedMove != nil:
		i := me.Volatiles.LockedMove.MoveIdx
		return i, "locked into " + m.moveName(me, i) + " — it continues automatically", true
	case me.Volatiles.MustRecharge:
		return -1, me.Name + " must recharge — the turn passes automatically", true
	}
	return 0, "", false
}

// maybeAutoAct submits a forced turn without prompting. Before it existed the
// menu made the player pick the one legal option — and rendered the recharge
// sentinel as "Struggle", which read as a bug. Guarded by autoActedTurn so a
// same-turn resync frame can't double-send, and cross-checked against
// LegalActions so a volatile/engine disagreement falls back to the menu
// rather than sending an illegal action.
func (m model) maybeAutoAct() model {
	if !m.needsAction || m.view == nil || m.view.Turn == m.autoActedTurn {
		return m
	}
	idx, why, ok := m.forcedAction()
	if !ok {
		return m
	}
	a, ok := findAction(ai.LegalActions(m.view.toAIView()), engine.ActionMove, idx)
	if !ok {
		return m
	}
	m.autoActedTurn = m.view.Turn
	m = m.sendAction(a)
	if m.status == "" { // sendAction failure keeps its own error visible
		m.status = why
	}
	return m
}

// moveName resolves a move slot to its display name; the recharge sentinel
// (-1) and anything out of range fall back generically.
func (m model) moveName(p engine.Pokemon, idx int) string {
	if idx < 0 || idx >= len(p.Moves) {
		return "the move"
	}
	if m.dex != nil {
		if mv, ok := m.dex.Moves[p.Moves[idx].MoveID]; ok && mv.Name != "" {
			return mv.Name
		}
	}
	return p.Moves[idx].MoveID
}

// submitTimeoutCmd schedules a submitTimeoutMsg for the given submit sequence.
func submitTimeoutCmd(seq int) tea.Cmd {
	return tea.Tick(submitAckTimeout, func(time.Time) tea.Msg { return submitTimeoutMsg{seq: seq} })
}

// spriteTickCmd schedules the next sprite frame after delayMs.
func spriteTickCmd(delayMs int) tea.Cmd {
	return tea.Tick(time.Duration(delayMs)*time.Millisecond, func(time.Time) tea.Msg { return spriteTickMsg{} })
}

// validView reports whether a decoded view is safe to render: a well-formed
// server view always has the active index in range (an engine invariant), so a
// frame that fails this is malformed/empty and must not reach the renderers.
func validView(v *battleView) bool {
	return v != nil && v.Self.Active >= 0 && v.Self.Active < len(v.Self.Team)
}

// setView installs a validated view and refreshes the derived foe dex number
// (the foe is name-only on the wire) so the foe sprite and its animation timing
// don't rescan the dex on every render/tick.
func (m *model) setView(v *battleView) {
	m.view = v
	m.meSide = v.Me
	m.foeDexNo, _ = dexNoByName(m.dex, v.Foe.Name)
}

// foeFrameDelay is how long the foe's current animation frame should display,
// taken from the GIF's own per-frame timing so the wiggle keeps its cadence.
func (m model) foeFrameDelay() int {
	if m.foeDexNo > 0 {
		front, _ := m.spriteSizes()
		if sp := foeSprite(m.foeDexNo, front); sp != nil {
			return sp.delayMs(m.spriteFrame)
		}
	}
	return spriteIdleDelayMs
}

// spriteSizes chooses front/back sprite pixel sizes that fit the current
// terminal, returning 0,0 to mean "no room — draw the stat boxes alone". The
// two sprites stack vertically in the arena and share the row budget left after
// the rest of the screen (header, field, log box, controls); the foe gets the
// larger share. The minimum (12px ≈ 6 rows) is about the stat box's own height,
// so a minimal sprite sits beside the box without adding height. Each size is
// clamped even within [min, near-native] and capped to leave the stat box its
// width. Before the first WindowSizeMsg (width/height 0) it assumes a roomy
// window so the opening frame isn't tiny.
func (m model) spriteSizes() (front, back int) {
	h, w := m.height, m.width
	if h <= 0 {
		h = 48
	}
	if w <= 0 {
		w = 100
	}
	// Rows the non-arena chrome consumes: the log box scales with logLines, the
	// rest (header, blanks, field, the boxed action menu, status) is roughly
	// constant.
	budget := h - (m.logLines() + 16)
	if budget < 12 {
		budget = 12 // box-dominated floor; minimal sprites add no height here
	}
	foeRows := budget * 9 / 16
	front = clampEven(foeRows*2, 12, frontPx+8)
	back = clampEven((budget-foeRows)*2, 12, backPx)
	// Leave ~28 cols for the stat box + a 2-col gap. If even a minimal sprite
	// won't fit beside it, drop the sprite column entirely.
	maxPx := w - 30
	if maxPx < 12 {
		return 0, 0
	}
	front = min(front, clampEven(maxPx, 12, frontPx+8))
	back = min(back, clampEven(maxPx, 12, backPx))
	return front, back
}

// logLines is how many transcript lines the log box shows — fewer on a short
// terminal so the box doesn't crowd the arena off-screen.
func (m model) logLines() int {
	n := logTail
	if m.height > 0 {
		n = m.height / 5
	}
	if n < 3 {
		return 3
	}
	if n > logTail {
		return logTail
	}
	return n
}

// clampEven clamps v to [lo, hi] and rounds down to even (half-block pairs need
// an even pixel height), without dropping below lo.
func clampEven(v, lo, hi int) int {
	if v > hi {
		v = hi
	}
	if v < lo {
		v = lo
	}
	if v%2 != 0 {
		v--
		if v < lo {
			v += 2
		}
	}
	return v
}

func findAction(acts []engine.Action, kind engine.ActionKind, index int) (engine.Action, bool) {
	for _, a := range acts {
		if a.Kind == kind && a.Index == index {
			return a, true
		}
	}
	return engine.Action{}, false
}
