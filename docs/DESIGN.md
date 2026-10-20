# Design

Last updated: 2026-10-20 (loop 8)

## Shape

One Go binary serves everything:

```
browser ──HTTP──► /            static client (go:embed web/)
        ──WS────► /ws          game server
        ──HTTP──► /api/leaderboard
                  /healthz
```

```
cmd/server/        main: flags/env, routes, graceful shutdown, security headers
internal/game/     pure rules: board, house rules, Apply(), Game.Roll(), per-seat stats
internal/room/     Hub (room registry), Room (one goroutine per table), WebSocket conn
internal/stats/    leaderboard: JSON file, atomic writes, ranking by points, bot games apart
internal/identity/ server-signed guest player IDs (HMAC), revocation, one-time transfer codes
internal/social/   Teman main (friends.json), presence socket, invites
internal/names/    name filter for rude words
internal/auth/     optional Google sign-in (OAuth 2.0 code flow + PKCE, server side)
web/               index.html, style.css, manifest.webmanifest, icons/, js/{main,scene,net,textures,audio}.js, vendor/{three,fonts}
scripts/           browser end-to-end test (Playwright)
deploy/            systemd unit, Caddy block, Ansible playbook
docs/              this file, RESEARCH, REVIEW, CHANGELOG
```

The only Go dependency is `github.com/coder/websocket`. three.js r170 and the Baloo 2 font are vendored into `web/vendor`. There is no CDN, no build step and no npm at runtime.

## Server

### Room = actor

Each room runs one goroutine that owns all of its state: seats, phase, game, timers. Connections never touch that state. They `post()` events (`joinEv`, `leaveEv`, `msgEv`) into the room's inbox. The room is the only goroutine that sends on or closes a client's `send` channel. That rule removes a whole class of races, and `go test -race` covers the full flow.

The `select` loop handles four things: inbox events, the turn timer, an idle ticker, and quit.

- **Slow clients never block a room.** Sends are non-blocking. If a client's 32-message buffer is full, the room drops that client, which can then reconnect with its token.
- **Idle rooms close themselves.** A room with no connections for 10 minutes removes itself from the hub.
- **Hub cap.** The hub refuses new rooms past 1000.
- **Pause when empty.** If no seated player is connected, the turn timer stops. It resumes when someone comes back.

### Phases

```
lobby ──start (host)──► playing ──someone reaches 100──► finished
  ▲                                                        │
  └──────────────── start (host) = rematch ◄───────────────┘
```

- **Lobby.** New connections take a free seat (0–3). The first player becomes host. The host can change the rules. Seats whose player disconnected are reclaimed by newcomers, and are dropped when the game starts.
- **Playing.** Newcomers become spectators. A disconnected player keeps their seat. Their turn auto-rolls after 2 s, and their token reclaims the seat when they come back.
- **Finished.** Results are sent in the state message. The host can start a rematch with whoever is still connected.

### Turns and timing

The server decides the dice value. It also decides how long the clients may spend animating:

```
anim = dice 1100ms + steps × 260ms + (snake|ladder ? 1000ms : 0) + settle 400ms
```

`busyUntil = now + anim`. A roll before `busyUntil` is refused (`still_moving`). The turn deadline is `busyUntil + 20 s` (or `+ 2 s` for a disconnected player). When the timer fires, the server rolls for that player and marks the move `auto`. The timing constants are sent to clients in `welcome`, so both sides agree.

### Table setup and bots

- **Size.** `size` (2–4) decides which seat numbers are in use: `[0,2]` for two players (opposite each other), `[0,1,2]` for three, all four otherwise.
- **Who gets a seat.** A person joining outside a game gets an empty seat first, then the seat of someone who left the lobby, then a bot's seat.
- **Bots** are seats with `bot=true` and no client. When it is a bot's turn, the turn timer is set to `BotThink` (900 ms) plus up to half again. The roll goes out as a normal move with `auto=false`.
- **Bots don't count as people.** They are never host. They don't keep a game running when every person has left; the pause rule only counts people.
- **Public list.** Each room publishes a `Listing` (an atomic pointer) whenever its state changes. A room is listed only while it is public, not playing, has at least one person, and has a seat a person could take. `Hub.Public()` reads the pointers without touching room state, sorted by most people, then newest.

### Cari lawan (quick match)

- **Joining.** `/ws?match=1` calls `Hub.QuickMatch()`. It picks the listed `match` table with the most people, or creates a room and runs `openMatch()` on it: public, 4 seats, default rules, no host, and `matchTimer` set to `MatchWait` (30 s).
- **Starting.** The room starts as soon as every open seat holds a connected person (`maybeStartMatch` after each join). When the timer fires instead, it clears seats of people who left, fills the rest with bots and starts. If no person is connected it does nothing, and the room idles out.
- **Losing the race.** A searcher who arrives after the last seat went gets `match_full`, and the client searches again.
- **One search at a time.** `QuickMatch` holds `matchMu` for the whole choose-or-create. The newest match table is kept as `fresh` and offered to the next searcher even before it's listed (a table is listed only once a person sits). So two simultaneous searchers can't open two tables.
- **Shrinking wait.** After each join, `tightenMatch` pulls the deadline in to `now + MatchWaitMore` (15 s) with 2 people and `now + MatchWaitMore/2` with 3. It never pushes the deadline later.
- **Listing.** Match tables are listed only in the lobby phase. `state.matchAt` carries the deadline for the countdown.

