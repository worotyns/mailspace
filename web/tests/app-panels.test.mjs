// SPDX-License-Identifier: AGPL-3.0-only
import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { JSDOM } from "jsdom";
const origin = "https://mail.example.test";
const panelSource = await readFile(
  new URL("../crypto/app-panels.mjs", import.meta.url),
  "utf8",
);
const childSource = await readFile(
  new URL("../../internal/webui/assets/panel-child.js", import.meta.url),
  "utf8",
);
function harness() {
  const dom = new JSDOM(
    '<a id="compose" href="/secure/compose?account=synthetic">Compose</a><a id="keys" href="/keys">Keys</a>',
    { url: origin + "/?space=synthetic", runScripts: "outside-only" },
  );
  const { window } = dom;
  window.HTMLDialogElement.prototype.showModal = function () {
    this.open = true;
  };
  window.HTMLDialogElement.prototype.close = function () {
    if (!this.open) return;
    this.open = false;
    this.dispatchEvent(new window.Event("close"));
  };
  const navigations = [];
  new window.Function("location", panelSource)({
    origin,
    href: origin + "/?space=synthetic",
    assign: (url) => navigations.push(url),
  });
  const click = (selector) => window.document.querySelector(selector).click();
  const getFrame = () => window.document.querySelector(".app-panel-frame");
  const send = (data, options = {}) =>
    window.dispatchEvent(
      new window.MessageEvent("message", {
        origin: options.origin || origin,
        source: options.source || getFrame()?.contentWindow,
        data,
      }),
    );
  return { dom, window, click, getFrame, send, navigations };
}
function beforeUnload(window) {
  const event = new window.Event("beforeunload", { cancelable: true });
  window.dispatchEvent(event);
  return event.defaultPrevented;
}
test("dirty close and new-panel navigation require explicit discard; keep preserves composer", () => {
  const h = harness();
  try {
    h.click("#compose");
    const frame = h.getFrame();
    let cleared = 0,
      relocked = 0;
    frame.contentWindow.addEventListener(
      "mailspace:clear-private",
      () => cleared++,
    );
    frame.contentWindow.addEventListener("mailspace:lock", () => relocked++);
    h.send({ type: "mailspace:panel-dirty", dirty: true });
    assert.equal(beforeUnload(h.window), true);
    h.click("#app-panel-dialog > header button");
    assert.equal(
      h.window.document.querySelector("#app-panel-discard").open,
      true,
    );
    assert.equal(frame.isConnected, true);
    assert.equal(
      h.window.document.querySelector("#app-panel-discard button").autofocus,
      true,
    );
    h.click("#app-panel-discard button");
    assert.equal(frame.isConnected, true);
    h.click("#keys");
    assert.equal(h.getFrame(), frame);
    h.click("#app-panel-discard button:last-child");
    assert.notEqual(h.getFrame(), frame);
    assert.match(h.getFrame().src, /\/keys$/);
    assert.equal(cleared, 1);
    assert.equal(relocked, 0, "normal disposal must not relock shared session");
    assert.equal(beforeUnload(h.window), false);
  } finally {
    h.dom.window.close();
  }
});
test("inline decrypt links keep their own click handling instead of opening a panel", () => {
  const h = harness();
  try {
    const link = h.window.document.createElement("a");
    link.href = "/messages/read?reference=synthetic";
    link.setAttribute("data-inline-decrypt", "");
    h.window.document.body.append(link);
    let inline = false;
    link.addEventListener("click", (event) => {
      inline = true;
      event.preventDefault();
    });
    link.click();
    assert.equal(inline, true);
    assert.equal(h.getFrame(), null);
  } finally {
    h.dom.window.close();
  }
});
test("iframe root and non-root links share the same discard guard", () => {
  for (const href of ["/?account=synthetic", "/keys"]) {
    const h = harness();
    try {
      h.click("#compose");
      const frame = h.getFrame();
      h.send({ type: "mailspace:panel-dirty", dirty: true });
      h.send({ type: "mailspace:panel-navigate", href });
      assert.equal(h.getFrame(), frame);
      assert.equal(h.navigations.length, 0);
      assert.equal(
        h.window.document.querySelector("#app-panel-discard").open,
        true,
      );
      h.click("#app-panel-discard button:last-child");
      if (href.startsWith("/?"))
        assert.deepEqual(h.navigations, [origin + href]);
      else assert.match(h.getFrame().src, /\/keys$/);
    } finally {
      h.dom.window.close();
    }
  }
});
test("only a current same-origin frame's recorded outcome bypasses discard", () => {
  const h = harness();
  try {
    h.click("#compose");
    const frame = h.getFrame();
    h.send({ type: "mailspace:panel-dirty", dirty: true });
    const sent = {
      type: "mailspace:panel-sent",
      state: "accepted",
      location: "/outbox?message=synthetic",
    };
    h.send(sent, { origin: "https://mail.example.test.attacker.test" });
    h.send(sent, { source: h.window });
    h.send({ ...sent, state: "clicked-send" });
    h.send({
      ...sent,
      location: "https://attacker.test/outbox?message=synthetic",
    });
    assert.equal(h.getFrame(), frame);
    assert.equal(beforeUnload(h.window), true);
    h.send(sent);
    assert.equal(h.getFrame(), null);
    assert.equal(beforeUnload(h.window), false);
  } finally {
    h.dom.window.close();
  }
});
test("late load does not erase dirty state and Escape keeps the composer", () => {
  const h = harness();
  try {
    h.click("#compose");
    const frame = h.getFrame();
    h.send({ type: "mailspace:panel-dirty", dirty: true });
    frame.dispatchEvent(new h.window.Event("load"));
    const cancel = new h.window.Event("cancel", { cancelable: true });
    h.window.document.querySelector("#app-panel-dialog").dispatchEvent(cancel);
    assert.equal(cancel.defaultPrevented, true);
    assert.equal(
      h.window.document.querySelector("#app-panel-discard").open,
      true,
    );
    assert.equal(beforeUnload(h.window), true);
  } finally {
    h.dom.window.close();
  }
});
test("child guards metadata, restored drafts and attachments without messaging content", async () => {
  const dom = new JSDOM(
    '<iframe src="https://mail.example.test/secure/compose"></iframe>',
    {
      url: origin + "/?space=synthetic",
      runScripts: "outside-only",
    },
  );
  try {
    const child = dom.window.document.querySelector("iframe").contentWindow;
    child.document.open();
    child.document.write(`<!doctype html><html><body>
      <input id="secure-to" value="initial@example.test"><input id="secure-cc"><input id="secure-bcc">
      <input id="secure-subject" value="Initial subject"><textarea id="secure-body"></textarea>
      <input id="secure-files" type="file"><p id="restored-attachments"></p><button id="secure-send">Send</button>
      <a id="root" href="${origin}/?account=synthetic">Timeline</a><a id="keys" href="${origin}/keys">Keys</a></body></html>`);
    child.document.close();
    const messages = [];
    dom.window.postMessage = (data, target) => {
      assert.equal(target, origin);
      messages.push(data);
    };
    let competingNavigation = 0;
    child.document.addEventListener("click", (event) => {
      if (event.target.closest("a[href]")) competingNavigation++;
    });
    new child.Function(childSource)();
    const input = (id, value) => {
      const element = child.document.getElementById(id);
      element.value = value;
      element.dispatchEvent(new child.Event("input", { bubbles: true }));
    };
    input("secure-to", "PRIVATE_RECIPIENT_CANARY@example.test");
    assert.equal(messages.at(-1).dirty, true);
    assert.equal(beforeUnload(child), true);
    input("secure-to", "initial@example.test");
    assert.equal(messages.at(-1).dirty, false);
    input("secure-subject", "PRIVATE_SUBJECT_CANARY");
    assert.equal(messages.at(-1).dirty, true);
    input("secure-subject", "Initial subject");
    child.document.getElementById("secure-body").value = "PRIVATE_DRAFT_CANARY";
    child.dispatchEvent(new child.Event("mailspace:compose-changed"));
    assert.equal(messages.at(-1).dirty, true);
    input("secure-body", "");
    const files = child.document.getElementById("secure-files");
    Object.defineProperty(files, "files", {
      value: [{ name: "PRIVATE_FILE_CANARY.txt", size: 10 }],
      configurable: true,
    });
    files.dispatchEvent(new child.Event("change", { bubbles: true }));
    assert.equal(messages.at(-1).dirty, true);
    Object.defineProperty(files, "files", { value: [], configurable: true });
    child.document.getElementById("restored-attachments").textContent =
      "PRIVATE_RESTORED_FILE_CANARY.txt";
    await Promise.resolve();
    assert.equal(messages.at(-1).dirty, true);
    child.document.getElementById("secure-send").click();
    assert.equal(beforeUnload(child), true, "click is not a recorded send");
    for (const id of ["root", "keys"]) {
      const click = new child.MouseEvent("click", {
        bubbles: true,
        cancelable: true,
      });
      child.document.getElementById(id).dispatchEvent(click);
      assert.equal(click.defaultPrevented, true);
      assert.equal(messages.at(-1).type, "mailspace:panel-navigate");
      assert.equal(
        competingNavigation,
        0,
        "HTMX must not navigate inside the child frame",
      );
    }
    child.dispatchEvent(new child.Event("mailspace:compose-sent"));
    assert.equal(messages.at(-1).dirty, false);
    assert.equal(beforeUnload(child), false);
    assert.equal(JSON.stringify(messages).includes("PRIVATE_"), false);
  } finally {
    dom.window.close();
  }
});

