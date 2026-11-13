// SPDX-License-Identifier: AGPL-3.0-only
import { messageText, MailHTMLPreview } from "./mail-html.mjs";
import { LocalDrafts } from "./drafts.mjs";
import { VaultStore } from "./store.mjs";
import { LocalVault, VaultError } from "./vault.mjs";
import {
  composeEntity,
  readAttachments,
  encryptedEntity,
  extractEncrypted,
  parseLocalMime,
} from "./mime.mjs";
import { addressParser } from "../vendor/postal-mime/postal-mime.js";
const root = document.getElementById("secure-mail");
const el = (id) => document.getElementById(id);
const status = el("secure-status"),
  reading = root.dataset.read === "true";
const ordinary = root.dataset.ordinary === "true";
let currentHTML = null;
let selectionClassifier;
const htmlPreview = reading
  ? new MailHTMLPreview(
      el("html-preview-dialog"),
      el("html-preview-frame"),
      window,
    )
  : null;
let available = false;
let vault,
  epoch = 0,
  busy = false,
  lastActivity = Date.now(),
  submitted = false,
  request;
const downloads = [];
let drafts,
  activeDraft = null,
  restoredFiles = [];
let requestId = root.dataset.requestId;
let replyLocator = {
  folder: root.dataset.replyFolder,
  uid: Number(root.dataset.replyUid),
  validity: Number(root.dataset.replyValidity),
};
function draftButtons() {
  if (reading) return;
  for (const id of ["draft-save", "draft-load", "draft-delete"])
    el(id).disabled = !drafts || vault?.locked || submitted || busy;
}
function showRestoredFiles() {
  if (reading) return;
  el("restored-attachments").textContent = restoredFiles.length
    ? "From draft: " + restoredFiles.map((file) => file.name).join(" · ")
    : "";
  el("clear-restored-attachments").hidden = !restoredFiles.length;
}
async function draftList() {
  const select = el("local-draft");
  select.replaceChildren();
  for (const draft of await drafts.list(root.dataset.account)) {
    const option = document.createElement("option");
    option.value = draft.id;
    option.textContent = `Local draft · ${new Date(draft.updatedAt).toLocaleString()} · ${draft.fingerprint.slice(-16)}`;
    select.append(option);
  }
  if (activeDraft) select.value = activeDraft;
}
async function composeSnapshot() {
  return {
    to: el("secure-to").value,
    cc: el("secure-cc").value,
    bcc: el("secure-bcc").value,
    subject: el("secure-subject").value,
    body: el("secure-body").value,
    requestId,
    reply: { ...replyLocator },
    attachments: [
      ...restoredFiles,
      ...(await readAttachments(el("secure-files").files)),
    ],
  };
}

