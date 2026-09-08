# `.codex/` — Codexのプロジェクト設定

このフォルダは、MyTechPulseでCodexを安全に動かすための共有設定です。

## 構成

- `config.toml`: 作業領域だけを書き込み可能にし、外部通信は初期状態で無効にする
- `hooks.json`: 危険操作、同じ操作の繰り返し、検証漏れを確認する
- `rules/default.rules`: サンドボックス外でも実行してはいけないコマンドを止める
- `hooks/config.test.mjs`: フック登録の欠落を検出する

フックの判定処理は `.claude/hooks/` と共用しています。防御方針の正本は `AGENTS.md` です。

## 初回だけ必要な操作

プロジェクト設定とフックは、信頼済みのプロジェクトでだけ読み込まれます。Codexをこのリポジトリで起動し、表示された信頼確認を内容を確認して承認してください。

その後、Codex内の `/hooks` でフック定義を確認し、信頼してください。フックを変更するとハッシュが変わるため、再確認が必要です。

## 検証

```bash
node --test ".claude/hooks/**/*.test.mjs" ".codex/hooks/**/*.test.mjs"
codex execpolicy check --pretty --rules .codex/rules/default.rules -- git push origin main
```

2つ目の結果が `forbidden` なら、`main` への直接pushを規則が止めています。

## 限界

- プロジェクトやフックを信頼するまでは、Codexはローカルフックを実行しません。
- コマンド規則はサンドボックス外での実行判断に使われます。サンドボックス内を含む確認はフックと作業ルールも併用します。
- MCPやWeb検索の通信は、コマンド用サンドボックスの外部通信設定とは別に管理されます。
