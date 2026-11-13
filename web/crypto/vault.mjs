// SPDX-License-Identifier: AGPL-3.0-only
import * as pgp from "../vendor/openpgp/openpgp.min.mjs";
import { VaultError } from "./errors.mjs";

export { VaultError };
export const cryptoConfig = Object.freeze({
  ...pgp.config,
  v6Keys: false,
  aeadProtect: false,
  preferredCompressionAlgorithm: pgp.enums.compression.uncompressed,
  preferredHashAlgorithm: pgp.enums.hash.sha512,
  s2kType: pgp.enums.s2k.iterated,
  s2kIterationCountByte: 224,
  maxArgon2MemoryExponent: 16,
  maxUserIDLength: 1024,
  maxDecompressedMessageSize: 4 << 20,
  allowUnauthenticatedMessages: false,
  allowUnauthenticatedStream: false,
});
const encoder = new TextEncoder();
export function normalizeFingerprint(value) {
  const result = String(value).replace(/\s/g, "").toUpperCase();
  if (!/^(?:[A-F0-9]{40}|[A-F0-9]{64})$/.test(result))
    throw new VaultError("invalid-fingerprint");
  return result;
}
function email(value) {
  const result = String(value).trim();
  if (
    result.length > 254 ||
    !/^[^\s<>@\x00-\x20]+@[^\s<>@\x00-\x20]+\.[^\s<>@\x00-\x20]+$/.test(result)
  )
    throw new VaultError("invalid-email");
  const at = result.lastIndexOf("@");
  return result.slice(0, at) + "@" + result.slice(at + 1).toLowerCase();
}
function armor(value) {
  if (typeof value !== "string" || encoder.encode(value).length > 2 << 20)
    throw new VaultError("invalid-key");
  return value;
}
function newPassphrase(value) {
  if (
    typeof value !== "string" ||
    [...value].length < 12 ||
    encoder.encode(value).length > 1024
  )
    throw new VaultError("unsafe-passphrase");
}
async function publicInfo(key) {
  let status = "usable";
  try {
    await key.verifyPrimaryKey(undefined, undefined, cryptoConfig);
  } catch {
    status = "unusable";
  }
  if (await key.isRevoked(undefined, undefined, undefined, cryptoConfig))
    status = "revoked";
  const expiry = await key
    .getExpirationTime(undefined, cryptoConfig)
    .catch(() => null);
  if (
    status !== "revoked" &&
    expiry instanceof Date &&
    expiry.getTime() <= Date.now()
  )
    status = "expired";
  const identities = key.getUserIDs().slice(0, 20);
  return {
    fingerprint: normalizeFingerprint(key.getFingerprint()),
    identities,
    status,
    expires: expiry instanceof Date ? expiry.toISOString() : null,
  };
}

