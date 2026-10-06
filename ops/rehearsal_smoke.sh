#!/usr/bin/env bash
# リハーサル環境のGo版APIで、主要な流れが通ることを確かめる（登録→本人確認→記事一覧→クリック→ログアウト）。
#
# 使い方: bash ops/rehearsal_smoke.sh
# 入力（環境変数）:
#   MTP_REHEARSAL_BASE_URL         APIの根のURL。既定は http://127.0.0.1:18001
#   MTP_REHEARSAL_ORIGIN           更新系の要求に付けるOrigin。APIの許可Originと同じ値にする。
#                                  既定は docker-compose.rehearsal.yml の既定値 https://localhost:18443
#   MTP_REHEARSAL_INSECURE_TLS     1のとき証明書の検証を省く（隔離環境の内部CA用）。既定は検証する
#   MTP_REHEARSAL_SMOKE_USERNAME   合成の利用者名。未指定なら rehearsal-smoke- で始まる名前を作る
#   MTP_REHEARSAL_SMOKE_PASSWORD   合成のパスワード。未指定ならこの実行だけの乱数を作る
#   MTP_REHEARSAL_EXISTING_USERNAME / MTP_REHEARSAL_EXISTING_PASSWORD
#                                  任意。復元したDBにもともとある利用者でのログイン確認（旧版が作ったパスワードの形式を
#                                  Go版が読めるかの確認）。2つ一緒に指定する。ログインして本人確認し、ログアウトするだけで、
#                                  興味度などは変えない。ops/make_synthetic_dump.sh の合成dumpならこの値で通る
# 出力は工程名と成否、HTTPの状態番号だけ。パスワード・Cookie・応答本文は出さない。
# 合成の利用者はDBへ残る。後始末は #127 の承認済みcleanupが「rehearsal-smoke-」始まりの利用者を対象に行う。
# 終了コード: 0=すべて成功、1=どれかの工程が失敗、2=設定が不正
set -euo pipefail
set +x
umask 077

fail() {
    printf 'smoke: failed (%s)\n' "$1" >&2
    exit "${2:-1}"
}

base_url="${MTP_REHEARSAL_BASE_URL:-http://127.0.0.1:18001}"
case "$base_url" in
    http://* | https://*) ;;
    *) fail 'E_CONFIG' 2 ;;
esac
base_url="${base_url%/}"
origin="${MTP_REHEARSAL_ORIGIN:-https://localhost:18443}"
case "$origin" in
    http://* | https://*) ;;
    *) fail 'E_CONFIG' 2 ;;
esac

random_hex() {
    head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'
}

username="${MTP_REHEARSAL_SMOKE_USERNAME:-rehearsal-smoke-$(random_hex 6)}"
password="${MTP_REHEARSAL_SMOKE_PASSWORD:-$(random_hex 16)}"
# JSONへそのまま入れるため、引用符・バックスラッシュ・制御文字は受け付けない
for value in "$username" "$password"; do
    case "$value" in
        '' | *[\"\\]* | *[[:cntrl:]]*) fail 'E_CONFIG' 2 ;;
    esac
done

existing_username="${MTP_REHEARSAL_EXISTING_USERNAME:-}"
existing_password="${MTP_REHEARSAL_EXISTING_PASSWORD:-}"
# 既存の利用者の確認は、利用者名とパスワードの両方があるときだけ行う
if [ -n "$existing_username$existing_password" ]; then
    for value in "$existing_username" "$existing_password"; do
        case "$value" in
            '' | *[\"\\]* | *[[:cntrl:]]*) fail 'E_CONFIG' 2 ;;
        esac
    done
fi

tmp_dir="$(mktemp -d)"
chmod 700 -- "$tmp_dir"
trap 'rm -rf -- "$tmp_dir"' EXIT
jar="$tmp_dir/jar"
body="$tmp_dir/body"

curl_flags=(-sS --max-time 20 --output "$body")
if [ "${MTP_REHEARSAL_INSECURE_TLS:-0}" = "1" ]; then
    curl_flags+=(-k)
fi

# call STEP METHOD PATH EXPECTED_STATUS [JAR_FILE [JAR_MODE]]
# 本文が要るときは標準入力から渡す。password等を引数へ載せないため。JAR_MODE: save | send
call() {
    local step="$1" method="$2" path="$3" expected="$4" jar_file="${5:-}" jar_mode="${6:-}"
    local args=("${curl_flags[@]}" -X "$method" -w '%{http_code}')
    case "$jar_mode" in
        save) args+=(-c "$jar_file") ;;
        send) args+=(-b "$jar_file") ;;
    esac
    if [ "$method" = POST ]; then
        # 更新系の要求には、許可されたOriginと専用ヘッダーの両方が要る（CSRF対策）
        args+=(-H 'Content-Type: application/json' -H "Origin: $origin" -H 'X-MTP-CSRF: 1' --data-binary @-)
    fi
    : >"$body"
    local status
    status="$(curl "${args[@]}" "$base_url$path")" || fail "$step: 接続できない"
    if [ "$status" != "$expected" ]; then
        fail "$step: 状態番号 $status（$expected を期待）"
    fi
    printf 'smoke: %s ok (%s)\n' "$step" "$status"
}

