// App glue: lobby, room HUD, networking and the move queue.
import { World, SEAT_COLORS } from './scene.js';
import { Connection } from './net.js';
import { sfx, unlock, isMuted, setMuted } from './audio.js';

const REACTIONS = ['😂', '😱', '🔥', '👏', '😭', '🐍'];

const $ = (id) => document.getElementById(id);
const store = {
  get(k) { try { return localStorage.getItem(k); } catch { return null; } },
  set(k, v) { try { localStorage.setItem(k, v); } catch { /* private mode */ } },
  del(k) { try { localStorage.removeItem(k); } catch { /* private mode */ } },
};

// A stable, random ID per browser so the leaderboard survives renames.
function playerId() {
  let id = store.get('ut.pid');
  if (!id) {
    id = crypto.randomUUID ? crypto.randomUUID() : Math.random().toString(36).slice(2) + Date.now().toString(36);
    store.set('ut.pid', id);
  }
  return id;
}

const CLASSIC_BOARD = {
  ladders: [[4, 14], [9, 31], [21, 42], [28, 84], [51, 67], [72, 91], [80, 99]].map(([from, to]) => ({ from, to })),
  snakes: [[17, 7], [54, 34], [62, 19], [64, 60], [87, 24], [93, 73], [95, 75], [98, 79]].map(([from, to]) => ({ from, to })),
};

const app = {
  room: null,
  seat: -1,
  timing: { diceMs: 1100, stepMs: 260, jumpMs: 1000 },
  state: null,
  clockSkew: 0, // server now − client now
  queue: [],
  animating: false,
  joinedOnce: false,
  lastPhase: null,
};

await document.fonts.load('800 64px "Baloo 2"').catch(() => {});
const world = new World($('scene'));
// Show the board behind the lobby straight away; the server sends the same
// layout on join, so setBoard() then does nothing.
world.setBoard(CLASSIC_BOARD);
world.setIdle(true);
// ?debug exposes internals for browser tests and poking from the console.
if (new URLSearchParams(location.search).has('debug')) window.__ut = { world, app };

const conn = new Connection({ onMessage, onStatus });

// --- lobby ---

$('name').value = store.get('ut.name') || '';
const urlCode = new URLSearchParams(location.search).get('meja');
if (urlCode) $('join-code').value = urlCode.toUpperCase();

function myName() {
  const n = $('name').value.trim();
  if (n) store.set('ut.name', n);
  return n;
}

function requireName() {
  if (myName()) return true;
  showLobbyError('Isi nama dulu supaya teman tahu siapa kamu.');
  $('name').focus();
  return false;
}

function showLobbyError(text) {
  $('lobby-error').textContent = text;
  $('lobby-error').hidden = !text;
}

$('create').addEventListener('click', () => {
  if (!requireName()) return;
  enterRoom('');
});

$('join-form').addEventListener('submit', (e) => {
  e.preventDefault();
  const code = $('join-code').value.trim().toUpperCase();
  if (!/^[A-Z0-9]{4}$/.test(code)) {
    showLobbyError('Kode meja terdiri dari 4 huruf atau angka, misalnya K7QX.');
    return;
  }
  if (!requireName()) return;
  enterRoom(code);
});

function enterRoom(code) {
  showLobbyError('');
  app.room = code || null;
  conn.connect(() => ({
    room: app.room || '',
    name: myName(),
    token: app.room ? store.get(`ut.token.${app.room}`) : '',
    pid: playerId(),
  }));
}

// Coming back to a room link we already hold a seat in: rejoin straight away.
if (urlCode && store.get(`ut.token.${urlCode.toUpperCase()}`) && myName()) {
  enterRoom(urlCode.toUpperCase());
}

// --- server messages ---

function onMessage(msg) {
  switch (msg.t) {
    case 'welcome': return onWelcome(msg);
    case 'state': return onState(msg);
    case 'moved': return onMoved(msg);
    case 'reaction': return onReaction(msg);
    case 'error': return onError(msg);
  }
}

function onWelcome(msg) {
  app.room = msg.room;
  app.seat = msg.seat;
  app.timing = msg.timing;
  if (msg.token) store.set(`ut.token.${msg.room}`, msg.token);
  history.replaceState(null, '', `?meja=${msg.room}`);
  world.setBoard(msg.board);
  world.setSeat(msg.seat);
  world.setIdle(false);
  $('lobby').hidden = true;
  $('room').hidden = false;
  $('room-code').textContent = msg.room;
  app.joinedOnce = true;
}