### Points and recording

`game.Placements()` ranks seats when a game ends: the winner first, then by square, with ties sharing a place. Points by place are 10/3 (2 players), 10/5/2 (3 players) and 10/6/3/1 (4 players).

| People at the table | What's recorded |
|---|---|
| 2 or more | Points, wins, rolls and so on, for the people only; bots still take places |
| 1 plus bots | `botGames` / `botWins` only, off the ranking |
| 1 alone | Nothing |

### Friends, presence and invites

```
browser ──/ws/presence──► social.Service ◄── room: Seated(id, code, on), Met(players)
                              │  Friends (friends.json)
                              └─► invite toast on the friend's presence socket
```

- **Friends.** When a game starts, the room passes `Met()` the people at the table (ID → name). Every pair becomes mutual friends. Each list keeps the 50 most recent.
- **Presence.** Each browser keeps one `/ws/presence?pid=` socket open, in the lobby and at a table. It only receives.
- **Status.** A friend is `playing` if they're seated anywhere, `online` if they have a presence socket, and offline otherwise. `table` is set only when that table is in the public list, which makes "Susul" possible without exposing private tables.
- **Invites.** An invite travels over the game socket to the room, then to `Service.Invite()`. That checks friendship and rate limits (20 s per friend, 10 per minute) and pushes `{t:"invite", from, room}` to every presence socket the friend has open.
- **Locking.** The social service never holds its lock while asking the hub whether a table is listed, so there is no lock-order cycle with room code.

### Reports

`POST /api/rooms/{code}/report?pid=` is passed to the room. Two distinct reporters who aren't seated at the table set `delisted`, which keeps it out of `Hub.Public()` for good.

### Identity

| Thing | Where | Purpose |
|---|---|---|
| Room code | URL `?meja=K7QX` | Find the table. 4 characters with no look-alikes. |
| Seat token | `localStorage ut.token.<room>` | Reclaim *this* seat after a reconnect. Server-generated, 130 bits. |
| Player ID | `localStorage ut.pid` | `<id>.<hmac>`, issued by `GET /api/identity` and checked on `/ws`, `/ws/presence` and every API. The leaderboard and friends store only `<id>`. |
| Transfer code | shown on screen | `POST /api/identity/transfer` creates a 6-character code (10 min, one use). `POST /api/identity/claim?code=` returns the player ID, with a per-IP limit. |
| Google link | `accounts.json` | Google `sub` → player ID. No email or name stored. |
| Retired IDs | `identity.key.revoked` | `POST /api/identity/rotate` issues a new ID, moves the record and friends, and revokes the old ID. |

## Protocol

JSON text frames, each with a `"t"` type field.

| Direction | Message | Notes |
|---|---|---|
| C→S | `start` | Host only. Lobby or finished. |
| C→S | `roll` | Your turn, not busy. |
| C→S | `rules` `{overshoot:"stay"\|"bounce", sixAgain}` | Host only, not while playing. |
| C→S | `leave` | Frees your seat in the lobby. Mid-game it is the same as disconnecting. |
| C→S | `react` `{emoji}` | Seated players only. One of 😂 😱 🔥 👏 😭 🐍, with a 1.2 s cooldown. |
| C→S | `kick` `{seat}` | Host only, outside a game, and only for a disconnected person. |
| C→S | `config` `{size?, public?}` | Host only, outside a game. Size is 2–4. |
| C→S | `bot` `{action:"add"\|"remove"\|"fill", seat?}` | Host only, outside a game. |
| C→S | `invite` `{friend}` | Seated player, outside a game. Answered with `invited`. |
| S→C | `welcome` | `seat` (−1 = spectator), `token`, `board`, `timing` |
| S→C | `state` | Full snapshot after every change: phase, rules, players, turn, winner, deadline, server `now`, results, ranked |
| S→C | `moved` | `move{seat,dice,from,landed,to,kind,bounced,stuck,again,won}`, `animMs`, `auto`. Always followed by a `state`. |
| S→C | `reaction` | `{seat, emoji}`, sent by people and sometimes by bots |
| S→C | `invited` | `{friend, result}`: `""` means delivered, otherwise `friend_offline`, `invite_too_soon`, `invite_rate_limit` or `not_friend` |
| S→C | `error` | `code`: `not_seated`, `bad_reaction`, `bad_seat`, `still_connected`, `bad_size`, `seats_taken`, `no_free_seat`, `room_not_found`, `server_full`, `not_host`, `not_your_turn`, `still_moving`, `already_playing`, `bad_rules`, `bad_message` |

