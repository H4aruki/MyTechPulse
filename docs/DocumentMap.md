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
│   └── SystemArchitectureDiagram.md      … システム構成図
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

### deploy/lightsail-provisioning.md

現行のサーバー（AWS Lightsail）を用意する手順書。インスタンスの作成から初期設定までを記載している。