const notify = (text) => {
  status.textContent = text;
};
function lock() {
  selectionClassifier?.clear();
  epoch++;
  vault?.lock();
  request?.abort();
  el("mail-passphrase").value = "";
  if (reading) {
    el("decrypted-content").hidden = true;
    el("decrypted-text").textContent = "";
    el("mail-signature").textContent = "";
    el("decrypted-attachments").replaceChildren();
    el("mail-decrypt").disabled = !ordinary;
    currentHTML = null;
    htmlPreview?.close();
    el("html-preview-open").hidden = true;
  } else {
    el("secure-body").value = "";
    el("secure-files").value = "";
    restoredFiles = [];
    showRestoredFiles();
    el("secure-compose-fields").disabled = true;
  }
  draftButtons();
  for (const url of downloads) URL.revokeObjectURL(url);
  downloads.length = 0;
  notify(
    reading
      ? "Locked. Decrypted content was cleared."
      : "Locked. Private controls were cleared; explicitly saved encrypted drafts remain in this browser.",
  );
}
function session() {
  if (
    !Number.isFinite(Number(root.dataset.sessionExpires)) ||
    Date.now() >= Number(root.dataset.sessionExpires)
  ) {
    lock();
    throw new VaultError("locked");
  }
}
const errors = {
  locked: "Unlock your local key to continue.",
  "bad-passphrase": "The key passphrase was not accepted.",
  "untrusted-recipient":
    "A recipient or your own address has no verified key. Import the public key and compare its full fingerprint in Local keys first.",
  "sender-key-mismatch":
    "The unlocked key does not match the verified key for the From address.",
  "unusable-key": "A key is expired, revoked or unsuitable for encryption.",
  "key-changed":
    "A recipient key changed. Compare and verify its fingerprint before sending.",
  "subject-limit": "Shorten the subject before saving or sending this message.",
  "invalid-recipients": "Enter valid email addresses for all recipients.",
  "decryption-failed":
    "Decryption failed. Check the selected key or download the original mail in another compatible client.",
  "unsupported-encryption":
    "This message uses an unsupported OpenPGP/MIME form. Its content was not displayed.",
  "html-limit":
    "HTML exceeds the safe preview limit. The original message remains available for download.",
  "html-unavailable":
    "HTML preview is unavailable in this browser. Use the text view or download the original message.",
  "draft-limit":
    "Local draft limit reached (20 drafts / 16 MiB). Delete an unneeded saved draft before creating another.",
  "missing-draft": "Select an existing draft for this account.",
  "invalid-draft":
    "This local draft could not be authenticated or does not match this account. It was not restored or sent.",
  "attachment-limit":
    "Choose up to 10 files, with at most 1 MiB total. Attachments are encrypted locally before upload.",
  "invalid-attachment-name":
    "Use attachment names up to 180 UTF-8 bytes without path separators or control characters.",
  "message-limit": "This message exceeds the supported size limit.",
  "invalid-mime": "The MIME structure is malformed or exceeds safety limits.",
};
async function perform(operation) {
  if (busy || !available) return;
  busy = true;
  draftButtons();
  if (!reading) el("secure-compose-fields").disabled = true;
  const current = epoch;
  try {
    session();
    await operation(current);
  } catch (error) {
    if (current === epoch)
      notify(
        errors[error.code] ||
          "The operation could not finish. No private content was uploaded. Check Outbox if a send was already submitted.",
      );
  } finally {
    busy = false;
    draftButtons();
    el("mail-passphrase").value = "";
    if (!reading)
      el("secure-compose-fields").disabled = vault?.locked || submitted;
  }
}
function recipients(value) {
  if (value.length > 8192 || /[\r\n\0]/.test(value))
    throw new VaultError("invalid-recipients");
  if (!value.trim()) return [];
  const result = addressParser(value, { flatten: true }).map(
    (item) => item.address,
  );
  if (
    !result.length ||
    result.length > 100 ||
    result.some(
      (address) =>
        !address || !address.includes("@") || /[^\x21-\x7e]/.test(address),
    )
  )
    throw new VaultError("invalid-recipients");
  return result;
}
try {
  if (!ordinary && (!isSecureContext || !crypto.subtle || !indexedDB))
    throw new Error("secure context required");
  session();
  if (!ordinary) vault = new LocalVault(new VaultStore(root.dataset.userId));
  const snapshot = ordinary ? { keys: [] } : await vault.list();
  const select = el("mail-private-key");
  for (const key of snapshot.keys.filter((key) => key.private)) {
    const option = document.createElement("option");
    option.value = key.fingerprint;
    option.textContent = `${key.identities.join(" · ")} · ${key.fingerprint}`;
    select.append(option);
  }
  el("mail-unlock").disabled = !select.options.length;
  notify(
    ordinary
      ? "Read as text; no local key is required. HTML preview is optional."
      : select.options.length
        ? "Local key vault is locked. Unlock a key to continue."
        : "No private key in this browser. Open Local keys to generate or import one, export backups, and verify recipient fingerprints.",
  );
  el("mail-unlock").addEventListener("click", () =>
    perform(async (current) => {
      await vault.unlock(select.value, el("mail-passphrase").value);
      if (current !== epoch) return;
      lastActivity = Date.now();
      if (reading) el("mail-decrypt").disabled = false;
      else el("secure-compose-fields").disabled = submitted;
      notify("Key unlocked in memory. No passphrase is sent to the server.");
    }),
  );
  el("mail-lock").addEventListener("click", lock);
  if (reading) {
    el("mail-decrypt").disabled = !ordinary;
    el("html-preview-open").addEventListener("click", () =>
      perform(async () => {
        if (currentHTML) htmlPreview.open(currentHTML);
      }),
    );
    el("html-preview-close").addEventListener("click", () =>
      htmlPreview.close(),
    );
  }
  if (!reading) {
    drafts = new LocalDrafts(root.dataset.userId);
    await draftList();
    draftButtons();
    el("clear-restored-attachments").addEventListener("click", () => {
      restoredFiles = [];
      showRestoredFiles();
    });
    el("draft-save").addEventListener("click", () =>
      perform(async (current) => {
        const snapshot = await composeSnapshot();
        const saved = await drafts.save(
          vault,
          root.dataset.account,
          root.dataset.from,
          snapshot,
          activeDraft || undefined,
        );
        if (current !== epoch) return;
        activeDraft = saved;
        await draftList();
        notify(
          "Draft saved as ciphertext in this browser only. It includes its attachments and original send request ID.",
        );
      }),
    );
    el("draft-load").addEventListener("click", () =>
      perform(async (current) => {
        const id = el("local-draft").value;
        const snapshot = await drafts.load(
          vault,
          root.dataset.account,
          root.dataset.from,
          id,
        );
        if (current !== epoch) return;
        for (const name of ["to", "cc", "bcc", "subject", "body"])
          el("secure-" + name).value = snapshot[name];
        replyLocator = snapshot.reply;
        requestId = snapshot.requestId;
        activeDraft = id;
        restoredFiles = snapshot.attachments;
        el("secure-files").value = "";
        showRestoredFiles();
        notify(
          "Draft decrypted locally. Review recipients and content before sending; nothing was uploaded.",
        );
      }),
    );
    el("draft-delete").addEventListener("click", () =>
      perform(async () => {
        const id = el("local-draft").value;
        await drafts.remove(root.dataset.account, id);
        if (activeDraft === id) activeDraft = null;
        await draftList();
        notify(
          "Selected local ciphertext draft deleted. The current unsent message was not submitted.",
        );
      }),
    );
  }
  if (reading && root.dataset.classifyReference) {
    const { ClientSelection } = await import("./client-selection.mjs");
    selectionClassifier = new ClientSelection(root);
  }
  if (reading)
    el("mail-decrypt").addEventListener("click", () =>
      perform(async (current) => {
        notify("Fetching ciphertext and decrypting locally…");
        request = new AbortController();
        const response = await fetch(root.dataset.rawUrl, {
          credentials: "same-origin",
          cache: "no-store",
          redirect: "error",
          signal: request.signal,
        });
        if (!response.ok) throw new Error("ciphertext unavailable");
        const bytes = new Uint8Array(await response.arrayBuffer());
        const result = ordinary
          ? { data: bytes, signatures: [], provenance: "ordinary-mail" }
          : await vault.decryptSigned(
              await extractEncrypted(bytes),
              root.dataset.sender,
            );
        const parsed = await parseLocalMime(result.data);
        if (current !== epoch) return;
        el("decrypted-text").textContent = messageText(parsed);
        currentHTML = typeof parsed.html === "string" ? parsed.html : null;
        el("html-preview-open").hidden = !currentHTML;
        el("mail-signature").textContent = ordinary
          ? "Ordinary mail · no OpenPGP signature verified"
          : !result.signatures.length
            ? "Unsigned message. Sender identity is not verified."
            : result.signatures
                .map((s) =>
                  s.status === "valid"
                    ? s.trustedSender
                      ? "Valid signature · verified sender fingerprint"
                      : "Valid signature · sender identity not trusted"
                    : s.status === "invalid"
                      ? "INVALID signature · do not trust this message"
                      : "Unknown signing key · signature not verified",
                )
                .join("; ");
        const attachments = el("decrypted-attachments");
        attachments.replaceChildren();
        for (const attachment of parsed.attachments) {
          const url = URL.createObjectURL(
            new Blob([attachment.content], {
              type: "application/octet-stream",
            }),
          );
          downloads.push(url);
          const link = document.createElement("a");
          link.href = url;
          link.download = (attachment.filename || "attachment.bin")
            .replace(/[\/\\\x00-\x1f]/g, "_")
            .slice(0, 180);
          link.textContent = `Download local attachment: ${attachment.filename || "unnamed"}`;
          link.className = "block text-sm text-accent [overflow-wrap:anywhere]";
          attachments.append(link);
        }
        el("decrypted-content").hidden = false;
        selectionClassifier?.enable();
        notify(
          ordinary
            ? "Text view ready. HTML appears only when you explicitly open its isolated preview."
            : "Decrypted locally. No content, attachment or signature payload was sent back to the server.",
        );
      }),
    );
  else
    el("secure-send").addEventListener("click", () =>
      perform(async (current) => {
        if (submitted) return;
        const metadata = {
          account: root.dataset.account,
          requestId,
          replyFolder: replyLocator.folder,
          replyValidity: replyLocator.validity,
          replyUid: replyLocator.uid,
          to: el("secure-to").value,
          cc: el("secure-cc").value,
          bcc: el("secure-bcc").value,
          subject: el("secure-subject").value,
        };
        const to = [
          ...recipients(metadata.to),
          ...recipients(metadata.cc),
          ...recipients(metadata.bcc),
        ];
        if (!to.length || to.length > 100 || /[\r\n\0]/.test(metadata.subject))
          throw new VaultError("invalid-recipients");
        if (new TextEncoder().encode(metadata.subject).length > 200)
          throw new VaultError("subject-limit");
        el("secure-compose-fields").disabled = true;
        notify("Signing and encrypting locally…");
        const body = el("secure-body").value;
        const attachments = [
          ...restoredFiles,
          ...(await readAttachments(el("secure-files").files)),
        ];
        const encrypted = await vault.encryptSigned(
          composeEntity(body, attachments),
          root.dataset.from,
          to,
        );
        if (current !== epoch) return;
        // Deliberate allowlist: body, passphrase, private key and decrypted MIME never
        // enter the request. Metadata is visible to SMTP independently of encryption.
        const payload = JSON.stringify({
          ...metadata,
          encryptedMime: encryptedEntity(encrypted),
        });
        session();
        submitted = true;
        request = new AbortController();
        notify(
          "Submitting ciphertext once. Check Outbox before any new send if the connection is interrupted.",
        );
        const response = await fetch("/api/outbox/encrypted", {
          method: "POST",
          credentials: "same-origin",
          redirect: "error",
          headers: {
            "Content-Type": "application/json",
            "X-CSRF-Token": root.dataset.csrf,
          },
          body: payload,
          signal: request.signal,
        });
        if (!response.ok) throw new Error("submission failed");
        const result = await response.json();
        if (
          typeof result.location !== "string" ||
          !result.location.startsWith("/outbox?")
        )
          throw new Error("invalid outcome");
        if (activeDraft) await drafts.remove(root.dataset.account, activeDraft);
        lock();
        location.assign(result.location);
      }),
    );
  window.addEventListener("mailspace:lock", lock);
  window.addEventListener("pagehide", lock);
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) lock();
  });
  for (const event of ["keydown", "pointerdown"])
    document.addEventListener(
      event,
      () => {
        lastActivity = Date.now();
      },
      { passive: true },
    );
  const timer = setInterval(() => {
    if (Date.now() >= Number(root.dataset.sessionExpires)) {
      lock();
      el("mail-unlock").disabled = true;
      if (reading) el("mail-decrypt").disabled = true;
      clearInterval(timer);
    } else if (
      ((vault && !vault.locked) || currentHTML) &&
      Date.now() - lastActivity > 5 * 60_000
    )
      lock();
  }, 1000);
  available = true;
} catch {
  available = false;
  lock();
  el("mail-unlock").disabled = true;
  el("mail-passphrase").disabled = true;
  el("mail-private-key").disabled = true;
  if (!reading) el("secure-compose-fields").disabled = true;
  notify(
    "Local cryptography is unavailable. Use HTTPS (or loopback), a current browser and enabled IndexedDB.",
  );
}