The connection URL is `/ws?room=CODE&name=NAME&token=TOKEN&pid=PLAYERID`. An empty `room` creates a new one.

Full snapshots rather than diffs keep reconnects trivial: the client just renders the latest state. At most four players makes each snapshot about 600 bytes.

## Client

- **`main.js`** handles the lobby, HUD, dialogs and the move queue. Incoming `moved` messages are queued and animated one by one. `state` messages update the HUD immediately, but pawn positions are only snapped to the server's numbers once the queue is empty. That way an animation never jumps, and the client can never drift from the server.
- **`scene.js`** holds the three.js world. The board is 10×10 units, with tile 1 in the south-west corner.
  - **Seats.** Seat *k* views the table rotated *k*×90°, so every player sees the board from their own side, like a real table. Other players appear as simple avatars with name cards.
  - **Camera framing.** The camera distance is computed so the board fits the screen. Portrait phones look down more steeply.
  - **Animation.** The dice are thrown from the roller's side and tumble to the server's value using a quaternion that puts the face up. Pawns hop tile by tile, climb ladders and slide down the snake's actual curve.
- **`textures.js`** draws everything on canvases: the board, wood grain, snake skins, dice faces and name labels. The game ships no image files.
- **`audio.js`** synthesises every sound with Web Audio oscillators and filtered noise, so there are no audio files. The mute setting is stored in `localStorage ut.mute`.
- **`net.js`** handles reconnection with exponential backoff, and reconnects immediately when the tab becomes visible again.
- **`?debug`** exposes `window.__ut = {world, app}` for browser tests.
- **Board layout** is defined once, in `internal/game.Classic()`. The lobby draws it from `GET /api/board`, and `welcome` carries the same layout.
- **Catch-up.** `drain()` plays queued moves at 0.45× duration while more are waiting, and places all but the newest instantly when more than three are queued.

## Visual direction

The look is a printed warung board on a teak table at night.

- **Colour.** The paper board uses light red, yellow, green and blue squares with ink numbers. Pawns are saturated glossy versions of the same four colours, so they pop against the tints. The table is dark teak under a warm pendant lamp.
- **Type and UI.** The UI is made of paper slips with a 2.5 px ink outline and a hard, stamped shadow (no soft grey shadows). The type is Baloo 2 (Ek Type, from India), which fits the game's origin and Indonesian toy packaging.
- **Language.** All copy is in Bahasa Indonesia.

## Install (PWA)

The page ships `manifest.webmanifest` (standalone display, teak theme colour) with 192/512 icons, a maskable icon and an Apple touch icon. The icons were drawn once with PIL and live in `web/icons`. There is no service worker yet; see REVIEW.

## Google sign-in

```
page ──POST /auth/google {pid}──► server: state+PKCE, cookie ut_oauth=state ──303──► Google
Google ──GET /auth/google/callback?code&state──► server: cookie == state? exchange code (+secret, verifier)
       ──► ID token: aud, iss, exp checked ──► link or find player ──► one-time code
server ──303 /?login=CODE&nama=First──► page: POST /api/identity/claim → pid
```

- **Which identity:**
  - Google account already linked: that player. A guest record on this device that isn't someone else's account is merged in, and the guest key is revoked.
  - New Google account: this device's guest identity, if it isn't linked to another Google account; otherwise a fresh one.
- **Sign-out** is client-side: drop the key and ask `/api/identity` for a new guest.
- **Config:** `PUBLIC_URL`, `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET`, all three or it stays off. `GOOGLE_AUTH_URL` / `GOOGLE_TOKEN_URL` exist only to point at `scripts/fake_google.py` in tests.

## Restarts and deploys

- **Restart notice.** `Hub.Close()` (on SIGTERM) broadcasts `{"t":"server_restart"}` to every table before stopping it; queued messages are still written before the socket closes. The client shows "Server sedang diperbarui…" while it reconnects, and when the table turns out to be gone it explains why.
- **Waiting for games.** `/healthz` → `playing` counts rooms whose `active` flag is set (a game running with someone connected), updated on every state broadcast. `deploy/deploy.sh` polls it every 10 s and restarts only at 0, or after `DRAIN_MINUTES`.
- **Service worker** (`web/sw.js`).
  - Precaches `offline.html`, `style.css`, the three font weights and one icon.
  - Navigation is network-first, falling back to `offline.html`. Other assets are network-first with a cache fallback. `/api/*` and `/ws*` are passed through untouched.

## Deploy

- **Build.** `make build-linux` produces a static binary.
- **Ship.** `make deploy` runs Ansible. It copies the binary, installs the systemd unit (sandboxed, state under `/var/lib/snake-ladder`), and drops a Caddy site block into `/etc/caddy/sites/`.
- **Server setup.** Caddy site, systemd unit and env file live in the homelab repo (`snake_ladder` role); the server listens on 127.0.0.1:8091 because 8090 is taken by daily-kotoba.
- **Config.** Environment variables `ADDR`, `DATA_DIR` and `ORIGINS`. `ORIGINS` is only needed if the page is served from a different host than `/ws`.
