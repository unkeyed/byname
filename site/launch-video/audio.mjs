// Synthesizes the launch video's soundtrack, one of three original songs picked by
// letter, so there is nothing to license. AT holds the moments of launch.html's
// timeline the music lands on; keep it in step with that page's T.
//   node audio.mjs [a|b|c] [out.wav]
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const [pick = "a", outPath = path.join(here, "byname-launch.wav")] = process.argv.slice(2);

const DURATION = 66;
const AT = { headline: 4.15, answer: 9.6, stamp: 14.6, check7: 27.3, alert: 29.85, gap: 32.4, logo: 60.5 };
const SR = 48000, N = Math.ceil(SR * DURATION), TAU = Math.PI * 2;
const L = new Float32Array(N), R = new Float32Array(N);
const VL = new Float32Array(N), VR = new Float32Array(N);
const EL = new Float32Array(N), ER = new Float32Array(N);
const DUCK = new Float32Array(N).fill(1);

let seed = 20261123;
const rand = () => ((seed = (seed * 1664525 + 1013904223) >>> 0) / 4294967296) * 2 - 1;
const midi = (n) => 440 * Math.pow(2, (n - 69) / 12);
const clamp = (x, a, b) => Math.min(b, Math.max(a, x));
const ramp = (t, t0, t1, a, b) => a + (b - a) * clamp((t - t0) / (t1 - t0), 0, 1);
const glide = (t, t0, t1, a, b) => a * Math.pow(b / a, clamp((t - t0) / (t1 - t0), 0, 1));
const fn = (x) => (typeof x === "function" ? x : () => x);

function out({ pan = 0, verb = 0, echo = 0, duck = 0 } = {}) {
  const gl = Math.cos(((pan + 1) * Math.PI) / 4), gr = Math.sin(((pan + 1) * Math.PI) / 4);
  return (i, l, r = l) => {
    if (i < 0 || i >= N) return;
    const g = duck ? 1 - duck * (1 - DUCK[i]) : 1;
    const a = l * gl * g, b = r * gr * g;
    L[i] += a; R[i] += b;
    VL[i] += a * verb; VR[i] += b * verb;
    EL[i] += a * echo; ER[i] += b * echo;
  };
}

function sidechain(times, depth, release) {
  for (const t0 of times) {
    const s0 = Math.round(t0 * SR);
    for (let k = 0; k < release * 7 * SR; k++) {
      const i = s0 + k, t = k / SR;
      if (i >= 0 && i < N) DUCK[i] = Math.min(DUCK[i], 1 - depth * Math.min(1, t / 0.004) * Math.exp(-t / release));
    }
  }
}

function svf(mode = "lp") {
  let s1 = 0, s2 = 0;
  return (x, fc, res = 0) => {
    const g = Math.tan((Math.PI * clamp(fc, 20, SR * 0.45)) / SR), k = 2 - 1.96 * res;
    const hp = (x - (g + k) * s1 - s2) / (1 + g * (g + k));
    const bp = g * hp + s1; s1 = g * hp + bp;
    const lp = g * bp + s2; s2 = g * bp + lp;
    return mode === "lp" ? lp : mode === "hp" ? hp : bp;
  };
}

const blep = (t, dt) => (t < dt ? ((t /= dt), t + t - t * t - 1) : t > 1 - dt ? ((t = (t - 1) / dt), t * t + t + t + 1) : 0);
const WAVES = {
  saw: (ph, dt) => 2 * ph - 1 - blep(ph, dt),
  square: (ph, dt) => (ph < 0.5 ? 1 : -1) + blep(ph, dt) - blep((ph + 0.5) % 1, dt),
  tri: (ph) => 1 - 4 * Math.abs(ph - 0.5),
  sine: (ph) => Math.sin(TAU * ph),
};

function adsr(t, len, a, d, s, r) {
  const u = Math.min(t, len);
  const h = u < a ? u / a : s + (1 - s) * Math.exp(-(u - a) / d);
  return t < len ? h : h * Math.exp(-(t - len) / r);
}

function synth(t0, notes, len, o) {
  const { wave = "saw", voices = 1, detune = 0, width = 0.6, cut = 2000, env = 0, decay = 0.2, res = 0.1, a = 0.005, d = 0.3, s = 1, r = 0.2, gain = 0.1 } = o;
  const w = out(o), osc = WAVES[wave], fl = svf(), fr = svf(), cutAt = fn(cut);
  const vs = [];
  for (const n of [notes].flat()) for (let v = 0; v < voices; v++) {
    const x = voices > 1 ? (2 * v) / (voices - 1) - 1 : 0;
    vs.push({ dt: (midi(n) * Math.pow(2, (x * detune) / 1200)) / SR, ph: (rand() + 1) / 2, side: x * width });
  }
  const norm = gain / Math.sqrt(vs.length), s0 = Math.round(t0 * SR), total = Math.round((len + 7 * r) * SR);
  for (let k = 0; k < total && s0 + k < N; k++) {
    const t = k / SR;
    let l = 0, rr = 0;
    for (const v of vs) {
      v.ph += v.dt; if (v.ph >= 1) v.ph -= 1;
      const y = osc(v.ph, v.dt);
      l += y * (1 - v.side); rr += y * (1 + v.side);
    }
    const c = cutAt(t0 + t) + env * Math.exp(-t / decay), e = adsr(t, len, a, d, s, r) * norm;
    w(s0 + k, fl(l, c, res) * e, fr(rr, c, res) * e);
  }
}

function epiano(t0, notes, len, o) {
  const { gain = 0.1, bright = 1, wow = 0, cut = 12000 } = o;
  const w = out(o), cutAt = fn(cut);
  for (const n of notes) {
    const f = midi(n), fl = svf(), tau = 1.6 * Math.pow(261 / f, 0.35);
    const s0 = Math.round(t0 * SR), total = Math.round((len + 0.5) * SR);
    let ph = 0;
    for (let k = 0; k < total; k++) {
      const t = k / SR, at = t0 + t;
      ph += (f * (1 + wow * 0.004 * Math.sin(TAU * 0.5 * at))) / SR;
      const idx = bright * (1.3 * Math.exp(-t / 0.22) + 0.2);
      let v = Math.sin(TAU * ph + idx * Math.sin(TAU * ph)) + 0.15 * bright * Math.sin(TAU * ph * 14.2) * Math.exp(-t / 0.018);
      v *= Math.min(1, t / 0.002) * Math.exp(-t / tau) * (t > len ? Math.exp(-(t - len) / 0.07) : 1) * gain;
      v = fl(v, cutAt(at), 0.05);
      const trem = 0.3 * Math.sin(TAU * 3.2 * at);
      w(s0 + k, v * (1 + trem), v * (1 - trem));
    }
  }
}

