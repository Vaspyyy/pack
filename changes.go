package main

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Vaspyyy/prism/outline"
)

type commit struct {
	short, date, subject string
	body                 []string // the message after the subject, without trailers
	first                bool     // the repository's first commit
	files                []fileChange
	more                 int // changed files not listed
}

type fileChange struct {
	path                    string
	status                  string // "new", "deleted" or ""
	changed, added, removed []string
}

const (
	filesPerCommit = 10
	namesPerFile   = 12
)

// recent reads the last n commits (merges left out) with the files each
// changed and, for code, the definitions those changes touched. keep says
// which files are worth listing (not lockfiles, vendored code, …).
func recent(s *source, n int, keep func(string) bool) ([]commit, error) {
	if n == 0 {
		return nil, nil
	}
	out, err := git(s.root, "log", "--no-merges", "--no-show-signature", "--encoding=UTF-8", "-n", strconv.Itoa(n),
		"--format=%H%x1f%cs%x1f%s%x1f%P%x1f%b%x1e", "HEAD", "--", pathspec(s.sub))
	if err != nil {
		return nil, err
	}
	boundary := s.boundaries()
	bl := &blobs{dir: s.root}
	defer bl.close()
	var commits []commit
	for _, rec := range strings.Split(out, "\x1e") {
		f := strings.Split(strings.TrimLeft(rec, "\n"), "\x1f")
		if len(f) < 5 {
			continue
		}
		// The short hash is cut here, not by git, whose length follows
		// core.abbrev and the size of the repository.
		c := commit{short: short(f[0]), date: f[1], subject: f[2], body: bodyLines(f[4])}
		parent := ""
		if ps := strings.Fields(f[3]); len(ps) > 0 {
			parent = ps[0]
		} else if boundary[f[0]] {
			// The oldest commit a shallow clone has: its parent wasn't
			// fetched, so what it changed isn't known.
			commits = append(commits, c)
			continue
		} else {
			c.first = true
		}
		// diff-tree, not diff: plumbing, so diff.* settings (noprefix,
		// mnemonicPrefix, algorithm, …) can't change what is read. The
		// options that matter are given anyway. --root diffs the first
		// commit against nothing.
		diff, err := gitRaw(s.root, "diff-tree", "-p", "--root", "--no-commit-id", "--no-color", "--no-ext-diff", "--no-textconv",
			"--no-renames", "-U0", "--inter-hunk-context=0", "--diff-algorithm=myers", "--indent-heuristic",
			"--src-prefix=a/", "--dst-prefix=b/", f[0], "--", pathspec(s.sub))
		if err != nil {
			return nil, err
		}
		for _, fd := range parseDiff(string(diff)) {
			p := fd.newPath
			if p == "" {
				p = fd.oldPath
			}
			if !keep(p) {
				continue
			}
			if len(c.files) == filesPerCommit {
				c.more++
				continue
			}
			fc := fileChange{path: p}
			switch {
			case fd.oldPath == "":
				fc.status = "new"
			case fd.newPath == "":
				fc.status = "deleted"
			}
			if outline.Supported(p) && len(fd.hunks) > 0 {
				touched(&fc, fd, bl, parent, f[0])
			}
			c.files = append(c.files, fc)
		}
		commits = append(commits, c)
	}
	return commits, nil
}

type fileDiff struct {
	oldPath, newPath string // "" for /dev/null
	hunks            [][4]int
}

// parseDiff reads a unified diff made with -U0: each file's paths and its
// hunks' line ranges (old start, old count, new start, new count).
func parseDiff(diff string) []fileDiff {
	var files []fileDiff
	var cur *fileDiff
	inHunks := false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			files = append(files, fileDiff{})
			cur = &files[len(files)-1]
			inHunks = false
			// Until --- and +++ say otherwise (a file with no content
			// change, like a mode change, has neither).
			if a, b, ok := gitPaths(strings.TrimPrefix(line, "diff --git ")); ok {
				cur.oldPath, cur.newPath = a, b
			}
		case cur == nil:
		case !inHunks && strings.HasPrefix(line, "new file mode"):
			cur.oldPath = ""
		case !inHunks && strings.HasPrefix(line, "deleted file mode"):
			cur.newPath = ""
		case !inHunks && strings.HasPrefix(line, "--- "):
			cur.oldPath = diffPath(line[4:], "a/")
		case !inHunks && strings.HasPrefix(line, "+++ "):
			cur.newPath = diffPath(line[4:], "b/")
		case strings.HasPrefix(line, "@@ "):
			inHunks = true
			if h, ok := hunk(line); ok {
				cur.hunks = append(cur.hunks, h)
			}
		}
	}
	return files
}

// gitPaths splits "a/x b/x" from a diff --git line, when both are the same
// path (the usual case without renames); quoted paths go through unquote.
func gitPaths(s string) (string, string, bool) {
	if strings.HasPrefix(s, `"`) {
		a, rest, ok := cutQuoted(s)
		if !ok {
			return "", "", false
		}
		b := diffPath(strings.TrimSpace(rest), "b/")
		return strings.TrimPrefix(a, "a/"), b, true
	}
	if len(s)%2 == 1 {
		half := (len(s) - 1) / 2
		a, b := s[:half], s[half+1:]
		if strings.HasPrefix(a, "a/") && strings.HasPrefix(b, "b/") && a[2:] == b[2:] {
			return a[2:], b[2:], true
		}
	}
	return "", "", false
}

