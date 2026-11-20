// Synthesizes the launch video's soundtrack: an original ambient bed in C
// major and sound effects for the enrichment scene, timed to launch.html.
// Everything is generated here, so there is nothing to license.
//
//   node audio.mjs [out.wav]
import fs from "node:fs";

const SR = 48000;
const DURATION = 47;
const N = SR * DURATION;
const L = new Float32Array(N), R = new Float32Array(N); // dry mix
const RL = new Float32Array(N), RR = new Float32Array(N); // reverb send

const TAU = Math.PI * 2;
const midi = (n) => 440 * Math.pow(2, (n - 69) / 12);
const clamp = (x, a, b) => Math.min(b, Math.max(a, x));
// Deterministic noise, so every render sounds the same.
let seed = 12345;
const rand = () => ((seed = (seed * 1664525 + 1013904223) >>> 0) / 4294967296) * 2 - 1;

function add(i, l, r, send = 0) {
  if (i < 0 || i >= N) return;
  L[i] += l; R[i] += r; RL[i] += l * send; RR[i] += r * send;
}
const panGains = (p) => [Math.cos((p + 1) * Math.PI / 4), Math.sin((p + 1) * Math.PI / 4)];

// Arrangement, in seconds, matched to the video's scenes.
const BPM = 120, BEAT = 60 / BPM, BAR = BEAT * 4;
// KICK_IN: four-on-the-floor starts with the headline. DRUMS_IN: claps,
// hats and the full groove arrive with the sign-up feed.
const ARP_IN = 2.0, KICK_IN = 4.5, DRUMS_IN = 10.0, RIM_IN = 18.8, RISER_AT = 38.2, END_AT = 40.2, FADE_FROM = 44.5;
// Sidechain pump: pads and bass duck after every kick while it plays.
const pump = (t) => (t < KICK_IN || t >= RISER_AT ? 1 : 1 - 0.5 * Math.exp(-(((t - KICK_IN) % BEAT + BEAT) % BEAT) / 0.1));
// Section gain: everything rides under this, then fades at the end.
const master = (t) => clamp(t / 1.5, 0, 1) * (t > FADE_FROM ? Math.pow(clamp(1 - (t - FADE_FROM) / (DURATION - FADE_FROM), 0, 1), 1.6) : 1);

// Chords, two bars each: Cmaj9, Am9, Fmaj9, G6sus.
const chords = [
  [48, 52, 55, 59, 62],
  [45, 48, 52, 55, 59],
  [41, 45, 48, 52, 55],
  [43, 45, 50, 52, 55],
];
const chordAt = (t) => chords[Math.floor(t / (BAR * 2)) % chords.length];

// Pad: detuned saws through a two-pole lowpass, slow attack and release.
function pad(start, len, notes, gain, cutoff = 1400) {
  for (const n of notes) {
    for (const det of [-0.08, 0, 0.07]) {
      const f = midi(n + det);
      const pan = det * 6;
      const [gl, gr] = panGains(pan);
      let ph = (n * 0.137 + det + 1) % 1, y1 = 0, y2 = 0;
      const a = 1 - Math.exp(-TAU * cutoff / SR);
      const s0 = Math.floor(start * SR), s1 = Math.floor((start + len + 2.2) * SR);
      for (let i = s0; i < s1 && i < N; i++) {
        const t = (i - s0) / SR;
        const env = Math.min(1, t / 1.6) * (t > len ? Math.exp(-(t - len) / 0.7) : 1);
        ph += f / SR; ph -= Math.floor(ph);
        const x = 2 * ph - 1;
        y1 += a * (x - y1); y2 += a * (y1 - y2);
        const v = y2 * env * gain * pump(i / SR) / notes.length;
        add(i, v * gl, v * gr, 0.55);
      }
    }
  }
}

