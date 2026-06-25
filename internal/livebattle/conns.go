package livebattle

import (
	"sync"
	"time"
)

// slotConns tracks the live WS connection per slot and the reconnect-grace timer
// that defers declaring a slot gone after a disconnect. It is a self-contained
// concurrency unit with its own mutex: it is touched from the action-pump
// goroutine (connected/disconnected) and from grace-timer goroutines, never from
// the coordinator goroutine that owns the battle state.
//
// The whole job is reconnect identity: a slot may disconnect and re-attach under
// a new connection id, and the durable action queue may replay a stale
// disconnect after a failover. A generation counter, bumped on every
// (dis)connect, lets a grace timer tell whether it is still the one that armed
// the window when it finally fires.
type slotConns struct {
	mu     sync.Mutex
	active [2]string      // id of the connection currently bound to the slot
	gen    [2]uint64      // bumped on every (dis)connect to invalidate a pending grace timer
	timer  [2]*time.Timer // pending reconnect-grace timer, if any
}

// connected records id as the live connection for slot. It cancels any
// reconnect-grace timer in flight (the slot came back); bumping gen invalidates
// a timer that may already have fired into its callback but not yet taken the
// lock. id may be empty (legacy/test).
func (c *slotConns) connected(slot int, id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active[slot] = id
	c.gen[slot]++
	c.stopTimerLocked(slot)
}

// disconnected handles a disconnect for slot from connection id.
//
//   - A non-empty id that does not match the slot's live connection is a stale
//     or redelivered disconnect (the durable queue replaying an old message after
//     a takeover, or a blip's disconnect arriving after the player already
//     reconnected). It is ignored — that connection is no longer current.
//   - Otherwise the connection is retired and, if grace > 0, a timer is armed to
//     call declareGone(slot) unless the slot re-attaches first. With no grace the
//     slot is declared gone at once.
func (c *slotConns) disconnected(slot int, id string, grace time.Duration, declareGone func(slot int)) {
	c.mu.Lock()
	if id != "" && id != c.active[slot] {
		c.mu.Unlock()
		return
	}
	c.active[slot] = ""
	c.gen[slot]++
	gen := c.gen[slot]
	c.stopTimerLocked(slot)
	if grace <= 0 {
		c.mu.Unlock()
		declareGone(slot)
		return
	}
	c.timer[slot] = time.AfterFunc(grace, func() {
		c.mu.Lock()
		// Fire only if no (dis)connect intervened since we armed this timer.
		expired := c.gen[slot] == gen && c.active[slot] == ""
		c.mu.Unlock()
		if expired {
			declareGone(slot)
		}
	})
	c.mu.Unlock()
}

// stopAll cancels any pending reconnect-grace timers. A slot that was mid-grace
// when the match ended for another reason (a win, or the other slot leaving)
// would otherwise leave a timer ticking past teardown. A stray fire is only a
// no-op declareGone, but stopping them keeps teardown clean and releases the
// timers promptly.
func (c *slotConns) stopAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range c.timer {
		c.stopTimerLocked(i)
	}
}

func (c *slotConns) stopTimerLocked(slot int) {
	if c.timer[slot] != nil {
		c.timer[slot].Stop()
		c.timer[slot] = nil
	}
}
