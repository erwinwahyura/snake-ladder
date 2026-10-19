// Canvas-drawn textures. Everything is generated at load, so the game
// ships no image files.
import * as THREE from '../vendor/three/three.module.min.js';

export const INK = '#1b1a17';
export const PAPER = '#fff6da';

// Light tints, like the cheap printed boards sold at warung: red, yellow,
// green and blue squares. Pawns use the saturated versions so they stand out.
const TILE_TINTS = ['#f29484', '#f7d160', '#8bcf98', '#8fb3ea'];

function canvas(w, h = w) {
  const c = document.createElement('canvas');
  c.width = w;
  c.height = h;
  return [c, c.getContext('2d')];
}

function finish(c, renderer, { repeat = false } = {}) {
  const t = new THREE.CanvasTexture(c);
  t.colorSpace = THREE.SRGBColorSpace;
  if (renderer) t.anisotropy = renderer.capabilities.getMaxAnisotropy();
  if (repeat) t.wrapS = t.wrapT = THREE.RepeatWrapping;
  return t;
}

// Tile n (1..100) → column/row on a 10×10 board, snaking row by row.
export function tileColRow(n) {
  const i = n - 1;
  const row = Math.floor(i / 10);
  const col = row % 2 === 0 ? i % 10 : 9 - (i % 10);
  return { col, row };
}

function star(ctx, cx, cy, r, points = 5) {
  ctx.beginPath();
  for (let i = 0; i < points * 2; i++) {
    const rr = i % 2 === 0 ? r : r * 0.45;
    const a = (Math.PI * i) / points - Math.PI / 2;
    ctx.lineTo(cx + Math.cos(a) * rr, cy + Math.sin(a) * rr);
  }
  ctx.closePath();
}

// The board face. Canvas top is the far side (row 10), so seat 0 reads it
// the right way up, like a board printed facing you.
export function boardTexture(renderer, jumps) {
  const S = 2048;
  const T = S / 10;
  const [c, ctx] = canvas(S);
  ctx.fillStyle = PAPER;
  ctx.fillRect(0, 0, S, S);

  for (let n = 1; n <= 100; n++) {
    const { col, row } = tileColRow(n);
    const x = col * T;
    const y = (9 - row) * T;
    ctx.fillStyle = TILE_TINTS[(col + row * 3) % 4];
    ctx.fillRect(x, y, T, T);

    // Print-run speckle so the squares look like ink on paper, not flat UI.
    ctx.fillStyle = 'rgba(255,255,255,0.18)';
    for (let k = 0; k < 40; k++) {
      ctx.fillRect(x + Math.random() * T, y + Math.random() * T, 3, 3);
    }

    const jump = jumps[n];
    if (jump) {
      // A small corner badge says where this square takes you.
      const up = jump > n;
      ctx.fillStyle = up ? 'rgba(30,140,69,0.9)' : 'rgba(215,38,30,0.9)';
      ctx.beginPath();
      ctx.roundRect(x + T - 92, y + T - 50, 82, 40, 12);
      ctx.fill();
      ctx.fillStyle = '#fff';
      ctx.font = '700 30px "Baloo 2", system-ui, sans-serif';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillText((up ? '▲' : '▼') + jump, x + T - 51, y + T - 28);
    }

    ctx.fillStyle = INK;
    ctx.textAlign = 'left';
    ctx.textBaseline = 'top';
    ctx.font = '800 64px "Baloo 2", system-ui, sans-serif';
    ctx.fillText(String(n), x + 14, y + 6);
  }

  // Start and finish marks.
  {
    const { col, row } = tileColRow(1);
    ctx.font = '800 44px "Baloo 2", system-ui, sans-serif';
    ctx.textAlign = 'center';
    ctx.fillStyle = INK;
    ctx.fillText('Mulai', col * T + T / 2, (9 - row) * T + T - 70);
  }
  {
    const { col, row } = tileColRow(100);
    const cx = col * T + T / 2;
    const cy = (9 - row) * T + T / 2 + 18;
    star(ctx, cx, cy, 58);
    ctx.fillStyle = '#f2b705';
    ctx.fill();
    ctx.lineWidth = 6;
    ctx.strokeStyle = INK;
    ctx.stroke();
  }

  // Grid lines last so they sit on top of everything.
  ctx.strokeStyle = INK;
  ctx.lineWidth = 4;
  for (let i = 0; i <= 10; i++) {
    ctx.beginPath();
    ctx.moveTo(i * T, 0);
    ctx.lineTo(i * T, S);
    ctx.moveTo(0, i * T);
    ctx.lineTo(S, i * T);
    ctx.stroke();
  }
  ctx.lineWidth = 14;
  ctx.strokeRect(7, 7, S - 14, S - 14);
  return finish(c, renderer);
}

