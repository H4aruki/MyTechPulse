package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
)

func TestNewHumaErrorDropsValuesAndSetsCode(t *testing.T) {
	err := newHumaError(http.StatusUnprocessableEntity, "validation failed",
		&huma.ErrorDetail{Message: "expected length <= 50", Location: "body.password", Value: "super-secret-value"},
		errors.New("plain"),
		nil,
	)
	data, mErr := json.Marshal(err)
	if mErr != nil {
		t.Fatal(mErr)
	}
	if strings.Contains(string(data), "super-secret-value") {
		t.Fatalf("input value leaked: %s", data)
	}
	var p Problem
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	if p.Status != 422 || p.Code != "validation_failed" || p.Type != "about:blank" || p.Title != "Unprocessable Entity" {
		t.Fatalf("problem = %+v", p)
	}
	if len(p.Errors) != 2 || p.Errors[0].Field != "password" || p.Errors[1].Message != "plain" {
		t.Fatalf("errors = %+v", p.Errors)
	}
}

func TestCodeForStatus(t *testing.T) {
	if codeForStatus(401) != "unauthenticated" || codeForStatus(503) != "service_unavailable" || codeForStatus(418) != "error" {
		t.Fatal("unexpected status codes")
	}
}
