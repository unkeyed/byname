// The landing page: the copy button, the hero answer card, the panel matrix,
// the product tabs, the agent terminal and the canvas halftones. Every
// animation is a timeline that runs only while it is on screen, and every
// piece shows its end state when the visitor prefers reduced motion.
const still = matchMedia("(prefers-reduced-motion: reduce)").matches;
const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // The Clipboard API needs a secure context and permission; fall back to a selection.
    const area = document.createElement("textarea");
    area.value = text;
    area.setAttribute("readonly", "");
    area.style.position = "fixed";
    area.style.opacity = "0";
    document.body.appendChild(area);
    area.select();
    const ok = document.execCommand("copy");
    area.remove();
    return ok;
  }
}

{
  const source = document.getElementById("agent-prompt");
  const toast = $(".toast");
  let hide;
  if (source && toast) {
    const prompt = source.textContent.trim();
    const buttons = $$("[data-copy-prompt]");
    for (const button of buttons) {
      button.addEventListener("click", async () => {
        const ok = await copyText(prompt);
        button.querySelector(".copy-state").textContent = ok ? "Copied" : "Copy failed";
        toast.textContent = ok ? "Prompt copied. Paste it into your coding agent to deploy on Unkey." : "Could not copy. Select the prompt and copy it by hand.";
        toast.classList.add("show");
        clearTimeout(hide);
        hide = setTimeout(() => {
          toast.classList.remove("show");
          for (const b of buttons) b.querySelector(".copy-state").textContent = b.dataset.label;
        }, 2400);
      });
    }
  }
}

// A timeline is a sorted list of { at, run } events. The player applies every
// event whose time has passed, so pausing and resuming lands exactly where it
// left off, and running is gated on visibility.
function timeline({ onFrame, onEnd } = {}) {
  let events = [];
  let i = 0;
  let t = 0;
  let end = 0;
  let raf = 0;
  let last = 0;
  let wanted = false;
  let visible = false;
  const frame = (now) => {
    t += Math.min(now - last, 64);
    last = now;
    while (i < events.length && events[i].at <= t) events[i++].run();
    onFrame?.(t, end);
    if (t >= end) {
      raf = 0;
      wanted = false;
      onEnd?.();
      return;
    }
    raf = requestAnimationFrame(frame);
  };
  const sync = () => {
    const go = wanted && visible && !document.hidden;
    if (go && !raf) {
      last = performance.now();
      raf = requestAnimationFrame(frame);
    } else if (!go && raf) {
      cancelAnimationFrame(raf);
      raf = 0;
    }
  };
  document.addEventListener("visibilitychange", sync);
  return {
    load(list, duration) {
      events = list.sort((a, b) => a.at - b.at);
      end = duration ?? (events.at(-1)?.at ?? 0);
      i = 0;
      t = 0;
    },
    play() { wanted = true; sync(); },
    pause() { wanted = false; sync(); },
    setVisible(v) { visible = v; sync(); },
  };
}

function watch(el, cb, margin = "0px") {
  const io = new IntersectionObserver((entries) => {
    for (const e of entries) cb(e.isIntersecting);
  }, { rootMargin: margin });
  io.observe(el);
}

// Wraps every character, or every word, of an element's text nodes in a
// span.tk, skipping nodes inside `keep`, which become one token each.
function tokenize(root, { chars = false, keep = null } = {}) {
  const tokens = [];
  const walk = (node) => {
    for (const child of [...node.childNodes]) {
      if (child.nodeType === 1 && keep && child.matches(keep)) {
        child.classList.add("tk");
        tokens.push(child);
      } else if (child.nodeType === 1) {
        walk(child);
      } else if (child.nodeType === 3 && child.textContent.trim()) {
        const frag = document.createDocumentFragment();
        const parts = chars ? [...child.textContent] : child.textContent.split(/(\s+)/);
        for (const part of parts) {
          if (!part) continue;
          if (/^\s+$/.test(part)) {
            frag.append(part);
            continue;
          }
          const s = document.createElement("span");
          s.className = "tk";
          s.textContent = part;
          frag.append(s);
          tokens.push(s);
        }
        child.replaceWith(frag);
      }
    }
  };
  walk(root);
  return tokens;
}