function bell(t0, n, o) {
  const { gain = 0.08, decay = 2.5 } = o;
  const w = out(o), f = midi(n), s0 = Math.round(t0 * SR);
  for (let k = 0; k < decay * 5 * SR; k++) {
    const t = k / SR, idx = 2.2 * Math.exp(-t / 0.5);
    const v = (Math.sin(TAU * f * t + idx * Math.sin(TAU * f * 3.5 * t)) * Math.exp(-t / decay) + 0.25 * Math.sin(TAU * f * 2 * t) * Math.exp(-t / (decay * 0.4))) * Math.min(1, t / 0.002) * gain;
    w(s0 + k, v);
  }
}

function sub(t0, n, len, o) {
  const { gain = 0.2, a = 0.01, r = 0.08, drive = 1.2 } = o;
  const w = out(o), f = midi(n), s0 = Math.round(t0 * SR);
  let ph = 0;
  for (let k = 0; k < (len + 7 * r) * SR; k++) {
    ph += f / SR;
    w(s0 + k, (Math.tanh(drive * Math.sin(TAU * ph)) / Math.tanh(drive)) * adsr(k / SR, len, a, 1, 1, r) * gain);
  }
}

function kick(t0, o) {
  const { gain = 0.5, hi = 160, lo = 48, sweep = 0.03, decay = 0.3, click = 0.2 } = o;
  const w = out(o), s0 = Math.round(t0 * SR);
  let ph = 0, n = 0;
  for (let k = 0; k < decay * 6 * SR; k++) {
    const t = k / SR;
    ph += (lo + (hi - lo) * Math.exp(-t / sweep)) / SR;
    n += 0.5 * (rand() - n);
    const v = Math.tanh(1.6 * Math.sin(TAU * ph) * Math.min(1, t / 0.0015) * Math.exp(-t / decay)) / Math.tanh(1.6) + click * n * Math.exp(-t / 0.003);
    w(s0 + k, v * gain);
  }
}

function boom(t0, o) {
  const { gain = 0.4, hi = 110, lo = 34, decay = 1.4 } = o;
  const w = out(o), s0 = Math.round(t0 * SR), lp = svf();
  let ph = 0;
  for (let k = 0; k < decay * 5 * SR; k++) {
    const t = k / SR;
    ph += (lo + (hi - lo) * Math.exp(-t / 0.12)) / SR;
    const v = Math.tanh(1.4 * Math.sin(TAU * ph) * Math.exp(-t / decay)) + lp(rand(), 400) * 2 * Math.exp(-t / 0.06);
    w(s0 + k, v * Math.min(1, t / 0.002) * gain);
  }
}

function clap(t0, o) {
  const { gain = 0.2, tail = 0.12 } = o;
  const w = out(o), bp = svf("bp"), s0 = Math.round(t0 * SR);
  for (let k = 0; k < (tail * 7 + 0.04) * SR; k++) {
    const t = k / SR;
    let e = 0;
    for (const d of [0, 0.01, 0.021]) if (t >= d) e += Math.exp(-(t - d) / 0.005);
    if (t >= 0.032) e += 0.8 * Math.exp(-(t - 0.032) / tail);
    w(s0 + k, bp(rand(), 1300, 0.35) * e * gain * 2);
  }
}

function snare(t0, o) {
  const { gain = 0.2, decay = 0.15, tone = 190 } = o;
  const w = out(o), hp = svf("hp"), s0 = Math.round(t0 * SR);
  for (let k = 0; k < decay * 7 * SR; k++) {
    const t = k / SR;
    const v = 0.5 * Math.sin(TAU * tone * t) * Math.exp(-t / 0.05) + 0.25 * Math.sin(TAU * tone * 1.8 * t) * Math.exp(-t / 0.03) + hp(rand(), 1500, 0.1) * Math.exp(-t / decay);
    w(s0 + k, v * Math.min(1, t / 0.001) * gain);
  }
}

function rim(t0, o) {
  const { gain = 0.08 } = o;
  const w = out(o), bp = svf("bp"), s0 = Math.round(t0 * SR);
  for (let k = 0; k < 0.08 * SR; k++) {
    const t = k / SR;
    w(s0 + k, (bp(rand(), 2500, 0.5) + 0.6 * Math.sin(TAU * 820 * t)) * Math.exp(-t / 0.012) * gain);
  }
}

function tom(t0, n, o) {
  const { gain = 0.25, decay = 0.35 } = o;
  const w = out(o), f = midi(n), s0 = Math.round(t0 * SR), lp = svf();
  let ph = 0;
  for (let k = 0; k < decay * 6 * SR; k++) {
    const t = k / SR;
    ph += (f * (1 + 0.6 * Math.exp(-t / 0.04))) / SR;
    w(s0 + k, (Math.sin(TAU * ph) * Math.exp(-t / decay) + lp(rand(), 2000) * 0.5 * Math.exp(-t / 0.02)) * Math.min(1, t / 0.001) * gain);
  }
}

const METAL = [205.3, 304.4, 369.6, 522.7, 540, 800].map((f) => f * 2.2);
function metal(t) {
  let m = 0;
  for (const f of METAL) m += Math.sin(TAU * f * t) > 0 ? 1 : -1;
  return m;
}

function hat(t0, o) {
  const { gain = 0.05, decay = 0.04 } = o;
  const w = out(o), bp = svf("bp"), hp = svf("hp"), s0 = Math.round(t0 * SR);
  for (let k = 0; k < decay * 7 * SR; k++) {
    const t = k / SR;
    w(s0 + k, hp(bp(0.6 * rand() + 0.08 * metal(t), 10000, 0.2), 7000, 0.1) * Math.min(1, t / 0.0005) * Math.exp(-t / decay) * gain * 3);
  }
}

