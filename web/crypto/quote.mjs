// SPDX-License-Identifier: AGPL-3.0-only
import { messageText } from "./mail-html.mjs";
import { parseLocalMime, extractEncrypted } from "./mime.mjs";
import { extractSigned } from "./signed-mime.mjs";
import { VaultError } from "./errors.mjs";

// This module performs no network/storage/DOM writes. Its result belongs only to
// the client crypto composer; decrypted quotes must never become authored export.
export async function originalQuote(raw, vault) {
  const original = await parseLocalMime(raw);
  const types = original.headers.filter(
    (header) => header.key === "content-type",
  );
  if (types.length > 1) throw new VaultError("invalid-mime");
  const encrypted = /^multipart\/encrypted(?:\s*;|\s*$)/i.test(
    types[0]?.value || "",
  );
  const sender = original.from?.address || "";
  let result = encrypted
    ? await vault.decryptSigned(await extractEncrypted(raw), sender)
    : { data: raw, signatures: [] };
  const detached = await extractSigned(result.data);
  if (detached) {
    const verified = await vault.verifyDetached(
      detached.data,
      detached.signature,
      sender,
    );
    result = {
      data: verified.data,
      signatures: [...result.signatures, ...verified.signatures],
    };
  }
  if (result.signatures.some((signature) => signature.status === "invalid"))
    throw new VaultError("invalid-signature");
  const parsed = await parseLocalMime(result.data);
  if (!parsed.text?.trim() && !parsed.html?.trim())
    throw new VaultError("quote-empty");
  const text = messageText(parsed).replace(/\r\n|\r/g, "\n");
  if (!text.trim()) throw new VaultError("quote-empty");
  if (new TextEncoder().encode(text).length > 32 << 10 || text.includes("\0"))
    throw new VaultError("quote-limit");
  const signatureState = !result.signatures.length
    ? "Unsigned original; sender identity is not verified."
    : result.signatures.every(
          (signature) =>
            signature.status === "valid" && signature.trustedSender,
        )
      ? "Valid signature with a verified sender fingerprint."
      : result.signatures.some((signature) => signature.status !== "valid")
        ? "Unknown signing key; original signature is not verified."
        : "Valid signature; original sender identity is not trusted.";
  return {
    text: text
      .split("\n")
      .map((line) => "> " + line)
      .join("\n"),
    encryptionRequired: encrypted,
    provenance: encrypted ? "client-decrypted" : "ordinary-mail",
    signatureState,
  };
}
