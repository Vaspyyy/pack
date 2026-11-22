// pack turns a git repository into one Markdown file (or a few, when it is
// big) for a notebook AI such as NotebookLM: a map of every file with its
// main definitions, the recent commits with the definitions they changed,
// then every file in full. Lockfiles, vendored code, media, license texts
// and binaries are left out, and the file says so.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/term"
)

const version = "0.1.0"

const (
	// defaultMaxChars keeps one part inside a Google Doc, which holds about
	// 1.02 million characters.
	defaultMaxChars = 1_000_000
	defaultCommits  = 20
	defaultMaxFile  = 256 // KB
)

// manifestName is the file -o writes next to the parts.
const manifestName = "pack.json"

type options struct {
	out       string // a folder to write into; "" prints to stdout
	maxChars  int    // characters per part; 0 for no limit
	commits   int
	maxFileKB int
	excludes  []string
	sources   []string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	o, code, msg := parse(args)
	if msg != "" {
		if code == 0 {
			fmt.Fprint(stdout, msg)
		} else {
			fmt.Fprintln(stderr, "pack: "+msg)
		}
		return code
	}
	// Pack everything before writing anything: one source that can't be
	// read leaves the folder as it was.
	var results []*result
	for _, arg := range o.sources {
		r, err := packSource(arg, o)
		if err != nil {
			fmt.Fprintf(stderr, "pack: %s: %v\n", arg, err)
			return 2
		}
		for _, w := range r.warnings {
			fmt.Fprintf(stderr, "pack: %s: %s\n", r.name, w)
		}
		results = append(results, r)
	}
	if o.out == "" {
		r := results[0]
		if len(r.parts) > 1 {
			fmt.Fprintf(stderr, "pack: %s is %s characters, which needs %d parts of at most %s; -o FOLDER writes them as separate files, --max-chars 0 prints it as one\n",
				r.name, num(r.chars), len(r.parts), num(o.maxChars))
			return 2
		}
		io.WriteString(stdout, r.parts[0])
		return 0
	}
	return writeFolder(o.out, results, stdout, stderr)
}

func packSource(arg string, o options) (*result, error) {
	s, err := open(arg, o.commits)
	if err != nil {
		return nil, err
	}
	defer s.close()
	r, err := build(s, o)
	if r != nil {
		r.arg = arg
	}
	return r, err
}

// manifest is pack.json: what -o wrote, for a program that copies the parts
// elsewhere (notebook/sync.gs keeps Google Docs in step with it).
type manifest struct {
	Pack  string      `json:"pack"`
	Files []entryJSON `json:"files"`
}

type entryJSON struct {
	File   string `json:"file"`
	Title  string `json:"title"`
	Source string `json:"source"`
	Commit string `json:"commit"`
	Part   int    `json:"part"`
	Parts  int    `json:"parts"`
	Chars  int    `json:"chars"`
	SHA256 string `json:"sha256"`
}

var reUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// fileName is a part's file name: name.md for the first part, name-2.md and
// on for the others, so the first part keeps its name when a repo grows.
func fileName(name string, part int) string {
	base := strings.Trim(reUnsafe.ReplaceAllString(name, "-"), "-.")
	if base == "" {
		base = "repo"
	}
	if part == 1 {
		return base + ".md"
	}
	return fmt.Sprintf("%s-%d.md", base, part)
}

func title(name string, part, parts int) string {
	if parts == 1 {
		return name
	}
	return fmt.Sprintf("%s (part %d of %d)", name, part, parts)
}