function crash(t0, o) {
  const { gain = 0.12, decay = 1.8 } = o;
  const w = out(o), hl = svf("hp"), hr = svf("hp"), s0 = Math.round(t0 * SR);
  for (let k = 0; k < decay * 5 * SR; k++) {
    const t = k / SR, e = Math.min(1, t / 0.002) * Math.exp(-t / decay) * gain, m = 0.05 * metal(t * 1.3);
    w(s0 + k, hl(rand() + m, 5000, 0.1) * e, hr(rand() + m, 5000, 0.1) * e);
  }
}

function riser(t0, len, o) {
  const { gain = 0.08, lo = 300, hi = 8000 } = o;
  const w = out(o), bl = svf("bp"), br = svf("bp"), s0 = Math.round(t0 * SR), total = Math.round(len * SR);
  for (let k = 0; k < total; k++) {
    const x = k / total, fc = lo * Math.pow(hi / lo, x), e = Math.pow(x, 2.2) * Math.min(1, (total - k) / (0.01 * SR)) * gain;
    w(s0 + k, bl(rand(), fc, 0.5) * e, br(rand(), fc, 0.5) * e);
  }
}

function air(t0, len, o) {
  const { gain = 0.03, fc = 6000 } = o;
  const w = out(o), bl = svf("bp"), br = svf("bp"), s0 = Math.round(t0 * SR);
  for (let k = 0; k < (len + 3) * SR; k++) {
    const t = k / SR, f = fc * (1 + 0.3 * Math.sin(TAU * 0.07 * (t0 + t))), e = adsr(t, len, 2, 1, 1, 1.2) * gain;
    w(s0 + k, bl(rand(), f, 0.3) * e, br(rand(), f * 1.1, 0.3) * e);
  }
}

function tempo(target, liftBars) {
  const span = AT.logo - AT.headline, bars = Math.round((span * target) / 240), BAR = span / bars, BEAT = BAR / 4;
  const bar = (n, beat = 0) => AT.headline + n * BAR + beat * BEAT;
  const nearest = (t) => Math.round((t - AT.headline) / BAR);
  const on16 = (t) => AT.headline + Math.round((t - AT.headline) / (BEAT / 4)) * (BEAT / 4);
  const g = { bpm: 240 / BAR, BAR, BEAT, bar, groove: nearest(AT.answer), brk: nearest(AT.check7), back: Math.ceil((AT.gap - AT.headline) / BAR), lift: bars - liftBars, end: bars, stamp: on16(AT.stamp), alert: on16(AT.alert) };
  return {
    ...g,
    barOf: (t) => Math.floor((t - AT.headline) / BAR + 1e-6),
    full: (n) => n >= g.groove && n < g.lift && (n < g.brk || n >= g.back),
    quiet: (n) => n >= g.brk && n < g.back - 1,
    rest: (t) => t > g.stamp - BEAT - 1e-6 && t < g.stamp - 1e-6,
    roll: (from, to, step, g0, g1, o = {}) => { for (let k = 0; from + k * step < to - 1e-6; k++) snare(from + k * step, { decay: 0.09, verb: 0.3, pan: -0.1, ...o, gain: ramp(from + k * step, from, to, g0, g1) }); },
  };
}

