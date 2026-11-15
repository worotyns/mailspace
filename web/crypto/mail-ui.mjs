// SPDX-License-Identifier: AGPL-3.0-only
import "./recipient-picker.mjs";
import { AttachmentViewer } from "./attachments.mjs";
import { messageText, MailHTMLPreview } from "./mail-html.mjs";
import { LocalDrafts } from "./drafts.mjs";
import { extractSigned } from "./signed-mime.mjs";
import { originalQuote } from "./quote.mjs";
import { VaultError } from "./vault.mjs";
import { createSessionVault } from "./session-vault.mjs";
import {
  composeEntity,
  readAttachments,
  encryptedEntity,
  signedEntity,
  extractEncrypted,
  parseLocalMime,
} from "./mime.mjs";
import { addressParser } from "../vendor/postal-mime/postal-mime.js";
import { markReaderRead } from "./read-state.mjs";
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
  vaultLastTouch = 0,
  submitted = false,
  request;
const attachmentViewer = reading
  ? new AttachmentViewer(el("decrypted-attachments"), document)
  : null;
let drafts,
  activeDraft = null,
  restoredFiles = [];
let readMessage;
let encryptionRequired = root.dataset.encryptionRequired === "true";
let quoteApplied = false;
function resetProtection() {
  if (reading) return;
  el("secure-encrypt").checked = true;
  el("secure-sign-only").checked = false;
  updateProtection();
}
function updateProtection() {
  el("secure-sign-only").disabled = encryptionRequired;
  const signed = el("secure-sign-only").checked === true;
  el("secure-plaintext-consent").checked = false;
  el("secure-plaintext-consent").removeAttribute?.("aria-invalid");
  el("secure-plaintext-consent-row").hidden = !signed;
  el("secure-send").textContent = signed
    ? "Sign and send unencrypted"
    : "Sign, encrypt and send";
  el("secure-protection-note").textContent = signed
    ? "The message, attachments and filenames will be readable on Mailspace and mail servers. No recipient keys are required."
    : encryptionRequired
      ? "This original or draft requires encryption. Signing only is unavailable."
      : "Message and attachments are signed and encrypted to all recipients and your own key.";
}
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
  el("secure-quote-original").disabled =
    vault?.locked ||
    submitted ||
    busy ||
    quoteApplied ||
    !replyLocator.folder ||
    !replyLocator.uid ||
    !replyLocator.validity;
}
function showRestoredFiles() {
  if (reading) return;
  window.dispatchEvent?.(new Event("mailspace:compose-changed"));
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
    encryptionRequired,
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
  vault?.lock();
  clearPrivate();
}
function clearPrivate() {
  selectionClassifier?.clear();
  epoch++;
  if (vault?.sessionMode === "page") vault.lock();
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
    quoteApplied = false;
    el("secure-quote-status").textContent = "";
    resetProtection();
    el("secure-body").value = "";
    el("secure-files").value = "";
    restoredFiles = [];
    showRestoredFiles();
    el("secure-compose-fields").disabled = true;
  }
  draftButtons();
  attachmentViewer?.clear();
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
  "quote-limit":
    "The original exceeds the local 32 KiB quote limit or contains unsupported text. Nothing was quoted.",
  "quote-source":
    "Select an available cached original before quoting. Restored drafts keep their own original source.",
  "quote-empty":
    "This original has no readable text to quote. Attachments are not copied into a quote.",
  "plaintext-consent":
    "Approve sending this authored message and attachments unencrypted before using signing only.",
  "encryption-required":
    "This original is encrypted. Keep encryption enabled for the reply.",
  locked: "Unlock your local key to continue.",
  "bad-passphrase": "The key passphrase was not accepted.",
  "untrusted-recipient":
    "A recipient or your own address has no verified key. Import the public key and compare its full fingerprint in Local keys first.",
  "sender-key-mismatch":
    "The unlocked key does not match the verified key for the From address.",
  "unusable-key":
    "A key is expired, revoked or unsuitable for the selected protection.",
  "key-changed":
    "A recipient key changed. Compare and verify its fingerprint before sending.",
  "subject-limit": "Shorten the subject before saving or sending this message.",
  "invalid-recipients": "Enter valid email addresses for all recipients.",
  "decryption-failed":
    "Decryption failed. Check the selected key or download the original mail in another compatible client.",
  "invalid-signature":
    "The detached OpenPGP signature could not be parsed or verified. No valid-signature claim was made.",
  "unsupported-signature":
    "This signed MIME format is not supported. Download the original for another client.",
  "verification-unavailable":
    "Local signature verification requires HTTPS, browser cryptography and enabled local key storage.",
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
    "Choose up to 10 files, with at most 1 MiB total. The chosen protection applies to attachments.",
  "invalid-attachment-name":
    "Use attachment names up to 180 UTF-8 bytes without path separators or control characters.",
  "message-limit": "This message exceeds the supported size limit.",
  "invalid-mime": "The MIME structure is malformed or exceeds safety limits.",
};
async function perform(operation) {
  if (busy || !available) return;
  busy = true;
  const pendingButtons = [
    "mail-unlock",
    "mail-decrypt",
    "secure-send",
    "secure-quote-original",
    "draft-save",
    "draft-load",
    "draft-delete",
    "html-preview-open",
  ]
    .map(el)
    .filter(Boolean)
    .map((button) => [button, button.disabled]);
  for (const [button] of pendingButtons) button.disabled = true;
  draftButtons();
  if (!reading) el("secure-compose-fields").disabled = true;
  const current = epoch;
  let invalidControl;
  try {
    session();
    await operation(current);
  } catch (error) {
    if (error.code === "plaintext-consent")
      invalidControl = el("secure-plaintext-consent");
    if (current === epoch)
      notify(
        errors[error.code] ||
          "The operation could not finish. If a send was submitted, check Outbox before any new send.",
      );
  } finally {
    busy = false;
    for (const [button, disabled] of pendingButtons) button.disabled = disabled;
    draftButtons();
    el("mail-unlock").disabled =
      !el("mail-private-key").options.length ||
      Date.now() >= Number(root.dataset.sessionExpires);
    if (reading) el("mail-decrypt").disabled = !ordinary && vault?.locked;
    el("mail-passphrase").value = "";
    if (!reading)
      el("secure-compose-fields").disabled = vault?.locked || submitted;
    invalidControl?.focus();
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
  if (!ordinary) vault = createSessionVault(root.dataset.userId);
  const snapshot = ordinary ? { keys: [], locked: true } : await vault.list();
  vault?.addEventListener?.("lock", clearPrivate);
  const select = el("mail-private-key");
  for (const key of snapshot.keys.filter((key) => key.private)) {
    const option = document.createElement("option");
    option.value = key.fingerprint;
    option.textContent = `${key.identities.join(" · ")} · ${key.fingerprint}`;
    select.append(option);
  }
  if (snapshot.fingerprint) select.value = snapshot.fingerprint;
  el("mail-unlock").disabled = !select.options.length;
  notify(
    ordinary
      ? "Read as text; no local key is required. HTML preview is optional."
      : !snapshot.locked
        ? "Key session is already unlocked locally."
        : select.options.length
          ? "Unlock a key for this browser session. It stays in memory for up to 5 minutes of inactivity."
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
    el("mail-decrypt").disabled = !ordinary && snapshot.locked;
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
    resetProtection();
    for (const id of ["secure-encrypt", "secure-sign-only"])
      el(id).addEventListener("change", updateProtection);
    el("secure-plaintext-consent").addEventListener("input", () =>
      el("secure-plaintext-consent").removeAttribute("aria-invalid"),
    );
    el("secure-compose-fields").disabled = snapshot.locked || submitted;
    drafts = new LocalDrafts(root.dataset.userId);
    await draftList();
    draftButtons();
    el("secure-quote-original").addEventListener("click", () =>
      perform(async (current) => {
        if (submitted || quoteApplied) return;
        const source = { ...replyLocator };
        if (!source.folder || !source.uid || !source.validity)
          throw new VaultError("quote-source");
        request = new AbortController();
        const query = new URLSearchParams({
          account: root.dataset.account,
          folder: source.folder,
          uid: String(source.uid),
          validity: String(source.validity),
        });
        notify("Fetching the selected original for a local quote…");
        const response = await fetch("/api/mail/raw?" + query, {
          credentials: "same-origin",
          cache: "no-store",
          redirect: "error",
          signal: request.signal,
        });
        if (!response.ok) throw new VaultError("quote-source");
        const quote = await originalQuote(
          new Uint8Array(await response.arrayBuffer()),
          vault,
        );
        if (current !== epoch) return;
        const body = el("secure-body").value;
        const combined = (body ? body + "\n\n" : "") + quote.text;
        composeEntity(combined); // Apply the existing UTF-8/body bounds before DOM mutation.
        encryptionRequired ||= quote.encryptionRequired;
        resetProtection();
        el("secure-body").value = combined;
        quoteApplied = true;
        el("secure-quote-status").textContent = quote.signatureState;
        window.dispatchEvent?.(new Event("mailspace:compose-changed"));
        notify(
          "Original text quoted locally; attachments were not copied. " +
            (encryptionRequired
              ? "Encryption is required for this draft. "
              : "") +
            quote.signatureState,
        );
      }),
    );
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
        encryptionRequired = snapshot.encryptionRequired;
        // Only a verified local draft may replace the page's original-source
        // metadata. The private flag remains authoritative for decrypted quotes.
        root.dataset.encryptionRequired = String(encryptionRequired);
        quoteApplied = false;
        el("secure-quote-status").textContent = replyLocator.uid
          ? "Loaded reply draft. The original signature has not been rechecked in this session."
          : "";
        resetProtection();
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
  if (reading) {
    readMessage = () =>
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
        let result = ordinary
          ? { data: bytes, signatures: [], provenance: "ordinary-mail" }
          : await vault.decryptSigned(
              await extractEncrypted(bytes),
              root.dataset.sender,
            );
        const detached = await extractSigned(result.data);
        if (detached) {
          if (!isSecureContext || !crypto.subtle || !indexedDB)
            throw new VaultError("verification-unavailable");
          vault ??= createSessionVault(root.dataset.userId);
          const verified = await vault.verifyDetached(
            detached.data,
            detached.signature,
            root.dataset.sender,
          );
          result = {
            ...result,
            data: verified.data,
            signatures: [...result.signatures, ...verified.signatures],
          };
        }
        const parsed = await parseLocalMime(result.data);
        if (current !== epoch) return;
        el("decrypted-text").textContent = messageText(parsed);
        currentHTML = typeof parsed.html === "string" ? parsed.html : null;
        el("html-preview-open").hidden = !currentHTML;
        el("mail-signature").textContent =
          ordinary && !result.signatures.length
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
        attachmentViewer.render(parsed.attachments);
        el("decrypted-content").hidden = false;
        // Content is actually visible now: this is the explicit open that may
        // mark the message read on the provider.
        void markReaderRead(root);
        selectionClassifier?.enable();
        notify(
          ordinary
            ? "Text view ready. HTML appears only when you explicitly open its isolated preview."
            : "Decrypted locally. No content, attachment or signature payload was sent back to the server.",
        );
      });
    el("mail-decrypt").addEventListener("click", readMessage);
  } else
    el("secure-send").addEventListener("click", () =>
      perform(async (current) => {
        if (submitted) return;
        const signingOnly = el("secure-sign-only").checked === true;
        if (
          signingOnly &&
          (encryptionRequired || root.dataset.encryptionRequired === "true")
        )
          throw new VaultError("encryption-required");
        if (signingOnly && !el("secure-plaintext-consent").checked) {
          el("secure-plaintext-consent").setAttribute("aria-invalid", "true");
          throw new VaultError("plaintext-consent");
        }
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
        notify(
          signingOnly ? "Signing locally…" : "Signing and encrypting locally…",
        );
        const body = el("secure-body").value;
        const attachments = [
          ...restoredFiles,
          ...(await readAttachments(el("secure-files").files)),
        ];
        const entity = composeEntity(body, attachments);
        let protection;
        if (signingOnly) {
          const signed = await vault.signDetached(entity, root.dataset.from);
          protection = {
            signedMime: signedEntity(signed.data, signed.signature),
            provenance: "authored",
            allowPlaintext: true,
          };
        } else {
          const encrypted = await vault.encryptSigned(
            entity,
            root.dataset.from,
            to,
          );
          protection = { encryptedMime: encryptedEntity(encrypted) };
        }
        if (current !== epoch) return;
        // Only the chosen authored transport enters this request. Keys,
        // passphrases and decrypted messages have no source in this composer.
        const payload = JSON.stringify({
          ...metadata,
          ...protection,
        });
        session();
        submitted = true;
        request = new AbortController();
        notify(
          "Submitting once. Check Outbox before any new send if the connection is interrupted.",
        );
        const response = await fetch(
          signingOnly ? "/api/outbox/signed" : "/api/outbox/encrypted",
          {
            method: "POST",
            credentials: "same-origin",
            redirect: "error",
            headers: {
              "Content-Type": "application/json",
              "X-CSRF-Token": root.dataset.csrf,
            },
            body: payload,
            signal: request.signal,
          },
        );
        if (!response.ok) throw new Error("submission failed");
        const result = await response.json();
        if (
          typeof result.location !== "string" ||
          !result.location.startsWith("/outbox?")
        )
          throw new Error("invalid outcome");
        if (activeDraft) await drafts.remove(root.dataset.account, activeDraft);
        clearPrivate();
        window.dispatchEvent?.(
          new CustomEvent("mailspace:compose-sent", {
            detail: { state: result.state, location: result.location },
          }),
        );
        notify(
          result.state === "accepted"
            ? "Mail server accepted this message."
            : "Send outcome needs attention. Check Outbox before any new send.",
        );
        const outcomeLink = document.createElement("a");
        outcomeLink.href = result.location;
        outcomeLink.textContent = " Outbox details";
        status.append(outcomeLink);
        if (window.parent && window.parent !== window)
          window.parent.postMessage(
            {
              type: "mailspace:panel-sent",
              state: result.state,
              location: result.location,
            },
            location.origin,
          );
      }),
    );
  window.addEventListener("mailspace:lock", lock);
  window.addEventListener("mailspace:clear-private", clearPrivate);
  window.addEventListener("pagehide", () => {
    clearPrivate();
    void vault?.close();
  });
  document.addEventListener("visibilitychange", () => {
    if (document.hidden) clearPrivate();
    else if (vault && !vault.locked) {
      if (reading) el("mail-decrypt").disabled = false;
      else el("secure-compose-fields").disabled = submitted;
    }
  });
  for (const event of ["keydown", "pointerdown"])
    document.addEventListener(
      event,
      () => {
        lastActivity = Date.now();
        if (
          lastActivity - (vaultLastTouch || 0) > 15000 &&
          vault &&
          !vault.locked
        ) {
          vaultLastTouch = lastActivity;
          void vault.touch?.().catch(() => {});
        }
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
  if (reading && ordinary) await readMessage();
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
