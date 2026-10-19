# Changelog

## Loop 1 — 2026-10-19

### Added
- **Go monolith.** One binary (`cmd/server`) serves the web client from `go:embed`, the WebSocket game server at `/ws`, `/api/leaderboard` and `/healthz`. It shuts down gracefully and sets security headers including a strict CSP.
- **`internal/game`.** The classic 100-tile board (7 ladders, 8 snakes) and two house rules: what happens past 100 (`stay` or `bounce`) and whether a 6 rolls again. It also tracks per-game stats (rolls, ladders, snakes).
- **`internal/room`.** One goroutine per room. Features:
  - 4 seats with host and spectators
  - Reconnect tokens
  - 20 s turn timer with auto-roll (2 s for disconnected players)
  - A pause when no seated player is connected
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
