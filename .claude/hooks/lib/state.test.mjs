import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fingerprint, readState, writeState } from './state.mjs';

test('同じ操作は同じ文字列になる', () => {
  const a = { tool_name: 'Bash', tool_input: { command: 'npm run lint' } };
  const b = { tool_name: 'Bash', tool_input: { command: 'npm run lint' } };
  assert.equal(fingerprint(a), fingerprint(b));
});

test('違う操作は違う文字列になる', () => {
  const a = { tool_name: 'Bash', tool_input: { command: 'npm run lint' } };
  const b = { tool_name: 'Bash', tool_input: { command: 'npm run build' } };
  assert.notEqual(fingerprint(a), fingerprint(b));
});

test('書いた内容がそのまま読める', () => {
  const id = 'test-session-' + Date.now();
  writeState(id, { last: 'abc', count: 2 });
  assert.deepEqual(readState(id), { last: 'abc', count: 2 });
});

test('無いセッションを読んでも壊れない', () => {
  assert.deepEqual(readState('does-not-exist-' + Date.now()), {});
});