const SONGS = {
  a() {
    const g = tempo(116, 2), { bar, BEAT, BAR } = g;
    const chords = [[51, 55, 58, 62, 65], [48, 51, 55, 58, 62], [44, 51, 55, 58, 60], [46, 51, 53, 58, 62]];
    const roots = [39, 36, 32, 34];
    const hook = [
      [[0, 79, 0.5], [0.75, 82, 0.5], [1.5, 79, 0.5], [2.25, 77, 0.25], [2.5, 75, 0.75], [3.5, 77, 0.5]],
      [[0, 84, 0.75], [0.75, 82, 0.5], [1.5, 79, 0.5], [2.25, 82, 0.25], [2.5, 79, 0.5], [3, 75, 1]],
      [[0, 79, 0.5], [0.75, 80, 0.5], [1.5, 79, 0.5], [2.25, 75, 0.25], [2.5, 72, 0.75], [3.5, 75, 0.5]],
      [[0, 77, 0.75], [0.75, 75, 0.5], [1.5, 77, 0.5], [2, 82, 1.5]],
    ];
    const at = (n) => (n < 0 ? [2, 3][n + 2] : n >= g.lift ? [2, 3][n - g.lift] : n % 4);
    const last = g.end - 1;

    const kicks = [];
    for (let n = 0; n < g.end; n++) {
      const pat = g.full(n) ? [0, 1.75, 2.5] : g.quiet(n) ? [] : n === 0 ? [0, 2] : n === last ? [0, 0.5, 1, 1.5, 2, 2.5, 3] : [0, 1, 2, 3];
      for (const b of pat) if (!g.rest(bar(n, b))) kicks.push(bar(n, b));
    }
    kicks.push(bar(g.end));
    sidechain(kicks, 0.6, 0.13);
    for (const t of kicks) kick(t, { gain: 0.55, hi: 170, lo: 50, decay: 0.26, click: 0.25 });

    for (let n = -2; n < g.end; n++) {
      const t = bar(n);
      const cut = n < 0 ? (u) => glide(u, 0, AT.headline, 600, 1400)
        : n < g.groove ? (u) => glide(u, bar(0), bar(g.groove), 1400, 3200)
        : g.quiet(n) ? 1600
        : n === g.back - 1 ? (u) => glide(u, t, t + BAR, 1600, 3200)
        : n >= g.lift ? (u) => glide(u, bar(g.lift), bar(g.end), 3000, 6000) : 3200;
      synth(t, chords[at(n)], n === last ? BAR - BEAT / 2 : BAR, { voices: 5, detune: 20, cut, res: 0.15, a: n < 0 ? 1.2 : 0.08, d: 1, r: n === last ? 0.03 : 0.4, gain: n < 0 || g.quiet(n) ? 0.1 : 0.07, verb: 0.35, duck: g.quiet(n) ? 0 : 0.6 });
    }

    const line = [[0.5, 0, 0.4], [1.5, 0, 0.4], [1.75, 12, 0.2], [2.5, 0, 0.4], [3.5, 0, 0.3], [3.75, 7, 0.2]];
    for (let n = 1; n < g.end; n++) {
      if (g.quiet(n) || n === g.back - 1) continue;
      const root = n >= g.lift ? 34 : roots[at(n)];
      const notes = n >= g.lift ? [0, 0.5, 1, 1.5, 2, 2.5, 3, 3.5].filter((b) => n < last || b < 3.5).map((b) => [b, 0, 0.35]) : line;
      const open = n < g.groove ? (n - 1) / (g.groove - 1) : 1;
      for (const [b, iv, len] of notes) {
        const t = bar(n, b);
        if (g.rest(t)) continue;
        synth(t, root + iv, len * BEAT, { cut: 160, env: 400 + 1600 * open, decay: 0.09, res: 0.35, a: 0.002, d: 0.16, s: 0.3, r: 0.04, gain: 0.3, duck: 0.8 });
        if (iv === 0) sub(t, root, len * BEAT * 0.9, { gain: 0.12, duck: 0.8 });
      }
    }

    for (let n = 1; n < g.end; n++) {
      if (g.quiet(n)) continue;
      const busy = g.full(n) || n >= g.lift;
      for (let b = 0; b < 4; b += busy ? 0.25 : 0.5) {
        const t = bar(n, b);
        if (g.rest(t) || (n === last && b >= 3.5)) continue;
        hat(t, { gain: 0.07 * [1, 0.4, 0.7, 0.4][Math.round(b * 4) % 4], decay: b % 1 === 0.5 ? 0.06 : 0.03, pan: 0.3 });
      }
      if (busy) for (const b of [1, 3]) if (!g.rest(bar(n, b))) clap(bar(n, b), { gain: 0.22, tail: 0.11, verb: 0.22, pan: 0.05 });
      if (g.full(n)) for (const b of [0.75, 2.25, 3.25]) if (!g.rest(bar(n, b))) rim(bar(n, b), { gain: 0.06, pan: -0.35 });
    }
    g.roll(bar(g.groove - 1, 2), bar(g.groove), BEAT / 4, 0.04, 0.16);
    g.roll(bar(g.back - 1), bar(g.back - 1, 2), BEAT / 2, 0.03, 0.08);
    g.roll(bar(g.back - 1, 2), bar(g.back), BEAT / 4, 0.08, 0.18);
    g.roll(bar(g.lift), bar(g.lift + 1), BEAT / 2, 0.04, 0.1);
    g.roll(bar(g.lift + 1), bar(last, 3.5), BEAT / 4, 0.1, 0.22);
    riser(bar(-1), BAR, { gain: 0.03, lo: 1500, hi: 9000, verb: 0.3 });
    riser(bar(g.groove - 1), BAR, { gain: 0.04, lo: 400, hi: 6000, verb: 0.3 });
    riser(bar(g.back - 1), BAR, { gain: 0.06, lo: 400, hi: 7000, verb: 0.3 });
    riser(bar(g.lift), bar(last, 3.5) - bar(g.lift), { gain: 0.08, lo: 300, hi: 9000, verb: 0.3 });
    for (const t of [bar(g.groove), bar(g.back), bar(g.back + 4), bar(g.back + 8)]) crash(t, { gain: 0.09, decay: 1.6, verb: 0.2 });

    const lead = (t, n, len, gain = 0.12, o = {}) => synth(t, n, len, { voices: 2, detune: 8, cut: 800, env: 5000, decay: 0.12, res: 0.25, a: 0.002, d: 0.22, s: 0.15, r: 0.12, gain, echo: 0.3, verb: 0.2, pan: 0.12, duck: 0.25, ...o });
    for (const [b, n, len] of hook[3]) lead(bar(-1, b), n, len * BEAT, 0.09, { cut: 700, env: 1500, echo: 0.45 });
    for (let n = g.brk; n < g.back - 1; n++) sub(bar(n), roots[at(n)], BAR, { gain: 0.14, a: 0.3, r: 0.4 });
    for (let n = g.groove; n < g.lift; n++) {
      if (!g.full(n)) continue;
      for (const [b, note, len] of hook[at(n)]) {
        lead(bar(n, b), note, len * BEAT);
        if (n >= g.back + 8) lead(bar(n, b), note + 12, len * BEAT, 0.045, { pan: -0.3 });
      }
      if (n >= g.back + 4) {
        const c = chords[at(n)];
        for (let s = 0; s < 16; s++) synth(bar(n, s / 4), c[[1, 3, 2, 4][s % 4]] + 12, BEAT / 4, { wave: "tri", cut: 3000, a: 0.002, d: 0.08, s: 0, r: 0.05, gain: 0.03, echo: 0.2, pan: s % 2 ? 0.4 : -0.4, duck: 0.4 });
      }
    }

    const stab = (t, notes, gain) => synth(t, notes, BEAT * 1.5, { voices: 3, detune: 12, cut: 1500, env: 7000, decay: 0.2, res: 0.2, a: 0.002, d: 0.4, s: 0.2, r: 0.5, gain, verb: 0.45, echo: 0.2 });
    stab(g.stamp, chords[at(g.barOf(g.stamp))].map((n) => n + 12), 0.12);
    crash(g.stamp, { gain: 0.12, decay: 2, verb: 0.2 });
    [[0, 79], [0.75, 82], [1.5, 84]].forEach(([b, n]) => { lead(g.alert + b * BEAT, n, BEAT * 0.6, 0.11, { echo: 0.5 }); bell(g.alert + b * BEAT, n + 12, { gain: 0.03, decay: 1.2, verb: 0.4, echo: 0.3 }); });

    const E = bar(g.end);
    crash(E, { gain: 0.14, decay: 2.5, verb: 0.25 });
    stab(E, [63, 67, 70, 74, 77], 0.11);
    synth(E, [51, 55, 58, 62, 65, 70], 3.2, { voices: 5, detune: 22, cut: (u) => glide(u, E, E + 4, 4000, 900), res: 0.1, a: 0.01, d: 2, s: 0.8, r: 1.6, gain: 0.1, verb: 0.5 });
    synth(E, 39, 3, { cut: 300, env: 1200, decay: 0.2, res: 0.3, a: 0.002, d: 1, s: 0.5, r: 0.8, gain: 0.3 });
    sub(E, 39, 3, { gain: 0.16, r: 0.8 });
    [[0, 79], [0.75, 82], [1.5, 87]].forEach(([b, n]) => lead(E + b * BEAT, n, BEAT * 0.7, 0.11, { echo: 0.45 }));
    return { name: "E-flat major", bpm: g.bpm, echo: BEAT * 0.75, feedback: 0.35, room: 0.82, wet: 2 };
  },

  b() {
    const g = tempo(124, 2), { bar, BEAT, BAR } = g;
    const keys = [[56, 60, 63, 67], [53, 56, 60, 63], [55, 58, 60, 65], [55, 58, 63, 65]];
    const roots = [41, 37, 39, 36];
    const riff = [
      [[0, 72, 0.5], [0.75, 75, 0.25], [1, 77, 0.5], [1.75, 75, 0.25], [2.5, 72, 0.5], [3, 70, 0.75]],
      [[0.5, 72, 0.5], [1.25, 77, 0.25], [1.5, 80, 0.75], [2.5, 77, 0.5], [3.25, 75, 0.5]],
      [[0, 79, 0.5], [0.75, 77, 0.25], [1, 75, 0.5], [1.75, 72, 0.25], [2.5, 75, 1]],
      [[0.5, 72, 0.5], [1, 70, 0.5], [1.5, 67, 0.5], [2, 70, 0.75], [3, 72, 1]],
    ];
    const at = (n) => (n < 0 ? [2, 3][n + 2] : n >= g.lift ? [1, 2][n - g.lift] : n % 4);
    const last = g.end - 1, build = g.back - 1;

    const kicks = [g.stamp];
    for (let n = 0; n < g.end; n++) {
      if (g.quiet(n)) continue;
      const pat = n === build ? [0, 1, 2, 2.5, 3, 3.5] : n === last ? [0, 1, 2, 2.5, 3] : [0, 1, 2, 3];
      for (const b of pat) if (!g.rest(bar(n, b))) kicks.push(bar(n, b));
    }
    kicks.push(bar(g.end));
    sidechain(kicks, 0.55, 0.16);
    for (const t of kicks) kick(t, { gain: 0.6, hi: 150, lo: 46, decay: 0.32, click: 0.15 });

    const stabs = [0, 0.75, 1.5, 2.5, 3];
    for (let n = -2; n < g.end; n++) {
      const c = keys[at(n)];
      const cut = n < 0 ? 700
        : n < g.groove ? (u) => glide(u, bar(0), bar(g.groove), 700, 9000)
        : n === build ? (u) => glide(u, bar(n), bar(n + 1), 1200, 9000)
        : g.quiet(n) ? 1600 : 9000;
      if (g.quiet(n) || n < 0) epiano(bar(n), c, BAR, { gain: 0.12, wow: 1, cut, verb: 0.45 });
      else for (const b of stabs) if (n < last || b < 3.5) epiano(bar(n, b), c, BEAT * 0.4, { gain: 0.11, wow: 1, cut, verb: 0.25, echo: 0.08, duck: 0.5 });
      if (n >= g.back && n < g.end) synth(bar(n), c.map((x) => x + 12), n === last ? BAR - BEAT / 2 : BAR, { voices: 5, detune: 18, cut: n >= g.lift ? (u) => glide(u, bar(g.lift), bar(g.end), 1500, 6000) : 2200, a: 0.1, r: n === last ? 0.03 : 0.3, gain: 0.035, verb: 0.4, duck: 0.6 });
    }

    const line = [[0.5, 0, 0.45], [1.5, 0, 0.45], [2.5, 0, 0.3], [2.75, 12, 0.2], [3.5, 0, 0.45]];
    for (let n = g.groove - 1; n < g.end; n++) {
      if (g.quiet(n) || n === build) continue;
      const root = roots[at(n)], notes = n % 2 ? [...line, [3.75, 10, 0.2]] : line;
      for (const [b, iv, len] of notes) {
        const t = bar(n, b);
        if (g.rest(t) || (n === last && b >= 3.5)) continue;
        synth(t, root + iv, len * BEAT, { wave: "square", cut: 220, env: n < g.groove ? 300 : 900, decay: 0.12, res: 0.2, a: 0.003, d: 0.2, s: 0.5, r: 0.05, gain: 0.26, duck: 0.85 });
        if (iv === 0) sub(t, root - 12, len * BEAT, { gain: 0.1, duck: 0.85 });
      }
    }

    for (let n = 1; n < g.end; n++) {
      if (g.quiet(n)) continue;
      for (let s = 0; s < 16; s++) {
        const b = s / 4 + (s % 2 ? 0.04 : 0), t = bar(n, b);
        if (g.rest(t) || (n === last && b >= 3.5)) continue;
        if (s % 4 === 2 && n >= g.groove - 1) hat(t, { gain: 0.065, decay: 0.18, pan: 0.2 });
        else if (s % 4 !== 2) hat(t, { gain: 0.032 * [1, 0.5, 0, 0.7][s % 4], decay: 0.025, pan: -0.25 });
      }
      if (g.full(n) || n >= g.lift) for (const b of [1, 3]) if (!g.rest(bar(n, b))) { clap(bar(n, b), { gain: 0.2, tail: 0.13, verb: 0.2 }); snare(bar(n, b), { gain: 0.06, decay: 0.08 }); }
    }
    g.roll(bar(g.groove - 1, 2), bar(g.groove), BEAT / 4, 0.04, 0.15);
    g.roll(bar(build), bar(build, 2), BEAT / 2, 0.03, 0.08);
    g.roll(bar(build, 2), bar(g.back), BEAT / 4, 0.08, 0.18);
    g.roll(bar(g.lift), bar(g.lift + 1), BEAT / 2, 0.04, 0.1);
    g.roll(bar(g.lift + 1), bar(last, 3.5), BEAT / 4, 0.1, 0.2);
    riser(bar(g.groove - 1), BAR, { gain: 0.04, lo: 400, hi: 6000, verb: 0.3 });
    riser(bar(build), BAR, { gain: 0.06, lo: 400, hi: 7000, verb: 0.3 });
    riser(bar(g.lift), bar(last, 3.5) - bar(g.lift), { gain: 0.08, lo: 300, hi: 9000, verb: 0.3 });
    for (const t of [bar(g.groove), bar(g.back), bar(g.back + 8)]) crash(t, { gain: 0.09, decay: 1.6, verb: 0.2 });

    const lead = (t, n, len, gain = 0.08, o = {}) => synth(t, n, len, { wave: "square", voices: 2, detune: 10, cut: 1400, env: 2500, decay: 0.15, res: 0.3, a: 0.005, d: 0.3, s: 0.5, r: 0.1, gain, echo: 0.25, verb: 0.2, pan: -0.1, duck: 0.3, ...o });
    for (let n = g.groove + 4; n < g.lift; n++) if (g.full(n)) for (const [b, note, len] of riff[at(n)]) if (!g.rest(bar(n, b))) lead(bar(n, b), note, len * BEAT);

    const big = (t, notes, gain) => epiano(t, notes, BEAT * 1.6, { gain, bright: 1.5, verb: 0.5, echo: 0.45 });
    big(g.stamp, keys[at(g.barOf(g.stamp))].map((n) => n + 12), 0.1);
    crash(g.stamp, { gain: 0.12, decay: 2, verb: 0.2 });
    clap(g.stamp, { gain: 0.22, tail: 0.15, verb: 0.3 });
    big(g.alert, keys[at(g.barOf(g.alert))].map((n) => n + 12), 0.11);
    boom(g.alert, { gain: 0.22, hi: 90, lo: 40, decay: 1.2 });

    const E = bar(g.end), chord = [56, 60, 63, 67, 70];
    kick(E, { gain: 0.6, hi: 150, lo: 46, decay: 0.32, click: 0.15 });
    crash(E, { gain: 0.14, decay: 2.5, verb: 0.25 });
    epiano(E, chord, 3.5, { gain: 0.12, bright: 1.3, verb: 0.5 });
    synth(E, chord.map((x) => x + 12), 3, { voices: 5, detune: 18, cut: (u) => glide(u, E, E + 4, 5000, 1200), a: 0.01, d: 2, s: 0.8, r: 1.5, gain: 0.06, verb: 0.5 });
    synth(E, 44, 3, { wave: "square", cut: 240, env: 900, decay: 0.2, a: 0.003, d: 1, s: 0.5, r: 0.8, gain: 0.26 });
    sub(E, 32, 3, { gain: 0.12, r: 0.8 });
    [[0, 72], [0.5, 75], [1, 80]].forEach(([b, n]) => lead(E + b * BEAT, n, BEAT * 0.8, 0.08, { echo: 0.45 }));
    return { name: "F minor", bpm: g.bpm, echo: BEAT * 0.75, feedback: 0.3, room: 0.8, wet: 1.8 };
  },

  c() {
    const g = tempo(88, 2), { bar, BEAT, BAR } = g;
    const pads = [[54, 59, 61, 62, 66], [55, 59, 62, 66, 69], [54, 57, 62, 64, 69], [52, 57, 62, 64, 69]];
    const roots = [35, 31, 30, 33];
    const theme = [
      [[0, 78, 1.5], [1.5, 76, 0.5], [2, 74, 2]],
      [[0, 74, 1], [1, 76, 1], [2, 78, 1], [3, 81, 1]],
      [[0, 78, 1.5], [1.5, 76, 0.5], [2, 74, 1], [3, 73, 1]],
      [[0, 76, 2], [2, 74, 1], [3, 71, 1]],
    ];
    const counter = [[[0, 66, 2], [2, 64, 2]], [[0, 62, 4]], [[0, 61, 2], [2, 62, 2]], [[0, 64, 4]]];
    const at = (n) => (n < 0 ? 3 : n >= g.lift ? [1, 3][n - g.lift] : n % 4);
    const last = g.end - 1, build = g.back - 1;

    const kicks = [];
    for (let n = 0; n < g.end; n++) {
      const pat = g.full(n) ? (n % 2 ? [0, 1.5, 3.75] : [0, 1.5]) : n < g.groove ? [0] : n === g.lift ? [0, 1, 2, 3] : n === last ? [0, 0.5, 1, 1.5, 2, 2.5, 3] : [];
      for (const b of pat) if (!g.rest(bar(n, b))) kicks.push(bar(n, b));
    }
    sidechain(kicks, 0.3, 0.2);
    for (const t of kicks) kick(t, { gain: 0.6, hi: 120, lo: 40, decay: 0.5, click: 0.12, verb: 0.05 });

    air(0, AT.logo + 2, { gain: 0.025, fc: 6500, verb: 0.6 });
    for (let n = -2; n < g.end; n++) {
      const t = Math.max(0, bar(n)), len = (n === last ? bar(n, 3.5) : bar(n + 1)) - t, p = pads[at(n)];
      if (n === -1) continue;
      const span = n === -2 ? bar(0) - t : len;
      synth(t, p, span, { wave: "tri", voices: 3, detune: 8, cut: 900, a: n === -2 ? 2.5 : 0.3, r: n === last ? 0.05 : 0.6, gain: 0.07, verb: 0.5, duck: 0.4 });
      if (n >= 0) synth(t, p.map((x) => x + 12), span, { voices: 5, detune: 16, cut: n >= g.lift ? (u) => glide(u, bar(g.lift), bar(g.end), 1800, 7000) : g.quiet(n) ? 1800 : 3000, a: 1, r: n === last ? 0.05 : 1, gain: 0.04, verb: 0.65, duck: 0.4 });
      sub(t, roots[at(n)], span, { gain: 0.17, a: n === -2 ? 2 : 0.05, r: n === last ? 0.03 : 0.25 });
    }

    for (let n = 0; n < g.end; n++) {
      if (g.quiet(n) || n === build) continue;
      const p = pads[at(n)], cut = n < g.groove ? (u) => glide(u, bar(0), bar(g.groove), 500, 1400) : n >= g.lift ? (u) => glide(u, bar(g.lift), bar(g.end), 1400, 4000) : 1400;
      for (let s = 0; s < 16; s++) {
        const t = bar(n, s / 4);
        if (n === last && s >= 14) break;
        synth(t, p[[0, 1, 2, 3, 1, 2, 3, 2][s % 8]], BEAT / 4, { cut, env: 1200, decay: 0.07, a: 0.002, d: 0.12, s: 0, r: 0.05, gain: 0.05, pan: s % 2 ? 0.3 : -0.3, echo: 0.15, duck: 0.5 });
      }
    }

    for (let n = g.groove; n < g.end; n++) {
      if (!g.full(n) && n < g.lift) continue;
      if (g.full(n)) {
        if (!g.rest(bar(n, 2))) { snare(bar(n, 2), { gain: 0.3, decay: 0.22, tone: 170, verb: 0.45 }); clap(bar(n, 2), { gain: 0.12, tail: 0.2, verb: 0.45 }); }
        for (let b = 0; b < 4; b += n % 4 === 3 && b >= 3 ? 0.25 : 0.5) if (!g.rest(bar(n, b))) hat(bar(n, b), { gain: b % 1 ? 0.03 : 0.02, decay: 0.035, pan: 0.25 });
        for (const [b, note, len] of theme[at(n)]) {
          bell(bar(n, b), note, { gain: 0.06, decay: Math.max(1.2, len * BEAT * 1.2), verb: 0.5, echo: 0.2, pan: 0.15 });
          bell(bar(n, b), note - 12, { gain: 0.025, decay: 1.6, verb: 0.5, pan: -0.15 });
        }
        if (n >= g.back) for (const [b, note, len] of counter[at(n)]) synth(bar(n, b), note, len * BEAT, { voices: 3, detune: 10, cut: 1400, a: 0.4, d: 1, s: 0.9, r: 0.6, gain: 0.045, verb: 0.5, pan: -0.2 });
      } else if (n === g.lift) {
        for (const b of [1, 3]) snare(bar(n, b), { gain: 0.24, decay: 0.2, tone: 170, verb: 0.45 });
      }
    }
    for (let k = 0; k < 8; k++) tom(bar(build, k / 2), [45, 50][k % 2], { gain: ramp(k, 0, 7, 0.12, 0.22), verb: 0.35 });
    for (let k = 0; k < 8; k++) tom(bar(build, 2 + k / 4), [50, 47, 45, 43][k % 4], { gain: ramp(k, 0, 7, 0.2, 0.3), verb: 0.35 });
    for (let k = 0; k < 14; k++) tom(bar(last, k / 4), [50, 47, 45, 43][k % 4], { gain: ramp(k, 0, 13, 0.15, 0.3), verb: 0.35 });
    g.roll(bar(build, 2), bar(g.back), BEAT / 4, 0.05, 0.16, { decay: 0.12, verb: 0.4 });
    g.roll(bar(g.lift), bar(last, 3.5), BEAT / 4, 0.03, 0.2, { decay: 0.12, verb: 0.4 });
    riser(bar(-1), bar(0) - bar(-1), { gain: 0.03, lo: 1500, hi: 8000, verb: 0.4 });
    riser(bar(build), BAR, { gain: 0.06, lo: 300, hi: 7000, verb: 0.4 });
    riser(bar(g.lift), bar(last, 3.5) - bar(g.lift), { gain: 0.09, lo: 200, hi: 9000, verb: 0.4 });
    for (const t of [bar(g.groove), bar(g.back)]) crash(t, { gain: 0.1, decay: 2.2, verb: 0.3 });
    bell(1.0, 78, { gain: 0.05, decay: 2.5, verb: 0.6, echo: 0.3 });

    const braam = (t, root) => synth(t, [root, root + 7, root + 12], 1.8, { voices: 3, detune: 14, cut: (u) => 150 + 1800 * Math.min(1, (u - t) / 0.15) * Math.exp(-Math.max(0, u - t - 0.15) / 0.6), res: 0.3, a: 0.02, d: 1.2, s: 0.5, r: 0.8, gain: 0.16, verb: 0.5 });
    for (let k = 0; k < 4; k++) tom(g.stamp - BEAT + (k * BEAT) / 4, [50, 47, 45, 43][k], { gain: 0.22, verb: 0.35 });
    boom(g.stamp, { gain: 0.38 });
    braam(g.stamp, roots[at(g.barOf(g.stamp))] + 12);
    crash(g.stamp, { gain: 0.12, decay: 2.4, verb: 0.3 });
    riser(g.alert - 1.2, 1.2, { gain: 0.04, lo: 1500, hi: 8000, verb: 0.4 });
    for (const [d, n] of [[0, 83], [BEAT / 2, 90]]) bell(g.alert + d, n, { gain: 0.07, decay: 2.5, verb: 0.6, echo: 0.35 });

    const E = bar(g.end);
    boom(E, { gain: 0.42, decay: 1.8 });
    kick(E, { gain: 0.6, hi: 120, lo: 40, decay: 0.5, click: 0.12 });
    braam(E, 38);
    crash(E, { gain: 0.14, decay: 3, verb: 0.3 });
    synth(E, [50, 57, 62, 64, 66, 69, 73], 3.6, { wave: "tri", voices: 3, detune: 8, cut: 1400, a: 0.02, d: 2, s: 0.8, r: 1.6, gain: 0.09, verb: 0.55 });
    synth(E, [62, 69, 74, 76, 78, 81], 3.6, { voices: 5, detune: 16, cut: (u) => glide(u, E, E + 4, 6000, 1500), a: 0.05, d: 2, s: 0.8, r: 1.6, gain: 0.05, verb: 0.65 });
    sub(E, 38, 3.6, { gain: 0.2, r: 1 });
    [[0, 78], [0.75, 81], [1.5, 86]].forEach(([b, n]) => bell(E + b * BEAT, n, { gain: 0.07, decay: 2.8, verb: 0.6, echo: 0.3 }));
    return { name: "B minor to D major", bpm: g.bpm, echo: BEAT * 0.75, feedback: 0.45, room: 0.88, wet: 2.6 };
  },
};

