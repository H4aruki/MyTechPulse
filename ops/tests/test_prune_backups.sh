#!/usr/bin/env bash
# ops/prune_backups.sh の試験（Dockerもデータベースも使わない）
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
script="$repo_root/ops/prune_backups.sh"
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

make_file() {
  # make_file NAME DAYS_AGO
  printf 'x' >"$work/$1"
  touch -d "$2 days ago" "$work/$1"
}

exists() { [ -e "$work/$1" ]; }

# --- 古いものだけが消え、新しいもの・無関係なもの・.keep付きは残る ---
make_file mytechpulse_old.dump 10
make_file mytechpulse_old.dump.sha256 10
make_file mytechpulse_new.dump 1
make_file mytechpulse_new.dump.sha256 1
make_file mytechpulse_kept.dump 30
make_file mytechpulse_kept.dump.sha256 30
make_file mytechpulse_kept.dump.keep 30
make_file notes.txt 30
make_file other_old.dump 30
mkdir "$work/mytechpulse_dir.dump"
touch -d "30 days ago" "$work/mytechpulse_dir.dump"

stdout="$("$script" "$work" 7 2>/dev/null)"
[ -z "$stdout" ] || fail "標準出力に何か出している"

exists mytechpulse_old.dump && fail "古いダンプが残っている"
exists mytechpulse_old.dump.sha256 && fail "古いチェックサムが残っている"
exists mytechpulse_new.dump || fail "新しいダンプが消えた"
exists mytechpulse_new.dump.sha256 || fail "新しいチェックサムが消えた"
exists mytechpulse_kept.dump || fail ".keep付きのダンプが消えた"
exists mytechpulse_kept.dump.sha256 || fail ".keep付きのチェックサムが消えた"
exists mytechpulse_kept.dump.keep || fail ".keepが消えた"
exists notes.txt || fail "無関係なファイルが消えた"
exists other_old.dump || fail "名前の違うファイルが消えた"
[ -d "$work/mytechpulse_dir.dump" ] || fail "directoryが消えた"
echo "ok: 古い世代だけが消える"

# --- 新しいバックアップが1つも無いときは、何も消さない ---
rm -rf -- "$work"/*
make_file mytechpulse_a.dump 10
make_file mytechpulse_b.dump 20
"$script" "$work" 7 2>/dev/null
exists mytechpulse_a.dump && exists mytechpulse_b.dump || fail "新しいバックアップが無いのに消した"
echo "ok: 新しいバックアップが無いときは消さない"

# --- 境目（ちょうど残す日数以内）は残す ---
rm -rf -- "$work"/*
make_file mytechpulse_edge.dump 6
make_file mytechpulse_old.dump 8
"$script" "$work" 7 2>/dev/null
exists mytechpulse_edge.dump || fail "残す日数以内のものを消した"
exists mytechpulse_old.dump && fail "残す日数を超えたものが残っている"
echo "ok: 境目の扱い"

# --- 入力の拒否 ---
if "$script" "$work" 0 2>/dev/null; then fail "0日を受け付けた"; fi
if "$script" "$work" abc 2>/dev/null; then fail "数字でない日数を受け付けた"; fi
if "$script" "$work/none" 7 2>/dev/null; then fail "存在しないdirectoryを受け付けた"; fi
if "$script" 2>/dev/null; then fail "引数なしを受け付けた"; fi
echo "ok: 入力の拒否"

echo "OK: prune_backups の試験にすべて成功しました"
