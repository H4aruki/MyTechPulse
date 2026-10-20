---
name: mytechpulse-domain
description: Applies MyTechPulse personalization and article-ranking invariants. Use when changing recommendation weights, click learning, article collection, filtering, scoring, or source fallback behavior.
---

# ドメイン仕様を確認する

変更前に `AGENTS.md` の「パーソナライズの重要仕様」と、`CONTEXT.md`（用語）、対象のGoのパッケージ（`server/internal/interest/`、`recommendation/`、`provider/`）を読む。

- 興味度は0〜1の小数として扱い、DBの `recommend.match_int` には10000倍した整数（固定小数点）で保存する（`interest.Scale`）。会員登録時は、選んだタグを10000（1.0）、選ばなかったタグを8000（0.8）にする（`InitialSelected` / `InitialUnselected`。[Issue #201](https://github.com/H4aruki/MyTechPulse/issues/201)）。0.8を選んだ理由は、オーナーの決定以上の記録がない。
- クリック学習は、全タグを0.8倍（8/10）に減衰し、クリックした記事のタグへ0.2（`ClickBoost` = 2000）を加える。上限は1。
- 記事取得は興味度上位5タグを使う。Qiitaは直近5日、Zennは直近14日（新しいものが0件なら古いものを使うフォールバック）で絞る。
- タグの比較は、前後の空白を除いて小文字にそろえて行う（`interest.NormalizeTag`）。
- スコアは提供元で分ける。Qiitaは「記事タグに対応する重みの合計 × (likes + 1)」、Zennは重みの合計のみ。提供元ごとに上位10件を返す（[PR #145](https://github.com/H4aruki/MyTechPulse/pull/145)）。Zennに likes を掛けない理由は Issue / PR のコメントには無く、移行設計には「提供元側の並び順を利用するため」とある。
- 外部提供元の片方だけが失敗したときは、成功した側を返して `warnings` で知らせる。両方失敗したときだけエラーにする。
- パッケージの責務（認証・推薦・提供元・DB）を混ぜない。

変更時は境界値、タグ無し、重複タグ、0件、外部API失敗を確認する。