// Plucked note: sine plus a little second harmonic, fast decay.
function pluck(t0, f, gain, pan, decay = 0.35, send = 0.45) {
  const [gl, gr] = panGains(pan);
  const s0 = Math.floor(t0 * SR), len = Math.floor(decay * 5 * SR);
  for (let k = 0; k < len; k++) {
    const t = k / SR;
    const env = Math.min(1, t / 0.004) * Math.exp(-t / decay);
    const v = (Math.sin(TAU * f * t) + 0.25 * Math.sin(TAU * 2 * f * t) * Math.exp(-t / (decay * 0.4))) * env * gain;
    add(s0 + k, v * gl, v * gr, send);
  }
}

// Bass: a short plucky saw-and-sine note, filtered, ducked by the kick.
function bass(t0, n, len, gain) {
  const f = midi(n - 12);
  const s0 = Math.floor(t0 * SR), s1 = Math.floor((t0 + len) * SR);
  let ph = 0, y1 = 0, y2 = 0;
  for (let i = s0; i < s1 && i < N; i++) {
    const t = (i - s0) / SR;
    const fc = 220 + 1300 * Math.exp(-t / 0.06);
    const a = 1 - Math.exp(-TAU * fc / SR);
    ph += f / SR; ph -= Math.floor(ph);
    y1 += a * ((2 * ph - 1) - y1); y2 += a * (y1 - y2);
    const env = Math.min(1, t / 0.004) * Math.min(1, (len - t) / 0.02);
    const v = (y2 * 0.7 + Math.sin(TAU * ph) * 0.6) * env * gain * pump(i / SR);
    add(i, v, v, 0.03);
  }
}

// Clap: three quick band-passed noise bursts and a short tail.
function clap(t0, gain) {
  const s0 = Math.floor(t0 * SR);
  let low = 0, band = 0;
  const fq = 2 * Math.sin(Math.PI * 1300 / SR);
  for (let k = 0; k < 0.25 * SR; k++) {
    const t = k / SR;
    const burst = [0, 0.011, 0.022].reduce((m, o) => m + (t >= o ? Math.exp(-(t - o) / 0.004) : 0), 0);
    const env = Math.min(1.2, burst) + 0.6 * Math.exp(-t / 0.06) * (t > 0.022 ? 1 : 0);
    const inp = rand();
    low += fq * band; const high = inp - low - 0.6 * band; band += fq * high;
    const v = band * env * gain;
    add(s0 + k, v * 0.95, v, 0.3);
  }
}

// Open hat: longer high-passed noise on the offbeat.
function openHat(t0, gain, pan) {
  const [gl, gr] = panGains(pan);
  const s0 = Math.floor(t0 * SR);
  let prev = 0, prev2 = 0;
  for (let k = 0; k < 0.2 * SR; k++) {
    const t = k / SR;
    const n = rand(); const hp = n - 2 * prev + prev2; prev2 = prev; prev = n;
    const v = hp * Math.exp(-t / 0.055) * gain;
    add(s0 + k, v * gl, v * gr, 0.2);
  }
}

function kick(t0, gain) {
  const s0 = Math.floor(t0 * SR);
  let ph = 0;
  for (let k = 0; k < 0.45 * SR; k++) {
    const t = k / SR;
    const f = 48 + 95 * Math.exp(-t / 0.035);
    ph += f / SR;
    const v = Math.sin(TAU * ph) * Math.exp(-t / 0.16) * gain;
    add(s0 + k, v, v, 0.04);
  }
}

function hat(t0, gain, pan) {
  const [gl, gr] = panGains(pan);
  const s0 = Math.floor(t0 * SR);
  let prev = 0;
  for (let k = 0; k < 0.06 * SR; k++) {
    const t = k / SR;
    const n = rand(); const hp = n - prev; prev = n;
    const v = hp * Math.exp(-t / 0.014) * gain;
    add(s0 + k, v * gl, v * gr, 0.15);
  }
}

function rim(t0, gain) {
  const s0 = Math.floor(t0 * SR);
  for (let k = 0; k < 0.12 * SR; k++) {
    const t = k / SR;
    const v = (Math.sin(TAU * 1650 * t) * 0.6 + rand() * 0.4) * Math.exp(-t / 0.022) * gain;
    add(s0 + k, v * 0.9, v, 0.35);
  }
}

