package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"pokearena/internal/ai"
	"pokearena/internal/engine"
	"pokearena/internal/messages"
)

const (
	humanSide   = 0
	aiSide      = 1
	turnTimeout = 90 * time.Second // safety net: auto-resolve an idle turn
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true }, // demo: same-origin SPA
}

type wsClientMsg struct {
	Type  string `json:"type"`  // "action"
	Kind  string `json:"kind"`  // "move" | "switch"
	Index int    `json:"index"` // move slot, or team index for a switch
}

// handleWS coordinates a live battle. The gateway owns the loop: it runs the
// (microsecond) engine inline and offloads only the AI's decision — the
// expensive, variable-latency part — to the ai-service over the queue.
func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	battleID := chi.URLParam(r, "id")
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	st, err := s.cache.LoadState(ctx, battleID)
	if err != nil {
		writeWS(conn, errMsg("battle not found or expired"))
		return
	}
	difficulty := s.cfg.AIDifficulty
	if b, err := s.store.GetBattle(ctx, battleID); err == nil && b.AIDifficulty != "" {
		difficulty = b.AIDifficulty
	}

	subID, events, err := s.hub.Subscribe(battleID)
	if err != nil {
		writeWS(conn, errMsg("could not subscribe to battle events"))
		return
	}
	defer s.hub.Unsubscribe(battleID, subID)

	// A reader goroutine turns blocking WebSocket reads into channel sends.
	clientMsgs := make(chan wsClientMsg, 8)
	go func() {
		defer cancel()
		conn.SetReadLimit(4096)
		for {
			var m wsClientMsg
			if err := conn.ReadJSON(&m); err != nil {
				close(clientMsgs)
				return
			}
			select {
			case clientMsgs <- m:
			case <-ctx.Done():
				return
			}
		}
	}()

	writeWS(conn, map[string]any{"type": "state", "state": st})

	for ctx.Err() == nil && !st.Ended() {
		// Loop invariant: st.Phase is PhaseChoosing here.
		actions, ok := s.collectTurnActions(ctx, conn, st, battleID, difficulty, clientMsgs, events)
		if !ok {
			return
		}
		turnLog := engine.ResolveTurn(s.dex, st, actions)
		writeWS(conn, turnMsg(turnLog, st))

		if st.Phase == engine.PhaseReplace {
			sw, ok := s.collectReplaceActions(ctx, conn, st, battleID, difficulty, clientMsgs, events)
			if !ok {
				return
			}
			replaceLog := engine.ResolveReplace(st, sw)
			turnLog = append(turnLog, replaceLog...)
			writeWS(conn, turnMsg(replaceLog, st))
		}
		s.persistLiveTurn(ctx, st, turnLog)
	}

	if st.Ended() {
		s.finishLiveBattle(ctx, st)
		writeWS(conn, map[string]any{"type": "end", "winner": st.Winner, "state": st})
	}
}

// collectTurnActions gathers both sides' actions for a choosing turn: the
// human's over the WebSocket, the AI's via the ai-service. The turn timer is
// a safety net — if either side stalls, an action is filled in locally.
func (s *Server) collectTurnActions(ctx context.Context, conn *websocket.Conn, st *engine.BattleState,
	battleID, difficulty string, clientMsgs <-chan wsClientMsg, events <-chan Event) ([2]engine.Action, bool) {

	var actions [2]engine.Action
	jobID := s.publishAIJob(ctx, battleID, st.Turn, aiSide, difficulty)
	gotHuman, gotAI := false, false

	timer := time.NewTimer(turnTimeout)
	defer timer.Stop()

	for !gotHuman || !gotAI {
		select {
		case <-ctx.Done():
			return actions, false
		case m, ok := <-clientMsgs:
			if !ok {
				return actions, false
			}
			if gotHuman {
				continue
			}
			act, err := parseClientAction(m, st, humanSide)
			if err != nil {
				writeWS(conn, errMsg(err.Error()))
				continue
			}
			actions[humanSide], gotHuman = act, true
		case ev := <-events:
			if ev.Type != messages.EventAIDecided {
				continue
			}
			var d messages.AIDecided
			if json.Unmarshal(ev.Body, &d) != nil || d.JobID != jobID {
				continue
			}
			actions[aiSide], gotAI = d.Action, true
			if d.Reasoning != "" {
				writeWS(conn, map[string]any{"type": "ai", "reasoning": d.Reasoning})
			}
		case <-timer.C:
			if !gotHuman {
				actions[humanSide], gotHuman = engine.LegalActions(st, humanSide)[0], true
				writeWS(conn, map[string]any{"type": "info", "message": "Turn timer expired — a move was auto-selected."})
			}
			if !gotAI {
				actions[aiSide], gotAI = s.localAIDecision(st, aiSide), true
			}
		}
	}
	return actions, true
}