# 1. 生存・準備完了
call health-live GET /health/live 200 </dev/null
call health-ready GET /health/ready 200 </dev/null

# 1b. 復元したDBにもともとある利用者でログインできる（指定があるときだけ）
if [ -n "$existing_username" ]; then
    existing_jar="$tmp_dir/jar.existing"
    printf '{"username":"%s","password":"%s"}' "$existing_username" "$existing_password"         | call login-existing POST /api/v1/auth/login 200 "$existing_jar" save
    grep -q '^#HttpOnly_' "$existing_jar" || fail 'login-existing: CookieがHttpOnlyでない'
    call me-existing GET /api/v1/auth/me 200 "$existing_jar" send </dev/null
    grep -qF "\"username\":\"$existing_username\"" "$body" || fail 'me-existing: 利用者名が一致しない'
    call logout-existing POST /api/v1/auth/logout 204 "$existing_jar" send </dev/null
fi

# 2. 登録（Cookieを受け取る）
printf '{"username":"%s","password":"%s","favorite_tags":["go","react"]}' "$username" "$password" \
    | call signup POST /api/v1/auth/signup 201 "$jar" save
# 受け取ったCookieはJavaScriptから読めない設定（HttpOnly）であること
grep -q '^#HttpOnly_' "$jar" || fail 'signup: CookieがHttpOnlyでない'

# 3. 本人確認
call me GET /api/v1/auth/me 200 "$jar" send </dev/null
grep -qF "\"username\":\"$username\"" "$body" || fail 'me: 利用者名が一致しない'

# 4. 記事一覧。形だけを確かめ、中身は出さない
call feed GET /api/v1/feed 200 "$jar" send </dev/null
grep -q '"qiita_articles":' "$body" && grep -q '"zenn_articles":' "$body" || fail 'feed: 記事一覧の項目が無い'
# 記事のURLは、QiitaかZennのhttpsだけ
if grep -Eo '"url":"[^"]*"' "$body" | grep -Ev '^"url":"https://(qiita\.com|zenn\.dev)/'; then
    fail 'feed: 想定外の記事URLがある'
fi
# 提供元はQiitaかZennだけ
if grep -Eo '"source":"[^"]*"' "$body" | grep -Ev '^"source":"(Qiita|Zenn)"$'; then
    fail 'feed: 想定外の提供元がある'
fi
# 内部の点数は外へ出さない
if grep -Eiq '"(score|weight|match_int)"' "$body"; then
    fail 'feed: 内部の点数が応答に出ている'
fi

# 5. クリックの記録
printf '{"tags":["go"]}' | call click POST /api/v1/feedback/article-clicks 204 "$jar" send

# 6. ログアウト。サーバー側でも無効になることを、ログアウト前のCookieで確かめる
cp -- "$jar" "$tmp_dir/jar.before-logout"
call logout POST /api/v1/auth/logout 204 "$jar" send </dev/null
call me-after-logout GET /api/v1/auth/me 401 "$tmp_dir/jar.before-logout" send </dev/null

printf 'smoke: ok\n'
