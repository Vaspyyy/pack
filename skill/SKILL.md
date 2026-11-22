---
name: pack
description: Turn a git repository into one Markdown file for NotebookLM, a Gemini or ChatGPT upload, or any other AI that reads documents, with `pack REPO > repo.md` (a folder or owner/repo on GitHub). The file has a header, a map of every file with its main definitions and line numbers, the last commits with the definitions each touched, then every file in full; lockfiles, vendored code, media, license texts and binaries are left out and counted. Big repositories are split into parts that each fit a Google Doc (`-o FOLDER`). Use it when someone wants a codebase as a document or notebook source, not for reading code yourself (use glint, seek and recap for that).
---

# pack for agents

`pack` writes a repository as one Markdown file meant for another AI to read: NotebookLM sources, a chat upload, a Google Doc. It is not for your own exploring: `glint -A`, `seek` and `recap` answer your questions with far less text.

## Commands

| Want | Command |
| --- | --- |
| This repository as one file | `pack . > repo.md` |
| A GitHub repository (shallow clone, removed afterwards) | `pack owner/repo > repo.md` |
| Only one folder of a repository | `pack site > site.md` (named `REPO-site`) |
| Several repositories, one file (or a few) each, plus pack.json | `pack -o out ~/src/a owner/b` |
| Leave out more | `pack -x testdata -x '*.golden' -x site/src .` |
| Fewer or no commits | `pack -c 5 .`, `pack -c 0 .` |

`-x` takes a name or glob without a slash (it matches any folder or the file name) or a path from the top with a slash.

## What it writes

1. A header: repository, commit, date, files and lines by language, and what was left out by reason (`Left out: 1 lockfile (go.sum); 72 vendored files (vendor/)`).
2. `## Map`: every file with its line count and its definitions with line numbers (top level and one level in, up to 60 per file; Markdown headings to `###`).
3. `## Recent changes`: the last 20 commits (merges left out), each with up to 8 lines of its message and the files it changed, with the definitions changed, added or removed.
4. `## Files`: every file in a code fence under `### path`: the top README first, then the other top-level files, then the files in folders.

Only what git tracks goes in, read from the work tree. A local folder with uncommitted changes to tracked files says so in the header.

## Size

One part holds at most 1,000,000 characters (`--max-chars`), which fits a Google Doc (about 1.02 million). Bigger repositories need `-o FOLDER` and come out as `NAME.md`, `NAME-2.md`, …; printing such a repository to stdout fails with exit code 2 and says so. Files over 256 KB are left out (`--max-file KB`). If a repository is far too big, leave out folders with `-x` (generated docs, fixtures, translations) or pack its folders one by one.

## Keeping a notebook up to date

The pack repository's `notebook/` folder has a GitHub workflow that packs a list of repositories every six hours and a Google Apps Script that copies changed parts into one Google Doc each; `notebook/README.md` explains the setup. Point the user there instead of uploading files by hand.
