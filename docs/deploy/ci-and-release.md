# CIと配布物（release）

`main` へ取り込む前の検査（CI）と、本番へ入れ替えるための配布物の作り方・照合のしかたをまとめた資料です。入れ替えそのものの手順は [go-auto-deploy.md](go-auto-deploy.md) を参照してください。

## 1. CIの検査

`.github/workflows/ci.yml` は、プルリクエストの作成・再開・更新と、各枝への push で動きます。GitHub上の表示名は次のとおりです。

| 表示名 | 確認内容 | `main` へ取り込むのに必須か |
| --- | --- | --- |
| フロントエンドの書き方チェック | API型の最新性、lint、画面テスト（カバレッジ付き）、組み立て | 必須 |
| Goバックエンドの検査 | gofmt、vet、競合検出付きの試験、ビルド（DB変更が追加だけであることの試験を含む） | 必須 |
| sqlcとOpenAPIの生成差分 | 生成物がコミット済みのものと一致するか | 必須 |
| Go APIのimage検査 | 公開せずに作った最終イメージの起動、疎通、非root実行 | 必須 |
| 設定まわりの仕掛けの確認 | フック・Skillsの内容一致、外部Actionの固定、自動デプロイの条件、文書のリンクと旧構成の言葉 | 必須ではない |

- 必須の4つは、GitHubのRuleset（Settings → Rules → Rulesets → `main`）で指定しています。表示名を変えるときは、Ruleset側も同じ名前に直してください（一致しないと取り込めなくなります）。
- PRでは、基本的に読み取り権限だけです。カバレッジをコメントする2つのjobだけ、PRへの書き込み権限を持ちます。本番の秘密や、パッケージへの書き込み権限は渡しません。
- `main` に取り込まれたときだけ、次の3つが続きます（`GO_DEPLOY_ENABLED` が `true` のときだけ。[go-auto-deploy.md](go-auto-deploy.md)）。

| 表示名 | 内容 |
| --- | --- |
| 本番への反映が要るか | 変更されたファイルから、本番への反映が要るかを判定する。文書・試験だけなら、反映しない |
| 配布物の作成 | 次の2章の配布物を作る（`release.yml`） |
| Go版の本番への反映 | 配布物を照合し、本番サーバーで入れ替える。成功したら画面を公開する |

## 2. 配布物（release）

`release.yml` は、同じ commit から次を作ります。手動の起動と、自動デプロイからの呼び出しの2通りがあります（`main` への push では、これ単独では動きません）。

### Go APIの箱（GHCR）

`ghcr.io/h4aruki/mytechpulse-api-go` へ送ります。本番で使う値は、tagではなく、作ったときに得られる完全な `sha256` のdigestです。`latest` は使いません。パッケージは公開のままです（オーナー決定。トークン不要）。そのため、**箱の中に秘密を入れない**運用を続けます。

### 3つの成果物

| Artifact名 | 含むもの |
| --- | --- |
| `frontend-<commit_sha>` | 画面の一式（`frontend-<commit_sha>.tar.gz`） |
| `ops-<commit_sha>` | サーバーで使う運用ファイルの一式（`ops-<commit_sha>.tar.gz`） |
| `release-manifest-<commit_sha>-<run_attempt>` | `release-manifest.json` と `release-manifest.json.sha256` |

manifestには、commit、実行回（run ID・attempt）、箱のdigest、各archiveのSHA256が入っています。別の実行回や別のattemptのarchiveを混ぜて使いません。

## 3. 配布物の照合

自動デプロイは、サーバーへ送る前と、サーバーで受け取った後に、それぞれ照合します。手で確かめるときは、次のとおりです。

実行回の画面から3つのartifactを同じ実行回でダウンロードし、1つのディレクトリへ置いて、manifest自身を確認します。

```bash
sha256sum -c release-manifest.json.sha256
```

続けて、manifestに書かれた実行回・commit・SHA256とarchiveを照合します（標準のNode.jsだけで動きます）。

```bash
node scripts/release-manifest.mjs verify \
  --manifest <download-dir>/release-manifest.json \
  --manifest-sha256 <manifestのSHA256> \
  --dir <frontendとopsのarchiveを置いたdirectory> \
  --run-id <manifestのworkflow.run_id> \
  --run-attempt <manifestのworkflow.run_attempt> \
  --commit-sha <manifestのcommit_sha>
```

サーバー側の照合は `ops/verify_release.sh` が行います。成功すると、manifestに由来する完全なdigest（`GO_API_IMAGE`）を出力します。tagへ置き換えず、このdigestだけを使ってください。

## 4. 保持期間と再作成

各artifactの保持期間は90日です。期限切れ・削除で取得できないときは、同じ名前で作り直して代用しません。`main` に新しいcommitを作り、新しい実行回から配布物を作り直します。箱のtagが残っていても、manifestのdigestとarchiveを復元できなければ、その配布物は使えません。

## 5. 必要な秘密（リポジトリのSecrets）

値は、この文書やリポジトリには書きません。オーナーが登録します。

| 名前 | 使い道 |
| --- | --- |
| `LIGHTSAIL_SSH_KEY`、`LIGHTSAIL_HOST`、`LIGHTSAIL_USER` | 自動デプロイが、本番サーバーへ接続して入れ替える |
| `CLOUDFLARE_API_TOKEN`、`CLOUDFLARE_ACCOUNT_ID` | 画面（Cloudflare Pages）の公開 |

リポジトリ変数 `GO_DEPLOY_ENABLED`（`true` のときだけ自動デプロイが動く）は、Secretsではなく Variables にあります。
