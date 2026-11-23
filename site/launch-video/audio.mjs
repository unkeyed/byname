// Synthesizes the launch video's soundtrack: an original piece in D dorian at
// 96 BPM for felt piano, plucked strings, upright bass and brushes, with a
// pen stroke under every hand-drawn mark, a stamp, chimes and a button click.
// The scene times and every sound cue come from launch.html itself
// (window.T and window.CUES), so the sound stays on the marks when the
// timeline changes. Everything is generated here: nothing to license.
//
//   node audio.mjs [out.wav]
import { chromium } from "playwright";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const browser = await chromium.launch({ channel: "chrome" });
const page = await browser.newPage({ viewport: { width: 1920, height: 1080 }, colorScheme: "light" });
await page.goto("file://" + path.join(here, "launch.html") + "#render", { waitUntil: "networkidle" });
const { T, DURATION, CUES } = await page.evaluate(async () => { await window.ready; return { T: window.T, DURATION: window.DURATION, CUES: window.CUES }; });
await browser.close();

const SR = 48000;
const N = Math.ceil(SR * DURATION);
const L = new Float32Array(N), R = new Float32Array(N);
const RL = new Float32Array(N), RR = new Float32Array(N);

const TAU = Math.PI * 2;
const midi = (n) => 440 * Math.pow(2, (n - 69) / 12);
const clamp = (x, a, b) => Math.min(b, Math.max(a, x));
let seed = 20261123;
const rand = () => ((seed = (seed * 1664525 + 1013904223) >>> 0) / 4294967296) * 2 - 1;
const panGains = (p) => [Math.cos((p + 1) * Math.PI / 4), Math.sin((p + 1) * Math.PI / 4)];
function add(i, l, r, send = 0) {
  if (i < 0 || i >= N) return;
  L[i] += l; R[i] += r; RL[i] += l * send; RR[i] += r * send;
}
// A Chamberlin state-variable band-pass, one sample at a time.
const bandpass = (q = 0.7) => { let low = 0, band = 0; return (x, fc) => { const f = 2 * Math.sin(Math.PI * Math.min(fc, SR / 6) / SR); low += f * band; const high = x - low - q * band; band += f * high; return band; }; };

// Arrangement, from the video's own timeline.
const BPM = 96, BEAT = 60 / BPM, BAR = BEAT * 4;
const GRID = T.question[0] + 0.35;                       // the headline's first word: beat one
const snap = (t) => GRID + Math.round((t - GRID) / BEAT) * BEAT;
const GROOVE_IN = snap(T.answer[0]);                     // brushes, plucks and melody
const BREAK_AT = snap(T.dropout[0] + 2.3);               // check #7 arrives: the band drops out
const BACK_AT = snap(T.gap[0]);                          // close the gap: everyone back
const END_AT = T.end[0] + 0.3;                           // the end card's logo
const SWELL_AT = END_AT - BAR;
const FADE_FROM = DURATION - 3.2;
const beatAt = (t) => (t - GRID) / BEAT;
const playing = (t) => t >= GRID && t < SWELL_AT;
const grooving = (t) => t >= GROOVE_IN && t < SWELL_AT && !(t >= BREAK_AT && t < BACK_AT);
const master = (t) => clamp(t / 1.2, 0, 1) * (t > FADE_FROM ? Math.pow(clamp(1 - (t - FADE_FROM) / (DURATION - FADE_FROM), 0, 1), 1.5) : 1);

// D dorian, two bars each: Dm9, G9, Fmaj9, Cadd9.
const chords = [[50, 53, 57, 60, 64], [43, 47, 50, 53, 57], [41, 45, 48, 52, 55], [48, 52, 55, 62, 64]];
const chordAt = (t) => chords[((Math.floor(beatAt(t) / 8) % 4) + 4) % 4];

