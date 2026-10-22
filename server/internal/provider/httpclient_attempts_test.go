package provider

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
)

func TestMaxAttemptsControlsRetries(t *testing.T) {
	for _, tc := range []struct {
		name string
		max  int
		want int32
	}{
		{"0は今までどおり2回", 0, 2},
		{"1は再試行しない", 1, 1},
		{"4なら最大4回", 4, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls, closed atomic.Int32
			f := fakeFetcher(func(req *http.Request) (*http.Response, error) {
				calls.Add(1)
				return fakeResponse(req, http.StatusServiceUnavailable, "", &closed), nil
			})
			f.MaxAttempts = tc.max
			var out okBody
			err := f.GetJSON(context.Background(), fakeURL, nil, &out)
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("err = %v", err)
			}
			if calls.Load() != tc.want {
				t.Fatalf("calls = %d, want %d", calls.Load(), tc.want)
			}
		})
	}
}

func TestMaxAttemptsAlsoLimitsTransportRetries(t *testing.T) {
	var calls atomic.Int32
	f := fakeFetcher(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, timeoutErr{}
	})
	f.MaxAttempts = 1
	var out okBody
	if err := f.GetJSON(context.Background(), fakeURL, nil, &out); !errors.Is(err, ErrTimeout) {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestGetJSONHeaderReturnsResponseHeader(t *testing.T) {
	var closed atomic.Int32
	f := fakeFetcher(func(req *http.Request) (*http.Response, error) {
		resp := fakeResponse(req, http.StatusOK, `{"ok":true}`, &closed)
		resp.Header.Set("Rate-Remaining", "321")
		return resp, nil
	})
	f.MaxBytes = 1024
	var out okBody
	h, err := f.GetJSONHeader(context.Background(), fakeURL, nil, &out)
	if err != nil || !out.OK {
		t.Fatalf("out = %+v, err = %v", out, err)
	}
	if got := h.Get("Rate-Remaining"); got != "321" {
		t.Fatalf("Rate-Remaining = %q", got)
	}
}

func TestGetJSONHeaderReturnsHeaderOnFailureStatus(t *testing.T) {
	var closed atomic.Int32
	f := fakeFetcher(func(req *http.Request) (*http.Response, error) {
		resp := fakeResponse(req, http.StatusTooManyRequests, "", &closed)
		resp.Header.Set("Rate-Remaining", "0")
		return resp, nil
	})
	var out okBody
	h, err := f.GetJSONHeader(context.Background(), fakeURL, nil, &out)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	if h.Get("Rate-Remaining") != "0" {
		t.Fatalf("header = %v", h)
	}
}
