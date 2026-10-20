// App glue: lobby, room HUD, networking and the move queue.
import { World, SEAT_COLORS } from './scene.js';
import { Connection } from './net.js';
import { sfx, unlock, isMuted, setMuted, isReady as audioReady } from './audio.js';
import { Presence } from './presence.js';

const REACTIONS = ['😂', '😱', '🔥', '👏', '😭', '🐍'];

const $ = (id) => document.getElementById(id);
const store = {
  get(k) { try { return localStorage.getItem(k); } catch { return null; } },
  set(k, v) { try { localStorage.setItem(k, v); } catch { /* private mode */ } },
  del(k) { try { localStorage.removeItem(k); } catch { /* private mode */ } },
};

// Guest identity: a server-signed ID kept in this browser, so the
// leaderboard and Teman main survive renames without any sign-up.
// "Pindah ke HP lain" moves it with a one-time code.
function playerId() {
  return store.get('ut.pid') || '';
}

async function ensureIdentity() {
  try {
    const res = await fetch(`/api/identity?pid=${encodeURIComponent(playerId())}`);
    const { pid, google, googleEnabled } = await res.json();
    store.set('ut.pid', pid);
    app.account = { google: !!google, enabled: !!googleEnabled };
  } catch {
    /* offline: keep whatever we have; games just won't be recorded */
  }
  renderAccount();
}

// --- Masuk dengan Google (optional) ---

const LOGIN_ERRORS = {
  cancelled: 'Masuk dengan Google dibatalkan. Kamu tetap bisa main tanpa masuk.',
  state: 'Tautan masuk itu bukan dari perangkat ini. Tekan "Masuk dengan Google" lagi.',
  failed: 'Masuk dengan Google gagal. Coba lagi sebentar lagi.',
};

function renderAccount() {
  const a = app.account || {};
  $('account-line').hidden = !a.enabled;
  $('google-in').hidden = $('google-hint').hidden = !!a.google;
  $('google-status').hidden = $('google-out').hidden = !a.google;
}

function showLobbyNote(text) {
  $('lobby-note').textContent = text;
  $('lobby-note').hidden = !text;
}

// Back from Google: the server left a one-time code (never the identity
// itself) in the URL, which we trade for the identity.
async function handleLoginReturn() {
  const params = new URLSearchParams(location.search);
  const code = params.get('login');
  const err = params.get('login_error');
  const nama = params.get('nama');
  if (!code && !err) return;
  for (const k of ['login', 'login_error', 'nama']) params.delete(k);
  history.replaceState(null, '', location.pathname + (params.toString() ? `?${params}` : ''));
  if (err) {
    showLobbyError(LOGIN_ERRORS[err] || LOGIN_ERRORS.failed);
    return;
  }
  try {
    const res = await fetch(`/api/identity/claim?code=${encodeURIComponent(code)}`, { method: 'POST' });
    if (!res.ok) throw new Error();
    store.set('ut.pid', (await res.json()).pid);
    if (nama && !$('name').value.trim()) {
      $('name').value = nama.slice(0, 16);
      myName();
    }
    showLobbyNote('Berhasil masuk dengan Google. Poin dan teman mainmu sekarang ikut ke perangkat mana pun.');
  } catch {
    showLobbyError(LOGIN_ERRORS.failed);
  }
}

// A form post, so the guest identity travels in the body, not the URL.
$('google-in').addEventListener('click', () => {
  const form = Object.assign(document.createElement('form'), { method: 'POST', action: '/auth/google' });
  form.append(Object.assign(document.createElement('input'), { type: 'hidden', name: 'pid', value: playerId() }));
  document.body.append(form);
  form.submit();
});

$('google-out').addEventListener('click', async () => {
  if (!confirm('Keluar dari akun Google di perangkat ini? Poin dan teman mainmu tetap aman di akunmu. Di sini kamu main sebagai tamu baru.')) return;
  store.del('ut.pid');
  await ensureIdentity();
  presence.restart();
  loadFriends();
  showLobbyNote('Kamu sudah keluar. Masuk lagi kapan saja dengan Google.');
});

