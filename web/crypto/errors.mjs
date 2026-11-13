// SPDX-License-Identifier: AGPL-3.0-only
export class VaultError extends Error {
  constructor(code) {
    super(code);
    this.name = "VaultError";
    this.code = code;
  }
}
