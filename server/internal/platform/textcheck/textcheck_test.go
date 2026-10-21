package textcheck_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/H4aruki/MyTechPulse/server/db/migrations"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/textcheck"
)

func TestValidTag(t *testing.T) {
	ok := []string{
		"C++", "C#", "CI/CD", "自然言語処理（NLP）", "#Next.js", "Spring Boot", "Next.js", "scikit-learn",
		"TDD（テスト駆動開発）", "C", "R", "a_b", "Q&A", "it's", "データ・分析", "Sass/SCSS", "ｱﾙｺﾞﾘｽﾞﾑ",
		"が", // 濁点が結合文字(NFD)
		strings.Repeat("あ", 50),
	}
	for _, tag := range ok {
		if !textcheck.ValidTag(tag) {
			t.Errorf("ValidTag(%q) = false, want true", tag)
		}
	}
	bad := map[string]string{
		"空":           "",
		"空白だけ":        " ",
		"前の空白":        " go",
		"後ろの空白":       "go ",
		"連続した空白":      "Spring  Boot",
		"全角の空白":       "Spring　Boot",
		"改行":          "go\nlang",
		"タブ":          "go\tlang",
		"CR":          "go\r",
		"NUL":         "go\x00",
		"DEL":         "go\x7f",
		"ゼロ幅スペース":     "go​lang",
		"ZWNJ":        "go‌lang",
		"ZWJ":         "go‍lang",
		"U+2060":      "go⁠lang",
		"BOM":         "\xef\xbb\xbfgo",
		"RLO":         "go‮lang",
		"LRE":         "‪go",
		"RLI":         "go⁧",
		"PDI":         "go⁩",
		"ソフトハイフン":     "go­lang",
		"点字の空白":       "go⠀",
		"ハングルのフィラー":   "goㅤ",
		"異体字セレクタ":     "go️",
		"タグ文字":        "go\U000e0041",
		"私用領域":        "go",
		"不正なUTF-8":    "go\xff",
		"結合文字だけ":      "゙",
		"空白の後の結合文字":   "go ゙",
		"絵文字":         "go🚀",
		"アットマーク":      "go@lang",
		"山括弧":         "<script>",
		"引用符":         `"go"`,
		"バックスラッシュ":    `go\lang`,
		"51文字":        strings.Repeat("あ", 51),
		"51文字(ASCII)": strings.Repeat("a", 51),
	}
	for name, tag := range bad {
		if textcheck.ValidTag(tag) {
			t.Errorf("%s: ValidTag(%q) = true, want false", name, tag)
		}
	}
}

func TestValidUsername(t *testing.T) {
	for _, name := range []string{"a", "synthetic-user", "山田 太郎", "user_01", "o'neil", strings.Repeat("あ", 50)} {
		if !textcheck.ValidUsername(name) {
			t.Errorf("ValidUsername(%q) = false, want true", name)
		}
	}
	for name, v := range map[string]string{
		"空":        "",
		"51文字":     strings.Repeat("a", 51),
		"改行":       "a\nb",
		"タブ":       "a\tb",
		"ゼロ幅スペース":  "a​b",
		"BOM":      "\xef\xbb\xbfa",
		"RLO":      "a‮b",
		"制御文字":     "a\x01",
		"不正なUTF-8": "a\xff",
		"点字の空白":    "a⠀",
	} {
		if textcheck.ValidUsername(v) {
			t.Errorf("%s: ValidUsername(%q) = true, want false", name, v)
		}
	}
}

// 画面の選択肢にもなる初期タグ100件は、すべて使えること。
func TestSeedSignupTagsAreValid(t *testing.T) {
	b, err := migrations.FS.ReadFile("00006_seed_signup_tags.sql")
	if err != nil {
		t.Fatal(err)
	}
	rule := regexp.MustCompile(`(?m)^\s*\('((?:[^']|'')*)'\),?\s*$`)
	n := 0
	for _, m := range rule.FindAllStringSubmatch(string(b), -1) {
		n++
		tag := strings.ReplaceAll(m[1], "''", "'")
		if !textcheck.ValidTag(tag) {
			t.Errorf("初期タグ %q が使えない形と判定された", tag)
		}
	}
	if n != 100 {
		t.Fatalf("初期タグの数 = %d, want 100", n)
	}
}
