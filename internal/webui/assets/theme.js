// SPDX-License-Identifier: AGPL-3.0-only
// Only a presentation preference is stored; no account or mail data.
(() => {
  const storageKey = "mailspace.color-theme";
  const choices = new Set(["system", "light", "dark"]);
  const media = window.matchMedia("(prefers-color-scheme: dark)");
  let choice = "system";
  try {
    const saved = localStorage.getItem(storageKey);
    if (choices.has(saved)) choice = saved;
  } catch {
    /* Storage may be disabled; the current page still works. */
  }
  function apply() {
    const theme =
      choice === "system" ? (media.matches ? "dark" : "light") : choice;
    document.documentElement.dataset.theme = theme;
    document.querySelectorAll("[data-theme-picker]").forEach((picker) => {
      picker.value = choice;
    });
  }
  apply();
  document.addEventListener("DOMContentLoaded", apply);
  document.addEventListener("htmx:afterSwap", apply);
  media.addEventListener("change", apply);
  document.addEventListener("change", (event) => {
    if (!event.target.matches("[data-theme-picker]")) return;
    if (!choices.has(event.target.value)) return;
    choice = event.target.value;
    try {
      localStorage.setItem(storageKey, choice);
    } catch {
      /* Page-only preference. */
    }
    apply();
  });
})();
