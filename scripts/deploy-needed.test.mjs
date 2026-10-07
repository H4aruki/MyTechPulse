import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { needsDeploy } from "./deploy-needed.mjs";

test("Go版・画面・運用スクリプト・composeの変更は、反映が要る", () => {
  for (const file of [
    "server/internal/auth/service.go",
    "server/db/migrations/00002_add_column.sql",
    "frontend/src/pages/LoginPage.tsx",
    "ops/deploy_go.sh",
    "docker-compose.yml",
    "Caddyfile",
    ".github/workflows/ci.yml",
    ".github/workflows/release.yml",
    "scripts/cutover-frontend.mjs",
  ]) {
    assert.equal(needsDeploy([file]), true, file);
  }
});

test("文書・設定・他のworkflowだけの変更は、反映しない", () => {
  for (const file of [
    "README.md",
    "docs/deploy/go-cutover.md",
    "CONTRIBUTING.md",
    "TASKS.md",
    ".claude/settings.json",
    ".github/workflows/cutover-frontend.yml",
    ".github/pull_request_template.md",
    "backend/app/main.py",
  ]) {
    assert.equal(needsDeploy([file]), false, file);
  }
});

test("試験だけの変更は、反映しない", () => {
  for (const file of [
    "server/internal/auth/service_test.go",
    "frontend/src/pages/LoginPage.test.tsx",
    "ops/deploy_go_test.sh",
    "ops/tests/release_fixture.sh",
    "scripts/deploy-needed.test.mjs",
  ]) {
    assert.equal(needsDeploy([file]), false, file);
  }
});

test("1つでも反映が要るものがあれば、全体として反映が要る", () => {
  assert.equal(needsDeploy(["README.md", "docs/a.md", "server/main.go"]), true);
  assert.equal(needsDeploy(["README.md", "server/internal/x_test.go"]), false);
});

test("空の一覧は、反映しない", () => {
  assert.equal(needsDeploy([]), false);
  assert.equal(needsDeploy(["", "  "]), false);
});

test("標準入力から読み、GITHUB_OUTPUTの形で出力する", () => {
  const script = fileURLToPath(new URL("./deploy-needed.mjs", import.meta.url));
  const run = (input) => spawnSync(process.execPath, [script], { input, encoding: "utf8" }).stdout.trim();
  assert.equal(run("README.md\nserver/main.go\n"), "needed=true");
  assert.equal(run("README.md\ndocs/x.md\n"), "needed=false");
  assert.equal(run(""), "needed=false");
});
