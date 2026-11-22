package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Vaspyyy/prism/outline"
)

type file struct {
	path  string
	text  string
	lines int
	lang  string // the language's name; "" if unknown
	tag   string // the fence's info string
	syms  []outline.Symbol
}

// result is one packed repository.
type result struct {
	arg      string
	name     string
	source   string // github.com/owner/repo, or the folder's name
	commit   string
	parts    []string
	chars    int
	files    int
	warnings []string
}

const (
	mapSymbols  = 60   // definitions listed per file in the map
	symbolLimit = 5000 // definitions read per file
)

func build(s *source, o options) (*result, error) {
	paths, err := s.files()
	if err != nil {
		return nil, err
	}
	marked := s.attrs(paths)
	sk := newSkips()
	var files []*file
	maxFile := int64(o.maxFileKB) << 10
	for _, p := range paths {
		if r := pathReason(p, o.excludes); r != "" {
			ex := p
			if r == "vendored" {
				ex = vendorExample(p)
			}
			sk.add(r, ex)
			continue
		}
		if r := marked[p]; r != "" {
			sk.add(r, p)
			continue
		}
		full := filepath.Join(s.root, filepath.FromSlash(p))
		fi, err := os.Lstat(full)
		switch {
		case err != nil:
			sk.add("missing", p)
			continue
		case fi.Mode()&os.ModeSymlink != 0:
			sk.add("symlink", p)
			continue
		case fi.IsDir():
			sk.add("submodule", p)
			continue
		case !fi.Mode().IsRegular():
			continue
		case o.maxFileKB > 0 && fi.Size() > maxFile:
			sk.add("big", fmt.Sprintf("%s %s KB", p, num(int((fi.Size()+1023)>>10))))
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			sk.add("missing", p)
			continue
		}
		if isBinary(data) {
			sk.add("binary", p)
			continue
		}
		f := &file{path: p, text: clean(data)}
		f.lines = strings.Count(f.text, "\n")
		f.lang, f.tag = language(p)
		if outline.Supported(p) {
			f.syms = outline.Parse(p, data, symbolLimit)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no files left to pack (%s)", orNone(sk.summary()))
	}
	// READMEs first: they say what the rest is for.
	sort.SliceStable(files, func(i, j int) bool { return readmeRank(files[i].path, s.sub) < readmeRank(files[j].path, s.sub) })

	keep := func(p string) bool { return pathReason(p, o.excludes) == "" && marked[p] == "" }
	commits, err := recent(s, o.commits, keep)
	if err != nil {
		return nil, err
	}

	r := &result{name: s.name, source: s.name, commit: s.commit, files: len(files)}
	if s.slug != "" {
		r.source = "github.com/" + s.slug
	}
	h := header{s: s, files: files, skipped: sk.summary(), commits: len(commits)}

	changes := renderChanges(commits, o.commits)
	mapText := renderMap(files, true)
	limit := o.maxChars
	prelude := func(part, parts int) string {
		return h.render(part, parts) + mapText + changes + "## Files\n\n"
	}
	if limit > 0 && charCount(prelude(99, 99)) > limit*2/5 {
		mapText = renderMap(files, false)
		r.warnings = append(r.warnings, "the map lists files without their definitions, to fit")
	}
	if n := charCount(prelude(99, 99)); limit > 0 && n > limit {
		r.warnings = append(r.warnings, fmt.Sprintf("the header, map and recent changes alone are %s characters, so part 1 is longer than --max-chars; fewer commits (-c), fewer files (-x) or one subfolder at a time make it fit", num(n)))
	}

	blocks := make([]string, len(files))
	for i, f := range files {
		fc := fence(f.text)
		blocks[i] = "### " + f.path + "\n\n" + fc + f.tag + "\n" + f.text + fc + "\n\n"
	}

	// Assign files to parts. The headers are sized for 99 parts, so the
	// real ones (fewer digits) can only be shorter.
	var parts [][]int
	cur := []int{}
	size := charCount(prelude(99, 99))
	for i, b := range blocks {
		n := charCount(b)
		if limit > 0 && size+n > limit && (len(cur) > 0 || len(parts) == 0) {
			parts = append(parts, cur)
			cur = []int{}
			size = charCount(h.renderMore(99, 99))
		}
		if limit > 0 && size+n > limit {
			r.warnings = append(r.warnings, fmt.Sprintf("%s alone is longer than a part (%s characters)", files[i].path, num(n)))
		}
		cur = append(cur, i)
		size += n
	}
	parts = append(parts, cur)

	for pi, idx := range parts {
		var b strings.Builder
		if pi == 0 {
			b.WriteString(prelude(1, len(parts)))
		} else {
			b.WriteString(h.renderMore(pi+1, len(parts)))
		}
		for _, i := range idx {
			b.WriteString(blocks[i])
		}
		text := strings.TrimRight(b.String(), "\n") + "\n"
		r.parts = append(r.parts, text)
		r.chars += charCount(text)
	}
	return r, nil
}

func orNone(s string) string {
	if s == "" {
		return "the repository tracks no files there"
	}
	return "left out: " + s
}

func readmeRank(p, sub string) int {
	dir, base := path.Split(p)
	if strings.TrimSuffix(dir, "/") != sub && !(sub == "" && dir == "") {
		return 2
	}
	if strings.HasPrefix(strings.ToLower(base), "readme") {
		return 0
	}
	return 1
}

func charCount(s string) int { return utf8.RuneCountInString(s) }

type header struct {
	s       *source
	files   []*file
	skipped string
	commits int // in Recent changes
}

func (h header) what() string {
	w := h.s.name
	if h.s.sub != "" {
		w = "the folder " + h.s.sub + "/ of " + w
	}
	if h.s.slug != "" {
		w += " (github.com/" + h.s.slug + ")"
	}
	return w
}

func (h header) at() string {
	a := "commit " + short(h.s.commit)
	if h.s.date != "" {
		a += ", " + h.s.date
	}
	if h.s.dirty {
		a += ", with uncommitted changes"
	}
	return a
}

func (h header) render(part, parts int) string {
	var b strings.Builder
	b.WriteString("# " + h.s.name)
	if parts > 1 {
		fmt.Fprintf(&b, ", part %d of %d", part, parts)
	}
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "The source code of %s at %s, put into one file by pack %s.\n\n", h.what(), h.at(), version)
	lines := 0
	for _, f := range h.files {
		lines += f.lines
	}
	fmt.Fprintf(&b, "%s %s, %s %s: %s.", num(len(h.files)), plural(len(h.files), "file", "files"), num(lines), plural(lines, "line", "lines"), langSummary(h.files))
	if h.skipped != "" {
		fmt.Fprintf(&b, " Left out: %s.", h.skipped)
	}
	b.WriteString("\n\n")
	b.WriteString("Sections: **Map** lists every file with its line count and its main definitions (L12 is line 12), ")
	if h.commits > 0 {
		fmt.Fprintf(&b, "**Recent changes** has the last %s with the start of each message and the files and definitions each one changed, ", plural(h.commits, "commit", fmt.Sprintf("%d commits", h.commits)))
	}
	b.WriteString("and **Files** has every file in full, each under a heading with its path.")
	if parts > 1 {
		fmt.Fprintf(&b, " This is part 1 of %d: it has the map, the recent changes and the first files; the other parts have the rest.", parts)
	}
	b.WriteString("\n\n")
	return b.String()
}

