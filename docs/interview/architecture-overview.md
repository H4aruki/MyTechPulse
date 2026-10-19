> 本番 main 時点のアーキテクチャ整理ノート。プロダクトの進展より遅れることがある。判断理由は ADR に加え、Issue/PR コメントも参照する（詳細は同名 HTML）。

# MyTechPulse 本番で「今」動いていることだけ（要約）

基準: main `5cc75fd`（2026-10-19 09:53 JST, #238）。同コミットの「Go版の本番への反映」CIジョブ成功 2026-10-19 09:59 JST。詳細は同名の HTML を参照。

1. **一言**: Qiita・Zenn から興味タグに合う新着技術記事を集めて点数順に並べるパーソナライズ技術ニュースWebアプリ。マイページは設定と分離・利用者名のみ表示（[#235](https://github.com/H4aruki/MyTechPulse/issues/235) → [#238](https://github.com/H4aruki/MyTechPulse/pull/238)）。
2. **構成**: ブラウザ → Cloudflare Pages（LP+React SPA）／ SPA → Cloudflare プロキシ → Lightsail東京1GB上の Docker Compose（Caddy → Go API → PostgreSQL 17）→ Qiita・Zenn。Caddy は Cloudflare 以外を 403（[#218](https://github.com/H4aruki/MyTechPulse/pull/218)；Tunnel 却下・FW手動残は [#74](https://github.com/H4aruki/MyTechPulse/issues/74)）。
3. **スタック**: React 19 / TS 6.0 / Vite 8 / Tailwind 4 / TanStack Query / RRD 7 / RHF+zod ・ Go 1.26.4 / Huma v2 / pgx v5 + sqlc / goose / go-webauthn / bcrypt(cost 12) ・ PostgreSQL 17 alpine。理由が文書・Issueにあるのは: モジュラーモノリス（ADR 0001）、サーバー側セッション（ADR 0002）、Cloudflare Pages + Lightsail・DB同居（ADR 0003、月約$8）、追加のみの移行（自動ロールバック時にDBを戻さないため；[#240](https://github.com/H4aruki/MyTechPulse/issues/240) でも goose DELETE 却下）、パスキーは go-webauthn（[#216](https://github.com/H4aruki/MyTechPulse/issues/216)）。画面ライブラリ等は文書・Issue/PRに理由の記録なし。
4. **ドメイン**: `user` / `tag` / `recommend`（user×tag の興味度 `match_int`、0〜10000）/ `auth_session`（SHA-256のみ）/ パスキー3表 / feedback_* 6表。**記事テーブルなし**。
5. **フィード（毎リクエスト）**: 興味度上位5タグ（同値はタグ名昇順）→ 5タグ×{Qiita,Zenn}=最大10 goroutine（channel回収、errgroupなし、全体8s/1回5s）→ Qiita直近5日・Zenn直近14日（0件時のみ古いもので補完）→ URL重複除去 → 採点 → 提供元ごと上位10。片側失敗は warnings、両方失敗は503。空白入りタグは問い合わせ時だけ空白除去（[#206](https://github.com/H4aruki/MyTechPulse/issues/206)/[#208](https://github.com/H4aruki/MyTechPulse/pull/208)）。
   - 興味合計 = 記事タグに一致する利用者の全タグ重みの和。Qiita = 合計×(likes+1)、Zenn = 合計。
6. **学習**: Scale 10000。登録時 選択10000・未選択(tag表の全タグ)8000（[#201](https://github.com/H4aruki/MyTechPulse/issues/201)→[#203](https://github.com/H4aruki/MyTechPulse/pull/203)）。未選択0.8の数値根拠は [#240](https://github.com/H4aruki/MyTechPulse/issues/240) で「記録なし」と明記。減衰0.8は意図したEMA。クリック時 全タグ×8/10 → クリックタグ +2000（上限10000）、未所持タグは2000で新規。見直し（未選択0・人気埋め）は [#240](https://github.com/H4aruki/MyTechPulse/issues/240)/[#242](https://github.com/H4aruki/MyTechPulse/pull/242) だが **#242 未マージのため本番は未選択0.8のまま**。
7. **認証**: HttpOnly・Secure・SameSite=Lax の `__Host-mtp_session`、24h、DBはハッシュのみ。CSRF は許可Origin + `X-MTP-CSRF: 1`。パスワード12文字以上・一般的なもの不可、試行制限（利用者名5回/15分→指数締め出し最大15分、IP20回/15分、メモリ内；WAFのみ案は却下 [#186](https://github.com/H4aruki/MyTechPulse/issues/186)/[#217](https://github.com/H4aruki/MyTechPulse/pull/217)）。パスキー登録/管理/ログイン/conditional UI（[#216](https://github.com/H4aruki/MyTechPulse/issues/216) 系列）、challengeToken は本文で受け渡し・5分・使い捨て（複数タブ問題 [#230](https://github.com/H4aruki/MyTechPulse/issues/230)→[#232](https://github.com/H4aruki/MyTechPulse/pull/232)；BroadcastChannelは見送り）。
8. **インフラ/CI**: CI（lint・Vitest・go vet/test -race・sqlc/OpenAPI差分・イメージ検査）→ 反映要否判定 → digest固定の配布物 → バックアップ → 追加のみ移行 → 入れ替え → 疎通失敗なら自動ロールバック → Cloudflare Pages 公開。人の承認なし、`GO_DEPLOY_ENABLED` で制御。イメージは FROM scratch・非root。
9. **トレードオフ**: 毎回外部API（遅延・障害・上限の影響）、1台構成（単一障害点）、1GB+スワップ、追加のみ移行、メモリ内の試行制限（1台前提）、バックアップは同一サーバー内。
10. **文書とコードのズレ**: DB文書の初期値「1」→ 実際10000/8000、AGENTSの採点式は提供元区別なし → 実際Zennはいいね無し、FeaturesListの試行制限・強度チェック「未実装」→ 実装済み(#217)。
11. **これから（未マージ・未実装。判断はコメント済み）**: [#242](https://github.com/H4aruki/MyTechPulse/pull/242)/[#240](https://github.com/H4aruki/MyTechPulse/issues/240)；[#66](https://github.com/H4aruki/MyTechPulse/issues/66) 定期一括・記事テーブル（1日3回案等）；[#236](https://github.com/H4aruki/MyTechPulse/issues/236) 保存はログイン必須・+3000・誤タップ10秒遅延学習；順は #240→#239→#66。
12. **未確認**: Zennの既定並び順、#203以前の登録者データ、#74のFW手動完了有無、本番サイトへの直接確認。
