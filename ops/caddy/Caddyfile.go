# Go版APIへ向ける、本番APIのHTTPS終端（#127）。
#
# 本番の Caddyfile（Python版の api:8000 向け）と同じ作りで、向け先だけが Go版の api-go:8001。
# 切り替えと切り戻しで、どの設定を使うかは、docker-compose.yml の MTP_CADDYFILE で選ぶ。
#   Python版へ向ける: Caddyfile（既定）
#   メンテナンス応答 : ops/caddy/Caddyfile.maintenance
#   Go版へ向ける     : ops/caddy/Caddyfile.go（このファイル）
# 証明書は caddy_data ボリュームに保存済みのものを引き続き使う（設定を切り替えても取り直しにならない）。
# ドメインは環境変数 API_DOMAIN で指定する。

{$API_DOMAIN} {
	reverse_proxy api-go:8001

	# HTTPSでしか繋がせない。ブラウザは一度アクセスすると以後HTTPを試さなくなる
	header Strict-Transport-Security "max-age=31536000; includeSubDomains"
}
