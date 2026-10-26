const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");

const source = fs.readFileSync("settings.go", "utf8");
const start = source.indexOf("    const settingForms =");
const end = source.indexOf("  </script>", start);
assert.ok(start >= 0 && end > start, "missing settings script");

function setup({ ownEditor = false, checked } = {}) {
  const button = { textContent: "Update", classList: { toggle() {} } };
  const toggle = checked === undefined ? null : {
    checked, defaultChecked: !checked, disabled: false, addEventListener() {},
  };
  const handlers = {};
  const form = {
    dataset: {},
    querySelector(selector) {
      if (selector === "[data-action-toggle]") return toggle;
      if (selector === "button[type=submit]") return button;
      return null;
    },
    addEventListener(name, callback) { handlers[name] = callback; },
    matches: () => false,
    getAttribute: () => "/settings",
  };
  const closeHandlers = [];
  const editor = {
    open: ownEditor,
    contains: candidate => ownEditor && candidate === form,
    addEventListener(name, callback, options) {
      assert.equal(name, "close");
      assert.equal(options.once, true);
      closeHandlers.push(callback);
    },
  };
  const navigations = [];
  let resolveResponse;
  let requestBody;
  vm.runInNewContext(source.slice(start, end), {
    document: {
      querySelectorAll: () => [form],
      getElementById: id => id === "action-dialog" ? editor : null,
    },
    window: {
      location: { set href(value) { navigations.push(value); } },
      setTimeout() {},
    },
    URLSearchParams,
    FormData: class {
      entries() {
        const values = [["section", "actions"], ["action", toggle ? "toggle" : "save"]];
        if (toggle?.checked && !toggle.disabled) values.push(["enabled", "on"]);
        return values;
      }
    },
    fetch(url, options) {
      requestBody = options.body;
      return new Promise(resolve => { resolveResponse = resolve; });
    },
  });
  return {
    editor, toggle, navigations,
    submit: () => handlers.submit({ preventDefault() {} }),
    respond(ok = true) {
      resolveResponse({ ok, json: async () => ({ ok, reload: true, error: "Save failed" }) });
    },
    get body() { return requestBody; },
    fireClose() {
      for (const callback of closeHandlers.splice(0)) callback();
    },
  };
}

(async () => {
  const pending = setup();
  const submitted = pending.submit();
  pending.editor.open = true;
  pending.respond();
  await submitted;
  assert.equal(pending.navigations.length, 0, "a delayed card response must preserve an open editor");
  // A close event may arrive after the user has already reopened the editor.
  pending.fireClose();
  assert.equal(pending.navigations.length, 0, "reopened editors must also preserve their draft");
  pending.editor.open = false;
  pending.fireClose();
  assert.deepEqual(pending.navigations, ["/settings?section=actions"]);

  for (const ownEditor of [false, true]) {
    const page = setup({ ownEditor });
    const submitted = page.submit();
    page.respond();
    await submitted;
    assert.equal(page.navigations.length, 1, "normal saves and the editor's own save must reload");
  }

  for (const checked of [false, true]) {
    const page = setup({ checked });
    const submitted = page.submit();
    assert.equal(page.body.get("enabled"), checked ? "on" : "off", "send the requested switch state explicitly");
    assert.equal(page.toggle.disabled, true, "prevent another change while the request is pending");
    page.respond(false);
    await submitted;
    assert.equal(page.toggle.disabled, false);
    assert.equal(page.toggle.checked, !checked, "failed requests must restore the saved state");
    assert.equal(page.navigations.length, 0);
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
