package passkey

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
)

func TestConstants(t *testing.T) {
	if MaxPasskeysPerUser != 10 || ChallengeTTL.Minutes() != 5 || UserHandleSize != 32 {
		t.Fatal("constants changed")
	}
}

func TestNewUserHandleIsRandom32Bytes(t *testing.T) {
	a, err := NewUserHandle()
	if err != nil || len(a) != 32 {
		t.Fatalf("handle = %d bytes, err %v", len(a), err)
	}
	b, _ := NewUserHandle()
	if bytes.Equal(a, b) {
		t.Fatal("handles must differ")
	}
}

func TestChallengeTokenHashMatches(t *testing.T) {
	plain, id, err := NewChallengeToken()
	if err != nil || plain == "" {
		t.Fatal(err)
	}
	if HashChallengeToken(plain) != id {
		t.Fatal("hash must match token")
	}
	other, otherID, _ := NewChallengeToken()
	if other == plain || otherID == id {
		t.Fatal("tokens must differ")
	}
}

func TestNewWebAuthnRequiresUserVerificationAndDiscoverable(t *testing.T) {
	w, err := NewWebAuthn(Config{RPID: "localhost", RPDisplayName: "MyTechPulse", Origins: []string{"http://localhost:5173"}})
	if err != nil {
		t.Fatal(err)
	}
	user := User{Handle: bytes.Repeat([]byte{1}, 32), Username: "alice"}
	creation, _, err := w.BeginRegistration(user)
	if err != nil {
		t.Fatal(err)
	}
	sel := creation.Response.AuthenticatorSelection
	if sel.UserVerification != protocol.VerificationRequired || sel.ResidentKey != protocol.ResidentKeyRequirementRequired ||
		sel.RequireResidentKey == nil || !*sel.RequireResidentKey {
		t.Fatalf("unexpected selection: %+v", sel)
	}
	assertion, _, err := w.BeginDiscoverableLogin()
	if err != nil {
		t.Fatal(err)
	}
	if assertion.Response.UserVerification != protocol.VerificationRequired || len(assertion.Response.AllowedCredentials) != 0 {
		t.Fatalf("unexpected assertion options: %+v", assertion.Response)
	}
}

func TestNewWebAuthnRejectsBadConfig(t *testing.T) {
	if _, err := NewWebAuthn(Config{RPID: "", RPDisplayName: "x", Origins: nil}); err == nil {
		t.Fatal("expected error")
	}
}

func TestCredentialRoundTrip(t *testing.T) {
	c := Credential{
		CredentialID: []byte{1, 2}, PublicKey: []byte{3}, SignCount: 7, Transports: []string{"internal", "hybrid"},
		AAGUID: bytes.Repeat([]byte{9}, 16), BackupEligible: true, BackupState: true,
	}
	w := c.ToWebAuthn()
	back := FromWebAuthn(5, "iPhone", &w)
	if back.UserID != 5 || back.Name != "iPhone" || !bytes.Equal(back.CredentialID, c.CredentialID) || back.SignCount != 7 ||
		!back.BackupEligible || !back.BackupState || len(back.Transports) != 2 || len(back.AAGUID) != 16 {
		t.Fatalf("round trip mismatch: %+v", back)
	}
	empty := webauthnCredentialWithoutAAGUID()
	if got := FromWebAuthn(1, "x", &empty); len(got.AAGUID) != 16 {
		t.Fatalf("aaguid must be padded to 16 bytes, got %d", len(got.AAGUID))
	}
}

