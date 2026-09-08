---
name: release-readiness
description: Assesses whether a MyTechPulse branch is ready for human review and automated deployment. Use before PR handoff, release decisions, or changes touching CI and deployment.
---

# 公開準備を確認する

1. `git status`、差分、コミット一覧を確認し、1機能1コミットと無関係な変更の混入を確認する。
2. `verify-change` でバックエンド、フロントエンド、ハーネスの該当検証を実行する。
3. 秘密情報、デバッグ出力、一時ファイル、生成物、ローカル設定が含まれないことを確認する。
4. API・DB・認証・公開設定の変更は、互換性、戻し方、必要な手動作業を整理する。
5. 公開は `main` 反映後のCIだけに任せる。手動デプロイ、本番SSH、`main` への直接pushは行わない。
6. 人間へ、変更内容、検証結果、既知のリスク、公開後に見る項目を渡す。PRの承認・マージはしない。
