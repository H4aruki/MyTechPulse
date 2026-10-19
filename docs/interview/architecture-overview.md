> 個人の面接準備・頭の整理用。本番 main 時点の事実のみに基づき、プロダクトの進展より遅れることがある。

# MyTechPulse 本番で「今」動いていることだけ（要約）

基準: main `5cc75fd`（2026-10-19 09:53 JST, #238）。同コミットの「Go版の本番への反映」CIジョブ成功 2026-10-19 09:59 JST。詳細は同名の HTML を参照。

1. **一言**: Qiita・Zenn から興味タグに合う新着技術記事を集めて点数順に並べるパーソナライズ技術ニュースWebアプリ。
2. **構成**: ブラウザ → Cloudflare Pages（LP+React SPA）／ SPA → Cloudflare プロキシ → Lightsail東京1GB上の Docker Compose（Caddy → Go API → PostgreSQL 17）→ Qiita・Zenn。Caddy は Cloudflare 以外を 403（#218）。
3. **スタック**: React 19 / TS 6.0 / Vite 8 / Tailwind 4 / TanStack Query / RRD 7 / RHF+zod ・ Go 1.26.4 / Huma v2 / pgx v5 + sqlc / goose / go-webauthn / bcrypt(cost 12) ・ PostgreSQL 17 alpine。理由が文書にあるのは: モジュラーモノリス（ADR 0001）、サーバー側セッション（ADR 0002）、Cloudflare Pages + Lightsail・DB同居（ADR 0003、月約$8）、追加のみの移行（自動ロールバック時にDBを戻さないため）。他は文書に理由なし。
4. **ドメイン**: `user` / `tag` / `recommend`（user×tag の興味度 `match_int`、0〜10000）/ `auth_session`（SHA-256のみ）/ パスキー3表 / feedback_* 6表。**記事テーブルなし**。
5. **フィード（毎リクエスト）**: 興味度上位5タグ（同値はタグ名昇順）→ 5タグ×{Qiita,Zenn}=最大10 goroutine（channel回収、errgroupなし、全体8s/1回5s）→ Qiita直近5日・Zenn直近14日（0件時のみ古いもので補完）→ URL重複除去 → 採点 → 提供元ごと上位10。片側失敗は warnings、両方失敗は503。
   - 興味合計 = 記事タグに一致する利用者の全タグ重みの和。Qiita = 合計×(likes+1)、Zenn = 合計。
6. **学習**: Scale 10000。登録時 選択10000・未選択(tag表の全タグ)8000（#203）。クリック時 全タグ×8/10 → クリックタグ +2000（上限10000）、未所持タグは2000で新規。利用者行 FOR UPDATE で直列化。
7. **認証**: HttpOnly・Secure・SameSite=Lax の `__Host-mtp_session`、24h、DBはハッシュのみ。CSRF は許可Origin + `X-MTP-CSRF: 1`。パスワード12文字以上・一般的なもの不可、試行制限（利用者名5回/15分→指数締め出し最大15分、IP20回/15分、メモリ内）。パスキー登録/管理/ログイン/conditional UI、challengeToken は本文で受け渡し・5分・使い捨て（#230→#232）。
8. **インフラ/CI**: CI（lint・Vitest・go vet/test -race・sqlc/OpenAPI差分・イメージ検査）→ 反映要否判定 → digest固定の配布物 → バックアップ → 追加のみ移行 → 入れ替え → 疎通失敗なら自動ロールバック → Cloudflare Pages 公開。人の承認なし、`GO_DEPLOY_ENABLED` で制御。イメージは FROM scratch・非root。
9. **トレードオフ**: 毎回外部API（遅延・障害・上限の影響）、1台構成（単一障害点）、1GB+スワップ、追加のみ移行、メモリ内の試行制限（1台前提）、バックアップは同一サーバー内。
10. **言わない**: pgvector/FTS/埋め込み/ML、記事バッチ・キャッシュ（#66）、未選択0・人気記事補完（#242 未マージ）、記事保存（#236）、似たタグ（#241）、errgroup、登録後のタグ変更、監視（#187）・外部バックアップ（#188）・規約（#185）、冗長化。
11. **文書とコードのズレ**: DB文書の初期値「1」→ 実際10000/8000、AGENTSの採点式は提供元区別なし → 実際Zennはいいね無し、FeaturesListの試行制限・強度チェック「未実装」→ 実装済み(#217)。
12. **未確認**: Zennの既定並び順、#203以前の登録者データ、#74の残作業、本番サイトへの直接確認。
