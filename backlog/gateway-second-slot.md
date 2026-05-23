# Gateway: claimable second trainer slot

**Status:** not started

**Why:** Today's `live` mode auto-binds slot 2 to a local AI. For Pv-Claude (and any
future Pv-anything), slot 2 must be claimable by an external WS client instead. The
gateway should not grow a Claude-specific code path — it should grow a generic
"second-slot open" path that *any* WS client can take, including a future second
human player.

**Changes:**
- New battle mode: `live_pvp` (name negotiable — `live_open`?). Server attaches no
  AI; both slots wait for WS clients.
- `POST /api/battles` returns `{battle_id, p1_url, p2_url}` for `live_pvp`. Each URL
  carries a single-use join token so randos can't claim the slot.
- WS handler accepts the token, binds the slot, refuses a second claim on the same
  slot.

**Acceptance:** Two browser tabs (or one browser + one MCP server) can join slot 1
and slot 2 of the same battle and play it to completion.

**Depends on:** none. **Required by:** [[pv-claude]].
