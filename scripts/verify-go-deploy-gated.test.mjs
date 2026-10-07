// Go版の自動デプロイ（ci.yml の deploy-changes・go-release・go-deploy）が、意図した条件でだけ動くことを、
// ci.yml の文面から確かめる（#125）。YAMLの解析ライブラリは使わず、jobの区切りと主要な行だけを読む。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { fileURLToPath } from "node:url";

const text = readFileSync(fileURLToPath(new URL("../.github/workflows/ci.yml", import.meta.url)), "utf8");

// jobsの下を、jobごとの本文に分ける
function splitJobs(source) {
  const jobs = {};
  let inJobs = false;
  let current = null;
  for (const line of source.split(/\r?\n/)) {
    if (/^jobs:\s*$/.test(line)) {
      inJobs = true;
      continue;
    }
    if (!inJobs) continue;
    if (/^\S/.test(line) && !line.startsWith("#")) break;
    const id = /^ {2}([A-Za-z0-9_-]+):\s*$/.exec(line);
    if (id) {
      current = { id: id[1], lines: [] };
      jobs[current.id] = current;
      continue;
    }
    if (current) current.lines.push(line);
  }
  for (const job of Object.values(jobs)) {
    job.body = job.lines.join("\n");
    job.condition = /^ {4}if:\s*(.+?)\s*$/m.exec(job.body)?.[1] ?? null;
    const needs = /^ {4}needs:\s*\[(.*?)\]\s*$/m.exec(job.body)?.[1];
    job.needs = needs ? needs.split(",").map((item) => item.trim()) : [];
    job.uses = /^ {4}uses:\s*(.+?)\s*$/m.exec(job.body)?.[1] ?? null;
  }
  return jobs;
}

// 「項目 == '値'」を && でつないだ条件式だけを評価する。想定外の書き方なら失敗させる
function evaluate(expression, context) {
  const known = new Set(["github.event_name", "github.ref", "vars.GO_DEPLOY_ENABLED", "needs.deploy-changes.outputs.needed"]);
  return expression.split("&&").every((term) => {
    const match = /^\s*([A-Za-z_.-]+)\s*==\s*'([^']*)'\s*$/.exec(term);
    assert.ok(match && known.has(match[1]), `想定外の記述が条件式にあります: ${term.trim()}`);
    return (context[match[1]] ?? "") === match[2];
  });
}

const jobs = splitJobs(text);
const checks = ["backend-lint", "frontend-lint", "hooks-test", "go-check", "go-generated", "go-image"];
const main = { "github.event_name": "push", "github.ref": "refs/heads/main" };

test("自動デプロイの3つのjobが存在する", () => {
  for (const id of ["deploy-changes", "go-release", "go-deploy"]) {
    assert.ok(jobs[id], `${id} がありません`);
  }
});

test("スイッチ（GO_DEPLOY_ENABLED）が 'true' で、mainへのpushのときだけ動く", () => {
  const condition = jobs["deploy-changes"].condition;
  assert.ok(condition, "deploy-changes に if がありません");
  const on = { ...main, "vars.GO_DEPLOY_ENABLED": "true" };
  assert.equal(evaluate(condition, on), true);
  // 未設定・空・false では動かない
  assert.equal(evaluate(condition, { ...main }), false);
  assert.equal(evaluate(condition, { ...main, "vars.GO_DEPLOY_ENABLED": "" }), false);
  assert.equal(evaluate(condition, { ...main, "vars.GO_DEPLOY_ENABLED": "false" }), false);
  // main以外・pull requestでは動かない
  assert.equal(evaluate(condition, { ...on, "github.event_name": "pull_request" }), false);
  assert.equal(evaluate(condition, { ...on, "github.ref": "refs/heads/feat/x" }), false);
});

test("配布物の作成は、検査が全部成功し、反映が要ると判定されたときだけ進む", () => {
  const release = jobs["go-release"];
  for (const id of [...checks, "deploy-changes"]) {
    assert.ok(release.needs.includes(id), `go-release の前提に ${id} がありません`);
  }
  assert.equal(release.uses, "./.github/workflows/release.yml");
  assert.equal(evaluate(release.condition, { "needs.deploy-changes.outputs.needed": "true" }), true);
  assert.equal(evaluate(release.condition, { "needs.deploy-changes.outputs.needed": "false" }), false);
  // スイッチが切れていると、deploy-changesが動かず、出力が空になる
  assert.equal(evaluate(release.condition, {}), false);
});

test("本番への反映は、配布物の作成が成功したときだけ動き、同時に2つは動かない", () => {
  const deploy = jobs["go-deploy"];
  assert.deepEqual(deploy.needs, ["go-release"]);
  assert.equal(deploy.condition, null, "go-deploy に独自の if があると、前提の成功を飛ばしかねません");
  assert.match(deploy.body, /concurrency:\s*\n\s+group: go-deploy\s*\n\s+cancel-in-progress: false/);
});

test("本番の鍵・Cloudflareの秘密は、反映のjob（と凍結中の旧公開）だけが使う", () => {
  const allowed = new Set(["go-deploy", "deploy-frontend", "deploy-backend"]);
  for (const job of Object.values(jobs)) {
    if (allowed.has(job.id)) continue;
    assert.ok(!/secrets\.(LIGHTSAIL_|CLOUDFLARE_)/.test(job.body), `${job.id} が本番の秘密を使っています`);
  }
  // 反映のjobは、本番サーバーの身元を照合してから接続する
  assert.match(jobs["go-deploy"].body, /StrictHostKeyChecking=yes/);
  assert.match(jobs["go-deploy"].body, /known_hosts/);
});

test("反映のjobの権限は、読み取りだけに絞られている", () => {
  const body = jobs["go-deploy"].body;
  assert.match(body, /permissions:\s*\n\s+contents: read\s*\n\s+actions: read/);
  assert.ok(!/(contents|packages|actions|id-token|pull-requests):\s*write/.test(body));
});

test("旧公開の凍結は、そのまま保たれている", () => {
  for (const id of ["deploy-frontend", "deploy-backend"]) {
    assert.ok(jobs[id].condition?.includes("vars.LEGACY_DEPLOY_ENABLED == 'true'"), `${id} の凍結条件が外れています`);
  }
});
