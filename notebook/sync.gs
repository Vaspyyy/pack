/**
 * Keeps one Google Doc per packed part in step with the notebook release,
 * so a NotebookLM notebook made from those Docs stays up to date.
 * Setup: notebook/README.md in github.com/Vaspyyy/pack.
 */

// Where the notebook workflow publishes pack.json and the parts.
const MANIFEST_URL = 'https://github.com/Vaspyyy/pack/releases/download/notebook/pack.json';
// The Drive folder the Docs go into (made on the first run).
const FOLDER_NAME = 'Tool notebook';

/** Run once by hand: syncs now, then every hour by itself. */
function setup() {
  ScriptApp.getProjectTriggers()
    .filter(t => t.getHandlerFunction() === 'sync')
    .forEach(t => ScriptApp.deleteTrigger(t));
  ScriptApp.newTrigger('sync').timeBased().everyHours(1).create();
  sync();
}

/** Brings the Docs up to date with pack.json; only changed parts are fetched. */
function sync() {
  const lock = LockService.getScriptLock();
  if (!lock.tryLock(1000)) {
    console.log('a sync is already running');
    return;
  }
  try {
    syncNow_();
  } finally {
    lock.releaseLock();
  }
}

// After this long a run starts no new part, so it ends before Apps
// Script's six-minute limit; the next run carries on.
const TIME_BUDGET_MS = 4.5 * 60 * 1000;
// A Doc that Drive can't find for this long counts as deleted for good
// (emptied from the trash), and its part gets a new Doc. A Google error or
// a used-up quota clears up long before.
const GONE_AFTER_MS = 3 * 24 * 60 * 60 * 1000;

function syncNow_() {
  const started = Date.now();
  const props = PropertiesService.getScriptProperties();
  const res = UrlFetchApp.fetch(MANIFEST_URL, { muteHttpExceptions: true });
  if (res.getResponseCode() !== 200) {
    console.warn('pack.json: HTTP ' + res.getResponseCode() + ', trying again next time');
    return;
  }
  const manifest = JSON.parse(res.getContentText('UTF-8'));
  const base = MANIFEST_URL.slice(0, MANIFEST_URL.lastIndexOf('/') + 1);
  const folder = folder_(props);
  const wanted = {};
  let updated = 0, failed = 0, left = 0;
  for (const f of manifest.files) {
    wanted[f.file] = true;
    if (Date.now() - started > TIME_BUDGET_MS) {
      left++;
      continue;
    }
    // One part that fails doesn't hold up the others.
    try {
      if (syncPart_(props, folder, base, f)) updated++;
    } catch (e) {
      failed++;
      console.warn(f.file + ': ' + e.message + ', trying again next time');
    }
  }
  // A part that is gone (its repository got smaller or left repos.txt)
  // keeps its Doc, emptied and renamed, so the notebook shows what to
  // remove instead of old code.
  for (const key of props.getKeys()) {
    if (!key.startsWith('doc:') || wanted[key.slice(4)]) continue;
    if (Date.now() - started > TIME_BUDGET_MS) break;
    try {
      retire_(props, key.slice(4));
    } catch (e) {
      console.warn(key.slice(4) + ': ' + e.message + ', trying again next time');
    }
  }
  let summary = updated ? 'updated ' + updated + ' of ' + manifest.files.length + ' Docs' : 'no Doc needed updating';
  if (failed) summary += ', ' + failed + ' failed';
  if (left) summary += ', ' + left + ' left for the next run';
  console.log(summary);
}

/**
 * Brings one part's Doc up to date. True if it was rewritten. A Doc whose
 * part didn't change is only looked at through Drive (its name), never
 * opened: opening a Doc of a million characters takes time.
 */