test("panel navigation shows pending state, deduplicates pending URL and ignores stale frame load", () => {
  const h = harness();
  try {
    h.click("#compose");
    const first = h.getFrame();
    const content = h.window.document.querySelector(".app-panel-content");
    assert.equal(content.getAttribute("aria-busy"), "true");
    assert.equal(
      content.querySelector('[role="status"]').textContent,
      "Loading…",
    );
    assert.equal(first.classList.contains("invisible"), true);
    h.click("#compose");
    assert.equal(
      h.getFrame(),
      first,
      "same pending navigation must not restart",
    );
    h.click("#keys");
    const second = h.getFrame();
    assert.notEqual(
      second,
      first,
      "new navigation must have a fresh browsing context",
    );
    first.dispatchEvent(new h.window.Event("load"));
    assert.equal(content.getAttribute("aria-busy"), "true");
    assert.equal(second.classList.contains("invisible"), true);
    second.dispatchEvent(new h.window.Event("load"));
    assert.equal(content.hasAttribute("aria-busy"), false);
    assert.equal(content.querySelector('[role="status"]'), null);
    assert.equal(second.classList.contains("invisible"), false);
    h.click("#app-panel-dialog > header button");
    assert.equal(h.getFrame(), null);
    assert.equal(content.hasAttribute("aria-busy"), false);
  } finally {
    h.dom.window.close();
  }
});