// Deterministic value noise, so each panel always gets the same texture.
function noise2(seed) {
  const h = (x, y) => {
    let n = (x * 374761393 + y * 668265263 + seed * 2147483647) | 0;
    n = (n ^ (n >>> 13)) * 1274126177;
    return ((n ^ (n >>> 16)) >>> 0) / 4294967295;
  };
  const s = (t) => t * t * (3 - 2 * t);
  return (x, y) => {
    const xi = Math.floor(x), yi = Math.floor(y);
    const xf = s(x - xi), yf = s(y - yi);
    const a = h(xi, yi), b = h(xi + 1, yi), c = h(xi, yi + 1), d = h(xi + 1, yi + 1);
    return a + (b - a) * xf + (c - a) * yf + (a - b - c + d) * xf * yf;
  };
}

// A panel is a patchwork of textures on paper, like a print proof: halftone
// dots that thin out from a corner, ordered dither, and rows of mono digits.
// Patch rects are fractions of the canvas: [x, y, w, h].
const tones = {
  lime: { seed: 3, patches: [{ r: [0, 0, 0.58, 1], t: "dots", from: [0, 1] }, { r: [0.58, 0, 0.42, 0.4], t: "dither" }, { r: [0.62, 0.66, 0.38, 0.34], t: "digits" }] },
  blue: { seed: 7, patches: [{ r: [0, 0, 1, 0.3], t: "dither" }, { r: [0, 0.3, 0.46, 0.7], t: "dots", from: [0, 0.3] }, { r: [0.7, 0.52, 0.3, 0.48], t: "digits" }] },
  orange: { seed: 11, patches: [{ r: [0, 0, 0.44, 0.46], t: "digits" }, { r: [0.44, 0, 0.56, 1], t: "dots", from: [1, 0] }, { r: [0, 0.72, 0.44, 0.28], t: "dither" }] },
  violet: { seed: 5, patches: [{ r: [0, 0, 1, 1], t: "dots", from: [0.5, 1.1] }, { r: [0, 0, 0.36, 0.22], t: "dither" }] },
  white: { seed: 13, patches: [{ r: [0, 0, 0.5, 1], t: "dots", from: [0, 0] }, { r: [0.5, 0.7, 0.5, 0.3], t: "dither" }, { r: [0.76, 0, 0.24, 0.5], t: "digits" }] },
};
const PAPER = "#d8d5cc";
const INK = "#0b0b0e";
const BAYER = [0, 8, 2, 10, 12, 4, 14, 6, 3, 11, 1, 9, 15, 7, 13, 5];

const textures = {
  dots(ctx, [x0, y0, w, h], n, from, W, H) {
    const cell = 6;
    const reach = Math.hypot(W, H) * 0.75;
    ctx.beginPath();
    for (let y = y0 + cell / 2; y < y0 + h; y += cell) {
      for (let x = x0 + cell / 2; x < x0 + w; x += cell) {
        const d = Math.hypot(x - from[0] * W, y - from[1] * H) / reach;
        const c = Math.pow(Math.max(0, 1 - d), 1.6) * (0.45 + 0.75 * n(x / 70, y / 70));
        const r = (cell / 2) * Math.sqrt(Math.min(1, c)) * 1.1;
        if (r < 0.45) continue;
        ctx.moveTo(x + r, y);
        ctx.arc(x, y, r, 0, Math.PI * 2);
      }
    }
    ctx.fill();
  },
  dither(ctx, [x0, y0, w, h], n) {
    const px = 2;
    for (let y = y0; y < y0 + h; y += px) {
      for (let x = x0; x < x0 + w; x += px) {
        const c = 0.15 + 0.55 * n(x / 50, y / 50) + 0.3 * ((x - x0) / w);
        if (c * 16 > BAYER[(Math.floor(y / px) % 4) * 4 + (Math.floor(x / px) % 4)]) ctx.fillRect(x, y, px, px);
      }
    }
  },
  digits(ctx, [x0, y0, w, h], n) {
    ctx.font = "500 9px 'JetBrains Mono', monospace";
    ctx.textBaseline = "top";
    for (let y = y0 + 4; y < y0 + h - 8; y += 12) {
      for (let x = x0 + 4; x < x0 + w - 6; x += 9) {
        const c = n(x / 40 + 5, y / 40);
        if (c < 0.3) continue;
        ctx.fillText(c > 0.62 ? "1" : "0", x, y);
      }
    }
  },
};

