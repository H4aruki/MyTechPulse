package passkey

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

func TestUserWebAuthnCredentialsMapsEveryStoredField(t *testing.T) {
	user := User{
		Handle:   bytes.Repeat([]byte{7}, UserHandleSize),
		Username: "alice",
		Credentials: []Credential{
			{
				ID: 11, UserID: 1, CredentialID: []byte{1, 1, 1}, PublicKey: []byte{2, 2},
				SignCount: 41, Transports: []string{"internal", "hybrid"}, AAGUID: bytes.Repeat([]byte{0xA1}, 16),
				BackupEligible: true, BackupState: true, Name: "iPhone",
			},
			{
				ID: 12, UserID: 1, CredentialID: []byte{9}, PublicKey: []byte{8, 8, 8},
				SignCount: 0, Transports: nil, AAGUID: bytes.Repeat([]byte{0xB2}, 16),
				BackupEligible: true, BackupState: false, Name: "Security key",
			},
			{
				ID: 13, UserID: 1, CredentialID: []byte{5, 6}, PublicKey: []byte{4},
				SignCount: 4294967295, Transports: []string{"usb"}, AAGUID: bytes.Repeat([]byte{0xC3}, 16),
				BackupEligible: false, BackupState: false, Name: "Old key",
			},
		},
	}

	got := user.WebAuthnCredentials()
	if len(got) != len(user.Credentials) {
		t.Fatalf("got %d credentials, want %d", len(got), len(user.Credentials))
	}

	want := []struct {
		id, key    []byte
		transport  []protocol.AuthenticatorTransport
		eligible   bool
		state      bool
		signCount  uint32
		aaguidByte byte
	}{
		{[]byte{1, 1, 1}, []byte{2, 2}, []protocol.AuthenticatorTransport{"internal", "hybrid"}, true, true, 41, 0xA1},
		{[]byte{9}, []byte{8, 8, 8}, []protocol.AuthenticatorTransport{}, true, false, 0, 0xB2},
		{[]byte{5, 6}, []byte{4}, []protocol.AuthenticatorTransport{"usb"}, false, false, 4294967295, 0xC3},
	}
	for i, w := range want {
		c := got[i]
		if !bytes.Equal(c.ID, w.id) || !bytes.Equal(c.PublicKey, w.key) {
			t.Errorf("#%d: id/key = %v / %v, want %v / %v", i, c.ID, c.PublicKey, w.id, w.key)
		}
		if !reflect.DeepEqual(c.Transport, w.transport) {
			t.Errorf("#%d: transport = %#v, want %#v", i, c.Transport, w.transport)
		}
		// 登録時に本人確認(UV)を必須にしているので、保存した認証情報は常にUP/UV済みとして渡す
		if !c.Flags.UserPresent || !c.Flags.UserVerified {
			t.Errorf("#%d: UP/UV must be set: %+v", i, c.Flags)
		}
		if c.Flags.BackupEligible != w.eligible || c.Flags.BackupState != w.state {
			t.Errorf("#%d: backup flags = %+v, want eligible=%v state=%v", i, c.Flags, w.eligible, w.state)
		}
		if c.Authenticator.SignCount != w.signCount {
			t.Errorf("#%d: sign count = %d, want %d", i, c.Authenticator.SignCount, w.signCount)
		}
		if !bytes.Equal(c.Authenticator.AAGUID, bytes.Repeat([]byte{w.aaguidByte}, 16)) {
			t.Errorf("#%d: aaguid = %x", i, c.Authenticator.AAGUID)
		}
	}
	// 順序は保存順のまま(検証器が返す順序と対応を取れるように)
	if got[0].Transport[0] != protocol.Internal || got[0].Transport[1] != protocol.Hybrid {
		t.Errorf("transport values must keep their order: %v", got[0].Transport)
	}
}

func TestUserWebAuthnCredentialsKeepsUnknownTransportValues(t *testing.T) {
	// 将来の値や未知の値は、落とさずそのまま検証器へ渡す
	user := User{Credentials: []Credential{{CredentialID: []byte{1}, Transports: []string{"nfc", "future-transport"}}}}
	got := user.WebAuthnCredentials()[0].Transport
	if !reflect.DeepEqual(got, []protocol.AuthenticatorTransport{protocol.NFC, "future-transport"}) {
		t.Fatalf("transport = %v", got)
	}
}

func TestUserWithoutCredentialsHasNoWebAuthnCredentials(t *testing.T) {
	for name, u := range map[string]User{"nil": {}, "empty": {Credentials: []Credential{}}} {
		if got := u.WebAuthnCredentials(); len(got) != 0 {
			t.Errorf("%s: got %d credentials, want 0", name, len(got))
		}
	}
}

func TestUserIdentifiesByRandomHandleAndUsesUsernameForNames(t *testing.T) {
	handle, err := NewUserHandle()
	if err != nil {
		t.Fatal(err)
	}
	u := User{Handle: handle, Username: "alice"}
	if !bytes.Equal(u.WebAuthnID(), handle) {
		t.Fatal("WebAuthnID must be the random handle")
	}
	if u.WebAuthnName() != "alice" || u.WebAuthnDisplayName() != "alice" {
		t.Fatalf("name = %q, display = %q", u.WebAuthnName(), u.WebAuthnDisplayName())
	}
	var _ webauthn.User = u
}

func TestNewWebAuthnPassesRelyingPartySettingsToTheVerifier(t *testing.T) {
	cfg := Config{RPID: "example.com", RPDisplayName: "MyTechPulse", Origins: []string{"https://example.com", "https://app.example.com"}}
	w, err := NewWebAuthn(cfg)
	if err != nil || w == nil {
		t.Fatalf("valid config must be accepted: %v", err)
	}
	// 複数のOriginが全部、検証器へ渡る
	if !reflect.DeepEqual(w.Config.RPOrigins, cfg.Origins) {
		t.Fatalf("origins = %v, want %v", w.Config.RPOrigins, cfg.Origins)
	}
	creation, _, err := w.BeginRegistration(User{Handle: bytes.Repeat([]byte{3}, UserHandleSize), Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	rp := creation.Response.RelyingParty
	if rp.ID != "example.com" || rp.Name != "MyTechPulse" {
		t.Fatalf("relying party = %+v", rp)
	}
	if creation.Response.User.Name != "alice" || creation.Response.User.DisplayName != "alice" {
		t.Fatalf("user entity = %+v", creation.Response.User)
	}
	// 利用者IDは乱数のハンドルで、利用者名や数値のIDではない
	if id, ok := creation.Response.User.ID.(protocol.URLEncodedBase64); !ok || !bytes.Equal(id, bytes.Repeat([]byte{3}, UserHandleSize)) {
		t.Fatalf("user id = %#v", creation.Response.User.ID)
	}
}

func TestNewWebAuthnRejectsMissingOriginsWithoutLeakingConfig(t *testing.T) {
	for name, origins := range map[string][]string{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			w, err := NewWebAuthn(Config{RPID: "secret-rp.example.com", RPDisplayName: "secret-name", Origins: origins})
			if err == nil {
				t.Fatal("want an error: the verifier cannot check any origin")
			}
			if w != nil {
				t.Fatal("no verifier may be returned with an error")
			}
			if strings.Contains(err.Error(), "secret-rp") || strings.Contains(err.Error(), "secret-name") {
				t.Fatalf("error leaks configuration: %v", err)
			}
		})
	}
}