// 目的: 制御文字・書式文字・見えない文字だけの名前を拒否し、日本語・絵文字(ZWJ・VS16を含む)の正当な名前は通すこと。
// 拒否の側は、長さ超過の2件を除いてすべてMaxNameLength文字以内。長さ超過とは別の理由で落ちることを確かめる。
// 見えない文字はソース上で区別できないので、すべて \U0000XXXX の形で書く。
func TestValidateName(t *testing.T) {
	const (
		zwjStr = "\U0000200D"
		vs16   = "\U0000FE0F"
		man    = "\U0001F468"
		woman  = "\U0001F469"
	)
	reject := map[string]string{
		"空":                 "",
		"半角の空白だけ":           "   ",
		"長さ超過":              strings.Repeat("あ", MaxNameLength+1),
		"長さ超過(絵文字)":         strings.Repeat("🔑", MaxNameLength+1),
		"NUL":               "iPhone\x00",
		"NULが途中":            "i\x00Phone",
		"改行":                "iPh\none",
		"タブ":                "iPh\tone",
		"ESC":               "iPh\x1bone",
		"DEL":               "iPh\x7fone",
		"C1(U+0085)":        "iPh\U00000085one",
		"RLO(U+202E)":       "iPhone\U0000202Egpj.exe",
		"ゼロ幅空白(U+200B)":     "iPh\U0000200Bone",
		"ゼロ幅空白だけ":           "\U0000200B",
		"ゼロ幅空白が並ぶだけ":        "\U0000200B\U0000200B\U0000200B",
		"空白+ゼロ幅空白":          " \U0000200B ",
		"全角空白+ゼロ幅空白":        "\U00003000\U0000200B",
		"ゼロ幅非接合子(U+200C)":   "iPh\U0000200Cone",
		"語結合子(U+2060)":      "iPh\U00002060one",
		"BOM(U+FEFF)":       "\U0000FEFFiPhone",
		"NBSPだけ":            "\U000000A0",
		"NBSPが並ぶだけ":         "\U000000A0\U000000A0",
		"全角空白だけ":            "\U00003000",
		"全角空白+半角空白だけ":       "\U00003000 \U00003000",
		"VS16だけ":            vs16,
		"空白+VS16":           " " + vs16,
		"結合記号だけ":            "\U00000301",
		"不正なUTF-8(単独の0xFF)": "iPhone\xff",
		"不正なUTF-8(途中で切れた3バイト)":   "iPh\xe3\x81one",
		"不正なUTF-8だけ":             "\xc3\x28",
		"ZWJが文字の間":               "a" + zwjStr + "b",
		"ZWJが先頭":                 zwjStr + woman,
		"ZWJが末尾":                 man + zwjStr,
		"ZWJの左だけ絵文字":             man + zwjStr + "a",
		"ZWJの右だけ絵文字":             "a" + zwjStr + woman,
		"ZWJだけ":                  zwjStr,
		"ZWJが連続":                 man + zwjStr + zwjStr + woman,
		"タグ文字(旗の連結)":             "\U0001F3F4\U000E0067\U000E0062\U000E007F",
		"行区切り(U+2028)が途中":        "iPh\U00002028one",
		"行区切り(U+2028)だけ":         "\U00002028",
		"段落区切り(U+2029)が途中":       "iPh\U00002029one",
		"段落区切り(U+2029)だけ":        "\U00002029",
		"私用領域(U+E000)が途中":        "iPh\U0000E000one",
		"私用領域(U+E000)だけ":         "\U0000E000",
		"未割り当て(U+FFF0)が途中":       "iPh\U0000FFF0one",
		"未割り当て(U+FFF0)だけ":        "\U0000FFF0",
		"ハングルフィラー(U+3164)が途中":    "iPh\U00003164one",
		"ハングルフィラー(U+3164)だけ":     "\U00003164",
		"半角ハングルフィラー(U+FFA0)が途中":  "iPh\U0000FFA0one",
		"半角ハングルフィラー(U+FFA0)だけ":   "\U0000FFA0",
		"点字の空白(U+2800)が途中":       "iPh\U00002800one",
		"点字の空白(U+2800)だけ":        "\U00002800",
		"結合用書記素ジョイナー(U+034F)が途中": "iPh\U0000034Fone",
		"結合用書記素ジョイナー(U+034F)だけ":  "\U0000034F",
		"異体字選択子(U+FE00)が途中":      "iPh\U0000FE00one",
		"異体字選択子(U+FE00)だけ":       "\U0000FE00",
		"文字の直後のVS16が途中":          "iPh" + "a" + vs16 + "one",
		"文字の直後のVS16":             "a" + vs16,
		"異体字選択子(U+E0100)が途中":     "iPh\U000E0100one",
		"異体字選択子(U+E0100)だけ":      "\U000E0100",
	}
	for name, bad := range reject {
		if n := len([]rune(bad)); n > MaxNameLength && name != "長さ超過" && name != "長さ超過(絵文字)" {
			t.Errorf("%s: 長さ超過とは別の理由で落ちる入力のはずが、%d文字ある", name, n)
		}
		if err := ValidateName(bad); err == nil {
			t.Errorf("%s: %q が通った", name, bad)
		}
	}

	accept := map[string]string{
		"英数":               "iPhone 15",
		"日本語":              "仕事用のiPhone",
		"記号":               "Work-PC_01 (青)",
		"鍵の絵文字":            "🔑",
		"絵文字と文字":           "🔑 仕事用",
		"ハート(VS16)":        "\U00002764" + vs16,
		"家族(ZWJ)":          man + zwjStr + woman + zwjStr + "\U0001F467",
		"肌色+職業(ZWJ)":       woman + "\U0001F3FD" + zwjStr + "\U0001F4BB",
		"性別(ZWJ+VS16)":     "\U0001F3C3" + zwjStr + "\U00002642" + vs16,
		"燃えるハート(VS16+ZWJ)": "\U00002764" + vs16 + zwjStr + "\U0001F525",
		"虹の旗(VS16+ZWJ)":    "\U0001F3F3" + vs16 + zwjStr + "\U0001F308",
		"家族の絵文字と文字":        man + zwjStr + woman + zwjStr + "\U0001F467 家",
		"前後の全角空白":          "\U00003000仕事用\U00003000",
		"前後のNBSP":          "\U000000A0仕事用\U000000A0",
		"前後の半角空白":          "  iPhone  ",
		"上限ちょうど(日本語)":      strings.Repeat("あ", MaxNameLength),
		"上限ちょうど(絵文字)":      strings.Repeat("🔑", MaxNameLength),
		"上限ちょうど(英数)":       strings.Repeat("x", MaxNameLength),
	}
	for name, ok := range accept {
		if err := ValidateName(ok); err != nil {
			t.Errorf("%s: %q が拒否された: %v", name, ok, err)
		}
	}
}
