import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { findUnpinnedActions } from "./verify-actions-pinned.mjs";

const SHA = "0123456789abcdef0123456789abcdef01234567";
const scriptPath = fileURLToPath(new URL("./verify-actions-pinned.mjs", import.meta.url));

test("タグだけで指定した外部actionは失敗として報告される", () => {
  const found = findUnpinnedActions("steps:\n  - uses: actions/checkout@v4\n", "a.yml");
  assert.equal(found.length, 1);
  assert.equal(found[0].file, "a.yml");
  assert.equal(found[0].line, 2);
  assert.equal(found[0].action, "actions/checkout");
});

test("40桁の小文字16進数で固定したactionは成功する", () => {
  const text = `steps:\n  - uses: actions/checkout@${SHA} # v4\n`;
  assert.deepEqual(findUnpinnedActions(text, "a.yml"), []);
});

test("リポジトリ内のlocal actionは対象外", () => {
  assert.deepEqual(findUnpinnedActions("steps:\n  - uses: ./.github/actions/x\n", "a.yml"), []);
});

test("大文字や桁数違いのSHA、ref無しは失敗する", () => {
  const bad = [
    `actions/checkout@${SHA.toUpperCase()}`,
    `actions/checkout@${SHA.slice(0, 39)}`,
    `actions/checkout@${SHA}0`,
    "actions/checkout",
    "actions/checkout@main",
  ];
  for (const ref of bad) {
    const found = findUnpinnedActions(`steps:\n  - uses: ${ref}\n`, "a.yml");
    assert.equal(found.length, 1, ref);
  }
});

test("引用符付きと再利用workflowの指定も検査する", () => {
  const text = [
    "jobs:",
    "  a:",
    '    uses: "org/repo/.github/workflows/x.yml@v1"',
    "  b:",
    "    steps:",
    "      - uses: 'actions/setup-node@v4'",
  ].join("\n");
  const found = findUnpinnedActions(text, "a.yml");
  assert.deepEqual(
    found.map((f) => [f.line, f.action]),
    [
      [3, "org/repo/.github/workflows/x.yml"],
      [6, "actions/setup-node"],
    ],
  );
});

test("docker:// 指定はsha256ダイジェスト固定のときだけ成功する", () => {
  const digest = "a".repeat(64);
  assert.deepEqual(findUnpinnedActions(`  - uses: docker://alpine@sha256:${digest}\n`, "a.yml"), []);
  assert.equal(findUnpinnedActions("  - uses: docker://alpine:3.20\n", "a.yml").length, 1);
});

test("コメント内のusesや文章中のusesは対象外", () => {
  const text = "# uses: actions/checkout@v4\nname: uses: は説明\nrun: echo 'uses: a/b@v1'\n";
  assert.deepEqual(findUnpinnedActions(text, "a.yml"), []);
});

function runScanner(files) {
  const root = mkdtempSync(path.join(tmpdir(), "pinned-"));
  try {
    const dir = path.join(root, ".github", "workflows");
    mkdirSync(dir, { recursive: true });
    for (const [name, body] of Object.entries(files)) writeFileSync(path.join(dir, name), body);
    return spawnSync(process.execPath, [scriptPath], { cwd: root, encoding: "utf8" });
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

test("CLIは未固定でexit 1、場所とaction名だけを出力する", () => {
  const result = runScanner({
    "ci.yml": "env:\n  TOKEN: secret-value-123\nsteps:\n  - uses: actions/checkout@v4\n",
  });
  assert.equal(result.status, 1);
  assert.match(result.stderr, /ci\.yml:4/);
  assert.match(result.stderr, /actions\/checkout/);
  assert.doesNotMatch(result.stderr + result.stdout, /secret-value-123/);
});

test("CLIはすべて固定されていればexit 0", () => {
  const result = runScanner({ "ci.yml": `steps:\n  - uses: actions/checkout@${SHA} # v4\n` });
  assert.equal(result.status, 0, result.stderr);
});
