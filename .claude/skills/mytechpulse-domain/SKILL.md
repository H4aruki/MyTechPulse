---
name: mytechpulse-domain
description: Applies MyTechPulse personalization and article-ranking invariants. Use when changing recommendation weights, click learning, article collection, filtering, scoring, or source fallback behavior.
---

# ドメイン仕様を確認する

変更前に `AGENTS.md` の「パーソナライズの重要仕様」と対象serviceを読む。

- 興味度は0〜1の小数として扱い、DBの `recommend.match_int` には10000倍した整数で保存する。
- クリック学習は全タグを `ALPHA = 0.8` で減衰し、記事タグへ `1 - ALPHA` を加える。
- 記事取得は興味度上位5タグ、直近5日、タグの小文字比較を守る。
- スコアは「記事タグに対応する重みの合計 × (likes + 1)」とする。
- Zennが0件のときの日付フィルターだけのフォールバックを壊さない。
- routes、services、crud、modelsの責務を混ぜない。

変更時は境界値、タグ無し、重複タグ、0件、外部API失敗を確認する。
