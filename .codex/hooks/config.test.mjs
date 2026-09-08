import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';
import assert from 'node:assert/strict';

const codexDir = join(dirname(fileURLToPath(import.meta.url)), '..');
const config = JSON.parse(readFileSync(join(codexDir, 'hooks.json'), 'utf8'));

test('Codexの主要なライフサイクルへフックを登録している', () => {
  for (const event of ['PreToolUse', 'PostToolUse', 'SessionStart', 'Stop']) {
    assert.ok(Array.isArray(config.hooks[event]), event);
    assert.ok(config.hooks[event].length > 0, event);
  }
});

test('すべてのコマンドフックにWindows用の実行方法がある', () => {
  for (const groups of Object.values(config.hooks)) {
    for (const group of groups) {
      for (const hook of group.hooks) {
        assert.equal(hook.type, 'command');
        assert.match(hook.commandWindows, /^powershell\.exe /);
      }
    }
  }
});

test('フックはリポジトリの共通実装を呼び出す', () => {
  const serialized = JSON.stringify(config);
  for (const script of [
    'guard-command.mjs',
    'loop-guard.mjs',
    'mark-verified.mjs',
    'session-start.mjs',
    'verify-gate.mjs',
  ]) {
    assert.match(serialized, new RegExp(script.replace('.', '\\.')));
  }
});
