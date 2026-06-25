package main

import (
	"strings"
	"testing"

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

func TestAppendLogCaps(t *testing.T) {
	var log []engine.LogLine
	for i := 0; i < maxLogLines+50; i++ {
		log = appendLog(log, []engine.LogLine{{Text: "x"}})
	}
	if len(log) != maxLogLines {
		t.Errorf("log length = %d, want capped at %d", len(log), maxLogLines)
	}
}
