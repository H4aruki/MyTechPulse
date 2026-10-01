package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// defaultRetryDelay は再試行までの待ち時間。
const defaultRetryDelay = 100 * time.Millisecond

// HTTPClient は http.Client を差し替え可能にするための最小の窓口。
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// JSONFetcher は外部のJSON APIを安全に取得する共通部品。
//
//   - httpsと許可ホストの完全一致だけを受理する
//   - リダイレクトは追従せず、受けたら拒否する
//   - 応答本文は MaxBytes を超えて読まない
//   - GETの一時的な失敗（期限切れ・接続失敗・502/503/504）だけ、
//     呼び出しの期限内で最大1回再試行する。429とその他の4xxは再試行しない
type JSONFetcher struct {
	Client       HTTPClient
	MaxBytes     int64
	AllowedHosts map[string]struct{}

	// RetryDelay は再試行までの待ち時間。0なら100ms。
	RetryDelay time.Duration
	// Now はRetry-Afterの解釈に使う現在時刻。nilなら time.Now。
	Now func() time.Time
}

// NewHTTPClient は、リダイレクトを追従せず、1回の通信に期限を持つクライアントを返す。
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// GetJSON は rawURL へGETし、200応答の本文を out へ復号する。
// 返すエラーは *Error で、URL・ヘッダー・本文は含まない。
func (f JSONFetcher) GetJSON(ctx context.Context, rawURL string, headers http.Header, out any) error {
	if f.Client == nil || f.MaxBytes <= 0 {
		return newError(ErrInvalidRequest, 0)
	}
	if !f.allowed(rawURL) {
		return newError(ErrInvalidRequest, 0)
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return newError(ErrInvalidRequest, 0)
		}
		req.Header.Set("Accept", "application/json")
		for k, vs := range headers {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}

		resp, err := f.Client.Do(req)
		if err != nil {
			perr := classifyTransport(ctx, err)
			if attempt == 0 && ctx.Err() == nil && f.waitRetry(ctx) {
				continue
			}
			return perr
		}

		perr, retryable := f.handle(ctx, req, resp, out)
		if perr == nil {
			return nil
		}
		if attempt == 0 && retryable && f.waitRetry(ctx) {
			continue
		}
		return perr
	}
}

// allowed は rawURL が https かつ許可ホスト（完全一致）か確認する。
func (f JSONFetcher) allowed(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" {
		return false
	}
	_, ok := f.AllowedHosts[u.Hostname()]
	return ok
}

// handle は1回分の応答を処理する。bodyは必ず閉じる。
// 2つ目の戻り値は、再試行してよい失敗かどうか。
func (f JSONFetcher) handle(ctx context.Context, req *http.Request, resp *http.Response, out any) (*Error, bool) {
	defer resp.Body.Close()

	// 注入されたクライアントが勝手に追従していた場合も、別URLの応答は受け取らない。
	if resp.Request != nil && resp.Request.URL != nil && resp.Request.URL.String() != req.URL.String() {
		return newError(ErrInvalidResponse, resp.StatusCode), false
	}

	switch code := resp.StatusCode; {
	case code == http.StatusOK:
		return f.decode(ctx, resp, out), false
	case code == http.StatusTooManyRequests:
		e := newError(ErrRateLimited, code)
		e.RetryAt = f.retryAt(resp.Header.Get("Retry-After"))
		return e, false
	case code >= 300 && code < 400:
		// リダイレクトは追従しない。想定外の応答として扱う。
		return newError(ErrInvalidResponse, code), false
	case code == http.StatusBadGateway, code == http.StatusServiceUnavailable, code == http.StatusGatewayTimeout:
		return newError(ErrUnavailable, code), true
	case code >= 200 && code < 300:
		return newError(ErrInvalidResponse, code), false
	default:
		return newError(ErrUnavailable, code), false
	}
}

func (f JSONFetcher) decode(ctx context.Context, resp *http.Response, out any) *Error {
	if resp.ContentLength > f.MaxBytes {
		return newError(ErrInvalidResponse, resp.StatusCode)
	}
	// 上限+1バイトまで読み、超えたかどうかだけを判定する。
	body, err := io.ReadAll(io.LimitReader(resp.Body, f.MaxBytes+1))
	if err != nil {
		e := classifyTransport(ctx, err)
		e.StatusCode = resp.StatusCode
		return e
	}
	if int64(len(body)) > f.MaxBytes {
		return newError(ErrInvalidResponse, resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return newError(ErrInvalidResponse, resp.StatusCode)
	}
	return nil
}

// classifyTransport は通信エラーを分類する。元のエラーはURLを含み得るので保持しない。
func classifyTransport(ctx context.Context, err error) *Error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		kind := ErrUnavailable
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			kind = ErrTimeout
		}
		return &Error{Kind: kind, cause: ctxErr}
	}
	var ne net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return newError(ErrTimeout, 0)
	}
	return newError(ErrUnavailable, 0)
}

// waitRetry は再試行まで待つ。期限の残りが待ち時間以下、または中断されたらfalse。
func (f JSONFetcher) waitRetry(ctx context.Context) bool {
	delay := f.RetryDelay
	if delay <= 0 {
		delay = defaultRetryDelay
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= delay {
		return false
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// retryAt はRetry-After（秒、またはHTTP日付）を再試行可能時刻へ変換する。解釈できなければゼロ値。
func (f JSONFetcher) retryAt(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	now := time.Now
	if f.Now != nil {
		now = f.Now
	}
	if secs, err := strconv.ParseInt(value, 10, 64); err == nil {
		if secs < 0 || secs > 86400*365 {
			return time.Time{}
		}
		return now().Add(time.Duration(secs) * time.Second).UTC()
	}
	if t, err := http.ParseTime(value); err == nil {
		return t.UTC()
	}
	return time.Time{}
}
