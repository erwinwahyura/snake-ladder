// Sound effects synthesised with Web Audio, so there are no files to ship.
// Browsers only allow audio after a user gesture; unlock() is called on
// the first tap or key press.
const store = {
  get: (k) => { try { return localStorage.getItem(k); } catch { return null; } },
  set: (k, v) => { try { localStorage.setItem(k, v); } catch { /* private mode */ } },
};

let ctx = null;
let master = null;
let noise = null;
let muted = store.get('ut.mute') === '1';

export function isMuted() {
  return muted;
}

export function setMuted(m) {
  muted = m;
  store.set('ut.mute', m ? '1' : '0');
  if (master) master.gain.value = m ? 0 : 0.6;
}

// isReady: the browser has let us start audio.
export function isReady() {
  return !!ctx && ctx.state === 'running';
}

// unlock starts audio; call it from a tap or key press. Resolves once
// the browser has actually started it.
export function unlock() {
  if (ctx) {
    return ctx.state === 'suspended' ? ctx.resume().catch(() => {}) : Promise.resolve();
  }
  const AC = window.AudioContext || window.webkitAudioContext;
  if (!AC) return Promise.resolve();
  ctx = new AC();
  master = ctx.createGain();
  master.gain.value = muted ? 0 : 0.6;
  master.connect(ctx.destination);
  // One second of white noise, reused for clatter and hiss.
  noise = ctx.createBuffer(1, ctx.sampleRate, ctx.sampleRate);
  const d = noise.getChannelData(0);
  for (let i = 0; i < d.length; i++) d[i] = Math.random() * 2 - 1;
  return ctx.state === 'running' ? Promise.resolve() : ctx.resume().catch(() => {});
}

function ready() {
  return ctx && !muted && ctx.state === 'running';
}

function tone(freq, start, dur, { type = 'sine', gain = 0.3, to = null } = {}) {
  const o = ctx.createOscillator();
  const g = ctx.createGain();
  o.type = type;
  o.frequency.setValueAtTime(freq, start);
  if (to) o.frequency.exponentialRampToValueAtTime(to, start + dur);
  g.gain.setValueAtTime(0.0001, start);
  g.gain.exponentialRampToValueAtTime(gain, start + 0.01);
  g.gain.exponentialRampToValueAtTime(0.0001, start + dur);
  o.connect(g).connect(master);
  o.start(start);
  o.stop(start + dur + 0.02);
}

function burst(start, dur, { freq = 2500, q = 1.5, gain = 0.4 } = {}) {
  const src = ctx.createBufferSource();
  src.buffer = noise;
  const f = ctx.createBiquadFilter();
  f.type = 'bandpass';
  f.frequency.value = freq;
  f.Q.value = q;
  const g = ctx.createGain();
  g.gain.setValueAtTime(gain, start);
  g.gain.exponentialRampToValueAtTime(0.0001, start + dur);
  src.connect(f).connect(g).connect(master);
  src.start(start, Math.random() * 0.5, dur + 0.05);
}

export const sfx = {
  // Dice rattling in a hand, then knocking on the wooden table.
  dice(ms = 1100) {
    if (!ready()) return;
    const t = ctx.currentTime;
    for (let i = 0; i < 7; i++) burst(t + i * 0.045, 0.03, { freq: 3200 + Math.random() * 1500, gain: 0.25 });
    const land = (ms / 1000) * 0.85;
    [0.35, 0.62, 0.8, 0.92].forEach((k, i) => {
      const at = t + land * k;
      burst(at, 0.05, { freq: 900 + i * 120, q: 4, gain: 0.5 - i * 0.1 });
      tone(180 + i * 15, at, 0.08, { gain: 0.25 - i * 0.05 });
    });
  },
  // A pawn tapping onto a square.
  hop(i = 0) {
    if (!ready()) return;
    tone(520 + (i % 6) * 40, ctx.currentTime, 0.07, { type: 'triangle', gain: 0.18 });
  },
  ladder() {
    if (!ready()) return;
    const t = ctx.currentTime;
    [523, 659, 784, 1047, 1319].forEach((f, i) => tone(f, t + i * 0.09, 0.18, { type: 'triangle', gain: 0.2 }));
  },
  snake() {
    if (!ready()) return;
    const t = ctx.currentTime;
    burst(t, 0.35, { freq: 5000, q: 0.8, gain: 0.18 }); // hiss
    tone(700, t + 0.15, 0.85, { type: 'sawtooth', gain: 0.08, to: 110 });
  },
  stuck() {
    if (!ready()) return;
    const t = ctx.currentTime;
    tone(220, t, 0.12, { type: 'square', gain: 0.08 });
    tone(196, t + 0.13, 0.16, { type: 'square', gain: 0.08 });
  },
  turn() {
    if (!ready()) return;
    const t = ctx.currentTime;
    tone(880, t, 0.15, { gain: 0.2 });
    tone(1320, t + 0.12, 0.25, { gain: 0.2 });
  },
  win() {
    if (!ready()) return;
    const t = ctx.currentTime;
    [523, 659, 784, 1047].forEach((f, i) => tone(f, t + i * 0.12, 0.35, { type: 'triangle', gain: 0.22 }));
    [523, 659, 784].forEach((f) => tone(f * 2, t + 0.55, 0.9, { type: 'sine', gain: 0.12 }));
  },
  pop() {
    if (!ready()) return;
    tone(600, ctx.currentTime, 0.09, { gain: 0.15, to: 1200 });
  },
};
