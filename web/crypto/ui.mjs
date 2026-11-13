// SPDX-License-Identifier: AGPL-3.0-only
import { VaultStore } from "./store.mjs";
import { LocalVault, VaultError } from "./vault.mjs";

const root = document.getElementById("local-key-vault");
const status = document.getElementById("vault-status");
const controls = document.getElementById("vault-controls");
const element = (id) => document.getElementById(id);
let vault;
let revocation = null;
let busy = false;
let uiEpoch = 0;
let lastActivity = Date.now();
const expiresAt = Number(root.dataset.sessionExpires);
const messages = {
  "mixed-key-protection":
    "This key mixes protected and unprotected secret packets. Re-export it with one consistent passphrase in GnuPG before importing.",
  "unsafe-passphrase":
    "Use at least 12 characters for a new or unprotected key's passphrase.",
  "bad-passphrase": "The key passphrase was not accepted.",
  "invalid-key":
    "Could not read this armored OpenPGP key, or it exceeds the import limit.",
  "private-key-in-public-import":
    "Use the private-key import for secret keys. They cannot be stored as public contacts.",
  "unprotected-key": "An unprotected private key cannot be stored.",
  "invalid-email": "Check the email address.",
  "invalid-name":
    "Enter a display name up to 120 characters, without newlines.",
  "fingerprint-mismatch":
    "The complete fingerprint does not match the selected key.",
  "invalid-fingerprint": "Enter the complete hexadecimal fingerprint.",
  "key-changed":
    "This email is already bound to a different key. Compare both fingerprints and explicitly choose replacement.",
  "unusable-key":
    "The key cannot encrypt for this email: check its self-certified identity, expiry and revocation.",
  locked: "The operation was cancelled because the vault was locked.",
  "missing-key": "The selected key is not in this browser vault.",
  "key-limit":
    "Local key limit reached. Export backups before reorganizing your vault.",
};
function notify(message) {
  status.textContent = message;
}
function clearSecrets() {
  for (const input of root.querySelectorAll(
    "input[type=password], #private-armor, #private-file",
  ))
    input.value = "";
  revocation = null;
  element("export-revocation").hidden = true;
}
function lock() {
  uiEpoch++;
  vault?.lock();
  clearSecrets();
  notify("Local vault locked.");
}
function checkSession() {
  if (!Number.isFinite(expiresAt) || Date.now() >= expiresAt) {
    lock();
    throw new VaultError("locked");
  }
}
function download(name, value) {
  const url = URL.createObjectURL(
    new Blob([value], { type: "application/pgp-keys" }),
  );
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
function node(tag, text, classes) {
  const result = document.createElement(tag);
  if (text != null) result.textContent = text;
  if (classes) result.className = classes;
  return result;
}
async function refresh() {
  const snapshot = await vault.list();
  const list = element("key-list");
  list.replaceChildren();
  const privateSelect = element("private-key");
  const trustSelect = element("trust-key");
  const privateValue = privateSelect.value,
    trustValue = trustSelect.value;
  privateSelect.replaceChildren();
  trustSelect.replaceChildren();
  for (const key of snapshot.keys) {
    const card = node(
      "article",
      null,
      "rounded-xl border border-line bg-panel p-4",
    );
    card.append(
      node(
        "p",
        key.identities.join(" · ") || "No usable user identity",
        "text-sm font-medium [overflow-wrap:anywhere]",
      ),
    );
    card.append(
      node(
        "p",
        key.fingerprint,
        "mt-2 font-mono text-xs text-accent [overflow-wrap:anywhere]",
      ),
    );
    card.append(
      node(
        "p",
        `${key.private ? "Protected private key · " : "Public contact · "}${key.status}${key.expires ? " · expires " + key.expires.slice(0, 10) : ""}`,
        "mt-2 text-xs text-muted",
      ),
    );
    const row = node("div", null, "mt-3 flex flex-wrap gap-3");
    const exportPublic = node(
      "button",
      "Export public key",
      "text-xs text-accent",
    );
    exportPublic.type = "button";
    exportPublic.addEventListener("click", () =>
      perform(async () =>
        download(
          `${key.fingerprint}.public.asc`,
          await vault.exportPublic(key.fingerprint),
        ),
      ),
    );
    row.append(exportPublic);
    if (key.private) {
      const exportPrivate = node(
        "button",
        "Export protected private key",
        "text-xs text-accent",
      );
      exportPrivate.type = "button";
      exportPrivate.addEventListener("click", () =>
        perform(async () =>
          download(
            `${key.fingerprint}.private.asc`,
            await vault.exportPrivate(key.fingerprint),
          ),
        ),
      );
      row.append(exportPrivate);
      privateSelect.append(new Option(key.fingerprint, key.fingerprint));
    }
    trustSelect.append(new Option(key.fingerprint, key.fingerprint));
    card.append(row);
    list.append(card);
  }
  if (!snapshot.keys.length)
    list.append(
      node("p", "No keys in this browser yet.", "text-sm text-muted"),
    );
  if ([...privateSelect.options].some((o) => o.value === privateValue))
    privateSelect.value = privateValue;
  if ([...trustSelect.options].some((o) => o.value === trustValue))
    trustSelect.value = trustValue;
  const bindings = element("trusted-bindings");
  bindings.replaceChildren();
  for (const binding of snapshot.bindings) {
    const row = node("div", null, "mt-3 border-t border-line pt-3");
    row.append(node("p", binding.email, "text-xs font-medium"));
    row.append(
      node(
        "p",
        binding.fingerprint,
        "mt-1 font-mono text-xs text-muted [overflow-wrap:anywhere]",
      ),
    );
    const untrust = node("button", "Remove trust", "mt-2 text-xs text-accent");
    untrust.type = "button";
    untrust.addEventListener("click", () =>
      perform(async () => {
        await vault.untrust(binding.email);
        notify(
          "Email binding removed. Encryption will require a new explicit fingerprint decision.",
        );
      }),
    );
    row.append(untrust);
    bindings.append(row);
  }
  if (!snapshot.bindings.length)
    bindings.append(
      node(
        "p",
        "No verified email bindings. Importing a contact does not establish trust.",
        "text-xs text-muted",
      ),
    );
  element("vault-lock-state").textContent = snapshot.locked
    ? "Locked"
    : `Unlocked · ${snapshot.fingerprint}`;
}
async function perform(action) {
  if (busy) return;
  busy = true;
  for (const button of controls.querySelectorAll("button"))
    if (button.id !== "lock-key") button.disabled = true;
  try {
    checkSession();
    await action();
    await refresh();
  } catch (error) {
    notify(
      error instanceof VaultError
        ? messages[error.code] ||
            "The local key operation could not be completed."
        : "The local key operation could not be completed. Check browser storage and key compatibility.",
    );
  } finally {
    busy = false;
    for (const button of controls.querySelectorAll("button"))
      button.disabled = false;
  }
}
async function readInput(textID, fileID) {
  const file = element(fileID).files[0];
  if (file) {
    if (file.size > 2 << 20) throw new VaultError("invalid-key");
    return file.text();
  }
  return element(textID).value;
}

try {
  if (!window.isSecureContext || !crypto.subtle || !window.indexedDB)
    throw new Error("unsupported browser");
  vault = new LocalVault(new VaultStore(root.dataset.userId));
  await refresh();
  controls.disabled = false;
  notify("Local key vault ready. Keys stay protected in this browser.");
  element("generate-key").addEventListener("click", () =>
    perform(async () => {
      try {
        const epoch = uiEpoch;
        const generated = await vault.generate(
          element("generate-name").value,
          element("generate-email").value,
          element("generate-passphrase").value,
        );
        if (epoch !== uiEpoch) throw new VaultError("locked");
        revocation = generated;
        element("export-revocation").hidden = false;
        notify(
          "Protected key generated. Export its private-key backup and revocation certificate before leaving.",
        );
      } finally {
        element("generate-passphrase").value = "";
      }
    }),
  );
  element("export-revocation").addEventListener("click", () => {
    if (revocation)
      download(
        `${revocation.fingerprint}.revocation.asc`,
        revocation.revocationCertificate,
      );
  });
  element("import-private").addEventListener("click", () =>
    perform(async () => {
      try {
        await vault.importPrivate(
          await readInput("private-armor", "private-file"),
          element("import-passphrase").value,
        );
        notify(
          "Private key imported in protected form. Unlock it only when needed.",
        );
      } finally {
        element("import-passphrase").value = "";
        element("private-armor").value = "";
        element("private-file").value = "";
      }
    }),
  );
  element("import-public").addEventListener("click", () =>
    perform(async () => {
      await vault.importPublic(await readInput("public-armor", "public-file"));
      element("public-armor").value = "";
      element("public-file").value = "";
      notify("Public key imported without establishing email trust.");
    }),
  );
  element("unlock-key").addEventListener("click", () =>
    perform(async () => {
      try {
        await vault.unlock(
          element("private-key").value,
          element("unlock-passphrase").value,
        );
        lastActivity = Date.now();
        notify(
          "Key unlocked in memory. It locks on inactivity, leaving this page or hiding the tab.",
        );
      } finally {
        element("unlock-passphrase").value = "";
      }
    }),
  );
  element("lock-key").addEventListener("click", async () => {
    lock();
    await refresh();
  });
  element("trust-email").addEventListener("click", () =>
    perform(async () => {
      await vault.trust(
        element("trust-email-address").value,
        element("trust-key").value,
        element("trust-fingerprint").value,
        element("replace-binding").checked,
      );
      element("trust-fingerprint").value = "";
      element("replace-binding").checked = false;
      notify("Fingerprint bound to this email by your explicit decision.");
    }),
  );
  window.addEventListener("mailspace:lock", () => {
    lock();
    refresh().catch(() => {});
  });
  window.addEventListener("pagehide", lock);
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) {
      lock();
      refresh().catch(() => {});
    }
  });
  for (const event of ["keydown", "pointerdown"])
    document.addEventListener(
      event,
      () => {
        lastActivity = Date.now();
      },
      { passive: true },
    );
  setInterval(() => {
    if (
      (!vault.locked && Date.now() - lastActivity > 5 * 60_000) ||
      Date.now() >= expiresAt
    ) {
      lock();
      refresh().catch(() => {});
    }
  }, 1000);
} catch {
  controls.disabled = true;
  notify(
    "Local key management is unavailable. Use a secure context and a current browser with IndexedDB enabled.",
  );
}
