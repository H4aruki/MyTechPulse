# CIとrelease成果物

## CI check

`.github/workflows/ci.yml` はpull requestの作成・再開・更新と、各branchへのpushで実行します。GitHub上のjob表示名は次のとおりです。

| Job | 確認内容 |
| --- | --- |
| バックエンドの書き方チェック | RuffとPython版の互換テスト |
| フロントエンドの書き方チェック | API型、lint、画面テスト、build |
| 設定まわりの仕掛けの確認 | harness testとaction pin、旧deploy停止条件 |
| Goバックエンドの検査 | format、vet、race付きtest、build |
| sqlcとOpenAPIの生成差分 | 生成結果がcommit済みファイルと一致するか |
| Go APIのimage検査 | pushせずbuildした最終imageの起動、health、非root実行 |

`CONTRIBUTING.md` に記載された現行の必須status check名は `バックエンドの書き方チェック` と `フロントエンドの書き方チェック` です。GitHub Rulesetを変更するときは、job表示名と完全一致させます。

PRでは基本的に `contents: read` です。カバレッジをPRへコメントする2 jobだけ、job単位で `pull-requests: write` を持ちます。本番secretやpackage書き込み権限はCIに渡しません。

Release workflowは、GHCRの公開範囲・認証の承認が済むまで手動起動だけを受け付けます。承認後にmainへのpushを起動条件へ追加します。jobはmain以外では動かず、`contents: read` と `packages: write` を使います。GHCR認証にはActionsが発行する `GITHUB_TOKEN` を使います。production Environment、SSH、Cloudflareのsecretは使いません。

Rulesetで必須にするstatus checkは、実行後にGitHubが表示するjob名と一致させてください。現行CIの必須check設定を変える場合は、既存Rulesetとの照合を先に行います。

## GHCR image

Release workflowは `ghcr.io/h4aruki/mytechpulse-api-go` にGo API imageを送ります。push時のtagはcommit SHAですが、releaseで使う値はworkflowのbuild outputから得た完全な `sha256` digestです。`latest` tagをrelease入力に使いません。

このworkflowを有効にする前に、ownerがGHCR packageの公開範囲をprivateに確認し、保存量・転送量の費用と、本番サーバーからprivate imageをpullするときの認証方法を判断します。package設定やsecretの登録はこの変更では行っていません。

今後のproduction切替用Environment secret名は、#127で確定する運用workflowに合わせてownerが登録します。現行の旧deployが参照する名前は `CLOUDFLARE_API_TOKEN`、`CLOUDFLARE_ACCOUNT_ID`、`LIGHTSAIL_HOST`、`LIGHTSAIL_USER`、`LIGHTSAIL_SSH_KEY` です。値はこの文書やrepositoryへ記録しません。GHCR pull用の認証情報も#127で方式を決めて登録します。

## 同一runの成果物を取得して照合する

Release workflowは、同じcheckoutから次の3成果物を作ります。

| Artifact名 | 含むもの |
| --- | --- |
| `frontend-<commit_sha>` | `frontend-<commit_sha>.tar.gz` |
| `ops-<commit_sha>` | `ops-<commit_sha>.tar.gz` |
| `release-manifest-<commit_sha>-<run_attempt>` | `release-manifest.json` と `release-manifest.json.sha256` |

GitHub Actionsの実行画面から、manifestに記録されたrun ID・attemptと一致する実行を開き、3 artifactを同じrunからdownloadします。別runや別attemptのarchiveを混ぜないでください。manifest側の `artifact_name` はfrontend/ops artifact名、`sha256` は各tar.gzのバイト列全体のSHA256です。

manifest artifactを展開したdirectoryでmanifest自身を確認します。

```bash
sha256sum -c release-manifest.json.sha256
```

2つのarchiveとmanifestを1つのdirectoryへ置き、manifestに書かれたrun ID、attempt、commit SHAとSHA256を再検証します。Node.jsはCI用ですが、検証は標準APIだけで動きます。

```bash
node scripts/release-manifest.mjs verify \
  --manifest <download-dir>/release-manifest.json \
  --manifest-sha256 <manifestのSHA256> \
  --dir <frontendとopsのarchiveを置いたdirectory> \
  --run-id <manifestのworkflow.run_id> \
  --run-attempt <manifestのworkflow.run_attempt> \
  --commit-sha <manifestのcommit_sha>
```

本番サーバー側の入力検証は `ops/verify_release.sh` が行います。成功して出力された `GO_API_IMAGE` はmanifest由来の完全digestです。tagへ置き換えず、migration、公開、service起動を行う前に3成果物をまとめて照合してください。

## 保持期間と再作成

各Actions artifactの保持期間は90日です。期限切れ、削除、run消失でartifactを取得できない場合、同じ名前で作り直して代用しません。mainに新しいcommitを作り、新しいrunからrelease一式を作り直したうえで、#126の検証をやり直します。image tagが残っていても、manifestのdigestとarchiveを復元できなければそのreleaseは使えません。

## このIssueの範囲とproduction rollback

#125はCIと成果物の生成・検査までです。本番serverへの接続、migration、Cloudflare公開、Caddy切替、API起動は行いません。既存のPython版frontend/backend自動deployも、`LEGACY_DEPLOY_ENABLED` が明示的に `true` でない限り実行されません。このvariableは登録しません。

#127ではGitHub Environment `production` とrequired reviewerの承認後にのみ、本番切替を行います。切替直前のAPI、frontend deployment、ops一式を対応づけた `previous-release.json` と、そのrelease固有directoryを保全します。rollbackはこのrecordにあるAPI image識別子、frontend deployment ID、ops directoryを一組として使い、候補releaseや可変tagから推測して戻しません。DBの破壊的変更やvolume・image・backupの削除をrollbackに含めません。
