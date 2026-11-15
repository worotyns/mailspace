// SPDX-License-Identifier: AGPL-3.0-only
// Reuse only the reviewed, local Tabler application sprite; never mail SVG.
const names = new Set(["x", "eye", "eye-off", "download"]);
export function controlIcon(document, control, name, label) {
  if (!names.has(name)) throw new Error("Unsupported application icon");
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.setAttribute("class", "icon");
  svg.setAttribute("aria-hidden", "true");
  const use = document.createElementNS("http://www.w3.org/2000/svg", "use");
  use.setAttribute("href", "#" + name);
  svg.append(use);
  control.replaceChildren(svg);
  control.setAttribute("aria-label", label);
  control.title = label;
}
