# 小さい修正PRの自動マージ（判定の部分）

小さく安全な修正のPRだけを、機械的に「自動マージの対象か」判定する仕組みです。
このPRで入るのは判定だけです。マージの実行（`automerge.sh`）は別のPRで入ります。

## 何をするか

PRの変更ファイル、行数、検査の結果、レビューの状態を GitHub から**読むだけ**で取り、ルールに合うかを判定します。
PRのコードは checkout も実行もしません。取得に失敗したら「対象外」にします。

## ルールファイル

`.github/automerge-rules.json` に、しきい値と一覧をすべて置きます。コードには埋め込みません。

| キー | 意味 |
|---|---|
| `requiredChecks` | 必ず成功していなければならない検査 |
| `allowedPaths` / `excludedPaths` | 変更してよいパスと、その中でも除くパス |
| `excludedPathSubstrings` | パスに含むと対象外になる文字列（auth など） |
| `docsOnlyEligible` | 文書だけのPRを対象にするか |
| `limits` | テスト以外の変更行・ファイル数、全体の変更行の上限 |
| `testFilePatterns` | テストファイルとみなすパス |
| `forbiddenAddedPatterns` / `secretPatterns` | 追加行に現れてはならない書き方、鍵の形 |
| `blockLabel` | 付いていたら対象外にするラベル（`no-automerge`） |
| `decisionSection` / `decisionNoneValue` | 本文の「判断が必要なこと」が「なし」のときだけ通す |
| `mergeGuards` | マージ実行側が読む設定。判定では使わない |

## クラスA / B / C

- **A**: 対象。全部のルールに合った
- **B**: 対象外。大きさ、検査、状態、本文などで外れた（直せば対象になりうる）
- **C**: 対象外。パスが外れた（DB、認証、インフラなど。常に人が見る）

## 使い方

```bash
# 1本のPRを判定（終了コード 0=対象 1=対象外 2=取得失敗・引数エラー）
node scripts/automerge/eligibility.mjs --pr 123
node scripts/automerge/eligibility.mjs --pr 123 --json
node scripts/automerge/eligibility.mjs --pr 123 --comment-markdown

# マージ済みPRの直近40本で試算（読むだけ。コメントもラベルも付けない）
node scripts/automerge/eligibility.mjs --dry-run-merged 40 --out dryrun.md
```

`gh` コマンドでログイン済みであることが必要です。

dry-run の表は、PRごとのクラス・対象か・当たったルールidと、集計を出します。
過去のPRは旧テンプレートなので、本文ルールを除いた場合の本数も併記します。

## ルールを変えるPRは必ず人がマージする

`.github/**` と `scripts/automerge/**` を触るPRは、判定ルールで必ず対象外（クラスC）になります。
ルールを自分で緩めるPRを、自動で通さないためです。

## 毎週の見直し

毎週、dry-run の結果と実際のPRを見て、しきい値（`limits` など）が厳しすぎないか、緩すぎないかを見直します。