// Felt piano: slightly inharmonic partials, two detuned strings, a soft hammer.
function felt(t0, n, len, gain, pan, send = 0.5) {
  const f = midi(n), [gl, gr] = panGains(pan);
  const s0 = Math.floor(t0 * SR), total = Math.floor((len + 1.2) * SR);
  const parts = [];
  for (let k = 1; k <= 7; k++) parts.push({ f: f * k * Math.sqrt(1 + 0.0003 * k * k), a: Math.pow(k, -1.7) * (k === 1 ? 1 : 0.8), d: 2.4 / (1 + 0.55 * k) * Math.pow(261 / f, 0.25) });
  let lp = 0;
  for (let k = 0; k < total; k++) {
    const t = k / SR;
    const env = Math.min(1, t / 0.012) * (t > len ? Math.exp(-(t - len) / 0.16) : 1);
    let v = 0;
    for (const p of parts) v += p.a * Math.exp(-t / p.d) * (Math.sin(TAU * p.f * t) + (p.f < f * 3.5 ? 0.6 * Math.sin(TAU * p.f * 1.0011 * t) : 0));
    lp += 0.25 * (rand() - lp);
    v += lp * 0.5 * Math.exp(-t / 0.008);
    v *= env * gain * 0.6;
    add(s0 + k, v * gl, v * gr, send);
  }
}

// Plucked string: Karplus-Strong with a fractional delay, so it stays in tune.
function pluck(t0, n, gain, pan, len = 1.6, decay = 0.9965, send = 0.4) {
  const f = midi(n), [gl, gr] = panGains(pan);
  const D = SR / f - 0.5, size = 4096, buf = new Float32Array(size);
  let w = Math.ceil(D) + 2, sm = 0;
  for (let i = 0; i < w; i++) { sm += 0.5 * (rand() - sm); buf[i] = sm; }
  let prev = 0;
  const s0 = Math.floor(t0 * SR), total = Math.floor(len * SR);
  for (let k = 0; k < total; k++) {
    const pos = w - D, i0 = Math.floor(pos), fr = pos - i0;
    const y = buf[i0 % size] * (1 - fr) + buf[(i0 + 1) % size] * fr;
    buf[w % size] = decay * (0.5 * y + 0.5 * prev);
    prev = y; w++;
    const v = y * gain * Math.min(1, (total - k) / (0.05 * SR));
    add(s0 + k, v * gl, v * gr, send);
  }
}

// Upright bass: a round fundamental with a little second harmonic and a thumb attack.
function bass(t0, n, len, gain) {
  const f = midi(n - 12);
  const s0 = Math.floor(t0 * SR), total = Math.floor((len + 0.08) * SR);
  for (let k = 0; k < total; k++) {
    const t = k / SR;
    const env = Math.min(1, t / 0.01) * Math.exp(-t / 0.9) * (t > len ? Math.exp(-(t - len) / 0.03) : 1);
    const v = (Math.sin(TAU * f * t) + 0.3 * Math.sin(TAU * 2 * f * t) * Math.exp(-t / 0.15) + 0.12 * Math.sin(TAU * 3 * f * t) * Math.exp(-t / 0.05)) * env * gain;
    add(s0 + k, v, v, 0.05);
  }
}

// Warm pad: triangle-ish voices under a low lowpass, slow in and out.
function pad(t0, len, notes, gain, cutoff = 900) {
  for (const n of notes) for (const det of [-0.06, 0.05]) {
    const f = midi(n + det), [gl, gr] = panGains(det * 8);
    const a = 1 - Math.exp(-TAU * cutoff / SR);
    let ph = (n * 0.31 + det + 1) % 1, y1 = 0, y2 = 0;
    const s0 = Math.floor(t0 * SR), total = Math.floor((len + 2) * SR);
    for (let k = 0; k < total; k++) {
      const t = k / SR;
      const env = Math.min(1, t / 1.8) * (t > len ? Math.exp(-(t - len) / 0.8) : 1);
      ph += f / SR; ph -= Math.floor(ph);
      const x = 1 - 4 * Math.abs(ph - 0.5);
      y1 += a * (x - y1); y2 += a * (y1 - y2);
      const v = y2 * env * gain / notes.length;
      add(s0 + k, v * gl, v * gr, 0.6);
    }
  }
}

function kick(t0, gain) {
  const s0 = Math.floor(t0 * SR);
  let ph = 0;
  for (let k = 0; k < 0.4 * SR; k++) {
    const t = k / SR;
    ph += (46 + 54 * Math.exp(-t / 0.04)) / SR;
    const v = Math.sin(TAU * ph) * Math.min(1, t / 0.004) * Math.exp(-t / 0.2) * gain;
    add(s0 + k, v, v, 0.03);
  }
}

// Brush: a short sweep of band-passed noise, softer than a snare.
function brush(t0, gain, pan, len = 0.28) {
  const [gl, gr] = panGains(pan), bp = bandpass(0.9);
  const s0 = Math.floor(t0 * SR);
  for (let k = 0; k < len * SR; k++) {
    const t = k / SR;
    const env = Math.min(1, t / 0.014) * (0.65 * Math.exp(-t / 0.06) + 0.35 * Math.exp(-t / 0.18));
    const v = bp(rand(), 3400 - 1200 * (t / len)) * env * gain;
    add(s0 + k, v * gl, v * gr, 0.3);
  }
}