// No network API and no serialization of unlocked state. Raw private key objects
// stay in private class fields; persisted secret-key packets are always protected.
export class LocalVault {
  #store;
  #unlocked = null;
  #epoch = 0;
  constructor(store) {
    this.#store = store;
  }
  get locked() {
    return !this.#unlocked;
  }
  get fingerprint() {
    return this.#unlocked
      ? normalizeFingerprint(this.#unlocked.getFingerprint())
      : null;
  }
  lock() {
    this.#epoch++;
    this.#unlocked = null;
  }
  async close() {
    this.lock();
    await this.#store.close();
  }
  async list() {
    const privates = await this.#store.privateKeys();
    const publicKeys = await this.#store.publicKeys();
    const result = [];
    for (const record of publicKeys) {
      try {
        const key = await pgp.readKey({
          armoredKey: armor(record.armoredKey),
          config: cryptoConfig,
        });
        result.push({
          ...(await publicInfo(key)),
          private: privates.some((p) => p.fingerprint === record.fingerprint),
          unlocked: this.fingerprint === record.fingerprint,
        });
      } catch {
        result.push({
          fingerprint: record.fingerprint,
          identities: [],
          status: "unusable",
          private: privates.some((p) => p.fingerprint === record.fingerprint),
        });
      }
    }
    return {
      keys: result,
      bindings: await this.#store.bindings(),
      locked: this.locked,
      fingerprint: this.fingerprint,
    };
  }
  async generate(name, address, passphrase) {
    const epoch = this.#epoch;
    newPassphrase(passphrase);
    address = email(address);
    if (!name.trim() || name.length > 120 || /[\x00\r\n]/.test(name))
      throw new VaultError("invalid-name");
    const generated = await pgp.generateKey({
      type: "ecc",
      curve: "curve25519Legacy",
      userIDs: [{ name: name.trim(), email: address }],
      passphrase,
      keyExpirationTime: 2 * 365 * 86400,
      format: "armored",
      config: cryptoConfig,
    });
    if (epoch !== this.#epoch) throw new VaultError("locked");
    const key = await pgp.readPrivateKey({
      armoredKey: generated.privateKey,
      config: cryptoConfig,
    });
    if (key.isDecrypted()) throw new VaultError("unprotected-key");
    const fingerprint = normalizeFingerprint(key.getFingerprint());
    await this.#store.putPrivate(
      { fingerprint, armoredKey: generated.privateKey },
      { fingerprint, armoredKey: generated.publicKey },
    );
    // Revocation authority is deliberately not persisted next to the protected
    // key. The UI offers its explicit local download while this result is alive.
    return {
      fingerprint,
      publicKey: generated.publicKey,
      revocationCertificate: generated.revocationCertificate,
    };
  }
  async importPrivate(value, passphrase) {
    this.lock();
    let key;
    try {
      key = await pgp.readPrivateKey({
        armoredKey: armor(value),
        config: cryptoConfig,
      });
    } catch {
      throw new VaultError("invalid-key");
    }
    if (key.isDecrypted()) {
      newPassphrase(passphrase);
      if (
        key
          .getKeys()
          .some(
            ({ keyPacket }) =>
              typeof keyPacket.isDecrypted === "function" &&
              !keyPacket.isDecrypted(),
          )
      ) {
        throw new VaultError("mixed-key-protection");
      }
      key = await pgp.encryptKey({
        privateKey: key,
        passphrase,
        config: cryptoConfig,
      });
    } else {
      // Validate the supplied password but never retain this temporary unlocked
      // object or replace protected armor with its plaintext serialization.
      try {
        await pgp.decryptKey({
          privateKey: key,
          passphrase,
          config: cryptoConfig,
        });
      } catch {
        throw new VaultError("bad-passphrase");
      }
    }
    if (key.isDecrypted()) throw new VaultError("unprotected-key");
    const fingerprint = normalizeFingerprint(key.getFingerprint());
    await this.#store.putPrivate(
      { fingerprint, armoredKey: key.armor(cryptoConfig) },
      { fingerprint, armoredKey: key.toPublic().armor(cryptoConfig) },
    );
    return fingerprint;
  }
  async importPublic(value) {
    let key;
    try {
      key = await pgp.readKey({
        armoredKey: armor(value),
        config: cryptoConfig,
      });
    } catch {
      throw new VaultError("invalid-key");
    }
    if (key.isPrivate()) throw new VaultError("private-key-in-public-import");
    const fingerprint = normalizeFingerprint(key.getFingerprint());
    await this.#store.putPublic({
      fingerprint,
      armoredKey: key.armor(cryptoConfig),
    });
    return fingerprint;
  }
  async unlock(fingerprint, passphrase) {
    this.lock();
    const epoch = this.#epoch;
    const record = await this.#store.privateKey(
      normalizeFingerprint(fingerprint),
    );
    if (!record) throw new VaultError("missing-key");
    let key;
    try {
      const protectedKey = await pgp.readPrivateKey({
        armoredKey: armor(record.armoredKey),
        config: cryptoConfig,
      });
      if (protectedKey.isDecrypted()) throw new VaultError("unprotected-key");
      key = await pgp.decryptKey({
        privateKey: protectedKey,
        passphrase,
        config: cryptoConfig,
      });
    } catch (error) {
      if (error instanceof VaultError) throw error;
      throw new VaultError("bad-passphrase");
    }
    if (epoch !== this.#epoch) throw new VaultError("locked");
    this.#unlocked = key;
    return this.fingerprint;
  }
  async exportPrivate(fingerprint) {
    const record = await this.#store.privateKey(
      normalizeFingerprint(fingerprint),
    );
    if (!record) throw new VaultError("missing-key");
    const key = await pgp.readPrivateKey({
      armoredKey: armor(record.armoredKey),
      config: cryptoConfig,
    });
    if (key.isDecrypted()) throw new VaultError("unprotected-key");
    return record.armoredKey;
  }
  async exportPublic(fingerprint) {
    const record = await this.#store.publicKey(
      normalizeFingerprint(fingerprint),
    );
    if (!record) throw new VaultError("missing-key");
    const key = await pgp.readKey({
      armoredKey: armor(record.armoredKey),
      config: cryptoConfig,
    });
    return key.toPublic().armor(cryptoConfig);
  }
  async trust(address, fingerprint, typedFingerprint, replace = false) {
    address = email(address);
    fingerprint = normalizeFingerprint(fingerprint);
    if (normalizeFingerprint(typedFingerprint) !== fingerprint)
      throw new VaultError("fingerprint-mismatch");
    const record = await this.#store.publicKey(fingerprint);
    if (!record) throw new VaultError("missing-key");
    const key = await pgp.readKey({
      armoredKey: armor(record.armoredKey),
      config: cryptoConfig,
    });
    try {
      const certifiedAddress = key.users
        .map((user) => user.userID?.email)
        .find((value) => {
          try {
            return value && email(value) === address;
          } catch {
            return false;
          }
        });
      if (!certifiedAddress) throw new VaultError("identity-mismatch");
      await key.verifyPrimaryKey(
        undefined,
        { email: certifiedAddress },
        cryptoConfig,
      );
      const primary = await key.getPrimaryUser(
        undefined,
        { email: certifiedAddress },
        cryptoConfig,
      );
      if (email(primary.user.userID.email) !== address)
        throw new VaultError("identity-mismatch");
      await key.getEncryptionKey(
        undefined,
        undefined,
        { email: certifiedAddress },
        cryptoConfig,
      );
    } catch {
      throw new VaultError("unusable-key");
    }
    await this.#store.trust(address, fingerprint, replace);
  }
  // Inputs and decrypted results stay in client memory. Only the caller's
  // explicit ciphertext submission boundary may transmit the encrypted result.
  async encryptSigned(data, from, recipients) {
    if (!this.#unlocked) throw new VaultError("locked");
    if (!(data instanceof Uint8Array) || data.length > 4 << 20)
      throw new VaultError("message-limit");
    const epoch = this.#epoch;
    from = email(from);
    if (
      !Array.isArray(recipients) ||
      !recipients.length ||
      recipients.length > 100
    )
      throw new VaultError("invalid-recipients");
    const addresses = [...new Set([from, ...recipients.map(email)])];
    const keys = [],
      bindings = [];
    for (const address of addresses) {
      const binding = await this.#store.binding(address);
      if (!binding) throw new VaultError("untrusted-recipient");
      // Revalidate current expiry/revocation/identity, even for a saved binding.
      await this.trust(address, binding.fingerprint, binding.fingerprint);
      if (epoch !== this.#epoch) throw new VaultError("locked");
      if (address === from && binding.fingerprint !== this.fingerprint)
        throw new VaultError("sender-key-mismatch");
      keys.push(
        await pgp.readKey({
          armoredKey: await this.exportPublic(binding.fingerprint),
          config: cryptoConfig,
        }),
      );
      bindings.push(binding);
    }
    if (epoch !== this.#epoch || !this.#unlocked)
      throw new VaultError("locked");
    let encrypted;
    try {
      encrypted = await pgp.encrypt({
        message: await pgp.createMessage({ binary: data }),
        encryptionKeys: keys,
        signingKeys: this.#unlocked,
        wildcard: true, // Do not expose Bcc recipient key IDs in PKESK packets.
        format: "armored",
        config: cryptoConfig,
      });
    } catch {
      throw new VaultError("encryption-failed");
    }
    for (const binding of bindings) {
      if (
        (await this.#store.binding(binding.email))?.fingerprint !==
        binding.fingerprint
      )
        throw new VaultError("key-changed");
    }
    if (epoch !== this.#epoch) throw new VaultError("locked");
    return encrypted;
  }
  async decryptSigned(armoredMessage, claimedSender) {
    if (!this.#unlocked) throw new VaultError("locked");
    if (
      !(
        typeof armoredMessage === "string" ||
        armoredMessage instanceof Uint8Array
      ) ||
      (typeof armoredMessage === "string"
        ? encoder.encode(armoredMessage).length
        : armoredMessage.length) >
        6 << 20
    )
      throw new VaultError("message-limit");
    const epoch = this.#epoch;
    const knownKeys = await this.#verificationKeys();
    let result;
    try {
      result = await pgp.decrypt({
        message: await pgp.readMessage({
          ...(typeof armoredMessage === "string"
            ? { armoredMessage }
            : { binaryMessage: armoredMessage }),
          config: cryptoConfig,
        }),
        decryptionKeys: this.#unlocked,
        verificationKeys: knownKeys,
        format: "binary",
        config: cryptoConfig,
      });
    } catch {
      throw new VaultError("decryption-failed");
    }
    if (!(result.data instanceof Uint8Array) || result.data.length > 4 << 20)
      throw new VaultError("message-limit");
    const signatures = await this.#signatureResults(
      result.signatures,
      knownKeys,
      claimedSender,
    );
    if (epoch !== this.#epoch) throw new VaultError("locked");
    return { provenance: "client-decrypted", data: result.data, signatures };
  }
  async #verificationKeys() {
    const knownKeys = [];
    for (const record of await this.#store.publicKeys()) {
      try {
        knownKeys.push(
          await pgp.readKey({
            armoredKey: record.armoredKey,
            config: cryptoConfig,
          }),
        );
      } catch {
        /* An unusable public contact cannot validate a signature. */
      }
    }
    return knownKeys;
  }
  async #signatureResults(rawSignatures, knownKeys, claimedSender) {
    const signatures = [];
    let binding;
    try {
      binding = await this.#store.binding(email(claimedSender));
    } catch {
      /* Untrusted outer identity. */
    }
    for (const signature of rawSignatures) {
      const key = knownKeys.find((key) =>
        key.getKeyIDs().some((id) => id.equals(signature.keyID)),
      );
      let valid = false;
      try {
        await signature.verified;
        valid = true;
      } catch {
        /* Bad and unknown signatures never become green. */
      }
      const fingerprint = key
        ? normalizeFingerprint(key.getFingerprint())
        : null;
      signatures.push({
        status: valid ? "valid" : key ? "invalid" : "unknown-key",
        fingerprint,
        trustedSender:
          valid && !!binding && binding.fingerprint === fingerprint,
      });
    }
    return signatures;
  }
  async verifyDetached(data, signature, claimedSender) {
    if (
      !(data instanceof Uint8Array) ||
      data.length > 4 << 20 ||
      !(typeof signature === "string" || signature instanceof Uint8Array) ||
      (typeof signature === "string"
        ? encoder.encode(signature).length
        : signature.length) >
        256 << 10
    )
      throw new VaultError("message-limit");
    const epoch = this.#epoch;
    const knownKeys = await this.#verificationKeys();
    let result;
    try {
      result = await pgp.verify({
        message: await pgp.createMessage({ binary: data }),
        signature: await pgp.readSignature({
          ...(typeof signature === "string"
            ? { armoredSignature: signature }
            : { binarySignature: signature }),
          config: cryptoConfig,
        }),
        verificationKeys: knownKeys,
        format: "binary",
        config: cryptoConfig,
      });
    } catch {
      throw new VaultError("invalid-signature");
    }
    const signatures = await this.#signatureResults(
      result.signatures,
      knownKeys,
      claimedSender,
    );
    if (epoch !== this.#epoch) throw new VaultError("locked");
    return { data, signatures };
  }
  async untrust(address) {
    await this.#store.untrust(email(address));
  }
}
