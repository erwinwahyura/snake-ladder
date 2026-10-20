# Ular Tangga

Online multiplayer snakes and ladders around a 3D table. Make a table, send the four-letter code to friends, and roll. Each of the up to four players sees the board from their own side of the table.

One Go binary serves the game server, the web client and the leaderboard. There is no database, CDN or build step.

## Run it

```sh
make run          # http://localhost:8080
make test         # vet + race tests (includes a game-length simulation)
python3 scripts/e2e_two_players.py http://127.0.0.1:8080 ./shots 10   # optional browser test
```

Open two browser windows to play against yourself. A solo game works too, but it doesn't count for the leaderboard.

| Env | Flag | Default | |
|---|---|---|---|
| `ADDR` | `-addr` | `:8080` | listen address |
| `DATA_DIR` | `-data` | `./data` | `leaderboard.json`, `friends.json`, `accounts.json`, `identity.key` and `identity.key.revoked` (back up all five) |
| `ORIGINS` | `-origins` | | extra allowed WebSocket origins, comma-separated |
| `PUBLIC_URL` | | | e.g. `https://ulartangga.erwarx.com`; needed for Google sign-in |
| `GOOGLE_CLIENT_ID` | | | turns on "Masuk dengan Google" (with the next two) |
| `GOOGLE_CLIENT_SECRET` | | | |

## Deploy (ulartangga.erwarx.com)

The server side (service user, systemd unit, `/etc/snake-ladder.env`, Caddy site, data in `/mnt/apps-data/snake-ladder`) is managed by the [homelab](https://github.com/erwinwahyura/homelab) repo's `snake_ladder` role. This repo only ships the binary.

- **Automatic:** every push to `main` runs `.github/workflows/deploy.yml`: tests, builds a static linux/amd64 binary and runs `deploy/deploy.sh`. It needs the `DEPLOY_SSH_KEY`, `DEPLOY_HOST` and `DEPLOY_USER` secrets (the same ones homelab uses).
- **Manual:** `make deploy` (override the host with `DEPLOY_TARGET=user@host`).
- If games are in progress it waits for them to finish, for up to 10 minutes (`DRAIN_MINUTES`), before restarting.
- Players at a table see "Server sedang diperbarui…" during the restart.

## Google sign-in (optional)

Playing never needs an account. "Masuk dengan Google" in the lobby only attaches a Google account to the player's guest identity, so their points and Teman main follow them to any device. If the three settings above aren't set, the button simply doesn't show.

To turn it on:

1. **Create a project.** In [Google Cloud console](https://console.cloud.google.com/), create a project (for example "Ular Tangga").
2. **Set up the consent screen.** Under **APIs & Services → OAuth consent screen**:
   - User type **External**.
   - App name "Ular Tangga", plus your support email and developer email.
   - Scopes: only the defaults, `openid` and `profile`. We don't ask for email, so no verification is needed. Press **Publish app**, otherwise only listed test users can sign in.
3. **Create the client.** Under **APIs & Services → Credentials → Create credentials → OAuth client ID**:
   - Type **Web application**.
   - **Authorized redirect URIs**: `https://ulartangga.erwarx.com/auth/google/callback`. For local testing, also add `http://localhost:8080/auth/google/callback`.
4. **Configure the server.** In the homelab repo, set `snake_ladder_google_client_id` / `snake_ladder_google_client_secret` (keep them in `group_vars/vault.yml`) and run the playbook with `--tags snake_ladder`. The role writes them to `/etc/snake-ladder.env` (readable by the service only), with `PUBLIC_URL=https://<domain>`.

To run it locally:

```sh
PUBLIC_URL=http://localhost:8080 GOOGLE_CLIENT_ID=... GOOGLE_CLIENT_SECRET=... make run
```

The button is plain text, "Masuk dengan Google". If you'd like Google's official button look, its assets are on [Google's branding page](https://developers.google.com/identity/branding-guidelines).

## How it plays

- **"Cari lawan"** seats you at a table with other people looking for a game. It starts when 4 people are there, or after 30 s with bots in the empty seats.
- **"Main lawan bot"** starts a game against three bots straight away.
- **"Buat meja"** makes a table where you are the host. The host chooses:
  - 2, 3 or 4 players
  - private (code or link only) or public (listed under **Meja terbuka**)
  - bots for empty seats
  - house rules: what happens past 100, and whether a 6 rolls again
- The server rolls the dice. You have 20 seconds to roll before it rolls for you.
- If your phone sleeps, reopen the link to get your seat back. While you're away, your turns are rolled for you.
- Points: the winner gets 10, then 6 / 3 / 1 at a 4-player table. Only games with two or more people count. Wins against bots are kept separately.
- People you play with become **Teman main**. The lobby shows who's online. Invite them from the waiting screen with "Ajak teman main", or "Susul" them to their public table.
- There's no sign-up. Your record lives in this browser. "Masuk dengan Google" (optional) makes it follow you to any device. Without Google, "Pindah ke HP lain" in the leaderboard gives a one-time code for another device, and "Ganti kunci" retires a leaked key.
- Rude names are replaced automatically. Public tables can be reported, and two reports hide one.
- Tap an emoji to react. Use "Bisukan" to mute the sound.
- On a phone, "Add to Home screen" installs it like an app.

## Docs

- [docs/RESEARCH.md](docs/RESEARCH.md): rules, variants, how long games last, and what online play needs
- [docs/DESIGN.md](docs/DESIGN.md): architecture, protocol, client and visual direction
- [docs/REVIEW.md](docs/REVIEW.md): what's good, what's not, and what's next, updated every loop
- [docs/CHANGELOG.md](docs/CHANGELOG.md)

## Credits

- [three.js](https://threejs.org) r170 (MIT), vendored in `web/vendor/three`
- [Baloo 2](https://github.com/EkType/Baloo2) by Ek Type (SIL OFL 1.1), vendored in `web/vendor/fonts`
