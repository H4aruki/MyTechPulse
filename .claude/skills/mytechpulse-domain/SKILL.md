---
name: mytechpulse-domain
description: Applies MyTechPulse personalization and article-ranking invariants. Use when changing recommendation weights, click learning, article collection, filtering, scoring, or source fallback behavior.
---

# ドメイン仕様を確認する

変更前に `AGENTS.md` の「パーソナライズの重要仕様」と、`CONTEXT.md`（用語）、対象のGoのパッケージ（`server/internal/interest/`、`recommendation/`、`provider/`）を読む。

- 興味度は0〜1の小数として扱い、DBの `recommend.match_int` には10000倍した整数（固定小数点）で保存する（`interest.Scale`）。会員登録時は、選んだタグだけに10000（1.0）の行を作る（`InitialSelected`。[Issue #201](https://github.com/H4aruki/MyTechPulse/issues/201)）。選ばなかったタグには行を作らず、興味度0と同じ扱いにする（[Issue #240](https://github.com/H4aruki/MyTechPulse/issues/240)）。
- クリック学習は、全タグを0.8倍（8/10）に減衰し、クリックした記事のタグへ0.2（`ClickBoost` = 2000）を加える。上限は1。
- 記事取得は興味度上位5タグを使う（興味度0のタグは入れない。`interest.Top`）。Qiitaは直近5日、Zennは直近14日（タグで見つけた新しいものが0件なら古いものを使うフォールバック）で絞る。
- 興味度が0より大きいタグが5つに満たないときは、提供元ごとに1回だけ、タグを問わない人気の記事（`provider.Client.Popular`）も取り、空いた枠の候補にする。人気の記事は14日以内のものだけ使い、点数0の記事どうしはいいね数の多い順に並べる。
- タグの比較は、前後の空白を除いて小文字にそろえて行う（`interest.NormalizeTag`）。
- スコアは提供元で分ける。Qiitaは「記事タグに対応する重みの合計 × (likes + 1)」、Zennは重みの合計のみ。提供元ごとに上位10件を返す（[PR #145](https://github.com/H4aruki/MyTechPulse/pull/145)）。Zennに likes を掛けない理由は Issue / PR のコメントには無く、移行設計には「提供元側の並び順を利用するため」とある。
- 外部提供元の片方だけが失敗したときは、成功した側を返して `warnings` で知らせる。両方失敗したときだけエラーにする。
- パッケージの責務（認証・推薦・提供元・DB）を混ぜない。

変更時は境界値、タグ無し、重複タグ、0件、外部API失敗を確認する。
