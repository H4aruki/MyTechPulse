-- name: CreateUserTermsAgreement :exec
INSERT INTO user_terms_agreement ("user_ID", terms_version) VALUES ($1, $2);
