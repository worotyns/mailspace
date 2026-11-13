// SPDX-License-Identifier: AGPL-3.0-only
// Locate the original signed MIME entity without reconstructing or decoding it.
import { parseLocalMime } from "./mime.mjs";
import { VaultError } from "./errors.mjs";
const encoder = new TextEncoder();
const decoder = new TextDecoder();
function octetString(bytes) {
  let result = "";
  for (let offset = 0; offset < bytes.length; offset += 32768)
    result += String.fromCharCode(...bytes.subarray(offset, offset + 32768));
  return result;
}
export async function extractSigned(raw) {
  const parsed = await parseLocalMime(raw);
  const types = parsed.headers.filter(
    (header) => header.key === "content-type",
  );
  if (
    !types.some((header) =>
      /^multipart\/signed(?:\s*;|\s*$)/i.test(header.value),
    )
  )
    return null;
  if (types.length !== 1) throw new VaultError("invalid-mime");
  const type = types[0].value;
  const protocols = [
    ...type.matchAll(/;\s*protocol\s*=\s*(?:"([^"]*)"|([^;\s]+))/gi),
  ];
  const boundaries = [
    ...type.matchAll(/;\s*boundary\s*=\s*(?:"([^"]*)"|([^;\s]+))/gi),
  ];
  if (
    protocols.length !== 1 ||
    (protocols[0][1] ?? protocols[0][2]).toLowerCase() !==
      "application/pgp-signature"
  )
    throw new VaultError("unsupported-signature");
  const boundary = boundaries[0]?.[1] ?? boundaries[0]?.[2];
  if (
    boundaries.length !== 1 ||
    !boundary ||
    boundary.length > 70 ||
    !/^[0-9a-z'()+_,.\/:=? -]+$/i.test(boundary) ||
    boundary.endsWith(" ")
  )
    throw new VaultError("invalid-mime");
  const source = octetString(raw);
  const headerEnd = source.indexOf("\r\n\r\n");
  // MIME canonicalization must be done by the sender, not a verification renderer.
  // Refuse non-canonical envelope line endings rather than guessing signed bytes.
  if (headerEnd < 0 || headerEnd > 64 << 10)
    throw new VaultError("invalid-mime");
  const escaped = boundary.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const delimiter = new RegExp(
    `(?:^|\\r\\n)--${escaped}(--)?[ \\t]*(?:\\r\\n|$)`,
    "g",
  );
  delimiter.lastIndex = headerEnd + 2;
  const markers = [...source.matchAll(delimiter)];
  if (
    markers.length !== 3 ||
    markers[0][1] ||
    markers[1][1] ||
    markers[2][1] !== "--"
  )
    throw new VaultError("invalid-mime");
  const data = raw.slice(
    markers[0].index + markers[0][0].length,
    markers[1].index,
  );
  const signaturePart = raw.slice(
    markers[1].index + markers[1][0].length,
    markers[2].index,
  );
  if (!data.length) throw new VaultError("invalid-mime");
  const signatureMIME = await parseLocalMime(signaturePart);
  const signatureTypes = signatureMIME.headers.filter(
    (header) => header.key === "content-type",
  );
  if (
    signatureTypes.length !== 1 ||
    !/^application\/pgp-signature(?:\s*;|\s*$)/i.test(
      signatureTypes[0].value,
    ) ||
    signatureMIME.attachments.length !== 1
  )
    throw new VaultError("invalid-mime");
  const signature = new Uint8Array(signatureMIME.attachments[0].content);
  if (!signature.length || signature.length > 256 << 10)
    throw new VaultError("message-limit");
  const prefix = encoder.encode("-----BEGIN PGP SIGNATURE-----");
  return {
    data,
    signature: prefix.every((value, index) => signature[index] === value)
      ? decoder.decode(signature)
      : signature,
  };
}
