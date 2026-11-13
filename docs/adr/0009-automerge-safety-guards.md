---
status: accepted
---

# 自動マージの安全装置

- 状態: 採用（2026-11-13）。ただし「人がいる時間（9:00–22:00 JST のみ）」は初期案のままで、**未確定（proposed）**。待ち時間30分も同じく未確定
- 日付: 2026-11-13
- 決定者: Haruki。AIは起案者
- 全体の概要: [0004](0004-automerge-small-prs-by-deterministic-script.md)

## 背景と問題

判定（[0005](0005-eligibility-decided-by-deterministic-script.md)）が正しくても、止めたいときに止められること、マージしてはいけない時間があること、壊れた main に重ねないことが必要である。また、判定の抜け道（AIがラベルで通す、PRの中でルールを書き換える、判定のあとにコミットを足す）を塞ぐ必要がある。

## 検討した案

| 案 | 内容 | 採否 | 採らなかった理由（採用なら採った理由） |
|---|---|---|---|
| A | 停止スイッチ `AUTOMERGE_ENABLED`（既定オフ。オフならコメントだけ） | 採用 | すぐ全部止められる。オンにするのは Harukiさんの OK の後 |
| B | 記事取得・バックアップの前後はマージしない | 採用 | 反映で api-go が入れ替わると、取得が途中で切れる |
| C | main の最後のCIが成功でなければ止まる | 採用 | 壊れた main に重ねない |
| D | ラベルで判定結果を持つ | 不採用 | AIが自分でラベルを付けて通せる。ラベルは結果の表示だけに使い、効くのは止める側の `no-automerge` だけにした |
| E | 日次上限で、小さく分けた大きな変更を防ぐ | 不採用 | 上限なしと決めた（[0007](0007-no-daily-cap-size-limits-weekly-review.md)）。サイズとパスを歯止めにし、週次で傾向を見る |
| F | 走っているデプロイがあるあいだ、マージしない | 不採用 | デプロイ待ちがボトルネックになる（[0008](0008-deploy-latest-main-without-cancelling.md)） |

## 決定

| 装置 | 内容 |
|---|---|
| 停止スイッチ | `AUTOMERGE_ENABLED` が `true` のときだけマージする。既定はオフ（コメントだけ）。設定ファイルの `enabled` も見る。`false` で即停止 |
| 取得の時間帯 | 5:30–6:20 / 11:00–11:50 / 16:00–16:50 JST はマージしない。反映の上限20分に余裕を足した |
| バックアップの時間帯 | 3:50–4:20 JST はマージしない |
| 人がいる時間 | 初期案は 9:00–22:00 JST のみ。**未確定** |
| main が赤なら止まる | 最後の main のCIが成功でなければ、止まる。人がマージして main が緑に戻るまで再開しない |
| main の保護の確認 | マージの前に、main の保護（ルールセット）を確認する |
| 判定が main に無いと止まる | 判定スクリプトが main に無いときは、進まない（止まる） |
| 再判定と固定 | マージ直前に毎回ゼロから再判定し、`--match-head-commit` で判定したコミットだけをマージする |
| ルールを触るPR | `.github/**` と `scripts/automerge/**` を触るPRは必ず対象外 |
| 戻し方 | 自動マージしたPRには `automerged` ラベルを付ける。戻すのは `git revert` のPRを作り、人がマージする（戻しのPRは自動マージの対象外） |

### Codex のレビューで見つかり、直した穴

| 穴 | 直し方（資料に残っている範囲） |
|---|---|
| symlink の見分け | patch にファイルのモードが無い。Contents API は、ファイルへの symlink を file と返す。そこで、親ディレクトリの一覧で type を見る |
| patch の無いバイナリ | 検出して対象外にする |
| dry-run の二重計上 | 直した |
| 時間帯の確認に失敗したときに進む（fail open） | 失敗したら止まる側に直した |
| 判定スクリプトが main に無いとき | 止まる |
| `--rules` の受け渡し | 直した |
| main の保護の確認 | 確認を入れた |

### gitleaks のダミー許可

判定のテストに置いたダミーの鍵が、gitleaks に検出された。「ファイル」と「ダミー値」の両方に当たるときだけ許可する（`condition = "AND"`）。今後のダミー値は、連結で書く。

## 本人の発言

- 2026-11-13 16:25 JST ごろ（親エージェント経由）: 自動マージのPRには意思決定ログを入れること。意思決定ログのフォルダは必須（docs/adr を唯一の置き場にする）。※親エージェントから伝えられた要旨で、Harukiさんの原文は記録なし
- 2026-11-13 午後（時刻の記録なし）:「マージ自体はAIがやってもいいんだけどマージするかしないかの判断をスクリプトで制御したいという話。」
- 停止スイッチ・時間帯・main が赤のときの停止について: Harukiさんの発言の記録なし（設計書の提案を、全体の設計の承認「一旦この案で行こう」とあわせて採用した。→ [0004](0004-automerge-small-prs-by-deterministic-script.md)）

## 理由（判断基準）

1. いつでも一瞬で全部止められること。
2. 壊れた main や、取得・バックアップの最中に、変更を重ねないこと。
3. 判定の抜け道（ラベル、ルールの書き換え、判定後のコミット追加）を、仕組みで塞ぐこと。
4. 迷ったら止まる側に倒れること（fail closed）。

## 根拠のデータ・事実

- 記事の定期取得は日本時間 6:00 / 11:30 / 16:30（`server/internal/articlefetch/`）。時間帯はその前後。
- ルールセット `main`（2026-11-13、`gh api repos/H4aruki/MyTechPulse/rules/branches/main`）: squash だけ、必須検査5つ、承認0、bypass なし、strict=false。strict が false なので、最新の main への追従は判定スクリプトが自分で見る。
- CODEOWNERS はあるが、コードオーナーの承認は必須にしていない。だから、ルールを触るPRは判定で対象外にして守る。
- 実装とテスト: `scripts/automerge/guards.mjs`、`scripts/automerge/guards.test.mjs`、`scripts/automerge/automerge_test.sh`。

## 結果と代償

- 良くなること: 暴走を止められ、迷えば止まる。
- 代償: 時間帯のあいだはマージが止まる。main が赤のときは、人が直すまで全部止まる。
- 残るリスク: 見つかっていない穴。人がいる時間と待ち時間30分は未確定。

## 見直し方

- いつ: phase 1 の開始前に、未確定の2つ（人がいる時間、待ち時間）を Harukiさんが決める。以後は毎週の見直しで、止まった理由の内訳を見る。
- 何を見て: 保留になった回数と理由、戻した本数、穴の報告。
- 誰が: AIが提案し、人が決める。

## 関連リンク

- #302 の後に見つかった2件（`--rules` の受け渡し、main の保護の確認）は、[PR #305](https://github.com/H4aruki/MyTechPulse/pull/305)（レビュー中）で入る。

- [0005](0005-eligibility-decided-by-deterministic-script.md)、[0006](0006-merge-with-box-gh-and-automerge-sh.md)、[0008](0008-deploy-latest-main-without-cancelling.md)
- [PR #301 gitleaks とローカルフック](https://github.com/H4aruki/MyTechPulse/pull/301)
- [PR #302](https://github.com/H4aruki/MyTechPulse/pull/302)（マージ済み、2026-11-13 16:39 JST、4b7a46b）、[PR #303](https://github.com/H4aruki/MyTechPulse/pull/303)（マージ済み、2026-11-13 16:51 JST、f4125a2）
- [docs/automerge-runner.md](../automerge-runner.md)
