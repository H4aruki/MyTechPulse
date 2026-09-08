---
name: prepare-pr
description: Prepares a MyTechPulse pull request with repository-compliant commits and plain Japanese descriptions. Use when asked to organize commits, push a work branch, or create a PR.
---

# PRを準備する

1. `git status --short --branch` で `main` ではないことと未コミット変更を確認する。
2. `git diff` と `git log` を確認し、利用者の無関係な変更を混ぜない。
3. 1機能1コミットになるよう分け、`CONTRIBUTING.md` のConventional Commitsに従う。
4. `verify-change` の手順で検証する。
5. PRのタイトルと本文を平易な日本語で作り、目的、変更内容、確認結果、残課題を書く。
6. 指示範囲なら作業ブランチへのpushとPR作成まで行える。PRの承認・マージと `main` へのpushは行わない。