function onState(st) {
  app.clockSkew = st.now - Date.now();
  const prev = app.state;
  app.state = st;
  world.setPlayers(st.players);
  if (!app.animating && app.queue.length === 0) {
    world.syncPawns(st.players);
    world.setTurn(st.turn);
  }
  renderPlayers(st);
  renderReactions();
  renderWaiting(st);
  renderRoll();

  if (st.phase === 'playing' && prev && prev.phase !== 'playing') {
    setLog('Permainan dimulai. Yang duluan sampai petak 100 menang.');
    $('results').open && $('results').close();
  }
  if (st.phase === 'finished' && app.lastPhase !== 'finished') {
    // Let the winning move land, celebrate, then show the results.
    whenIdle(async () => {
      sfx.win();
      await world.celebrate(st.winner);
      showResults(app.state);
    });
  }
  // Nudge the player when the turn comes round to them.
  if (st.phase === 'playing' && st.turn === app.seat && app.seat >= 0 && (!prev || prev.turn !== app.seat)) {
    whenIdle(() => {
      if (!myTurn()) return;
      sfx.turn();
      if (document.visibilityState === 'visible') navigator.vibrate?.([40, 60, 40]);
    });
  }
  app.lastPhase = st.phase;
}

function onMoved(msg) {
  app.queue.push(msg);
  renderRoll();
  if (!app.animating) drain();
}

async function drain() {
  app.animating = true;
  while (app.queue.length) {
    const msg = app.queue.shift();
    const m = msg.move;
    setLog('');
    await world.animateMove(m, app.timing);
    setLog(describe(m, msg.auto));
    if (app.state) world.syncPawns(app.state.players);
  }
  app.animating = false;
  if (app.state) world.setTurn(app.state.turn);
  renderRoll();
  for (const fn of idleWaiters.splice(0)) fn();
}

const idleWaiters = [];
function whenIdle(fn) {
  if (!app.animating && app.queue.length === 0) fn();
  else idleWaiters.push(fn);
}

// seat → emoji, shown next to the name for a moment. Side seats' avatars
// are often off-screen, so the players list is where everyone sees it.
const recentReactions = new Map();
const reactionTimers = new Map();

function onReaction(msg) {
  sfx.pop();
  if (msg.seat === app.seat) return; // already shown when we tapped
  world.showReaction(msg.seat, msg.emoji);
  recentReactions.set(msg.seat, msg.emoji);
  if (app.state) renderPlayers(app.state);
  clearTimeout(reactionTimers.get(msg.seat));
  reactionTimers.set(msg.seat, setTimeout(() => {
    recentReactions.delete(msg.seat);
    if (app.state) renderPlayers(app.state);
  }, 2500));
}

function showMyReaction(emoji) {
  const el = $('my-reaction');
  el.textContent = emoji;
  el.classList.remove('show');
  void el.offsetWidth; // restart the animation
  el.classList.add('show');
}

function onError(msg) {
  const text = {
    room_not_found: `Meja ${app.room || ''} tidak ada lagi. Cek kodenya atau buat meja baru.`,
    server_full: 'Server sedang penuh. Coba lagi sebentar lagi.',
    not_your_turn: 'Belum giliranmu.',
    still_moving: 'Tunggu bidaknya berhenti dulu.',
    not_host: 'Hanya tuan rumah yang bisa mengatur dan memulai.',
  }[msg.code];
  if (msg.code === 'room_not_found' || msg.code === 'server_full') {
    if (app.room) store.del(`ut.token.${app.room}`);
    backToLobby();
    showLobbyError(text);
    return;
  }
  if (text) setLog(text);
}

function onStatus(status) {
  const el = $('conn');
  if (status === 'reconnecting' && app.joinedOnce) {
    el.textContent = 'Menyambung ulang…';
    el.hidden = false;
  } else {
    el.hidden = true;
  }
}

function backToLobby() {
  conn.close();
  app.room = null;
  app.seat = -1;
  app.state = null;
  app.joinedOnce = false;
  history.replaceState(null, '', location.pathname);
  $('room').hidden = true;
  $('lobby').hidden = false;
  world.setPlayers([]);
  world.setTurn(-1);
  world.setIdle(true);
}

// --- rendering ---

function nameOf(seat) {
  const p = app.state && app.state.players.find((x) => x.seat === seat);
  return p ? p.name : '?';
}

function describe(m, auto) {
  const who = m.seat === app.seat ? 'Kamu' : nameOf(m.seat);
  let s = `${who} ${auto ? 'otomatis ' : ''}dapat ${m.dice}`;
  if (m.stuck) s += `, kelebihan langkah, tetap di ${m.from}.`;
  else if (m.kind === 'ladder') s += `, naik tangga dari ${m.landed} ke ${m.to}!`;
  else if (m.kind === 'snake') s += `, dimakan ular di ${m.landed}, turun ke ${m.to}.`;
  else if (m.bounced) s += `, mentok 100 dan mundur ke ${m.to}.`;
  else s += `, maju ke ${m.to}.`;
  if (m.won) s = `${who} sampai di 100 dan menang!`;
  else if (m.again) s += ' Dapat 6, lempar lagi.';
  return s;
}

