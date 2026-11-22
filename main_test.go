package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isolate keeps the user's git config (signing, hooks, templates) out.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv("GIT_AUTHOR_DATE", "2026-01-02T10:00:00Z")
	t.Setenv("GIT_COMMITTER_DATE", "2026-01-02T10:00:00Z")
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, dir, name, text string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

const mainV1 = `package main

func main() {
	helper()
}

func helper() int {
	return 1
}

func gone() {}
`

const mainV2 = `package main

func main() {
	helper()
}

func helper() int {
	return 2
}

func fresh() {}
`

// demo makes a repository named demo with two commits and one file of
// each kind pack leaves out.
func demo(t *testing.T) string {
	t.Helper()
	isolate(t)
	dir := filepath.Join(t.TempDir(), "demo")
	gitT(t, filepath.Dir(dir), "init", "-q", "-b", "main", dir)
	write(t, dir, "README.md", "# demo\n\nA demo.\n\n## Usage\n\nRun it.\n")
	write(t, dir, "main.go", mainV1)
	write(t, dir, "go.sum", "x v1 h1:abc\n")
	write(t, dir, "LICENSE", "license text\n")
	write(t, dir, "vendor/lib/lib.go", "package lib\n")
	write(t, dir, "logo.png", "\x89PNG\r\n\x1a\n")
	write(t, dir, "data.bin", "a\x00b")
	write(t, dir, "big.txt", strings.Repeat("x", 300<<10))
	write(t, dir, "ctrl.txt", "one\r\ntwo\x1bthree\r\n")
	write(t, dir, "doc.md", "Example:\n\n```sh\nls\n```\n")
	write(t, dir, "gen.go", "package main\n")
	write(t, dir, ".gitattributes", "gen.go linguist-generated\n")
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", "first")
	write(t, dir, "main.go", mainV2)
	gitT(t, dir, "commit", "-q", "-am", "second\n\nWhy it changed.\n\nCo-Authored-By: Someone <s@example.com>")
	return dir
}

func pack(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return out.String(), errb.String(), code
}

func wants(t *testing.T, out string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(out, p) {
			t.Errorf("missing %q in:\n%s", p, out)
		}
	}
}

func lacks(t *testing.T, out string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if strings.Contains(out, p) {
			t.Errorf("unexpected %q in:\n%s", p, out)
		}
	}
}

