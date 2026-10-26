import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const script = path.join(here, "check-ca-bundle.sh");
const DISTRUSTED = "MIICTjCCAdOgAwIBAgIKPPYHqWhwDtqLhDAKBggqhkjOPQQDAzBrMQswCQYDVQQG";

const pem = (body) =>
  `-----BEGIN CERTIFICATE-----\n${body}\nAAAA\n-----END CERTIFICATE-----\n`;
const innocent = [
  pem("MIIBsyntheticAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"),
  pem("MIICsyntheticBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"),
];

// content が null のときはファイルを作らない。args を渡すと、ファイルの代わりに使う。
function run(content, args) {
  const dir = mkdtempSync(path.join(tmpdir(), "ca-bundle-"));
  try {
    const file = path.join(dir, "bundle.crt");
    if (content !== null) writeFileSync(file, content);
    return spawnSync("bash", [script, ...(args ?? [file])], { encoding: "utf8" });
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

test("無関係の証明書だけなら成功する", () => {
  const r = run(innocent.join(""));
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.stdout, /0件/);
});

test("不信任のrootが混ざっていたら失敗し、本文は出力しない", () => {
  const r = run(innocent[0] + pem(DISTRUSTED) + innocent[1]);
  assert.equal(r.status, 1);
  assert.match(r.stderr, /emSign ECC Root CA - G3/);
  assert.ok(!(r.stdout + r.stderr).includes(DISTRUSTED));
});

test("CRLF改行でも検出する", () => {
  const r = run((innocent[0] + pem(DISTRUSTED) + innocent[1]).replace(/\n/g, "\r\n"));
  assert.equal(r.status, 1);
  assert.match(r.stderr, /emSign ECC Root CA - G3/);
});

test("空ファイルと存在しないファイルは失敗する", () => {
  assert.equal(run("").status, 1);
  assert.equal(run(null).status, 1);
});

test("引数が無ければ使い方を出して終了コード2", () => {
  const r = run(null, []);
  assert.equal(r.status, 2);
  assert.match(r.stderr, /使い方/);
});

test("check-go-image.sh が check-ca-bundle.sh を呼んでいる", () => {
  const body = readFileSync(path.join(here, "check-go-image.sh"), "utf8");
  assert.ok(body.includes("check-ca-bundle.sh"));
  assert.ok(body.includes("docker cp"));
});
