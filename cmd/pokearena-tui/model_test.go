package main

import (
	"strings"
	"testing"

	"pokearena"
	"pokearena/internal/domain"
	"pokearena/internal/engine"
	"pokearena/internal/protocol"
)

// These tests drive Update directly. handleFrame builds (but never runs) a
// waitForFrame command, so a nil client is fine — the closure is only invoked
// by the bubbletea runtime, which the tests don't start.

func TestSubmitTimeoutClearsStalePendingSubmit(t *testing.T) {
	m := newModel(nil, nil, "b", "p1")
	m.screen = screenRoom
	m.submitting = true
	m.submitSeq = 3

	out, _ := m.Update(submitTimeoutMsg{seq: 3})
	mm := out.(model)
	if mm.submitting {
		t.Error("a matching timeout must clear the in-flight submit so the user can retry")
	}
	if !strings.Contains(mm.status, "retry") {
		t.Errorf("expected a retry prompt, got %q", mm.status)
	}
}

func TestSubmitTimeoutIgnoresStaleSeq(t *testing.T) {
	m := newModel(nil, nil, "b", "p1")
	m.submitting = true
	m.submitSeq = 5 // a newer submit is in flight

	out, _ := m.Update(submitTimeoutMsg{seq: 4}) // timeout from an older submit
	mm := out.(model)
	if !mm.submitting {
		t.Error("a stale timeout must not cancel a newer submit attempt")
	}
}

func TestSubmitTimeoutNoopAfterAck(t *testing.T) {
	m := newModel(nil, nil, "b", "p1")
	m.submitSeq = 2
	m.submitting = false
	m.submitted = true

	out, _ := m.Update(submitTimeoutMsg{seq: 2})
	mm := out.(model)
	if !mm.submitted || mm.submitting {
		t.Error("a timeout arriving after acceptance must not disturb the submitted state")
	}
}

func TestFrameErrorWhileSubmittingClearsAndDefaultsMessage(t *testing.T) {
	m := newModel(nil, nil, "b", "p1")
	m.submitting = true

	out, _ := m.Update(frameMsg(frame{Type: protocol.FrameError, Message: ""}))
	mm := out.(model)
	if mm.submitting {
		t.Error("FrameError during submit must clear the in-flight flag for retry")
	}
	if !strings.Contains(strings.ToLower(mm.status), "reject") {
		t.Errorf("empty FrameError should still produce feedback, got %q", mm.status)
	}
}

func TestFrameErrorMidBattleReenablesInput(t *testing.T) {
	m := newModel(nil, nil, "b", "p1")
	m.view = &battleView{}
	m.needsAction = false // latched off after a send

	out, _ := m.Update(frameMsg(frame{Type: protocol.FrameError, Message: "illegal move"}))
	mm := out.(model)
	if !mm.needsAction {
		t.Error("an action rejected mid-battle is still our turn — input must re-enable")
	}
}

func TestSpriteTickAdvancesInBattle(t *testing.T) {
	m := newModel(nil, nil, "b", "p1")
	m.screen = screenBattle
	m.spriteFrame = 4

	out, cmd := m.Update(spriteTickMsg{})
	mm := out.(model)
	if mm.spriteFrame != 5 {
		t.Errorf("spriteFrame = %d, want 5 (advanced one frame)", mm.spriteFrame)
	}
	if cmd == nil {
		t.Error("battle sprite tick must reschedule itself to keep animating")
	}
}

func TestSpriteTickIdlesOutsideBattle(t *testing.T) {
	m := newModel(nil, nil, "b", "p1")
	m.screen = screenRoom
	m.spriteFrame = 9

	out, cmd := m.Update(spriteTickMsg{})
	mm := out.(model)
	if mm.spriteFrame != 9 {
		t.Errorf("spriteFrame = %d, want 9 (must not advance off the battle screen)", mm.spriteFrame)
	}
	if cmd == nil {
		t.Error("idle sprite tick must still reschedule so the loop stays alive for the battle")
	}
}

func TestSpriteTickStopsWhenEnded(t *testing.T) {
	m := newModel(nil, nil, "b", "p1")
	m.ended = true

	_, cmd := m.Update(spriteTickMsg{})
	if cmd != nil {
		t.Error("the sprite loop must stop once the battle has ended")
	}
}

