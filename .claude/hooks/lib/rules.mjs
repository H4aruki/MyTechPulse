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

const RULES = [
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