function setIdentity(pid) {
  store.set('ut.pid', pid);
  presence.restart();
  loadFriends();
  ensureIdentity(); // refresh whether this identity has Google attached
}

// Seats in use for a table size; two players sit opposite each other.
function openSeatsFor(size) {
  return size === 2 ? [0, 2] : [0, 1, 2, 3].slice(0, size);
}

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
  intent: null, // 'bots' when "Main lawan bot" is setting up its table
  matchRetries: 0,
  restarting: false, // the server said it is about to restart
  account: null, // { google: linked?, enabled: sign-in offered? }
};

await document.fonts.load('800 64px "Baloo 2"').catch(() => {});
const world = new World($('scene'));
// Draw the board behind the lobby straight away. The server is the only
// place the layout lives; on join it sends the same one, and setBoard()
// then does nothing.
fetch('/api/board')
  .then((r) => r.json())
  .then((b) => world.setBoard(b))
  .catch(() => {});
world.setIdle(true);
// Lets the installed app show a friendly screen when there's no signal.
if ('serviceWorker' in navigator) navigator.serviceWorker.register('/sw.js').catch(() => {});
// ?debug exposes internals for browser tests and poking from the console.
if (new URLSearchParams(location.search).has('debug')) window.__ut = { world, app };

const conn = new Connection({ onMessage, onStatus });
const presence = new Presence({ getPid: playerId, onInvite: showInvite });

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

// Cari lawan: the server seats you at the busiest waiting table, or opens
// one; it starts when full, or after 30 s with bots in the empty seats.
$('quick-match').addEventListener('click', () => {
  if (!requireName()) return;
  app.matchRetries = 0;
  findMatch();
});

function findMatch() {
  showLobbyError('');
  app.room = null;
  conn.connect(() => ({
    room: app.room || '',
    match: app.room ? '' : '1',
    name: myName(),
    token: app.room ? store.get(`ut.token.${app.room}`) : '',
    pid: playerId(),
  }));
}

