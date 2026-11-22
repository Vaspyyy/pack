package main

import (
	"bytes"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

// The reasons a file is left out, in the order the header lists them.
var reasons = []struct{ key, one, many string }{
	{"excluded", "file excluded with -x", "files excluded with -x"},
	{"vendored", "vendored file", "vendored files"},
	{"generated", "generated file", "generated files"},
	{"lockfile", "lockfile", "lockfiles"},
	{"license", "license text", "license texts"},
	{"media", "image, font or media file", "images, fonts and media files"},
	{"binary", "binary or non-UTF-8 file", "binary or non-UTF-8 files"},
	{"big", "file over the size limit", "files over the size limit"},
	{"symlink", "symlink", "symlinks"},
	{"submodule", "submodule", "submodules"},
	{"missing", "file not in the work tree", "files not in the work tree"},
}

var vendorDirs = map[string]bool{"vendor": true, "third_party": true, "third-party": true, "node_modules": true, "bower_components": true}

var lockfiles = map[string]bool{
	"go.sum": true, "go.work.sum": true, "Cargo.lock": true, "package-lock.json": true, "npm-shrinkwrap.json": true,
	"yarn.lock": true, "pnpm-lock.yaml": true, "bun.lock": true, "bun.lockb": true, "deno.lock": true,
	"poetry.lock": true, "Pipfile.lock": true, "uv.lock": true, "pdm.lock": true, "Gemfile.lock": true,
	"composer.lock": true, "flake.lock": true, "mix.lock": true, "pubspec.lock": true, "Podfile.lock": true,
	"packages.lock.json": true, "gradle.lockfile": true, "Package.resolved": true, "conan.lock": true,
}

// generatedNames are files tools write that nobody reads.
var generatedNames = map[string]bool{"gradlew": true, "gradlew.bat": true, "mvnw": true, "mvnw.cmd": true}

var mediaExts = map[string]bool{
	"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true, "avif": true, "bmp": true, "ico": true,
	"icns": true, "tif": true, "tiff": true, "svg": true, "psd": true, "xcf": true, "kra": true, "aseprite": true,
	"ttf": true, "otf": true, "woff": true, "woff2": true, "eot": true,
	"wav": true, "mp3": true, "ogg": true, "opus": true, "flac": true, "m4a": true, "aac": true,
	"mp4": true, "webm": true, "mov": true, "mkv": true, "avi": true, "gltf": true, "glb": true, "blend": true, "fbx": true, "obj": true,
	"pdf": true, "zip": true, "gz": true, "tgz": true, "xz": true, "bz2": true, "zst": true, "7z": true, "rar": true, "jar": true,
}

// pathReason says why a file is left out by its path alone, or "".
func pathReason(p string, excludes []string) string {
	for _, x := range excludes {
		if excluded(p, x) {
			return "excluded"
		}
	}
	parts := strings.Split(p, "/")
	for _, d := range parts[:len(parts)-1] {
		if vendorDirs[d] {
			return "vendored"
		}
	}
	base := parts[len(parts)-1]
	lower := strings.ToLower(base)
	switch {
	case lockfiles[base]:
		return "lockfile"
	case isLicense(lower):
		return "license"
	case generatedNames[base], strings.HasSuffix(lower, ".min.js"), strings.HasSuffix(lower, ".min.css"),
		strings.HasSuffix(lower, ".js.map"), strings.HasSuffix(lower, ".css.map"):
		return "generated"
	case mediaExts[ext(lower)]:
		return "media"
	}
	return ""
}

func isLicense(lower string) bool {
	stem := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(lower, ".md"), ".txt"), ".rst")
	switch stem {
	case "license", "licence", "copying", "unlicense", "license-mit", "license-apache", "copying.lesser":
		return true
	}
	return strings.HasPrefix(stem, "license-") || strings.HasPrefix(stem, "licence-")
}

// excluded matches a -x pattern: one without a slash matches any part of
// the path (a folder or the file name, with * and ?), one with a slash
// matches the path from the start, as a whole or as a folder above it.
func excluded(p, pat string) bool {
	pat = strings.TrimPrefix(strings.TrimSuffix(pat, "/"), "./")
	if pat == "" {
		return false
	}
	parts := strings.Split(p, "/")
	if !strings.Contains(pat, "/") {
		for _, part := range parts {
			if ok, _ := path.Match(pat, part); ok {
				return true
			}
		}
		return false
	}
	n := strings.Count(pat, "/") + 1
	if n > len(parts) {
		return false
	}
	ok, _ := path.Match(pat, strings.Join(parts[:n], "/"))
	return ok
}

