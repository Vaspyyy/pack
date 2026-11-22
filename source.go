package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Vaspyyy/prism/repo"
)

// source is the repository being packed: a local work tree, or a shallow
// clone of a GitHub repo.
type source struct {
	name    string // short name: base, and the sub folder if there is one
	base    string // the GitHub repo's name, else the work tree folder's
	slug    string // owner/repo on GitHub; "" if not known
	root    string // the work tree
	sub     string // only this repo-relative folder ("" for all of it)
	commit  string // HEAD
	date    string // HEAD's commit date, YYYY-MM-DD
	dirty   bool   // tracked files have uncommitted changes
	shallow bool
	temp    string // a clone to remove when done
}

// githubURL is where owner/repo is cloned from (tests point it elsewhere).
var githubURL = func(slug string) string { return "https://github.com/" + slug + ".git" }

// reSlug reads owner/repo, github.com/owner/repo, an https URL or an ssh
// address of a GitHub repo.
var reSlug = regexp.MustCompile(`^(?:(?:https?://|ssh://git@|git@)?github\.com[/:])?([A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)/([A-Za-z0-9._-]+?)(?:\.git)?/?$`)

// slugOf returns owner/repo for a GitHub address, or "".
func slugOf(s string) string {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "github.com") {
		return ""
	}
	m := reSlug.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1] + "/" + m[2]
}

// open finds the repository arg names: an existing folder, else a GitHub
// repo, which is cloned with enough history for the recent commits.
func open(arg string, commits int) (*source, error) {
	if fi, err := os.Stat(arg); err == nil {
		if !fi.IsDir() {
			return nil, fmt.Errorf("is a file; pack takes a repository folder or owner/repo")
		}
		return openDir(arg)
	}
	m := reSlug.FindStringSubmatch(arg)
	if m == nil || m[2] == "." || m[2] == ".." {
		return nil, fmt.Errorf("no folder by that name, and it isn't a GitHub repo (owner/repo)")
	}
	// "src/lib" where a folder src exists is a mistyped path, not a repo
	// to ask GitHub for (github.com/src/lib works for that).
	if first, _, _ := strings.Cut(arg, "/"); !strings.Contains(arg, "github.com") {
		if fi, err := os.Stat(first); err == nil && fi.IsDir() {
			return nil, fmt.Errorf("no folder by that name (for the GitHub repo, write github.com/%s)", arg)
		}
	}
	return clone(m[1]+"/"+m[2], commits)
}

func openDir(dir string) (*source, error) {
	info, err := repo.Detect(dir)
	if err != nil {
		return nil, err
	}
	if !info.Git {
		return nil, fmt.Errorf("isn't in a git repository (pack packs what git tracks)")
	}
	if info.Head == "" {
		return nil, fmt.Errorf("the repository has no commits yet")
	}
	s := &source{root: info.Root, commit: info.Head}
	if rel, ok := info.Rel(dir); ok && rel != "." {
		s.sub = rel
	}
	remote, _ := git(s.root, "remote", "get-url", "origin")
	s.slug = slugOf(remote)
	s.base = filepath.Base(s.root)
	if s.slug != "" {
		s.base = s.slug[strings.Index(s.slug, "/")+1:]
	}
	s.name = s.base
	if s.sub != "" {
		s.name += "-" + strings.ReplaceAll(s.sub, "/", "-")
	}
	s.date, _ = git(s.root, "log", "-1", "--format=%cs", "HEAD")
	status, _ := git(s.root, "status", "--porcelain", "--untracked-files=no", "--", pathspec(s.sub))
	s.dirty = status != ""
	sh, _ := git(s.root, "rev-parse", "--is-shallow-repository")
	s.shallow = sh == "true"
	return s, nil
}

func clone(slug string, commits int) (*source, error) {
	tmp, err := os.MkdirTemp("", "pack-")
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(tmp, "repo")
	// Merges aren't listed, so fetch more history than the commits shown.
	depth := max(commits*3, 1)
	cmd := exec.Command("git", "clone", "--quiet", "--no-tags", "--depth", strconv.Itoa(depth), githubURL(slug), dir)
	// No password prompts, and no Git LFS downloads: LFS files are media
	// and binaries pack leaves out anyway.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_LFS_SKIP_SMUDGE=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tmp)
		return nil, fmt.Errorf("couldn't clone github.com/%s: %s", slug, firstLine(string(out), err))
	}
	s, err := openDir(dir)
	if err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	s.temp = tmp
	s.slug = slug
	s.base = slug[strings.Index(slug, "/")+1:]
	s.name = s.base
	return s, nil
}

// boundaries are the commits a shallow clone has without their parents.
func (s *source) boundaries() map[string]bool {
	b := map[string]bool{}
	if !s.shallow {
		return b
	}
	p, err := git(s.root, "rev-parse", "--path-format=absolute", "--git-path", "shallow")
	if err != nil {
		return b
	}
	data, _ := os.ReadFile(p)
	for _, l := range strings.Fields(string(data)) {
		b[l] = true
	}
	return b
}

func (s *source) close() {
	if s.temp != "" {
		os.RemoveAll(s.temp)
	}
}

// files lists what git tracks (in the sub folder, if any), repo-relative.
func (s *source) files() ([]string, error) {
	out, err := gitRaw(s.root, "ls-files", "-z", "--cached", "--", pathspec(s.sub))
	if err != nil {
		return nil, err
	}
	var files []string
	seen := map[string]bool{}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" && !seen[p] { // a conflicted file is listed once per stage
			seen[p] = true
			files = append(files, p)
		}
	}
	return files, nil
}

// attrs returns the files that .gitattributes marks linguist-vendored or
// linguist-generated, with "vendored" or "generated".
func (s *source) attrs(files []string) map[string]string {
	marked := map[string]string{}
	if len(files) == 0 {
		return marked
	}
	cmd := exec.Command("git", "-C", s.root, "check-attr", "-z", "--stdin", "linguist-vendored", "linguist-generated")
	cmd.Stdin = strings.NewReader(strings.Join(files, "\x00") + "\x00")
	out, err := cmd.Output()
	if err != nil {
		return marked
	}
	f := strings.Split(string(out), "\x00")
	for i := 0; i+2 < len(f); i += 3 {
		path, attr, val := f[i], f[i+1], f[i+2]
		if val != "set" && val != "true" {
			continue
		}
		if attr == "linguist-vendored" {
			marked[path] = "vendored"
		} else if marked[path] == "" {
			marked[path] = "generated"
		}
	}
	return marked
}

// pathspec is a git pathspec for a repo-relative folder ("" for all).
func pathspec(sub string) string {
	if sub == "" {
		return ":/"
	}
	return ":(top,literal)" + sub
}

func git(dir string, args ...string) (string, error) {
	out, err := gitRaw(dir, args...)
	return strings.TrimSpace(string(out)), err
}

func gitRaw(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "core.quotePath=false"}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("git %s: %s", args[0], firstLine(stderr.String(), err))
	}
	return out, nil
}

func firstLine(s string, err error) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return strings.TrimPrefix(l, "fatal: ")
		}
	}
	return err.Error()
}
