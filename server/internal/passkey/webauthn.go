package passkey

import (
	"errors"
	"strconv"
	"unicode"
	"unicode/utf8"

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

// ValidateName は利用者が付ける名前を確かめる。許可リストに近い方針で、次のどれかなら拒否する。
//   - 不正なUTF-8
//   - MaxNameLength文字(rune数)を超える
//   - 画面に出せる文字(unicode.IsGraphic: 文字・結合記号・数字・記号・句読点・空白)ではないもの。
//     制御文字(Cc。NUL・改行・タブ・ESC・DEL・C1)、書式文字(Cf。U+202Eの右から左への上書き、U+200Bのゼロ幅空白など)、
//     行区切り・段落区切り(Zl U+2028、Zp U+2029)、私用領域(Co)、未割り当て(Cn)、サロゲート(Cs)をまとめて落とす
//   - 見えないのに文字や記号に分類される文字。Other_Default_Ignorable_Code_Point(ハングルのフィラーU+3164・U+FFA0・
//     U+115F・U+1160、結合用のU+034F、U+17B4・U+17B5など)と、点字の空白U+2800
//   - 異体字選択子(Variation_Selector)。絵文字の直後のVS16(U+FE0F)だけ許す
//   - 見える文字(文字・数字・記号・句読点)が1つもない(空白だけ、結合記号だけ)
//
// ZWJ(U+200D)はCfだが、家族や職業の絵文字(人+ZWJ+人など)の連結に使われる。そのため「直前(VS16を除く)と
// 直後がどちらも絵文字」のときだけ許す。VS16はハートなどの絵文字表示に要るので、直前が絵文字のときだけ許す。
// タグ文字(U+E0020〜)を使うイングランドなどの旗はCfなので拒否する。
func ValidateName(name string) error {
	if !validName(name) {
		return errors.New("passkey: 名前は1〜" + strconv.Itoa(MaxNameLength) + "文字にしてください")
	}
	return nil
}

const (
	zwj          = '\U0000200D'
	vs16         = '\U0000FE0F'
	brailleBlank = '\U00002800'
)

func validName(name string) bool {
	// 不正なバイト列は []rune にすると U+FFFD になって見分けがつかないので、先に調べる
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > MaxNameLength {
		return false
	}
	runes := []rune(name)
	visible := false
	for i, r := range runes {
		switch {
		case r == zwj:
			if !joinsEmoji(runes, i) {
				return false
			}
		case !unicode.IsGraphic(r):
			return false
		case r == brailleBlank || unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r):
			return false
		case unicode.Is(unicode.Variation_Selector, r):
			if r != vs16 || i == 0 || !isPictographic(runes[i-1]) {
				return false
			}
		case unicode.In(r, unicode.L, unicode.N, unicode.P, unicode.S):
			visible = true
		}
	}
	return visible
}

// joinsEmoji は、runes[i](ZWJ)の両側が絵文字かを返す。左側はVS16を1つ読み飛ばす(燃えるハート)。
func joinsEmoji(runes []rune, i int) bool {
	left := i - 1
	if left >= 0 && runes[left] == vs16 {
		left--
	}
	return left >= 0 && i+1 < len(runes) && isPictographic(runes[left]) && isPictographic(runes[i+1])
}

// isPictographic は絵文字として使われる主な範囲か。Goの標準の表にはExtended_Pictographicが無く、
// 外部ライブラリは足さない方針なので、ZWJで連結される絵文字(人・家族・性別・職業・旗・ハート等)を覆う範囲で近似する。
func isPictographic(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF: // 絵文字の本体、肌色の修飾子を含む
		return true
	case r >= 0x2300 && r <= 0x23FF: // 時計・手など
		return true
	case r >= 0x2600 && r <= 0x27BF: // ❤ ♂ ♀ ⚕ ☠ など
		return true
	case r >= 0x2B00 && r <= 0x2BFF: // ⬛ ⭐ など
		return true
	}
	return false
}
