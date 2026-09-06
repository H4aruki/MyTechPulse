// セッションごとの小さな覚え書きを作業ファイルとして置く。
// 置き場は .claude/.cache/（無視指定してあるのでコミットされない）。

import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));

function cacheDir() {
  const dir = join(here, '..', '..', '.cache');
  mkdirSync(dir, { recursive: true });
  return dir;
}

function pathFor(sessionId) {
  const safe = String(sessionId || 'unknown').replace(/[^A-Za-z0-9_-]/g, '_');
  return join(cacheDir(), `${safe}.json`);
}

export function readState(sessionId) {
  try {
    return JSON.parse(readFileSync(pathFor(sessionId), 'utf8'));
  } catch {
    return {};
  }
}

export function writeState(sessionId, obj) {
  try {
    writeFileSync(pathFor(sessionId), JSON.stringify(obj), 'utf8');
  } catch {
    // 書けなくても作業は止めない
  }
}

// 操作を1つの文字列にまとめる。同じ操作かどうかの判定に使う。
export function fingerprint(input) {
  const tool = input?.tool_name ?? '';
  const ti = input?.tool_input ?? {};
  const detail = ti.command ?? `${ti.file_path ?? ''}:${(ti.old_string ?? '').slice(0, 200)}`;
  return `${tool} ${detail}`;
}
