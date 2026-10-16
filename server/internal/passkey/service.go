package passkey

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// DefaultName は登録時に名前を省略したときの名前。あとから変更できる。
const DefaultName = "パスキー"

var (
	// ErrInvalidName は名前が空、または長すぎること。
	ErrInvalidName = errors.New("passkey: 名前が正しくありません")
	// ErrVerificationFailed は端末の応答を検証できなかったこと。原因は区別して返さない。
	ErrVerificationFailed = errors.New("passkey: 認証情報を検証できません")
)

// Clock は現在時刻。テストで固定できるようにする。
type Clock interface {
	Now() time.Time
}

// Service はパスキーの登録・一覧・名前変更・削除の用途を実装する。
type Service struct {
	WebAuthn   *webauthn.WebAuthn
	Passkeys   Repository
	Challenges ChallengeRepository
	Clock      Clock
	// Logger はセキュリティ上の出来事と清掃失敗の記録用。nilなら標準のslogを使う。
	// 公開鍵・認証情報ID・チャレンジは渡さない。
	Logger *slog.Logger
	// Limiter はログイン失敗の回数制限(接続元IPごと)。nilなら制限しない。
	Limiter IPLimiter
}

func (s Service) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// newUser は検証器に渡す利用者を作る。ハンドルは保存済みを使い、無ければ乱数を保存する。
func (s Service) newUser(ctx context.Context, userID int64, username string) (User, error) {
	fresh, err := NewUserHandle()
	if err != nil {
		return User{}, err
	}
	handle, err := s.Passkeys.UserHandle(ctx, userID, fresh)
	if err != nil {
		return User{}, err
	}
	creds, err := s.Passkeys.ListByUser(ctx, userID)
	if err != nil {
		return User{}, err
	}
	return User{Handle: handle, Username: username, Credentials: creds}, nil
}

// BeginRegistration は登録の開始。ブラウザへ渡す平文のチャレンジトークンと、端末へ渡す設定を返す。
// 登録済みの認証情報は excludeCredentials に入れ、同じ認証器の重複登録を防ぐ。
func (s Service) BeginRegistration(ctx context.Context, userID int64, username string) (string, *protocol.CredentialCreation, error) {
	user, err := s.newUser(ctx, userID, username)
	if err != nil {
		return "", nil, err
	}
	if len(user.Credentials) >= MaxPasskeysPerUser {
		return "", nil, ErrLimitReached
	}
	exclusions := webauthn.Credentials(user.WebAuthnCredentials()).CredentialDescriptors()
	creation, session, err := s.WebAuthn.BeginRegistration(user, webauthn.WithExclusions(exclusions))
	if err != nil {
		return "", nil, errors.New("passkey: 登録を開始できません")
	}
	data, err := json.Marshal(session)
	if err != nil {
		return "", nil, errors.New("passkey: 登録を開始できません")
	}
	token, id, err := NewChallengeToken()
	if err != nil {
		return "", nil, err
	}
	now := s.Clock.Now()
	if err := s.Challenges.DeleteExpired(ctx, now); err != nil {
		// 清掃だけの失敗で登録は止めない
		s.logger().Warn("expired passkey challenge cleanup failed")
	}
	if err := s.Challenges.Create(ctx, id, PurposeRegister, &userID, data, now.Add(ChallengeTTL)); err != nil {
		return "", nil, err
	}
	return token, creation, nil
}

