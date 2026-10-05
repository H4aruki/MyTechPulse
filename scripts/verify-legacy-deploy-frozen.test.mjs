// 旧画面・旧サーバーの自動公開が凍結されていることを、ci.yml の文面から確かめる。
// YAMLの解析ライブラリは使わず、jobの見出しと name/if の行だけを読む。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { fileURLToPath } from "node:url";

const ciPath = fileURLToPath(new URL("../.github/workflows/ci.yml", import.meta.url));
const text = readFileSync(ciPath, "utf8");

function parseJobs(source) {
  const jobs = {};
  let inJobs = false;
  let current = null;
  for (const line of source.split(/\r?\n/)) {
    if (/^jobs:\s*$/.test(line)) {
      inJobs = true;
      continue;
    }
    if (!inJobs) continue;
    if (/^\S/.test(line) && !line.startsWith("#")) break; // jobs以外の最上位キー
    const id = /^ {2}([A-Za-z0-9_-]+):\s*$/.exec(line);
    if (id) {
      current = { id: id[1], name: null, condition: null };
      jobs[current.id] = current;
      continue;
    }
    if (!current) continue;
    const name = /^ {4}name:\s*(.+?)\s*$/.exec(line);
    if (name) current.name = name[1];
    const condition = /^ {4}if:\s*(.+?)\s*$/.exec(line);
    if (condition) current.condition = condition[1];
  }
  return jobs;
}

// 「項目 == '値'」を && でつないだ条件式だけを評価する小さな評価器。
// 想定外の書き方が来たら評価せず失敗させる（式を実行はしない）。
// 未設定の項目は、GitHub Actionsと同じく空文字として扱う
function evaluate(expression, context) {
  const known = new Set(["github.event_name", "github.ref", "vars.LEGACY_DEPLOY_ENABLED"]);
  return expression.split("&&").every((term) => {
    const match = /^\s*([A-Za-z_.]+)\s*==\s*'([^']*)'\s*$/.exec(term);
    assert.ok(match && known.has(match[1]), `想定外の記述が条件式にあります: ${term.trim()}`);
    return (context[match[1]] ?? "") === match[2];
  });
}

const jobs = parseJobs(text);
const frozen = ["deploy-frontend", "deploy-backend"];
const main = { "github.event_name": "push", "github.ref": "refs/heads/main" };

test("旧公開の2つのjobが存在し、凍結条件を持つ", () => {
  for (const id of frozen) {
    assert.ok(jobs[id], `${id} がありません`);
    assert.ok(jobs[id].condition, `${id} に if がありません`);
    assert.ok(
      jobs[id].condition.includes("vars.LEGACY_DEPLOY_ENABLED == 'true'"),
      `${id} に凍結条件がありません`,
    );
  }
});

test("リポジトリ変数が未設定なら、mainへのpushでも旧公開はskipされる", () => {
  for (const id of frozen) {
    assert.equal(evaluate(jobs[id].condition, { ...main }), false, id);
    assert.equal(evaluate(jobs[id].condition, { ...main, "vars.LEGACY_DEPLOY_ENABLED": "" }), false, id);
    assert.equal(evaluate(jobs[id].condition, { ...main, "vars.LEGACY_DEPLOY_ENABLED": "false" }), false, id);
  }
});

test("変数が'true'でも、main以外やpull requestでは動かない（従来条件を保つ）", () => {
  const enabled = { "vars.LEGACY_DEPLOY_ENABLED": "true" };
  for (const id of frozen) {
    assert.equal(evaluate(jobs[id].condition, { ...main, ...enabled }), true, id);
    assert.equal(
      evaluate(jobs[id].condition, { "github.event_name": "pull_request", "github.ref": "refs/heads/main", ...enabled }),
      false,
      id,
    );
    assert.equal(
      evaluate(jobs[id].condition, { "github.event_name": "push", "github.ref": "refs/heads/feat/x", ...enabled }),
      false,
      id,
    );
  }
});

test("必須checkとして使われている既存の表示名は変えない", () => {
  const names = Object.values(jobs).map((job) => job.name);
  for (const required of [
    "バックエンドの書き方チェック",
    "フロントエンドの書き方チェック",
    "設定まわりの仕掛けの確認",
    "画面の公開",
    "サーバーの反映",
  ]) {
    assert.ok(names.includes(required), `表示名 ${required} が見つかりません`);
  }
});

test("Go・生成差分・imageの検査jobが、公開jobの前提に入らず独立して存在する", () => {
  assert.equal(jobs["go-check"]?.name, "Goバックエンドの検査");
  assert.equal(jobs["go-generated"]?.name, "sqlcとOpenAPIの生成差分");
  assert.equal(jobs["go-image"]?.name, "Go APIのimage検査");
  for (const id of ["go-check", "go-generated", "go-image"]) {
    assert.equal(jobs[id].condition, null, `${id} は常に実行される`);
  }
});
