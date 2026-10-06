#!/usr/bin/env bash
# ops/rehearsal_smoke.sh を、本物のAPIを使わず、偽のcurlで確かめる。
# 使い方: bash ops/rehearsal_smoke_test.sh
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
chmod 700 -- "$work"
trap 'rm -rf -- "$work"' EXIT

mkdir -p "$work/bin" "$work/tmp"

# 偽のcurl。呼び出しを calls.log に控え、環境変数FAKE_*の指定どおりに応答する
cat >"$work/bin/curl" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail
headers="" method=GET out="" write="" jar_save="" jar_send="" url="" data=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        -X) method="$2"; shift 2 ;;
        -w) write="$2"; shift 2 ;;
        --output) out="$2"; shift 2 ;;
        -c) jar_save="$2"; shift 2 ;;
        -b) jar_send="$2"; shift 2 ;;
        -H) headers+="$2"$'\n'; shift 2 ;;
        --max-time) shift 2 ;;
        --data-binary) data="$2"; shift 2 ;;
        -s | -sS | -k) shift ;;
        -*) echo "fake curl: 未対応の引数 $1" >&2; exit 99 ;;
        *) url="$1"; shift ;;
    esac
done
path="/${url#*://*/}"
printf '%s\n' "$*" >>"$FAKE_DIR/args.log"
body_in=""
[ -z "$data" ] || body_in="$(cat)"
token_sent=""
if [ -n "$jar_send" ] && [ -f "$jar_send" ]; then
    token_sent="$(awk -F'\t' '$6 == "mtp_session" {print $7}' "$jar_send")"
fi
printf '%s %s cookie=%s\n' "$method" "$path" "${token_sent:-none}" >>"$FAKE_DIR/calls.log"

respond() { printf '%s' "$2" >"$out"; printf '%s' "$1"; }

# 更新系の要求は、許可されたOriginと専用ヘッダーが無ければ本物のAPIと同じく403にする
if [ "$method" = POST ]; then
    case "$headers" in *$'Origin: https://localhost:18443\n'*) ;; *) respond 403 ''; exit 0 ;; esac
    case "$headers" in *$'X-MTP-CSRF: 1\n'*) ;; *) respond 403 ''; exit 0 ;; esac
fi

forced="$(grep -F "$method $path=" "$FAKE_DIR/force" 2>/dev/null | head -1 || true)"
if [ -n "$forced" ]; then
    respond "${forced##*=}" ''
    exit 0
fi

case "$method $path" in
    "GET /health/live" | "GET /health/ready") respond 200 '{"status":"ok"}' ;;
    "POST /api/v1/auth/signup")
        name="${body_in#*\"username\":\"}"; name="${name%%\"*}"
        printf '%s' "$name" >"$FAKE_DIR/username"
        mark='#HttpOnly_'
        [ -z "${FAKE_NO_HTTPONLY:-}" ] || mark=''
        printf '%s127.0.0.1\tFALSE\t/\tFALSE\t0\tmtp_session\tSYNTHETIC-SESSION-TOKEN\n' "$mark" >"$jar_save"
        respond 201 "{\"user\":{\"id\":1,\"username\":\"$name\",\"role\":\"member\"}}" ;;
    "GET /api/v1/auth/me")
        if [ -z "$token_sent" ] || [ -f "$FAKE_DIR/revoked" ]; then
            respond 401 '{"code":"unauthenticated"}'
        else
            respond 200 "{\"id\":1,\"username\":\"$(cat "$FAKE_DIR/username")\",\"role\":\"member\"}"
        fi ;;
    "GET /api/v1/feed")
        default_feed='{"qiita_articles":[{"title":"t","url":"https://qiita.com/a/items/1","source":"Qiita","tags":["go"],"likes":3,"published_at":"2026-10-01T00:00:00Z"}],"zenn_articles":[],"warnings":[]}'
        respond 200 "${FAKE_FEED:-$default_feed}" ;;
    "POST /api/v1/feedback/article-clicks") respond 204 '' ;;
    "POST /api/v1/auth/logout")
        [ -n "${FAKE_SESSION_SURVIVES:-}" ] || : >"$FAKE_DIR/revoked"
        respond 204 '' ;;
    *) respond 404 '' ;;
esac
FAKE
chmod +x "$work/bin/curl"

passed=0
fail() {
    printf 'FAIL: %s\n' "$1" >&2
    exit 1
}

PASSWORD='synthetic-Password-for-test-1'
TOKEN='SYNTHETIC-SESSION-TOKEN'