// FinishRegistration は登録の完了。チャレンジは検証の成否にかかわらず1回で使い切る。
// credentialJSON はブラウザが返した PublicKeyCredential のJSON表現。
func (s Service) FinishRegistration(ctx context.Context, userID int64, username, token, name string, credentialJSON []byte) (Credential, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = DefaultName
	}
	if err := ValidateName(name); err != nil {
		return Credential{}, ErrInvalidName
	}
	if token == "" {
		return Credential{}, ErrChallengeInvalid
	}
	challenge, err := s.Challenges.Consume(ctx, HashChallengeToken(token), PurposeRegister, s.Clock.Now())
	if err != nil {
		return Credential{}, err
	}
	// 別の利用者が開始したチャレンジは、使わせない
	if challenge.UserID == nil || *challenge.UserID != userID {
		return Credential{}, ErrChallengeInvalid
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(challenge.SessionData, &session); err != nil {
		return Credential{}, ErrChallengeInvalid
	}
	user, err := s.newUser(ctx, userID, username)
	if err != nil {
		return Credential{}, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(credentialJSON)
	if err != nil {
		s.logger().Warn("passkey registration rejected", "event", "passkey_register_failed", "user_id", userID)
		return Credential{}, ErrVerificationFailed
	}
	created, err := s.WebAuthn.CreateCredential(user, session, parsed)
	if err != nil {
		s.logger().Warn("passkey registration rejected", "event", "passkey_register_failed", "user_id", userID)
		return Credential{}, ErrVerificationFailed
	}
	saved, err := s.Passkeys.Create(ctx, FromWebAuthn(userID, name, created))
	if err != nil {
		return Credential{}, err
	}
	s.logger().Info("passkey registered", "event", "passkey_registered", "user_id", userID, "passkey_id", saved.ID)
	return saved, nil
}

// BeginLogin は利用者名なし(discoverable)のログインの開始。ブラウザへ渡す平文のチャレンジトークンと、端末へ渡す設定を返す。
// 接続元IPが締め出し中、または開始の回数が上限なら *auth.TooManyAttemptsError を返す。
// 開始は失敗として数えない(正規の利用者を締め出さないため)が、DBへ書くので、失敗とは別の枠でIPごとに数える。
func (s Service) BeginLogin(ctx context.Context, ip string) (string, *protocol.CredentialAssertion, error) {
	if s.Limiter != nil {
		if err := s.Limiter.CheckIP(ip); err != nil {
			return "", nil, err
		}
		if err := s.Limiter.AllowBegin(ip); err != nil {
			return "", nil, err
		}
	}
	assertion, session, err := s.WebAuthn.BeginDiscoverableLogin()
	if err != nil {
		return "", nil, errors.New("passkey: ログインを開始できません")
	}
	data, err := json.Marshal(session)
	if err != nil {
		return "", nil, errors.New("passkey: ログインを開始できません")
	}
	token, id, err := NewChallengeToken()
	if err != nil {
		return "", nil, err
	}
	now := s.Clock.Now()
	if err := s.Challenges.DeleteExpired(ctx, now); err != nil {
		// 清掃だけの失敗でログインは止めない
		s.logger().Warn("expired passkey challenge cleanup failed")
	}
	if err := s.Challenges.Create(ctx, id, PurposeLogin, nil, data, now.Add(ChallengeTTL)); err != nil {
		return "", nil, err
	}
	return token, assertion, nil
}

// errUnknownCredential は、検証器へ渡す利用者の検索で、認証情報や利用者が見つからなかったこと。
var errUnknownCredential = errors.New("passkey: 認証情報が見つかりません")

// FinishLogin は利用者名なしのログインの完了。検証に成功した利用者のIDを返し、セッションは発行しない
// (呼び出し側が auth.Service.StartSession を呼ぶ)。チャレンジは検証の成否にかかわらず1回で使い切る。
//
// 失敗はすべて ErrLoginFailed にまとめ、IPごとの失敗として数える。締め出し中は *auth.TooManyAttemptsError
// (チャレンジは消費しない)。DBの障害は数えず、そのまま返す。
func (s Service) FinishLogin(ctx context.Context, ip, token string, credentialJSON []byte) (int64, error) {
	if s.Limiter != nil {
		if err := s.Limiter.CheckIP(ip); err != nil {
			return 0, err
		}
	}
	if token == "" {
		return 0, s.rejectLogin(ip, "challenge")
	}
	challenge, err := s.Challenges.Consume(ctx, HashChallengeToken(token), PurposeLogin, s.Clock.Now())
	if errors.Is(err, ErrChallengeInvalid) {
		return 0, s.rejectLogin(ip, "challenge")
	}
	if err != nil {
		return 0, err
	}
	var session webauthn.SessionData
	// ログインのチャレンジは利用者に結び付けない。結び付いているものは、使わせない
	if err := json.Unmarshal(challenge.SessionData, &session); err != nil || len(session.UserID) != 0 || challenge.UserID != nil {
		return 0, s.rejectLogin(ip, "challenge")
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(credentialJSON)
	if err != nil {
		return 0, s.rejectLogin(ip, "parse")
	}

	var found Credential
	var lookupErr error
	lookup := func(rawID, userHandle []byte) (webauthn.User, error) {
		cred, err := s.Passkeys.FindByCredentialID(ctx, rawID)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				lookupErr = err
			}
			return nil, errUnknownCredential
		}
		owner, err := s.Passkeys.UserIDByHandle(ctx, userHandle)
		if err != nil {
			if !errors.Is(err, ErrNotFound) {
				lookupErr = err
			}
			return nil, errUnknownCredential
		}
		// 別の利用者のハンドルへ差し替えられていないか
		if owner != cred.UserID {
			return nil, errUnknownCredential
		}
		found = cred
		return User{Handle: userHandle, Credentials: []Credential{cred}}, nil
	}
	_, verified, err := s.WebAuthn.ValidatePasskeyLogin(lookup, session, parsed)
	if lookupErr != nil {
		return 0, lookupErr
	}
	if err != nil || verified == nil {
		return 0, s.rejectLogin(ip, "verification")
	}

	// 認証回数が増えていないものは、複製された認証器の疑いがある。ライブラリの判定(CloneWarning)に加えて、
	// 保存済みの値と受信した値を直接比べる。両方0(同期パスキー)だけは許す。
	received := parsed.Response.AuthenticatorData.Counter
	if verified.Authenticator.CloneWarning || ((found.SignCount > 0 || received > 0) && received <= found.SignCount) {
		s.logger().Warn("passkey sign count did not increase", "event", "passkey_sign_count_rollback",
			"user_id", found.UserID, "passkey_id", found.ID)
		return 0, s.rejectLogin(ip, "sign_count")
	}
	// 更新は「回数が増えたとき」だけ通る。同時ログインで先を越された場合や、削除された場合は ErrNotFound
	if err := s.Passkeys.RecordUse(ctx, found.CredentialID, received, verified.Flags.BackupState, s.Clock.Now()); err != nil {
		if errors.Is(err, ErrNotFound) {
			return 0, s.rejectLogin(ip, "record_conflict")
		}
		return 0, err
	}
	s.logger().Info("passkey login succeeded", "event", "passkey_login_succeeded", "user_id", found.UserID, "passkey_id", found.ID)
	return found.UserID, nil
}

