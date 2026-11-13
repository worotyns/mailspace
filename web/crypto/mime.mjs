// SPDX-License-Identifier: AGPL-3.0-only
import PostalMime from "../vendor/postal-mime/postal-mime.js";
import { VaultError } from "./errors.mjs";
const encoder = new TextEncoder();
const decoder = new TextDecoder();
export const mimeLimits = Object.freeze({
  maxNestingDepth: 12,
  maxPartCount: 100,
  maxHeadersSize: 64 << 10,
  maxRfc822NestingDepth: 0,
  forceRfc822Attachments: true,
});
export async function parseLocalMime(raw) {
  if (!(raw instanceof Uint8Array) || raw.length > 4 << 20)
    throw new VaultError("message-limit");
  try {
    return await PostalMime.parse(raw, mimeLimits);
  } catch {
    throw new VaultError("invalid-mime");
  }
}
export function textEntity(text) {
  if (
    typeof text !== "string" ||
    encoder.encode(text).length > 256 << 10 ||
    text.includes("\0")
  )
    throw new VaultError("message-limit");
  const bytes = encoder.encode(text.replace(/\r\n|\r|\n/g, "\r\n"));
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  const body =
    btoa(binary)
      .match(/.{1,76}/g)
      ?.join("\r\n") || "";
  return encoder.encode(
    `Content-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n${body}\r\n`,
  );
}
export function encryptedEntity(armored) {
  if (
    typeof armored !== "string" ||
    !armored.startsWith("-----BEGIN PGP MESSAGE-----") ||
    encoder.encode(armored).length > 6 << 20
  )
    throw new VaultError("invalid-mime");
  const boundary =
    "mailspace-" +
    Array.from(crypto.getRandomValues(new Uint8Array(16)), (n) =>
      n.toString(16).padStart(2, "0"),
    ).join("");
  const ciphertext = armored.replace(/\r\n|\r|\n/g, "\r\n").trimEnd();
  return `MIME-Version: 1.0\r\nContent-Type: multipart/encrypted; protocol="application/pgp-encrypted";\r\n boundary="${boundary}"\r\n\r\n--${boundary}\r\nContent-Type: application/pgp-encrypted\r\n\r\nVersion: 1\r\n\r\n--${boundary}\r\nContent-Type: application/octet-stream\r\nContent-Disposition: inline; filename="encrypted.asc"\r\n\r\n${ciphertext}\r\n--${boundary}--\r\n`;
}
export async function extractEncrypted(raw) {
  const parsed = await parseLocalMime(raw);
  const types = parsed.headers.filter((h) => h.key === "content-type");
  if (
    types.length !== 1 ||
    !/^multipart\/encrypted\s*;/i.test(types[0].value) ||
    !/;\s*protocol\s*=\s*(?:"application\/pgp-encrypted"|application\/pgp-encrypted)(?:\s*;|\s*$)/i.test(
      types[0].value,
    )
  )
    throw new VaultError("unsupported-encryption");
  const [control, data] = parsed.attachments;
  if (
    parsed.attachments.length !== 2 ||
    control?.mimeType !== "application/pgp-encrypted" ||
    data?.mimeType !== "application/octet-stream" ||
    decoder.decode(control.content).trim() !== "Version: 1"
  )
    throw new VaultError("invalid-mime");
  const bytes = new Uint8Array(data.content);
  if (!bytes.length || bytes.length > 4 << 20)
    throw new VaultError("message-limit");
  // Preserve binary packet octets: decoding ciphertext through UTF-8 corrupts it.
  // OpenPGP.js validates packet structure and authenticated encryption locally.
  const armorPrefix = encoder.encode("-----BEGIN PGP MESSAGE-----");
  if (armorPrefix.every((value, index) => bytes[index] === value))
    return decoder.decode(bytes);
  if ((bytes[0] & 0x80) === 0) throw new VaultError("unsupported-encryption");
  return bytes;
}

function base64Lines(data) {
  let binary = "";
  for (let offset = 0; offset < data.length; offset += 32768)
    binary += String.fromCharCode(...data.subarray(offset, offset + 32768));
  return (
    btoa(binary)
      .match(/.{1,76}/g)
      ?.join("\r\n") || ""
  );
}
export async function readAttachments(files) {
  files = Array.from(files || []);
  if (
    files.length > 10 ||
    files.some((file) => !Number.isSafeInteger(file.size) || file.size < 0) ||
    files.reduce((sum, file) => sum + file.size, 0) > 1 << 20
  )
    throw new VaultError("attachment-limit");
  const result = [];
  for (const file of files)
    result.push({
      name: file.name,
      data: new Uint8Array(await file.arrayBuffer()),
    });
  return result;
}
export function composeEntity(text, attachments = []) {
  const plain = textEntity(text);
  if (!attachments.length) return plain;
  if (
    attachments.length > 10 ||
    attachments.some((file) => !(file.data instanceof Uint8Array)) ||
    attachments.reduce((sum, file) => sum + file.data.length, 0) > 1 << 20
  )
    throw new VaultError("attachment-limit");
  const boundary =
    "mailspace-inner-" +
    Array.from(crypto.getRandomValues(new Uint8Array(16)), (n) =>
      n.toString(16).padStart(2, "0"),
    ).join("");
  let result = `Content-Type: multipart/mixed; boundary="${boundary}"\r\n\r\n--${boundary}\r\n${decoder.decode(plain)}`;
  for (const file of attachments) {
    if (
      typeof file.name !== "string" ||
      !file.name ||
      encoder.encode(file.name).length > 180 ||
      /[\x00-\x1f\x7f\/\\]/.test(file.name)
    )
      throw new VaultError("invalid-attachment-name");
    const filename = encodeURIComponent(file.name).replace(
      /[!'()*]/g,
      (char) => "%" + char.charCodeAt(0).toString(16).toUpperCase(),
    );
    result += `\r\n--${boundary}\r\nContent-Type: application/octet-stream\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: attachment;\r\n filename*=utf-8''${filename}\r\n\r\n${base64Lines(file.data)}\r\n`;
  }
  return encoder.encode(`${result}\r\n--${boundary}--\r\n`);
}
