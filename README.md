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
| `DATA_DIR` | `-data` | `./data` | where `leaderboard.json` lives |
| `ORIGINS` | `-origins` | | extra allowed WebSocket origins, comma-separated |

## Deploy (Hetzner + Caddy + Ansible)

1. Set your host in `deploy/ansible/inventory.ini` and the domain in `deploy/ansible/playbook.yml`.
2. Make sure your main Caddyfile has `import sites/*`.
3. Run `make deploy`. It builds a static linux/amd64 binary, installs the systemd unit and adds the Caddy site.

## How it plays

- The first person at the table is the host. The host picks house rules (what happens past 100, and whether a 6 rolls again) and starts the game.
- The server rolls the dice. You have 20 seconds to roll before it rolls for you.
- If your phone sleeps, reopen the link to get your seat back. While you're away, your turns are rolled for you.
- Games with two or more players count for the leaderboard.
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
