package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"pokearena/internal/messages"
)

// sseTurn is the shape replayed for an already-stored turn.
type sseTurn struct {
	Turn  int             `json:"turn"`
	Log   json.RawMessage `json:"log"`
	State json.RawMessage `json:"state"`
}

// handleSSE streams a battle's turns to a spectator over Server-Sent Events.
// Stored turns are replayed first so a late joiner catches up; live events
// follow. The client deduplicates by turn number.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	battleID := chi.URLParam(r, "id")
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ctx := r.Context()
	// Subscribe before snapshotting stored turns, so no turn falls in the gap.
	subID, events, err := s.hub.Subscribe(battleID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "subscribe failed")
		return
	}
	defer s.hub.Unsubscribe(battleID, subID)

	if turns, err := s.store.GetTurns(ctx, battleID); err == nil {
		for _, t := range turns {
			body, _ := json.Marshal(sseTurn{Turn: t.TurnNo, Log: t.Log, State: t.StateDigest})
			writeSSE(w, messages.EventTurnResolved, body)
		}
		flusher.Flush()
	}

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-keepalive.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case ev := <-events:
			writeSSE(w, ev.Type, ev.Body)
			flusher.Flush()
			if ev.Type == messages.EventBattleCompleted {
				return
			}
		}
	}
}

func writeSSE(w http.ResponseWriter, event string, data []byte) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
}
