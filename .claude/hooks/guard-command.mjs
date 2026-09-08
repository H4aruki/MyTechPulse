#!/usr/bin/env node
// コマンドを実行する直前に走り、リポジトリの禁止事項に当たるものを止める。
// 判定そのものは lib/rules.mjs にある。
//
// この仕掛けが要る理由: 許可設定が見ているのは「どの道具を呼んだか」なので、
// ファイルを直接読む操作は止まっても、コマンド経由で同じことをされると
// 素通りしてしまう。その穴をここで塞ぐ。

import { readInput, emit, buildDeny } from './lib/io.mjs';
import { checkCommand } from './lib/rules.mjs';

const input = await readInput();
const command = input?.tool_input?.command ?? '';

const result = checkCommand(command);
if (result.blocked) {
  emit(buildDeny('PreToolUse', result.message));
}
// 当たらなければ何も出さずに終わる（そのまま実行される）
