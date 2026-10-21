package auth

import (
	"strings"
	"testing"
)

func TestNormalizeTagsAcceptsRealTagsAndTrims(t *testing.T) {
	got, fe := normalizeTags([]string{" C++ ", "C#", "CI/CD", "自然言語処理（NLP）", "#Next.js", "Spring Boot", "c++"})
	if fe != nil {
		t.Fatalf("unexpected error: %+v", fe)
	}
	want := []string{"C++", "C#", "CI/CD", "自然言語処理（NLP）", "#Next.js", "Spring Boot"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNormalizeTagsRejectsHiddenAndUnusableCharacters(t *testing.T) {
	for name, tag := range map[string]string{
		"空":        "  ",
		"改行":       "go\nlang",
		"タブ":       "go\tlang",
		"ゼロ幅スペース":  "go​lang",
		"向きの制御":    "go‮lang",
		"連続した空白":   "Spring  Boot",
		"使えない記号":   "<script>",
		"51文字":     strings.Repeat("a", 51),
		"不正なUTF-8": "go\xff",
	} {
		_, fe := normalizeTags([]string{"go", tag})
		if fe == nil || fe.Field != "favorite_tags" || fe.Message != TagRuleMessage {
			t.Errorf("%s: fe = %+v", name, fe)
		}
	}
}

func TestSignupUsernameRejectsHiddenCharactersButLoginDoesNot(t *testing.T) {
	for _, name := range []string{"a​b", "a\nb", "a\tb", "‮ab", "a\x00"} {
		if _, fe := validateNewUsername(name); fe == nil || fe.Field != "username" {
			t.Errorf("signup %q: fe = %+v", name, fe)
		}
	}
	// ログインの検証は変えない(すでにある利用者名でログインできなくならないように)
	if _, fe := validateUsername("a​b"); fe != nil {
		t.Errorf("login must keep the old rule: %+v", fe)
	}
	for _, name := range []string{"synthetic-user", " 山田 太郎 ", strings.Repeat("あ", 50)} {
		if _, fe := validateNewUsername(name); fe != nil {
			t.Errorf("signup %q: %+v", name, fe)
		}
	}
	if _, fe := validateNewUsername(strings.Repeat("a", 51)); fe == nil {
		t.Error("51 chars must be rejected")
	}
}
