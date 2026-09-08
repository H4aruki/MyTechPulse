#!/usr/bin/env node
// 終了しようとしたときに、コードを編集したのに検査を一度も実行していなければ止める。
//
// 抜けられなくなる事故が起きない理由: 検査を実行すれば、結果が失敗でも
// 条件は解除される。実行しさえすれば必ず抜けられる。
//
// 唯一の安全弁: 何の操作もせずに終了だけを繰り返した場合、3回目で通す。
// 検査でもコード修正でもなく終了の再試行だけが続く完全な膠着状態で、
// 止め続けても何も進まないため。何か操作をすれば数え直しはゼロに戻る。

import { readInput, emit, buildStopBlock } from './lib/io.mjs';
import { readState, writeState } from './lib/state.mjs';

const STUCK_LIMIT = 3;

const input = await readInput();
const sessionId = input?.session_id ?? 'unknown';
const state = readState(sessionId);

if (!state.needsVerify) process.exit(0);

const stuckCount = (state.stuckCount ?? 0) + 1;

if (stuckCount >= STUCK_LIMIT) {
  // 膠着しているので通す。ただし何も確認できていないことは画面に残す
  writeState(sessionId, { ...state, needsVerify: false, stuckCount: 0 });
  emit({
    systemMessage:
      '検査を実行しないまま終了しようとする状態が続いたため、確認を打ち切りました。この作業の結果は確認できていません。',
  });
  process.exit(0);
}

writeState(sessionId, { ...state, stuckCount });

emit(
  buildStopBlock(
    'コードを編集しましたが、検査も組み立ても一度も実行していません。終わりにする前に実際に動かして、その出力を確認してください。' +
      '\n  サーバー側: ruff check backend' +
      '\n  画面側: cd frontend && npm run lint && npm run build' +
      '\n  設定まわりの仕掛け: node --test ".claude/hooks/**/*.test.mjs"' +
      '\n結果が失敗でも構いません。失敗しているならその内容をそのまま報告してください。',
  ),
);