# run_smoke [ENV=VALUE ...] : 結果を SMOKE_STATUS / SMOKE_OUT / SMOKE_ERR へ入れる
run_smoke() {
    rm -rf -- "$work/fake" "$work/tmp"
    mkdir -p "$work/fake" "$work/tmp"
    : >"$work/fake/force"
    : >"$work/fake/calls.log"
    : >"$work/fake/args.log"
    set +e
    env PATH="$work/bin:$PATH" TMPDIR="$work/tmp" FAKE_DIR="$work/fake" \
        MTP_REHEARSAL_SMOKE_PASSWORD="$PASSWORD" "$@" \
        bash "$repo_root/ops/rehearsal_smoke.sh" >"$work/out" 2>"$work/err"
    SMOKE_STATUS=$?
    set -e
    SMOKE_OUT="$(cat "$work/out")"
    SMOKE_ERR="$(cat "$work/err")"
}

expect_failure() {
    local name="$1" status="$2" message="$3"
    [ "$SMOKE_STATUS" -eq "$status" ] || fail "$name: 終了コード $SMOKE_STATUS（$status を期待）"
    case "$SMOKE_ERR" in
        "smoke: failed ($message"*) ;;
        *) fail "$name: 失敗文が想定外: $SMOKE_ERR" ;;
    esac
    [ -z "$(ls -A "$work/tmp")" ] || fail "$name: 一時ファイルが残っている"
    passed=$((passed + 1))
    printf 'ok: %s\n' "$name"
}

# 1. 正常系: 呼び出しの順番と出力
run_smoke
[ "$SMOKE_STATUS" -eq 0 ] || fail "正常系が失敗した: $SMOKE_ERR"
expected_calls="GET /health/live cookie=none
GET /health/ready cookie=none
POST /api/v1/auth/signup cookie=none
GET /api/v1/auth/me cookie=$TOKEN
GET /api/v1/feed cookie=$TOKEN
POST /api/v1/feedback/article-clicks cookie=$TOKEN
POST /api/v1/auth/logout cookie=$TOKEN
GET /api/v1/auth/me cookie=$TOKEN"
[ "$(cat "$work/fake/calls.log")" = "$expected_calls" ] || fail "呼び出しの順番が違う: $(cat "$work/fake/calls.log")"
expected_out="smoke: health-live ok (200)
smoke: health-ready ok (200)
smoke: signup ok (201)
smoke: me ok (200)
smoke: feed ok (200)
smoke: click ok (204)
smoke: logout ok (204)
smoke: me-after-logout ok (401)
smoke: ok"
[ "$SMOKE_OUT" = "$expected_out" ] || fail "出力が想定外: $SMOKE_OUT"
[ -z "$SMOKE_ERR" ] || fail "正常系なのに標準エラーがある"
[ -z "$(ls -A "$work/tmp")" ] || fail "正常系で一時ファイルが残っている"
passed=$((passed + 1))
printf 'ok: 正常系の順番と出力\n'

# 2. パスワード・Cookie・利用者名が、出力にも引数にも出ない
for secret in "$PASSWORD" "$TOKEN"; do
    for f in "$work/out" "$work/err" "$work/fake/args.log"; do
        if grep -qF -- "$secret" "$f"; then
            fail "秘密の値が $(basename "$f") に出ている"
        fi
    done
done
if grep -q 'rehearsal-smoke-' "$work/out" "$work/err" "$work/fake/args.log"; then
    fail "利用者名が出力か引数に出ている"
fi
passed=$((passed + 1))
printf 'ok: 秘密の値が出力・引数に出ない\n'

# 3. 利用者名は rehearsal-smoke- で始まる（後始末の対象を絞れる）
run_smoke
grep -q '^rehearsal-smoke-[0-9a-f]\{12\}$' "$work/fake/username" || fail "既定の利用者名の形が違う"
passed=$((passed + 1))
printf 'ok: 既定の利用者名は rehearsal-smoke- 始まり\n'

