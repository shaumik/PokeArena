package mcpserver

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shaumik/PokeArena/internal/ai"
	"github.com/shaumik/PokeArena/internal/engine"
	"github.com/shaumik/PokeArena/internal/protocol"
)

func awaitSession(t *testing.T, s *session, ready func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		s.mu.Lock()
		ok := ready()
		tick := s.tick
		s.mu.Unlock()
		if ok {
			return
		}
		select {
		case <-tick:
		case <-ctx.Done():
			t.Fatal("session did not receive scripted frames")
		}
	}
}

func TestMCPAccumulatesEventsClearsOnActionAndKeepsTerminalLog(t *testing.T) {
	first := []engine.LogLine{{Type: "switch", Side: 1, Text: "Blue withdrew Venusaur."}, {Type: "switch", Side: 1, Text: "Blue sent out Alakazam."}}
	second := []engine.LogLine{{Type: "move", Side: 0, Text: "Charizard used Flamethrower!"}, {Type: "damage", Side: 1, Text: "Alakazam took damage."}}
	last := []engine.LogLine{{Type: "win", Side: 0, Text: "Red wins!"}}
	sendEvents := make(chan struct{})
	base, cleanup := fakeGateway(t, func(t *testing.T, c *websocket.Conn) {
		must(t, "state", c.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameState, View: fakeView("Red", 1)}))
		<-sendEvents
		must(t, "first", c.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameTurn, View: fakeView("Red", 2), Log: first}))
		must(t, "second", c.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameTurn, View: fakeView("Red", 3), Log: second}))
		var action protocol.WsClientMsg
		if err := c.ReadJSON(&action); err != nil {
			t.Error(err)
			return
		}
		if action.SwitchTarget == nil || *action.SwitchTarget != 2 {
			t.Errorf("lost switch target: %+v", action)
		}
		must(t, "reject", c.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameError, Message: "try again"}))
		if err := c.ReadJSON(&action); err != nil {
			t.Error(err)
			return
		}
		winner := 0
		must(t, "end", c.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameEnd, View: fakeView("Red", 4), Winner: &winner, Log: last}))
		blockUntilPeerClose(c)
	})
	defer cleanup()
	s := newTestSession(base)
	defer s.Leave()
	ctx := context.Background()
	_, err := s.Join(ctx, "b", "p1", "tok")
	must(t, "join", err)
	w, err := s.Wait(ctx, 1)
	must(t, "initial wait", err)
	if w.RecentLog == nil || len(w.RecentLog) != 0 {
		t.Fatalf("initial log: %v", w.RecentLog)
	}
	close(sendEvents)
	awaitSession(t, s, func() bool { return s.latest != nil && s.latest.Turn == 3 })
	w, err = s.Wait(ctx, 1)
	must(t, "events wait", err)
	want := append(append([]engine.LogLine{}, first...), second...)
	if !reflect.DeepEqual(w.RecentLog, want) {
		t.Fatalf("events = %v, want %v", w.RecentLog, want)
	}
	v, err := s.ViewWire()
	must(t, "view", err)
	if !reflect.DeepEqual(v["recent_log"], want) {
		t.Fatalf("view lost log: %v", v)
	}
	w.RecentLog[0].Text = "mutated by caller"
	target := 2
	out, err := s.ActAndWait(ctx, "move", 0, 1, &target)
	must(t, "refused action", err)
	if out.Error != "try again" || len(out.RecentLog) != 0 {
		t.Fatalf("refusal retained old events: %+v", out)
	}
	out, err = s.ActAndWait(ctx, "move", 1, 1, nil)
	must(t, "terminal action", err)
	if !out.Terminal || !reflect.DeepEqual(out.RecentLog, last) {
		t.Fatalf("terminal log lost: %+v", out)
	}
}

func TestOfflineAgentCanUseOnlyPublishedActions(t *testing.T) {
	d := offlineOrSkip(t)
	s := newSession(Config{GatewayURL: "ws://127.0.0.1:1"})
	defer s.Leave()
	ctx := context.Background()
	_, err := s.StartLocal(ctx, d, opponentHeuristic, 7)
	must(t, "start", err)
	must(t, "team", s.SubmitTeam(legalTeam(t, d)))
	w, err := s.Wait(ctx, 5)
	must(t, "wait", err)
	sawMove, sawDamage := false, false
	for step := 0; step < 500; step++ {
		if !w.Ready {
			t.Fatal("offline wait timed out")
		}
		for _, line := range w.RecentLog {
			sawMove = sawMove || line.Type == "move"
			sawDamage = sawDamage || line.Type == "damage"
		}
		if w.Terminal {
			if !sawMove || !sawDamage {
				t.Fatal("full battle did not expose move and damage events")
			}
			actions := w.View["legal_actions"].([]any)
			if len(actions) != 0 {
				t.Fatal("ended battle offers actions")
			}
			return
		}
		raw, err := json.Marshal(w.View["legal_actions"])
		must(t, "actions JSON", err)
		var actions []engine.Action
		must(t, "actions decode", json.Unmarshal(raw, &actions))
		if len(actions) == 0 {
			t.Fatal("decision has no actions")
		}
		a := actions[0]
		out, err := s.ActAndWait(ctx, string(a.Kind), a.Index, 5, a.SwitchTarget)
		must(t, "act", err)
		if out.Error != "" {
			t.Fatalf("published action was refused: %s", out.Error)
		}
		w = waitOut{Ready: out.Ready, Terminal: out.Terminal, View: out.View, RecentLog: out.RecentLog}
	}
	t.Fatal("battle did not finish")
}

func TestGatewayRelayPreservesPublishedContext(t *testing.T) {
	d := offlineOrSkip(t)
	state, err := engine.NewBattle(d.dex, "b", "Red", []int{6, 9}, "Blue", []int{3}, 7)
	must(t, "battle", err)
	state.Sides[1].Team[0].Moves[0].PP--
	v := ai.MakeViewDex(d.dex, state, 0)
	base, cleanup := fakeGateway(t, func(t *testing.T, c *websocket.Conn) {
		must(t, "state", c.WriteJSON(protocol.MatchUpdate{Type: protocol.FrameState, View: &v}))
		blockUntilPeerClose(c)
	})
	defer cleanup()
	s := newTestSession(base)
	defer s.Leave()
	joined, err := s.Join(context.Background(), "b", "p1", "tok")
	must(t, "join", err)
	moves := joined.View["foe"].(map[string]any)["moves"].([]any)
	if _, ok := moves[0].(map[string]any)["bp"]; !ok {
		t.Fatal("relay dropped revealed move metadata")
	}
	if len(joined.View["legal_actions"].([]any)) == 0 {
		t.Fatal("relay dropped legal actions")
	}
}
