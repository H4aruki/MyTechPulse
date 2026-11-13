# 自動マージのスクリプト（automerge.sh）

判定OKのPRだけを、box からマージするスクリプトです。マージの判定は別のスクリプト（`eligibility.mjs`）が行い、この資料はそれを動かす側の説明です。

## 置き方（box）

- MyTechPulse の専用の clone を1つ用意します。既定は `/home/box/shared/automerge/MyTechPulse` で、`AUTOMERGE_HOME` で変えられます。
- 更新は、その clone で `git pull --ff-only` します。
- 判定スクリプトとルール（`.github/automerge-rules.json`）は、PRのブランチではなく `origin/main` の版を毎回取り出して使います。PRに書かれたコードは動かしません。

## 起動のしかた

PRを出したら、すぐにバックグラウンドで起動して、次の作業に戻ります。

```bash
nohup scripts/automerge/automerge.sh <PR番号> >/dev/null 2>&1 &
```

- CIの終了待ちはこのスクリプトが行います（AIは待ちません）。
- 同じPRで古いスクリプトが動いていたら、止めてからやり直します（`/tmp/automerge/pr-<番号>.pid`。場所は `AUTOMERGE_STATE_DIR`）。
- マージは、リポジトリ全体で同時に1つだけです（`merge.lock`）。

## 停止スイッチ

- 既定は**オフ**です。環境変数 `AUTOMERGE_ENABLED=true` のときだけマージします。
- オフのときは、判定の結果をコメントするだけです（「マージ: 停止中」と書きます）。
- オンにするのは、Harukiさんの OK が出たあとです。

## マージしない時間帯（日本時間）

5:30〜6:20、11:00〜11:50、16:00〜16:50（記事の取得）と、3:50〜4:20（バックアップ）です。時間帯の中なら「保留」とコメントし、窓の終わりまで待って、判定からやり直します（やり直しは1回だけ）。窓は `automerge-rules.json` の `mergeGuards.blackoutWindowsJst` で変えられます。

## main が赤のとき

main の最後に終わった CI が成功でなければ、「保留（mainが赤）」とコメントして終わります。壊れた main に重ねないためです。走っているデプロイは打ち切られないので、既定では待ちません（`mergeGuards.blockWhileMainDeployRunning` を `true` にすると待ちます）。

## 結果の見方

- PRのコメントを1つだけ、毎回上書きします（先頭に `<!-- automerge-eligibility -->`）。
- マージしたら `automerged` ラベルを付けます（ラベルが無ければ付きません）。
- ログは `/home/box/shared/bot-logs/automerge/automerge.log` に1行ずつ残ります（`AUTOMERGE_LOG_DIR`）。時刻、PR、SHA、判定、マージしたか、理由です。トークンは書きません。

## 試験

`bash scripts/automerge/automerge_test.sh` と `node --test scripts/automerge/guards.test.mjs`（CIでも動きます）。
