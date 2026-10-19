# Review: what's good, what's not, what's next

Each loop ends with an honest pass over the build. New loops go on top.

---

## Loop 3 — 2026-10-19

### What I checked
- **`go test -race ./...`** now also covers the HTTP layer:
  - The page, the CSP header, the manifest's content type and long caching on vendored files.
  - The leaderboard returns your own row and never leaks player IDs.
  - Host removal works, along with its refusals: not host, player still connected, and wrong phase.
- **Phone-sized browser run.** A guest leaves the lobby, and the host sees "Keluarkan" and removes them. A solo game then starts and the roll lands on-screen. The manifest loads as JSON. No console errors.
- **Browser test script** is saved in `scripts/e2e_two_players.py` so it can be rerun.

### Good
- **It installs like Hafalan and Lembar:** home-screen icon and full-screen.
- **Stale lobbies can be cleaned up** without restarting the table.
- **The die stays in view** from all four seats.
- **The strict CSP held up.** It blocked Playwright's eval-based helpers until the test context bypassed it, which shows the page itself runs no eval.

### Not good yet
- **No offline shell.** There is no service worker, so opening the installed app with no signal shows the browser's offline page. The game needs a connection anyway, but a friendly "Tidak ada koneksi" screen would be nicer.
- **Leaderboard identity is still browser-trusted** (loop 1).
- **Sound needs a first tap** (loop 2, a browser rule).
- **Live games don't survive a server restart.** Deploys end running games, so deploy when nobody is playing (`/healthz` reports the room count).
- **The one big untested path is real phones on real networks.** Headless swiftshader can't show frame pacing or touch feel. Next step: play a real game on two phones.

### Next
1. Play-test on two real Android phones over mobile data, and tune the turn timer, animation speed and camera.
2. Server-signed player IDs (HMAC), so leaderboard rows can't be claimed by copying an ID.
3. Service worker with an offline fallback page.
4. Optional: a Nusantara-themed board (wayang snakes, bambu ladders) as a second layout. The board is already data-driven.

---

## Loop 2 — 2026-10-19

### What I checked
- **Race tests pass,** including new cases for reactions (relay, whitelist, cooldown) and pausing when everyone leaves. The cooldown test watches every message up to a marker, so a leaked reaction fails it.
- **Two-player browser run (desktop + phone):** a reaction is sent, appears in the other player's list, and no console errors occur.
- **Confetti** was verified by stepping its tween by hand. In headless swiftshader each frame takes seconds, so time-based animations jump straight to the end and screenshots miss them. That is a test-environment limit, not a game bug.

### Good
- **Game feel is much better.** Sound carries most of the drama: the snake hiss followed by a falling whistle lands well.
- **No asset weight.** Sound is generated in code, so the binary barely grew.
- **Reactions are safe by construction.** Only a fixed set of emoji is accepted, there is no free text (so no moderation needed), and spam is rate-limited.
- **Abandoned games can no longer farm leaderboard wins.**

### Not good yet
- **Sound only starts after the first tap or key** (a browser rule). Someone who joins via a link and never taps hears nothing until they do.
- **Your own reaction is shown as a page overlay, not in 3D,** because you have no avatar. It works, but it is less charming.
- **The leaderboard still trusts the browser ID** (from loop 1).
- **No kick for AFK lobby players** (from loop 1).
- **Can't install it like an app.** There is no manifest or icon, unlike the Hafalan/Lembar PWAs.
- **Dice sometimes land at the screen edge on portrait phones** for side seats.

### Next (loop 3)
1. PWA manifest and icon, so it can be added to the home screen.
2. Keep the dice landing on-screen for every seat.
3. Host can remove a disconnected player from the lobby.

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
