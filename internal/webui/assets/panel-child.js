// SPDX-License-Identifier: AGPL-3.0-only
(() => {
  for (const link of document.querySelectorAll(".settings-navigation a")) {
    const path = new URL(link.href, location.href).pathname;
    if (
      location.pathname === path ||
      (path === "/settings" && location.pathname.startsWith("/accounts"))
    )
      link.setAttribute("aria-current", "page");
  }
  if (window.self === window.top) return;
  try {
    if (window.parent.location.origin !== location.origin) return;
  } catch {
    return;
  }
  document.documentElement.classList.add("in-app-panel");
  const send = (data) => window.parent.postMessage(data, location.origin);
  if (document.querySelector("[data-session-rotated]"))
    send({ type: "mailspace:session-rotated" });
  const appPath =
    /^\/(settings|profile|workspaces|accounts|keys|rules|labels|automation|compose|secure\/compose|messages\/read|outbox|archive\/preview)(\/|$)/;
  const fields = ["to", "cc", "bcc", "subject", "body"].map((name) =>
    document.getElementById("secure-" + name),
  );
  const composer = !!document.getElementById("secure-body");
  const baseline = fields.map((field) => field?.value || "");
  let dirty = false,
    completed = false;
  function preventUnload(event) {
    if (!dirty) return;
    event.preventDefault();
    event.returnValue = "";
  }
  function reportDirty() {
    if (!composer) return;
    const changed =
      !completed &&
      (fields.some(
        (field, index) => (field?.value || "") !== baseline[index],
      ) ||
        !!document.getElementById("secure-files")?.files?.length ||
        !!document.getElementById("restored-attachments")?.textContent.trim());
    dirty = changed;
    if (dirty) window.addEventListener("beforeunload", preventUnload);
    else window.removeEventListener("beforeunload", preventUnload);
    // Only a boolean crosses documents; never fields, file names or plaintext.
    send({ type: "mailspace:panel-dirty", dirty });
  }
  document.addEventListener(
    "click",
    (event) => {
      const link = event.target.closest("a[href]");
      if (
        !link ||
        event.defaultPrevented ||
        event.button !== 0 ||
        event.metaKey ||
        event.ctrlKey ||
        event.shiftKey ||
        event.altKey ||
        link.hasAttribute("download") ||
        (link.target && link.target !== "_self")
      )
        return;
      const url = new URL(link.href, location.href);
      if (
        url.origin === location.origin &&
        (url.pathname === "/" ||
          url.pathname === "/security" ||
          appPath.test(url.pathname))
      ) {
        event.preventDefault();
        event.stopImmediatePropagation();
        reportDirty();
        send({ type: "mailspace:panel-navigate", href: url.href });
      }
    },
    true,
  );
  document.addEventListener("keydown", (event) => {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
      event.preventDefault();
      send({ type: "mailspace:commands" });
    }
  });
  for (const type of ["input", "change"])
    document.addEventListener(type, reportDirty);
  window.addEventListener("mailspace:compose-changed", reportDirty);
  for (const type of ["mailspace:compose-sent", "mailspace:discard-composer"])
    window.addEventListener(type, () => {
      completed = true;
      reportDirty();
    });
  const restored = document.getElementById("restored-attachments");
  if (composer && restored)
    new MutationObserver(reportDirty).observe(restored, {
      childList: true,
      characterData: true,
      subtree: true,
    });
  document.addEventListener("submit", (event) => {
    if (
      new URL(event.target.action, location.href).pathname.startsWith(
        "/auth/logout",
      )
    )
      send({ type: "mailspace:session-ending" });
  });
})();