// FM bell: the "enriched" chime.
function bell(t0, f, gain, pan, decay = 1.3) {
  const [gl, gr] = panGains(pan);
  const s0 = Math.floor(t0 * SR);
  for (let k = 0; k < decay * 4 * SR; k++) {
    const t = k / SR;
    const idx = 2.4 * Math.exp(-t / 0.18);
    const env = Math.min(1, t / 0.002) * Math.exp(-t / decay);
    const v = (Math.sin(TAU * f * t + idx * Math.sin(TAU * f * 3.5 * t)) + 0.35 * Math.sin(TAU * f * 2 * t) * Math.exp(-t / (decay * 0.3))) * env * gain;
    add(s0 + k, v * gl, v * gr, 0.6);
  }
}

// Soft tick: a row arriving.
function tick(t0, gain, pan) {
  const [gl, gr] = panGains(pan);
  const s0 = Math.floor(t0 * SR);
  let ph = 0;
  for (let k = 0; k < 0.12 * SR; k++) {
    const t = k / SR;
    const f = 900 + 1100 * Math.exp(-t / 0.012);
    ph += f / SR;
    const v = (Math.sin(TAU * ph) * 0.85 + rand() * 0.15 * Math.exp(-t / 0.004)) * Math.min(1, t / 0.0015) * Math.exp(-t / 0.028) * gain;
    add(s0 + k, v * gl, v * gr, 0.25);
  }
}

// Shimmer: band-passed noise rising while a row is "enriching".
function shimmer(t0, len, gain, pan) {
  const [gl, gr] = panGains(pan);
  const s0 = Math.floor(t0 * SR), n = Math.floor(len * SR);
  let low = 0, band = 0;
  for (let k = 0; k < n; k++) {
    const x = k / n;
    const fc = 900 * Math.pow(4.2, x);
    const fq = 2 * Math.sin(Math.PI * fc / SR);
    // Chamberlin state-variable filter, band-pass output.
    const inp = rand();
    low += fq * band; const high = inp - low - 0.5 * band; band += fq * high;
    const env = Math.sin(Math.PI * Math.min(1, x * 1.15)) * (0.4 + 0.6 * x);
    const v = band * env * gain;
    add(s0 + k, v * gl, v * gr, 0.5);
  }
}

// Riser into the end card, then a soft impact.
function riser(t0, len, gain) {
  const s0 = Math.floor(t0 * SR), n = Math.floor(len * SR);
  let low = 0, band = 0;
  for (let k = 0; k < n; k++) {
    const x = k / n;
    const fc = 300 * Math.pow(20, x);
    const fq = 2 * Math.sin(Math.PI * Math.min(fc, SR / 6) / SR);
    const inp = rand();
    low += fq * band; const high = inp - low - 0.7 * band; band += fq * high;
    const v = band * Math.pow(x, 2.2) * gain;
    add(s0 + k, v * 0.9, v, 0.5);
  }
}

// --- Arrangement ---