func TestPack(t *testing.T) {
	dir := demo(t)
	out, errs, code := pack(t, dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	wants(t, out,
		"# demo\n",
		"The source code of demo at commit ",
		", 2026-01-02, put into one file by pack "+version,
		"1 lockfile (go.sum)", "1 license text (LICENSE)", "1 vendored file (vendor/)",
		"1 generated file (gen.go)", "1 image, font or media file (logo.png)", "1 binary file (data.bin)",
		"1 file over the size limit (big.txt 300 KB)",
		"## Map\n\nREADME.md (7 lines)\n  L1 # demo\n    L5 ## Usage\n",
		"main.go (11 lines)\n  L3 func main()\n  L7 func helper() int\n  L11 func fresh()\n",
		"## Recent changes\n",
		"second\n    Why it changed.\n  main.go: changed helper; added fresh; removed gone\n",
		"first\n  (the first commit)\n",
		"## Files\n\n### README.md\n",
		"one\ntwo\\x1bthree\n",
		"````markdown\nExample:\n\n```sh\nls\n```\n````\n",
	)
	lacks(t, out, "Co-Authored-By", "\r", "x v1 h1", "package lib", "### gen.go")
}

func TestSplitAndManifest(t *testing.T) {
	dir := demo(t)
	for i := 0; i < 30; i++ {
		write(t, dir, filepath.Join("pkg", string(rune('a'+i%26))+strings.Repeat("x", i/26)+".go"), "package pkg\n\n// "+strings.Repeat("filler ", 200)+"\n")
	}
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", "more")

	outDir := filepath.Join(t.TempDir(), "out")
	write(t, outDir, "keep.txt", "not pack's")
	out, errs, code := pack(t, "--max-chars", "10000", "-o", outDir, dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	var m manifest
	data, err := os.ReadFile(filepath.Join(outDir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Files) < 3 {
		t.Fatalf("want several parts, got %d: %s", len(m.Files), out)
	}
	for i, e := range m.Files {
		text, err := os.ReadFile(filepath.Join(outDir, e.File))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(text)
		switch {
		case e.SHA256 != hex.EncodeToString(sum[:]):
			t.Errorf("%s: sha256 doesn't match", e.File)
		case charCount(string(text)) > 10000 || e.Chars != charCount(string(text)):
			t.Errorf("%s: %d characters, manifest says %d", e.File, charCount(string(text)), e.Chars)
		case e.Part != i+1 || e.Parts != len(m.Files):
			t.Errorf("%s: part %d of %d", e.File, e.Part, e.Parts)
		case e.File != fileName("demo", i+1):
			t.Errorf("part %d is %s", i+1, e.File)
		}
		if i == 0 {
			wants(t, string(text), "# demo, part 1 of ", "## Map", "## Recent changes")
		} else {
			wants(t, string(text), "# demo, part ", "## Files (continued)")
		}
	}
	if m.Files[1].Title != "demo (part 2 of "+itoa(len(m.Files))+")" {
		t.Errorf("title %q", m.Files[1].Title)
	}
	wants(t, out, "demo.md + demo-2.md", "parts", "pack.json")

	// One part now: the other parts go, other files stay.
	out, errs, code = pack(t, "--max-chars", "0", "-o", outDir, dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	wants(t, out, "pack: removed demo-2.md, demo-3.md")
	if _, err := os.Stat(filepath.Join(outDir, "demo-2.md")); err == nil {
		t.Error("demo-2.md is still there")
	}
	if _, err := os.Stat(filepath.Join(outDir, "keep.txt")); err != nil {
		t.Error("keep.txt was removed")
	}
}

func itoa(n int) string { return num(n) }

func TestStdoutRefusesParts(t *testing.T) {
	dir := demo(t)
	for i := 0; i < 10; i++ {
		write(t, dir, "f"+itoa(i)+".txt", strings.Repeat("words ", 500)+"\n")
	}
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", "more")
	out, errs, code := pack(t, "--max-chars", "10000", dir)
	if code != 2 || out != "" {
		t.Fatalf("exit %d, output %q", code, out)
	}
	wants(t, errs, "needs", "parts of at most 10,000", "-o FOLDER")
}

func TestFailureWritesNothing(t *testing.T) {
	dir := demo(t)
	outDir := filepath.Join(t.TempDir(), "out")
	_, errs, code := pack(t, "-o", outDir, dir, filepath.Join(t.TempDir(), "missing", "x", "y"))
	if code != 2 {
		t.Fatalf("exit %d", code)
	}
	wants(t, errs, "no folder by that name")
	if _, err := os.Stat(outDir); err == nil {
		t.Error("the folder was made anyway")
	}
}

func TestClone(t *testing.T) {
	dir := demo(t)
	bare := filepath.Join(t.TempDir(), "owner", "demo.git")
	gitT(t, filepath.Dir(dir), "clone", "-q", "--bare", dir, bare)
	old := githubURL
	githubURL = func(slug string) string {
		return "file://" + filepath.Join(filepath.Dir(filepath.Dir(bare)), slug+".git")
	}
	defer func() { githubURL = old }()

	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	out, errs, code := pack(t, "https://github.com/owner/demo")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	wants(t, out, "# demo\n", "The source code of demo (github.com/owner/demo) at commit ", "added fresh")
	if left, _ := os.ReadDir(tmp); len(left) != 0 {
		t.Errorf("the clone was left behind: %v", left)
	}
	_, errs, code = pack(t, "owner/nothing-here")
	if code != 2 {
		t.Fatalf("exit %d", code)
	}
	wants(t, errs, "couldn't clone github.com/owner/nothing-here")
}

func TestSubfolderAndDirty(t *testing.T) {
	dir := demo(t)
	write(t, dir, "site/index.md", "# Site\n")
	write(t, dir, "site/app.js", "function go() {}\n")
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", "site")
	write(t, dir, "site/app.js", "function go() { return 1 }\n")
	out, errs, code := pack(t, filepath.Join(dir, "site"))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	wants(t, out, "# demo-site\n", "The source code of the folder site/ of demo", "with uncommitted changes",
		"2 files", "### site/index.md\n", "### site/app.js\n")
	lacks(t, out, "### main.go", "second")
	// Only untracked files: not uncommitted changes to what is packed.
	gitT(t, dir, "checkout", "-q", "--", ".")
	write(t, dir, "notes.txt", "untracked\n")
	out, _, _ = pack(t, dir)
	lacks(t, out, "uncommitted", "untracked")
}

func TestExclude(t *testing.T) {
	dir := demo(t)
	out, _, code := pack(t, "-x", "README.md", "--exclude=*.txt", dir)
	if code != 0 {
		t.Fatal(code)
	}
	wants(t, out, "files excluded with -x (README.md, big.txt, ctrl.txt)")
	lacks(t, out, "### README.md", "### ctrl.txt")

	for _, c := range []struct {
		path, pat string
		want      bool
	}{
		{"testdata/a.json", "testdata", true},
		{"pkg/testdata/a.json", "testdata", true},
		{"pkg/a.golden", "*.golden", true},
		{"site/src/x.astro", "site/src", true},
		{"site/src/x.astro", "site/", true},
		{"site/src/x.astro", "site/src/", true},
		{"site/srcs/x.astro", "site/src", false},
		{"other/site/src/x", "site/src", false},
		{"site/src/x.astro", "site/*/x.astro", true},
		{"main.go", "", false},
	} {
		if got := excluded(c.path, c.pat); got != c.want {
			t.Errorf("excluded(%q, %q) = %v", c.path, c.pat, got)
		}
	}
}

func TestSlug(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/Vaspyyy/pack.git": "Vaspyyy/pack",
		"https://github.com/Vaspyyy/pack":     "Vaspyyy/pack",
		"git@github.com:Vaspyyy/pack.git":     "Vaspyyy/pack",
		"ssh://git@github.com/o/r.js":         "o/r.js",
		"github.com/o/r/":                     "o/r",
		"https://gitlab.com/o/r.git":          "",
		"https://github.com/o/r/tree/main":    "",
	} {
		if got := slugOf(in); got != want {
			t.Errorf("slugOf(%q) = %q, want %q", in, got, want)
		}
	}
	for in, ok := range map[string]bool{"o/r": true, "o/r.git": true, "-o/r": false, "o/r/x": false, "o": false} {
		if got := reSlug.MatchString(in); got != ok {
			t.Errorf("reSlug(%q) = %v", in, got)
		}
	}
}

func TestParseDiff(t *testing.T) {
	diff := "diff --git a/x.go b/x.go\nindex 1..2 100644\n--- a/x.go\n+++ b/x.go\n@@ -3,2 +3 @@ func a\n-old\n-+++ b/fake.go\n+new\n" +
		"diff --git a/new.go b/new.go\nnew file mode 100644\n--- /dev/null\n+++ b/new.go\n@@ -0,0 +1,4 @@\n+a\n" +
		"diff --git \"a/sp ace\\tx.go\" \"b/sp ace\\tx.go\"\ndeleted file mode 100644\n--- \"a/sp ace\\tx.go\"\n+++ /dev/null\n@@ -1 +0,0 @@\n-x\n" +
		"diff --git a/bin b/bin\nBinary files a/bin and b/bin differ\n"
	got := parseDiff(diff)
	if len(got) != 4 {
		t.Fatalf("%d files: %+v", len(got), got)
	}
	if got[0].oldPath != "x.go" || got[0].newPath != "x.go" || len(got[0].hunks) != 1 || got[0].hunks[0] != [4]int{3, 2, 3, 1} {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].oldPath != "" || got[1].newPath != "new.go" || got[1].hunks[0] != [4]int{0, 0, 1, 4} {
		t.Errorf("new: %+v", got[1])
	}
	if got[2].oldPath != "sp ace\tx.go" || got[2].newPath != "" {
		t.Errorf("deleted: %+v", got[2])
	}
	if got[3].oldPath != "bin" || got[3].newPath != "bin" || len(got[3].hunks) != 0 {
		t.Errorf("binary: %+v", got[3])
	}
}

func TestParseArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, "give a repository"},
		{[]string{"a", "b"}, "several repositories need -o"},
		{[]string{"-c", "x", "a"}, "-c takes a number"},
		{[]string{"--max-chars", "500", "a"}, "--max-chars below 10,000"},
		{[]string{"--nope", "a"}, "unknown flag --nope"},
		{[]string{"-o"}, "-o needs a value"},
	} {
		_, errs, code := pack(t, c.args...)
		if code != 2 || !strings.Contains(errs, c.want) {
			t.Errorf("%v: exit %d, %q", c.args, code, errs)
		}
	}
	o, code, msg := parse([]string{"--max-chars=20_000", "-c", "0", "--", "-dir"})
	if code != 0 || msg != "" || o.maxChars != 20000 || o.commits != 0 || len(o.sources) != 1 || o.sources[0] != "-dir" {
		t.Errorf("parse: %+v %d %q", o, code, msg)
	}
	out, _, code := pack(t, "-V")
	if code != 0 || out != "pack "+version+"\n" {
		t.Errorf("-V: %q", out)
	}
}

