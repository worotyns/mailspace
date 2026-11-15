// SPDX-License-Identifier: AGPL-3.0-only
// Native forms remain functional without this local enhancement. No credential,
// setup seed or recovery code is copied to storage, messages or network APIs.
let submitting = false;
document.addEventListener(
  "submit",
  () => {
    // session.js broadcasts a lock during native submit. Keep controls intact
    // until the browser has serialized them; a foreign lock still clears them.
    submitting = true;
    queueMicrotask(() => {
      submitting = false;
    });
  },
  true,
);
function clearPrivate() {
  for (const node of document.querySelectorAll("[data-security-qr]"))
    node.removeAttribute("src");
  for (const node of document.querySelectorAll("[data-security-secret]"))
    node.replaceChildren();
  for (const input of document.querySelectorAll(
    'input[name="password"], input[name="code"]',
  ))
    input.value = "";
}
window.addEventListener("pagehide", clearPrivate);
window.addEventListener("mailspace:clear-private", clearPrivate);
window.addEventListener("mailspace:lock", () => {
  if (!submitting) clearPrivate();
});
for (const choice of document.querySelectorAll("[data-factor-choice]")) {
  const code = choice.querySelector("[data-factor-code]");
  const update = () => {
    const recovery =
      choice.querySelector('input[name="proof_type"]:checked')?.value ===
      "recovery";
    code.inputMode = recovery ? "text" : "numeric";
    code.value = "";
  };
  update();
  choice.addEventListener("change", (event) => {
    if (event.target.name !== "proof_type") return;
    update();
    code.focus();
  });
}
document.querySelector("[data-security-feedback]")?.focus();