function echo(time, feedback) {
  const d = Math.round(time * SR), bl = new Float32Array(N), br = new Float32Array(N), ol = new Float32Array(N), or = new Float32Array(N);
  let fl = 0, fr = 0;
  for (let i = 0; i < N; i++) {
    const yl = i >= d ? bl[i - d] : 0, yr = i >= d ? br[i - d] : 0;
    fl += 0.35 * (yl - fl); fr += 0.35 * (yr - fr);
    bl[i] = (EL[i] + ER[i]) * 0.5 + fr * feedback;
    br[i] = fl * feedback;
    ol[i] = fl; or[i] = fr;
  }
  return [ol, or];
}

function freeverb(inp, spread, fb) {
  const combs = [1557, 1617, 1491, 1422, 1277, 1356, 1188, 1116].map((d) => ({ buf: new Float32Array(d + spread), i: 0, store: 0 }));
  const aps = [556, 441, 341, 225].map((d) => ({ buf: new Float32Array(d + spread), i: 0 }));
  const o = new Float32Array(inp.length);
  for (let n = 0; n < inp.length; n++) {
    const x = inp[n] * 0.015;
    let y = 0;
    for (const c of combs) {
      const v = c.buf[c.i];
      c.store = v * 0.6 + c.store * 0.4;
      c.buf[c.i] = x + c.store * fb;
      c.i = (c.i + 1) % c.buf.length;
      y += v;
    }
    for (const a of aps) {
      const v = a.buf[a.i];
      a.buf[a.i] = y + v * 0.5;
      a.i = (a.i + 1) % a.buf.length;
      y = v - y;
    }
    o[n] = y;
  }
  return o;
}

