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
	dex      *domain.Dex
	battleID string
	slot     string
	meSide   int // 0 for p1, 1 for p2; refined from view.Me once frames arrive

	screen screen

	// Battle state.
	view        *battleView
	log         []engine.LogLine
	needsAction bool // true between an incoming state/turn frame and our next send
	spriteFrame int  // foe idle-animation cursor; advanced by spriteTickMsg, modulo'd at render

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
		dex:      dex,
		battleID: battleID,
		slot:     slot,
		meSide:   side,
		screen:   screenConnecting,
		status:   "connecting…",
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
			m.view = f.View
			m.meSide = f.View.Me
			m.needsAction = f.View.Phase != engine.PhaseEnded
			m.screen = screenBattle
			m.status = ""
		}
		m.log = appendLog(m.log, f.Log)

	case protocol.FrameEnd:
		if f.View != nil {
			m.view = f.View
			m.meSide = f.View.Me
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
		if err := m.cl.Send(protocol.WsClientMsg{Type: protocol.MsgSubmitTeam, Picks: m.team}); err != nil {
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
	if err := m.cl.Send(protocol.WsClientMsg{Type: protocol.MsgAction, Kind: kind, Index: a.Index}); err != nil {
		m.status = "⚠ send failed: " + err.Error()
		return m
	}
	m.needsAction = false
	m.status = ""
	return m
}

// submitTimeoutCmd schedules a submitTimeoutMsg for the given submit sequence.
func submitTimeoutCmd(seq int) tea.Cmd {
	return tea.Tick(submitAckTimeout, func(time.Time) tea.Msg { return submitTimeoutMsg{seq: seq} })
}

// spriteTickCmd schedules the next sprite frame after delayMs.
func spriteTickCmd(delayMs int) tea.Cmd {
	return tea.Tick(time.Duration(delayMs)*time.Millisecond, func(time.Time) tea.Msg { return spriteTickMsg{} })
}

// foeFrameDelay is how long the foe's current animation frame should display,
// taken from the GIF's own per-frame timing so the wiggle keeps its cadence.
func (m model) foeFrameDelay() int {
	if m.view != nil {
		if dexNo, ok := dexNoByName(m.dex, m.view.Foe.Name); ok {
			if sp := foeSprite(dexNo); sp != nil {
				return sp.delayMs(m.spriteFrame)
			}
		}
	}
	return spriteIdleDelayMs
}

func findAction(acts []engine.Action, kind engine.ActionKind, index int) (engine.Action, bool) {
	for _, a := range acts {
		if a.Kind == kind && a.Index == index {
			return a, true
		}
	}
	return engine.Action{}, false
}
