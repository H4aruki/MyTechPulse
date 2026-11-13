// コマンド文字列を見て、リポジトリの禁止事項に当たるかを判定する。
// 外部に触らない純粋な判定だけを置く（テストしやすさのため）。
//
// 限界: よくある書き方を止めるものであって、意図的な回避はできる。
// 事故防止であって、悪意への防御ではない。

// つなぎ記号で分解する。`ls && cat .env` のように隠されても1つずつ見るため。
export function splitSegments(command) {
  return String(command || '')
    .split(/&&|\|\||;|\||\n/)
    .map((s) => s.trim())
    .filter(Boolean);
}

// コマンドを単語に切り、囲みの記号と入力の向きを変える記号を落とす。
// 「行のどこかに見本ファイルがあれば全部見逃す」を避けるため、ファイルは
// 1つずつ判定する必要がある。
function tokens(segment) {
  return String(segment)
    .split(/\s+/)
    .map((t) => t.replace(/^[<>"']+/, '').replace(/["']+$/, ''))
    .filter(Boolean);
}

// 見本ファイルと、機密でないと明記された開発用の設定は対象外
const SAFE_ENV = /\.env\.(example|development)$/;
const ENV_TOKEN = /(^|[/\\])\.env$/;

// 中身を見ずに有無だけを見るものは止めない
const EXISTENCE_ONLY = /^(ls|dir|stat|test|file|Test-Path|Get-Item)$/;

// コマンドの前に付きうる飾りを落とす。`sudo git push --force` のような形でも
// 本体のコマンドで判定できるようにするため。
function stripWrappers(segment) {
  let s = String(segment).trim();
  for (;;) {
    const next = s.replace(/^(sudo|time|command|env|nohup|[A-Za-z_][A-Za-z0-9_]*=\S*)\s+/, '');
    if (next === s) return s;
    s = next;
  }
}

// Codex workerのログを丸ごと表示するコマンド。head・tail・grep のように
// 範囲が絞れるものは止めない。
const WORKER_LOG_TOKEN = /\.codex\.log$/i;
const DUMPING_READERS = /^(cat|type|more|less|bat|nl|Get-Content|gc)$/i;
const PS_RANGE_OPTION = /(^|\s)-(Tail|TotalCount|First|Last)/i;

const WORKER_LOG_MESSAGE =
  'Codex workerのログは大きいので、全文は読まないでください。失敗時は結果の末尾の数行を見て、足りなければ tail や grep など範囲を絞って読みます。';

// 引用の中は1語にする。コミットメッセージ内の --no-verify を、オプションと見分けないため。
function shellWords(segment) {
  const words = [];
  const s = String(segment);
  let i = 0;
  while (i < s.length) {
    while (i < s.length && /\s/.test(s[i])) i += 1;
    if (i >= s.length) break;
    let word = '';
    let quote = '';
    while (i < s.length) {
      const c = s[i];
      if (quote) {
        if (c === quote) {
          quote = '';
          i += 1;
          continue;
        }
        if (c === '\\' && quote === '"' && i + 1 < s.length) {
          word += s[i + 1];
          i += 2;
          continue;
        }
        word += c;
        i += 1;
        continue;
      }
      if (c === '"' || c === "'") {
        quote = c;
        i += 1;
        continue;
      }
      if (/\s/.test(c)) break;
      word += c;
      i += 1;
    }
    if (word) words.push(word);
  }
  return words;
}

function wordIsNoVerify(word) {
  return word === '--no-verify' || word.startsWith('--no-verify=');
}

// git のグローバルオプションを飛ばし、サブコマンドと -c の代入を取り出す。
function parseGit(segment) {
  const ts = shellWords(segment);
  if (ts[0] !== 'git') return null;
  const configAssignments = [];
  let i = 1;
  while (i < ts.length) {
    const t = ts[i];
    if (t === '--') {
      return { sub: ts[i + 1] ?? '', args: ts.slice(i + 2), configAssignments };
    }
    if (t === '-c' || t === '-C') {
      const arg = ts[i + 1];
      if (t === '-c' && arg !== undefined) configAssignments.push(arg);
      i += arg === undefined ? 1 : 2;
      continue;
    }
    if (t.startsWith('-C') && t.length > 2) {
      i += 1;
      continue;
    }
    if (t.startsWith('-c') && t.length > 2) {
      configAssignments.push(t.slice(2));
      i += 1;
      continue;
    }
    if (t.startsWith('--')) {
      const body = t.slice(2);
      const name = body.split('=')[0];
      const hasEq = body.includes('=');
      const takesArg =
        name === 'git-dir' ||
        name === 'work-tree' ||
        name === 'namespace' ||
        name === 'super-prefix' ||
        name === 'config-env' ||
        name === 'list-cmds';
      i += !hasEq && takesArg ? 2 : 1;
      continue;
    }
    if (t.startsWith('-')) {
      i += 1;
      continue;
    }
    return { sub: t, args: ts.slice(i + 1), configAssignments };
  }
  return { sub: '', args: [], configAssignments };
}

function setsHooksPath(assignment) {
  return /^core\.hookspath(?:=|$)/i.test(String(assignment));
}

const CONFIG_READ = new Set(['get', 'get-all', 'get-regexp', 'get-urlmatch', 'list', 'l']);
const CONFIG_WRITE = new Set(['add', 'replace-all', 'unset', 'unset-all']);
const CONFIG_WITH_ARG = new Set(['file', 'f', 'blob', 'default', 'type', 'comment', 'includes']);

// 値を書く・消す変更だけを止める。引数なしの参照と --get は許可する。
function configWritesHooksPath(args) {
  let mode = 'implicit';
  const positionals = [];
  for (let i = 0; i < args.length; i++) {
    const t = args[i];
    if (t === '--') {
      positionals.push(...args.slice(i + 1));
      break;
    }
    if (t.startsWith('--')) {
      const body = t.slice(2);
      const eq = body.indexOf('=');
      const name = eq === -1 ? body : body.slice(0, eq);
      if (CONFIG_READ.has(name) && mode !== 'write') mode = 'read';
      if (CONFIG_WRITE.has(name)) mode = 'write';
      if (eq === -1 && CONFIG_WITH_ARG.has(name)) i += 1;
      continue;
    }
    if (t.startsWith('-') && t.length > 1) {
      if (t.slice(1).includes('l')) mode = 'read';
      if (t === '-f') i += 1;
      continue;
    }
    positionals.push(t);
  }
  const idx = positionals.findIndex((p) => /^core\.hookspath$/i.test(p));
  if (idx === -1) return false;
  if (mode === 'read') return false;
  if (mode === 'write') return true;
  return positionals.length > idx + 1;
}

// git commit の短い -n は --no-verify。引数を取る文字の後ろはメッセージなので見ない。
const COMMIT_TAKES_ARG = new Set(['C', 'c', 'F', 'm', 't']);
const COMMIT_OPTIONAL_ARG = new Set(['u', 'S']);

// -m などの引数に入った文字列は、フックを飛ばす指定ではない。
function commitSkipsVerify(args) {
  for (let i = 0; i < args.length; i++) {
    const t = args[i];
    if (t === '--') break;
    if (wordIsNoVerify(t)) return true;
    if (!t.startsWith('-') || t.startsWith('--')) continue;
    const letters = t.slice(1);
    for (let j = 0; j < letters.length; j++) {
      const ch = letters[j];
      if (ch === 'n') return true;
      if (COMMIT_TAKES_ARG.has(ch)) {
        if (j === letters.length - 1) i += 1;
        break;
      }
      if (COMMIT_OPTIONAL_ARG.has(ch) && j < letters.length - 1) break;
    }
  }
  return false;
}

function gitSkipsVerify(segment) {
  const parsed = parseGit(segment);
  if (!parsed) return false;
  if (parsed.sub === 'commit') return commitSkipsVerify(parsed.args);
  return shellWords(segment).some(wordIsNoVerify);
}

function gitOverridesHooksPath(segment) {
  const parsed = parseGit(segment);
  if (!parsed) return false;
  if (parsed.configAssignments.some(setsHooksPath)) return true;
  return parsed.sub === 'config' && configWritesHooksPath(parsed.args);
}

const RULES = [
  {
    id: 'worker-log-full-read',
    test: (s) => {
      if (!tokens(s).some((t) => WORKER_LOG_TOKEN.test(t))) return false;
      const command = stripWrappers(s).split(/\s+/)[0] ?? '';
      return DUMPING_READERS.test(command) && !PS_RANGE_OPTION.test(s);
    },
    message: WORKER_LOG_MESSAGE,
  },
  {
    id: 'secret-file',
    // 接続情報ファイルを指す単語が1つでもあれば止める。読む・写す・記録に加える
    // のいずれであっても漏れるため、コマンドの種類では絞らない。
    test: (s) => {
      const ts = tokens(s);
      const touchesSecret = ts.some((t) => ENV_TOKEN.test(t) && !SAFE_ENV.test(t));
      if (!touchesSecret) return false;
      return !EXISTENCE_ONLY.test(stripWrappers(s).split(/\s+/)[0] ?? '');
    },
    message:
      '接続情報ファイル（.env）を読んだり持ち出したりする操作は禁止です。項目名を知りたいときは .env.example を見てください。値そのものが必要な作業は、オーナーに依頼してください。',
  },
  {
    id: 'force-push',
    test: (s) =>
      /^git\s+push\b/.test(s) && /(--force\b|--force-with-lease\b|(^|\s)-f(\s|$))/.test(s),
    message:
      '履歴の強制上書きは禁止です。他のメンバーの作業が消えます。取り込み済みの内容を直したいときは、新しいコミットを積んでください。',
  },
  {
    id: 'push-to-main',
    test: (s) => /^git\s+push\b/.test(s) && /(\s|:)main(\s|$)/.test(s),
    message:
      'main への直接反映は禁止です。main へ入ると本番公開まで自動で走ります（.github/workflows/ci.yml）。作業用の枝へ送って、プルリクエスト経由で取り込んでください。',
  },
  {
    id: 'skip-verify',
    test: gitSkipsVerify,
    message:
      'フックを飛ばす指定（--no-verify や git commit の -n）は禁止です。秘密情報の検査などが動かなくなります。通常の git commit や git push を使ってください。git push -n は送らない確認なので、そのまま使えます。',
  },
  {
    id: 'hooks-path',
    test: gitOverridesHooksPath,
    message:
      'Gitフックの場所（core.hooksPath）を書き換える操作は禁止です。フックを外すと、秘密情報の検査などが飛ばされます。有効化は ./scripts/setup-hooks.sh を使い、今の値を見るだけなら git config core.hooksPath と引数なしで読んでください。',
  },
  {
    id: 'pr-merge-or-approve',
    // gh と pr の間に別の指定が入ることがあるので、間を許して照合する
    test: (s) =>
      /^gh\b/.test(s) &&
      (/\bpr\s+merge\b/.test(s) || (/\bpr\s+review\b/.test(s) && /\s--approve\b/.test(s))),
    message:
      'プルリクエストの承認と取り込みは、人間だけが行います。自分の書いたものを自分で通せてしまうためです。準備ができたことを報告して、オーナーの判断を待ってください。',
  },
  {
    id: 'drop-volumes',
    test: (s) =>
      (/^docker\s+compose\s+down\b/.test(s) && /(--volumes\b|(^|\s)-v(\s|$))/.test(s)) ||
      /^docker\s+volume\s+rm\b/.test(s),
    message:
      'データの入れ物ごと消す操作は禁止です。データベースの中身が丸ごと失われます。コンテナを止めたいだけなら docker compose down を使ってください。',
  },
  {
    id: 'production-access',
    test: (s) => /^ssh\b/.test(s) || /\bwrangler\b/.test(s),
    message:
      '本番サーバーへの直接接続と手動での公開は禁止です。公開は main に取り込まれたときの自動処理に一本化してあります（.github/workflows/ci.yml）。',
  },
];

export function checkCommand(command) {
  for (const raw of splitSegments(command)) {
    // 飾りを落としたうえで判定する（接続情報の判定だけは元の並びも見るため
    // 自前で落とし直している）
    const segment = stripWrappers(raw);
    for (const rule of RULES) {
      if (rule.test(segment)) {
        return { blocked: true, id: rule.id, message: rule.message };
      }
    }
  }
  return { blocked: false };
}

// Codex workerのログは大きいので、範囲を決めずに丸ごと読むのを止める。
const WORKER_LOG_MAX_LINES = 100;

export function checkWorkerLogRead(toolInput) {
  const path = String(toolInput?.file_path ?? '');
  if (!WORKER_LOG_TOKEN.test(path)) return { blocked: false };
  const limit = Number(toolInput?.limit);
  if (Number.isFinite(limit) && limit > 0 && limit <= WORKER_LOG_MAX_LINES) {
    return { blocked: false };
  }
  return {
    blocked: true,
    id: 'worker-log-full-read',
    message: `${WORKER_LOG_MESSAGE}（Readなら limit を${WORKER_LOG_MAX_LINES}以下にする、またはGrepで検索する）`,
  };
}
