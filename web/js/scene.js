// The 3D table: board, snakes, ladders, pawns, dice and the other players.
//
// World layout: the board is 10×10 units centred on the origin, tile 1 in
// the south-west corner. Seat 0 sits on the +z side; seat k is rotated
// k × 90° around the y axis, so every player sees the board from their side.
import * as THREE from '../vendor/three/three.module.min.js';
import { OrbitControls } from '../vendor/three/OrbitControls.js';
import { RoundedBoxGeometry } from '../vendor/three/RoundedBoxGeometry.js';
import { boardTexture, woodTexture, snakeSkin, diceFace, labelTexture, tileColRow } from './textures.js';

export const SEAT_COLORS = ['#d7261e', '#1f5fc0', '#1e8c45', '#f2b705'];
export const SEAT_NAMES = ['Merah', 'Biru', 'Hijau', 'Kuning'];

const BOARD_TOP = 0.16;
const TABLE_HALF = 8.5;
const SNAKE_SKINS = [
  ['#2f8f3a', '#f2c230'],
  ['#b8322a', '#1b1a17'],
  ['#6d3c9c', '#efe4ff'],
  ['#e07a1f', '#2a2a2a'],
];
// Each seat's pawn sits in its own corner of a shared tile.
const PAWN_OFFSET = [[-0.24, 0.24], [0.24, 0.24], [0.24, -0.24], [-0.24, -0.24]];

const reducedMotion = matchMedia('(prefers-reduced-motion: reduce)').matches;

function seatAngle(seat) {
  return (Math.max(0, seat) * Math.PI) / 2;
}

// Rotate a seat-local (x, z) point into world space.
function fromSeat(seat, x, y, z) {
  const a = seatAngle(seat);
  const c = Math.cos(a);
  const s = Math.sin(a);
  return new THREE.Vector3(x * c + z * s, y, -x * s + z * c);
}

export function tileCenter(n) {
  const { col, row } = tileColRow(n);
  return new THREE.Vector3(col - 4.5, BOARD_TOP, 4.5 - row);
}

const ease = (t) => (t < 0.5 ? 2 * t * t : 1 - (-2 * t + 2) ** 2 / 2);

export class World {
  constructor(canvas) {
    this.canvas = canvas;
    this.renderer = new THREE.WebGLRenderer({ canvas, antialias: true });
    this.renderer.setPixelRatio(Math.min(devicePixelRatio, 2));
    this.renderer.outputColorSpace = THREE.SRGBColorSpace;
    this.renderer.toneMapping = THREE.ACESFilmicToneMapping;
    this.renderer.toneMappingExposure = 0.95;
    this.renderer.shadowMap.enabled = true;
    this.renderer.shadowMap.type = THREE.PCFSoftShadowMap;

    this.scene = new THREE.Scene();
    this.scene.background = new THREE.Color('#1c130d');
    this.scene.fog = new THREE.Fog('#1c130d', 28, 60);

    this.camera = new THREE.PerspectiveCamera(42, 1, 0.1, 200);
    this.controls = new OrbitControls(this.camera, canvas);
    this.controls.enableDamping = true;
    this.controls.enablePan = false;
    this.controls.minDistance = 8;
    this.controls.maxDistance = 30;
    this.controls.minPolarAngle = 0.15;
    this.controls.maxPolarAngle = 1.2;

    this.tweens = new Set();
    this.pawns = new Map(); // seat → mesh
    this.pawnTile = new Map(); // seat → tile shown
    this.avatars = [];
    this.snakeCurves = new Map(); // head tile → curve
    this.mySeat = 0;
    this.boardBuilt = false;

    this.buildRoom();
    this.buildDice();
    this.buildTurnRing();
    this.setSeat(0);
    this.resize();
    addEventListener('resize', () => this.resize());
    this.renderer.setAnimationLoop((t) => this.frame(t));
  }

  // --- static set ---