function loudness(l, r) {
  const kw = (x) => {
    const y = new Float32Array(N);
    const stages = [[[1.53512485958697, -2.69169618940638, 1.19839281085285], [-1.69065929318241, 0.73248077421585]], [[1, -2, 1], [-1.99004745483398, 0.99007225036621]]];
    let src = x;
    for (const [[b0, b1, b2], [a1, a2]] of stages) {
      let x1 = 0, x2 = 0, y1 = 0, y2 = 0;
      for (let i = 0; i < N; i++) {
        const v = b0 * src[i] + b1 * x1 + b2 * x2 - a1 * y1 - a2 * y2;
        x2 = x1; x1 = src[i]; y2 = y1; y1 = v; y[i] = v;
      }
      src = y.slice();
    }
    return src;
  };
  const a = kw(l), b = kw(r), block = Math.round(0.4 * SR), hop = Math.round(0.1 * SR), z = [];
  for (let s = 0; s + block <= N; s += hop) {
    let sum = 0;
    for (let i = s; i < s + block; i++) sum += a[i] * a[i] + b[i] * b[i];
    z.push(sum / block);
  }
  const lu = (m) => -0.691 + 10 * Math.log10(m), mean = (xs) => xs.reduce((p, q) => p + q, 0) / xs.length;
  const gated = z.filter((m) => lu(m) > -70), rel = lu(mean(gated)) - 10;
  return lu(mean(gated.filter((m) => lu(m) > rel)));
}

