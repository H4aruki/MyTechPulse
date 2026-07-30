#!/usr/bin/env bash
# Oracle Cloud Always Free VM（Ampere A1 / Ubuntu 24.04 aarch64 想定）の初期セットアップ。
# VMにSSHしたあと、root権限で1回実行すれば本番実行環境の土台が整う:
#   sudo ./ops/oracle-vm-setup.sh
#
# 何度実行しても同じ結果になるよう冪等に書いてある。失敗して途中で止まった場合は
# 原因を直してそのまま再実行すればよい。
#
# このスクリプトでやらないこと（コンソール側のオーナー作業）:
#   - VCNのSecurity List / NSG でのingress開放（22 / 80 / 443）
#   - インスタンス作成そのもの
#   詳細な手順は docs/deploy/oracle-vm-provisioning.md を参照。
set -euo pipefail

# ---- 設定（環境変数で上書き可能） ----
TIMEZONE="${TIMEZONE:-Asia/Tokyo}"
# dockerグループに追加するユーザー。sudo実行なら呼び出し元ユーザー、無ければ
# Oracleのubuntuイメージの既定ユーザー名を使う
DOCKER_USER="${DOCKER_USER:-${SUDO_USER:-ubuntu}}"
# 追加で開放するTCPポート。22はイメージ既定で開いているので含めない
OPEN_TCP_PORTS="${OPEN_TCP_PORTS:-80 443}"
SSHD_DROPIN="/etc/ssh/sshd_config.d/99-mytechpulse-hardening.conf"
FAIL2BAN_JAIL="/etc/fail2ban/jail.d/sshd.local"

# dockerグループを追加した場合に「再ログインが必要」と最後に案内するためのフラグ
NEED_RELOGIN=0

export DEBIAN_FRONTEND=noninteractive

log() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33mWARN: %s\033[0m\n' "$*" >&2; }

# ---- 0. 前提チェック ----
if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: root権限で実行してください: sudo $0" >&2
    exit 1
fi

# apt リポジトリのパスがディストリ依存なのでUbuntu以外は弾く
# shellcheck disable=SC1091
. /etc/os-release
if [ "${ID:-}" != "ubuntu" ]; then
    echo "ERROR: Ubuntu専用です（検出: ${PRETTY_NAME:-unknown}）" >&2
    exit 1
fi

DPKG_ARCH="$(dpkg --print-architecture)"
if [ "$DPKG_ARCH" != "arm64" ]; then
    # Always Free には amd64 の VM.Standard.E2.1.Micro もあるため警告のみで続行する
    warn "arm64以外のアーキテクチャです（$DPKG_ARCH）。Ampere A1ではない可能性があります"
fi

log "Ubuntu ${VERSION_ID:-?} / ${DPKG_ARCH} でセットアップを開始"

# ---- 1. 基本パッケージ ----
log "aptパッケージを更新して基本パッケージを導入"
apt-get update -qq
# python3-systemd は fail2ban の systemd バックエンド（後述）に必要
apt-get install -y -qq ca-certificates curl gnupg git fail2ban python3-systemd

# Oracleのubuntuイメージは netfilter-persistent を同梱しているが、無い場合は入れる。
# 対話プロンプト（現在のルールを保存するか）は noninteractive で既定のyesになる
if ! command -v netfilter-persistent >/dev/null 2>&1; then
    log "iptables-persistent を導入（iptablesルールの永続化用）"
    apt-get install -y -qq iptables-persistent
fi

# ---- 2. Docker Engine + Compose plugin ----
if command -v docker >/dev/null 2>&1; then
    log "Docker は既に導入済みのためスキップ"
else
    log "Docker公式aptリポジトリから Docker Engine + Compose plugin を導入"
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc
    # arch を明示しないと arm64 環境で amd64 のパッケージを探しに行って失敗する
    cat > /etc/apt/sources.list.d/docker.list <<EOF
deb [arch=${DPKG_ARCH} signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu ${VERSION_CODENAME} stable
EOF
    apt-get update -qq
    apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
fi

# snap版dockerなどsystemdユニット名が違うケースでも止まらないようにする
systemctl enable --now docker || warn "docker.serviceの有効化に失敗しました（導入形態を確認してください）"

# sudo なしで docker コマンドを使えるようにする（反映は再ログイン後）
if id -u "$DOCKER_USER" >/dev/null 2>&1; then
    if id -nG "$DOCKER_USER" | tr ' ' '\n' | grep -qx docker; then
        log "ユーザー $DOCKER_USER は既にdockerグループに所属"
    else
        log "ユーザー $DOCKER_USER をdockerグループに追加"
        usermod -aG docker "$DOCKER_USER"
        NEED_RELOGIN=1
    fi
else
    warn "ユーザー $DOCKER_USER が存在しないためdockerグループ追加をスキップ（DOCKER_USER= で指定可）"
fi

# ---- 3. ファイアウォール（最大の落とし穴） ----
# OracleのUbuntuイメージは iptables に「これ以降を全部REJECTする」ルールを最後に持つ。
# Security Listでポートを開けてもここで落ちるので、REJECTより前にACCEPTを挿入する必要がある。
# ufw は使わない（同じiptablesを二重管理すると事故るため。公式もrules.v4編集を案内している）
if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q "Status: active"; then
    warn "ufwが有効です。iptablesを直接編集する本スクリプトと二重管理になるため設定を見直してください"
fi

log "iptables のINPUTチェインに ${OPEN_TCP_PORTS} のACCEPTを挿入"
IPTABLES_CHANGED=0
for port in $OPEN_TCP_PORTS; do
    if iptables -C INPUT -p tcp --dport "$port" -m state --state NEW -j ACCEPT 2>/dev/null; then
        echo "  - tcp/$port: 既にACCEPTルールあり"
        continue
    fi
    # 最初のREJECT/DROPの行番号を探し、その手前に挿入する。
    # iptables -L の列は: num target prot opt source destination
    reject_line="$(iptables -L INPUT --line-numbers -n | awk '$2 == "REJECT" || $2 == "DROP" { print $1; exit }')"
    if [ -n "$reject_line" ]; then
        iptables -I INPUT "$reject_line" -p tcp --dport "$port" -m state --state NEW -j ACCEPT
        echo "  - tcp/$port: ${reject_line}行目（REJECTの手前）に挿入"
    else
        iptables -A INPUT -p tcp --dport "$port" -m state --state NEW -j ACCEPT
        echo "  - tcp/$port: REJECTルールが無いため末尾に追加"
    fi
    IPTABLES_CHANGED=1
done

if [ "$IPTABLES_CHANGED" -eq 1 ]; then
    log "iptablesルールを永続化（再起動後も有効にする）"
    netfilter-persistent save
fi

# ---- 4. SSH硬化 ----
# Oracleのイメージは既定で公開鍵認証のみだが、明示的に固定して意図を残す。
# sshd_config は先に読まれた値が勝つため、Include されるドロップインで上書きする
if grep -qs "^Include /etc/ssh/sshd_config.d/\*.conf" /etc/ssh/sshd_config; then
    log "SSH設定を硬化（パスワード認証・root ログインを無効化）"
    install -m 0755 -d /etc/ssh/sshd_config.d
    cat > "$SSHD_DROPIN" <<'EOF'
# MyTechPulse: 本番VMのSSH硬化設定（ops/oracle-vm-setup.sh が生成）
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
EOF
    # 構文エラーのある設定を読み込ませてSSHを閉じ込めないよう、必ず検証してから反映する
    if sshd -t; then
        systemctl try-reload-or-restart ssh
    else
        warn "sshd設定の検証に失敗したため $SSHD_DROPIN を削除して反映を中止しました"
        rm -f "$SSHD_DROPIN"
    fi
else
    warn "/etc/ssh/sshd_config に sshd_config.d のIncludeが無いためSSH硬化をスキップしました"
fi

# ---- 5. fail2ban（SSHブルートフォース対策） ----
# Ubuntu 24.04 は /var/log/auth.log を使わずjournaldに記録するため、
# backend=auto だと fail2ban が何も読めずBANが効かない。systemdバックエンドを明示する
log "fail2ban のsshd jailを有効化"
install -m 0755 -d /etc/fail2ban/jail.d
cat > "$FAIL2BAN_JAIL" <<'EOF'
# MyTechPulse: SSHブルートフォース対策（ops/oracle-vm-setup.sh が生成）
[sshd]
enabled = true
# Ubuntu 24.04 は auth.log を持たずjournaldに記録するため systemd を明示する
backend = systemd
maxretry = 5
findtime = 10m
bantime = 1h
EOF
systemctl enable -q fail2ban
# jail設定を書き換えた直後なので、起動済みでも確実に読み直させる
systemctl restart fail2ban

# ---- 6. タイムゾーン ----
# ops/backup_db.sh のcrontab（日次4時）を意図した時刻で回すためJSTに合わせる
if [ "$(timedatectl show -p Timezone --value)" != "$TIMEZONE" ]; then
    log "タイムゾーンを $TIMEZONE に設定"
    timedatectl set-timezone "$TIMEZONE"
fi

# ---- 7. 結果確認と次の手順 ----
log "セットアップ完了。状態を確認する"
docker --version
docker compose version
fail2ban-client status sshd || warn "fail2banのsshd jailが起動していません（journal権限を確認）"
echo
echo "現在のINPUTチェイン:"
iptables -L INPUT -n --line-numbers

echo
echo "========================================================================"
echo "初期セットアップが完了しました。"
echo
if [ "$NEED_RELOGIN" -eq 1 ]; then
    echo "▲ dockerグループの追加を反映するため、いったんSSHを切断して再ログインしてください。"
fi

cat <<'EOF'
▲ VCNのSecurity List / NSG で ingress（tcp 22 / 80 / 443）の開放が別途必要です。
  未設定だとホスト側のiptablesを開けても外部から到達できません。

次の手順（詳細は docs/deploy/oracle-vm-provisioning.md）:
  1. git clone <このリポジトリ> && cd MyTechPulse
  2. cp backend/.env.example backend/.env して値を埋める
     - SECRET_KEY は本番用に新規生成する（開発用を使い回さない）:
         python3 -c "import secrets; print(secrets.token_hex(32))"
     - QIITA_ACCESS_TOKEN を設定する
  3. ルートに .env を作り MYSQL_ROOT_PASSWORD を強いランダム値で設定する
  4. docker compose up -d --build
     （テーブル作成は backend/entrypoint.sh の init_db.py が自動実行する）
  5. curl -sS http://localhost:8000/ で {"Hello":"World"} を確認
========================================================================
EOF