function shaker(t0, gain, pan) {
  const [gl, gr] = panGains(pan);
  const s0 = Math.floor(t0 * SR);
  let p1 = 0, p2 = 0;
  for (let k = 0; k < 0.09 * SR; k++) {
    const t = k / SR;
    const n = rand(), hp = n - 2 * p1 + p2; p2 = p1; p1 = n;
    const v = hp * Math.min(1, t / 0.006) * Math.exp(-t / 0.03) * gain;
    add(s0 + k, v * gl, v * gr, 0.15);
  }
}

// A felt-tip stroke on paper: scratchy band-passed noise that follows the mark.
function pen(t0, len, gain, pan) {
  const [gl, gr] = panGains(pan), bp = bandpass(1.1);
  const s0 = Math.floor(t0 * SR), total = Math.floor(len * SR);
  for (let k = 0; k < total; k++) {
    const x = k / total, t = k / SR;
    const env = Math.pow(Math.sin(Math.PI * x), 0.6) * (0.72 + 0.28 * Math.sin(TAU * 21 * t + 1.3));
    const v = bp(rand(), 2900 + 1100 * Math.sin(TAU * 3.1 * t)) * env * gain;
    add(s0 + k, v * gl, v * gr, 0.12);
  }
  for (let k = 0; k < 0.012 * SR; k++) {
    const t = k / SR, v = Math.sin(TAU * 2300 * t) * Math.exp(-t / 0.003) * gain * 0.5;
    add(s0 + k, v * gl, v * gr, 0.1);
  }
}

// The rubber stamp: a low thud and a paper slap.
function stamp(t0, gain, pan) {
  const [gl, gr] = panGains(pan), bp = bandpass(0.8);
  const s0 = Math.floor(t0 * SR);
  let ph = 0, lp = 0;
  for (let k = 0; k < 0.35 * SR; k++) {
    const t = k / SR;
    ph += (52 + 80 * Math.exp(-t / 0.025)) / SR;
    lp += 0.08 * (rand() - lp);
    const v = (Math.sin(TAU * ph) * Math.exp(-t / 0.09) + lp * 3 * Math.exp(-t / 0.02) + bp(rand(), 1300) * 0.8 * Math.exp(-t / 0.035)) * gain;
    add(s0 + k, v * gl, v * gr, 0.35);
  }
}

// A glass chime: bell partials, long and quiet.
function chime(t0, n, gain, pan, decay = 1.6) {
  const f = midi(n), [gl, gr] = panGains(pan);
  const s0 = Math.floor(t0 * SR);
  const parts = [[1, 1, 1], [2.76, 0.35, 0.45], [5.4, 0.16, 0.22], [8.93, 0.06, 0.12]];
  for (let k = 0; k < decay * 4 * SR; k++) {
    const t = k / SR;
    let v = 0;
    for (const [m, a, d] of parts) v += a * Math.sin(TAU * f * m * t) * Math.exp(-t / (decay * d));
    v *= Math.min(1, t / 0.003) * gain;
    add(s0 + k, v * gl, v * gr, 0.65);
  }
}

function click(t0, gain, pan) {
  const [gl, gr] = panGains(pan), bp = bandpass(0.6);
  const s0 = Math.floor(t0 * SR);
  for (let k = 0; k < 0.04 * SR; k++) {
    const t = k / SR;
    const v = (bp(rand(), 1900) * 1.4 + Math.sin(TAU * 420 * t)) * Math.exp(-t / 0.006) * gain;
    add(s0 + k, v * gl, v * gr, 0.2);
  }
}

// --- The band ---

// Pads under everything, one chord every two bars, from the very start.
for (let b = Math.floor(beatAt(0) / 8); GRID + b * 8 * BEAT < SWELL_AT; b++) {
  const t = Math.max(0, GRID + b * 8 * BEAT);
  const len = GRID + (b + 1) * 8 * BEAT - t;
  const quiet = t >= BREAK_AT && t < BACK_AT;
  pad(t, len, chordAt(t + 0.01), quiet ? 0.1 : 0.13, t < GROOVE_IN || quiet ? 700 : 1000);
}

