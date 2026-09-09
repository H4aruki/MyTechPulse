# MyTechPulse

> Qiita・Zennから、興味に合う技術記事を集めるパーソナライズニュースアプリ

[公開版を試す](https://mytechpulse.net) ・ [機能一覧](./docs/BasicDesignSpecifications/FeaturesList.md) ・ [コントリビューションガイド](./CONTRIBUTING.md)

MyTechPulseは、複数の技術情報サイトを巡回する手間を減らし、短い時間で必要な記事を見つけるためのWebアプリです。登録した興味タグと記事の閲覧傾向をもとに、Qiita・Zennの新しい記事を優先度順に表示します。

## 主な機能

- ユーザー登録・ログイン（JWT認証）
- カテゴリ別の興味タグ選択と一括選択
- 興味の強い上位5タグを使ったQiita・Zennの記事取得
- 提供元ごとに最大10件の記事を表示
- 興味度をもとにした並べ替え（Qiitaの記事は反響の大きさも加味）
- 記事クリックを次回以降のおすすめへ反映
- 読み込み中・取得失敗・記事がない場合の状態表示

詳しい実装状況は[機能一覧](./docs/BasicDesignSpecifications/FeaturesList.md)を参照してください。

## Gallery

### サービス紹介ページ

![MyTechPulseのサービス紹介ページ](./img/gallery-landing.png)

### 興味タグの選択

![MyTechPulseの興味タグ選択画面](./img/gallery-tag-selection.png)

## 仕組み

1. ユーザー登録時に興味のある技術タグを選びます。
2. 興味度の高いタグを使い、Qiita・Zennから記事を同時に取得します。
3. 記事を読むと、その記事のタグが興味の傾向へ反映されます。

現在は画面を開くたびに外部サイトから記事を取得します。Qiitaは直近5日、Zennは直近2週間の記事を基本の対象とし、該当するZenn記事がない場合は期間条件を外して補完します。

## 技術スタック

| 区分 | 主な技術 |
| --- | --- |
| フロントエンド | React 19、TypeScript、Vite、Tailwind CSS 4、TanStack Query、Zod |
| バックエンド | Python 3.12、FastAPI、SQLAlchemy |
| データベース | PostgreSQL 17 |
| 認証 | JWT、bcrypt |
| 開発・運用 | Docker Compose、GitHub Actions、Caddy |
| 本番環境 | Cloudflare Pages（フロントエンド）、AWS Lightsail（API・DB） |

構成の詳細は[システム構成図](./docs/BasicDesignSpecifications/SystemArchitectureDiagram.md)を参照してください。

## ローカルで動かす

### 必要なもの

- Git
- Docker Desktop（Docker Composeを含む）
- Node.js 22
- Qiitaのアクセストークン（[Qiitaの設定画面](https://qiita.com/settings/applications)で発行）

### 1. リポジトリを準備する

```bash
git clone https://github.com/H4aruki/MyTechPulse.git
cd MyTechPulse
```

ZIPは[mainブランチの最新版](https://github.com/H4aruki/MyTechPulse/archive/refs/heads/main.zip)からも取得できます。

### 2. 環境変数を準備する

```bash
cp backend/.env.example backend/.env
cp frontend/.env.example frontend/.env
```

Windows PowerShellでは、`cp`の代わりに次を使えます。

```powershell
Copy-Item backend/.env.example backend/.env
Copy-Item frontend/.env.example frontend/.env
```

`backend/.env`で次の2項目を設定してください。

- `QIITA_ACCESS_TOKEN`: Qiitaから記事を取得するためのトークン
- `SECRET_KEY`: JWTへの署名に使うランダムな文字列

`SECRET_KEY`は次のコマンドで生成できます。

```bash
python -c "import secrets; print(secrets.token_hex(32))"
```

Docker Composeで起動する場合、`DATABASE_URL`はDBコンテナ向けに自動で上書きされるため、開発用の初期値から変更する必要はありません。

### 3. APIとデータベースを起動する

```bash
docker compose up --build
```

初回起動時にデータベースとテーブルが作成されます。APIは `http://127.0.0.1:8000`、APIドキュメントは `http://127.0.0.1:8000/docs` で確認できます。

### 4. フロントエンドを起動する

別のターミナルで実行します。

```bash
cd frontend
npm ci
npm run dev
```

`http://localhost:5173` をブラウザで開いてください。

## バックエンドを直接動かして開発する

自動再読み込みを使う場合は、DBだけをDockerで起動します。Python 3.12を推奨します。

```bash
python -m venv backend/venv
backend/venv/Scripts/python.exe -m pip install -r requirements.txt
docker compose up -d db
cd backend
venv/Scripts/python.exe init_db.py
venv/Scripts/python.exe -m uvicorn app.main:app --reload
```

macOS・Linuxでは、仮想環境内の実行ファイルを `backend/venv/bin/python` に読み替えてください。フロントエンドは前節と同じ手順で起動します。

## Roadmap

- 登録後の興味タグ変更
- 記事の日次一括取得とDB保存
- ブックマーク・既読管理
- キーワード検索
- ダークモード
- AI要約・自動カテゴリ分類
- 利用規約・プライバシーポリシー
- ログイン試行回数の制限とパスワード強度チェック
- バックアップの外部保管

優先順位と進捗は[TASKS.md](./TASKS.md)で管理しています。

## Contributors

<table>
  <tr>
    <td align="center">
      <a href="https://github.com/H4aruki">
        <img src="https://github.com/H4aruki.png" width="100" alt="H4aruki"><br>
        <sub><b>H4aruki</b></sub>
      </a>
    </td>
    <td align="center">
      <a href="https://github.com/KaichoHarry">
        <img src="https://github.com/KaichoHarry.png" width="100" alt="はりぃ会長"><br>
        <sub><b>はりぃ会長</b></sub>
      </a>
    </td>
  </tr>
</table>
