/// <reference path="../../packages/pocketci/src/global.d.ts" />

// Pure-JS resource version store built on the storage.set/get primitives.
// Replaces the four Go-backed storage.saveResourceVersion/getLatestResourceVersion/
// listResourceVersions/getVersionsAfter methods that were removed from the Driver
// interface.  All keys are scoped under /rv/{name}/… so they are namespaced by
// the storage Driver's own namespace automatically.

function zeroPad(num: number, places: number): string {
  return String(num).padStart(places, "0");
}

// djb2-style 32-bit hash turned into a hex string.  Good enough for dedup
// keying — we additionally store the original JSON alongside so hash
// collisions are caught and handled.
function hashString(str: string): string {
  let h = 5381;
  for (let i = 0; i < str.length; i++) {
    h = Math.imul(h, 31) ^ str.charCodeAt(i);
  }
  return (h >>> 0).toString(16);
}

export interface StoredVersion {
  version: Record<string, string>;
  job_name: string;
  fetched_at: string;
}

function metaKey(name: string): string {
  return `/rv/${name}/meta`;
}

function versionKey(name: string, index: number): string {
  return `/rv/${name}/versions/${zeroPad(index, 10)}`;
}

function dedupKey(name: string, versionJSON: string): string {
  return `/rv/${name}/v/${hashString(versionJSON)}`;
}

// Safely call storage.get().  Returns null when the key is not found (the Go
// driver throws ErrNotFound which Goja surfaces as an exception).
function safeGet(key: string): unknown {
  try {
    return storage.get(key);
  } catch (_e) {
    return null;
  }
}

export function saveResourceVersion(
  name: string,
  version: Record<string, string>,
  jobName: string,
): void {
  const versionJSON = JSON.stringify(version);
  const now = new Date().toISOString();
  const dk = dedupKey(name, versionJSON);

  const dedupEntry = safeGet(dk) as
    | { index: number; version_json: string }
    | null;
  if (dedupEntry !== null && dedupEntry !== undefined) {
    // Confirm not a hash collision before skipping insert.
    if (dedupEntry.version_json === versionJSON) {
      // Known version – only update mutable fields.
      const vk = versionKey(name, dedupEntry.index);
      const existing = safeGet(vk) as StoredVersion | null;
      if (existing) {
        storage.set(vk, { ...existing, job_name: jobName, fetched_at: now });
      }
      return;
    }
    // Hash collision – fall through and insert as a new entry.
  }

  const metaData = safeGet(metaKey(name)) as { count: number } | null;
  const count = metaData?.count ?? 0;

  storage.set(versionKey(name, count), {
    version,
    job_name: jobName,
    fetched_at: now,
  });
  storage.set(dk, { index: count, version_json: versionJSON });
  storage.set(metaKey(name), { count: count + 1 });
}

export function getLatestResourceVersion(name: string): StoredVersion | null {
  const metaData = safeGet(metaKey(name)) as { count: number } | null;
  const count = metaData?.count ?? 0;
  if (count <= 0) {
    return null;
  }
  return safeGet(versionKey(name, count - 1)) as StoredVersion | null;
}

export function listResourceVersions(
  name: string,
  limit: number,
): StoredVersion[] {
  const metaData = safeGet(metaKey(name)) as { count: number } | null;
  const count = metaData?.count ?? 0;
  const actualCount = limit > 0 ? Math.min(limit, count) : count;

  const versions: StoredVersion[] = [];
  for (let i = 0; i < actualCount; i++) {
    const v = safeGet(versionKey(name, i)) as StoredVersion | null;
    if (v) versions.push(v);
  }
  return versions;
}

export function getVersionsAfter(
  name: string,
  afterVersion: Record<string, string> | null,
): StoredVersion[] {
  if (afterVersion === null || afterVersion === undefined) {
    return listResourceVersions(name, 0);
  }

  const metaData = safeGet(metaKey(name)) as { count: number } | null;
  const count = metaData?.count ?? 0;
  const afterJSON = JSON.stringify(afterVersion);

  let afterIndex = -1;
  for (let i = 0; i < count; i++) {
    const v = safeGet(versionKey(name, i)) as StoredVersion | null;
    if (v && JSON.stringify(v.version) === afterJSON) {
      afterIndex = i;
      break;
    }
  }

  if (afterIndex === -1) {
    return listResourceVersions(name, 0);
  }

  const results: StoredVersion[] = [];
  for (let i = afterIndex + 1; i < count; i++) {
    const v = safeGet(versionKey(name, i)) as StoredVersion | null;
    if (v) results.push(v);
  }
  return results;
}
