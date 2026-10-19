# Research: what an online ular tangga needs

Last updated: 2026-10-19 (loop 1)

## The game itself

- **Origin.** Snakes and ladders comes from India, where it was a teaching game about karma (Moksha Patam): ladders were virtues, snakes were vices. Snakes outnumbered ladders, a reminder that falling is easier than climbing. It reached England in the 1890s, and the American "Chutes and Ladders" version dates from 1943. [Wikipedia][wiki]
- **Core rules.** Everyone starts off the board. Players take turns rolling one die and moving forward. A ladder's foot sends you up and a snake's head sends you down. The first player to reach 100 wins. [Wikipedia][wiki], [YM Imports rules][ym]
- **Common variants** [wiki], [ym]:
  - Exact roll to finish. If the roll would overshoot 100, the pawn stays where it is.
  - Bounce back. If the roll overshoots, the pawn walks to 100 and then back by the excess.
  - Extra turn on a 6.
  - Two dice, for a shorter game.
- **In Indonesia.** Ular tangga is a staple childhood game, sold as a cheap printed paper board. It is also widely reused in teaching, for example numeracy for children or themed boards about COVID-19 and anti-corruption. [UNM Kebajikan journal][unm], [UMPO Ultaco][ultaco], [UMPO Latantisa][latantisa] **Design note:** this is why the board art copies the printed-paper look (light red, yellow, green and blue squares, ink numbers), and why the UI is in Bahasa Indonesia.
- **"Dapat 6, lempar lagi."** Rolling again on a 6 is the house rule most of us grew up with. *Assumption, not cited.* It is on by default and can be switched off in the lobby.

## How long a game lasts on our board

Our board uses the classic Milton Bradley layout of 7 ladders and 8 snakes (`internal/game.Classic`). Wikipedia gives about 39.2 rolls on average for one player under the exact-roll rule [wiki]. I simulated our board (see "Simulator" below) and got the following:

| Rule | Solo, mean rolls | 2 players, total rolls | 4 players, total rolls |
|---|---|---|---|
| Stay (exact roll) | 39.2 (median 34) | 56 | 85 |
| Bounce back | 60.3 (median 47) | 75 | 103 |

- The solo stay result matches the published 39.2, which validates the simulator.
- **Bounce is about 50% longer** on this board, because snakes at 93, 95 and 98 punish players near the end.
- **A 6 rolling again** barely changes the total number of rolls, but it cuts rounds (time between your own turns) by roughly 17%. For example, a 4-player game drops from 21.7 to 17.9 rounds. Waiting less is what makes it feel faster.
- A roll takes about 2.5–3 s of animation plus thinking time. A 4-player game with default rules therefore runs **about 5–7 minutes**, which is the right length for a "one more game" loop.

**Decision:** default to stay + 6-again. Bounce is offered as a house-rule toggle.

## What playing online adds (design reasoning, not citations)

| Need | Why | Loop 1 status |
|---|---|---|
| Server rolls the dice | Otherwise anyone can fake a 6 | Done |
| Join by short code and link | Friends share in WhatsApp, so the code must be easy to type and say aloud | 4 characters, no 0/O/1/I. Done |
| Survive phone sleep and app switch | Mobile browsers drop WebSockets all the time | Reconnect token per seat, backoff, reconnect on tab focus. Done |
| No stalled games | One AFK player blocks everyone | 20 s turn timer with auto-roll, 2 s for disconnected players. Done |
| See your opponents | The "around a table" feel was the brief | 3D avatars and name cards at each seat, and each seat has its own camera. Done |
| Spectators | Late joiners, kids watching parents | Join mid-game as a spectator. Done |
| Leaderboard | Bragging rights | Persistent JSON, ranked games only (2+ players). Done |
| Sound and haptics | Dice clatter, ladder and snake stings carry a lot of the fun | Not yet |
| Quick reactions/emotes | Social glue when you can't talk | Not yet |
| Anti-cheat on identity | The leaderboard trusts a browser-generated ID | Known gap, see REVIEW |

## Sources

[wiki]: https://en.wikipedia.org/wiki/Snakes_and_ladders
[ym]: https://www.ymimports.com/pages/how-to-play-snakes-and-ladders
[unm]: https://ojs.unm.ac.id/kebajikan/article/view/50242
[ultaco]: https://eprints.umpo.ac.id/12894/
[latantisa]: https://eprints.umpo.ac.id/8541/

- [Snakes and ladders, Wikipedia](https://en.wikipedia.org/wiki/Snakes_and_ladders)
- [How to play snakes and ladders, YM Imports](https://www.ymimports.com/pages/how-to-play-snakes-and-ladders)
- [Modifikasi permainan ular tangga…, Jurnal Kebajikan UNM](https://ojs.unm.ac.id/kebajikan/article/view/50242)
- [Ultaco (Ular Tangga COVID-19), UMPO](https://eprints.umpo.ac.id/12894/)
- [Latantisa (Ular Tangga Anti Korupsi), UMPO](https://eprints.umpo.ac.id/8541/)
- [Markov chains and snakes and ladders, Mathematical Gazette](https://www.cambridge.org/core/journals/mathematical-gazette/article/markov-chains-and-snakes-and-ladders/A1D0E8480C638542D9D97EC6C8243B72), background on the absorbing Markov chain model

### Simulator

The solo numbers are now a regression test that runs on every `go test`: `internal/game/sim_test.go`. It plays 40,000 games per rule on the real Go rules and fails if the exact-roll mean moves away from 39.2. Latest run: stay 39.1, bounce 59.9. The multiplayer and "6 again" numbers in the table came from a one-off Python run of the same rules (20,000 games each).
