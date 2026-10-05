#!/usr/bin/env bash
# verify_release_test.sh と deploy_release_test.sh が共有する、検査用の部品。
# 単独では実行せず、source して使う。本物のimage・secret・本番の値は一切使わない。

FX_COMMIT="0123456789abcdef0123456789abcdef01234567"
FX_DIGEST="$(printf 'a%.0s' $(seq 1 64))"
FX_IMAGE="ghcr.io/h4aruki/mytechpulse-api-go@sha256:${FX_DIGEST}"

fx_sha256() {
  sha256sum "$1" | cut -d' ' -f1
}

# 作業用の一時directoryを作る。後片付けは、この関数を呼んだtest側のtrapで行う
fx_init() {
  FX_TMP="$(mktemp -d)"
  FX_ART="$FX_TMP/artifacts"
  FX_ROOT="$FX_TMP/releases"
  mkdir -p "$FX_ART" "$FX_ROOT"
  FX_RUN_ID="4242"
  FX_ATTEMPT="1"
  fx_make_archives
  fx_write_manifest
}

# frontendとopsの正常なarchiveを作る
fx_make_archives() {
  local src="$FX_TMP/src"
  rm -rf "$src"
  mkdir -p "$src/frontend/assets" "$src/ops/ops"
  echo '<html></html>' > "$src/frontend/index.html"
  echo 'console.log(1)' > "$src/frontend/assets/app.js"
  echo 'services: {}' > "$src/ops/docker-compose.yml"
  echo ':80 { respond ok }' > "$src/ops/Caddyfile"
  echo '#!/usr/bin/env bash' > "$src/ops/ops/backup_db.sh"
  tar -czf "$FX_ART/frontend-${FX_COMMIT}.tar.gz" -C "$src/frontend" index.html assets
  tar -czf "$FX_ART/ops-${FX_COMMIT}.tar.gz" -C "$src/ops" docker-compose.yml Caddyfile ops
}

# 正しい形式のmanifestを書く。次の変数で一部を差し替えて不正なmanifestを作れる
#   FX_API_IMAGE / FX_MANIFEST_COMMIT / FX_MANIFEST_RUN_ID / FX_MANIFEST_ATTEMPT
# 書いた後で MTP_RELEASE_MANIFEST などの入力環境変数も設定する
fx_write_manifest() {
  local commit="${FX_MANIFEST_COMMIT:-$FX_COMMIT}"
  local image="${FX_API_IMAGE:-$FX_IMAGE}"
  local run_id="${FX_MANIFEST_RUN_ID:-$FX_RUN_ID}"
  local attempt="${FX_MANIFEST_ATTEMPT:-$FX_ATTEMPT}"
  local fe ops
  fe="$(fx_sha256 "$FX_ART/frontend-${FX_COMMIT}.tar.gz")"
  ops="$(fx_sha256 "$FX_ART/ops-${FX_COMMIT}.tar.gz")"
  cat > "$FX_ART/release-manifest.json" <<JSON
{
  "schema_version": 1,
  "commit_sha": "${commit}",
  "api_image": "${image}",
  "frontend": {
    "artifact_name": "frontend-${commit}",
    "file": "frontend-${commit}.tar.gz",
    "sha256": "${fe}"
  },
  "ops": {
    "artifact_name": "ops-${commit}",
    "file": "ops-${commit}.tar.gz",
    "sha256": "${ops}"
  },
  "workflow": {
    "run_id": "${run_id}",
    "run_attempt": "${attempt}",
    "url": "https://github.com/H4aruki/MyTechPulse/actions/runs/${run_id}/attempts/${attempt}"
  }
}
JSON
  fx_export_inputs
}

# 反映scriptへ渡す入力を、正常な値で設定する
fx_export_inputs() {
  export MTP_RELEASE_MANIFEST="$FX_ART/release-manifest.json"
  MTP_MANIFEST_SHA256="$(fx_sha256 "$MTP_RELEASE_MANIFEST")"
  export MTP_MANIFEST_SHA256
  export MTP_RELEASE_RUN_ID="$FX_RUN_ID"
  export MTP_RELEASE_RUN_ATTEMPT="$FX_ATTEMPT"
  export MTP_RELEASES_ROOT="$FX_ROOT"
}

# 危険な中身のarchiveをPythonで作る（symlinkやhardlinkはtarコマンドだけだと
# Windowsで再現できないため）。使い方: fx_craft_tar 種類 出力先
#   absolute / dotdot / symlink / hardlink
fx_craft_tar() {
  local kind="$1" out="$2" py
  py="$(command -v python3 || command -v python)"
  "$py" - "$kind" "$out" <<'PY'
import io, sys, tarfile
kind, out = sys.argv[1], sys.argv[2]
with tarfile.open(out, "w:gz") as tar:
    def add_file(name, data=b"x"):
        info = tarfile.TarInfo(name)
        info.size = len(data)
        tar.addfile(info, io.BytesIO(data))
    add_file("docker-compose.yml", b"services: {}\n")
    if kind == "absolute":
        add_file("/tmp/mtp-escape.txt")
    elif kind == "dotdot":
        add_file("../mtp-escape.txt")
    elif kind == "symlink":
        info = tarfile.TarInfo("link")
        info.type = tarfile.SYMTYPE
        info.linkname = "/etc/passwd"
        tar.addfile(info)
    elif kind == "hardlink":
        info = tarfile.TarInfo("hard")
        info.type = tarfile.LNKTYPE
        info.linkname = "docker-compose.yml"
        tar.addfile(info)
    else:
        raise SystemExit("unknown kind")
PY
}
