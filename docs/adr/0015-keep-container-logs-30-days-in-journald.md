---
status: accepted
---

# コンテナログを journald で30日残す

- 状態: 採用（accepted）。本番への適用はまだしていない（本文の「結果と代償」）
- 日付: 2026-11-13
- 決定者: Haruki。AIは起案者

## 背景と問題

- 本番の障害の調査では、後から検証したり、別の環境で再現したりするために、過去のログが要る。いまのリポジトリには、Docker のログの回転設定が無い。Docker の既定は `json-file` で、サイズの上限が無い（`docs/deploy/logging-and-monitoring.md`）。本番の実際のドライバは未確認。
- 何日残すかが決まっていないと、上限なしで増え続けるか、気づかないうちに消えるかのどちらかになる。Issue #187（本番の障害に気づく仕組み）の一部として、保持期間を決める必要がある。
- ディスクは 40GB SSD。2026-10-08 時点の使用率は約 20%、空きは約 32GB（Issue #55）。その後の実測はしていない。

## 検討した案

保持期間の候補:

| 案 | 内容 | 採否 | 採らなかった理由（採用なら採った理由） |
|---|---|---|---|
| 7日 | 1週間だけ残す | ❌ 不採用 | 検証や再現で、1週間より前のログが要るかもしれない。ディスクの節約になる量ではない（下の見積もり） |
| 14日 | 2週間残す。事前調査（Opus）の推奨だった | ❌ 不採用 | 推奨だったが、Harukiさんが再現のために最低30日は要ると判断した。30日との差はディスクでは決め手にならない（14日で約42MiB、30日で約89MiB の見積もり） |
| 30日 | 最低30日残す | ✅ 採用 | 再現・検証に使うという用途から、Harukiさんが選んだ。容量の負担がほぼ無い |

実装方法の案:

| 案 | 内容 | 採否 | 採らなかった理由（採用なら採った理由） |
|---|---|---|---|
| journald | Docker のログドライバを journald にし、ホストの journald で日数とサイズを制限する | ✅ 採用 | 日数（`MaxRetentionSec`）で保持を指定できる。`docker logs` も `journalctl` も使える（Docker の文書）。ホストの設定ファイル2つで足り、追加のソフトが要らない |
| json-file のサイズ上限だけ | `max-size` と `max-file` で回転する | ❌ 不採用 | 日数ではなくサイズで消えるため、「最低30日」を保証できない。エラーが多い日は30日より前に消える |
| logrotate | ホスト側の logrotate でログファイルを回転する | ❌ 不採用 | Docker の json-file のファイルを外から回転するのは、Docker の回転と二重になりやすい。journald なら日数の指定が標準の機能にある |
| Better Stack Logs の無料枠 | 外部のログ保管サービスへ送る | ❌ 不採用 | 無料枠の保持は3日で、30日を満たさない（2026-11-13 に公開の料金ページで確認）。外部への送信と登録も増える |

## 決定

- Docker のコンテナログは、**最低30日**残す。
- Docker のログドライバは **journald**。本番ホストの `/etc/docker/daemon.json` に `"log-driver": "journald"`（見本: `ops/docker-daemon.journald.json`）。
- ホストの journald に、次を設定する（drop-in: `ops/journald-retention.conf`、置き場所: `/etc/systemd/journald.conf.d/`）。
  - `MaxRetentionSec=30day`
  - `MaxFileSec=1day`（journald はファイル単位で消すため、消える単位を1日にする）
  - `SystemMaxUse=1G`（安全弁。通常の30日分は数十MB程度の見積もりなので、普段は日数が先に効く。エラーが回り続けたときだけ、日数より前にサイズで消える）
- ローカル開発用の `docker-compose.yml` には journald を書かない。journald は systemd のある Linux ホスト専用で、開発機（Docker Desktop など）が壊れるため。
- 手順は `docs/deploy/lightsail-provisioning.md` の 6-2、スクリプトは `ops/lightsail-vm-setup.sh`（`LOG_RETENTION=1` のときだけ）。
- 誰が・いつ: 本番への適用はオーナーが行う。PR #308 では適用しない。

## 本人の発言

2026-11-13 21:26 JST、K.Haruki との会話（Rex 経由）。ログの方針に関する部分（原文のまま）:

> で、ログは多分商売起きたとかに使うと思うんだけど、うーん、そういうこと考えるとなんか結構長めに取った方がいいのかなって思うんだよね。やっぱりなんか検証とかをする時にさ、こうグロックボットの環境の方で再現しなきゃいけなかったりもするから、そういう意味で最低30日とかはいるんじゃないかなって思います。

（解説。引用の外。「商売起きた」と「グロックボット」は音声入力の変換と思われるが、引用は原文のままにした。）

