# A NotebookLM notebook that updates itself

This folder keeps a NotebookLM notebook of the repositories in [repos.txt](repos.txt) up to date, with no computer of yours switched on:

```
GitHub Actions (every 6 hours)        Google Apps Script (every hour)       NotebookLM
pack the repos in repos.txt    ──►    copy changed parts into         ──►   the Docs are
publish them as release assets        one Google Doc each                   its sources
```

1. **[.github/workflows/notebook.yml](../.github/workflows/notebook.yml)** packs every repository in `repos.txt` every six hours, after every push to this repository's `main`, and when started by hand. It publishes the parts and `pack.json` as assets of the [`notebook` release](https://github.com/Vaspyyy/pack/releases/tag/notebook). When nothing changed, it leaves the assets alone.
2. **[sync.gs](sync.gs)** runs in your Google account every hour. It reads `pack.json` and, for each part whose SHA-256 changed, downloads it, checks the SHA-256, and writes it into that part's Google Doc in a Drive folder called `Tool notebook`. Each part always goes into the same Doc, so a notebook source made from it stays the same source.
3. **NotebookLM** uses the Docs as sources. Google Docs from Drive are the source type NotebookLM keeps in step with Drive; uploaded files are copies that never change.

## Setup (once, about five minutes)

1. Open [script.google.com](https://script.google.com) and make a **New project**. Name it, for example "Tool notebook sync".
2. Replace everything in `Code.gs` with the contents of [sync.gs](sync.gs) and save (Ctrl+S).
3. In the toolbar, choose the function **setup** and click **Run**.
4. Google asks for permission: click **Review permissions** and pick your account. It's your own script, so Google warns that it hasn't verified it: click **Advanced**, then **Go to "Tool notebook sync" (unsafe)**, then **Allow**. The script asks for:
   - Google Docs: to write the Docs,
   - Google Drive: to put them in a folder and notice when one was deleted,
   - external requests: to download the parts from GitHub,
   - triggers: to run every hour by itself.
5. The execution log shows a line per Doc (`updated glint (575,670 characters)`). The Docs are in the Drive folder `Tool notebook`. If the first run stops at Apps Script's six-minute limit, the next hourly run carries on where it stopped: each Doc that was written is remembered.
6. In [NotebookLM](https://notebooklm.google.com), make a notebook, choose **Add source**, then **Google Drive**, and pick the Docs in `Tool notebook`.

From then on nothing needs doing. To refresh right away after a push to one of the repositories, start the workflow (`gh workflow run notebook.yml -R Vaspyyy/pack`), wait for it to finish, then run **sync** in the script editor.

## Adding or removing a repository

Edit [repos.txt](repos.txt). After the next workflow run, a new repository gets a new Doc (add it to the notebook once), and the Doc of a removed one is emptied and renamed "(unused) …".

A repository that grows past 1,000,000 characters gets a second part and so a new Doc (`lumen-2.md`, titled "lumen (part 2 of 2)"). When it shrinks back, the Doc of the part that is gone is emptied and renamed "(unused) …", so the notebook shows which source to remove.

## For your own repositories

Fork this repository, put your repositories in `repos.txt` (public ones; a private one needs a token with access in the workflow), and change `MANIFEST_URL` at the top of `sync.gs` to your fork's release (`https://github.com/<you>/pack/releases/download/notebook/pack.json`).