func writeFolder(dir string, results []*result, stdout, stderr io.Writer) int {
	// Old parts this folder's last pack.json listed and this run doesn't
	// write again are removed; nothing else in the folder is touched.
	var old manifest
	if data, err := os.ReadFile(filepath.Join(dir, manifestName)); err == nil {
		json.Unmarshal(data, &old)
	}
	m := manifest{Pack: version}
	type out struct{ path, text string }
	var outs []out
	seen := map[string]string{}
	for _, r := range results {
		for i, text := range r.parts {
			name := fileName(r.name, i+1)
			if prev, ok := seen[name]; ok {
				fmt.Fprintf(stderr, "pack: %s and %s would both write %s\n", prev, r.arg, name)
				return 2
			}
			seen[name] = r.arg
			sum := sha256.Sum256([]byte(text))
			m.Files = append(m.Files, entryJSON{
				File: name, Title: title(r.name, i+1, len(r.parts)), Source: r.source, Commit: r.commit,
				Part: i + 1, Parts: len(r.parts), Chars: charCount(text), SHA256: hex.EncodeToString(sum[:]),
			})
			outs = append(outs, out{filepath.Join(dir, name), text})
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(stderr, "pack: %v\n", err)
		return 2
	}
	for _, f := range outs {
		if err := writeFile(f.path, []byte(f.text)); err != nil {
			fmt.Fprintf(stderr, "pack: %v\n", err)
			return 2
		}
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := writeFile(filepath.Join(dir, manifestName), append(data, '\n')); err != nil {
		fmt.Fprintf(stderr, "pack: %v\n", err)
		return 2
	}
	var removed []string
	for _, e := range old.Files {
		name := filepath.Base(e.File)
		if _, ok := seen[name]; ok || name != e.File || !strings.HasSuffix(name, ".md") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err == nil {
			removed = append(removed, name)
		} else if !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "pack: %v\n", err)
		}
	}
	for _, r := range results {
		names := make([]string, len(r.parts))
		for i := range r.parts {
			names[i] = fileName(r.name, i+1)
		}
		what := fmt.Sprintf("%s files, %s characters", num(r.files), num(r.chars))
		if len(r.parts) > 1 {
			what += fmt.Sprintf(", %d parts", len(r.parts))
		}
		fmt.Fprintf(stdout, "pack: %s  %s %s, %s\n", strings.Join(names, " + "), r.source, short(r.commit), what)
	}
	if len(removed) > 0 {
		fmt.Fprintf(stdout, "pack: removed %s (no longer written)\n", strings.Join(removed, ", "))
	}
	fmt.Fprintf(stdout, "pack: wrote %d %s and %s to %s\n", len(outs), plural(len(outs), "file", "files"), manifestName, dir)
	return 0
}

// writeFile replaces a file in one step, so a reader never sees half of it.
func writeFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pack-*")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// parse reads the command line. A message with code 0 is printed as is.
func parse(args []string) (options, int, string) {
	o := options{maxChars: defaultMaxChars, commits: defaultCommits, maxFileKB: defaultMaxFile}
	flags := true
	for i := 0; i < len(args); i++ {
		a := args[i]
		value := func() (string, bool) {
			if i+1 >= len(args) {
				return "", false
			}
			i++
			return args[i], true
		}
		// --flag=value
		if flags && strings.HasPrefix(a, "--") && strings.Contains(a, "=") {
			k, v, _ := strings.Cut(a, "=")
			args = append(args[:i:i], append([]string{k, v}, args[i+1:]...)...)
			a = k
		}
		switch {
		case !flags || a == "-" || !strings.HasPrefix(a, "-"):
			o.sources = append(o.sources, a)
		case a == "--":
			flags = false
		case a == "-h" || a == "--help":
			return o, 0, helpText(term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("NO_COLOR") == "")
		case a == "-V" || a == "--version":
			return o, 0, "pack " + version + "\n"
		case a == "-A":
			// plain output: it always is
		case a == "-o" || a == "-x" || a == "--exclude" || a == "-c" || a == "--commits" || a == "--max-chars" || a == "--max-file":
			v, ok := value()
			if !ok {
				return o, 2, a + " needs a value"
			}
			switch a {
			case "-o":
				o.out = v
			case "-x", "--exclude":
				o.excludes = append(o.excludes, v)
			default:
				n, err := strconv.Atoi(strings.ReplaceAll(strings.ReplaceAll(v, "_", ""), ",", ""))
				if err != nil || n < 0 {
					return o, 2, fmt.Sprintf("%s takes a number, not %q", a, v)
				}
				switch a {
				case "-c", "--commits":
					o.commits = n
				case "--max-chars":
					o.maxChars = n
				case "--max-file":
					o.maxFileKB = n
				}
			}
		default:
			return o, 2, fmt.Sprintf("unknown flag %s (pack -h lists them)", a)
		}
	}
	switch {
	case len(o.sources) == 0:
		return o, 2, "give a repository: a folder, or owner/repo on GitHub (pack -h shows how)"
	case len(o.sources) > 1 && o.out == "":
		return o, 2, "several repositories need -o FOLDER, one file each"
	case o.maxChars > 0 && o.maxChars < 10_000:
		return o, 2, "--max-chars below 10,000 would cut the map apart; use 0 for no limit"
	}
	return o, 0, ""
}

// num writes 1234567 as 1,234,567.
func num(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func short(hash string) string {
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
}
