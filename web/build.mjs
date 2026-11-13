// SPDX-License-Identifier: AGPL-3.0-only
import { previewCSS } from "./crypto/preview-css.mjs";
import { mkdir, readFile, writeFile, readdir } from "node:fs/promises";
const out = "../internal/webui/assets";
await mkdir(out, { recursive: true });
await writeFile(
  `${out}/htmx.min.js`,
  await readFile("node_modules/htmx.org/dist/htmx.min.js"),
);
const names = [
  "calendar",
  "tag",
  "filter",
  "hash",
  "inbox",
  "star",
  "send",
  "archive",
  "search",
  "chevron-down",
  "chevron-right",
  "chevron-left",
  "plus",
  "lock",
  "shield-lock",
  "x",
  "dots",
  "message-circle",
  "paperclip",
  "file-text",
  "arrow-up-right",
  "mail",
  "layout-list",
  "messages",
  "check",
  "arrow-down",
  "settings",
  "pencil",
  "at",
  "users",
  "circle",
  "download",
];
const symbols = [];
for (const name of names) {
  const svg = await readFile(
    `node_modules/@tabler/icons/icons/outline/${name}.svg`,
    "utf8",
  );
  const body = svg
    .replace(/^[\s\S]*?<svg[^>]*>/, "")
    .replace(/<\/svg>[\s\S]*$/, "");
  symbols.push(
    `<symbol id="${name}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">${body}</symbol>`,
  );
}
await writeFile(
  `${out}/icons.svg`,
  `<svg xmlns="http://www.w3.org/2000/svg">${symbols.join("")}</svg>`,
);
const notices = [];
for (const pkg of [
  "htmx.org",
  "@tabler/icons",
  "tailwindcss",
  "openpgp",
  "idb",
  "postal-mime",
  "dompurify",
]) {
  const files = await readdir(`node_modules/${pkg}`);
  const file = files.find((f) => /^licen[cs]e([.-]|$)/i.test(f));
  if (!file) throw new Error(`Missing license for ${pkg}`);
  notices.push(
    `=== ${pkg} ===\n${await readFile(`node_modules/${pkg}/${file}`, "utf8")}`,
  );
}
notices.push(await readFile("../licenses/OpenPGP-bundled-NOTICES.txt", "utf8"));
notices.push(await readFile("../licenses/GPL-3.0.txt", "utf8"));
await writeFile(`${out}/THIRD_PARTY_NOTICES.txt`, notices.join("\n\n"));

await mkdir(`${out}/vendor/openpgp`, { recursive: true });
for (const file of ["openpgp.min.mjs", "openpgp.min.mjs.map", "openpgp.mjs"]) {
  await writeFile(
    `${out}/vendor/openpgp/${file}`,
    await readFile(`node_modules/openpgp/dist/${file}`),
  );
}
await mkdir(`${out}/vendor/idb`, { recursive: true });
await writeFile(
  `${out}/vendor/idb/index.js`,
  await readFile("node_modules/idb/build/index.js"),
);
await mkdir(`${out}/crypto`, { recursive: true });
for (const file of await readdir("crypto")) {
  if (file.endsWith(".mjs"))
    await writeFile(`${out}/crypto/${file}`, await readFile(`crypto/${file}`));
}

await mkdir(`${out}/vendor/postal-mime`, { recursive: true });
for (const file of await readdir("node_modules/postal-mime/dist/esm")) {
  if (file.endsWith(".js"))
    await writeFile(
      `${out}/vendor/postal-mime/${file}`,
      await readFile(`node_modules/postal-mime/dist/esm/${file}`),
    );
}

await mkdir(`${out}/vendor/dompurify`, { recursive: true });
for (const file of ["purify.es.mjs", "purify.es.mjs.map"])
  await writeFile(
    `${out}/vendor/dompurify/${file}`,
    await readFile(`node_modules/dompurify/dist/${file}`),
  );

await writeFile(`${out}/crypto/preview.css`, previewCSS);