function syncPart_(props, folder, base, f) {
  const shaKey = 'sha:' + f.file;
  const file = existingFile_(props, f.file) || newDoc_(props, folder, f);
  if (file.getName() !== f.title) file.setName(f.title);
  if (props.getProperty(shaKey) === f.sha256) return false;
  const part = UrlFetchApp.fetch(base + encodeURIComponent(f.file), { muteHttpExceptions: true });
  if (part.getResponseCode() !== 200) throw new Error('HTTP ' + part.getResponseCode());
  // A part being replaced right now can be the old or the new version.
  if (hex_(Utilities.computeDigest(Utilities.DigestAlgorithm.SHA_256, part.getContent())) !== f.sha256) {
    throw new Error('not the version pack.json lists yet');
  }
  const doc = DocumentApp.openById(file.getId());
  doc.getBody().setText(part.getContentText('UTF-8'));
  doc.saveAndClose();
  props.setProperty(shaKey, f.sha256);
  console.log('updated ' + f.title + ' (' + commas_(f.chars) + ' characters)');
  return true;
}

/** Makes a part's Doc in the folder; returns its Drive file. */
function newDoc_(props, folder, f) {
  const doc = DocumentApp.create(f.title);
  // Remembered before anything else can fail, so a failed move doesn't
  // make another Doc next run.
  props.setProperty('doc:' + f.file, doc.getId());
  props.deleteProperty('sha:' + f.file);
  const file = DriveApp.getFileById(doc.getId());
  try {
    file.moveTo(folder);
  } catch (e) {
    console.warn(f.file + ': its new Doc stays in My Drive (' + e.message + ')');
  }
  return file;
}

/**
 * The Drive file of the Doc a part had, or null if it had none or it is in
 * the trash. When Drive can't get the file, the error is thrown, so the
 * part is tried again next run with the same Doc, until that has gone on
 * for GONE_AFTER_MS: then the Doc counts as deleted for good.
 */
function existingFile_(props, part) {
  const id = props.getProperty('doc:' + part);
  if (!id) return null;
  const sinceKey = 'missing:' + part;
  let file;
  try {
    file = DriveApp.getFileById(id);
  } catch (e) {
    const since = Number(props.getProperty(sinceKey)) || Date.now();
    props.setProperty(sinceKey, String(since));
    if (Date.now() - since < GONE_AFTER_MS) throw e;
    props.deleteProperty(sinceKey);
    console.warn(part + ': Drive has not found its Doc for three days, so it counts as deleted');
    return null;
  }
  props.deleteProperty(sinceKey);
  return file.isTrashed() ? null : file;
}

/** Empties and renames the Doc of a part that is gone, once. */
function retire_(props, part) {
  if (props.getProperty('sha:' + part) === 'unused') return;
  const file = existingFile_(props, part);
  if (file) {
    const doc = DocumentApp.openById(file.getId());
    doc.getBody().setText('This part is no longer published: its repository got smaller or was taken off the list. Remove this source from the notebook.');
    doc.saveAndClose();
    file.setName('(unused) ' + file.getName());
    console.log('emptied ' + part + ', which is no longer published');
  }
  props.setProperty('sha:' + part, 'unused');
}

/** 575670 as "575,670". */
function commas_(n) {
  return String(n).replace(/\B(?=(\d{3})+(?!\d))/g, ',');
}

/** The Docs' folder: the one made before, else one by that name, else a new one. */
function folder_(props) {
  const id = props.getProperty('folder');
  if (id) {
    try {
      const f = DriveApp.getFolderById(id);
      if (!f.isTrashed()) return f;
    } catch (e) {
      // gone: find or make another
    }
  }
  const found = DriveApp.getFoldersByName(FOLDER_NAME);
  while (found.hasNext()) {
    const f = found.next();
    if (!f.isTrashed()) {
      props.setProperty('folder', f.getId());
      return f;
    }
  }
  const f = DriveApp.createFolder(FOLDER_NAME);
  props.setProperty('folder', f.getId());
  return f;
}

/** Bytes (as Apps Script gives them, -128 to 127) as lowercase hex. */
function hex_(bytes) {
  return bytes.map(b => ((b + 256) % 256).toString(16).padStart(2, '0')).join('');
}