# 4. 各工程が想定外の状態番号を返したら、そこで止まる
force_case() {
    local name="$1" request="$2" step="$3" last_call="$4"
    rm -rf -- "$work/fake"
    run_smoke_forced "$request"
    expect_failure "$name" 1 "$step: 状態番号"
    [ "$(tail -1 "$work/fake/calls.log" | cut -d' ' -f1-2)" = "$last_call" ] \
        || fail "$name: 失敗後も次の工程を呼んでいる: $(tail -1 "$work/fake/calls.log")"
}
run_smoke_forced() {
    rm -rf -- "$work/fake" "$work/tmp"
    mkdir -p "$work/fake" "$work/tmp"
    printf '%s\n' "$1" >"$work/fake/force"
    : >"$work/fake/calls.log"
    : >"$work/fake/args.log"
    set +e
    env PATH="$work/bin:$PATH" TMPDIR="$work/tmp" FAKE_DIR="$work/fake" \
        MTP_REHEARSAL_SMOKE_PASSWORD="$PASSWORD" \
        bash "$repo_root/ops/rehearsal_smoke.sh" >"$work/out" 2>"$work/err"
    SMOKE_STATUS=$?
    set -e
    SMOKE_OUT="$(cat "$work/out")"
    SMOKE_ERR="$(cat "$work/err")"
}
force_case 'ready失敗で止まる' 'GET /health/ready=503' health-ready 'GET /health/ready'
force_case '登録失敗で止まる' 'POST /api/v1/auth/signup=500' signup 'POST /api/v1/auth/signup'
force_case '重複登録(409)で止まる' 'POST /api/v1/auth/signup=409' signup 'POST /api/v1/auth/signup'
force_case '記事一覧が全提供元失敗(503)で止まる' 'GET /api/v1/feed=503' feed 'GET /api/v1/feed'
force_case 'クリックが204以外で止まる' 'POST /api/v1/feedback/article-clicks=200' click 'POST /api/v1/feedback/article-clicks'
force_case 'ログアウトが204以外で止まる' 'POST /api/v1/auth/logout=500' logout 'POST /api/v1/auth/logout'

# 5. ログアウト後も同じCookieが通ってしまう（サーバー側で無効にならない）
run_smoke FAKE_SESSION_SURVIVES=1
expect_failure 'ログアウト後も旧Cookieが通ると失敗する' 1 'me-after-logout: 状態番号 200'

# 6. 記事一覧の中身の検査
feed_case() {
    local name="$1" feed="$2" message="$3"
    run_smoke FAKE_FEED="$feed"
    expect_failure "$name" 1 "$message"
}
feed_case '記事URLがhttpだと失敗する' \
    '{"qiita_articles":[{"url":"http://qiita.com/a","source":"Qiita"}],"zenn_articles":[],"warnings":[]}' 'feed: 想定外の記事URL'
feed_case '記事URLが別ドメインだと失敗する' \
    '{"qiita_articles":[],"zenn_articles":[{"url":"https://evil.example/a","source":"Zenn"}],"warnings":[]}' 'feed: 想定外の記事URL'
feed_case '提供元が想定外だと失敗する' \
    '{"qiita_articles":[{"url":"https://qiita.com/a","source":"Other"}],"zenn_articles":[],"warnings":[]}' 'feed: 想定外の提供元'
feed_case '内部の点数が出ていると失敗する' \
    '{"qiita_articles":[{"url":"https://qiita.com/a","source":"Qiita","score":1}],"zenn_articles":[],"warnings":[]}' 'feed: 内部の点数'
feed_case '記事一覧の項目が欠けると失敗する' \
    '{"qiita_articles":[],"warnings":[]}' 'feed: 記事一覧の項目が無い'

# 部分成功（片方が空で警告つき）は成功として扱う
run_smoke FAKE_FEED='{"qiita_articles":[],"zenn_articles":[{"url":"https://zenn.dev/a/articles/b","source":"Zenn"}],"warnings":[{"provider":"qiita","code":"provider_unavailable"}]}'
[ "$SMOKE_STATUS" -eq 0 ] || fail "部分成功が失敗扱いになった: $SMOKE_ERR"
passed=$((passed + 1))
printf 'ok: 片方の提供元だけの部分成功は成功\n'

# 7. CookieがHttpOnlyでなければ失敗
run_smoke FAKE_NO_HTTPONLY=1
expect_failure 'CookieがHttpOnlyでないと失敗する' 1 'signup: CookieがHttpOnlyでない'

# 8. 設定の不正は、APIを呼ぶ前に終了コード2で止まる
run_smoke MTP_REHEARSAL_BASE_URL='ftp://x'
expect_failure '不正なURLは設定エラー' 2 'E_CONFIG'
[ ! -s "$work/fake/calls.log" ] || fail "設定エラーなのにAPIを呼んだ"
run_smoke MTP_REHEARSAL_SMOKE_PASSWORD='bad"quote'
expect_failure '引用符入りのpasswordは設定エラー' 2 'E_CONFIG'
run_smoke MTP_REHEARSAL_SMOKE_USERNAME='back\slash'
expect_failure 'バックスラッシュ入りの利用者名は設定エラー' 2 'E_CONFIG'
[ ! -s "$work/fake/calls.log" ] || fail "設定エラーなのにAPIを呼んだ"

printf 'OK: %s smoke cases passed\n' "$passed"
