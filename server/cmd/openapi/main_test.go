package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestWriteSpecMatchesRepository(t *testing.T) {
	want := repositorySpec(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "openapi.json")
	if err := writeSpec(path); err != nil {
		t.Fatal(err)
	}
	if err := writeSpec(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("生成結果が server/openapi/openapi.json と一致しない\n%s", firstDiff(got, want))
	}
}

func TestRunWritesRepositorySpecAndIgnoresArgs(t *testing.T) {
	want := repositorySpec(t)
	dir := t.TempDir()
	t.Chdir(dir)
	var stdout, stderr bytes.Buffer
	// いまのコマンドは引数を見ない。-h を渡しても使い方は出さず、仕様を書く。
	code := run([]string{"-h", "extra"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, specPath))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("生成結果が server/openapi/openapi.json と一致しない\n%s", firstDiff(got, want))
	}
}

// core.autocrlf=true の Windows では、Git が LF の正本を CRLF で取り出す。
// 正本を CRLF にした一時コピーを、生成結果と比べる。
func TestWriteSpecMatchesCRLFRepositoryCopy(t *testing.T) {
	repo := repositorySpec(t)
	copyPath := filepath.Join(t.TempDir(), "openapi.json")
	crlf := bytes.ReplaceAll(repo, []byte("\n"), []byte("\r\n"))
	if !bytes.Contains(crlf, []byte("\r\n")) {
		t.Fatal("CRLF に変換した一時コピーに改行が無い")
	}
	if err := os.WriteFile(copyPath, crlf, 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(raw, repo) {
		t.Fatal("そろえる前の一時コピーが、すでに LF の正本と一致している")
	}

	out := filepath.Join(t.TempDir(), "openapi.json")
	if err := writeSpec(out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := readSpecNormalized(t, copyPath)
	if !bytes.Equal(got, want) {
		t.Fatalf("生成結果が CRLF の一時コピーと一致しない\n%s", firstDiff(got, want))
	}
}

func TestRunExitCodeWhenSpecCannotBeWritten(t *testing.T) {
	t.Run("親がファイル", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "openapi"), []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)
		assertSpecWriteFailed(t)
	})
	t.Run("出力先がディレクトリ", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, specPath), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(dir)
		assertSpecWriteFailed(t)
	})
}

func TestNoopCheckerPing(t *testing.T) {
	if err := (noopChecker{}).Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func assertSpecWriteFailed(t *testing.T) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(nil, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.HasPrefix(stderr.String(), "openapi: ") || !strings.HasSuffix(stderr.String(), "\n") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if strings.Count(stderr.String(), "\n") != 1 {
		t.Fatalf("stderr が1行ではない: %q", stderr.String())
	}
}

// repositorySpec は server/openapi/openapi.json を読む。
// .gitattributes で LF 固定なのは *.sh だけなので、core.autocrlf=true の
// Windows では正本が CRLF で取り出される。中身が同じでもバイト比較が落ちないよう、
// 読み込んだ正本の CRLF を LF にそろえてから返す。生成側は常に LF のまま比べる。
func repositorySpec(t *testing.T) []byte {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("テストファイルの場所が分からない")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "openapi", "openapi.json")
	return readSpecNormalized(t, path)
}

// readSpecNormalized は path の仕様ファイルを読み、改行を LF にそろえて返す。
func readSpecNormalized(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return normalizeSpecNewlines(data)
}

// normalizeSpecNewlines は正本側の CRLF を LF にする。生成結果には使わない。
func normalizeSpecNewlines(data []byte) []byte {
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}

func firstDiff(got, want []byte) string {
	g := strings.Split(string(got), "\n")
	w := strings.Split(string(want), "\n")
	n := len(g)
	if len(w) > n {
		n = len(w)
	}
	for i := 0; i < n; i++ {
		var gs, ws string
		if i < len(g) {
			gs = g[i]
		}
		if i < len(w) {
			ws = w[i]
		}
		if gs != ws {
			return strings.Join([]string{
				"行 " + strconv.Itoa(i+1) + " が違う",
				"生成: " + gs,
				"正本: " + ws,
			}, "\n")
		}
	}
	return "行は同じだがバイト列が違う"
}