function limit(l, r, gain, ceiling) {
  const LA = Math.round(0.003 * SR), rel = Math.exp(-1 / (0.08 * SR));
  const need = new Float32Array(N), hold = new Float32Array(N), q = new Int32Array(N);
  for (let i = 0; i < N; i++) {
    const p = Math.max(Math.abs(l[i]), Math.abs(r[i])) * gain;
    need[i] = p > ceiling ? ceiling / p : 1;
  }
  for (let i = N - 1, h = 0, t = 0; i >= 0; i--) {
    while (t > h && need[q[t - 1]] >= need[i]) t--;
    q[t++] = i;
    while (q[h] > i + LA) h++;
    hold[i] = need[q[h]];
  }
  const ol = new Float32Array(N), or = new Float32Array(N), env = new Float32Array(N);
  let e = 1, sum = LA + 1;
  for (let i = 0; i < N; i++) {
    e = Math.min(hold[i], 1 - (1 - e) * rel);
    env[i] = e;
    sum += e - (i > LA ? env[i - LA - 1] : 1);
    const gg = (sum / (LA + 1)) * gain;
    ol[i] = clamp(l[i] * gg, -ceiling, ceiling);
    or[i] = clamp(r[i] * gg, -ceiling, ceiling);
  }
  return [ol, or];
}