// renderMore is the top of a part after the first.
func (h header) renderMore(part, parts int) string {
	return fmt.Sprintf("# %s, part %d of %d\n\nMore files of %s at %s, put into one file by pack %s. Part 1 has the map, the recent changes and the first files.\n\n## Files (continued)\n\n",
		h.s.name, part, parts, h.what(), h.at(), version)
}

// renderMap lists every file with its line count and, with syms, its main
// definitions: top-level ones and those right inside them (a class's
// methods), or Markdown headings down to ###.
func renderMap(files []*file, syms bool) string {
	var b strings.Builder
	b.WriteString("## Map\n\n")
	for _, f := range files {
		fmt.Fprintf(&b, "%s (%s %s)\n", f.path, num(f.lines), plural(f.lines, "line", "lines"))
		if !syms {
			continue
		}
		listed, more := 0, 0
		for i, s := range f.syms {
			d := depth(f.syms, i)
			if heading(s.Kind) {
				if s.Kind > "h3" {
					continue
				}
				d = int(s.Kind[1] - '1')
			} else if d > 1 {
				continue
			}
			if listed == mapSymbols {
				more++
				continue
			}
			listed++
			fmt.Fprintf(&b, "  %sL%d %s\n", strings.Repeat("  ", d), s.Line, sig(s))
		}
		if more > 0 {
			fmt.Fprintf(&b, "  … %d more\n", more)
		}
	}
	b.WriteString("\n")
	return b.String()
}

func depth(syms []outline.Symbol, i int) int {
	d := 0
	for p := syms[i].Parent; p >= 0 && d < 50; p = syms[p].Parent {
		d++
	}
	return d
}

func heading(kind string) bool {
	return len(kind) == 2 && kind[0] == 'h' && kind[1] >= '1' && kind[1] <= '6'
}

// sig is a definition's line, without an opening brace, at most 100 characters.
func sig(s outline.Symbol) string {
	t := strings.TrimSpace(s.Sig)
	if t == "" {
		t = s.Kind + " " + s.Name
	}
	t = strings.TrimSpace(strings.TrimSuffix(t, "{"))
	if utf8.RuneCountInString(t) > 100 {
		t = string([]rune(t)[:99]) + "…"
	}
	return t
}
