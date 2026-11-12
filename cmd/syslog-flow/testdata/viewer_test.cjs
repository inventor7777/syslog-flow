const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");

// Run the actual viewer script with a small DOM stand-in and controlled polls.
const source = fs.readFileSync("web.go", "utf8");
const script = source.slice(source.lastIndexOf("  <script>\n") + "  <script>\n".length,
  source.lastIndexOf("  </script>"));

class Element {
  constructor() {
    this.children = [];
    this.dataset = {};
    this.style = {};
    this.listeners = {};
  }
  appendChild(child) {
    if (child.fragment) {
      for (const node of [...child.children]) this.appendChild(node);
    } else {
      child.parent = this;
      this.children.push(child);
    }
  }
  replaceChildren(fragment) {
    this.children = [];
    this.appendChild(fragment);
  }
  insertBefore(fragment) {
    const previous = this.children;
    this.children = [];
    this.appendChild(fragment);
    this.children.push(...previous);
  }
  remove() {
    this.parent.children.splice(this.parent.children.indexOf(this), 1);
  }
  querySelectorAll() { return [...this.children]; }
  contains(node) { return node === this || this.children.some(child => child === node || child.contains?.(node)); }
  addEventListener(name, callback) { this.listeners[name] = callback; }
  get scrollHeight() { return this.children.length * 20; }
  get clientHeight() { return this.height ?? 100; }
  getBoundingClientRect() { return { top: 0, height: this.clientHeight }; }
}

function viewerContext(day, layout, target) {
  const viewer = new Element();
  viewer.scrollTop = 0;
  viewer.dataset.refreshUrl = day ? "/day/2026/10/20?partial=logs&format=json" : "/?partial=logs&format=json";
  if (day) Object.assign(viewer.dataset, {
    olderUrl: viewer.dataset.refreshUrl, refreshCursor: "10", start: "0", hasOlder: "false",
  });
  const timers = [];
  const timeouts = [];
  const requests = [];
  let reloads = 0;
  let payload;
  const document = {
    body: { dataset: { generation: "generation-1" } },
    querySelectorAll: () => [],
    querySelector: () => null,
    getElementById: id => id === "log-viewer" ? viewer : id === "log-target" ? target : null,
    createElement: () => new Element(),
    createTextNode: text => ({ textContent: text }),
    createDocumentFragment: () => Object.assign(new Element(), { fragment: true }),
  };
  const context = vm.createContext({
    document, URL,
    window: { location: { origin: "http://localhost", reload: () => { reloads++; } }, getSelection: () => null },
    requestAnimationFrame: callback => callback(),
    setInterval: callback => timers.push(callback),
    setTimeout: (callback, delay) => timeouts.push({ callback, delay }),
    fetch: async url => {
      requests.push(String(url));
      return { ok: true, json: async () => payload };
    },
  });
  if (layout) {
    viewer.height = layout.viewerHeight;
    const classes = new Set();
    const panel = {
      querySelectorAll: () => layout.filters,
      classList: {
        contains: name => classes.has(name),
        toggle: (name, enabled) => enabled ? classes.add(name) : classes.delete(name),
      },
    };
    viewer.closest = () => panel;
    context.ResizeObserver = class {
      constructor(callback) { layout.resize = callback; }
      observe() {}
    };
    layout.compact = () => classes.has("compact-filters");
  }
  vm.runInContext(script, context);
  return {
    viewer, requests, context, timeouts,
    get reloads() { return reloads; },
    async poll(value, atBottom = true) {
      payload = value;
      if (atBottom) viewer.scrollTop = Math.max(0, viewer.scrollHeight - viewer.clientHeight);
      await timers[0]();
    },
    async pollStats(value) {
      payload = value;
      await timers[1]();
    },
    async older(value) {
      payload = value;
      viewer.scrollTop = 0;
      viewer.listeners.scroll();
      // The scroll listener starts an async pagination request.
      await new Promise(resolve => setImmediate(resolve));
    },
  };
}