// Intro: the felt piano spells out Dm9 slowly before the headline lands.
[[0.5, 50], [1.1, 57], [1.7, 60], [2.3, 64], [2.9, 69], [3.5, 72]].forEach(([t, n], i) => felt(t, n, 1.6, 0.11 - i * 0.006, (i % 2 ? 0.25 : -0.25)));

// Kick, bass and shaker from the headline; brushes and plucks from the answer.
for (let i = 0; GRID + i * BEAT < SWELL_AT - 0.01; i++) {
  const t = GRID + i * BEAT, inBar = i % 4, c = chordAt(t);
  const quiet = t >= BREAK_AT && t < BACK_AT;
  if (!quiet) {
    if (inBar === 0) kick(t, 0.3);
    if (inBar === 2) kick(t + BEAT / 2, 0.2);
    if (inBar === 0) bass(t, c[0], BEAT * 1.4, 0.17);
    if (inBar === 2) bass(t + BEAT / 2, c[0] + 7, BEAT * 0.9, 0.14);
    if (inBar === 3 && i % 8 === 7) bass(t + BEAT / 2, c[0] + 10, BEAT * 0.45, 0.12);
  }
  // Shakers swing: the offbeat lands 58% of the way through the beat.
  shaker(t, quiet ? 0.035 : 0.05, 0.35);
  shaker(t + BEAT * 0.58, quiet ? 0.05 : 0.075, 0.35);
  if (grooving(t)) {
    if (inBar === 1 || inBar === 3) brush(t, 0.2, -0.15);
    if (inBar === 3) brush(t + BEAT * 0.58, 0.06, 0.2, 0.12);
    const arp = [1, 3, 2, 4, 3, 1, 4, 2];
    pluck(t, c[arp[(i * 2) % 8]] + 12, 0.075, -0.45);
    pluck(t + BEAT * 0.58, c[arp[(i * 2 + 1) % 8]] + 12, 0.06, -0.45);
  }
}

// The melody: an eight-bar felt piano line over the four chords.
const melody = [
  [0, 69, 1.5], [1.5, 72, 0.5], [2, 74, 2], [4.5, 76, 0.5], [5, 74, 1], [6, 72, 1], [7, 69, 1],
  [8, 71, 1.5], [9.5, 74, 0.5], [10, 77, 2], [12.5, 76, 0.5], [13, 74, 1], [14, 71, 2],
  [16, 72, 1.5], [17.5, 69, 0.5], [18, 67, 1], [19, 69, 3], [22.5, 72, 0.5], [23, 74, 1],
  [24, 76, 1.5], [25.5, 74, 0.5], [26, 72, 1], [27, 67, 1], [28, 69, 4],
];
for (let cycle = Math.floor(beatAt(GROOVE_IN) / 32); GRID + cycle * 32 * BEAT < SWELL_AT; cycle++) {
  for (const [b, n, len] of melody) {
    const t = GRID + (cycle * 32 + b) * BEAT;
    if (t < GROOVE_IN || !grooving(t) || t + len * BEAT > SWELL_AT + BEAT) continue;
    felt(t, n, len * BEAT * 0.95, 0.085, 0.15, 0.55);
  }
}

// Into the end card: a soft swell, then one held Dm9 with a chime on top.
{
  const s0 = Math.floor(SWELL_AT * SR), total = Math.floor((END_AT - SWELL_AT) * SR), bp = bandpass(0.8);
  for (let k = 0; k < total; k++) {
    const x = k / total;
    const v = bp(rand(), 500 * Math.pow(9, x)) * Math.pow(x, 2) * 0.1;
    add(s0 + k, v * 0.9, v, 0.6);
  }
  pad(SWELL_AT, END_AT - SWELL_AT - 0.3, chords[3], 0.13, 1000);
  [62, 65, 69, 72].forEach((n, i) => pluck(SWELL_AT + BAR * (0.5 + i * 0.125), n + 12, 0.05, i % 2 ? 0.4 : -0.4));
}
kick(END_AT, 0.4);
bass(END_AT, 50, 3.5, 0.2);
pad(END_AT, DURATION - END_AT - 1.5, [38, 50, 53, 57, 60, 64, 69], 0.2, 1100);
[50, 57, 60, 64, 69].forEach((n, i) => felt(END_AT + i * 0.06, n, 4, 0.08, (i - 2) * 0.15));
chime(END_AT + 0.6, 86, 0.06, 0.2, 2.4);