  buildRoom() {
    const hemi = new THREE.HemisphereLight('#ffe3bd', '#3a2414', 0.9);
    this.scene.add(hemi);

    const lamp = new THREE.SpotLight('#ffd7a0', 900, 50, 0.75, 0.55, 1.6);
    lamp.position.set(2, 20, 3);
    lamp.target.position.set(0, 0, 0);
    lamp.castShadow = true;
    lamp.shadow.mapSize.set(1024, 1024);
    lamp.shadow.bias = -0.0004;
    this.scene.add(lamp, lamp.target);

    // The pendant lamp shade, just visible above the table.
    const shade = new THREE.Mesh(
      new THREE.ConeGeometry(1.6, 1.1, 32, 1, true),
      new THREE.MeshStandardMaterial({ color: '#d9a441', side: THREE.DoubleSide, roughness: 0.6 }),
    );
    shade.position.set(2, 21.5, 3);
    this.scene.add(shade);

    const wood = woodTexture(this.renderer);
    wood.repeat.set(2, 2);
    const woodMat = new THREE.MeshStandardMaterial({ map: wood, roughness: 0.55, metalness: 0.05 });
    const top = new THREE.Mesh(new THREE.BoxGeometry(TABLE_HALF * 2, 0.5, TABLE_HALF * 2), woodMat);
    top.position.y = -0.25;
    top.receiveShadow = true;
    this.scene.add(top);
    const legGeo = new THREE.BoxGeometry(0.8, 6, 0.8);
    for (const [x, z] of [[1, 1], [1, -1], [-1, 1], [-1, -1]]) {
      const leg = new THREE.Mesh(legGeo, woodMat);
      leg.position.set(x * (TABLE_HALF - 0.8), -3.5, z * (TABLE_HALF - 0.8));
      this.scene.add(leg);
    }
    const floor = new THREE.Mesh(
      new THREE.PlaneGeometry(200, 200),
      new THREE.MeshStandardMaterial({ color: '#2b1e15', roughness: 1 }),
    );
    floor.rotation.x = -Math.PI / 2;
    floor.position.y = -6.5;
    floor.receiveShadow = true;
    this.scene.add(floor);

    // Board frame, waiting for its printed face in setBoard().
    const frame = new THREE.Mesh(
      new THREE.BoxGeometry(10.7, BOARD_TOP, 10.7),
      new THREE.MeshStandardMaterial({ color: '#8f2a1d', roughness: 0.7 }),
    );
    frame.position.y = BOARD_TOP / 2;
    frame.castShadow = true;
    frame.receiveShadow = true;
    this.scene.add(frame);

    // Four chairs; avatars are added on top when someone sits.
    const chairMat = new THREE.MeshStandardMaterial({ color: '#4a2a17', roughness: 0.8 });
    for (let s = 0; s < 4; s++) {
      const chair = new THREE.Group();
      const seatBox = new THREE.Mesh(new THREE.BoxGeometry(3, 0.3, 2.4), chairMat);
      seatBox.position.set(0, -1.4, 10.4);
      const back = new THREE.Mesh(new THREE.BoxGeometry(3, 3.4, 0.3), chairMat);
      back.position.set(0, 0.2, 11.6);
      chair.add(seatBox, back);
      chair.rotation.y = seatAngle(s);
      this.scene.add(chair);
    }
  }

  buildDice() {
    const mats = [3, 4, 1, 6, 2, 5].map(
      (v) => new THREE.MeshStandardMaterial({ map: diceFace(this.renderer, v), roughness: 0.35 }),
    );
    // Face order for BoxGeometry: +x, -x, +y, -y, +z, -z. Opposite faces sum to 7.
    this.diceNormals = {
      3: new THREE.Vector3(1, 0, 0),
      4: new THREE.Vector3(-1, 0, 0),
      1: new THREE.Vector3(0, 1, 0),
      6: new THREE.Vector3(0, -1, 0),
      2: new THREE.Vector3(0, 0, 1),
      5: new THREE.Vector3(0, 0, -1),
    };
    this.dice = new THREE.Mesh(new RoundedBoxGeometry(0.8, 0.8, 0.8, 4, 0.12), mats);
    this.dice.castShadow = true;
    this.dice.position.copy(fromSeat(0, 2.6, 0.4, 6.4));
    this.scene.add(this.dice);
  }

  buildTurnRing() {
    this.ring = new THREE.Mesh(
      new THREE.RingGeometry(0.34, 0.46, 40),
      new THREE.MeshBasicMaterial({ color: '#ffffff', transparent: true, opacity: 0.9, side: THREE.DoubleSide }),
    );
    this.ring.rotation.x = -Math.PI / 2;
    this.ring.visible = false;
    this.scene.add(this.ring);
  }

