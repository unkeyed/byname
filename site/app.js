// The landing page: hand-drawn marks, the reveal as sections scroll in, the
// hero's annotated answer, the ledger's margin notes and the copy button.
// Marks draw once as they enter the screen. The hero cycles only while it is
// on screen, and a visitor who prefers reduced motion sees every end state.
const reduce = matchMedia("(prefers-reduced-motion: reduce)").matches;
const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];
document.documentElement.classList.replace("no-js", "js");

// Marks: an SVG stroke over each .mk, shaped to its box and seeded from its
// text, so a mark looks the same on every load and redraws to fit on resize.
const Marks = (() => {
  const NS = "http://www.w3.org/2000/svg";
  const seeded = (str) => {
    let h = 2166136261;
    for (const c of str) h = Math.imul(h ^ c.charCodeAt(0), 16777619);
    return () => ((h = Math.imul(h ^ (h >>> 15), 2246822507) ^ Math.imul(h ^ (h >>> 13), 3266489909)) >>> 0) / 4294967296;
  };
  // A Catmull-Rom curve through the points, as cubic Bezier segments.
  const smooth = (pts) => {
    const f = (n) => n.toFixed(1);
    let d = `M${f(pts[0][0])} ${f(pts[0][1])}`;
    for (let i = 0; i < pts.length - 1; i++) {
      const p0 = pts[i - 1] || pts[i], p1 = pts[i], p2 = pts[i + 1], p3 = pts[i + 2] || p2;
      d += `C${f(p1[0] + (p2[0] - p0[0]) / 6)} ${f(p1[1] + (p2[1] - p0[1]) / 6)} ${f(p2[0] - (p3[0] - p1[0]) / 6)} ${f(p2[1] - (p3[1] - p1[1]) / 6)} ${f(p2[0])} ${f(p2[1])}`;
    }
    return d;
  };
  const shapes = {
    // A pen circle: starts upper left, overshoots its start, drifts inward.
    circle(w, h, r) {
      const cx = w / 2, cy = h / 2, rx = w / 2, ry = h / 2;
      const start = -2.35 + r() * 0.5, sweep = Math.PI * 2 * (1.04 + r() * 0.05);
      const p1 = r() * 6, p2 = r() * 6, drift = 0.03 + r() * 0.04, tilt = (r() - 0.5) * 0.08;
      const pts = [];
      for (let i = 0; i <= 36; i++) {
        const t = i / 36, a = start + sweep * t;
        const k = 1 + 0.025 * Math.sin(2 * a + p1) + 0.018 * Math.sin(3 * a + p2) - drift * t;
        const x = Math.cos(a) * rx * k, y = Math.sin(a) * ry * k;
        pts.push([cx + x - y * tilt, cy + y + x * tilt]);
      }
      return smooth(pts);
    },
    underline(w, h, r) {
      const sag = (r() - 0.3) * 3, pts = [];
      for (let i = 0; i <= 8; i++) {
        const t = i / 8;
        pts.push([-2 + (w + 5) * t, h + 1 + Math.sin(t * Math.PI) * sag + (r() - 0.5) * 0.8 - t * 1.4]);
      }
      return smooth(pts);
    },
    double(w, h, r) {
      return `${shapes.underline(w, h - 1, r)} ${shapes.underline(w * 0.9, h + 4, r).replace(/^M(-?[\d.]+)/, (m, x) => `M${(Number(x) + w * 0.06).toFixed(1)}`)}`;
    },
    dash(w, h, r) {
      return smooth([[w * 0.2, h * 0.58 + r() * 2], [w * 0.5, h * 0.5], [w * 0.8, h * 0.4 + r() * 2]]);
    },
    tick(w, h, r) {
      const y = h * 0.88;
      return smooth([[w * 0.2, y - 1 + r()], [w * 0.5, y + 1], [w * 0.82, y - 2 + r()]]);
    },
    caret(w, h, r) {
      return `M${(r() * 1.5).toFixed(1)} ${h.toFixed(1)}L${(w / 2 + r() - 0.5).toFixed(1)} ${(r() * 1.5).toFixed(1)}L${w.toFixed(1)} ${(h - r() * 1.5).toFixed(1)}`;
    },
  };
  const pads = { circle: [10, 6], underline: [0, 0], double: [0, 0], dash: [0, 0], tick: [0, 0], caret: [0, 0] };

  function draw(el) {
    const kind = el.dataset.mark || "circle";
    el.querySelector(":scope > svg.mk-svg")?.remove();
    const w = el.offsetWidth, h = el.offsetHeight;
    if (!w || !h) return;
    // data-pad is "x y" in px, or in em ("0.3em 0.1em") to scale with the text.
    const em = parseFloat(getComputedStyle(el).fontSize);
    const [px, py] = el.dataset.pad
      ? el.dataset.pad.split(" ").map((v) => (v.endsWith("em") ? parseFloat(v) * em : Number(v)))
      : el.classList.contains("tm") ? [3, 3] : pads[kind];
    if (el.dataset.sw) el.style.setProperty("--sw", el.dataset.sw);
    const svg = document.createElementNS(NS, "svg");
    svg.setAttribute("class", "mk-svg");
    svg.setAttribute("aria-hidden", "true");
    svg.setAttribute("width", w + px * 2 + 12);
    svg.setAttribute("height", h + py * 2 + 12);
    svg.style.left = `${-px - 6}px`;
    svg.style.top = `${-py - 6}px`;
    const path = document.createElementNS(NS, "path");
    path.setAttribute("d", shapes[kind](w + px * 2, h + py * 2, seeded(el.textContent + kind + w)));
    path.setAttribute("transform", "translate(6 6)");
    path.setAttribute("pathLength", "1");
    svg.append(path);
    el.append(svg);
  }
  return { draw, drawAll: (root = document) => $$(".mk", root).forEach(draw) };
})();

