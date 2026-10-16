package passkey

import "github.com/go-webauthn/webauthn/webauthn"

func webauthnCredentialWithoutAAGUID() webauthn.Credential {
	return webauthn.Credential{ID: []byte{1}, PublicKey: []byte{2}}
}