// Teak table top: warm base with long irregular grain lines.
export function woodTexture(renderer) {
  const [c, ctx] = canvas(1024);
  ctx.fillStyle = '#7a4524';
  ctx.fillRect(0, 0, 1024, 1024);
  for (let i = 0; i < 220; i++) {
    const y = Math.random() * 1024;
    const a = 0.04 + Math.random() * 0.1;
    ctx.strokeStyle = Math.random() < 0.5 ? `rgba(40,18,6,${a})` : `rgba(210,140,80,${a})`;
    ctx.lineWidth = 1 + Math.random() * 4;
    ctx.beginPath();
    ctx.moveTo(0, y);
    for (let x = 0; x <= 1024; x += 64) {
      ctx.lineTo(x, y + Math.sin(x / 140 + i) * 6 + (Math.random() - 0.5) * 3);
    }
    ctx.stroke();
  }
  // A couple of knots.
  for (let k = 0; k < 3; k++) {
    const kx = Math.random() * 1024;
    const ky = Math.random() * 1024;
    for (let r = 4; r < 40; r += 5) {
      ctx.strokeStyle = 'rgba(40,18,6,0.12)';
      ctx.beginPath();
      ctx.ellipse(kx, ky, r * 2.2, r, 0, 0, Math.PI * 2);
      ctx.stroke();
    }
  }
  return finish(c, renderer, { repeat: true });
}

// Repeating band texture for a snake's body.
export function snakeSkin(renderer, base, band) {
  const [c, ctx] = canvas(256, 64);
  ctx.fillStyle = base;
  ctx.fillRect(0, 0, 256, 64);
  ctx.fillStyle = band;
  for (let x = -64; x < 256; x += 64) {
    ctx.beginPath();
    ctx.moveTo(x, 0);
    ctx.lineTo(x + 26, 0);
    ctx.lineTo(x + 58, 64);
    ctx.lineTo(x + 32, 64);
    ctx.closePath();
    ctx.fill();
  }
  ctx.fillStyle = 'rgba(255,255,255,0.12)';
  ctx.fillRect(0, 26, 256, 8);
  return finish(c, renderer, { repeat: true });
}

// Six dice faces. Pip layouts on a 3×3 grid.
const PIPS = {
  1: [[1, 1]],
  2: [[0, 0], [2, 2]],
  3: [[0, 0], [1, 1], [2, 2]],
  4: [[0, 0], [2, 0], [0, 2], [2, 2]],
  5: [[0, 0], [2, 0], [1, 1], [0, 2], [2, 2]],
  6: [[0, 0], [2, 0], [0, 1], [2, 1], [0, 2], [2, 2]],
};

export function diceFace(renderer, value) {
  const [c, ctx] = canvas(256);
  ctx.fillStyle = '#fbf7ee';
  ctx.fillRect(0, 0, 256, 256);
  // The single pip is red and larger, as on most Asian dice.
  ctx.fillStyle = value === 1 ? '#c8231b' : INK;
  const r = value === 1 ? 40 : 24;
  for (const [gx, gy] of PIPS[value]) {
    ctx.beginPath();
    ctx.arc(64 + gx * 64, 64 + gy * 64, r, 0, Math.PI * 2);
    ctx.fill();
  }
  return finish(c, renderer);
}

// A name card that floats above an avatar.
export function labelTexture(text, color) {
  const [c, ctx] = canvas(512, 128);
  ctx.font = '800 64px "Baloo 2", system-ui, sans-serif';
  const w = Math.min(500, ctx.measureText(text).width + 64);
  const x = (512 - w) / 2;
  ctx.fillStyle = PAPER;
  ctx.strokeStyle = INK;
  ctx.lineWidth = 6;
  ctx.beginPath();
  ctx.roundRect(x + 3, 14, w - 6, 100, 24);
  ctx.fill();
  ctx.stroke();
  ctx.fillStyle = color;
  ctx.beginPath();
  ctx.arc(x + 34, 64, 12, 0, Math.PI * 2);
  ctx.fill();
  ctx.fillStyle = INK;
  ctx.textAlign = 'left';
  ctx.textBaseline = 'middle';
  ctx.fillText(text, x + 54, 68, w - 70);
  return finish(c);
}

export function emojiTexture(emoji) {
  const [c, ctx] = canvas(128);
  ctx.font = '96px "Apple Color Emoji", "Segoe UI Emoji", "Noto Color Emoji", sans-serif';
  ctx.textAlign = 'center';
  ctx.textBaseline = 'middle';
  ctx.fillText(emoji, 64, 72);
  return finish(c);
}