function setLog(text) {
  $('log').textContent = text;
}

function renderPlayers(st) {
  const ol = $('players');
  ol.replaceChildren(
    ...st.players.map((p) => {
      const li = document.createElement('li');
      li.className = [p.connected ? '' : 'away', st.turn === p.seat ? 'turn' : ''].join(' ').trim();
      const dot = document.createElement('span');
      dot.className = 'pawn-dot';
      dot.style.background = SEAT_COLORS[p.seat];
      const who = document.createElement('span');
      who.className = 'who';
      who.textContent = p.name + (p.seat === app.seat ? ' (kamu)' : '');
      if (p.host) who.append(Object.assign(document.createElement('span'), { className: 'tag', textContent: ' tuan rumah' }));
      if (!p.connected) who.append(Object.assign(document.createElement('span'), { className: 'tag', textContent: ' terputus' }));
      if (recentReactions.has(p.seat)) who.append(Object.assign(document.createElement('span'), { className: 'bubble', textContent: recentReactions.get(p.seat) }));
      const where = document.createElement('span');
      where.className = 'where';
      where.textContent = p.playing ? (p.pos === 0 ? 'Mulai' : p.pos) : '';
      li.append(dot, who, where);
      return li;
    }),
  );
  const sp = $('spectators');
  sp.hidden = !st.spectators;
  sp.textContent = `${st.spectators} penonton`;
}

function amHost() {
  const me = app.state && app.state.players.find((p) => p.seat === app.seat);
  return !!(me && me.host);
}

function renderReactions() {
  const bar = $('reactions');
  if (app.seat < 0) {
    bar.replaceChildren();
    return;
  }
  if (bar.childElementCount) return;
  bar.replaceChildren(
    ...REACTIONS.map((e) => {
      const b = document.createElement('button');
      b.type = 'button';
      b.textContent = e;
      b.setAttribute('aria-label', `Kirim ${e}`);
      b.addEventListener('click', () => {
        conn.send({ t: 'react', emoji: e });
        showMyReaction(e);
        // Match the server's cooldown so taps are never silently dropped.
        for (const x of bar.children) x.disabled = true;
        setTimeout(() => { for (const x of bar.children) x.disabled = false; }, 1300);
      });
      return b;
    }),
  );
}

function renderWaiting(st) {
  const show = st.phase === 'lobby';
  $('waiting').hidden = !show;
  if (!show) return;
  const host = amHost();
  const n = st.players.filter((p) => p.connected).length;
  $('waiting-hint').textContent =
    app.seat < 0
      ? 'Kamu menonton. Kursi sudah penuh.'
      : n < 2
        ? `Kirim kode ${st.room} ke teman. Bisa juga main sendiri untuk latihan (tidak masuk papan juara).`
        : host
          ? `${n} pemain siap. Mulai kapan saja.`
          : 'Menunggu tuan rumah memulai.';
  $('rules').disabled = !host;
  for (const r of document.querySelectorAll('input[name=overshoot]')) r.checked = r.value === st.rules.overshoot;
  $('six-again').checked = st.rules.sixAgain;
  $('start').hidden = !host;
  $('leave').hidden = app.seat < 0;
}

function myTurn() {
  return app.state && app.state.phase === 'playing' && app.state.turn === app.seat;
}

let countdownTimer;
function renderRoll() {
  const st = app.state;
  const playing = st && st.phase === 'playing';
  const busy = app.animating || app.queue.length > 0;
  const btn = $('roll');
  btn.hidden = !playing || app.seat < 0;
  btn.disabled = !myTurn() || busy;
  clearInterval(countdownTimer);
  const tick = () => {
    if (!playing) {
      $('status').textContent = '';
      return;
    }
    if (busy) {
      $('status').textContent = '';
      return;
    }
    const left = Math.max(0, Math.ceil((st.deadline - (Date.now() + app.clockSkew)) / 1000));
    const who = myTurn() ? 'Giliranmu' : `Giliran ${nameOf(st.turn)}`;
    $('status').textContent = st.deadline ? `${who}, ${left} dtk` : `${who}, menunggu pemain kembali`;
  };
  tick();
  if (playing) countdownTimer = setInterval(tick, 250);
}

