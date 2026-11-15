// SPDX-License-Identifier: AGPL-3.0-only
// Opening a message is the only automatic read-state trigger. It reuses the
// manual toggle's bounded POST contract and never runs on a timeline fetch.
// No message text, key or passphrase is read or sent.
async function post(url, data) {
  try {
    const response = await fetch(url, {
      method: "POST",
      credentials: "same-origin",
      redirect: "error",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        // Selects a bodyless confirmation response; not a security boundary.
        "X-Mailspace-Background": "1",
      },
      body: new URLSearchParams(data),
    });
    return response.ok;
  } catch {
    return false;
  }
}

// The full reader page marks ordinary mail on load and encrypted mail only
// after the local decrypt succeeded.
export async function markReaderRead(root) {
  if (
    !root ||
    root.dataset.seenWrites !== "true" ||
    root.dataset.seenUnread !== "true" ||
    root.dataset.seenSent === "true"
  )
    return false;
  const reference = root.dataset.seenReference || "";
  if (!reference) return false;
  root.dataset.seenSent = "true";
  const ok = await post("/messages/seen", {
    csrf: root.dataset.csrf || "",
    reference,
    seen: "1",
  });
  if (!ok) delete root.dataset.seenSent;
  return ok;
}

// An inline preview in the timeline reuses its own scoped form so account,
// revision and reference checks stay identical to the manual toggle.
export async function markMessageRead(article) {
  const form = article?.querySelector?.("[data-message-seen] form");
  if (!form || form.dataset.seenSent === "true") return false;
  const data = new FormData(form);
  // Only an unread row is marked; an already read row is left untouched.
  if (data.get("seen") !== "1") return false;
  form.dataset.seenSent = "true";
  const ok = await post(form.getAttribute("action") || "/messages/seen", data);
  if (!ok) delete form.dataset.seenSent;
  return ok;
}