func TestSpriteSizesAdaptAndStayValid(t *testing.T) {
	for _, c := range []struct{ w, h int }{{0, 0}, {80, 24}, {120, 60}, {100, 40}, {64, 20}} {
		m := newModel(nil, nil, "b", "p1")
		m.width, m.height = c.w, c.h
		f, b := m.spriteSizes()
		if f%2 != 0 || b%2 != 0 {
			t.Errorf("%dx%d: sizes must be even, got front=%d back=%d", c.w, c.h, f, b)
		}
		// Each size is either dropped (0) or within [12, near-native].
		if f != 0 && (f < 12 || f > frontPx+8) {
			t.Errorf("%dx%d: front %d out of {0}∪[12,%d]", c.w, c.h, f, frontPx+8)
		}
		if b != 0 && (b < 12 || b > backPx) {
			t.Errorf("%dx%d: back %d out of {0}∪[12,%d]", c.w, c.h, b, backPx)
		}
	}
	// A taller terminal must not produce a smaller foe sprite than a short one.
	short := newModel(nil, nil, "b", "p1")
	short.width, short.height = 100, 24
	tall := newModel(nil, nil, "b", "p1")
	tall.width, tall.height = 100, 60
	sf, _ := short.spriteSizes()
	tf, _ := tall.spriteSizes()
	if tf < sf {
		t.Errorf("taller terminal shrank the foe sprite: %d < %d", tf, sf)
	}
	// A terminal too narrow for a sprite beside the stat box drops both.
	narrow := newModel(nil, nil, "b", "p1")
	narrow.width, narrow.height = 30, 40
	if f, b := narrow.spriteSizes(); f != 0 || b != 0 {
		t.Errorf("narrow terminal should drop sprites, got front=%d back=%d", f, b)
	}
}

// TestBattleScreenFitsCommonTerminals guards the overflow fix: on terminals a
// player would actually maximize, the rendered battle screen must not exceed the
// window height (bubbletea keeps the bottom rows, so overflow scrolls the foe's
// HP and the header off the top).
func TestBattleScreenFitsCommonTerminals(t *testing.T) {
	dex, err := domain.LoadDexFS(pokearena.DataFS(), "gen1-v1")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	m := newModel(nil, dex, "battle123", "p1")
	m.setView(decodeBattleFrame(t, dex)) // resolves foeDexNo so the foe sprite is included
	m.screen = screenBattle
	m.needsAction = true
	m.log = []engine.LogLine{{Side: -1, Text: "Battle started!"}, {Side: 0, Text: "Venusaur used Razor Leaf!"}}
	for _, c := range []struct{ w, h int }{{100, 30}, {100, 40}, {120, 50}, {120, 55}} {
		m.width, m.height = c.w, c.h
		got := strings.Count(m.View(), "\n") + 1
		if got > c.h {
			t.Errorf("%dx%d: rendered %d lines, exceeds terminal height", c.w, c.h, got)
		}
	}
}

func TestMalformedViewIsIgnored(t *testing.T) {
	m := newModel(nil, nil, "b", "p1")
	bad := &battleView{Me: 0} // empty Self.Team, Active 0 -> out of range
	out, _ := m.Update(frameMsg(frame{Type: protocol.FrameState, View: bad}))
	mm := out.(model)
	if mm.screen == screenBattle {
		t.Error("a malformed view must not promote to the battle screen (would panic the renderers)")
	}
	if mm.view != nil {
		t.Error("a malformed view must not be installed")
	}
}

func TestValidViewResolvesFoeDex(t *testing.T) {
	dex, err := domain.LoadDexFS(pokearena.DataFS(), "gen1-v1")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	m := newModel(nil, dex, "b", "p1")
	out, _ := m.Update(frameMsg(frame{Type: protocol.FrameState, View: decodeBattleFrame(t, dex)}))
	mm := out.(model)
	if mm.screen != screenBattle {
		t.Fatal("a valid view should promote to the battle screen")
	}
	if mm.foeDexNo != 12 { // decodeBattleFrame's foe is Butterfree
		t.Errorf("foeDexNo = %d, want 12 (resolved once, not rescanned per frame)", mm.foeDexNo)
	}
}

// ---- forced-turn auto-act ----

// battleFrameWithActive builds a decoded live-wire battle frame and lets the
// test mutate the active Pokémon (to plant charge/rampage/recharge volatiles).
func battleFrameWithActive(t *testing.T, dex *domain.Dex, mut func(*engine.Pokemon)) frame {
	t.Helper()
	v := decodeBattleFrame(t, dex)
	mut(&v.Self.Team[v.Self.Active])
	return frame{Type: protocol.FrameState, View: v}
}

// modelWithSendSpy returns a battle-ready model whose send is captured.
func modelWithSendSpy(t *testing.T) (model, *[]protocol.WsClientMsg, *domain.Dex) {
	t.Helper()
	dex, err := domain.LoadDexFS(pokearena.DataFS(), "gen1-v1")
	if err != nil {
		t.Fatalf("load dex: %v", err)
	}
	m := newModel(nil, dex, "b", "p1")
	sent := &[]protocol.WsClientMsg{}
	m.send = func(msg protocol.WsClientMsg) error {
		*sent = append(*sent, msg)
		return nil
	}
	return m, sent, dex
}

