// Package httpapi is the gateway: the REST API, the WebSocket live-battle
// endpoint, the SSE spectator endpoint, and the static SPA. It owns no game
// logic — it coordinates, persists, and pushes.
package httpapi

import (
	"context"
	"strings"
	"sync"

	"pokearena/internal/mq"
)

// Event is a domain event delivered to a battle's watchers.
type Event struct {
	Type     string // turn-resolved | ai-decided | battle-completed | battle-started
	BattleID string
	Body     []byte
}

// Hub fans RabbitMQ events out to per-battle subscribers. It binds
// "*.{battleID}" on the gateway's exclusive event queue only while at least
// one WebSocket or SSE client is watching that battle, and unbinds when the
// last leaves — so an instance receives only the events it actually needs.
type Hub struct {
	eq   *mq.EventQueue
	mu   sync.Mutex
	subs map[string]map[int]chan Event
	next int
}

// NewHub creates a hub over the gateway's event queue.
func NewHub(eq *mq.EventQueue) *Hub {
	return &Hub{eq: eq, subs: map[string]map[int]chan Event{}}
}

// Run consumes the event queue and dispatches until ctx is cancelled. The
// EventQueue already filters out events this process published itself (by
// AppId), so anything that reaches here is a true external delivery — no
// further dedup needed.
func (h *Hub) Run(ctx context.Context) error {
	return h.eq.Consume(ctx, func(routingKey string, body []byte) {
		// routing key is "{eventType}.{battleID}" — neither part contains a dot.
		dot := strings.LastIndexByte(routingKey, '.')
		if dot < 0 {
			return
		}
		h.dispatch(Event{Type: routingKey[:dot], BattleID: routingKey[dot+1:], Body: body})
	})
}

// Inject fans an event out to local subscribers without going through Rabbit.
// Called by the gateway alongside broker.PublishEvent so spectators on the
// same process see the turn in microseconds instead of waiting on the broker
// round-trip. The Rabbit publish still happens for cross-process consumers
// (leaderboard-worker, future replicas); the EventQueue drops the loopback.
func (h *Hub) Inject(eventType, battleID string, body []byte) {
	h.dispatch(Event{Type: eventType, BattleID: battleID, Body: body})
}

func (h *Hub) dispatch(ev Event) {
	h.mu.Lock()
	targets := make([]chan Event, 0, len(h.subs[ev.BattleID]))
	for _, ch := range h.subs[ev.BattleID] {
		targets = append(targets, ch)
	}
	h.mu.Unlock()

	for _, ch := range targets {
		select {
		case ch <- ev:
		default: // a slow watcher must not stall the whole hub
		}
	}
}

// Subscribe registers a watcher for a battle. The first watcher of a battle
// triggers the routing-key bind. The returned channel receives the battle's
// events; the id is needed to Unsubscribe.
func (h *Hub) Subscribe(battleID string) (int, <-chan Event, error) {
	h.mu.Lock()
	first := len(h.subs[battleID]) == 0
	if first {
		h.subs[battleID] = map[int]chan Event{}
	}
	id := h.next
	h.next++
	ch := make(chan Event, 64)
	h.subs[battleID][id] = ch
	h.mu.Unlock()

	if first {
		if err := h.eq.Bind("*." + battleID); err != nil {
			h.Unsubscribe(battleID, id)
			return 0, nil, err
		}
	}
	return id, ch, nil
}

// Unsubscribe removes a watcher, unbinding the routing key once the last one
// for a battle is gone.
func (h *Hub) Unsubscribe(battleID string, id int) {
	h.mu.Lock()
	watchers, ok := h.subs[battleID]
	if !ok {
		h.mu.Unlock()
		return
	}
	delete(watchers, id)
	last := len(watchers) == 0
	if last {
		delete(h.subs, battleID)
	}
	h.mu.Unlock()

	if last {
		_ = h.eq.Unbind("*." + battleID)
	}
}