function paintHalftone(canvas, toneName) {
  const tone = tones[toneName] ?? tones.white;
  const dpr = Math.min(devicePixelRatio || 1, 2);
  const W = canvas.clientWidth, H = canvas.clientHeight;
  if (!W || !H) return false;
  canvas.width = Math.round(W * dpr);
  canvas.height = Math.round(H * dpr);
  const ctx = canvas.getContext("2d");
  ctx.scale(dpr, dpr);
  ctx.fillStyle = PAPER;
  ctx.fillRect(0, 0, W, H);
  const n = noise2(tone.seed);
  for (const p of tone.patches) {
    const r = [Math.round(p.r[0] * W), Math.round(p.r[1] * H), Math.round(p.r[2] * W), Math.round(p.r[3] * H)];
    ctx.save();
    ctx.beginPath();
    ctx.rect(...r);
    ctx.clip();
    ctx.fillStyle = PAPER;
    ctx.fillRect(...r);
    ctx.fillStyle = INK;
    textures[p.t](ctx, r, n, p.from, W, H);
    ctx.restore();
  }
  return true;
}

function halftoneOn(canvas, toneName) {
  let seen = false;
  const paint = () => seen && paintHalftone(canvas, toneName);
  watch(canvas, (v) => {
    if (v && !seen) {
      seen = true;
      paint();
    }
  }, "200px");
  new ResizeObserver(paint).observe(canvas);
  return () => { seen = true; paint(); };
}

const answerCard = $("[data-answers]");
const heroGrid = $(".hero-grid");

