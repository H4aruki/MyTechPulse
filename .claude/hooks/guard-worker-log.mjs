#!/usr/bin/env node
// Codex workerのログを、範囲を決めずに丸ごと読む操作を止める。
// 判定そのものは lib/rules.mjs にある。
//
// 限界: 止められるのはReadツールだけ。cat などコマンド経由の読み方は止めない。

import { readInput, emit, buildDeny } from './lib/io.mjs';
import { checkWorkerLogRead } from './lib/rules.mjs';

const input = await readInput();

const result = checkWorkerLogRead(input?.tool_input);
if (result.blocked) {
  emit(buildDeny('PreToolUse', result.message));
}
