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

func TestValidateName(t *testing.T) {
	for _, bad := range []string{"", "   ", strings.Repeat("あ", MaxNameLength+1)} {
		if ValidateName(bad) == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
	for _, ok := range []string{"iPhone", strings.Repeat("あ", MaxNameLength)} {
		if err := ValidateName(ok); err != nil {
			t.Fatalf("unexpected error for %q: %v", ok, err)
		}
	}
}