func cutQuoted(s string) (string, string, bool) {
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			u, err := strconv.Unquote(s[:i+1])
			return u, s[i+1:], err == nil
		}
	}
	return "", "", false
}

func diffPath(p, prefix string) string {
	p = strings.TrimSuffix(p, "\t")
	if p == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(p, `"`) {
		if u, err := strconv.Unquote(p); err == nil {
			p = u
		}
	}
	return strings.TrimPrefix(p, prefix)
}

// hunk reads "@@ -a,b +c,d @@" (a count left out is 1).
func hunk(line string) ([4]int, bool) {
	var h [4]int
	f := strings.Fields(line)
	if len(f) < 3 || !strings.HasPrefix(f[1], "-") || !strings.HasPrefix(f[2], "+") {
		return h, false
	}
	read := func(s string) (int, int, bool) {
		a, b, found := strings.Cut(s, ",")
		start, err := strconv.Atoi(a)
		if err != nil {
			return 0, 0, false
		}
		count := 1
		if found {
			if count, err = strconv.Atoi(b); err != nil {
				return 0, 0, false
			}
		}
		return start, count, true
	}
	var ok1, ok2 bool
	h[0], h[1], ok1 = read(f[1][1:])
	h[2], h[3], ok2 = read(f[2][1:])
	return h, ok1 && ok2
}

// touched fills in the definitions a file's hunks overlap: in the old
// version for removed lines, in the new one for added lines. A name only
// in the new version was added, one only in the old was removed.
func touched(fc *fileChange, fd fileDiff, bl *blobs, parent, commit string) {
	var oldSyms, newSyms []outline.Symbol
	if fd.oldPath != "" {
		if data, ok := bl.get(parent, fd.oldPath); ok {
			oldSyms = outline.Parse(fd.oldPath, data, symbolLimit)
		}
	}
	if fd.newPath != "" {
		if data, ok := bl.get(commit, fd.newPath); ok {
			newSyms = outline.Parse(fd.newPath, data, symbolLimit)
		}
	}
	oldNames, newNames := names(oldSyms), names(newSyms)
	seen := map[string]bool{}
	note := func(name, kind string) {
		if seen[name] {
			return
		}
		seen[name] = true
		switch kind {
		case "added":
			fc.added = append(fc.added, name)
		case "removed":
			fc.removed = append(fc.removed, name)
		default:
			fc.changed = append(fc.changed, name)
		}
	}
	for _, h := range fd.hunks {
		if h[1] > 0 {
			for _, i := range overlapping(oldSyms, h[0], h[0]+h[1]-1) {
				name := qualify(oldSyms, i)
				if newNames[name] {
					note(name, "changed")
				} else {
					note(name, "removed")
				}
			}
		}
		if h[3] > 0 {
			for _, i := range overlapping(newSyms, h[2], h[2]+h[3]-1) {
				name := qualify(newSyms, i)
				if oldNames[name] {
					note(name, "changed")
				} else {
					note(name, "added")
				}
			}
		}
	}
}

func names(syms []outline.Symbol) map[string]bool {
	m := make(map[string]bool, len(syms))
	for i := range syms {
		m[qualify(syms, i)] = true
	}
	return m
}

// overlapping returns the innermost definitions that lines from..to touch:
// a method rather than the class around it, unless the change is in the
// class outside its methods.
func overlapping(syms []outline.Symbol, from, to int) []int {
	var hit []int
	for i, s := range syms {
		end := max(s.End, s.Line)
		if s.Line <= to && end >= from {
			hit = append(hit, i)
		}
	}
	outer := map[int]bool{}
	for _, i := range hit {
		for p, n := syms[i].Parent, 0; p >= 0 && n < 50; p, n = syms[p].Parent, n+1 {
			outer[p] = true
		}
	}
	inner := hit[:0]
	for _, i := range hit {
		if !outer[i] {
			inner = append(inner, i)
		}
	}
	return inner
}

// qualify is a definition's name with its parents': Menu.run, or for
// Markdown headings "Usage > Flags".
func qualify(syms []outline.Symbol, i int) string {
	if !heading(syms[i].Kind) {
		return outline.Qualified(syms, i)
	}
	var parts []string
	for p, n := i, 0; p >= 0 && n < 50; p, n = syms[p].Parent, n+1 {
		parts = append([]string{syms[p].Name}, parts...)
	}
	if len(parts) > 1 && syms[rootOf(syms, i)].Kind == "h1" {
		parts = parts[1:] // the document's title says nothing
	}
	return strings.Join(parts, " > ")
}

func rootOf(syms []outline.Symbol, i int) int {
	for n := 0; syms[i].Parent >= 0 && n < 50; n++ {
		i = syms[i].Parent
	}
	return i
}

