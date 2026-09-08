---
name: review-change
description: Reviews MyTechPulse diffs for defects, security risks, contract drift, and missing verification. Use for code review, self-review, or pre-PR inspection.
---

# 変更をレビューする

1. 依頼がレビューだけなら、ファイルを変更せず差分と周辺コードを読む。
2. 正しさ、認証・権限、秘密情報、API契約、DB整合性、画面の退行、検証不足の順に確認する。
3. 指摘は重要度順にし、`path/to/file:line`、起きる条件、影響、修正案を書く。
4. 推測は推測と明記し、根拠のない指摘を増やさない。
5. 問題が無い場合もその旨と、未検証の範囲や残るリスクを伝える。
6. 修正も依頼された場合だけ、変更前報告を行ってから最小限の修正へ進む。