// The hero grid fades out under the copy so the headline stays readable.
function drawHeroGrid(note) {
  if (!heroGrid || !answerCard) return;
  const dpr = Math.min(devicePixelRatio || 1, 2);
  const w = heroGrid.clientWidth, h = heroGrid.clientHeight;
  if (!w || !h) return;
  heroGrid.width = Math.round(w * dpr);
  heroGrid.height = Math.round(h * dpr);
  const ctx = heroGrid.getContext("2d");
  ctx.scale(dpr, dpr);
  const box = heroGrid.getBoundingClientRect();
  const rel = (r) => ({ l: r.left - box.left, t: r.top - box.top, r: r.right - box.left, b: r.bottom - box.top });
  const card = rel(answerCard.getBoundingClientRect());
  const copy = rel($(".hero-copy").getBoundingClientRect());
  const narrow = w < 700;
  const nx = card.l + (narrow ? 28 : 56);
  const ny = card.t - (narrow ? 34 : 56);
  const step = narrow ? 24 : 30;
  const reach = narrow ? 300 : 560;
  const outside = (x, y, r, m) => x < r.l - m || x > r.r + m || y < r.t - m || y > r.b + m;
  const copyFade = (x, y) => {
    const dx = Math.max(copy.l - x, 0, x - copy.r);
    const dy = Math.max(copy.t - y, 0, y - copy.b);
    return 0.1 + 0.9 * Math.min(1, Math.hypot(dx, dy) / 110);
  };
  const n = noise2(21);
  const near = [];
  // Keep the note readable: no squares under its two lines of text.
  const label = { l: nx - 18, t: ny - 24, r: nx + (narrow ? 210 : 260), b: ny + 22 };
  for (let y = step / 2; y < h; y += step) {
    for (let x = step / 2; x < w; x += step) {
      if (!outside(x, y, card, 2) || !outside(x, y, label, 4)) continue;
      const d = Math.hypot(x - nx, y - ny) / reach;
      const k = Math.pow(Math.max(0, 1 - d), 2) * (0.5 + 0.5 * n(x / 80, y / 80));
      const s = 1.5 + k * 9;
      const a = (0.1 + k * 0.8) * copyFade(x, y);
      ctx.fillStyle = `rgba(255,255,255,${a.toFixed(3)})`;
      ctx.fillRect(Math.round(x - s / 2), Math.round(y - s / 2), Math.round(s), Math.round(s));
      if (!narrow && d > 0.08 && d < 0.4 && x < nx - 20 && y > 74 && outside(x, y, copy, 30)) near.push([x, y]);
    }
  }
  near.sort((p, q) => n(p[0] / 9, p[1] / 9) - n(q[0] / 9, q[1] / 9));
  ctx.strokeStyle = "rgba(255,255,255,0.4)";
  ctx.lineWidth = 1;
  ctx.beginPath();
  for (const [x, y] of near.slice(0, 7)) {
    ctx.moveTo(nx, ny);
    ctx.lineTo(x, y);
  }
  ctx.stroke();
  const half = narrow ? 9 : 13;
  ctx.fillStyle = "#dff23a";
  ctx.fillRect(nx - half, ny - half, half * 2, half * 2);
  if (note) {
    ctx.font = `500 ${narrow ? 11 : 12}px 'JetBrains Mono', monospace`;
    ctx.textBaseline = "middle";
    ctx.fillStyle = "rgba(255,255,255,0.8)";
    ctx.fillText(note[0], nx + half + 14, ny - (narrow ? 0 : 8));
    if (!narrow) {
      ctx.fillStyle = "rgba(255,255,255,0.45)";
      ctx.fillText(note[1], nx + half + 14, ny + 9);
    }
  }
}

if (answerCard) {
  const tabs = $$("[role=tab]", answerCard);
  const panes = tabs.map((t) => document.getElementById(t.getAttribute("aria-controls")));
  const playBtn = $("[data-play]", answerCard);
  const bar = $(".ac-bar", answerCard);
  const endpointOut = $("[data-endpoint-out]", answerCard);
  const keys = ["chatgpt_app", "google_ai_mode", "claude_code"];
  const parts = panes.map((pane) => ({
    q: $(".ac-q", pane),
    chars: tokenize($(".ac-q", pane), { chars: true }),
    words: tokenize($(".ac-a", pane), { keep: ".bn" }),
    cites: $$(".ac-cites > *", pane),
    rec: $$(".ac-rec > div", pane),
  }));
  let current = 0;
  let paused = false;

  const note = (k) => [keys[k], panes[k].dataset.fidelity];
  const show = (k) => {
    current = k;
    tabs.forEach((t, j) => {
      t.setAttribute("aria-selected", String(j === k));
      t.tabIndex = j === k ? 0 : -1;
      panes[j].hidden = j !== k;
    });
    endpointOut.textContent = panes[k].dataset.endpoint;
    drawHeroGrid(note(k));
  };

  const player = timeline({
    onFrame: (t, end) => bar.style.setProperty("--p", (t / end).toFixed(4)),
    onEnd: () => start((current + 1) % panes.length),
  });

  function start(k) {
    show(k);
    const p = parts[k];
    const pane = panes[k];
    pane.classList.add("anim");
    for (const el of [...p.chars, ...p.words, ...p.cites, ...p.rec]) el.classList.remove("on", "lit");
    const ev = [];
    let t = 200;
    ev.push({ at: 0, run: () => p.q.classList.add("typing") });
    for (const c of p.chars) ev.push({ at: (t += 20), run: () => c.classList.add("on") });
    ev.push({ at: (t += 300), run: () => p.q.classList.remove("typing") });
    for (const w of p.words) {
      ev.push({ at: (t += 32), run: () => w.classList.add("on") });
      if (w.classList.contains("bn")) ev.push({ at: t + 260, run: () => w.classList.add("lit") });
    }
    t += 420;
    for (const c of p.cites) ev.push({ at: (t += 200), run: () => c.classList.add("on") });
    t += 200;
    for (const r of p.rec) ev.push({ at: (t += 180), run: () => r.classList.add("on") });
    player.load(ev, t + 3600);
    if (!paused) player.play();
  }

  // A finished pane holds for the same beat a played one ends on, so
  // resuming after a paused click moves on at the usual pace.
  const finish = (k) => {
    show(k);
    panes[k].classList.remove("anim");
    parts[k].q.classList.remove("typing");
    for (const w of parts[k].words) if (w.classList.contains("bn")) w.classList.add("lit");
    player.load([], 3600);
  };

  const choose = (k) => (still || paused ? finish(k) : start(k));

  tabs.forEach((tab, k) => {
    tab.addEventListener("click", () => choose(k));
    tab.addEventListener("keydown", (e) => {
      const to = { ArrowRight: k + 1, ArrowLeft: k - 1, Home: 0, End: tabs.length - 1 }[e.key];
      if (to === undefined) return;
      e.preventDefault();
      const next = (to + tabs.length) % tabs.length;
      tabs[next].focus();
      choose(next);
    });
  });

  if (still) {
    parts.forEach((_, k) => finish(k));
    finish(0);
  } else {
    playBtn.hidden = false;
    playBtn.addEventListener("click", () => {
      paused = !paused;
      playBtn.toggleAttribute("data-paused", paused);
      playBtn.setAttribute("aria-label", paused ? "Play" : "Pause");
      paused ? player.pause() : player.play();
    });
    watch(answerCard, (v) => player.setVisible(v));
    start(0);
  }
  new ResizeObserver(() => drawHeroGrid(note(current))).observe(heroGrid);
  document.fonts?.ready.then(() => drawHeroGrid(note(current)));
}