  // --- board ---

  setBoard(board) {
    if (this.boardBuilt) return;
    this.boardBuilt = true;
    const jumps = {};
    for (const j of [...board.ladders, ...board.snakes]) jumps[j.from] = j.to;

    const face = new THREE.Mesh(
      new THREE.PlaneGeometry(10, 10),
      new THREE.MeshStandardMaterial({ map: boardTexture(this.renderer, jumps), roughness: 0.85 }),
    );
    face.rotation.x = -Math.PI / 2;
    face.position.y = BOARD_TOP + 0.002;
    face.receiveShadow = true;
    this.scene.add(face);

    board.ladders.forEach((l) => this.addLadder(l.from, l.to));
    board.snakes.forEach((s, i) => this.addSnake(s.from, s.to, SNAKE_SKINS[i % SNAKE_SKINS.length]));
  }

  addLadder(from, to) {
    const a = tileCenter(from);
    const b = tileCenter(to);
    a.y = b.y = BOARD_TOP + 0.1;
    const dir = b.clone().sub(a).normalize();
    const side = new THREE.Vector3(-dir.z, 0, dir.x).multiplyScalar(0.24);
    const mat = new THREE.MeshStandardMaterial({ color: '#c98b4a', roughness: 0.6 });
    const group = new THREE.Group();
    group.add(this.rod(a.clone().add(side), b.clone().add(side), 0.06, mat));
    group.add(this.rod(a.clone().sub(side), b.clone().sub(side), 0.06, mat));
    const len = a.distanceTo(b);
    for (let d = 0.3; d < len - 0.1; d += 0.42) {
      const p = a.clone().lerp(b, d / len);
      group.add(this.rod(p.clone().add(side), p.clone().sub(side), 0.035, mat));
    }
    group.traverse((m) => (m.castShadow = true));
    this.scene.add(group);
  }

  rod(a, b, r, mat) {
    const len = a.distanceTo(b);
    const m = new THREE.Mesh(new THREE.CylinderGeometry(r, r, len, 10), mat);
    m.position.copy(a).lerp(b, 0.5);
    m.quaternion.setFromUnitVectors(new THREE.Vector3(0, 1, 0), b.clone().sub(a).normalize());
    return m;
  }

  addSnake(head, tail, [base, band]) {
    const a = tileCenter(head);
    const b = tileCenter(tail);
    const len = a.distanceTo(b);
    const dir = b.clone().sub(a).normalize();
    const side = new THREE.Vector3(-dir.z, 0, dir.x);
    const waves = Math.max(1, Math.round(len / 2.6));
    const pts = [];
    const N = 14;
    for (let i = 0; i <= N; i++) {
      const t = i / N;
      const p = a.clone().lerp(b, t);
      // Amplitude fades at both ends so head and tail sit on their tiles.
      const amp = 0.55 * Math.sin(Math.PI * t);
      p.addScaledVector(side, Math.sin(t * Math.PI * 2 * waves) * amp);
      p.y = BOARD_TOP + 0.13;
      pts.push(p);
    }
    const curve = new THREE.CatmullRomCurve3(pts);
    this.snakeCurves.set(head, curve);

    const segs = 120;
    const radial = 12;
    const geo = new THREE.TubeGeometry(curve, segs, 0.15, radial, false);
    // Taper the tube from head to tail.
    const pos = geo.attributes.position;
    const v = new THREE.Vector3();
    for (let i = 0; i <= segs; i++) {
      const t = i / segs;
      const c = curve.getPointAt(t);
      const k = 1 - 0.8 * t ** 1.4;
      for (let j = 0; j <= radial; j++) {
        const idx = i * (radial + 1) + j;
        v.fromBufferAttribute(pos, idx).sub(c).multiplyScalar(k).add(c);
        pos.setXYZ(idx, v.x, v.y, v.z);
      }
    }
    geo.computeVertexNormals();
    const skin = snakeSkin(this.renderer, base, band);
    skin.repeat.set(Math.round(len * 2), 1);
    const body = new THREE.Mesh(geo, new THREE.MeshStandardMaterial({ map: skin, roughness: 0.45 }));
    body.castShadow = true;

    // Head: a flattened egg looking away from the body, with two eyes.
    const headGroup = new THREE.Group();
    const skull = new THREE.Mesh(
      new THREE.SphereGeometry(0.25, 20, 14),
      new THREE.MeshStandardMaterial({ color: base, roughness: 0.4 }),
    );
    skull.scale.set(1, 0.7, 1.35);
    skull.castShadow = true;
    headGroup.add(skull);
    const eyeWhite = new THREE.MeshStandardMaterial({ color: '#fff' });
    const pupil = new THREE.MeshStandardMaterial({ color: '#111' });
    for (const sx of [-1, 1]) {
      const eye = new THREE.Mesh(new THREE.SphereGeometry(0.075, 10, 8), eyeWhite);
      eye.position.set(sx * 0.13, 0.1, 0.16);
      const p = new THREE.Mesh(new THREE.SphereGeometry(0.04, 8, 6), pupil);
      p.position.set(sx * 0.15, 0.12, 0.22);
      headGroup.add(eye, p);
    }
    const tongue = new THREE.Mesh(
      new THREE.BoxGeometry(0.04, 0.01, 0.22),
      new THREE.MeshStandardMaterial({ color: '#d7261e' }),
    );
    tongue.position.set(0, -0.02, 0.42);
    headGroup.add(tongue);
    const start = curve.getPointAt(0);
    const back = curve.getPointAt(0.03);
    headGroup.position.copy(start);
    headGroup.position.y += 0.06;
    headGroup.lookAt(start.clone().multiplyScalar(2).sub(back).setY(headGroup.position.y));
    this.scene.add(body, headGroup);
  }

