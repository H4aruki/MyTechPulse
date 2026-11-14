// 利用規約とプライバシーポリシーの版（2つで1つの版）。会員登録の同意の記録に使う。
// サーバーの auth.CurrentTermsVersion（server/internal/auth/model.go）と同じ値にする。
// 本文を改定したときは、両方を同時に更新する（server/internal/auth/terms_version_test.go が一致を確かめる）。
export const LEGAL_VERSION = '2026-10-draft'
