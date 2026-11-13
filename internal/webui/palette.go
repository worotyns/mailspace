// SPDX-License-Identifier: AGPL-3.0-only
package webui

type PaletteColor struct{ Value, Name, Tone string }

var spacePalette = []PaletteColor{
	{"lilac", "Lilac", "bg-violet-100 text-violet-800"},
	{"blue", "Blue", "bg-blue-100 text-blue-800"},
	{"green", "Green", "bg-emerald-100 text-emerald-800"},
	{"peach", "Peach", "bg-orange-100 text-orange-800"},
	{"rose", "Rose", "bg-rose-100 text-rose-800"},
	{"amber", "Amber", "bg-amber-100 text-amber-800"},
	{"red", "Red", "bg-red-100 text-red-800"},
	{"pink", "Pink", "bg-pink-100 text-pink-800"},
	{"fuchsia", "Fuchsia", "bg-fuchsia-100 text-fuchsia-800"},
	{"purple", "Purple", "bg-purple-100 text-purple-800"},
	{"indigo", "Indigo", "bg-indigo-100 text-indigo-800"},
	{"cyan", "Cyan", "bg-cyan-100 text-cyan-800"},
	{"teal", "Teal", "bg-teal-100 text-teal-800"},
	{"lime", "Lime", "bg-lime-100 text-lime-800"},
	{"yellow", "Yellow", "bg-yellow-100 text-yellow-800"},
	{"slate", "Slate", "bg-slate-100 text-slate-800"},
}

func (f CollectionForm) Colors() []PaletteColor { return spacePalette }
