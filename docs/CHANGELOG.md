# Changelog

## Loop 8 — 2026-10-20

### Added
- **"Masuk dengan Google" (optional).** A button in the lobby attaches a Google account to the player's guest identity, so points and Teman main follow them to any device. Playing still never needs an account.
  - **Server-side OAuth 2.0 code flow with PKCE** (`internal/auth`). No Google script in the page, so the strict CSP stays. Scopes are only `openid profile`.
  - **What is stored:** only Google's stable account ID (`DATA_DIR/accounts.json`). No email; the first name only pre-fills the display name.
  - **First sign-in** keeps the current guest identity, now linked.
  - **Signing in on another device** gives that same identity. Any guest record on that device is merged in, and the leftover guest key is retired.
  - **Someone already linked who signs in with a second Google account** gets a separate identity. Nothing is stolen or merged.
  - **Safety:**
    - The state is single-use, expires after 10 minutes, and is tied to an HttpOnly SameSite cookie, so a callback link sent by someone else can't sign you in to their account.
    - The identity comes back as a one-time code, never as a token in the URL.
    - The start is a form POST, so the guest token never appears in a URL.
  - **"Keluar"** on a device switches back to a fresh guest. "Ganti kunci" keeps the Google link.
  - **Off by default.** It turns on when `PUBLIC_URL`, `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` are set. The Ansible playbook writes them to `/etc/snake-ladder.env` when `google_client_id` and `google_client_secret` are given.
- **Teman main merge.** Merging a guest into an account now merges the two friend lists (it used to overwrite), and nobody ends up as their own friend.
- **Test tooling.** `scripts/fake_google.py` plus `scripts/e2e_google_signin.py` exercise the whole browser flow without real Google.

## Loop 7 — 2026-10-20

