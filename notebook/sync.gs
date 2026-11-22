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

function syncNow_() {
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
  let updated = 0;
  for (const f of manifest.files) {
    wanted[f.file] = true;
    const docKey = 'doc:' + f.file;
    const shaKey = 'sha:' + f.file;
    let doc = openDoc_(props.getProperty(docKey));
    if (!doc) {
      doc = DocumentApp.create(f.title);
      DriveApp.getFileById(doc.getId()).moveTo(folder);
      props.setProperty(docKey, doc.getId());
      props.deleteProperty(shaKey);
    }
    if (doc.getName() !== f.title) doc.setName(f.title);
    if (props.getProperty(shaKey) === f.sha256) continue;
    const part = UrlFetchApp.fetch(base + encodeURIComponent(f.file), { muteHttpExceptions: true });
    if (part.getResponseCode() !== 200) {
      console.warn(f.file + ': HTTP ' + part.getResponseCode() + ', trying again next time');
      continue;
    }
    // A part being replaced right now can be the old or the new version.
    if (hex_(Utilities.computeDigest(Utilities.DigestAlgorithm.SHA_256, part.getContent())) !== f.sha256) {
      console.warn(f.file + ': not the version pack.json lists yet, trying again next time');
      continue;
    }
    doc.getBody().setText(part.getContentText('UTF-8'));
    doc.saveAndClose();
    props.setProperty(shaKey, f.sha256);
    updated++;
    console.log('updated ' + f.title + ' (' + f.chars + ' characters)');
  }
  // A part that is gone (its repository got smaller or left repos.txt)
  // keeps its Doc, emptied and renamed, so the notebook shows what to
  // remove instead of old code.
  for (const key of props.getKeys()) {
    if (!key.startsWith('doc:')) continue;
    const file = key.slice(4);
    if (wanted[file] || props.getProperty('sha:' + file) === 'unused') continue;
    const doc = openDoc_(props.getProperty(key));
    if (doc) {
      doc.setName('(unused) ' + doc.getName());
      doc.getBody().setText('This part is no longer published: its repository got smaller or was taken off the list. Remove this source from the notebook.');
      doc.saveAndClose();
      console.log('emptied ' + file + ', which is no longer written');
    }
    props.setProperty('sha:' + file, 'unused');
  }
  const n = manifest.files.length;
  console.log(updated ? 'updated ' + updated + ' of ' + n + ' Docs' : 'all ' + n + ' Docs were up to date');
}

/** The Doc with this id, or null if there is none (deleted or in the trash). */
function openDoc_(id) {
  if (!id) return null;
  try {
    if (DriveApp.getFileById(id).isTrashed()) return null;
    return DocumentApp.openById(id);
  } catch (e) {
    return null;
  }
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
