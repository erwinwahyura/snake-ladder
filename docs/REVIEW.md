# Review: what's good, what's not, what's next

Each loop ends with an honest pass over the build. New loops go on top.

---

## Loop 1 — 2026-10-19

### What I checked

- **`go test -race ./...`** covers rules, simulation against published maths, full room flow over real WebSockets (create, join, rules, start, out-of-turn roll, roll, reconnect with token, spectator, auto-roll for an absent player, leaderboard recording, pause when everyone leaves), and the leaderboard file round-trip.
- **Headless Chromium, two players at once.** One desktop (1280×800) and one phone (390×844, touch), playing 10 real rolls against the real server. Screenshots were reviewed and no console errors appeared.
- **Results and leaderboard dialogs.** These were driven by injecting server messages through Playwright's WebSocket routing.

### Good

- **The table feels like a table.** Each seat sees the board from its own side, opponents sit across from you with name cards, and the dice fly from the roller's side. This was the core of the brief and it works on both desktop and portrait phones.
- **Cheat-proof core.** The server rolls, validates turns and decides animation windows. Clients are pure renderers.
- **Mobile resilience.** Seat tokens, backoff reconnect, reconnect on tab focus, auto-roll for absent players and pausing when nobody is left together mean a sleeping phone doesn't ruin the game.
- **Rules are grounded.** The default (exact roll + 6 again) comes from the research. The simulator test pins the board to the published 39.2-roll average, so a broken board or rule fails CI.
- **Zero external runtime dependencies.** Everything (three.js, font, textures) is embedded in a 7.5 MB binary. It works on a LAN with no internet.
- **Small server.** About 600 lines of Go plus tests, one dependency, one goroutine per room.

### Not good yet

- **No sound.** The dice clatter and the snake "whoosh" are half the fun of the real thing. This is the biggest gap in game feel.
- **The leaderboard trusts the browser.** The player ID is generated client-side, so anyone can make a new identity and names can collide. That is fine among friends, but not for a public board. A fix needs real accounts or at least a server-signed ID.
- **No way to talk.** There are no emotes or reactions. When friends play remotely, a quick "😂" after someone hits the 98 snake matters.
- **Slow devices lag behind.** Animations are time-based, but each hop waits for a frame. On a very slow GPU (swiftshader in tests) a client can fall a few seconds behind. The queue keeps it correct, but the countdown keeps running meanwhile.
- **One shared die is thrown "at" the roller's side.** On a portrait phone it can land partly off-screen for some seats.
- **Accessibility.** The 3D board itself is not screen-reader friendly. The players list and log are, and the log is `aria-live`, but there is no text board view.
- **Host can't kick** an AFK lobby player (their seat is reclaimed only when the table is full).
- **The board layout is duplicated** in Go and JS, and must be kept in sync by hand.
- **No persistence of live rooms.** A deploy or restart ends all games. That's acceptable for v1, and the leaderboard does persist.

### Next (planned for loop 2)

1. Synthesised sound effects (Web Audio, no files) plus a mute toggle that persists.
2. Haptic buzz when it becomes your turn (Android `navigator.vibrate`).
3. Win celebration: a confetti burst in 3D.
4. Quick reactions (emoji) that pop above your avatar for everyone.
5. Leaderboard shows your own row even if you're outside the top 20.
