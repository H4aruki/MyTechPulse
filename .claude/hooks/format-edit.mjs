#!/usr/bin/env node
// ファイルを編集した直後に、そのファイル1つだけを対象に書き方を確認する。
//
// 道具（gofmt / oxlint）が見つからない環境では黙って何もしない。
// 仕掛けの失敗で他のメンバーの作業が止まるほうが害が大きいため。

import { spawnSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { readInput } from './lib/io.mjs';

const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..', '..');

// 道具の置き場は環境によって違う。PATH に入っていないことのほうが多いので、
// このリポジトリで実際に使われている場所も順に見る。
function findTool(candidates) {
  for (const c of candidates) {
    if (c.absolute) {
      if (existsSync(c.path)) return c.path;
    } else {
      const probe = spawnSync(c.path, ['--version'], { stdio: 'ignore', shell: false });
      if (probe.status === 0) return c.path;
    }
  }
  return null;
}

// gofmt は Go に同梱される。PATH に無いことが多いので、既定の導入先も順に見る
const GOFMT = findTool([
  { path: 'gofmt', absolute: false },
  { path: 'C:\\Program Files\\Go\\bin\\gofmt.exe', absolute: true },
  { path: '/usr/local/go/bin/gofmt', absolute: true },
]);

const OXLINT = findTool([
  { path: join(repoRoot, 'frontend', 'node_modules', '.bin', 'oxlint.cmd'), absolute: true },
  { path: join(repoRoot, 'frontend', 'node_modules', '.bin', 'oxlint'), absolute: true },
]);

const input = await readInput();
const file = input?.tool_response?.filePath ?? input?.tool_input?.file_path ?? '';
if (!file) process.exit(0);

function run(cmd, args) {
  if (!cmd) return;
  try {
    spawnSync(cmd, args, { stdio: 'ignore', shell: false, timeout: 20000, cwd: repoRoot });
  } catch {
    // 失敗しても何もしない
  }
}

if (file.endsWith('.go')) {
  // 利用者が確認していない変更を増やさないよう、自動修正はしない（-l は、整っていないfileを列挙するだけ）
  run(GOFMT, ['-l', file]);
} else if (/\.(ts|tsx|js|jsx|mjs)$/.test(file)) {
  // このリポジトリのフロントエンドには整形の道具が無いので検査だけ行う。
  // CIと同じ基準で早めに壊れに気づける
  run(OXLINT, [file]);
}
