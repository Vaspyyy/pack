# A NotebookLM notebook that updates itself

This folder keeps the sources of a NotebookLM notebook of the repositories in [repos.txt](repos.txt) up to date, with no computer of yours switched on:

```
GitHub Actions (every 6 hours)        Google Apps Script (every hour)       NotebookLM
pack the repos in repos.txt    ──►    copy changed parts into         ──►   the Docs are
publish them as release assets        one Google Doc each                   its sources
```

1. **[.github/workflows/notebook.yml](../.github/workflows/notebook.yml)** packs every repository in `repos.txt` every six hours, after every push to this repository's `main`, and when started by hand. It publishes the parts and `pack.json` as assets of the [`notebook` release](https://github.com/Vaspyyy/pack/releases/tag/notebook). When nothing changed, it leaves the assets alone.
2. **[sync.gs](sync.gs)** runs in your Google account every hour. It reads `pack.json` and, for each part whose SHA-256 changed, downloads it, checks the SHA-256, and uploads it to Drive as that part's Google Doc in a Drive folder called `Tool notebook` (one upload per Doc, which Drive converts in seconds). Each part always goes into the same Doc, so a notebook source made from it stays the same source.
3. **NotebookLM** uses the Docs as sources. A Google Doc added from Drive stays linked to it: since May 2026 NotebookLM syncs such sources by itself (Google rolled this out gradually), and where it doesn't yet, a source's sync button pulls in the new text. An uploaded file is a copy that never changes.

## Setup (once, about five minutes)

1. Open [script.google.com](https://script.google.com) and make a **New project**. Name it, for example "Tool notebook sync".
2. Replace everything in `Code.gs` with the contents of [sync.gs](sync.gs) and save (Ctrl+S).
3. In the toolbar, choose the function **setup** and click **Run**.
4. Google asks for permission: click **Review permissions** and pick your account. It's your own script, so Google warns that it hasn't verified it: click **Advanced**, then **Go to "Tool notebook sync" (unsafe)**, then **Allow**. The script asks for:
   - Google Drive: to make the Docs in a folder, write them and notice when one was deleted,
   - external requests: to download the parts from GitHub and hand them to Drive's upload service, which turns each into its Doc's text,
   - triggers: to run every hour by itself.

   If the log says the Drive API is off for the script, click **+** next to **Services** in the editor, pick **Drive API**, click **Add**, and run **sync** again.
5. The execution log shows a line per Doc (`updated glint (575,670 characters)`). The Docs are in the Drive folder `Tool notebook`. A run starts no new Doc after four and a half minutes (Apps Script stops scripts at six); the next run carries on, since each Doc that was written is remembered.
6. In [NotebookLM](https://notebooklm.google.com), make a notebook, choose **Add source**, then **Google Drive**, and pick the Docs in `Tool notebook`.

From then on the Docs keep themselves up to date (and NotebookLM with them, where its Drive sync is on). If a run of the workflow fails, for example because a repository in `repos.txt` was renamed or deleted, GitHub emails you, and the Docs keep their last good version until `repos.txt` is fixed. To refresh right away after a push to one of the repositories, start the workflow (`gh workflow run notebook.yml -R Vaspyyy/pack`), wait for it to finish, then run **sync** in the script editor.

## Adding or removing a repository

Edit [repos.txt](repos.txt). After the next workflow run, a new repository gets a new Doc (add it to the notebook once), and the Doc of a removed one is emptied and renamed "(unused) …".

## When something goes wrong

A part that can't be downloaded or written is logged and tried again on the next run; the other parts go on. A Doc you move to the trash gets a new Doc on the next run (add that one to the notebook). Any other error keeps the Doc and tries again next run: only when Drive hasn't been able to find a Doc for three days does it count as deleted for good, and the part gets a new Doc.

An hourly run that finds nothing changed only reads `pack.json` and the Docs' names; it opens no Doc.

GitHub turns off a public repository's scheduled workflows after 60 days without activity in it (it emails a warning first). A push to this repository counts, a release upload doesn't. If that happens, `gh workflow enable notebook.yml -R Vaspyyy/pack` turns it back on.

## Growing and shrinking

A repository that grows past 1,000,000 characters gets a second part and so a new Doc (`lumen-2.md`, titled "lumen (part 2 of 2)"). When it shrinks back, the Doc of the part that is gone is emptied and renamed "(unused) …", so the notebook shows which source to remove.

## For your own repositories

Fork this repository, put your repositories in `repos.txt`, and change `MANIFEST_URL` at the top of `sync.gs` to your fork's release (`https://github.com/<you>/pack/releases/download/notebook/pack.json`).

Only public repositories belong in `repos.txt`: the packed files are assets of a public release, which anyone can download.
