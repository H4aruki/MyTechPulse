package passkey

import (
	"errors"
	"strconv"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Config は検証器の設定。platform/config の値をそのまま渡す。
type Config struct {
	RPID          string
	RPDisplayName string
	Origins       []string
}

// NewWebAuthn は検証器を作る。端末の本人確認(userVerification)と、利用者名なしで選べるパスキー
// (discoverable credential)を必須にする。
func NewWebAuthn(cfg Config) (*webauthn.WebAuthn, error) {
	required := true
	w, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.RPID,
		RPDisplayName: cfg.RPDisplayName,
		RPOrigins:     cfg.Origins,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			RequireResidentKey: &required,
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   protocol.VerificationRequired,
		},
	})
	if err != nil {
		return nil, errors.New("passkey: WebAuthnの設定が不正です")
	}
	return w, nil
}

// User は webauthn.User を満たす。表示名は利用者名を使い、識別には Handle(乱数)だけを使う。
type User struct {
	Handle      []byte
	Username    string
	Credentials []Credential
}

var _ webauthn.User = User{}

func (u User) WebAuthnID() []byte          { return u.Handle }
func (u User) WebAuthnName() string        { return u.Username }
func (u User) WebAuthnDisplayName() string { return u.Username }

func (u User) WebAuthnCredentials() []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(u.Credentials))
	for _, c := range u.Credentials {
		out = append(out, c.ToWebAuthn())
	}
	return out
}

// ToWebAuthn はライブラリの検証に渡す形へ変える。
func (c Credential) ToWebAuthn() webauthn.Credential {
	transports := make([]protocol.AuthenticatorTransport, 0, len(c.Transports))
	for _, t := range c.Transports {
		transports = append(transports, protocol.AuthenticatorTransport(t))
	}
	return webauthn.Credential{
		ID:        c.CredentialID,
		PublicKey: c.PublicKey,
		Transport: transports,
		Flags: webauthn.CredentialFlags{
			UserPresent:    true,
			UserVerified:   true,
			BackupEligible: c.BackupEligible,
			BackupState:    c.BackupState,
		},
		Authenticator: webauthn.Authenticator{AAGUID: c.AAGUID, SignCount: c.SignCount},
	}
}

// FromWebAuthn はライブラリが作った認証情報を、保存する形へ変える。名前と利用者は呼び出し側が決める。
func FromWebAuthn(userID int64, name string, c *webauthn.Credential) Credential {
	transports := make([]string, 0, len(c.Transport))
	for _, t := range c.Transport {
		transports = append(transports, string(t))
	}
	aaguid := make([]byte, 16)
	copy(aaguid, c.Authenticator.AAGUID)
	return Credential{
		UserID:         userID,
		CredentialID:   c.ID,
		PublicKey:      c.PublicKey,
		SignCount:      c.Authenticator.SignCount,
		Transports:     transports,
		AAGUID:         aaguid,
		BackupEligible: c.Flags.BackupEligible,
		BackupState:    c.Flags.BackupState,
		Name:           name,
	}
}

// ValidateName は利用者が付ける名前を確かめる(空でなく、MaxNameLength文字以内)。
func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" || len([]rune(name)) > MaxNameLength {
		return errors.New("passkey: 名前は1〜" + strconv.Itoa(MaxNameLength) + "文字にしてください")
	}
	return nil
}
