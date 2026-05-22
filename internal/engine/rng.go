package engine

// RNG is a deterministic splitmix64 generator. Its entire state is a single
// uint64, so it serializes trivially with the battle state — which is what
// makes every battle replayable bit-for-bit from its seed.
type RNG struct {
	state uint64
}

// NewRNG seeds a generator.
func NewRNG(seed uint64) *RNG { return &RNG{state: seed} }

func (r *RNG) next() uint64 {
	r.state += 0x9E3779B97F4A7C15
	z := r.state
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// IntN returns a pseudo-random int in [0, n).
func (r *RNG) IntN(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.next() % uint64(n))
}

// Range returns a pseudo-random int in [lo, hi] inclusive.
func (r *RNG) Range(lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + r.IntN(hi-lo+1)
}

// Chance returns true with the given percentage probability.
func (r *RNG) Chance(pct int) bool {
	if pct <= 0 {
		return false
	}
	if pct >= 100 {
		return true
	}
	return r.IntN(100) < pct
}

// State / SetState expose the raw generator state for serialization.
func (r *RNG) State() uint64     { return r.state }
func (r *RNG) SetState(s uint64) { r.state = s }
