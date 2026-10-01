package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestNewWritesJSONAtOrAboveLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, slog.LevelInfo)
	logger.Debug("hidden")
	logger.Info("shown", "k", "v")

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("log is not single JSON line: %v: %s", err, buf.String())
	}
	if rec["msg"] != "shown" || rec["k"] != "v" || rec["level"] != "INFO" {
		t.Fatalf("unexpected record: %v", rec)
	}
}