// Check-by-check cells from the example instance: a digit is the position
// when the answer named the brand, c cited it without naming it, - neither,
// x the call failed. Rows are questions; columns are surface and model pairs.
const checks = [
  { cells: "122211|---c-2|-32-41|c-c-33|-----1|cc----", answered: 36, cited: 11 },
  { cells: "122121|-23--c|1-15c1|-44142|--1c1-|-----c", answered: 36, cited: 16 },
  { cells: "121221|xx22c-|11-24c|--153-|3113--|-cc---", answered: 34, cited: 13 },
  { cells: "121112|-311-3|134333|2-35c-|3221-5|--c--c", answered: 36, cited: 22 },
  { cells: "111212|-42143|52c2-2|-421-1|422423|--c-c-", answered: 36, cited: 15 },
  { cells: "122111|-22443|243232|415442|314233|c-----", answered: 36, cited: 15 },
  { cells: "222112|31313-|c1-11-|3331-4|312-22|------", answered: 36, cited: 16 },
  { cells: "222211|412112|43132-|111232|113523|-c-c--", answered: 36, cited: 19 },
].map((c) => {
  const flat = c.cells.replace(/\|/g, "");
  return { ...c, flat, named: [...flat].filter((ch) => /\d/.test(ch)).length };
});

const matrix = $("[data-matrix]");
if (matrix) {
  const cells = $$(".mx-c", matrix);
  const cols = $$(".mx-col", matrix);
  const rowCounts = $$(".mx-row:not(.mx-head) .mx-n", matrix);
  const hist = $$(".mx-h", matrix);
  const out = {
    check: $("[data-mx-check]", matrix),
    status: $("[data-mx-status]", matrix),
    named: $("[data-mx-named]", matrix),
    answered: $("[data-mx-answered]", matrix),
    delta: $("[data-mx-delta]", matrix),
    rate: $("[data-mx-rate]", matrix),
    cite: $("[data-mx-cite]", matrix),
    gap: $("[data-mx-gap]", matrix),
    replay: $("[data-mx-replay]", matrix),
    stats: $(".mx-stats", matrix),
  };
  const pct = (a, b) => Math.round((a / b) * 100);
  let shown = checks.length - 1;

  const paint = (cell, ch) => {
    cell.className = "mx-c " + (/\d/.test(ch) ? "is-named" : ch === "c" ? "is-cited" : ch === "x" ? "is-failed" : "is-miss");
    const label = /\d/.test(ch) ? `<i>#${ch}</i><span class="sr-only"> named</span>` : `<span class="sr-only">${ch === "c" ? "Cited, not named" : ch === "x" ? "Call failed" : "Not named"}</span>`;
    cell.innerHTML = label;
  };

  const summary = (k) => {
    const c = checks[k];
    const prev = checks[k - 1];
    out.check.textContent = `Check #${k + 1}`;
    out.status.textContent = `Check #${k + 1} · ${c.answered} of 36 answered`;
    out.named.textContent = c.named;
    out.answered.textContent = c.answered;
    out.rate.textContent = `${pct(c.named, c.answered)}%`;
    out.cite.textContent = `${pct(c.cited, c.answered)}%`;
    out.delta.classList.remove("down", "flat");
    if (prev) {
      const d = Math.round(((c.named / c.answered) - (prev.named / prev.answered)) * 100);
      out.delta.textContent = d === 0 ? `No change since check #${k}` : `${d > 0 ? "+" : ""}${d} pts since check #${k}`;
      if (d < 0) out.delta.classList.add("down");
      if (d === 0) out.delta.classList.add("flat");
    } else {
      out.delta.textContent = "The first check";
      out.delta.classList.add("flat");
    }
    c.cells.split("|").forEach((row, r) => {
      rowCounts[r].innerHTML = `${[...row].filter((ch) => /\d/.test(ch)).length}<small>/6</small>`;
    });
    out.gap.hidden = k !== checks.length - 1;
    out.replay.textContent = `Run check #${k + 1} again`;
    hist.forEach((b, j) => b.setAttribute("aria-pressed", String(j === k)));
  };

  const setCheck = (k) => {
    shown = k;
    checks[k].flat.split("").forEach((ch, i) => paint(cells[i], ch));
    summary(k);
  };

  const player = timeline();
  // Asks one column, one surface and model pair, at a time, as a check does.
  function scan(k) {
    shown = k;
    const c = checks[k];
    summary(k);
    hist.forEach((b, j) => b.setAttribute("aria-pressed", String(j === k)));
    for (const cell of cells) {
      cell.className = "mx-c is-pending";
      cell.innerHTML = "";
    }
    rowCounts.forEach((el) => (el.innerHTML = `0<small>/6</small>`));
    out.delta.style.opacity = "0";
    out.gap.style.opacity = "0";
    out.stats.style.opacity = "0";
    out.named.textContent = "0";
    const ev = [];
    let t = 150;
    let named = 0;
    let done = 0;
    const perRow = [0, 0, 0, 0, 0, 0];
    for (let j = 0; j < 6; j++) {
      ev.push({ at: t, run: () => {
        cols.forEach((col, x) => col.classList.toggle("is-asking", x === j));
        out.status.textContent = `Asking ${cols[j].dataset.name}`;
      } });
      for (let r = 0; r < 6; r++) {
        const i = r * 6 + j;
        const ch = c.flat[i];
        ev.push({ at: (t += 70), run: () => {
          paint(cells[i], ch);
          done++;
          if (/\d/.test(ch)) {
            named++;
            perRow[r]++;
            out.named.textContent = named;
            rowCounts[r].innerHTML = `${perRow[r]}<small>/6</small>`;
          }
        } });
      }
      t += 140;
    }
    ev.push({ at: t, run: () => {
      cols.forEach((col) => col.classList.remove("is-asking"));
      out.status.textContent = `Check #${k + 1} · ${c.answered} of 36 answered`;
      out.delta.style.opacity = "";
      out.stats.style.opacity = "";
    } });
    ev.push({ at: t + 400, run: () => (out.gap.style.opacity = "") });
    player.load(ev, t + 400);
    player.play();
  }

  hist.forEach((b, k) => b.addEventListener("click", () => {
    player.pause();
    cols.forEach((col) => col.classList.remove("is-asking"));
    out.delta.style.opacity = "";
    out.gap.style.opacity = "";
    out.stats.style.opacity = "";
    setCheck(k);
  }));
  out.replay.addEventListener("click", () => (still ? setCheck(shown) : scan(shown)));

  if (!still) {
    let started = false;
    watch(matrix, (v) => {
      player.setVisible(v);
      if (v && !started) {
        started = true;
        scan(checks.length - 1);
      }
    }, "-20% 0px");
  }
}

