# API一覧

MyTechPulseの画面（フロントエンド）とサーバー（バックエンド）がやり取りするための窓口を一覧にしたもの。正本は、Goサーバーが自動で書き出すAPIの仕様ファイル（`server/openapi/openapi.json`）で、この文書はその内容を人が読める形に書き起こしている。

- API IDは `A-<分類番号>-<連番>` の形式で付ける。分類番号は機能一覧（[FeaturesList.md](../FeaturesList.md)）の分類と対応させる
- 「状態」は **実装済み** / **未実装** の2種類
- 仕様ファイルとの食い違いは、サーバー側の検査（生成物の差分チェック）で見つかる。窓口を変えたら、仕様ファイル・画面側の型（`frontend/src/api/generated.ts`）・この文書をそろえて直す
- 開発中は、サーバーを動かすと自動生成の説明ページ（`/docs`）でも確認できる。本番では閉じている

## 1. 資料の構成

APIの資料は役割ごとに次のように分けている。このファイルは入口にあたる。

| ファイル | 書かれていること |
| --- | --- |
| ApiList.md（このファイル） | 窓口の一覧と、各資料への案内 |
| [ApiCommonRules.md](ApiCommonRules.md) | すべての窓口に共通する決まりごと（送り方・結果の表し方・本人確認・全体の流れ） |
| [Details/Auth.md](Details/Auth.md) | 会員登録・ログイン・ログアウト・ログイン中の利用者の確認の詳細 |
| [Details/News.md](Details/News.md) | おすすめ記事の取得の詳細 |
| [Details/Article.md](Details/Article.md) | 記事クリックの記録の詳細 |
| [Details/UserFeedback.md](Details/UserFeedback.md) | 利用者アンケートの詳細 |
| [ApiExternal.md](ApiExternal.md) | サーバーがQiita・Zennへ問い合わせている内容 |

## 2. API一覧

| API ID | 名前 | 方式 | パス | 本人確認 | 対応する機能 | 状態 | 詳細 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| A-0-1 | 生存確認 | GET | `/health/live` | 不要 | F-6-5 | 実装済み | [共通の決まりごと](ApiCommonRules.md#6-動作確認a-0-1a-0-2) |
| A-0-2 | データベース接続の確認 | GET | `/health/ready` | 不要 | F-6-5 | 実装済み | [共通の決まりごと](ApiCommonRules.md#6-動作確認a-0-1a-0-2) |
| A-2-1 | 会員登録 | POST | `/api/v1/auth/signup` | 不要 | F-2-1, F-2-2, F-3-4 | 実装済み | [Auth.md](Details/Auth.md#1-a-2-1-会員登録) |
| A-2-2 | ログイン | POST | `/api/v1/auth/login` | 不要 | F-2-4, F-2-5, F-2-6 | 実装済み | [Auth.md](Details/Auth.md#2-a-2-2-ログイン) |
| A-2-3 | ログイン中の利用者の確認 | GET | `/api/v1/auth/me` | 必要 | F-2-6, F-2-7 | 実装済み | [Auth.md](Details/Auth.md#3-a-2-3-ログイン中の利用者の確認) |
| A-2-4 | ログアウト | POST | `/api/v1/auth/logout` | 不要（あれば失効する） | F-2-8 | 実装済み | [Auth.md](Details/Auth.md#4-a-2-4-ログアウト) |
| A-4-1 | おすすめ記事の取得 | GET | `/api/v1/feed` | 必要 | F-4-1〜F-4-4, F-4-7 | 実装済み | [News.md](Details/News.md) |
| A-5-1 | 記事クリックの記録 | POST | `/api/v1/feedback/article-clicks` | 必要 | F-5-1〜F-5-3 | 実装済み | [Article.md](Details/Article.md) |
| A-7-1 | アンケートの表示可否の確認 | GET | `/api/v1/user-feedback/status` | 必要 | F-7-2 | 実装済み | [UserFeedback.md](Details/UserFeedback.md#1-a-7-1-アンケートの表示可否の確認) |
| A-7-2 | アンケートの表示要求 | POST | `/api/v1/user-feedback/presentations` | 必要 | F-7-2 | 実装済み | [UserFeedback.md](Details/UserFeedback.md#2-a-7-2-アンケートの表示要求) |
| A-7-3 | 総合評価の保存 | POST | `/api/v1/user-feedback/submissions` | 必要 | F-7-2 | 実装済み | [UserFeedback.md](Details/UserFeedback.md#3-a-7-3-総合評価の保存) |
| A-7-4 | 追加質問の回答の保存 | PUT | `/api/v1/user-feedback/submissions/{submission_id}` | 必要 | F-7-2 | 実装済み | [UserFeedback.md](Details/UserFeedback.md#4-a-7-4-追加質問の回答の保存) |
| A-7-5 | 回答せず閉じたことの保存 | POST | `/api/v1/user-feedback/dismissals` | 必要 | F-7-2 | 実装済み | [UserFeedback.md](Details/UserFeedback.md#5-a-7-5-回答せず閉じたことの保存) |

現在の窓口は以上の13個。

## 3. 補足

- 画面側の呼び出し口は`frontend/src/api/`にまとまっている。送受信する項目の型は、仕様ファイルから自動で作った`generated.ts`を元にしているため、手で書き写して食い違う心配がない
- 今後の追加候補（ブックマーク、登録後の興味タグの変更など）はまだ窓口を持っていない。優先順位と進捗はリポジトリ直下の`TASKS.md`で管理している
- 窓口を増やすときは、このファイルの一覧に1行足したうえで、該当する詳細ファイルに項目を追加する
- 窓口の前に付く`/api/v1`は、将来仕様を大きく変えるときに旧版と並べて動かせるようにするための版番号
