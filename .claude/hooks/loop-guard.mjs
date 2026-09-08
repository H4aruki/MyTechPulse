#!/usr/bin/env node
// まったく同じ操作が3回続いたら止めて、状況の報告を求める。
//
// 限界: 少しずつ違うコマンドで迷走するループは捕まえられない。半分の対策。

import { readInput, emit, buildDeny } from './lib/io.mjs';
import { readState, writeState, fingerprint } from './lib/state.mjs';

const LIMIT = 3;

const input = await readInput();
const sessionId = input?.session_id ?? 'unknown';
const current = fingerprint(input);

const state = readState(sessionId);
const count = state.lastFingerprint === current ? (state.repeatCount ?? 1) + 1 : 1;

if (count >= LIMIT) {
  // 数え直しをゼロに戻しておく。人間が「そのまま進めて」と判断したときに、
  // 次の1回でまた止まってしまわないようにするため。
  writeState(sessionId, { ...state, lastFingerprint: current, repeatCount: 0 });
  emit(
    buildDeny(
      'PreToolUse',
      `まったく同じ操作を${LIMIT}回続けようとしています。うまくいっていない可能性が高いので、いったん止めます。何を試して何が起きたかを報告し、次にどうするかを相談してください。`,
    ),
  );
} else {
  writeState(sessionId, { ...state, lastFingerprint: current, repeatCount: count });
}
