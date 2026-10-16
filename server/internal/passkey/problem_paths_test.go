package passkey

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/H4aruki/MyTechPulse/server/internal/platform/httpx"
)

func TestProblemMapsEveryPasskeyErrorEvenWhenWrapped(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{ErrNotFound, http.StatusNotFound, "not_found"},
		{ErrLimitReached, http.StatusConflict, "passkey_limit_reached"},
		{ErrCredentialExists, http.StatusConflict, "passkey_already_registered"},
		{ErrChallengeInvalid, http.StatusBadRequest, "passkey_challenge_invalid"},
		{ErrVerificationFailed, http.StatusBadRequest, "passkey_verification_failed"},
		{ErrInvalidName, http.StatusUnprocessableEntity, "validation_failed"},
	}
	for _, c := range cases {
		for _, err := range []error{c.err, fmt.Errorf("repository: %w", c.err)} {
			got, ok := problem(err).(*httpx.ErrorModel)
			if !ok || got.Status != c.status || got.Code != c.code {
				t.Errorf("problem(%v) = %+v", err, got)
			}
		}
	}
}

func TestProblemInvalidNamePointsAtTheNameField(t *testing.T) {
	got := problem(ErrInvalidName).(*httpx.ErrorModel)
	if len(got.Errors) != 1 || got.Errors[0].Field != "name" {
		t.Fatalf("errors = %+v", got.Errors)
	}
}

func TestProblemKeepsAnExistingStatusErrorUntouched(t *testing.T) {
	existing := httpx.NewProblem(http.StatusTooManyRequests, "too_many_requests", "しばらくしてからお試しください")
	if got := problem(existing); got != error(existing) {
		t.Fatalf("別のエラーに作り替えた: %+v", got)
	}
}

func TestProblemHidesTheCauseOfUnknownErrors(t *testing.T) {
	got := problem(fmt.Errorf("insert: %w", fmt.Errorf("pq: password authentication failed for user mtp_app"))).(*httpx.ErrorModel)
	if got.Status != http.StatusInternalServerError || got.Code != "internal_error" || len(got.Errors) != 0 {
		t.Fatalf("got = %+v", got)
	}
	// 応答に載る項目すべて(Detail以外も)に原因が無いことを、JSON全体で確かめる
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"pq", "password", "mtp_app", "insert"} {
		if strings.Contains(string(raw), leaked) {
			t.Errorf("応答に原因が出ている(%q): %s", leaked, raw)
		}
	}
}
