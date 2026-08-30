# コントリビューションガイド

MyTechPulse の開発ルールをまとめたものです。チームで安全かつ綺麗に開発を進めるため、以下に沿って作業してください。

---

## 1. ブランチ戦略（GitHub Flow）

- **`main` は常にデプロイ可能な状態を保つ**。`main` へ直接 push しない。作業は必ずブランチを切る。
- ブランチ名は `<type>/<短い説明>` の形式。`<type>` はコミットメッセージの type と共通の語彙を使い、説明は**英小文字のケバブケース**で書く。

  | 例 | 用途 |
  |----|------|
  | `fix/recommend-typo` | バグ修正 |
  | `feat/keyword-search` | 新機能 |
  | `docs/contributing` | ドキュメント |
  | `refactor/news-service` | リファクタ |

### 作業フロー

```
Issue作成 → ブランチ作成 → コミット → push → PR作成 → レビュー → main へマージ
```

1. **Issue を立てる**（何をやるか・なぜやるかを書く）
2. `main` から作業ブランチを切る
   ```bash
   git switch main
   git pull
   git switch -c fix/recommend-typo
   ```
3. コミット規約（下記2章）に沿ってコミットする
4. push して **Pull Request** を作成する（PRテンプレートに沿って記入）
5. セルフレビュー → レビュー依頼 → 承認をもらう
6. **Squash and merge** で `main` にマージする（`main` の履歴を1機能=1コミットに保つ）
7. マージ済みブランチは削除する

---

## 2. コミットメッセージ規約（Conventional Commits）

### 形式

```
<type>(<scope>): <概要>

<本文（任意）>

<フッター（任意）>
```

- **`<概要>`**: 日本語・命令形寄り・**末尾に句点（。）を付けない**・50字以内を目安
- **`(<scope>)`**: 任意。変更範囲を表す（例: `recommend` / `news` / `auth` / `frontend` / `db`）
- **本文**: 任意。空行を1つ挟んでから、日本語で「**なぜ**その変更をしたか」を中心に書く（1行72字を目安に折り返す）
- **フッター**: 任意。`Closes #12` のように関連 Issue を紐付ける

### type 一覧

| type | 用途 |
|------|------|
| `feat` | 新機能 |
| `fix` | バグ修正 |
| `docs` | ドキュメントのみの変更 |
| `style` | 挙動に影響しない整形（空白・フォーマット・セミコロン等） |
| `refactor` | 挙動を変えないコード改善 |
| `test` | テストの追加・修正 |
| `chore` | ビルド・設定・依存・雑務（例: `.gitignore` 追加） |

### 例

```
fix(recommend): クリック学習が失敗するtypoを修正

get_recommendation_by_user_and_tag のフィルタが user_ID と
tag_id を比較していたため既存レコードが取得できず、複合PK重複で
IntegrityErrorになっていた。tag_ID との比較に修正。

Closes #1
```

その他の短い例:

```
feat(news): キーワード検索を追加
docs: READMEのpip installのtypoとセットアップ手順を修正
chore: .gitignoreとPR/Issueテンプレートを追加
```

### コミットテンプレートの利用（任意・推奨）

リポジトリ同梱の `.gitmessage.txt` を有効化すると、`git commit` 時に雛形とガイドが表示されます。各自ローカルで一度だけ設定してください。

```bash
git config commit.template .gitmessage.txt
```

---

## 3. Pull Request の進め方

- PR テンプレート（`.github/pull_request_template.md`）に沿って記入する
- **まず自分でセルフレビュー**（差分を見返す）してからレビューを依頼する
- 動作確認の手順と結果を PR に書く
- **最低1人の承認**を目安にマージする

### AI（Claude 等）が作成した PR の扱い

- **AI が作成した PR は、必ず人間がレビュー・承認を行う**。AI が作成した変更を人間の確認なしにマージしない。
- **AI による PR の承認（approve）・マージは禁止**。承認とマージは必ず人間が行う。
- AI は PR の作成・説明・修正提案までを担い、最終的な可否判断は人間が持つ。

> **補足**: `main` を守る設定（直接 push の禁止・PR 必須・自動チェックの通過を必須にする）は、リポジトリオーナーが GitHub 側で行います。手順は次の4章にあります。

---

## 4. 自動チェック（CI）

プルリクエストを作る／更新するたびに、GitHub Actions が自動でチェックを走らせます。
定義は `.github/workflows/ci.yml` にあります。

| チェック | 中身 | 落ちたら |
|----------|------|----------|
| バックエンドの書き方チェック | Ruff で、未定義の名前・消し忘れた読み込み・構文の誤りを検出 | **取り込めない**（直す必要がある） |
| （同上・見た目のズレ） | 字下げや引用符の統一のズレを一覧表示 | 落とさない（いまは参考情報のみ） |
| フロントエンドの書き方チェック | oxlint と、型の食い違いの検出 | **取り込めない**（直す必要がある） |

テストの自動実行とカバレッジ計測は、テストコードを書く段階で追加します（現時点ではテストが1件も無いため入れていません）。

### 手元で同じチェックを走らせる

```bash
# バックエンド（初回のみ導入）
backend/venv/Scripts/python.exe -m pip install -r requirements-dev.txt
backend/venv/Scripts/python.exe -m ruff check backend      # 誤りの検出
backend/venv/Scripts/python.exe -m ruff format backend     # 見た目を自動で整える

# フロントエンド
cd frontend
npm run lint
npx tsc -b
```

判定の基準は `backend/pyproject.toml` と `frontend/.oxlintrc.json` に書いてあります。
厳しさは意図的に段階を分けてあり、いまは「明らかな誤り」だけを必須にしています。
既存コードを一括で整え終えたら、見た目のズレも必須に引き上げます。

### `main` を守る設定（リポジトリオーナー向け・一度だけ）

**この設定は、上のチェックが一度でも実行されたあとに行ってください。**
一度も走っていないチェックは選択肢に出てきません。

1. リポジトリの **Settings → Branches → Add branch protection rule**
2. **Branch name pattern** に `main` と入力
3. **Require a pull request before merging** にチェック
   - レビュー必須人数は運用に合わせて決める（1人開発なら 0 のままでも、直接 push は禁止できる）
4. **Require status checks to pass before merging** にチェック
   - **Require branches to be up to date before merging** にもチェック（古い状態のまま取り込むのを防ぐ）
   - 検索欄で次の2つを選ぶ
     - `バックエンドの書き方チェック`
     - `フロントエンドの書き方チェック`
5. **Do not allow bypassing the above settings** にチェック（オーナー自身にもルールを適用する場合）
6. **Create** で保存

これで、チェックが赤いプルリクエストは取り込みボタンが押せなくなります。

---

## 5. 迷ったら

- どの type / scope にすべきか迷ったら、近い意味のものを選び、本文で補足すれば十分です。厳密さより「後から履歴を読んで意図が分かること」を優先してください。