func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		"\xef\xbb\xbfa\r\nb":   "a\nb\n",
		"a\rb\n":               "a\nb\n",
		"tab\tbell\x07del\x7f": "tab\tbell\\x07del\\x7f\n",
		"":                     "",
	} {
		if got := clean([]byte(in)); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
	if f := fence("a ``` b ```` c"); f != "`````" {
		t.Errorf("fence %q", f)
	}
}

func TestBodyLines(t *testing.T) {
	body := "line one\n\n  - two\n\nCo-Authored-By: X <x@example.com>\nSigned-off-by: Y <y@example.com>\n"
	got := bodyLines(body)
	if strings.Join(got, "|") != "line one|  - two" {
		t.Errorf("%q", got)
	}
	// A last line that only looks like a trailer stays.
	if got := bodyLines("Note: this is prose\n"); len(got) != 1 {
		t.Errorf("%q", got)
	}
}

func TestFirstCommitAndShallowBoundary(t *testing.T) {
	dir := demo(t)
	out, _, _ := pack(t, dir)
	wants(t, out, "first\n  (the first commit)\n", "  main.go (new): added main, helper, gone\n")

	// In a shallow clone the oldest commit's parent is missing: what it
	// changed isn't known, and it isn't called the first commit.
	shallow := filepath.Join(t.TempDir(), "demo")
	gitT(t, filepath.Dir(shallow), "clone", "-q", "--depth", "1", "file://"+dir, shallow)
	out, errs, code := pack(t, shallow)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	wants(t, out, "second\n    Why it changed.\n\n## Files")
	lacks(t, out, "the first commit", "main.go (new)")
}

