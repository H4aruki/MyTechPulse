#!/usr/bin/env bash
# CA bundle に、Mozilla が不信任にした root「emSign ECC Root CA - G3」
# （eMudhra、Mozilla bug 2083449）が含まれていないことを確かめる。
# 使い方: check-ca-bundle.sh <bundleファイル>
# 証明書の本文は出力しない。
set -euo pipefail

if [ "$#" -ne 1 ] || [ -z "$1" ]; then
  echo "使い方: $0 <bundleファイル>" >&2
  exit 2
fi
BUNDLE="$1"

# この証明書のPEM本文の1行目（これで識別する）
DISTRUSTED_LINE="MIICTjCCAdOgAwIBAgIKPPYHqWhwDtqLhDAKBggqhkjOPQQDAzBrMQswCQYDVQQG"

if [ ! -f "$BUNDLE" ] || [ ! -r "$BUNDLE" ]; then
  echo "CA bundleが空か読めません: $BUNDLE" >&2
  exit 1
fi

# 一致が0件でもgrepの終了コード1で止まらないよう、|| true を付ける。
# CRLFでも数えられるよう、行全体の一致(-x)は使わない。
total="$(grep -c -F -- "-----BEGIN CERTIFICATE-----" "$BUNDLE" || true)"
if [ "$total" -eq 0 ]; then
  echo "CA bundleが空か読めません: $BUNDLE" >&2
  exit 1
fi

found="$(grep -c -F -- "$DISTRUSTED_LINE" "$BUNDLE" || true)"
if [ "$found" -ge 1 ]; then
  echo "不信任の root（emSign ECC Root CA - G3）が ${found} 件含まれています" >&2
  exit 1
fi
echo "  不信任の root（emSign ECC Root CA - G3）: 0件（証明書 ${total} 件）"