### Fixed
- **Cari lawan no longer splits people who search at the same moment.** Searches are serialised in the hub, and a just-opened table is offered to the next searcher even before its first person has sat down (when it isn't listed yet).
- **Stale selector in `scripts/e2e_bots_and_public.py`.** Since "Laporkan" was added in loop 5, the script was clicking it instead of "Gabung". Found by rerunning every browser script this loop.
- **The board layout lives only on the server.** The lobby draws it from `GET /api/board`; the copy in `main.js` is gone.

### Added
- **Shorter Cari lawan wait as people arrive.** Two people: start within 15 s. Three people: within 7.5 s. Four: at once. One person alone still waits the full 30 s.
- **Friendly offline screen.** A service worker (`sw.js`) shows "Tidak ada koneksi" with a **Coba lagi** button when the app is opened with no connection.
  - It is network-first for everything, so a deploy is picked up on the next load and old JavaScript never talks to a new server. The cache is only an offline fallback.
  - Game sockets and `/api` are never touched.
- **Restart notice.** On shutdown the server tells every table it is restarting. Players see "Server sedang diperbarui…", and afterwards "Server baru saja diperbarui, jadi permainan tadi berakhir. Maaf!" instead of a bare "table not found".
- **Deploys wait for games to finish.**
  - `/healthz` now reports `playing`: games with someone connected.
  - The Ansible playbook waits for it to reach 0 before restarting, for up to `drain_minutes` (10), then deploys anyway and says how many games were still running.
  - The port is now a playbook variable.
- **Catch-up animation.** If moves pile up (a slow phone, or the tab was in the background), they play at about twice the speed. With more than three waiting, all but the latest jump straight to their squares.
- **"🔈 Ketuk untuk suara".** When the browser hasn't allowed audio yet (rejoining straight from a link with no tap), the sound button says so. The first tap turns sound on rather than muting.

## Loop 6 — 2026-10-19

### Added
- **Cari lawan** (quick match), the big button in the lobby.
  - The server seats you at the waiting Cari lawan table with the most people, or opens a new one (`/ws?match=1`).
  - These tables have no host, 4 seats and the standard rules. They're public, so they also appear under Meja terbuka as "Cari lawan, mulai otomatis".
  - A table starts **as soon as all 4 seats hold people**. Otherwise it starts **after 30 s**, with bots in the empty seats. Either way you're playing within 30 seconds.
  - The waiting screen shows people seated and a countdown, plus "Ajak teman main" and "Batal cari".
  - If someone takes the last seat first, the client quietly searches again (up to 3 times).
  - After the game, "Cari lawan lagi" starts a new search.
  - Points work as before: with at least one other person it's ranked, and against bots only it's recorded separately. People met this way become Teman main.
- **A Cari lawan table that everyone has left** doesn't start with only bots; it idles out.

## Loop 5 — 2026-10-19

### Added
- **Teman main.** People who sit down to a game together become each other's "teman main", up to 50, with the most recently played kept. The lobby lists them with status: 🟢 online, 🟡 playing, or offline. A friend waiting at a public table can be followed in with **Susul**. **Hapus** removes someone from your list only.
- **Presence socket** (`/ws/presence`). While the app is open you show as online and can receive invites anywhere: in the lobby or at another table.
- **Invites.** "Ajak teman main" on the waiting screen lists your friends, and **Ajak** sends an invite to online friends. It arrives as a toast with **Gabung**, or **Pindah ke sana** if you are already at a table. Limits: friends only, at most one invite per friend every 20 s, and 10 per minute.
- **Name filter** (`internal/names`). Indonesian and English swear words are caught, including l33t spelling, repeated letters and spaced-out letters, while ordinary names such as "Santai" or "Pantai" are left alone. A caught name is shown as "Pemain N".
- **Report a public table.** Each entry in Meja terbuka has **Laporkan**. Reports from two different players take the table off the list; it can still be joined by code. Reporting your own table does nothing.
- **Safer device transfer.** The permanent `?akun=` link is replaced by a **6-character code**, valid for 10 minutes and usable once. Claiming is rate-limited to 20 attempts per 10 minutes per IP.
- **Ganti kunci.** Issues a new ID that keeps your record and friends, and retires the old one everywhere (`DATA_DIR/identity.key.revoked`).
- **Bot reactions.** Bots sometimes react once a move has played out: 🐍 or 😭 when they're eaten, 🔥 on a ladder, 😂 when a person is eaten.

### Fixed
- **Flaky identity test.** Its "tampered token" case could pick the same first character as the real token. It is now deterministic, checked over 50 runs.

### Removed
- **`?akun=` account links** (replaced by transfer codes).

## Loop 4 — 2026-10-19

### Added
- **Table size.** The host picks 2, 3 or 4 players before the game. Two players sit opposite each other. Shrinking the table is refused if a connected person sits in a seat that would go away.
- **Private or public tables.** Tables are private by default (code or link only). Public tables appear under **Meja terbuka** in the lobby (`GET /api/rooms`) while they have a seat free or a bot that can be replaced. The list shows the host, how many people and bots are seated, and the rules. It refreshes every 5 s.
- **Bots.**
  - The host can add a bot, fill every empty seat, or remove a bot. Bots have local names (Bot Ucok, Bot Siti, ...).
  - Bots roll on the server after a short random pause (0.9–1.35 s).
  - A person joining in the lobby takes a bot's seat if there is no empty one.
- **"Main lawan bot".** One tap from the lobby gives you a private 4-seat table with 3 bots that starts immediately.
- **Points.**
  - Finishing places earn points: winner 10, then 6 / 3 / 1 at a 4-player table (10/5/2 for 3 players, 10/3 for 2). Pawns on the same square share a place.
  - The leaderboard now ranks by points, then wins, then win rate. The results table shows each player's place and points.
- **Bot games kept apart.** One person against bots is recorded as bot games and bot wins with no points, shown as a line under the leaderboard. It never appears on the ranking.
- **Signed guest identity (`internal/identity`).**
  - Player IDs are issued and HMAC-signed by the server (`GET /api/identity`), so they can't be forged. A forged or unknown ID still plays, but isn't recorded.
  - The key lives in `DATA_DIR/identity.key`. Back it up together with `leaderboard.json`.
- **"Pindah ke HP atau browser lain".** A private `?akun=` link moves your record to another device without an account.
- **Docs.** RESEARCH covers how comparable games handle table size, bots, login, invites and points.

### Fixed
- **Public list lagging behind the table.** The list is now updated before the state is broadcast, so it never shows a table older than what players see.

## Loop 3 — 2026-10-19

### Added
- **Installable as an app (PWA).** Adds a web manifest and icons (normal, maskable and Apple touch), so Android and iOS can put Ular Tangga on the home screen and open it full-screen.
- **Host can remove away players.** In the lobby or after a game, the host sees a "Keluarkan" button next to anyone who has disconnected. Connected players can't be removed.

### Changed
- **Dice landing spot.** The dice now land just off the board edge near the middle of the roller's side, so they stay on-screen from every seat and on portrait phones.

### Fixed
- **Changelog accuracy.** The "pause when everyone leaves" fix shipped in loop 1, not loop 2.

## Loop 2 — 2026-10-19

### Added
- **Sound.** All effects are synthesised with Web Audio, so there are no audio files. Covered events: dice rattle and table knocks, pawn taps, ladder arpeggio, snake hiss and slide, stuck buzz, your-turn chime, win fanfare and reaction pop. A mute toggle in the top bar persists across visits.
- **Your-turn nudge.** A chime plays and phones vibrate (Android) when the turn comes round to you. It waits until the previous move has finished animating.
- **Win celebration.** A confetti burst comes from the winner's pawn before the results appear. It is skipped with reduced motion.
- **Emoji reactions.** The set is 😂 😱 🔥 👏 😭 🐍. They appear above the sender's avatar and next to their name in the player list, because side seats are often off-screen. The server only relays this fixed set and applies a 1.2 s cooldown per connection.
- **Leaderboard shows your own row.** Your row is highlighted, and appended below the top 20 if you are lower down (`/api/leaderboard?pid=`). It can also be opened from the results dialog.
- **`?debug`.** Exposes `window.__ut` for browser tests.

### Fixed
- **Results table alignment.** Names are left-aligned and numbers right-aligned.
- **Lobby background.** The board is drawn behind the lobby before you join, instead of an empty frame.
- **Spectators get a steeper overview camera,** so the seat-0 avatar no longer blocks the view.

## Loop 1 — 2026-10-19

### Added
- **Go monolith.** One binary (`cmd/server`) serves the web client from `go:embed`, the WebSocket game server at `/ws`, `/api/leaderboard` and `/healthz`. It shuts down gracefully and sets security headers including a strict CSP.
- **`internal/game`.** The classic 100-tile board (7 ladders, 8 snakes) and two house rules: what happens past 100 (`stay` or `bounce`) and whether a 6 rolls again. It also tracks per-game stats (rolls, ladders, snakes).
- **`internal/room`.** One goroutine per room. Features:
  - 4 seats with host and spectators
  - Reconnect tokens
  - 20 s turn timer with auto-roll (2 s for disconnected players)
  - A pause when no seated player is connected, so an abandoned game can't hand anyone a leaderboard win
  - Server-timed animation windows
  - Rematch
  - Idle-room cleanup
- **`internal/stats`.** Persistent leaderboard in a JSON file with atomic writes. Only games with 2+ players count. Ranking: wins, then win rate, then fewest rolls in a win.
- **3D client (three.js r170, vendored).**
  - Teak table with a seat-relative camera for each player, and opponent avatars with name cards.
  - Canvas-drawn printed board, tapered striped snakes with heads, ladders, and glossy pawns.
  - Tumbling dice that land on the server's value.
  - Hop, climb and slide animations, plus a turn ring.
- **UI in Bahasa Indonesia.** Lobby, room HUD, rules toggles, invite link and share sheet, results dialog, leaderboard dialog, keyboard rolling (Space/Enter) and reduced-motion support.
- **Deploy.** Makefile, systemd unit, Caddy block and Ansible playbook.
- **Docs.** RESEARCH (rules, variants, simulated game length), DESIGN, REVIEW and this changelog.

### Decided
- **Default rules: exact roll to finish + 6 rolls again.** On this board, bounce makes games about 50% longer (see RESEARCH).
- **No accounts for v1.** A per-browser player ID is used for the leaderboard (see REVIEW for the trade-off).
