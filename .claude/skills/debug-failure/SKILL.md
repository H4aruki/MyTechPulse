---
name: debug-failure
description: Diagnoses and fixes reproducible MyTechPulse failures using evidence-first debugging. Use for errors, failing checks, broken builds, or unexpected behavior.
---

# 障害を調査する

1. 症状、期待結果、再現条件、直前の変更を整理する。
2. 最小の再現コマンドを実行し、エラー全文と終了コードを確認する。
3. ログ、設定例、関連コードから仮説を立て、事実と推測を分ける。`.env` は読まない。
4. 仮説ごとに小さな確認を行い、原因を絞る。むやみに依存更新や広範囲な書き換えをしない。
5. 修正依頼を含む場合は、原因・修正方法・ゴールを報告してから最小修正を行う。
6. 同じ再現手順と `verify-change` で再確認し、未解決なら次に必要な情報を示す。