function showResults(st) {
  if (!st || st.phase !== 'finished') return;
  const winner = st.players.find((p) => p.seat === st.winner);
  $('results-title').textContent = st.winner === app.seat ? 'Kamu menang!' : `${winner ? winner.name : 'Seseorang'} menang!`;
  $('results-body').replaceChildren(
    ...(st.results || []).map((r) => {
      const tr = document.createElement('tr');
      if (r.won) tr.className = 'winner';
      [r.name, r.pos, r.rolls, r.ladders, r.snakes].forEach((v, i) => {
        const td = document.createElement('td');
        td.textContent = v;
        if (i > 0) td.className = 'num';
        tr.append(td);
      });
      return tr;
    }),
  );
  $('results-note').textContent = st.ranked ? 'Hasil ini masuk papan juara.' : 'Main sendiri tidak masuk papan juara.';
  $('rematch').hidden = !amHost();
  $('results').showModal();
}

// --- actions ---

function roll() {
  if (!$('roll').disabled) {
    $('roll').disabled = true;
    conn.send({ t: 'roll' });
  }
}
$('roll').addEventListener('click', roll);
addEventListener('keydown', (e) => {
  if ((e.code === 'Space' || e.code === 'Enter') && myTurn() && !e.target.closest('input, button, dialog')) {
    e.preventDefault();
    roll();
  }
});

$('start').addEventListener('click', () => conn.send({ t: 'start' }));
$('rematch').addEventListener('click', () => {
  $('results').close();
  conn.send({ t: 'start' });
});
$('results-close').addEventListener('click', () => $('results').close());
$('results-board').addEventListener('click', () => {
  $('results').close();
  openLeaderboard();
});
$('leave').addEventListener('click', () => {
  conn.send({ t: 'leave' });
  if (app.room) store.del(`ut.token.${app.room}`);
});

$('rules').addEventListener('change', () => {
  const overshoot = document.querySelector('input[name=overshoot]:checked')?.value || 'stay';
  conn.send({ t: 'rules', rules: { overshoot, sixAgain: $('six-again').checked } });
});

$('copy-link').addEventListener('click', async () => {
  const url = `${location.origin}/?meja=${app.room}`;
  const text = `Main ular tangga yuk! Meja ${app.room}: ${url}`;
  try {
    if (navigator.share && matchMedia('(pointer: coarse)').matches) {
      await navigator.share({ title: 'Ular Tangga', text, url });
    } else {
      await navigator.clipboard.writeText(url);
      $('copy-link').textContent = 'Tersalin';
      setTimeout(() => ($('copy-link').textContent = 'Salin tautan'), 1500);
    }
  } catch {
    /* user cancelled the share sheet */
  }
});

// --- leaderboard ---

$('open-board').addEventListener('click', openLeaderboard);
$('leaderboard-close').addEventListener('click', () => $('leaderboard').close());

async function openLeaderboard() {
  const body = $('leaderboard-body');
  body.replaceChildren(row(['', 'Memuat…', '', '', '']));
  $('leaderboard').showModal();
  try {
    const res = await fetch(`/api/leaderboard?pid=${encodeURIComponent(playerId())}`);
    const { players, me } = await res.json();
    if (!players.length) {
      body.replaceChildren(row(['', 'Belum ada juara. Main dengan teman untuk jadi yang pertama.', '', '', '']));
      return;
    }
    const line = (p) => {
      const tr = row([p.rank, p.name, p.wins, p.games, p.bestRolls ? `${p.bestRolls} lemparan` : '-']);
      if (me && p.rank === me.rank) tr.className = 'me';
      return tr;
    };
    const rows = players.map(line);
    if (me && me.rank > players.length) {
      const gap = document.createElement('tr');
      gap.className = 'gap';
      gap.innerHTML = '<td colspan="5">…</td>';
      rows.push(gap, line(me));
    }
    body.replaceChildren(...rows);
  } catch {
    body.replaceChildren(row(['', 'Papan juara tidak bisa dimuat. Coba lagi.', '', '', '']));
  }
}

function row(cells) {
  const tr = document.createElement('tr');
  cells.forEach((c, i) => {
    const td = document.createElement('td');
    td.textContent = c;
    if (i !== 1) td.className = 'num';
    tr.append(td);
  });
  return tr;
}

// --- sound ---

function renderMute() {
  const m = isMuted();
  $('mute').textContent = m ? 'Nyalakan suara' : 'Bisukan';
  $('mute').setAttribute('aria-pressed', String(m));
}
$('mute').addEventListener('click', () => {
  unlock();
  setMuted(!isMuted());
  renderMute();
});
renderMute();
// Browsers only start audio after a gesture.
for (const ev of ['pointerdown', 'keydown']) addEventListener(ev, unlock, { passive: true });
