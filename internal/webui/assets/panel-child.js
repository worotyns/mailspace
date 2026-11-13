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
  document.addEventListener("click", (event) => {
    const link = event.target.closest("a[href]");
    if (
      !link ||
      event.button !== 0 ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey
    )
      return;
    const url = new URL(link.href, location.href);
    if (url.origin === location.origin && url.pathname === "/") {
      event.preventDefault();
      send({ type: "mailspace:panel-navigate", href: url.href });
    }
  });
  document.addEventListener("keydown", (event) => {
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
      event.preventDefault();
      send({ type: "mailspace:commands" });
    }
  });
  document.addEventListener("input", (event) => {
    if (event.target.id === "secure-body")
      send({ type: "mailspace:panel-dirty", dirty: !!event.target.value });
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