func ext(name string) string {
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[i+1:]
	}
	return ""
}

// isBinary: a NUL byte near the start, or bytes that aren't UTF-8.
func isBinary(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 || !utf8.Valid(data)
}

// clean makes a file's text safe for a document: no byte order mark,
// Unix line ends, control characters written as \xNN, a final newline.
func clean(data []byte) string {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	var b strings.Builder
	b.Grow(len(data) + 1)
	for i := 0; i < len(data); i++ {
		c := data[i]
		switch {
		case c == '\r':
			if i+1 < len(data) && data[i+1] == '\n' {
				continue
			}
			b.WriteByte('\n')
		case c == '\n' || c == '\t' || (c >= 0x20 && c != 0x7f):
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, `\x%02x`, c)
		}
	}
	s := b.String()
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

// fence is a code fence longer than any run of backticks in the text.
func fence(text string) string {
	longest, run := 0, 0
	for i := 0; i < len(text); i++ {
		if text[i] == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}

// languages maps an extension (or a whole file name) to a language's name
// and the fence's info string.
var languages = map[string][2]string{
	"go": {"Go", "go"}, "rs": {"Rust", "rust"}, "kt": {"Kotlin", "kotlin"}, "kts": {"Kotlin", "kotlin"},
	"java": {"Java", "java"}, "scala": {"Scala", "scala"}, "swift": {"Swift", "swift"}, "cs": {"C#", "csharp"},
	"c": {"C", "c"}, "h": {"C", "c"}, "cc": {"C++", "cpp"}, "cpp": {"C++", "cpp"}, "cxx": {"C++", "cpp"},
	"hpp": {"C++", "cpp"}, "hh": {"C++", "cpp"}, "m": {"Objective-C", "objectivec"}, "zig": {"Zig", "zig"},
	"py": {"Python", "python"}, "pyi": {"Python", "python"}, "rb": {"Ruby", "ruby"}, "lua": {"Lua", "lua"},
	"php": {"PHP", "php"}, "pl": {"Perl", "perl"}, "ex": {"Elixir", "elixir"}, "exs": {"Elixir", "elixir"},
	"hs": {"Haskell", "haskell"}, "ml": {"OCaml", "ocaml"}, "dart": {"Dart", "dart"}, "gd": {"GDScript", "gdscript"},
	"js": {"JavaScript", "javascript"}, "mjs": {"JavaScript", "javascript"}, "cjs": {"JavaScript", "javascript"},
	"jsx": {"JavaScript", "jsx"}, "ts": {"TypeScript", "typescript"}, "mts": {"TypeScript", "typescript"},
	"tsx": {"TypeScript", "tsx"}, "svelte": {"Svelte", "svelte"}, "vue": {"Vue", "vue"}, "astro": {"Astro", "astro"},
	"html": {"HTML", "html"}, "htm": {"HTML", "html"}, "css": {"CSS", "css"}, "scss": {"SCSS", "scss"},
	"sh": {"Shell", "sh"}, "bash": {"Shell", "bash"}, "zsh": {"Shell", "zsh"}, "fish": {"Shell", "fish"},
	"ps1": {"PowerShell", "powershell"}, "bat": {"Batch", "bat"}, "cmd": {"Batch", "bat"},
	"md": {"Markdown", "markdown"}, "mdx": {"Markdown", "mdx"}, "rst": {"reStructuredText", "rst"}, "txt": {"Text", "text"},
	"json": {"JSON", "json"}, "jsonc": {"JSON", "jsonc"}, "jsonl": {"JSON", "json"}, "yml": {"YAML", "yaml"},
	"yaml": {"YAML", "yaml"}, "toml": {"TOML", "toml"}, "ini": {"INI", "ini"}, "xml": {"XML", "xml"},
	"sql": {"SQL", "sql"}, "graphql": {"GraphQL", "graphql"}, "proto": {"Protocol Buffers", "protobuf"},
	"wgsl": {"WGSL", "wgsl"}, "glsl": {"GLSL", "glsl"}, "vert": {"GLSL", "glsl"}, "frag": {"GLSL", "glsl"},
	"hlsl": {"HLSL", "hlsl"}, "gdshader": {"Godot shader", "glsl"}, "nix": {"Nix", "nix"}, "tf": {"Terraform", "hcl"},
	"gs": {"Apps Script", "javascript"}, "cmake": {"CMake", "cmake"}, "mk": {"Makefile", "make"},
	"go.mod": {"Go module", "go"}, "go.work": {"Go module", "go"}, "makefile": {"Makefile", "make"},
	"dockerfile": {"Dockerfile", "dockerfile"}, "justfile": {"Justfile", "just"}, "cmakelists.txt": {"CMake", "cmake"},
	"pkgbuild": {"Shell", "sh"},
}

// language returns a file's language and fence info string ("" if unknown).
func language(p string) (string, string) {
	base := strings.ToLower(path.Base(p))
	if l, ok := languages[base]; ok {
		return l[0], l[1]
	}
	if l, ok := languages[ext(base)]; ok {
		return l[0], l[1]
	}
	return "", ""
}

// skips collects the files left out, by reason.
type skips struct {
	n        map[string]int
	examples map[string][]string
}

func newSkips() *skips { return &skips{n: map[string]int{}, examples: map[string][]string{}} }

// add counts a file. example is what the header may name for it (a vendored
// file is named by its folder, so one folder doesn't fill the list).
func (s *skips) add(reason, example string) {
	s.n[reason]++
	for _, e := range s.examples[reason] {
		if e == example {
			return
		}
	}
	if len(s.examples[reason]) < 3 {
		s.examples[reason] = append(s.examples[reason], example)
	} else if len(s.examples[reason]) == 3 {
		s.examples[reason] = append(s.examples[reason], "…")
	}
}

// summary is "1 lockfile (go.sum), 2 license texts (LICENSE, docs/COPYING)".
func (s *skips) summary() string {
	var out []string
	for _, r := range reasons {
		n := s.n[r.key]
		if n == 0 {
			continue
		}
		out = append(out, fmt.Sprintf("%s %s (%s)", num(n), plural(n, r.one, r.many), strings.Join(s.examples[r.key], ", ")))
	}
	return strings.Join(out, "; ")
}

// vendorExample names a vendored file by its vendor folder.
func vendorExample(p string) string {
	parts := strings.Split(p, "/")
	for i, d := range parts[:len(parts)-1] {
		if vendorDirs[d] {
			return strings.Join(parts[:i+1], "/") + "/"
		}
	}
	return p
}

// langSummary is "Go 60, Markdown 12, YAML 3", the most used first.
func langSummary(files []*file) string {
	count := map[string]int{}
	for _, f := range files {
		name := f.lang
		if name == "" {
			name = "other"
		}
		count[name]++
	}
	type lc struct {
		name string
		n    int
	}
	var ls []lc
	for k, v := range count {
		ls = append(ls, lc{k, v})
	}
	sort.Slice(ls, func(i, j int) bool {
		if ls[i].n != ls[j].n {
			return ls[i].n > ls[j].n
		}
		return ls[i].name < ls[j].name
	})
	var out []string
	for i, l := range ls {
		if i == 8 {
			out = append(out, fmt.Sprintf("%d more", len(ls)-8))
			break
		}
		out = append(out, fmt.Sprintf("%s %s", l.name, num(l.n)))
	}
	return strings.Join(out, ", ")
}

// oneLine writes the control characters in s as \xNN, newlines too (tabs
// stay): for a path or a commit subject that must stay on one line.
func oneLine(s string) string {
	bad := func(r rune) bool { return (r < 0x20 && r != '\t') || r == 0x7f }
	if !strings.ContainsFunc(s, bad) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if bad(r) {
			fmt.Fprintf(&b, `\x%02x`, r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// clip cuts s to at most n characters, ending in an ellipsis when cut.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

// outlineName is the name prism's outline gets for a file, which picks the
// language: Apps Script (.gs) is JavaScript.
func outlineName(p string) string {
	if strings.HasSuffix(strings.ToLower(p), ".gs") {
		return p + ".js"
	}
	return p
}
