// SPDX-License-Identifier: AGPL-3.0-only
// Missing palette source in the production asset stage must fail the build.
import { readFile } from "node:fs/promises";
const source = await readFile("../internal/webui/palette.go", "utf8");
const css = await readFile("../internal/webui/assets/app.css", "utf8");
const tones = [...source.matchAll(/"(bg-[a-z]+-100) (text-[a-z]+-800)"/g)];
if (tones.length !== 16) throw new Error("Expected sixteen palette tones");
for (const [, background, text] of tones) {
  if (!css.includes(`.${background}`) || !css.includes(`.${text}`))
    throw new Error(`Production CSS is missing palette classes: ${background}, ${text}`);
}
console.log("Verified all sixteen palette background/text pairs");