  // --- seats, players, pawns ---

  setSeat(seat) {
    this.mySeat = seat;
    this.frameSeat();
  }

  // Put the camera at our side of the table, far enough back that the
  // whole board fits whatever the screen shape. Portrait phones look down
  // more steeply so the board fills the width instead of a thin strip.
  frameSeat() {
    const s = this.mySeat < 0 ? 0 : this.mySeat;
    const aspect = this.camera.aspect || 1;
    const vfov = (this.camera.fov * Math.PI) / 180;
    const hfov = 2 * Math.atan(Math.tan(vfov / 2) * aspect);
    const tilt = this.mySeat < 0 ? 0.35 : aspect < 0.9 ? 0.55 : 0.78; // radians from straight down; spectators look from above
    const halfWidth = 6.4;
    const halfDepth = 6.0 * Math.cos(tilt) + 1.2;
    const dist = Math.max(halfWidth / Math.tan(hfov / 2), halfDepth / Math.tan(vfov / 2)) + 3;
    this.camera.position.copy(fromSeat(s, 0, dist * Math.cos(tilt), dist * Math.sin(tilt) + 0.6));
    this.controls.target.copy(fromSeat(s, 0, 0, 0.6));
    this.controls.maxDistance = Math.max(30, dist * 1.6);
    this.controls.update();
  }

  setIdle(idle) {
    this.controls.autoRotate = idle && !reducedMotion;
    this.controls.autoRotateSpeed = 0.5;
  }

  makePawn(seat) {
    const color = SEAT_COLORS[seat];
    const mat = new THREE.MeshPhysicalMaterial({ color, roughness: 0.25, clearcoat: 1, clearcoatRoughness: 0.15 });
    const profile = [
      [0, 0], [0.3, 0], [0.31, 0.05], [0.27, 0.1], [0.16, 0.18], [0.11, 0.36],
      [0.1, 0.46], [0.18, 0.5], [0.18, 0.54], [0.08, 0.57], [0, 0.57],
    ].map(([x, y]) => new THREE.Vector2(x, y));
    const body = new THREE.Mesh(new THREE.LatheGeometry(profile, 28), mat);
    const head = new THREE.Mesh(new THREE.SphereGeometry(0.15, 20, 14), mat);
    head.position.y = 0.68;
    const pawn = new THREE.Group();
    pawn.add(body, head);
    pawn.traverse((m) => (m.castShadow = true));
    pawn.scale.setScalar(0.85);
    this.scene.add(pawn);
    return pawn;
  }

