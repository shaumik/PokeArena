# Team builder: explicit moves

**Status:** not started

**Why:** Right now teams are `[]int` of dex IDs and the engine picks 4 moves from the
learnset. For competitive play (and especially Pv-Claude) both sides need real
agency over movesets — a Tauros with Body Slam / Hyper Beam / Earthquake / Blizzard
is a different Pokémon from one with Tackle / Growl / Stomp / Leer.

**Schema:**
```go
type TeamEntry struct { Dex int; Moves [4]string }
type Team []TeamEntry  // 1..6 entries; Gen 1 so no items/abilities
```

**UI:** Clicking a roster slot opens a move-picker listing the species's learnset.
User picks exactly 4. Validates server-side against the learnset.

**Migration:** Old `[]int` shape stays accepted on the API for backward compat;
gateway expands it to auto-picked moves the way it does today. New shape is
preferred.

**Acceptance:** UI flow lets a user pick a team where every Pokémon has 4 chosen
moves from its legal learnset. API rejects illegal moves with a clear error.
