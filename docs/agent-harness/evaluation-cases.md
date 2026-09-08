# ハーネス評価ケース

設定変更後は、実際の破壊操作を行わず、判定関数とCLIの検査機能で確認します。

## 自動確認

- `.env` と `backend/.env` の内容を読むコマンドを拒否する
- `.env.example` の参照と、秘密ファイルの存在確認は許可する
- force pushと `main` への直接pushを拒否する
- PRのマージと承認を拒否し、通常コメントは許可する
- DBボリューム削除、SSH、手動Wrangler公開を拒否する
- 同じ操作の3回連続を止める
- コード変更後、検証を行わず終了する操作を止める
- Codexの主要フックイベントとWindows用コマンドが登録されている
- `.agents/skills/` と `.claude/skills/` の全ファイルが一致する

## 手動確認

1. Claude Codeを開き、`CLAUDE.md` 経由で `AGENTS.md` が読み込まれることを確認する。
2. Codexを開き、プロジェクトとフックを信頼した後に `/hooks` で4種類のイベントを確認する。
3. `codex.cmd mcp list` でExa、Context7、Playwrightの状態を確認する。
4. `claude.cmd plugin list --json` で同等プラグインが有効なことを確認する。
5. ローカル画面でPlaywrightの読み取り中心の操作を試し、`.playwright-mcp/` がGit管理外であることを確認する。

## 合格条件

- 自動テストがすべて成功する。
- 禁止操作が実行前に止まる。
- 通常の読み取り、編集、lint、buildが不必要に止まらない。
- 認証情報とローカル作業ファイルがGit差分へ現れない。
