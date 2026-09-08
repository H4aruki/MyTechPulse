#!/usr/bin/env node
// コードを編集したこと／検査を実行したことを覚え書きに記録する。
// 終了しようとしたときの判定（verify-gate.mjs）が、この記録を見る。

import { readInput } from './lib/io.mjs';
import { readState, writeState } from './lib/state.mjs';

const CODE_FILE = /\.(py|ts|tsx|js|jsx|mjs)$/;
const VERIFY_COMMAND = /\b(ruff|oxlint|tsc|pytest)\b|npm run (lint|build)|node --test/;

const input = await readInput();
const sessionId = input?.session_id ?? 'unknown';
const state = readState(sessionId);

if (input?.tool_name === 'Bash') {
  const command = input?.tool_input?.command ?? '';
  if (VERIFY_COMMAND.test(command)) {
    // 検査を実行した。結果が失敗でも記録する（実物の出力を見たことが大事）
    writeState(sessionId, { ...state, needsVerify: false });
  }
} else {
  // Claude Codeはファイルパスを、Codexのapply_patchは差分本文を渡す。
  // どちらでもコード変更を見つけられるよう、候補をまとめて判定する。
  const edit =
    input?.tool_response?.filePath ??
    input?.tool_input?.file_path ??
    input?.tool_input?.command ??
    '';
  if (CODE_FILE.test(edit)) {
    writeState(sessionId, { ...state, needsVerify: true, stuckCount: 0 });
  }
}
