# Design

Last updated: 2026-10-19 (loop 3)

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
internal/stats/    leaderboard: JSON file, atomic writes, ranking
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

### Identity

| Thing | Where | Purpose |
|---|---|---|
| Room code | URL `?meja=K7QX` | Find the table. 4 characters with no look-alikes. |
| Seat token | `localStorage ut.token.<room>` | Reclaim *this* seat after a reconnect. Server-generated, 130 bits. |
| Player ID | `localStorage ut.pid` | Stable per browser, used for the leaderboard. Client-generated UUID. |

## Protocol

JSON text frames, each with a `"t"` type field.

| Direction | Message | Notes |
|---|---|---|
| C→S | `start` | Host only. Lobby or finished. |
| C→S | `roll` | Your turn, not busy. |
| C→S | `rules` `{overshoot:"stay"\|"bounce", sixAgain}` | Host only, not while playing. |
| C→S | `leave` | Frees your seat in the lobby. Mid-game it is the same as disconnecting. |
| C→S | `react` `{emoji}` | Seated players only. One of 😂 😱 🔥 👏 😭 🐍, with a 1.2 s cooldown. |
| C→S | `kick` `{seat}` | Host only, outside a game, and only for a disconnected seat. |
| S→C | `welcome` | `seat` (−1 = spectator), `token`, `board`, `timing` |
| S→C | `state` | Full snapshot after every change: phase, rules, players, turn, winner, deadline, server `now`, results, ranked |
| S→C | `moved` | `move{seat,dice,from,landed,to,kind,bounced,stuck,again,won}`, `animMs`, `auto`. Always followed by a `state`. |
| S→C | `reaction` | `{seat, emoji}` |
| S→C | `error` | `code`: `not_seated`, `bad_reaction`, `bad_seat`, `still_connected`, `room_not_found`, `server_full`, `not_host`, `not_your_turn`, `still_moving`, `already_playing`, `bad_rules`, `bad_message` |

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
- **Board layout** is defined twice, in `internal/game.Classic()` and in `CLASSIC_BOARD` in `main.js`. The client copy is only there to draw the lobby background before joining; the server sends the real layout in `welcome`. Keep the two in sync.

## Visual direction

The look is a printed warung board on a teak table at night.

- **Colour.** The paper board uses light red, yellow, green and blue squares with ink numbers. Pawns are saturated glossy versions of the same four colours, so they pop against the tints. The table is dark teak under a warm pendant lamp.
- **Type and UI.** The UI is made of paper slips with a 2.5 px ink outline and a hard, stamped shadow (no soft grey shadows). The type is Baloo 2 (Ek Type, from India), which fits the game's origin and Indonesian toy packaging.
- **Language.** All copy is in Bahasa Indonesia.

## Install (PWA)

The page ships `manifest.webmanifest` (standalone display, teak theme colour) with 192/512 icons, a maskable icon and an Apple touch icon. The icons were drawn once with PIL and live in `web/icons`. There is no service worker yet; see REVIEW.

## Deploy

- **Build.** `make build-linux` produces a static binary.
- **Ship.** `make deploy` runs Ansible. It copies the binary, installs the systemd unit (sandboxed, state under `/var/lib/snake-ladder`), and drops a Caddy site block into `/etc/caddy/sites/`.
- **Caddy prerequisite.** The main Caddyfile must contain `import sites/*`.
- **Config.** Environment variables `ADDR`, `DATA_DIR` and `ORIGINS`. `ORIGINS` is only needed if the page is served from a different host than `/ws`.