// renderChanges is the Recent changes section ("" without commits).
func renderChanges(commits []commit, asked int) string {
	if len(commits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Recent changes\n\n")
	fmt.Fprintf(&b, "The last %s, newest first (merges left out). Under each: the start of its message, then the files it changed and, in code, the definitions the change touched.\n\n",
		plural(len(commits), "commit", fmt.Sprintf("%d commits", len(commits))))
	for _, c := range commits {
		fmt.Fprintf(&b, "%s %s %s\n", c.date, c.short, c.subject)
		for _, l := range c.body {
			b.WriteString("    " + l + "\n")
		}
		if c.first {
			b.WriteString("  (the first commit)\n")
		}
		for _, f := range c.files {
			b.WriteString("  " + f.path)
			if f.status != "" {
				b.WriteString(" (" + f.status + ")")
			}
			var groups []string
			left := namesPerFile
			for _, g := range []struct {
				verb  string
				names []string
			}{{"changed", f.changed}, {"added", f.added}, {"removed", f.removed}} {
				if len(g.names) == 0 {
					continue
				}
				shown := g.names
				if left <= 0 {
					groups = append(groups, fmt.Sprintf("%s %d", g.verb, len(g.names)))
					continue
				}
				extra := ""
				if len(shown) > left {
					extra = fmt.Sprintf(" and %d more", len(shown)-left)
					shown = shown[:left]
				}
				left -= len(shown)
				groups = append(groups, g.verb+" "+strings.Join(shown, ", ")+extra)
			}
			if len(groups) > 0 {
				b.WriteString(": " + strings.Join(groups, "; "))
			}
			b.WriteString("\n")
		}
		if c.more > 0 {
			fmt.Fprintf(&b, "  … and %d more %s\n", c.more, plural(c.more, "file", "files"))
		}
	}
	b.WriteString("\n")
	return b.String()
}

// blobs reads file versions through one git cat-file --batch process.
type blobs struct {
	dir string
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
	bad bool
}

func (b *blobs) get(rev, path string) ([]byte, bool) {
	if b.bad || strings.ContainsAny(path, "\n\r") {
		return nil, false
	}
	if b.cmd == nil {
		b.cmd = exec.Command("git", "-C", b.dir, "cat-file", "--batch")
		in, err1 := b.cmd.StdinPipe()
		out, err2 := b.cmd.StdoutPipe()
		if err1 != nil || err2 != nil || b.cmd.Start() != nil {
			b.bad = true
			return nil, false
		}
		b.in, b.out = in, bufio.NewReaderSize(out, 64<<10)
	}
	if _, err := fmt.Fprintf(b.in, "%s:%s\n", rev, path); err != nil {
		b.bad = true
		return nil, false
	}
	head, err := b.out.ReadString('\n')
	if err != nil {
		b.bad = true
		return nil, false
	}
	f := strings.Fields(head)
	if len(f) != 3 {
		return nil, false // missing or ambiguous: nothing follows
	}
	size, err := strconv.Atoi(f[2])
	if err != nil {
		b.bad = true
		return nil, false
	}
	if f[1] != "blob" || size > outline.MaxSize {
		// Skip it (and the newline after it) without holding it.
		if _, err := io.CopyN(io.Discard, b.out, int64(size)+1); err != nil {
			b.bad = true
		}
		return nil, false
	}
	data := make([]byte, size+1) // and the newline after it
	if _, err := io.ReadFull(b.out, data); err != nil {
		b.bad = true
		return nil, false
	}
	return data[:size], true
}

func (b *blobs) close() {
	if b.cmd != nil && b.in != nil {
		b.in.Close()
		b.cmd.Wait()
	}
}

const bodyLimit = 8 // message lines kept per commit

var reTrailer = regexp.MustCompile(`^[A-Za-z][A-Za-z-]*: `)

// bodyLines is a commit message's body without blank lines and trailers
// (Co-Authored-By: …), at most bodyLimit lines of at most 160 characters.
func bodyLines(body string) []string {
	var lines []string
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimRight(l, " \t\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		lines = append(lines, l)
	}
	// Trailers are the last lines, all in Key: value form.
	for len(lines) > 0 && reTrailer.MatchString(lines[len(lines)-1]) && isTrailerKey(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > bodyLimit {
		lines = append(lines[:bodyLimit], fmt.Sprintf("… (%d more lines)", len(lines)-bodyLimit))
	}
	for i, l := range lines {
		l = clean([]byte(l))
		l = strings.TrimSuffix(l, "\n")
		if utf8.RuneCountInString(l) > 160 {
			l = string([]rune(l)[:159]) + "…"
		}
		lines[i] = l
	}
	return lines
}

func isTrailerKey(line string) bool {
	k, _, _ := strings.Cut(line, ":")
	switch strings.ToLower(k) {
	case "co-authored-by", "signed-off-by", "reviewed-by", "acked-by", "tested-by", "reported-by", "suggested-by",
		"helped-by", "change-id", "fixes", "closes", "refs", "see-also", "cc", "link", "generated-with":
		return true
	}
	return false
}