  // Where a pawn of `seat` stands on `tile` (0 = its start spot on the table).
  pawnSpot(seat, tile) {
    if (tile <= 0) return fromSeat(seat, -2.4, 0, 6.3);
    const p = tileCenter(tile);
    const [ox, oz] = PAWN_OFFSET[seat];
    p.x += ox;
    p.z += oz;
    return p;
  }

  // players: [{seat, name, pos, connected, playing}]
  setPlayers(players) {
    const playing = new Set(players.filter((p) => p.playing).map((p) => p.seat));
    for (const [seat, pawn] of this.pawns) {
      if (!playing.has(seat)) {
        this.scene.remove(pawn);
        this.pawns.delete(seat);
        this.pawnTile.delete(seat);
      }
    }
    for (const seat of playing) {
      if (!this.pawns.has(seat)) {
        this.pawns.set(seat, this.makePawn(seat));
        this.placePawn(seat, 0);
      }
    }

    // Avatars only change when someone sits, leaves, renames or drops.
    const key = JSON.stringify([this.mySeat, players.map((p) => [p.seat, p.name, p.connected])]);
    if (key === this.avatarKey) return;
    this.avatarKey = key;
    for (const a of this.avatars) {
      this.scene.remove(a);
      a.traverse((o) => {
        o.geometry?.dispose();
        o.material?.map?.dispose();
        o.material?.dispose();
      });
    }
    this.avatars = [];
    for (const p of players) {
      if (p.seat === this.mySeat) continue;
      this.avatars.push(this.makeAvatar(p));
    }
    this.avatars.forEach((a) => this.scene.add(a));
  }

  makeAvatar(p) {
    const g = new THREE.Group();
    const mat = new THREE.MeshStandardMaterial({
      color: SEAT_COLORS[p.seat],
      roughness: 0.7,
      transparent: !p.connected,
      opacity: p.connected ? 1 : 0.35,
    });
    const body = new THREE.Mesh(new THREE.CapsuleGeometry(1.0, 1.4, 6, 16), mat);
    body.position.set(0, 0.2, 10.5);
    const head = new THREE.Mesh(
      new THREE.SphereGeometry(0.75, 24, 16),
      new THREE.MeshStandardMaterial({ color: '#d9a679', roughness: 0.8, transparent: !p.connected, opacity: p.connected ? 1 : 0.35 }),
    );
    head.position.set(0, 2.45, 10.5);
    body.castShadow = head.castShadow = true;
    const label = new THREE.Sprite(
      new THREE.SpriteMaterial({ map: labelTexture(p.name, SEAT_COLORS[p.seat]), depthTest: false }),
    );
    label.scale.set(4, 1, 1);
    label.position.set(0, 3.9, 10.5);
    g.add(body, head, label);
    g.rotation.y = seatAngle(p.seat);
    return g;
  }

  placePawn(seat, tile) {
    const pawn = this.pawns.get(seat);
    if (!pawn) return;
    pawn.position.copy(this.pawnSpot(seat, tile));
    this.pawnTile.set(seat, tile);
  }

  // Snap all pawns to the server's positions (used on join and reconnect).
  syncPawns(players) {
    for (const p of players) {
      if (p.playing && this.pawnTile.get(p.seat) !== p.pos) this.placePawn(p.seat, p.pos);
    }
  }

  setTurn(seat) {
    this.turnSeat = seat;
    this.ring.visible = seat >= 0 && this.pawns.has(seat);
    if (this.ring.visible) this.ring.material.color.set(SEAT_COLORS[seat]);
  }

  // --- animation ---

  tween(ms, fn) {
    if (reducedMotion) ms = Math.min(ms, 120);
    return new Promise((resolve) => {
      this.tweens.add({ start: performance.now(), ms, fn, resolve });
    });
  }

