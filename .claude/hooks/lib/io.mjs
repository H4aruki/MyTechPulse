// 各フックの入口ファイルが共通で使う、入力の読み取りと出力の組み立て。
// Claude Code はフックの標準入力にJSONを渡し、標準出力のJSONで挙動を決める。

export function buildDeny(hookEventName, reason) {
  return {
    hookSpecificOutput: {
      hookEventName,
      permissionDecision: 'deny',
      permissionDecisionReason: reason,
    },
  };
}

export function buildContext(hookEventName, additionalContext) {
  return { hookSpecificOutput: { hookEventName, additionalContext } };
}

export function buildStopBlock(reason) {
  return { decision: 'block', reason };
}

// 標準入力を最後まで読む。空でも壊れていても、作業を止めないよう空の入れ物を返す。
export async function readInput() {
  const chunks = [];
  for await (const chunk of process.stdin) chunks.push(chunk);
  const raw = Buffer.concat(chunks).toString('utf8').trim();
  if (!raw) return {};
  try {
    return JSON.parse(raw);
  } catch {
    return {};
  }
}

export function emit(obj) {
  process.stdout.write(JSON.stringify(obj));
}
