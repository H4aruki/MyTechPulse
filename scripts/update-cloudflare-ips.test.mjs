import assert from "node:assert/strict";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { fetchIps, render, run } from "./update-cloudflare-ips.mjs";

const payload = {
  success: true,
  result: { ipv4_cidrs: ["173.245.48.0/20", "103.21.244.0/22"], ipv6_cidrs: ["2400:cb00::/32"] },
};
const fakeFetch = (body = payload, ok = true) => async () => ({ ok, status: ok ? 200 : 500, json: async () => body });

test("IPv4とIPv6の範囲を続けて取り出す", async () => {
  assert.deepEqual(await fetchIps(fakeFetch()), ["173.245.48.0/20", "103.21.244.0/22", "2400:cb00::/32"]);
});

test("HTTPエラーや形の違う応答は失敗にする", async () => {
  await assert.rejects(fetchIps(fakeFetch(payload, false)), /HTTP 500/);
  await assert.rejects(fetchIps(fakeFetch({ success: true, result: {} })), /形/);
  await assert.rejects(fetchIps(fakeFetch({ success: false, result: payload.result })), /形/);
});

test("設定片は403を返す窓口と信頼する中継の2つを含む", () => {
  const out = render(["1.2.3.0/24", "2400:cb00::/32"], "2026-10-15");
  assert.match(out, /^\(cloudflare_only\) \{$/m);
  assert.match(out, /@not_cloudflare not remote_ip 1\.2\.3\.0\/24 2400:cb00::\/32/);
  assert.match(out, /respond @not_cloudflare 403$/m);
  assert.match(out, /trusted_proxies static 1\.2\.3\.0\/24 2400:cb00::\/32/);
  assert.match(out, /取得日: 2026-10-15/);
});

test("--check は日付だけの違いなら成功、範囲が違えば1を返す", async () => {
  const dir = await mkdtemp(join(tmpdir(), "cfips-"));
  const path = join(dir, "cloudflare-ips.caddy");
  const today = new Date("2026-10-15T00:00:00Z");
  assert.equal(await run({ fetchFn: fakeFetch(), path, today }), 0);
  assert.match(await readFile(path, "utf8"), /173\.245\.48\.0\/20/);

  const later = new Date("2026-11-01T00:00:00Z");
  assert.equal(await run({ args: ["--check"], fetchFn: fakeFetch(), path, today: later }), 0);

  const changed = { ...payload, result: { ...payload.result, ipv4_cidrs: ["198.51.100.0/24"] } };
  assert.equal(await run({ args: ["--check"], fetchFn: fakeFetch(changed), path, today }), 1);
  assert.equal(await run({ args: ["--check"], fetchFn: fakeFetch(), path: join(dir, "none"), today }), 1);
  await writeFile(path, "");
  assert.equal(await run({ args: ["--check"], fetchFn: fakeFetch(), path, today }), 1);
});