// collectReplaceActions gathers forced-switch choices after faints, only for
// the sides that actually need to replace.
func (s *Server) collectReplaceActions(ctx context.Context, conn *websocket.Conn, st *engine.BattleState,
	battleID, difficulty string, clientMsgs <-chan wsClientMsg, events <-chan Event) ([2]*engine.Action, bool) {

	var sw [2]*engine.Action
	needHuman, needAI := st.Replace[humanSide], st.Replace[aiSide]
	gotHuman, gotAI := !needHuman, !needAI

	var jobID string
	if needAI {
		jobID = s.publishAIJob(ctx, battleID, st.Turn, aiSide, difficulty)
	}
	if needHuman {
		writeWS(conn, map[string]any{"type": "info", "message": "Your Pokémon fainted — choose a replacement."})
	}

	timer := time.NewTimer(turnTimeout)
	defer timer.Stop()

	for !gotHuman || !gotAI {
		select {
		case <-ctx.Done():
			return sw, false
		case m, ok := <-clientMsgs:
			if !ok {
				return sw, false
			}
			if gotHuman {
				continue
			}
			act, err := parseClientAction(m, st, humanSide)
			if err != nil || act.Kind != engine.ActionSwitch {
				writeWS(conn, errMsg("choose a Pokémon to send in"))
				continue
			}
			a := act
			sw[humanSide], gotHuman = &a, true
		case ev := <-events:
			if ev.Type != messages.EventAIDecided {
				continue
			}
			var d messages.AIDecided
			if json.Unmarshal(ev.Body, &d) != nil || d.JobID != jobID {
				continue
			}
			a := d.Action
			sw[aiSide], gotAI = &a, true
		case <-timer.C:
			if !gotHuman {
				a := engine.LegalActions(st, humanSide)[0]
				sw[humanSide], gotHuman = &a, true
			}
			if !gotAI {
				a := s.localAIDecision(st, aiSide)
				sw[aiSide], gotAI = &a, true
			}
		}
	}
	return sw, true
}

// publishAIJob asks the ai-service for one decision and returns the job id to
// correlate with the AIDecided reply.
func (s *Server) publishAIJob(ctx context.Context, battleID string, turn, side int, difficulty string) string {
	jobID := uuid.NewString()
	_ = s.broker.PublishJob(ctx, messages.QueueAI, messages.AIJob{
		JobID: jobID, BattleID: battleID, Turn: turn, Side: side, Difficulty: difficulty,
	})
	return jobID
}

// localAIDecision is the gateway's own fallback when the ai-service is silent.
func (s *Server) localAIDecision(st *engine.BattleState, side int) engine.Action {
	act, _ := s.fallbackAI.Decide(context.Background(), ai.MakeView(st, side))
	return act
}

// persistLiveTurn writes the post-turn state to Redis and the turn to Postgres.
func (s *Server) persistLiveTurn(ctx context.Context, st *engine.BattleState, log []engine.LogLine) {
	if err := s.cache.SaveState(ctx, st); err != nil {
		return
	}
	logJSON, _ := json.Marshal(log)
	stateJSON, _ := json.Marshal(st)
	_ = s.store.AppendTurn(ctx, st.ID, st.Turn, logJSON, stateJSON)
}

// finishLiveBattle records the result and announces it for the leaderboard.
func (s *Server) finishLiveBattle(ctx context.Context, st *engine.BattleState) {
	_ = s.store.CompleteBattle(ctx, st.ID, st.Winner, st.Turn)
	_ = s.broker.PublishEvent(ctx, messages.EventBattleCompleted, st.ID, messages.BattleCompleted{
		BattleID: st.ID, Winner: st.Winner, TurnCount: st.Turn,
	})
	_ = s.cache.DeleteState(ctx, st.ID)
}

// parseClientAction validates a client message against the legal actions.
func parseClientAction(m wsClientMsg, st *engine.BattleState, side int) (engine.Action, error) {
	kind := engine.ActionMove
	if m.Kind == "switch" {
		kind = engine.ActionSwitch
	}
	act := engine.Action{Kind: kind, Index: m.Index}
	for _, legal := range engine.LegalActions(st, side) {
		if legal == act {
			return act, nil
		}
	}
	return engine.Action{}, errors.New("that action is not legal right now")
}

func writeWS(conn *websocket.Conn, v any) { _ = conn.WriteJSON(v) }

func errMsg(msg string) map[string]any {
	return map[string]any{"type": "error", "message": msg}
}

func turnMsg(log []engine.LogLine, st *engine.BattleState) map[string]any {
	return map[string]any{"type": "turn", "log": log, "state": st}
}
