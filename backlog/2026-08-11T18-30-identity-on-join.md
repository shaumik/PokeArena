# Identity belongs to the joiner, not the creator

The leaderboard has had working Elo since the beginning and has never
meant anything. Today's change is small — a name on the join — but it is
the one that makes a result attributable to whoever produced it.

## The bug was in *when*, not *whether*, we asked for a name

The README has said "free-text name, no ownership; clients barely prompt"
for months, and I read that as a UI gap: add a name field to more screens
and the board improves. That is not what was wrong.

A `live_pvp` battle is created before its players arrive. The creator
POSTs `p1_name` / `p2_name`, both trainer FKs are bound *then*, and the
slots are filled later over the join WebSocket. So the names on a battle
are always the creator's guesses about who will show up — `"Opponent"`,
`"AI"`, `"Agent 2"`. There was no moment in the protocol at which the
actual occupant of a slot could say who it was. `pokearena-agent` had no
name flag, MCP `join_battle` had no name argument, and the SPA's
share-link join hardcoded `'Trainer'`. Not one of them was under-prompting;
there was nothing to prompt *into*.

That reframes the fix. It isn't "collect the name in more places," it's
"make join a point at which identity can be declared," which needs a wire
field (`?name=` on the play URL), a message field (`LiveAction.Trainer`),
and a write (`RebindBattleTrainer`). Once those exist the three clients
are one line each.

## The second track was blocked on the same field

I went looking at PR #109 (decision-quality eval) in the same session and
its handoff doc says, in the gotchas:

> **Postgres has NO model identity** — `p1_name` is always "Agent",
> `p2_name` "AI". Model attribution ONLY comes from the `bid=`→model
> mapping in the run dirs. The previous mapping (`/tmp/pk-agentic`) was
> wiped, which is why we re-ran.

That is the same missing field, discovered independently, and it cost a
re-run of a full attributed batch. The benchmark had been reconstructing
in `/tmp` a fact the database should have carried. Worth naming because
the two symptoms look unrelated — "the leaderboard is meaningless" and
"we lost the model attribution for a batch" — and have one cause.

## What I deliberately did not build

The name is **self-reported**. Any holder of a slot token can claim any
name, including one already on the board. I considered gating it — a
secret per handle, first-writer-wins on a name — and stopped, because
that is a different feature (accounts) wearing this one's clothes, and
shipping half of it would produce a board that *looks* verified.

So the honest split is: this change makes identity **expressible and
attributable**; it does not make it **verified**. Both the README status
row and `live-pvp.md` §3 now say that in those words, and §3 lists
impersonation under "not designing against" rather than leaving it
unmentioned. The README's "for fun, unverified" disclaimer stays exactly
as it was — it is still true, just for a narrower reason.

Two things I did constrain, because they are cheap and the absence would
be a real bug rather than a known limitation:

- **A name is only accepted after the slot claim succeeds.** Rebinding
  before the claim would let anyone who can guess a battle id rewrite its
  trainers without ever playing.
- **`RebindBattleTrainer` refuses a battle with a winner.** Elo is applied
  from `p1_trainer` / `p2_trainer` at completion; a late or replayed join
  that renamed a settled battle would move a rating that was already
  computed against the old trainer. The `winner < 0` guard is the whole
  defense and it is why that method has an integration test.

## A concurrency seam I didn't expect

`trainerName` was a plain `[2]string` on `Match`, written once at
construction and read by the coordinator goroutine. Accepting a name at
attach makes it a field written by the *action-pump* goroutine at an
arbitrary moment — a data race, even though nothing about a display name
can affect a turn's outcome.

I gave it its own tiny mutex-guarded type (`trainerNames`) rather than
routing the name through the coordinator's channel, following the
`slotConns` precedent already in the package: a self-contained concurrency
unit, documented as touched from one goroutine and read from another. The
comment on it says out loud that a concurrent name arrival is benign in
behavior and a race only in the memory-model sense, because that's the
thing a future reader will otherwise have to re-derive before touching it.

`set("")` is a no-op rather than a store. Without that, an anonymous
re-attach — which is exactly what a reconnect after a blip is — would
blank a name the slot declared on its first attach.

## Where the default came from

`pokearena-agent --name` defaults to the model id rather than to empty.
An unnamed agent inherits the creator's placeholder, which is the
behavior that caused all of this, so "stay anonymous" is the wrong
default for a bot: the useful thing a harness can do with no
configuration is say which model played. The SPA remembers its name in
`localStorage` so a returning player keeps their row instead of minting
"Challenger" every session.

The one path still weaker than I'd like: a first-time share-link joiner
in the browser gets `"Challenger"`, because they never passed through the
setup form and I didn't want to put a modal in front of a battle
invitation. A name field on the picker screen is the obvious follow-up
and is UI work, not protocol work — the protocol side is done.