// Pads across the whole video, opening up as the sections build.
for (let b = 0; b * BAR * 2 < END_AT - 0.5; b++) {
  const t = b * BAR * 2;
  pad(t, BAR * 2, chordAt(t), 0.15, t < KICK_IN ? 1200 : t < DRUMS_IN ? 1800 : 2600);
}
// Sixteenth-note arp, two octaves up, its level rising into the feed.
for (let t = ARP_IN, i = 0; t < END_AT - 0.05; t += BEAT / 2 / 2, i++) {
  const c = chordAt(t);
  const n = c[[0, 2, 4, 2, 3, 1, 4, 3][i % 8]] + 24;
  const g = t < KICK_IN ? 0.02 + 0.025 * clamp((t - ARP_IN) / 2.5, 0, 1) : t < DRUMS_IN ? 0.05 : 0.06;
  pluck(t, midi(n), g * (i % 4 === 0 ? 1.25 : 1), i % 2 ? 0.5 : -0.5, 0.16);
}
// Offbeat bass from the kick: root, root, octave, root on the "and"s.
for (let t = KICK_IN, i = 0; t < RISER_AT - 0.05; t += BEAT, i++) {
  const root = chordAt(t)[0];
  bass(t + BEAT / 2, [root, root, root + 12, root][i % 4], BEAT / 2 - 0.02, t < DRUMS_IN ? 0.12 : 0.16);
}
// Drums: four on the floor from the headline; claps, open hats and
// sixteenth hats from the feed; a rim click from the providers.
for (let t = KICK_IN, i = 0; t < RISER_AT - 0.01; t += BEAT, i++) {
  kick(t, 0.34);
  if (t >= DRUMS_IN) {
    if (i % 2 === 1) clap(t, 0.22);
    openHat(t + BEAT / 2, 0.06, 0.25);
    hat(t + BEAT / 4, 0.03, -0.3);
    hat(t + (BEAT * 3) / 4, 0.03, -0.3);
    if (t >= RIM_IN && i % 4 === 3) rim(t + (BEAT * 3) / 4, 0.06);
  } else {
    hat(t + BEAT / 2, 0.035, 0.25);
  }
}
riser(RISER_AT - 0.2, END_AT - RISER_AT + 0.2, 0.09);
// End card: kick, a wide Cmaj9 and a bell.
kick(END_AT, 0.5);
pad(END_AT, DURATION - END_AT - 2.2, [36, 48, 52, 55, 59, 62, 67], 0.26, 2200);
bass(END_AT, 48, 2.5, 0.14);
bell(END_AT + 0.05, midi(76), 0.12, 0, 2.2);
bell(END_AT + 0.4, midi(79), 0.07, 0.4, 2.0);

// --- Enrichment sound effects, synced to launch.html's feed ---
// Rows arrive at 11.3 + i*0.95 and flip to "Enriched" about 1.2s later.
const chime = [76, 79, 81, 84, 86]; // E5 G5 A5 C6 D6, rising with each row
for (let i = 0; i < 5; i++) {
  const a = 11.3 + i * 0.95;
  const pan = [-0.35, 0.3, -0.15, 0.4, 0][i];
  tick(a + 0.02, 0.22, pan);
  shimmer(a + 0.15, 1.05, 0.05, pan);
  bell(a + 1.2, midi(chime[i]), 0.11, pan, 1.1);
  bell(a + 1.2, midi(chime[i] + 12), 0.025, -pan, 0.5);
}

// --- Reverb (Freeverb style) on the send, then master ---
function freeverb(inp, spread) {
  const combs = [1557, 1617, 1491, 1422, 1277, 1356, 1188, 1116].map((d) => ({ buf: new Float32Array(d + spread), i: 0, store: 0 }));
  const aps = [556, 441, 341, 225].map((d) => ({ buf: new Float32Array(d + spread), i: 0 }));
  const out = new Float32Array(inp.length);
  const fb = 0.86, damp = 0.25;
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
let peak = 0;
for (let i = 0; i < N; i++) {
  const g = master(i / SR);
  // Gentle saturation keeps peaks round instead of clipping.
  const l = Math.tanh((L[i] + wetL[i] * 2.4) * g * 1.5);
  const r = Math.tanh((R[i] + wetR[i] * 2.4) * g * 1.5);
  peak = Math.max(peak, Math.abs(l), Math.abs(r));
  pcm.writeInt16LE(Math.round(clamp(l, -1, 1) * 32000), i * 4);
  pcm.writeInt16LE(Math.round(clamp(r, -1, 1) * 32000), i * 4 + 2);
}

const out = process.argv[2] ?? new URL("./keyring-launch.wav", import.meta.url).pathname;
const header = Buffer.alloc(44);
header.write("RIFF", 0); header.writeUInt32LE(36 + pcm.length, 4); header.write("WAVE", 8);
header.write("fmt ", 12); header.writeUInt32LE(16, 16); header.writeUInt16LE(1, 20); header.writeUInt16LE(2, 22);
header.writeUInt32LE(SR, 24); header.writeUInt32LE(SR * 4, 28); header.writeUInt16LE(4, 32); header.writeUInt16LE(16, 34);
header.write("data", 36); header.writeUInt32LE(pcm.length, 40);
fs.writeFileSync(out, Buffer.concat([header, pcm]));
console.log(`wrote ${out}: ${DURATION}s, peak ${peak.toFixed(2)}`);
