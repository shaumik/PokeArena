// Package httpapi is the gateway: the REST API, the WebSocket live-battle
// endpoint, the SSE spectator endpoint, and the static SPA. It owns no game
// logic — it coordinates, persists, and pushes.
package httpapi

import (
	"context"
	"strings"
	"sync"

	"pokearena/internal/messages"
	"pokearena/internal/mq"
)

// Event is a domain event delivered to a battle's watchers.
type Event struct {
	Type     string // turn-resolved | ai-decided | battle-completed | battle-started
	BattleID string
	Body     []byte
}

// Hub fans RabbitMQ deliveries out to per-battle subscribers over the gateway's
// exclusive event queue. It handles two key shapes on the events exchange:
//
//   - domain events  "{eventType}.{battleID}" → battle watchers (SSE/WS spectators)
//   - live frames    "live.frame.{battleID}.{slot}" → the WS bridge for that slot
//
// Bindings are added only while at least one subscriber needs them and removed
// when the last leaves, so an instance receives only the traffic it serves.
type Hub struct {
	eq   *mq.EventQueue
	mu   sync.Mutex
	subs map[string]map[int]chan Event // keyed by battleID
	// frameSubs is keyed by frameKey(battleID, slot); each value is the raw
	// frame body destined for one WS bridge.
	frameSubs map[string]map[int]chan []byte
	next      int
}

// NewHub creates a hub over the gateway's event queue.
func NewHub(eq *mq.EventQueue) *Hub {
	return &Hub{
		eq:        eq,
		subs:      map[string]map[int]chan Event{},
		frameSubs: map[string]map[int]chan []byte{},
	}
}

func frameKey(battleID, slot string) string { return battleID + "\x00" + slot }

// Run consumes the event queue and dispatches until ctx is canceled. The
// EventQueue already filters out deliveries this process published itself (by
// AppId), so anything that reaches here is a true external delivery.
func (h *Hub) Run(ctx context.Context) error {
	return h.eq.Consume(ctx, func(routingKey string, body []byte) {
		// Live frames carry an extra ".{slot}" word and route to a single WS
		// bridge rather than to every battle watcher.
		if strings.HasPrefix(routingKey, messages.RKLiveFrame) {
			h.dispatchFrame(routingKey, body)
			return
		}
		// routing key is "{eventType}.{battleID}" — neither part contains a dot.
		dot := strings.LastIndexByte(routingKey, '.')
		if dot < 0 {
			return
		}
		h.dispatch(Event{Type: routingKey[:dot], BattleID: routingKey[dot+1:], Body: body})
	})
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

// dispatchFrame routes a "live.frame.{battleID}.{slot}" delivery to the bridge
// subscribed to that exact slot.
func (h *Hub) dispatchFrame(routingKey string, body []byte) {
	rest := strings.TrimPrefix(routingKey, messages.RKLiveFrame) // "{battleID}.{slot}"
	dot := strings.LastIndexByte(rest, '.')
	if dot < 0 {
		return
	}
	key := frameKey(rest[:dot], rest[dot+1:])

	h.mu.Lock()
	targets := make([]chan []byte, 0, len(h.frameSubs[key]))
	for _, ch := range h.frameSubs[key] {
		targets = append(targets, ch)
	}
	h.mu.Unlock()

	for _, ch := range targets {
		select {
		case ch <- body:
		default: // slow bridge: drop, the client resyncs from persisted state
		}
	}
}

// Subscribe registers a watcher for a battle's domain events. The first watcher
// of a battle triggers the routing-key bind.
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

// Unsubscribe removes a domain-event watcher, unbinding once the last one for a
// battle is gone.
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

// SubscribeFrames registers a WS bridge for one slot's outbound frames, binding
// live.frame.{battleID}.{slot} on the first subscriber. The returned channel
// carries raw frame bodies (marshaled protocol.MatchUpdate) the bridge forwards
// straight to the socket.
func (h *Hub) SubscribeFrames(battleID, slot string) (int, <-chan []byte, error) {
	key := frameKey(battleID, slot)
	h.mu.Lock()
	first := len(h.frameSubs[key]) == 0
	if first {
		h.frameSubs[key] = map[int]chan []byte{}
	}
	id := h.next
	h.next++
	ch := make(chan []byte, 64)
	h.frameSubs[key][id] = ch
	h.mu.Unlock()

	if first {
		if err := h.eq.Bind(messages.LiveFrameKey(battleID, slot)); err != nil {
			h.UnsubscribeFrames(battleID, slot, id)
			return 0, nil, err
		}
	}
	return id, ch, nil
}

// UnsubscribeFrames removes a frame bridge, unbinding once the last one for a
// slot is gone.
func (h *Hub) UnsubscribeFrames(battleID, slot string, id int) {
	key := frameKey(battleID, slot)
	h.mu.Lock()
	subs, ok := h.frameSubs[key]
	if !ok {
		h.mu.Unlock()
		return
	}
	delete(subs, id)
	last := len(subs) == 0
	if last {
		delete(h.frameSubs, key)
	}
	h.mu.Unlock()

	if last {
		_ = h.eq.Unbind(messages.LiveFrameKey(battleID, slot))
	}
}