// Settings that change what git prints must not change pack's output.
func TestGitSettingsDontChangeOutput(t *testing.T) {
	dir := demo(t)
	before, _, _ := pack(t, dir)
	for _, kv := range [][2]string{
		{"core.abbrev", "12"}, {"diff.noprefix", "true"}, {"diff.mnemonicPrefix", "true"},
		{"diff.algorithm", "histogram"}, {"diff.interHunkContext", "10"}, {"diff.context", "5"},
		{"diff.renames", "copies"}, {"color.ui", "always"}, {"log.showSignature", "false"},
	} {
		gitT(t, dir, "config", kv[0], kv[1])
	}
	after, _, _ := pack(t, dir)
	if before != after {
		t.Errorf("output changed with git settings:\n%s\n---\n%s", before, after)
	}
}

func TestPreludeTooBigWarns(t *testing.T) {
	dir := demo(t)
	for i := 0; i < 600; i++ {
		write(t, dir, filepath.Join("many", "file-with-a-longish-name-"+itoa(i)+".txt"), "x\n")
	}
	gitT(t, dir, "add", "-A")
	gitT(t, dir, "commit", "-q", "-m", "many")
	_, errs, code := pack(t, "--max-chars", "10000", "-o", t.TempDir(), dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	wants(t, errs, "the map lists files without their definitions", "part 1 is longer than --max-chars")
}