// One tap to a full table: you plus three bots, starting straight away.
$('vs-bots').addEventListener('click', () => {
  if (!requireName()) return;
  app.intent = 'bots';
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

await handleLoginReturn();
await ensureIdentity();
presence.open();

// Coming back to a room link we already hold a seat in: rejoin straight away.
if (urlCode && store.get(`ut.token.${urlCode.toUpperCase()}`) && myName()) {
  enterRoom(urlCode.toUpperCase());
}

// --- open tables ---

const RULES_SHORT = (r) => `${r.overshoot === 'bounce' ? 'mundur di 100' : 'pas 100'}${r.sixAgain ? ', 6 lempar lagi' : ''}`;

async function loadRooms() {
  if (!$('lobby').hidden) {
    try {
      const { rooms } = await (await fetch('/api/rooms')).json();
      renderRooms(rooms);
    } catch {
      renderRooms([]);
    }
  }
}

function renderRooms(rooms) {
  $('room-empty').hidden = rooms.length > 0;
  $('room-list').replaceChildren(
    ...rooms.map((r) => {
      const li = document.createElement('li');
      const info = document.createElement('div');
      info.append(Object.assign(document.createElement('b'), { textContent: r.match ? 'Cari lawan' : `Meja ${r.host}` }));
      const people = `${r.humans}/${r.size} orang${r.bots ? `, ${r.bots} bot` : ''}${r.match ? ', mulai otomatis' : ''}`;
      info.append(Object.assign(document.createElement('span'), { className: 'meta', textContent: `${people}, ${RULES_SHORT(r.rules)}` }));
      const join = Object.assign(document.createElement('button'), { className: 'btn small', textContent: 'Gabung', type: 'button' });
      join.setAttribute('aria-label', r.match ? 'Gabung ke meja Cari lawan' : `Gabung ke meja ${r.host}`);
      join.addEventListener('click', () => {
        if (requireName()) enterRoom(r.code);
      });
      const report = Object.assign(document.createElement('button'), { className: 'link small', textContent: 'Laporkan', type: 'button' });
      report.setAttribute('aria-label', `Laporkan meja ${r.host}`);
      report.addEventListener('click', async () => {
        if (!confirm(`Laporkan meja ${r.host}? Meja yang dilaporkan dua orang tidak ditampilkan lagi di Meja terbuka.`)) return;
        await fetch(`/api/rooms/${r.code}/report?pid=${encodeURIComponent(playerId())}`, { method: 'POST' }).catch(() => {});
        report.textContent = 'Dilaporkan';
        report.disabled = true;
      });
      const actions = Object.assign(document.createElement('div'), { className: 'row-actions' });
      actions.append(report, join);
      li.append(info, actions);
      return li;
    }),
  );
}

$('refresh-rooms').addEventListener('click', loadRooms);
loadRooms();
setInterval(loadRooms, 5000);

// --- teman main ---

const STATUS_TEXT = { online: 'online', playing: 'sedang main', '': 'offline' };
let friends = [];

async function loadFriends() {
  try {
    ({ friends } = await (await fetch(`/api/friends?pid=${encodeURIComponent(playerId())}`)).json());
  } catch {
    friends = [];
  }
  renderFriends();
  if ($('invite-dialog').open) renderInviteList();
}

function friendInfo(f) {
  const info = document.createElement('div');
  const dot = Object.assign(document.createElement('span'), { className: `status-dot ${f.status}` });
  info.append(dot, Object.assign(document.createElement('b'), { textContent: f.name }));
  info.append(Object.assign(document.createElement('span'), { className: 'meta', textContent: STATUS_TEXT[f.status] ?? 'offline' }));
  return info;
}

function renderFriends() {
  $('friend-empty').hidden = friends.length > 0;
  $('friend-list').replaceChildren(
    ...friends.slice(0, 8).map((f) => {
      const li = document.createElement('li');
      const actions = Object.assign(document.createElement('div'), { className: 'row-actions' });
      const remove = Object.assign(document.createElement('button'), { className: 'link small', textContent: 'Hapus', type: 'button' });
      remove.setAttribute('aria-label', `Hapus ${f.name} dari teman main`);
      remove.addEventListener('click', async () => {
        await fetch(`/api/friends/remove?pid=${encodeURIComponent(playerId())}&id=${encodeURIComponent(f.id)}`, { method: 'POST' }).catch(() => {});
        loadFriends();
      });
      actions.append(remove);
      // A friend waiting at a public table can be followed straight in.
      if (f.table) {
        const follow = Object.assign(document.createElement('button'), { className: 'btn small', textContent: 'Susul', type: 'button' });
        follow.setAttribute('aria-label', `Susul ${f.name} ke mejanya`);
        follow.addEventListener('click', () => {
          if (requireName()) enterRoom(f.table);
        });
        actions.append(follow);
      }
      li.append(friendInfo(f), actions);
      return li;
    }),
  );
}

loadFriends();
setInterval(() => {
  if (!$('lobby').hidden || $('invite-dialog').open) loadFriends();
}, 5000);

// Invites: a friend's invite lands as a toast, wherever you are.
let pendingInvite = null;
function showInvite(msg) {
  if (msg.room === app.room) return; // already at that table
  pendingInvite = msg;
  $('invite-text').textContent = `${msg.from} mengajak kamu main di meja ${msg.room}.`;
  $('invite-join').textContent = app.room ? 'Pindah ke sana' : 'Gabung';
  $('invite-toast').hidden = false;
  sfx.turn();
  navigator.vibrate?.(60);
  clearTimeout(showInvite.timer);
  showInvite.timer = setTimeout(() => ($('invite-toast').hidden = true), 30000);
}
$('invite-dismiss').addEventListener('click', () => ($('invite-toast').hidden = true));
$('invite-join').addEventListener('click', () => {
  $('invite-toast').hidden = true;
  if (!pendingInvite) return;
  if (!myName()) $('name').value = 'Pemain';
  if (app.room) {
    conn.send({ t: 'leave' });
    backToLobby();
  }
  enterRoom(pendingInvite.room);
});

function renderInviteList() {
  const list = $('invite-list');
  if (!friends.length) {
    list.replaceChildren(Object.assign(document.createElement('li'), { textContent: 'Belum ada teman main. Kirim tautan meja lewat WhatsApp dulu, nanti mereka muncul di sini.' }));
    return;
  }
  const here = new Set((app.state?.players || []).map((p) => p.name));
  list.replaceChildren(
    ...friends.map((f) => {
      const li = document.createElement('li');
      li.dataset.id = f.id;
      const btn = Object.assign(document.createElement('button'), { className: 'btn small', type: 'button' });
      if (here.has(f.name) && f.status === 'playing') {
        btn.textContent = 'Sudah di sini';
        btn.disabled = true;
      } else if (f.status !== 'online') {
        btn.textContent = f.status === 'playing' ? 'Sedang main' : 'Offline';
        btn.disabled = true;
      } else {
        btn.textContent = 'Ajak';
        btn.addEventListener('click', () => {
          btn.disabled = true;
          btn.textContent = 'Mengirim…';
          conn.send({ t: 'invite', friend: f.id });
        });
      }
      li.append(friendInfo(f), btn);
      return li;
    }),
  );
}

function onInvited(msg) {
  const btn = document.querySelector(`#invite-list li[data-id="${CSS.escape(msg.friend)}"] button`);
  const text = {
    '': 'Terkirim',
    friend_offline: 'Sudah offline',
    invite_too_soon: 'Tunggu sebentar',
    invite_rate_limit: 'Terlalu banyak ajakan',
    not_friend: 'Bukan teman main',
  }[msg.result] ?? 'Gagal';
  if (btn) {
    btn.textContent = text;
    btn.disabled = true;
  }
}

$('invite-open').addEventListener('click', async () => {
  $('invite-dialog').showModal();
  renderInviteList();
  await loadFriends();
});
$('invite-close').addEventListener('click', () => $('invite-dialog').close());

// --- server messages ---

function onMessage(msg) {
  switch (msg.t) {
    case 'welcome': return onWelcome(msg);
    case 'state': return onState(msg);
    case 'moved': return onMoved(msg);
    case 'reaction': return onReaction(msg);
    case 'invited': return onInvited(msg);
    case 'server_restart':
      app.restarting = true;
      return onStatus('reconnecting');
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
  if (app.intent === 'bots' && msg.seat >= 0) {
    // The room handles these in order: size, three bots, go.
    conn.send({ t: 'config', size: 4, public: false });
    conn.send({ t: 'bot', action: 'fill' });
    conn.send({ t: 'start' });
  }
  app.intent = null;
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

// Moves are animated in order. A phone that falls behind (slow GPU, or
// the tab was in the background) catches up: with a backlog, moves play
// faster, and with a long one, all but the latest jump straight to their
// squares.
async function drain() {
  app.animating = true;
  while (app.queue.length) {
    if (app.queue.length > 3) {
      for (const skipped of app.queue.splice(0, app.queue.length - 1)) {
        world.placePawn(skipped.move.seat, skipped.move.to);
      }
    }
    const msg = app.queue.shift();
    const m = msg.move;
    const f = app.queue.length > 0 ? 0.45 : 1;
    const t = app.timing;
    setLog('');
    await world.animateMove(m, { diceMs: t.diceMs * f, stepMs: t.stepMs * f, jumpMs: t.jumpMs * f });
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
    seats_taken: 'Ada pemain di kursi yang mau dihapus. Minta dia keluar dulu, atau pilih jumlah yang lebih besar.',
    no_free_seat: 'Semua kursi sudah terisi.',
  }[msg.code];
  if (msg.code === 'match_full') {
    // Someone took the last seat first; look again.
    if (app.matchRetries++ < 3) return findMatch();
    backToLobby();
    showLobbyError('Belum dapat meja. Coba "Cari lawan" lagi.');
    return;
  }
  if (msg.code === 'room_not_found' || msg.code === 'server_full') {
    if (app.room) store.del(`ut.token.${app.room}`);
    backToLobby();
    showLobbyError(
      app.restarting && msg.code === 'room_not_found'
        ? 'Server baru saja diperbarui, jadi permainan tadi berakhir. Maaf! Silakan mulai lagi.'
        : text,
    );
    app.restarting = false;
    return;
  }
  if (text) setLog(text);
  if (app.state) renderWaiting(app.state); // undo a refused setup change in the controls
}

function onStatus(status) {
  const el = $('conn');
  if (status === 'reconnecting' && app.joinedOnce) {
    el.textContent = app.restarting ? 'Server sedang diperbarui…' : 'Menyambung ulang…';
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
  loadRooms();
  loadFriends();
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
  const rows = st.players.map((p) => {
      const li = document.createElement('li');
      li.className = [p.connected ? '' : 'away', st.turn === p.seat ? 'turn' : ''].join(' ').trim();
      const dot = document.createElement('span');
      dot.className = 'pawn-dot';
      dot.style.background = SEAT_COLORS[p.seat];
      const who = document.createElement('span');
      who.className = 'who';
      who.textContent = p.name + (p.seat === app.seat ? ' (kamu)' : '');
      if (p.host) who.append(Object.assign(document.createElement('span'), { className: 'tag', textContent: ' tuan rumah' }));
      if (p.bot) who.append(Object.assign(document.createElement('span'), { className: 'tag', textContent: ' 🤖' }));
      if (!p.connected) who.append(Object.assign(document.createElement('span'), { className: 'tag', textContent: ' terputus' }));
      if (recentReactions.has(p.seat)) who.append(Object.assign(document.createElement('span'), { className: 'bubble', textContent: recentReactions.get(p.seat) }));
      const where = document.createElement('span');
      where.className = 'where';
      where.textContent = p.playing ? (p.pos === 0 ? 'Mulai' : p.pos) : '';
      // Outside a game the host can remove bots and people who have left.
      if (amHost() && st.phase !== 'playing' && p.seat !== app.seat && (p.bot || !p.connected)) {
        const kick = document.createElement('button');
        kick.className = 'btn small';
        kick.textContent = 'Keluarkan';
        kick.setAttribute('aria-label', `Keluarkan ${p.name} dari meja`);
        kick.addEventListener('click', () =>
          conn.send(p.bot ? { t: 'bot', action: 'remove', seat: p.seat } : { t: 'kick', seat: p.seat }));
        where.replaceChildren(kick);
      }
      li.append(dot, who, where);
      return li;
    });
  // Empty seats before a game, so everyone sees how many are still open.
  if (st.phase !== 'playing') {
    const taken = new Set(st.players.map((p) => p.seat));
    for (const seat of openSeatsFor(st.size)) {
      if (taken.has(seat)) continue;
      const li = document.createElement('li');
      li.className = 'empty';
      const dot = Object.assign(document.createElement('span'), { className: 'pawn-dot' });
      dot.style.background = SEAT_COLORS[seat];
      li.append(dot, Object.assign(document.createElement('span'), { className: 'who', textContent: 'Kursi kosong' }), document.createElement('span'));
      rows.push(li);
    }
  }
  ol.replaceChildren(...rows);
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

let matchTicker;
function renderWaiting(st) {
  const show = st.phase === 'lobby';
  $('waiting').hidden = !show;
  clearInterval(matchTicker);
  if (!show) return;
  // A Cari lawan table has no host and nothing to set up: just the clock.
  for (const id of ['setup', 'rules', 'start']) $(id).hidden = st.match;
  $('leave').textContent = st.match ? 'Batal cari' : 'Keluar dari kursi';
  $('waiting').querySelector('h2').textContent = st.match ? 'Mencari lawan' : 'Menunggu pemain';
  if (st.match) {
    const people = st.players.filter((p) => !p.bot && p.connected).length;
    const tick = () => {
      const left = Math.max(0, Math.ceil((st.matchAt - (Date.now() + app.clockSkew)) / 1000));
      $('waiting-hint').textContent =
        `${people}/${st.size} orang. Mulai dalam ${left} dtk, atau langsung saat meja penuh. Kursi kosong diisi bot.`;
    };
    tick();
    matchTicker = setInterval(tick, 250);
    $('invite-open').hidden = app.seat < 0;
    return;
  }
  const host = amHost();
  const people = st.players.filter((p) => !p.bot && p.connected).length;
  const bots = st.players.filter((p) => p.bot).length;
  const empty = openSeatsFor(st.size).length - st.players.length;
  let hint;
  if (app.seat < 0) hint = 'Kamu menonton. Kursi sudah penuh.';
  else if (!host) hint = 'Menunggu tuan rumah memulai.';
  else if (people < 2 && bots === 0) hint = `Kirim kode ${st.room} ke teman${st.public ? ', atau tunggu orang dari Meja terbuka' : ''}. Bisa juga tambah bot.`;
  else if (empty > 0) hint = `${empty} kursi masih kosong. Mulai sekarang atau tunggu yang lain.`;
  else hint = 'Meja penuh. Mulai kapan saja.';
  if (host && people === 1 && bots > 0) hint += ' Lawan bot dicatat terpisah, tanpa poin.';
  $('waiting-hint').textContent = hint;
  $('setup').disabled = !host;
  for (const r of document.querySelectorAll('input[name=size]')) r.checked = Number(r.value) === st.size;
  for (const r of document.querySelectorAll('input[name=visibility]')) r.checked = (r.value === 'public') === st.public;
  $('add-bot').disabled = $('fill-bots').disabled = empty <= 0;
  $('rules').disabled = !host;
  for (const r of document.querySelectorAll('input[name=overshoot]')) r.checked = r.value === st.rules.overshoot;
  $('six-again').checked = st.rules.sixAgain;
  $('start').hidden = !host;
  $('leave').hidden = app.seat < 0;
  $('invite-open').hidden = app.seat < 0;
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
      if (r.seat === app.seat) tr.classList.add('me');
      const name = r.bot ? `${r.name} 🤖` : r.name;
      [r.place, name, r.pos, r.ladders, r.snakes, st.ranked && !r.bot ? `+${r.points}` : '-'].forEach((v, i) => {
        const td = document.createElement('td');
        td.textContent = v;
        if (i !== 1) td.className = 'num';
        tr.append(td);
      });
      return tr;
    }),
  );
  const humans = (st.results || []).filter((r) => !r.bot).length;
  $('results-note').textContent = st.ranked
    ? 'Poin masuk papan juara.'
    : humans === 1 && st.results.length > 1
      ? 'Lawan bot dicatat terpisah, tanpa poin.'
      : 'Main sendiri tidak dicatat.';
  $('rematch').hidden = !amHost() && !st.match;
  $('rematch').textContent = st.match ? 'Cari lawan lagi' : 'Main lagi';
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
  if (app.state?.match) {
    backToLobby();
    app.matchRetries = 0;
    findMatch();
    return;
  }
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
  if (app.state?.match) backToLobby(); // nothing to watch at a table you stopped waiting for
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
    const bot = $('my-bot-record');
    bot.hidden = !(me && me.botGames);
    if (me && me.botGames) bot.textContent = `Lawan bot: kamu menang ${me.botWins} dari ${me.botGames} permainan.`;
    if (!players.length) {
      body.replaceChildren(row(['', 'Belum ada juara. Main dengan teman untuk jadi yang pertama.', '', '', '']));
      return;
    }
    const line = (p) => {
      const tr = row([p.rank, p.name, p.points, p.wins, p.games]);
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

// Moving to another device: a 6-letter code, 10 minutes, one use.
const transferMsg = (t) => ($('transfer-msg').textContent = t);
$('make-code').addEventListener('click', async () => {
  try {
    const res = await fetch(`/api/identity/transfer?pid=${encodeURIComponent(playerId())}`, { method: 'POST' });
    if (!res.ok) throw new Error();
    const { code } = await res.json();
    $('my-code').textContent = code;
    $('my-code').hidden = false;
    transferMsg('Masukkan kode ini di perangkat baru dalam 10 menit.');
  } catch {
    transferMsg('Kode tidak bisa dibuat. Coba lagi.');
  }
});
$('claim-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  const code = $('claim-code').value.trim().toUpperCase();
  if (!/^[A-Z0-9]{6}$/.test(code)) {
    transferMsg('Kode pindah terdiri dari 6 huruf atau angka.');
    return;
  }
  try {
    const res = await fetch(`/api/identity/claim?code=${code}`, { method: 'POST' });
    if (res.status === 429) return transferMsg('Terlalu banyak percobaan. Coba lagi beberapa menit lagi.');
    if (!res.ok) return transferMsg('Kode tidak ditemukan atau sudah kedaluwarsa. Buat kode baru di perangkat lama.');
    setIdentity((await res.json()).pid);
    $('claim-code').value = '';
    transferMsg('Berhasil. Catatan dan teman mainmu sekarang ada di perangkat ini.');
    openLeaderboard();
  } catch {
    transferMsg('Tidak bisa terhubung. Coba lagi.');
  }
});
$('rotate-key').addEventListener('click', async () => {
  if (!confirm('Ganti kunci? Kunci lama berhenti berlaku di semua perangkat. Catatan dan teman mainmu tetap.')) return;
  try {
    const res = await fetch(`/api/identity/rotate?pid=${encodeURIComponent(playerId())}`, { method: 'POST' });
    if (!res.ok) throw new Error();
    setIdentity((await res.json()).pid);
    transferMsg('Kunci sudah diganti. Perangkat lain perlu kode pindah baru.');
  } catch {
    transferMsg('Kunci tidak bisa diganti. Coba lagi.');
  }
});

// --- table setup (host) ---

$('setup').addEventListener('change', (e) => {
  if (e.target.name === 'size') conn.send({ t: 'config', size: Number(e.target.value) });
  if (e.target.name === 'visibility') conn.send({ t: 'config', public: e.target.value === 'public' });
});
$('add-bot').addEventListener('click', () => conn.send({ t: 'bot', action: 'add' }));
$('fill-bots').addEventListener('click', () => conn.send({ t: 'bot', action: 'fill' }));

// --- sound ---

// Browsers only start audio after a tap. Someone who rejoins straight from
// a link may never have tapped, so the button says so until they do.
function renderMute() {
  const m = isMuted();
  const locked = !m && !audioReady();
  $('mute').textContent = locked ? '🔈 Ketuk untuk suara' : m ? 'Nyalakan suara' : 'Bisukan';
  $('mute').setAttribute('aria-pressed', String(m));
}
$('mute').addEventListener('click', async () => {
  if (!isMuted() && !audioReady()) {
    await unlock(); // the first tap only turns sound on
  } else {
    await unlock();
    setMuted(!isMuted());
  }
  renderMute();
});
renderMute();
for (const ev of ['pointerdown', 'keydown']) {
  addEventListener(
    ev,
    (e) => {
      if (e.target.closest?.('#mute')) return; // the button handles its own tap
      unlock().then(renderMute);
    },
    { passive: true },
  );
}
