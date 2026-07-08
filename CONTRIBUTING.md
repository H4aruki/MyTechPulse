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

> **補足**: GitHub 側の `main` ブランチ保護設定（直 push 禁止・PR 必須・レビュー必須）は、リポジトリオーナーが別途設定します。設定は GitHub の *Settings → Branches → Branch protection rules* から行えます。

---

## 4. 迷ったら

- どの type / scope にすべきか迷ったら、近い意味のものを選び、本文で補足すれば十分です。厳密さより「後から履歴を読んで意図が分かること」を優先してください。