async function main() {
  const target = new Element();
  let highlighted = true;
  target.classList = { remove: name => { if (name === "log-target-highlight") highlighted = false; } };
  target.getBoundingClientRect = () => ({ top: 500, height: 20 });
  const targeted = viewerContext(true, undefined, target);
  assert.equal(targeted.viewer.scrollTop, 460, "source links must center the highlighted entry inside the log viewer");
  assert.equal(highlighted, true, "highlight must initially identify the destination line");
  assert.equal(targeted.timeouts[0].delay, 1000);
  targeted.timeouts[0].callback();
  assert.equal(highlighted, false, "destination highlight must clear automatically");
  highlighted = true;
  target.listeners.click();
  assert.equal(highlighted, false, "clicking the destination line must also clear its highlight");

  const layout = { viewerHeight: 180, filters: [{ scrollHeight: 280, clientHeight: 280 }] };
  const fitted = viewerContext(true, layout);
  assert.equal(layout.compact(), true, "pills must compact when the viewer is too short");
  layout.filters[0].clientHeight = 90;
  fitted.viewer.height = 370;
  layout.resize();
  assert.equal(layout.compact(), true, "reclaimed space must not cause an expand/collapse loop");
  fitted.viewer.height = 515;
  layout.resize();
  assert.equal(layout.compact(), true, "leave space above the collapse threshold to prevent boundary jitter");
  fitted.viewer.height = 550;
  layout.resize();
  assert.equal(layout.compact(), false, "pills should expand when enough viewer space remains");
  layout.filters[0].clientHeight = 280;
  fitted.viewer.height = 360;
  layout.resize();
  assert.equal(layout.compact(), false, "expanded layout must stay stable");
  fitted.viewer.height = 150;
  layout.resize();
  assert.equal(layout.compact(), true, "other controls growing must compact the pills again");
  layout.filters[0].scrollHeight = 90;
  layout.filters[0].clientHeight = 90;
  fitted.viewer.height = 360;
  layout.resize();
  assert.equal(layout.compact(), false, "wider layouts with fewer pill rows should expand again");

  const home = viewerContext(false);
  await home.poll({ lines: ["first log"] });
  assert.equal(home.viewer.children.length, 1, "an initially empty homepage must receive logs");

  for (const isDay of [false, true]) {
    const reading = viewerContext(isDay);
    const logs = Array.from({ length: 60 }, (_, i) => "log " + i);
    await reading.poll({ lines: logs.slice(0, 40), cursor: 50 });
    reading.viewer.scrollTop = 200;
    const entry = reading.viewer.children[10];
    await reading.poll({ lines: logs.slice(isDay ? 40 : 20), cursor: 70 }, false);
    assert.equal(reading.requests.length, 2, "scrolling up must not stop polling");
    assert.equal(reading.viewer.scrollTop, 200, "background updates must preserve the reading position");
    assert.equal(reading.viewer.children[10], entry, "background updates must preserve the entry being read");
    assert.equal(reading.viewer.children.length, 60, "new entries must append without duplicating overlapping logs");
    await reading.poll({ lines: isDay ? [] : logs.slice(20), cursor: 70 }, false);
    assert.equal(reading.viewer.children.length, 60, "repeated polls must not duplicate entries");
    if (isDay) {
      assert.equal(new URL(reading.requests.at(-1), "http://localhost").searchParams.get("since"), "70",
        "background polls must advance the cursor");
    } else {
      await reading.poll({ lines: ["new snapshot without overlap"] }, false);
      assert.equal(reading.viewer.children[10], entry, "a rolling homepage snapshot must not replace the entry being read");
    }
    await reading.poll({ lines: isDay ? ["latest"] : ["new snapshot without overlap", "latest"], cursor: 71 });
    assert.equal(reading.viewer.scrollTop, reading.viewer.scrollHeight, "following the bottom must resume automatic scrolling");

    const slowRead = viewerContext(isDay);
    await slowRead.poll({ lines: logs, cursor: 70 });
    let finish;
    slowRead.context.fetch = () => new Promise(resolve => { finish = resolve; });
    const pending = slowRead.poll();
    slowRead.viewer.scrollTop = 100;
    finish({ ok: true, json: async () => ({ lines: isDay ? ["latest"] : [...logs, "latest"], cursor: 71 }) });
    await pending;
    assert.equal(slowRead.viewer.scrollTop, 100, "scrolling up during a request must also preserve position");
  }

  for (const isDay of [false, true]) {
    const selecting = viewerContext(isDay);
    const logs = Array.from({ length: 5020 }, (_, i) => "log " + i);
    await selecting.poll({ lines: logs.slice(0, 5000), cursor: 5010 });
    const selected = selecting.viewer.children[0];
    const selection = { rangeCount: 1, isCollapsed: false, getRangeAt: () => ({ commonAncestorContainer: selected }) };
    selecting.context.window.getSelection = () => selection;
    await selecting.poll({ lines: logs.slice(isDay ? 5000 : 20), cursor: 5030 });
    assert.equal(selecting.requests.length, 2, "text selection must not stop polling");
    assert.equal(selecting.viewer.scrollTop, 99900, "selected text must prevent automatic scrolling even at the bottom");
    assert.equal(selecting.viewer.children[0], selected, "selected nodes must not be removed by trimming or rolling snapshots");
    assert.equal(selecting.viewer.children.length, 5020, "new entries must append while text is selected");
    await selecting.poll({ lines: isDay ? [] : logs.slice(20), cursor: 5030 }, false);
    assert.equal(selecting.viewer.children.length, 5020, "selection must not cause duplicate entries");
    if (isDay) {
      assert.equal(new URL(selecting.requests.at(-1), "http://localhost").searchParams.get("since"), "5030",
        "polling during selection must advance the cursor");
    }
    selection.isCollapsed = true;
    await selecting.poll({ lines: isDay ? [] : logs.slice(20), cursor: 5030 });
    assert.equal(selecting.viewer.children.length, 5000, "clearing selection and returning to the bottom must restore trimming");
    assert.equal(selecting.viewer.scrollTop, selecting.viewer.scrollHeight);

    const slowSelection = viewerContext(isDay);
    await slowSelection.poll({ lines: logs.slice(0, 40), cursor: 50 });
    let finish;
    slowSelection.context.fetch = () => new Promise(resolve => { finish = resolve; });
    const pending = slowSelection.poll();
    slowSelection.context.window.getSelection = () => ({
      rangeCount: 1, isCollapsed: false,
      getRangeAt: () => ({ commonAncestorContainer: slowSelection.viewer.children[0] }),
    });
    finish({ ok: true, json: async () => ({ lines: isDay ? ["latest"] : [...logs.slice(20, 40), "latest"], cursor: 51 }) });
    await pending;
    assert.equal(slowSelection.viewer.scrollTop, 700, "selection started during a request must also prevent scrolling");
    assert.equal(slowSelection.viewer.children.length, 41, "selection started during a request must preserve existing entries");
  }

  for (const isDay of [false, true]) {
    const slow = viewerContext(isDay);
    const normalFetch = slow.context.fetch;
    let finish;
    slow.context.fetch = url => {
      slow.requests.push(String(url));
      return new Promise(resolve => { finish = resolve; });
    };
    const pending = slow.poll();
    await slow.poll();
    assert.equal(slow.requests.length, 1, "slow log polls must not overlap");
    finish({ ok: true, json: async () => ({ lines: ["one event"], cursor: 11 }) });
    await pending;
    assert.equal(slow.viewer.children.length, 1, "a slow response must append an event only once");
    slow.context.fetch = async () => { throw new Error("network failure"); };
    await slow.poll();
    slow.context.fetch = normalFetch;
    await slow.poll({ lines: isDay ? ["next event"] : ["one event", "next event"], cursor: 12 });
    assert.equal(slow.viewer.children.length, 2, "polling must resume after success and failure");
  }

  const day = viewerContext(true);
  await day.poll({ lines: Array.from({ length: 6000 }, (_, i) => "log " + i), cursor: 6010 });
  assert.equal(day.viewer.children.length, 5000, "live rows must be bounded");
  assert.equal(day.viewer.children[0].dataset.raw, "log 1000");
  await day.poll({ lines: ["next log"], cursor: 6011 });
  assert.equal(day.viewer.children.length, 5000);
  assert.equal(day.viewer.children[0].dataset.raw, "log 1001");
  day.viewer.scrollTop = 200;
  const readingEntry = day.viewer.children[10];
  await day.poll({ lines: ["background log"], cursor: 6012 }, false);
  assert.equal(day.viewer.children.length, 5001, "trimming must wait while older entries are being read");
  assert.equal(day.viewer.children[10], readingEntry);
  assert.equal(day.viewer.scrollTop, 200);
  await day.poll({ lines: [], cursor: 6012 });
  assert.equal(day.viewer.children.length, 5000, "returning to the bottom must trim even without new entries");
  await day.older({ lines: ["older log"], start: 1000, hasMoreBefore: true });
  assert.equal(new URL(day.requests.at(-1), "http://localhost").searchParams.get("before"), "1002");
  assert.equal(day.viewer.children[0].dataset.raw, "older log", "trimmed history must remain accessible");

  const gap = { lines: ["replacement log"], cursor: 7000, replace: true, start: 6500, hasMoreBefore: true };
  const beforeGap = day.viewer.children[0];
  await day.poll(gap, false);
  assert.equal(day.viewer.children[0], beforeGap, "a missed-data gap must not replace the view while reading");
  assert.equal(vm.runInContext("refreshCursor", day.context), 6012, "a deferred replacement must not advance the cursor");
  await day.poll(gap);
  assert.equal(day.viewer.children.length, 1);
  await day.older({ lines: ["previous log"], start: 6499, hasMoreBefore: true });
  assert.equal(new URL(day.requests.at(-1), "http://localhost").searchParams.get("before"), "6500",
    "a gap replacement must reset the pagination offset");

  const restarted = viewerContext(true);
  await restarted.poll({ generation: "generation-1", lines: ["before restart"], cursor: 11 });
  assert.equal(restarted.reloads, 0, "the same generation must not reload");
  await restarted.poll({ generation: "after-restart", lines: ["renumbered log"], cursor: 1 });
  assert.equal(restarted.reloads, 1, "a restarted index must reload the page");
  assert.equal(restarted.viewer.children.length, 1, "new-generation entries must not be appended");
  assert.equal(vm.runInContext("refreshCursor", restarted.context), 11, "a stale page must not adopt the new cursor");
  await restarted.pollStats({ generation: "after-restart" });
  assert.equal(restarted.reloads, 1, "overlapping polls must not trigger multiple reloads");

  const midnight = viewerContext(false);
  await midnight.pollStats({ generation: "generation-1" });
  assert.equal(midnight.reloads, 0);
  await midnight.pollStats({ generation: "after-midnight" });
  assert.equal(midnight.reloads, 1, "stats polling must detect midnight even without new logs");

  const older = viewerContext(true);
  await older.poll({ generation: "generation-1", lines: Array.from({ length: 5001 }, () => "log"), cursor: 5011 });
  await older.older({ generation: "after-midnight", lines: ["old generation"], start: 0 });
  assert.equal(older.reloads, 1, "pagination must also detect a generation change");
  assert.equal(older.viewer.children.length, 5000, "pagination must not apply a different generation");
}

main().catch(error => { console.error(error); process.exitCode = 1; });
