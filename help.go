package main

import (
	"fmt"
	"strings"
)

// help colors, the same 256-color palette as the other tools' help
const (
	hTitle   = "\x1b[38;5;183;1m"
	hDim     = "\x1b[38;5;59m"
	hTagline = "\x1b[38;5;146;3m"
	hSection = "\x1b[38;5;111;1m"
	hShort   = "\x1b[38;5;151m"
	hLong    = "\x1b[38;5;117m"
	hText    = "\x1b[38;5;189m"
	hReset   = "\x1b[0m"
)

type helpFlag struct{ short, long, arg, text string }

type helpPair struct{ name, text string }

var helpBlocks = []string{
	`One Markdown file for a notebook AI (NotebookLM, a chat's file upload):
a short header, a map of every file with its line count and main
definitions, the last commits with the definitions each one touched,
then every file in full under its path.`,
	`It packs what git tracks. Lockfiles, vendored code (vendor/,
node_modules/, linguist-vendored), generated and minified files, license
texts, images, fonts, media, binaries and files over the size limit are
left out, and the header counts them.`,
	`A repository bigger than --max-chars is split into parts (name.md,
name-2.md, ...); the default fits one part into a Google Doc. -o also
writes pack.json, listing each part with its SHA-256.`,
}

var helpSections = []struct {
	title string
	flags []helpFlag
}{
	{"WHAT TO PACK", []helpFlag{
		{"-x", "--exclude", "PATTERN", "leave out more: a name (testdata, *.golden) or a path from the top (site/src)"},
		{"-c", "--commits", "N", "recent commits to list (default 20; 0: none)"},
		{"", "--max-file", "KB", "leave out files bigger than this (default 256; 0: no limit)"},
	}},
	{"OUTPUT", []helpFlag{
		{"-o", "", "FOLDER", "write NAME.md (and parts) and pack.json there; needed for several repos"},
		{"", "--max-chars", "N", "characters per part (default 1,000,000; 0: one part)"},
		{"-h", "--help", "", "show this help"},
		{"-V", "--version", "", "show the version"},
	}},
}

var helpExamples = []helpPair{
	{"pack . > repo.md", "this repository, as one file"},
	{"pack Vaspyyy/glint > glint.md", "a GitHub repo (a shallow clone)"},
	{"pack -o out ~/src/a owner/b", "several, one file (or a few) each"},
	{"pack -x testdata -x docs .", "leave out more"},
}

var helpNotes = []helpPair{
	{"REPO", "a folder in a git repository (a subfolder packs only itself), or owner/repo, github.com/owner/repo, a GitHub URL"},
	{"exit code", "0 packed, 2 bad input or a repository that can't be read (then nothing is written)"},
	{"NO_COLOR", "set it to turn colors off"},
}

// helpText renders the help, colored like the other tools' when color is true.
func helpText(color bool) string {
	paint := func(code, s string) string {
		if !color || s == "" {
			return s
		}
		return code + s + hReset
	}
	const flagWidth = 24
	var b strings.Builder
	section := func(title string) {
		fmt.Fprintf(&b, "\n  %s\n", paint(hSection, title))
	}
	pairs := func(ps []helpPair, w int, nameColor, textColor string) {
		for _, p := range ps {
			w = max(w, len(p.name))
		}
		for _, p := range ps {
			pad := strings.Repeat(" ", w-len(p.name)+2)
			fmt.Fprintf(&b, "    %s%s%s\n", paint(nameColor, p.name), pad, paint(textColor, p.text))
		}
	}

	fmt.Fprintf(&b, "\n  %s %s  %s\n", paint(hTitle, "✦ pack"), paint(hDim, "v"+version),
		paint(hTagline, "a whole repository as one Markdown file, for a notebook AI"))

	section("USAGE")
	fmt.Fprintf(&b, "    pack %s %s %s\n    pack %s %s %s\n", paint(hDim, "[flags]"), paint(hLong, "REPO"), paint(hDim, "> repo.md"),
		paint(hDim, "[flags]"), paint(hShort, "-o FOLDER"), paint(hLong, "REPO..."))

	section("WHAT IT WRITES")
	for i, p := range helpBlocks {
		if i > 0 {
			b.WriteString("\n")
		}
		for _, line := range strings.Split(p, "\n") {
			fmt.Fprintf(&b, "    %s\n", paint(hText, line))
		}
	}

	for _, s := range helpSections {
		section(s.title)
		for _, f := range s.flags {
			left, plain := "    ", "    "
			if f.short != "" {
				left = paint(hShort, f.short)
				plain = f.short
				if f.long != "" {
					left += paint(hDim, ", ")
					plain += ", "
				}
			}
			if f.long != "" {
				left += paint(hLong, f.long)
				plain += f.long
			}
			if f.arg != "" {
				left += " " + paint(hDim, f.arg)
				plain += " " + f.arg
			}
			fmt.Fprintf(&b, "    %s%s%s\n", left, strings.Repeat(" ", max(flagWidth-len(plain), 2)), paint(hText, f.text))
		}
	}

	section("EXAMPLES")
	pairs(helpExamples, flagWidth-2, hLong, hDim)
	section("GOOD TO KNOW")
	pairs(helpNotes, 0, hLong, hDim)
	b.WriteString("\n")
	return b.String()
}
