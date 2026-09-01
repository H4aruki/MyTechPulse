# API一覧

MyTechPulseの画面（フロントエンド）とサーバー（バックエンド）がやり取りするための窓口を一覧にしたもの。実際の定義は`backend/app/routes/`と`backend/app/schemas/`にあり、この文書はその内容を人が読める形に書き起こしている。

- API IDは `A-<分類番号>-<連番>` の形式で付ける。分類番号は機能一覧（[FeaturesList.md](../FeaturesList.md)）の分類と対応させる
- 「状態」は **実装済み** / **未実装** の2種類
- サーバーを動かすとブラウザから確認できる自動生成の説明ページ（`/docs`）もあり、実際に送れる項目はそちらでも確認できる

## 1. 資料の構成

APIの資料は役割ごとに次のように分けている。このファイルは入口にあたる。

| ファイル | 書かれていること |
| --- | --- |
| ApiList.md（このファイル） | 窓口の一覧と、各資料への案内 |
| [ApiCommonRules.md](ApiCommonRules.md) | すべての窓口に共通する決まりごと（送り方・結果の表し方・本人確認・全体の流れ） |
| [Details/Auth.md](Details/Auth.md) | 会員登録・ログインの詳細 |
| [Details/News.md](Details/News.md) | おすすめ記事の取得の詳細 |
| [Details/Article.md](Details/Article.md) | 記事クリックの記録の詳細 |
| [ApiExternal.md](ApiExternal.md) | サーバーがQiita・Zennへ問い合わせている内容 |

## 2. API一覧

| API ID | 名前 | 方式 | パス | 本人確認 | 対応する機能 | 状態 | 詳細 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| A-0-1 | 動作確認 | GET | `/` | 不要 | ― | 実装済み | [共通の決まりごと](ApiCommonRules.md#5-動作確認a-0-1) |
| A-2-1 | 会員登録 | POST | `/auth/create_user` | 不要 | F-2-1, F-2-2, F-3-4 | 実装済み | [Auth.md](Details/Auth.md#1-a-2-1-会員登録) |
| A-2-2 | ログイン | POST | `/auth/login_check` | 不要 | F-2-4, F-2-5 | 実装済み | [Auth.md](Details/Auth.md#2-a-2-2-ログイン) |
| A-4-1 | おすすめ記事の取得 | POST | `/news/personal_news` | 必要 | F-4-1〜F-4-4, F-4-7 | 実装済み | [News.md](Details/News.md) |
| A-5-1 | 記事クリックの記録 | POST | `/article/click` | 必要 | F-5-1〜F-5-3 | 実装済み | [Article.md](Details/Article.md) |

現在の窓口は以上の5つのみ。ログアウトは画面側だけで完結するため、専用の窓口は用意していない。

## 3. 補足

- 画面側の呼び出し口は`frontend/src/api/`にまとまっており、送受信する項目の型は`frontend/src/api/types.ts`がサーバー側の定義と一対一で対応している。片方だけ変えると食い違うため、変更時は必ず両方を直す
- 今後の追加候補（ブックマーク、登録後の興味タグの変更など）はまだ窓口を持っていない。優先順位と進捗はリポジトリ直下の`TASKS.md`で管理している
- 窓口を増やすときは、このファイルの一覧に1行足したうえで、該当する詳細ファイルに項目を追加する