// rejectLogin はログインの失敗を、IPごとの失敗として数え、原因の分類だけを記録して ErrLoginFailed を返す。
// reason は応答へ出さない。ライブラリのエラー文には、チャレンジやオリジンの値が入りうるため記録しない。
func (s Service) rejectLogin(ip, reason string) error {
	if s.Limiter != nil {
		s.Limiter.FailureIP(ip)
	}
	s.logger().Warn("passkey login rejected", "event", "passkey_login_failed", "reason", reason)
	return ErrLoginFailed
}

// List は利用者のパスキーを登録順に返す。
func (s Service) List(ctx context.Context, userID int64) ([]Credential, error) {
	return s.Passkeys.ListByUser(ctx, userID)
}

// Rename は本人のパスキーの名前を変える。他人のもの・無いものは ErrNotFound。
func (s Service) Rename(ctx context.Context, userID, id int64, name string) error {
	name = strings.TrimSpace(name)
	if err := ValidateName(name); err != nil {
		return ErrInvalidName
	}
	return s.Passkeys.Rename(ctx, userID, id, name)
}

// Delete は本人のパスキーを物理削除する。他人のもの・無いものは ErrNotFound。
func (s Service) Delete(ctx context.Context, userID, id int64) error {
	if err := s.Passkeys.Delete(ctx, userID, id); err != nil {
		return err
	}
	s.logger().Info("passkey deleted", "event", "passkey_deleted", "user_id", userID, "passkey_id", id)
	return nil
}