// TestChargingTurnAutoActs pins the handheld behavior: mid-charge (Fly, Solar
// Beam) there is no menu — the TUI finishes the move itself and explains why
// in the status line.
func TestChargingTurnAutoActs(t *testing.T) {
	m, sent, dex := modelWithSendSpy(t)
	f := battleFrameWithActive(t, dex, func(p *engine.Pokemon) {
		p.Volatiles.Charging = &engine.ChargingState{MoveIdx: 0}
	})
	out, _ := m.Update(frameMsg(f))
	mm := out.(model)
	if len(*sent) != 1 || (*sent)[0].Kind != protocol.ActionKindMove || (*sent)[0].Index != 0 {
		t.Fatalf("expected one auto-sent move at index 0, got %+v", *sent)
	}
	if mm.needsAction {
		t.Error("a forced turn must latch input off after the auto-send")
	}
	if mm.status == "" {
		t.Error("the auto-played turn must explain itself in the status line")
	}
}

// TestRampageTurnAutoActs: mid-Outrage/Thrash the locked move continues on
// its own instead of the player having to re-pick the only legal slot.
func TestRampageTurnAutoActs(t *testing.T) {
	m, sent, dex := modelWithSendSpy(t)
	f := battleFrameWithActive(t, dex, func(p *engine.Pokemon) {
		p.Volatiles.LockedMove = &engine.LockedMoveState{MoveIdx: 0, Turns: 1}
	})
	out, _ := m.Update(frameMsg(f))
	mm := out.(model)
	if len(*sent) != 1 || (*sent)[0].Index != 0 {
		t.Fatalf("expected the locked move auto-sent, got %+v", *sent)
	}
	if !strings.Contains(mm.status, "locked into") {
		t.Errorf("status should say the move is locked in, got %q", mm.status)
	}
}

// TestRechargeTurnAutoActs guards the worst of the old UX: the Hyper Beam
// recharge turn surfaced as a "Struggle" menu item. It must instead pass the
// turn automatically via the engine's index -1 sentinel.
func TestRechargeTurnAutoActs(t *testing.T) {
	m, sent, dex := modelWithSendSpy(t)
	f := battleFrameWithActive(t, dex, func(p *engine.Pokemon) {
		p.Volatiles.MustRecharge = true
	})
	out, _ := m.Update(frameMsg(f))
	mm := out.(model)
	if len(*sent) != 1 || (*sent)[0].Kind != protocol.ActionKindMove || (*sent)[0].Index != -1 {
		t.Fatalf("expected the recharge sentinel (-1) auto-sent, got %+v", *sent)
	}
	if !strings.Contains(mm.status, "recharge") {
		t.Errorf("status should mention recharging, got %q", mm.status)
	}
}

// TestSameTurnResyncDoesNotDoubleSend: the gateway can resend a state frame
// for a turn we already auto-acted on; the guard must swallow the repeat.
func TestSameTurnResyncDoesNotDoubleSend(t *testing.T) {
	m, sent, dex := modelWithSendSpy(t)
	f := battleFrameWithActive(t, dex, func(p *engine.Pokemon) {
		p.Volatiles.LockedMove = &engine.LockedMoveState{MoveIdx: 0, Turns: 2}
	})
	out, _ := m.Update(frameMsg(f))
	out, _ = out.(model).Update(frameMsg(f))
	if got := len(*sent); got != 1 {
		t.Fatalf("resync frame for the same turn double-sent: %d sends", got)
	}
	if mm := out.(model); mm.autoActedTurn != f.View.Turn {
		t.Errorf("autoActedTurn = %d, want %d", mm.autoActedTurn, f.View.Turn)
	}
}

// TestChoiceLockStillPrompts: a Choice lock is not a forced turn — the player
// may still switch out — so the menu must appear, not an auto-send.
func TestChoiceLockStillPrompts(t *testing.T) {
	m, sent, dex := modelWithSendSpy(t)
	var lockedID string
	f := battleFrameWithActive(t, dex, func(p *engine.Pokemon) {
		lockedID = p.Moves[0].MoveID
		p.Volatiles.ChoiceLockMoveID = lockedID
	})
	out, _ := m.Update(frameMsg(f))
	mm := out.(model)
	if len(*sent) != 0 {
		t.Fatalf("choice lock must not auto-act (switching is still legal), got %+v", *sent)
	}
	if !mm.needsAction {
		t.Error("the menu must stay open under a choice lock")
	}
}

func TestAppendLogCaps(t *testing.T) {
	var log []engine.LogLine
	for i := 0; i < maxLogLines+50; i++ {
		log = appendLog(log, []engine.LogLine{{Text: "x"}})
	}
	if len(log) != maxLogLines {
		t.Errorf("log length = %d, want capped at %d", len(log), maxLogLines)
	}
}
