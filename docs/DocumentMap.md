# DocumentMap

このファイルは、ドキュメントのファイルの地図としての役割を持つ。

## このファイルに書かれていること
- ドキュメントファイルがどこにあるかのマップ
- 各ドキュメントファイルに何が書かれているかの概要

## ファイルの配置

```
docs/
├── DocumentMap.md                        … このファイル
├── RequirementsSpecification.md          … 要件定義書
├── BasicDesignSpecifications/            … 基本設計書
│   ├── FeaturesList.md                   … 機能一覧
│   ├── SystemArchitectureDiagram.md      … システム構成図
│   ├── DataBaseArchitecture.md           … データベース構成
│   ├── API/                              … APIの設計書
│   │   ├── ApiList.md                    … API一覧（APIの資料の入口）
│   │   ├── ApiCommonRules.md             … APIに共通する決まりごと
│   │   ├── ApiExternal.md                … 外部サービスへの問い合わせ
│   │   └── Details/                      … 窓口ごとの詳細
│   │       ├── Auth.md                   … 会員登録・ログイン
│   │       ├── News.md                   … おすすめ記事の取得
│   │       └── Article.md                … 記事クリックの記録
│   └── Screen/                           … 画面の設計書
│       ├── ScreenList.md                 … 画面一覧（画面の資料の入口）
│       ├── ScreenCommonRules.md          … 画面に共通する決まりごと
│       └── Details/                      … 画面ごとの詳細
│           ├── LandingPage.md            … G-01 サービス紹介ページ
│           ├── SignUp.md                 … G-02 会員登録画面
│           ├── Login.md                  … G-03 ログイン画面
│           └── ArticleList.md            … G-04 記事一覧画面
└── deploy/                               … 公開・運用の手順書
    ├── lightsail-provisioning.md         … サーバー準備手順（現行）

```

## ドキュメント一覧

### RequirementsSpecification.md

MyTechPulseの要件定義書。背景・課題・目標、利用対象者（エンドユーザー／システム管理者）を記載。ユースケース図、実装済み機能（登録・ログイン、興味タグ選択、Qiita・Zennからの記事取得、クリックによる興味学習）と未実装機能（ブックマーク、アンケート）の一覧、画面一覧・画面遷移図を掲載。非機能要件として性能・可用性・セキュリティ（HTTPS、パスワードハッシュ化、JWT認証）・対応環境・バックアップ方針をまとめている。

### BasicDesignSpecifications/FeaturesList.md

機能一覧。要件定義書の機能要件を、画面・処理の単位まで細かく分けて整理したもの。機能を6分類（サービス紹介／アカウント／興味タグ／記事の配信／興味の学習／運用・保守）に分け、機能ID・機能名・対応する画面・実装状態・概要の表で示す。画面IDは要件定義書の画面一覧（G-01〜G-04）に対応する。末尾に未実装機能の一覧を掲載しており、その優先順位と進捗はリポジトリ直下の`TASKS.md`で管理している。

### BasicDesignSpecifications/SystemArchitectureDiagram.md

システム構成図。フロントエンドとバックエンドを分けた論理構成の図（Mermaid）、採用技術の一覧、Qiita・Zennとの外部連携の内容、本番環境の配置（フロントエンドはCloudflare Pages、バックエンドとデータベースはAWS Lightsail）をまとめている。

### BasicDesignSpecifications/DataBaseArchitecture.md

データベース構成。保存に使っているデータベースの種類と、テーブル（データの入れ物）の一覧、それぞれのつながりを表したER図を掲載している。利用者・技術タグ・興味の強さの3つについて、項目ごとの意味と制約を表でまとめ、興味の強さを1万倍した整数で保存している理由と、その値が変わるタイミング（会員登録時・記事クリック時）も説明している。記事そのものは保存していない点にも触れている。

### BasicDesignSpecifications/API/ApiList.md

API一覧。画面とサーバーがやり取りする窓口を一覧にしたもので、APIの資料の入口にあたる。窓口ごとにID・方式・パス・本人確認の要否・対応する機能ID・詳細ファイルへのリンクを表で示す。共通の決まりごとと窓口ごとの詳細は別ファイルに分けてあり、このファイルからたどる。

### BasicDesignSpecifications/API/ApiCommonRules.md

APIに共通する決まりごと。データの形式や送り方、結果を本文の数値（status）で伝えるという独自の取り決め、期限付きの合言葉による本人確認のしくみをまとめている。登録から記事表示・クリックまでの呼び出しの流れ図と、動作確認用の窓口の説明もここに含む。

### BasicDesignSpecifications/API/ApiExternal.md

外部サービスへの問い合わせ。記事を集めるためにQiita・Zennへ投げている呼び出し先と条件、1回の記事表示あたりの問い合わせ回数、相手先ごとの癖への対処（小文字での検索、タグ情報の補い方など）をまとめている。

### BasicDesignSpecifications/API/Details/

窓口ごとの詳細。送る項目・返る項目・結果の数値ごとの意味を表で示す。`Auth.md`が会員登録とログイン、`News.md`がおすすめ記事の取得（並べ替えの点数の付け方を含む）、`Article.md`が記事クリックの記録（興味の強さの更新のしかたを含む）に対応する。

### BasicDesignSpecifications/Screen/ScreenList.md

画面一覧。画面の資料の入口にあたる。画面ごとにID・名前・ログインの要否・実装場所・対応する機能・詳細ファイルへのリンクを表で示し、画面遷移の概要をMermaid図で掲載している。

### BasicDesignSpecifications/Screen/ScreenCommonRules.md

画面に共通する決まりごと。スマホ・タブレットを優先する方針、レイアウトを切り替える幅の境目（ブレークポイント）、共通のヘッダー・フッター構成、入力チェックの共通ルール、エラー表示の見た目（出す場所・形。文言そのものはAPI側の資料が対象）をまとめている。

### BasicDesignSpecifications/Screen/Details/

画面ごとの詳細。構成・表示項目・入力チェック・状態ごとの見え方（読み込み中／0件／失敗時）・レスポンシブの扱いを画面ごとに示す。`LandingPage.md`がサービス紹介ページ（G-01）、`SignUp.md`が会員登録画面（G-02）、`Login.md`がログイン画面（G-03）、`ArticleList.md`が記事一覧画面（G-04）に対応する。

### deploy/lightsail-provisioning.md

現行のサーバー（AWS Lightsail）を用意する手順書。インスタンスの作成から初期設定までを記載している。