## 理由（判断基準）

1. いちばん重い基準: 後からの検証と、別の環境での再現に足りる期間を残すこと。Harukiさんの判断で、最低30日。
2. 期間をどれにしても、ディスクの負担は決め手にならない。危ないのは、上限が無いままエラーが回り続けたとき。日数に加えてサイズの安全弁（`SystemMaxUse=1G`）を置く。
3. 日数で保持を指定でき、`docker logs` が引き続き使える方法を選ぶ。

## 根拠のデータ・事実

2026-11-13 の事前調査（Opus）の結果。

- ディスク: 40GB SSD（`docs/deploy/lightsail-provisioning.md`）。2026-10-08 時点の使用率は約 20%、空きは約 32GB（Issue #55）。その後の実測はしていない。
- いまのリポジトリには Docker ログの回転設定が無い。既定なら json-file でサイズ上限なし。本番の実際のドライバは未確認。
- ログ量の見積もり（実測ではない）: api-go のアクセスログは1行約 312 バイト。IP・クエリ・本文は出していない。10,000 リクエスト/日でも約 3MiB/日、14日で約 42MiB、**30日で約 89MiB**。記事の定期取得と Caddy のログは1日数行〜数十行。
- 法令・外部の目安に、固定の日数は無い。
  - 個人情報保護法（APPI）: 保存期間の下限も上限も無い。不要になったら遅滞なく消すよう努める（第22条、個人情報保護委員会 FAQ）。
  - IPA: 日数は決めていない。「例：1週間、3か月、1年」とし、組織で決めるよう求めている。
  - OWASP: 日数は決めていない。必要な期間より前に捨てず、それを超えて持たない、という考え方。
- 通信履歴ガイドラインの「6か月」等は、ISP や SNS・掲示板向けの目安で、このサービスの Docker ログに最低期間を課すものではない（調査者の整理。法的な確定ではない）。
- Better Stack Logs の無料枠の保持は3日（2026-11-13、公開の料金ページ）。
- Docker の文書（2026-11-13 に確認）: journald ドライバのログは、`journalctl`、journal API、`docker logs` で取り出せる。既定ドライバは `daemon.json` に `"log-driver": "journald"` を書き、Docker を再起動して切り替える。新しいコンテナから効き、既存のコンテナは新しい設定を自動では使わない。

## 結果と代償

- 良くなること: 最低30日前までのログで、検証と再現ができる。保持が日数で決まり、サイズの安全弁もある。
- 代償・リスク:
  - 適用には、ホストの journald の再読み込み、Docker の再起動（全コンテナが止まる時間が出る）、対象コンテナの作り直しが要る。作り直すまでは古い設定のまま。**この PR では適用していない。**
  - 個人情報を含みうるログを30日持つ。アクセスログは IP・クエリ・本文を出さない設計のため、持つ内容は限られる。
  - エラーが回り続けると、`SystemMaxUse=1G` が先に効き、30日より前に消える。
  - 未確認: journald がログをディスクに残す設定か（`/var/log/journal` の有無）、`docker compose logs` が journald で同じように使えるか、本番の現在のドライバ。適用後に確かめる。

## 見直し方

- 誰が: オーナー（Haruki）。
- 何を見て: `ops/check_resources.sh` を cron で回して残すディスク使用の記録（`docs/deploy/logging-and-monitoring.md` の6章）と、適用後の `journalctl --disk-usage`。
- 見直す条件:
  - ログ量やディスク使用率が増えてきたとき（例: `SystemMaxUse=1G` に近づく、ディスクの警告が出る）。
  - 30日では調査が足りなかったと分かったとき。
  - 逆に、短くしてよいと分かったとき。
- 本番に適用した後に、1回見直す（適用の実績と、実際のログ量を見るため）。

## 関連リンク

- Issue: <https://github.com/H4aruki/MyTechPulse/issues/187>
- PR: <https://github.com/H4aruki/MyTechPulse/pull/308>
- 資料: [logging-and-monitoring.md](../deploy/logging-and-monitoring.md)、[lightsail-provisioning.md](../deploy/lightsail-provisioning.md)
- 設定の見本: `ops/journald-retention.conf`、`ops/docker-daemon.journald.json`
- Docker: <https://docs.docker.com/engine/logging/drivers/journald/>、<https://docs.docker.com/engine/logging/configure/>
- 個人情報保護委員会: <https://www.ppc.go.jp/>（APPI 第22条と FAQ。個別ページの URL は未確認）
- IPA: <https://www.ipa.go.jp/>（個別ページの URL は未確認）
- OWASP Logging Cheat Sheet: <https://cheatsheetseries.owasp.org/cheatsheets/Logging_Cheat_Sheet.html>
- Better Stack の料金: <https://betterstack.com/pricing>
