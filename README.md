<div align="center">

# pack

A whole repository as one Markdown file, for a notebook AI.

[![Go](https://img.shields.io/github/go-mod/go-version/Vaspyyy/pack?style=flat-square&labelColor=1e1e2e&color=89b4fa&logo=go&logoColor=89b4fa)](go.mod)
[![Release](https://img.shields.io/github/v/tag/Vaspyyy/pack?sort=semver&style=flat-square&labelColor=1e1e2e&color=cba6f7&label=release)](https://github.com/Vaspyyy/pack/tags)
[![License](https://img.shields.io/github/license/Vaspyyy/pack?style=flat-square&labelColor=1e1e2e&color=a6e3a1)](LICENSE)

</div>

pack turns a git repository into one Markdown file that NotebookLM, or any chat that takes a file, can read as a source: a short header, a **map** of every file with its line count and main definitions, the **recent changes** with the definitions each commit touched, then every **file** in full under its path. Lockfiles, vendored code, images, license texts and binaries are left out, and the header says what was left out.

NotebookLM reads a GitHub link as a web page (the README, not the code), so a repository has to become a document first. The map and the recent changes are what make that document useful: with them, a notebook can answer "where is the config read?" and "what changed in the parser last week?", not only "what does this line do?". The definitions come from [prism](https://github.com/Vaspyyy/prism), the same symbol finder as [glint](https://github.com/Vaspyyy/glint), [seek](https://github.com/Vaspyyy/seek) and [recap](https://github.com/Vaspyyy/recap).

[notebook/](notebook/README.md) has the rest of the setup for a notebook that updates itself: a GitHub workflow that packs a list of repositories every six hours, and a Google Apps Script that keeps one Google Doc per repository in step with it.

## Install

```sh
go install github.com/Vaspyyy/pack@latest
```

You'll need Go 1.26 or newer (older versions fetch the right toolchain on their own) and git. pack is a single binary. It's developed on Linux and should work on macOS and the BSDs.

## Usage

```sh
pack [flags] REPO > repo.md
pack [flags] -o FOLDER REPO...
```

`REPO` is a folder in a git repository, or a GitHub repository: `owner/repo`, `github.com/owner/repo`, an https URL or a `git@github.com:` address. A GitHub repository is cloned shallowly into a temporary folder, which is removed afterwards. A subfolder packs only itself (`pack site` in a repository with a `site/` folder), named after the repository and the folder (`lumen-site`).

```
$ pack Vaspyyy/peek > peek.md
$ head -40 peek.md
# peek

The source code of peek (github.com/Vaspyyy/peek) at commit 3178407, 2026-11-13, put into one file by pack 0.1.0.

17 files, 4,339 lines: Go 10, Markdown 3, YAML 2, Go module 1, other 1. Left out: 1 lockfile (go.sum); 1 license text (LICENSE).

Sections: **Map** lists every file with its line count and its main definitions (L12 is line 12), ...

## Map

README.md (150 lines)
  L3 # peek
    L17 ## Install
    L25 ## Usage
      L32 ### JSON
...
json.go (536 lines)
  L26 type node struct
  L35 func decodeOne(dec *json.Decoder) (*node, error)
...

## Recent changes

The last 6 commits, newest first (merges left out). ...

2026-11-13 3178407 0.1.3: fixes from Sonnet 5.5's re-check
    - 'Segmentation fault' and jest's ✕ are errors
    ...
  README.md: changed Usage > Text and logs, Usage > Anything else
  main.go: changed detect, parse; added init
  table.go: changed sniffDelimN; removed sniffDelim

## Files

### README.md
...
```

### What goes in

- **The header**: the repository, commit and date (and "with uncommitted changes" for a local folder that has them), the number of files and lines by language, and what was left out, counted by reason with a few names each.
- **Map**: every packed file with its line count. Under each, its definitions with their line numbers: top-level ones and those right inside them (a class's methods, a Rust `impl`'s functions), up to 60 per file, and Markdown headings down to `###`. Line numbers count from the top of the file, as in the file section below.
- **Recent changes**: the last 20 commits (`-c N`), merges left out, newest first. Each has its date, short hash and subject, the first 8 lines of its message without trailers like `Co-Authored-By:`, and the files it changed, each with the definitions the change touched: `changed`, `added` (only in the new version) or `removed` (only in the old). A change inside a method names the method, not the class around it. Files pack leaves out (a lockfile, vendored code) aren't listed.
- **Files**: every packed file, READMEs first, then by path. Each sits under `### path` in a code fence tagged with its language, made longer than any run of backticks inside it, so a Markdown file with code blocks can't end it early. Line ends become `\n`, a byte order mark is dropped, and control characters other than tab and newline are written out as `\x1b` and so on, so the text stays plain text.

pack reads what git tracks (`git ls-files`), from the work tree. Untracked and ignored files never go in.

### What stays out

| Reason | Which files |
| --- | --- |
| excluded | matched by `-x`: a pattern without a slash matches any folder or the file name (`testdata`, `*.golden`), one with a slash matches from the top (`site/src`) |
| vendored | in a `vendor/`, `third_party/`, `third-party/`, `node_modules/` or `bower_components/` folder, or marked `linguist-vendored` in `.gitattributes` |
| generated | marked `linguist-generated`, `*.min.js`, `*.min.css`, `*.js.map`, `*.css.map`, Gradle and Maven wrapper scripts |
| lockfile | `go.sum`, `Cargo.lock`, `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`, `bun.lock`, `poetry.lock`, `uv.lock`, `Gemfile.lock`, `composer.lock`, `flake.lock` and others |
| license text | `LICENSE`, `LICENCE`, `COPYING`, `UNLICENSE`, with or without `.md` or `.txt` |
| media | images (SVG too), fonts, audio, video, 3D models, PDFs and archives, by extension |
| binary | a NUL byte in the first 8,000 bytes, or bytes that aren't UTF-8 |
| size | files over 256 KB (`--max-file KB`, 0 for no limit) |
| other | symlinks, submodules, files deleted in the work tree |

### Parts

A Google Doc holds about 1.02 million characters, so a repository longer than 1,000,000 characters (`--max-chars N`, 0 for no limit) is split between files into parts. Part 1 has the header, the map, the recent changes and the first files; each later part starts with its own short header. The parts are `NAME.md`, `NAME-2.md`, `NAME-3.md` and so on, so the first part keeps its name when a repository grows. Printing to standard output takes one part: a repository that needs more stops with exit code 2 and says so.

### Several repositories, and pack.json

`-o FOLDER` writes each repository's parts into the folder, plus `pack.json`:

```json
{
  "pack": "0.1.0",
  "files": [
    {
      "file": "lumen.md",
      "title": "lumen (part 1 of 2)",
      "source": "github.com/Vaspyyy/lumen",
      "commit": "071d76ac18228715394df7a452fb7df5a42f20fe",
      "part": 1,
      "parts": 2,
      "chars": 999876,
      "sha256": "37c5b6eeb4f2a7eb39fc9d0b362012ccee2d7931f56b72e667c8bc4783394460"
    }
  ]
}
```

Every repository is read before anything is written: if one can't be packed, pack stops with exit code 2 and the folder stays as it was. A part that the folder's previous `pack.json` listed and this run doesn't write any more is removed; nothing else in the folder is touched. The output depends only on the files, the commits and pack's version, not on the time it was made, so packing the same commit again gives the same bytes and the same `sha256`.

### Flags

| Flag | |
| --- | --- |
| `-o FOLDER` | write `NAME.md` (and further parts) and `pack.json` into the folder; needed for several repositories |
| `-x`, `--exclude PATTERN` | leave out more files (repeatable) |
| `-c`, `--commits N` | recent commits to list (default 20; 0 for none) |
| `--max-file KB` | leave out files bigger than this (default 256; 0 for no limit) |
| `--max-chars N` | characters per part (default 1,000,000; 0 for one part) |
| `-h`, `--help` | help |
| `-V`, `--version` | version |

Exit code: 0 packed, 2 bad input or a repository that couldn't be read. Warnings (a map without definitions to fit a part, a single file longer than a part) go to standard error.

## For coding agents

`skill/SKILL.md` is an agent skill that says when to use pack and how. Link the folder into your agent's skills folder, for Claude Code `ln -s "$PWD/skill" ~/.claude/skills/pack`.

## License

GPL-3.0, see [LICENSE](LICENSE).
