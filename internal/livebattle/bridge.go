package livebattle

import (
	"sync"

	"pokearena/internal/messages"
)

// slotIndex maps a wire slot name ("p1"|"p2") to its 0/1 array position, or -1.
func slotIndex(s string) int {
	switch s {
	case "p1":
		return 0
	case "p2":
		return 1
	default:
		return -1
	}
}

// SlotName maps a 0/1 slot index to its wire name. Used by a broker-backed
// FrameSink to choose the frame routing key.
func SlotName(i int) string {
	if i == 1 {
		return "p2"
	}
	return "p1"
}

// Pump feeds decoded LiveAction messages from a broker into a Match. The
// battle-session owns one Pump per battle: it attaches each WS slot lazily on
// the slot's first message and routes subsequent traffic by phase. Routing is
// broker-agnostic — a test can drive a match by calling Route directly, with no
// RabbitMQ in sight.
type Pump struct {
	m     *Match
	kinds [2]SideKind

	mu        sync.Mutex
	producers [2]*Producer
}

// NewPump builds a Pump for a match. kinds mirrors the match's slot kinds so the
// Pump ignores any stray message aimed at an AI slot.
func NewPump(m *Match, kinds [2]SideKind) *Pump {
	return &Pump{m: m, kinds: kinds}
}

// Route applies one inbound action. It blocks only as long as the coordinator
// takes to accept the message (bounded by the per-slot channel buffer), bailing
// out if the match shuts down.
func (p *Pump) Route(a messages.LiveAction) {
	slot := slotIndex(a.Slot)
	if slot < 0 || p.kinds[slot] != SideWS {
		return // unknown slot, or a message aimed at an AI slot
	}
	switch a.Phase {
	case messages.LivePhaseAttach:
		// Record the connection before attaching the producer: a re-attach under a
		// new id must cancel any reconnect-grace timer from the prior connection's
		// disconnect, even though the producer is already registered.
		p.m.SlotConnected(slot, a.Conn)
		p.attach(slot)
	case messages.LivePhaseSubmit:
		if prod := p.attach(slot); prod != nil {
			select {
			case prod.Submits <- a.Picks:
			case <-p.m.Done():
			}
		}
	case messages.LivePhaseAction:
		// Drop a redelivered action from an already-resolved turn (a failover
		// owner re-reading the durable queue). Turn 0 means the client never saw
		// a frame to stamp, so it can't be stale — let the coordinator judge it.
		if a.Turn > 0 && a.Turn < p.m.CurrentTurn() {
			return
		}
		if prod := p.attach(slot); prod != nil {
			select {
			case prod.Actions <- a.Action:
			case <-p.m.Done():
			}
		}
	case messages.LivePhaseDisconnect:
		p.m.SlotDisconnected(slot, a.Conn)
	}
}

// attach registers the slot's producer on first use and returns it. Returns nil
// if the slot was already attached by someone else (shouldn't happen — one
// bridge owns one slot).
func (p *Pump) attach(slot int) *Producer {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.producers[slot] == nil {
		if prod, ok := p.m.Attach(slot); ok {
			p.producers[slot] = &prod
		}
	}
	return p.producers[slot]
}