  async rollDice(seat, value, ms) {
    const from = fromSeat(seat, 0.6, 2.2, 8.0);
    const to = fromSeat(seat, 2.6, 0.4, 6.2 - Math.random() * 0.6);
    const up = new THREE.Vector3(0, 1, 0);
    const align = new THREE.Quaternion().setFromUnitVectors(this.diceNormals[value], up);
    const yaw = new THREE.Quaternion().setFromAxisAngle(up, seatAngle(seat) + (Math.random() - 0.5) * 0.6);
    const final = new THREE.Quaternion().multiplyQuaternions(yaw, align);
    const axis = new THREE.Vector3(Math.random() - 0.5, Math.random() - 0.5, Math.random() - 0.5).normalize();
    const spin = new THREE.Quaternion();
    const tumble = Math.PI * (4 + Math.random() * 2);

    await this.tween(ms * 0.85, (t) => {
      const e = 1 - (1 - t) ** 3;
      this.dice.position.lerpVectors(from, to, e);
      // Two shrinking bounces on the way down.
      this.dice.position.y = to.y + Math.abs(Math.cos(t * Math.PI * 1.5)) * (1 - t) * 1.8;
      spin.setFromAxisAngle(axis, (1 - e) * tumble);
      this.dice.quaternion.multiplyQuaternions(final, spin);
    });
    await this.tween(ms * 0.15, () => {});
  }

  hop(seat, fromTile, toTile, ms) {
    const pawn = this.pawns.get(seat);
    const a = this.pawnSpot(seat, fromTile);
    const b = this.pawnSpot(seat, toTile);
    return this.tween(ms, (t) => {
      pawn.position.lerpVectors(a, b, t);
      pawn.position.y += Math.sin(Math.PI * t) * 0.5;
    }).then(() => this.pawnTile.set(seat, toTile));
  }

  // Play one server move. Resolves when the pawn has come to rest.
  async animateMove(m, timing) {
    if (!this.pawns.has(m.seat)) return;
    this.ring.visible = false;
    await this.rollDice(m.seat, m.dice, timing.diceMs);

    const pawn = this.pawns.get(m.seat);
    if (m.stuck) {
      const home = pawn.position.clone();
      await this.tween(400, (t) => {
        pawn.position.x = home.x + Math.sin(t * Math.PI * 6) * 0.08 * (1 - t);
      });
      pawn.position.copy(home);
      return;
    }

    // Walk tile by tile, turning back at 100 if the roll bounced.
    let tile = m.from;
    let dir = 1;
    for (let i = 0; i < m.dice; i++) {
      if (tile === 100) dir = -1;
      const next = tile + dir;
      await this.hop(m.seat, tile, next, timing.stepMs);
      tile = next;
    }

    if (m.kind === 'ladder') {
      const a = this.pawnSpot(m.seat, m.landed);
      const b = this.pawnSpot(m.seat, m.to);
      await this.tween(timing.jumpMs, (t) => {
        pawn.position.lerpVectors(a, b, ease(t));
        pawn.position.y += 0.25 + Math.sin(t * Math.PI * 14) * 0.04;
      });
    } else if (m.kind === 'snake') {
      const curve = this.snakeCurves.get(m.landed);
      const end = this.pawnSpot(m.seat, m.to);
      const [ox, oz] = PAWN_OFFSET[m.seat];
      await this.tween(timing.jumpMs, (t) => {
        const p = curve.getPointAt(ease(t));
        pawn.position.set(p.x + ox * t, p.y + 0.12, p.z + oz * t);
      });
      pawn.position.copy(end);
    }
    this.placePawn(m.seat, m.to);
  }

  // --- loop ---

  resize() {
    const w = this.canvas.clientWidth || innerWidth;
    const h = this.canvas.clientHeight || innerHeight;
    this.renderer.setSize(w, h, false);
    this.camera.aspect = w / h;
    this.camera.fov = w < h ? 50 : 40;
    this.camera.updateProjectionMatrix();
    this.frameSeat();
  }

  frame(now) {
    for (const tw of this.tweens) {
      const t = Math.min(1, (now - tw.start) / tw.ms);
      tw.fn(t);
      if (t >= 1) {
        this.tweens.delete(tw);
        tw.resolve();
      }
    }
    if (this.ring.visible) {
      const pawn = this.pawns.get(this.turnSeat);
      if (pawn) {
        this.ring.position.set(pawn.position.x, pawn.position.y + 0.02, pawn.position.z);
        const s = 1 + Math.sin(now / 220) * 0.12;
        this.ring.scale.set(s, s, s);
      }
    }
    this.controls.update();
    this.renderer.render(this.scene, this.camera);
  }
}