// Reveal: a [data-reveal] block, or a lone mark, gets .on the first time it
// is well inside the screen. Marks inside a block draw one after another.
function stagger(block) {
  const step = Number(block.dataset.stagger || 110);
  if (block.classList.contains("transcript")) {
    $$(".t-body > *", block).forEach((line, i) => {
      line.style.setProperty("--d", `${i * 0.35}s`);
      $$(".mk", line).forEach((m) => m.style.setProperty("--d", `${i * 0.35 + 0.45}s`));
    });
    return;
  }
  $$(".mk, path.draw", block).forEach((el, i) => {
    if (!el.style.getPropertyValue("--d")) el.style.setProperty("--d", `${Math.min(i * step, 1800) / 1000}s`);
  });
}
{
  const blocks = $$("[data-reveal]");
  const lone = $$(".mk").filter((m) => !m.closest("[data-reveal], [data-hero], .asked, .mast, .foot, .hero h1"));
  blocks.forEach(stagger);
  if (reduce || !("IntersectionObserver" in window)) {
    [...blocks, ...lone].forEach((el) => el.classList.add("on"));
  } else {
    const io = new IntersectionObserver((entries) => {
      for (const e of entries) {
        if (!e.isIntersecting) continue;
        e.target.classList.add("on");
        io.unobserve(e.target);
      }
    }, { rootMargin: "0px 0px -18% 0px", threshold: 0.15 });
    [...blocks, ...lone].forEach((el) => io.observe(el));
  }
  // The logo and the headline draw as the page opens.
  setTimeout(() => $$(".mast .mk, .foot .mk, .hero h1 .mk").forEach((m) => m.classList.add("on")), reduce ? 0 : 450);
}

