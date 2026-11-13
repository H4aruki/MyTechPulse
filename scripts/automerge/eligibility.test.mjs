import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { main, makeApi, gatherInput, dryRunMerged } from "./eligibility.mjs";
import { evaluate } from "./eligibility-core.mjs";

const rules = JSON.parse(readFileSync(new URL("../../.github/automerge-rules.json", import.meta.url), "utf8"));
const R = "o/r";
const PATCH = "@@ -1,1 +1,2 @@\n a\n+b";

function fakeExec({ fail = null, prs = [1] } = {}) {
  const pkg = Buffer.from(JSON.stringify({ dependencies: { react: "1" } })).toString("base64");
  return (args) => {
    const key = args.join(" ");
    if (fail && key.includes(fail)) throw new Error("boom");
    if (args[0] === "repo") return JSON.stringify({ nameWithOwner: R });
    if (args[0] === "pr") return JSON.stringify(prs.map((number) => ({ number })));
    if (args[0] === "api" && args[1] === "graphql") return JSON.stringify({ data: { repository: { pullRequest: { reviewThreads: { pageInfo: { hasNextPage: false }, nodes: [{ isResolved: true }] } } } } });
    const p = args[1];
    if (/\/pulls\/\d+$/.test(p)) {
      return JSON.stringify({ title: "修正", state: "open", draft: false, mergeable_state: "clean", body: "## 判断が必要なこと\nなし", labels: [], base: { ref: "main", repo: { full_name: R } }, head: { sha: "abc1234567", repo: { full_name: R } } });
    }
    if (p.includes("/files")) return JSON.stringify([[{ filename: "server/internal/x/x.go", status: "modified", additions: 5, deletions: 1, patch: PATCH }, { filename: "server/internal/x/x_test.go", status: "modified", additions: 9, deletions: 0, patch: PATCH }]]);
    if (p.includes("/check-runs")) return JSON.stringify([{ check_runs: rules.requiredChecks.map((name) => ({ name, status: "completed", conclusion: "success" })) }]);
    if (p.endsWith("/status")) return JSON.stringify({ statuses: [] });
    if (p.includes("/compare/")) return JSON.stringify({ behind_by: 0 });
    if (p.includes("/reviews")) return JSON.stringify([[]]);
    if (p.includes("/contents/")) return JSON.stringify({ encoding: "base64", content: pkg });
    throw new Error("想定外: " + key);
  };
}

function run(argv, exec) {
  let out = "", errText = "";
  const code = main(["--rules", new URL("../../.github/automerge-rules.json", import.meta.url).pathname, "--repo", R, ...argv], { exec, write: (s) => (out += s), err: (s) => (errText += s) });
  return { code, out, errText };
}

test("組み立て→判定→要約（対象は終了コード0）", () => {
  const r = run(["--pr", "1"], fakeExec());
  assert.equal(r.code, 0);
  assert.match(r.out, /PR #1 対象（クラスA）/);
});

test("--json は判定結果と headSha", () => {
  const r = run(["--pr", "1", "--json"], fakeExec());
  const j = JSON.parse(r.out);
  assert.equal(j.eligible, true);
  assert.equal(j.headSha, "abc1234567");
});

test("--comment-markdown は renderComment の結果", () => {
  const r = run(["--pr", "1", "--comment-markdown"], fakeExec());
  assert.match(r.out, /対象（クラスA）/);
});

test("取得失敗は終了コード2（fail closed）", () => {
  const r = run(["--pr", "1"], fakeExec({ fail: "/check-runs" }));
  assert.equal(r.code, 2);
  assert.match(r.errText, /取得失敗/);
});

test("引数エラーは終了コード2", () => {
  assert.equal(run([], fakeExec()).code, 2);
  assert.equal(run(["--pr", "abc"], fakeExec()).code, 2);
  assert.equal(run(["--nope"], fakeExec()).code, 2);
});

test("対象外は終了コード1", () => {
  const exec = fakeExec();
  const bad = (args) => (args[1]?.includes("/compare/") ? JSON.stringify({ behind_by: 3 }) : exec(args));
  const r = run(["--pr", "1"], bad);
  assert.equal(r.code, 1);
  assert.match(r.out, /state\.behind/);
});

test("gatherInput は merged-dryrun で compare を取らない", () => {
  const { input } = gatherInput(makeApi(fakeExec({ fail: "/compare/" })), R, 1, "merged-dryrun");
  assert.equal(input.compare, null);
  assert.equal(evaluate(input, rules).eligible, true);
});

test("dry-run は表と集計の形を出し、取得失敗の行は対象外", () => {
  const exec = fakeExec({ prs: [1, 2] });
  const api = makeApi((args) => {
    if (args[1] === "repos/o/r/pulls/2") throw new Error("boom");
    return exec(args);
  });
  const r = dryRunMerged({ api, repo: R, count: 2, rules, listMerged: () => [1, 2] });
  assert.match(r.markdown, /\| PR \| 題 \| クラス \| 対象か \| 当たったルール \|/);
  assert.match(r.markdown, /クラス別: A 1 本 \/ B 0 本 \/ C 0 本/);
  assert.match(r.markdown, /取得失敗（対象外）/);
  assert.match(r.markdown, /本文ルール/);
  assert.equal(r.eligible, 1);
});

test("dry-run は main 経由で markdown を出力する", () => {
  const r = run(["--dry-run-merged", "1"], fakeExec());
  assert.equal(r.code, 0);
  assert.match(r.out, /# 自動マージ判定のドライラン/);
});
