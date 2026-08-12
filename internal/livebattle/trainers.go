package livebattle

import "sync"

// trainerNames holds the display name of each slot's occupant.
//
// It needs its own mutex because, unlike everything else the coordinator
// reads, these are written from a different goroutine at an arbitrary moment:
// the names start as whatever the battle's creator supplied, and a slot may
// replace its own when it attaches (see messages.LiveAction.Trainer). The
// write lands on the action-pump goroutine; the reads are on the coordinator
// goroutine, building room frames and the initial engine state.
//
// Names are display-only — no turn, action, or validation decision reads
// them — so a name that arrives concurrently with a room broadcast is a
// benign race in behavior, and only a data race in the memory-model sense.
// That is exactly what this guards.
type trainerNames struct {
	mu   sync.RWMutex
	name [2]string
}

func (t *trainerNames) get(slot int) string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.name[slot]
}

// set replaces the slot's name. An empty name is ignored rather than stored:
// a joiner that declares nothing keeps the creator-supplied placeholder, and
// treating "" as a value would blank the label on every anonymous attach.
func (t *trainerNames) set(slot int, name string) {
	if name == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.name[slot] = name
}
