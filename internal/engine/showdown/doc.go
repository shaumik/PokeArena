// Package showdown holds the PokeArena engine's port of Pokémon Showdown's
// simulator test suite (`test/sim/**` in smogon/pokemon-showdown).
//
// Everything in here is behind the `showdown` build tag, so a plain
// `go test ./...` — and therefore CI — does not compile it. Run it with:
//
//	make test-showdown          # the whole port, with a summary
//	go test -tags showdown ./internal/engine/showdown/ -run TestKnockOff
//
// # Why the suite exists
//
// The engine's own tests were written against the engine. They pin what it
// does, which is exactly the wrong instrument for finding out where what it
// does differs from competitive Pokémon. This suite is written against
// somebody else's implementation of the same game: each case here is a
// translation of an `it(...)` block from Showdown's suite, which is the
// closest thing the community has to an executable specification.
//
// The port follows the pattern the PostgreSQL-in-Rust rewrites used: bring the
// upstream regression corpus over *first*, in full, and let it be red. A test
// that fails here is a question, not a defect report — it might be an engine
// bug, a deliberate scope decision, or a bad translation — and the triage that
// answers it is the point of the exercise. See docs/showdown-port.md.
//
// # Layout
//
//	harness_test.go   the describe/it runner, battle driver and assertions
//	names_test.go     Showdown id → PokeArena slug, and the species stand-ins
//	gaps_test.go      the expected-failure ledger and its report
//
// The ports themselves are one file per upstream file, named for where they
// came from: test/sim/moves/knockoff.js becomes moves_knockoff_test.go.
package showdown