// The hero: an answer streams in word by word, then Byname marks it up. It
// moves to the next surface while on screen, stops for good when the visitor
// picks a surface, and holds while they scroll away or switch tabs.
const Hero = (() => {
  const root = $("[data-hero]");
  if (!root) return null;
  const panes = $$(".pane", root), tabs = $$(".asked [role=tab]");
  const stamp = $(".stamp", root);
  let current = 0, auto = !reduce, onScreen = true, timers = [];
  const at = (ms, fn) => timers.push(setTimeout(fn, ms));
  const clear = () => { timers.forEach(clearTimeout); timers = []; };

  // Words become spans so they can appear one at a time without reflowing.
  for (const ans of $$(".ans", root)) {
    const walker = document.createTreeWalker(ans, NodeFilter.SHOW_TEXT);
    const nodes = [];
    while (walker.nextNode()) if (walker.currentNode.textContent.trim()) nodes.push(walker.currentNode);
    for (const node of nodes) {
      if (node.parentElement.closest(".note")) continue;
      const frag = document.createDocumentFragment();
      for (const part of node.textContent.split(/(\s+)/)) {
        if (!part) continue;
        if (/^\s+$/.test(part)) frag.append(part);
        else { const w = document.createElement("span"); w.className = "w"; w.textContent = part; frag.append(w); }
      }
      node.replaceWith(frag);
    }
  }
  for (const tab of tabs) tab.innerHTML = `<span class="mk" data-mark="underline">${tab.textContent}</span>`;

  function show(i, animate) {
    clear();
    current = i;
    panes.forEach((p, k) => { p.hidden = k !== i; });
    tabs.forEach((t, k) => {
      t.setAttribute("aria-selected", String(k === i));
      t.tabIndex = k === i ? 0 : -1;
      $(".mk", t).classList.toggle("on", k === i);
    });
    const pane = panes[i];
    stamp.textContent = pane.dataset.stamp;
    stamp.className = `stamp${pane.dataset.stampTone === "instead" ? " instead" : ""}`;
    Marks.drawAll(pane);
    const words = $$(".w", pane), notes = $$(".note", pane);
    const marks = $$(".ans .mk", pane).sort((a, b) => (b.dataset.mark === "circle") - (a.dataset.mark === "circle"));
    if (!animate) {
      pane.classList.remove("streaming");
      [...words, ...notes, ...marks].forEach((e) => e.classList.add("on"));
      stamp.classList.remove("pop");
      return;
    }
    [...words, ...notes, ...marks].forEach((e) => e.classList.remove("on"));
    pane.classList.add("streaming");
    stamp.classList.add("pop");
    words.forEach((w, k) => at(120 + k * 30, () => w.classList.add("on")));
    let t = 260 + words.length * 30;
    marks.forEach((m, k) => {
      at(t + k * 280, () => m.classList.add("on"));
      at(t + k * 280 + 420, () => m.closest("li").querySelector(".note").classList.add("on"));
    });
    t += marks.length * 280 + 700;
    at(t, () => stamp.classList.remove("pop"));
    if (auto && onScreen) at(t + 4200, () => show((i + 1) % panes.length, true));
  }

  tabs.forEach((tab, k) => {
    tab.addEventListener("click", () => { auto = false; show(k, !reduce); });
    tab.addEventListener("keydown", (e) => {
      const to = { ArrowRight: k + 1, ArrowLeft: k - 1, Home: 0, End: tabs.length - 1 }[e.key];
      if (to === undefined) return;
      e.preventDefault();
      const next = (to + tabs.length) % tabs.length;
      tabs[next].focus();
      tabs[next].click();
    });
  });
  if (!reduce) {
    const pauseFor = (away) => {
      if (onScreen === !away) return;
      onScreen = !away;
      if (away) show(current, false);
      else if (auto) { clear(); at(900, () => show((current + 1) % panes.length, true)); }
    };
    new IntersectionObserver(([e]) => pauseFor(!e.isIntersecting), { threshold: 0.2 }).observe(root);
    document.addEventListener("visibilitychange", () => pauseFor(document.hidden));
  }
  show(0, !reduce);
  // Fonts or a new width change the text's boxes, so the marks are redrawn
  // in place, keeping how far the pane has got.
  return { redraw: () => Marks.drawAll(panes[current]) };
})();

// Ledger notes sit in the margin beside the row they speak about.
function placeNotes() {
  const box = $(".ledger-notes");
  if (!box) return;
  const beside = getComputedStyle($(".lnote", box)).position === "absolute";
  for (const note of $$(".lnote", box)) {
    const row = $(`.ltable tr[data-check="${note.dataset.for}"]`);
    note.style.top = beside ? `${row.getBoundingClientRect().top - box.getBoundingClientRect().top + row.offsetHeight / 2}px` : "";
  }
}

function redraw() {
  $$(".mk").filter((m) => !m.closest("[data-hero]")).forEach(Marks.draw);
  Hero?.redraw();
  placeNotes();
}
redraw();
document.fonts?.ready.then(redraw);
{
  let width = innerWidth, t;
  addEventListener("resize", () => {
    if (innerWidth === width) return;
    width = innerWidth;
    clearTimeout(t);
    t = setTimeout(redraw, 160);
  });
}

// The masthead gets a rule once the page scrolls, and the phone menu closes
// when one of its links is followed.
if ($(".mast")) {
  const mast = $(".mast");
  const onScroll = () => mast.classList.toggle("scrolled", scrollY > 8);
  addEventListener("scroll", onScroll, { passive: true });
  onScroll();
  const menu = $(".mast-menu");
  $$("a", menu).forEach((a) => a.addEventListener("click", () => { menu.open = false; }));
  document.addEventListener("keydown", (e) => { if (e.key === "Escape" && menu.open) { menu.open = false; $("summary", menu).focus(); } });
}

// Copy the deploy prompt.
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
    document.body.append(area);
    area.select();
    const ok = document.execCommand("copy");
    area.remove();
    return ok;
  }
}
if ($("#agent-prompt")) {
  const source = $("#agent-prompt"), toast = $(".toast");
  const buttons = $$("[data-copy-prompt]");
  let t;
  for (const button of buttons) {
    button.addEventListener("click", async () => {
      const ok = await copyText(source.textContent);
      button.querySelector(".copy-state").textContent = ok ? "Copied" : "Copy failed";
      toast.textContent = ok ? "Prompt copied. Paste it into your coding agent to deploy on Unkey." : "Could not copy. Select the prompt and copy it by hand.";
      toast.classList.add("show");
      clearTimeout(t);
      t = setTimeout(() => {
        toast.classList.remove("show");
        for (const b of buttons) b.querySelector(".copy-state").textContent = b.dataset.label;
      }, 2600);
    });
  }
}
