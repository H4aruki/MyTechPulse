#!/usr/bin/env node
// 作業を始めるときに、いまの状況だけを読み込ませる。
//
// 残タスクの一覧は入れない。状況は毎回変わるので自動で入れる価値があるが、
// 残タスクは変わらないので必要なときに TASKS.md を読めば足りる。

import { spawnSync } from 'node:child_process';
import { emit, buildContext } from './lib/io.mjs';

const LIMIT_BYTES = 1024;

function capture(cmd, args) {
  try {
    const r = spawnSync(cmd, args, { encoding: 'utf8', shell: false, timeout: 8000 });
    return r.status === 0 ? (r.stdout ?? '').trim() : '';
  } catch {
    return '';
  }
}

const lines = [];

const branch = capture('git', ['branch', '--show-current']);
if (branch) lines.push(`いまの枝: ${branch}`);

const dirty = capture('git', ['status', '--porcelain']);
lines.push(`コミットしていない変更: ${dirty ? dirty.split('\n').length : 0}件`);

// 一覧は素のまま受け取って自分で組み立てる。書式を指定する記法は
// 改行の書き方が環境で壊れやすく、黙って空になるため使わない。
function ghList(kind) {
  const raw = capture('gh', [
    kind, 'list', '--state', 'open', '--limit', '5', '--json', 'number,title',
  ]);
  if (!raw) return '';
  try {
    return JSON.parse(raw)
      .map((x) => `#${x.number} ${x.title}`)
      .join('\n');
  } catch {
    return '';
  }
}

const prs = ghList('pr');
if (prs) lines.push(`開いているプルリクエスト:\n${prs}`);

const issues = ghList('issue');
if (issues) lines.push(`開いている課題（新しい順に5件）:\n${issues}`);

lines.push('残タスクの全体像は TASKS.md にある（必要になったら読む）。');

let text = lines.join('\n');
if (Buffer.byteLength(text, 'utf8') > LIMIT_BYTES) {
  text = Buffer.from(text, 'utf8').subarray(0, LIMIT_BYTES).toString('utf8') + '\n…（省略）';
}

emit(buildContext('SessionStart', text));