const song = SONGS[pick];
if (!song) { console.error(`usage: node audio.mjs [${Object.keys(SONGS).join("|")}] [out.wav]`); process.exit(2); }
const mix = song();
const [ecL, ecR] = echo(mix.echo, mix.feedback);
for (let i = 0; i < N; i++) { VL[i] += ecL[i] * 0.3; VR[i] += ecR[i] * 0.3; }
const wetL = freeverb(VL, 0, mix.room), wetR = freeverb(VR, 23, mix.room);
const FADE = DURATION - 3.5;
const busL = new Float32Array(N), busR = new Float32Array(N);
for (let i = 0; i < N; i++) {
  const t = i / SR, f = t < FADE ? 1 : Math.pow(1 - (t - FADE) / (DURATION - FADE), 2);
  busL[i] = (L[i] + ecL[i] + wetL[i] * mix.wet) * f;
  busR[i] = (R[i] + ecR[i] + wetR[i] * mix.wet) * f;
}

const TARGET = -16, CEILING = 0.95;
let gain = Math.pow(10, (TARGET - loudness(busL, busR)) / 20), master;
for (let pass = 0; pass < 3; pass++) {
  master = limit(busL, busR, gain, CEILING);
  gain *= Math.pow(10, (TARGET - loudness(...master)) / 20);
}
master = limit(busL, busR, gain, CEILING);
const [ml, mr] = master;

const pcm = Buffer.alloc(N * 4);
let peak = 0;
for (let i = 0; i < N; i++) {
  peak = Math.max(peak, Math.abs(ml[i]), Math.abs(mr[i]));
  pcm.writeInt16LE(Math.round(ml[i] * 32767), i * 4);
  pcm.writeInt16LE(Math.round(mr[i] * 32767), i * 4 + 2);
}
const header = Buffer.alloc(44);
header.write("RIFF", 0); header.writeUInt32LE(36 + pcm.length, 4); header.write("WAVE", 8);
header.write("fmt ", 12); header.writeUInt32LE(16, 16); header.writeUInt16LE(1, 20); header.writeUInt16LE(2, 22);
header.writeUInt32LE(SR, 24); header.writeUInt32LE(SR * 4, 28); header.writeUInt16LE(4, 32); header.writeUInt16LE(16, 34);
header.write("data", 36); header.writeUInt32LE(pcm.length, 40);
fs.writeFileSync(outPath, Buffer.concat([header, pcm]));
console.log(`song ${pick}: ${mix.name}, ${mix.bpm.toFixed(1)} BPM, ${loudness(ml, mr).toFixed(1)} LUFS, peak ${peak.toFixed(3)}, wrote ${outPath}`);