const tabRoot = $("[data-tabs]");
if (tabRoot) {
  const tabs = $$("[role=tab]", tabRoot);
  const panels = tabs.map((t) => document.getElementById(t.getAttribute("aria-controls")));
  const painters = panels.map((p) => {
    const art = $(".tp-art", p);
    return halftoneOn($(".halftone", p), art.dataset.tone);
  });
  const select = (k, focus) => {
    tabs.forEach((t, j) => {
      const on = j === k;
      t.setAttribute("aria-selected", String(on));
      t.tabIndex = on ? 0 : -1;
      panels[j].hidden = !on;
    });
    painters[k]();
    if (focus) tabs[k].focus();
  };
  tabs.forEach((tab, k) => {
    tab.addEventListener("click", () => select(k));
    tab.addEventListener("keydown", (e) => {
      const to = { ArrowRight: k + 1, ArrowLeft: k - 1, Home: 0, End: tabs.length - 1 }[e.key];
      if (to === undefined) return;
      e.preventDefault();
      select((to + tabs.length) % tabs.length, true);
    });
  });
}

for (const c of $$(".dp-art .halftone")) halftoneOn(c, c.dataset.tone);

const field = $(".fid-field");
if (field) {
  const paint = () => {
    const dpr = Math.min(devicePixelRatio || 1, 2);
    const w = field.clientWidth, h = field.clientHeight;
    if (!w || !h) return;
    field.width = Math.round(w * dpr);
    field.height = Math.round(h * dpr);
    const ctx = field.getContext("2d");
    ctx.scale(dpr, dpr);
    const n = noise2(17);
    const cell = 8;
    ctx.fillStyle = "#2c2c36";
    ctx.beginPath();
    // Coverage thins from left to right, as fidelity does.
    for (let y = cell / 2; y < h; y += cell) {
      for (let x = cell / 2; x < w; x += cell) {
        const u = x / w;
        const c = Math.pow(1 - u, 1.6) * (0.55 + 0.45 * n(x / 60, y / 60));
        const r = (cell / 2) * Math.sqrt(c);
        if (r < 0.4) continue;
        ctx.moveTo(x + r, y);
        ctx.arc(x, y, r, 0, Math.PI * 2);
      }
    }
    ctx.fill();
  };
  new ResizeObserver(paint).observe(field);
}

const term = $("[data-term]");
if (term && !still) {
  const lines = $$(".term-body > p", term);
  const user = lines[0];
  const chars = tokenize(user, { chars: true });
  const player = timeline();
  term.classList.add("anim");
  const ev = [];
  let t = 300;
  ev.push({ at: t, run: () => user.classList.add("on") });
  for (const c of chars) ev.push({ at: (t += 24), run: () => c.classList.add("on") });
  t += 500;
  for (const line of lines.slice(1)) {
    const pause = line.classList.contains("t-call") ? 700 : line.classList.contains("t-res") ? 500 : 260;
    ev.push({ at: (t += pause), run: () => line.classList.add("on") });
  }
  player.load(ev, t);
  let started = false;
  watch(term, (v) => {
    if (v && !started) {
      started = true;
      player.play();
    }
    player.setVisible(v);
  }, "-25% 0px");
}
