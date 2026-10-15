// CloudflareのIP範囲から、Caddy用の設定片 ops/caddy/cloudflare-ips.caddy を作り直す（#74）。
//
// 使い方:
//   node scripts/update-cloudflare-ips.mjs           # ファイルを書き換える
//   node scripts/update-cloudflare-ips.mjs --check   # 最新の範囲と違えば exit 1（書き換えない）
//
// 取得先は認証不要の公開API。Node標準機能だけで動く。
import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

export const SOURCE_URL = "https://api.cloudflare.com/client/v4/ips";
export const OUTPUT_PATH = fileURLToPath(new URL("../ops/caddy/cloudflare-ips.caddy", import.meta.url));
// グローバル設定（Caddyfileの先頭）では、スニペットがまだ定義されておらず使えない。
// そこで trusted_proxies の1行だけを別ファイルにして、パスで import する
export const TRUSTED_PATH = fileURLToPath(new URL("../ops/caddy/cloudflare-trusted-proxies.caddy", import.meta.url));

// 日付の行は毎日変わるので、--check では比べない
const DATE_LINE = /^# 取得日:.*$/m;

export async function fetchIps(fetchFn = fetch) {
  const res = await fetchFn(SOURCE_URL);
  if (!res.ok) throw new Error(`Cloudflare IP取得に失敗: HTTP ${res.status}`);
  const body = await res.json();
  const v4 = body?.result?.ipv4_cidrs;
  const v6 = body?.result?.ipv6_cidrs;
  if (!body?.success || !Array.isArray(v4) || !Array.isArray(v6) || v4.length === 0 || v6.length === 0) {
    throw new Error("Cloudflare IPの応答の形が想定と違う");
  }
  return [...v4, ...v6];
}

export function render(cidrs, date) {
  const list = cidrs.join(" ");
  return `# 自動生成: scripts/update-cloudflare-ips.mjs で作り直す。手で編集しない。
# 取得元: ${SOURCE_URL}
# 取得日: ${date}
#
# 本番の入口（Caddy）は、Cloudflare経由の要求にだけ答える（#74）。
# cloudflare_only: 接続元がCloudflareのIP範囲に無い要求へ、本文なしの403を返す。
# 範囲が変わったら、スクリプトを実行して差分をコミットする。
# trusted_proxies 用の cloudflare-trusted-proxies.caddy も同時に作り直される。

(cloudflare_only) {
	@not_cloudflare not remote_ip ${list}
	respond @not_cloudflare 403
}
`;
}

export function renderTrusted(cidrs, date) {
  return `# 自動生成: scripts/update-cloudflare-ips.mjs で作り直す。手で編集しない。
# 取得元: ${SOURCE_URL}
# 取得日: ${date}
#
# Caddyfile のグローバル設定 servers から、パスで import する（#74）。
# Cloudflareが付けた X-Forwarded-For から、本当の利用者のIPを client_ip に取り出すため。
trusted_proxies static ${cidrs.join(" ")}
`;
}

export function sameIgnoringDate(a, b) {
  return a.replace(DATE_LINE, "") === b.replace(DATE_LINE, "");
}

export async function run({
  args = [],
  fetchFn = fetch,
  path = OUTPUT_PATH,
  trustedPath = TRUSTED_PATH,
  today = new Date(),
} = {}) {
  const cidrs = await fetchIps(fetchFn);
  const date = today.toISOString().slice(0, 10);
  const files = [
    [path, render(cidrs, date)],
    [trustedPath, renderTrusted(cidrs, date)],
  ];
  if (args.includes("--check")) {
    let stale = false;
    for (const [file, next] of files) {
      const current = await readFile(file, "utf8").catch(() => "");
      if (!sameIgnoringDate(current, next)) {
        console.error(`${file} が最新のCloudflare IP範囲と違います。node scripts/update-cloudflare-ips.mjs を実行してください。`);
        stale = true;
      }
    }
    return stale ? 1 : 0;
  }
  for (const [file, next] of files) await writeFile(file, next);
  return 0;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  process.exitCode = await run({ args: process.argv.slice(2) });
}
