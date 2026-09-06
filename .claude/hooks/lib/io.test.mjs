import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildDeny, buildContext, buildStopBlock } from './io.mjs';

test('denyは、操作を実行させない形のJSONを作る', () => {
  const out = buildDeny('PreToolUse', 'だめな理由');
  assert.equal(out.hookSpecificOutput.hookEventName, 'PreToolUse');
  assert.equal(out.hookSpecificOutput.permissionDecision, 'deny');
  assert.equal(out.hookSpecificOutput.permissionDecisionReason, 'だめな理由');
});

test('contextは、会話の冒頭に足す文章を運ぶ', () => {
  const out = buildContext('SessionStart', 'いまの枝: main');
  assert.equal(out.hookSpecificOutput.hookEventName, 'SessionStart');
  assert.equal(out.hookSpecificOutput.additionalContext, 'いまの枝: main');
});

test('stopBlockは、終了を止める形のJSONを作る', () => {
  const out = buildStopBlock('検査をしてから終わってください');
  assert.equal(out.decision, 'block');
  assert.equal(out.reason, '検査をしてから終わってください');
});
