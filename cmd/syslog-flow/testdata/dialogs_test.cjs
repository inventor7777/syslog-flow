const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");

for (const [file, selector, startMarker, endMarker] of [
  ["actions_settings.go", "action", ' for (const button of document.querySelectorAll("[data-close-action]"))', ' actionType.addEventListener'],
  ["search.go", "search", '  dialog.querySelectorAll("[data-close-search]")', '  const start = document.getElementById'],
]) {
  const source = fs.readFileSync(file, "utf8");
  const start = source.indexOf(startMarker);
  const end = source.indexOf(endMarker, start);
  assert.ok(start >= 0 && end > start, "missing dialog close handlers");
  const controls = source.slice(0, source.indexOf("</dialog>")).match(new RegExp('<button\\b[^>]*data-close-' + selector + '[^>]*>[^<]*</button>', "g"));
  assert.equal(controls.length, 1, "the X should be the only dismiss button");
  assert.match(controls[0], />×<\/button>/);

  const handlers = {};
  const close = { addEventListener(name, callback) { this[name] = callback; } };
  const dialog = {
    open: true,
    close() { this.open = false; },
    querySelectorAll: () => [close],
    addEventListener(name, callback) { handlers[name] = callback; },
  };
  vm.runInNewContext(source.slice(start, end), {
    document: { querySelectorAll: () => [close] },
    dialog, actionDialog: dialog,
  });
  // A selection that starts in the form and ends outside can target the dialog.
  handlers.click?.({ target: dialog, clientX: -1, clientY: -1 });
  assert.equal(dialog.open, true, "releasing the mouse outside must keep the form open");
  let prevented = false;
  handlers.cancel?.({ preventDefault() { prevented = true; } });
  if (!prevented) dialog.close();
  assert.equal(dialog.open, false, "Escape must retain the browser's default dialog dismissal");
  dialog.open = true;
  close.click();
  assert.equal(dialog.open, false, "the X must still close the form");
}