// --- Sound on the marks, from launch.html's cues ---
const penta = [62, 65, 67, 69, 72, 74, 77, 79, 81, 84, 86];
let cells = 0, checks = 0, chimes = 0;
const counts = {};
for (const c of CUES) {
  counts[c.kind] = (counts[c.kind] ?? 0) + 1;
  if (["circle", "underline", "caret", "dash"].includes(c.kind)) pen(c.t, Math.max(0.18, c.dur * 0.85), c.kind === "circle" ? 0.32 : 0.26, c.pan);
  else if (c.kind === "cell") {
    const k = cells++;
    pen(c.t, 0.09, 0.14, c.pan);
    pluck(c.t, penta[Math.floor(k / 6) + (k % 6) * (c.mark === "circle" ? 1 : 0)] + (c.mark === "circle" ? 0 : -12), c.mark === "circle" ? 0.07 : 0.045, c.pan, 1.0);
  } else if (c.kind === "check") {
    pen(c.t, 0.16, 0.22, c.pan);
    pluck(c.t + 0.05, [74, 76, 77, 79, 81][checks++ % 5], 0.11, c.pan);
  } else if (c.kind === "stamp") stamp(c.t, 0.42, c.pan);
  else if (c.kind === "chime") {
    const [a, b] = [[81, 86], [79, 83], [81, 88]][chimes++ % 3];
    chime(c.t, a, 0.11, c.pan); chime(c.t + 0.16, b, 0.08, c.pan);
  } else if (c.kind === "press") { click(c.t, 0.4, c.pan); click(c.t + 0.16, 0.24, c.pan); }
}

// --- Reverb on the send, then the master bus ---
function freeverb(inp, spread) {
  const combs = [1557, 1617, 1491, 1422, 1277, 1356, 1188, 1116].map((d) => ({ buf: new Float32Array(d + spread), i: 0, store: 0 }));
  const aps = [556, 441, 341, 225].map((d) => ({ buf: new Float32Array(d + spread), i: 0 }));
  const out = new Float32Array(inp.length);
  const fb = 0.84, damp = 0.4;
  for (let n = 0; n < inp.length; n++) {
    const x = inp[n] * 0.015;
    let y = 0;
    for (const c of combs) {
      const o = c.buf[c.i];
      c.store = o * (1 - damp) + c.store * damp;
      c.buf[c.i] = x + c.store * fb;
      c.i = (c.i + 1) % c.buf.length;
      y += o;
    }
    for (const a of aps) {
      const o = a.buf[a.i];
      a.buf[a.i] = y + o * 0.5;
      a.i = (a.i + 1) % a.buf.length;
      y = o - y;
    }
    out[n] = y;
  }
  return out;
}
const wetL = freeverb(RL, 0), wetR = freeverb(RR, 23);

const pcm = Buffer.alloc(N * 4);
let peak = 0, clipped = 0;
for (let i = 0; i < N; i++) {
  const g = master(i / SR);
  const l = Math.tanh((L[i] + wetL[i] * 2.2) * g * 1.4);
  const r = Math.tanh((R[i] + wetR[i] * 2.2) * g * 1.4);
  peak = Math.max(peak, Math.abs(l), Math.abs(r));
  if (Math.abs(l) > 0.99 || Math.abs(r) > 0.99) clipped++;
  pcm.writeInt16LE(Math.round(clamp(l, -1, 1) * 32000), i * 4);
  pcm.writeInt16LE(Math.round(clamp(r, -1, 1) * 32000), i * 4 + 2);
}

const out = process.argv[2] ?? path.join(here, "byname-launch.wav");
const header = Buffer.alloc(44);
header.write("RIFF", 0); header.writeUInt32LE(36 + pcm.length, 4); header.write("WAVE", 8);
header.write("fmt ", 12); header.writeUInt32LE(16, 16); header.writeUInt16LE(1, 20); header.writeUInt16LE(2, 22);
header.writeUInt32LE(SR, 24); header.writeUInt32LE(SR * 4, 28); header.writeUInt16LE(4, 32); header.writeUInt16LE(16, 34);
header.write("data", 36); header.writeUInt32LE(pcm.length, 40);
fs.writeFileSync(out, Buffer.concat([header, pcm]));
console.log(`cues: ${Object.entries(counts).map(([k, v]) => `${v} ${k}`).join(", ")}`);
console.log(`wrote ${out}: ${DURATION}s, peak ${peak.toFixed(2)}, ${clipped} samples above 0.99`);
