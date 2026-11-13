import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { evaluate, renderComment, globToRegExp } from "./eligibility-core.mjs";

const rules = JSON.parse(readFileSync(new URL("../../.github/automerge-rules.json", import.meta.url), "utf8"));

const patchOf = (adds, dels = 0) =>
  `@@ -1,${dels} +1,${adds} @@\n` + "-x\n".repeat(dels) + "+y\n".repeat(adds);
const file = (filename, additions, deletions = 0, extra = {}) => ({
  filename,
  status: "modified",
  kind: "file",
  additions,
  deletions,
  changes: additions + deletions,
  patch: patchOf(additions, deletions),
  ...extra,
});
const withPatch = (filename, lines, extra = {}) => ({
  filename,
  status: "modified",
  kind: "file",
  additions: lines.length,
  deletions: 0,
  changes: lines.length,
  patch: `@@ -1,0 +1,${lines.length} @@\n` + lines.map((l) => `+${l}`).join("\n"),
  ...extra,
});
const OK_BODY = "## なぜ（Why）\nx\n\n## 確認（Verification）\ny\n\n## 判断が必要なこと\nなし\n";
const goodChecks = () => rules.requiredChecks.map((name) => ({ name, status: "completed", conclusion: "success" }));
const base = (over = {}) => ({
  pr: {
    number: 1,
    title: "t",
    body: OK_BODY,
    draft: false,
    state: "open",
    merged: false,
    base: { ref: "main", repo: { full_name: "H4aruki/MyTechPulse" } },
    head: { sha: "3f1f9db0000", ref: "fix/x", repo: { full_name: "H4aruki/MyTechPulse" } },
    labels: [],
    mergeable_state: "clean",
  },
  files: [file("server/internal/feed/feed.go", 10, 2), file("server/internal/feed/feed_test.go", 20)],
  checks: goodChecks(),
  statuses: [],
  compare: { behind_by: 0 },
  reviews: [],
  unresolvedThreads: 0,
  baseDeps: ["react", "@tanstack/react-query"],
  baseGoModules: ["github.com/jackc/pgx/v5", "github.com/pressly/goose/v3"],
  ...over,
});
const rulesOf = (r) => r.failures.map((f) => f.rule);

test("基本: 全部OKならクラスA", () => {
  const r = evaluate(base(), rules);
  assert.deepEqual(r.failures, []);
  assert.equal(r.eligible, true);
  assert.equal(r.class, "A");
  assert.equal(r.stats.files, 2);
  assert.equal(r.stats.nonTestFiles, 1);
  assert.equal(r.stats.testFiles, 1);
  assert.equal(r.stats.nonTestAdditions, 10);
});

test("globToRegExp", () => {
  assert.ok(globToRegExp("server/internal/**").test("server/internal/a/b.go"));
  assert.ok(!globToRegExp("server/internal/**").test("server/cmd/a.go"));
  assert.ok(globToRegExp("**/Dockerfile").test("Dockerfile"));
  assert.ok(globToRegExp("**/Dockerfile").test("a/b/Dockerfile"));
  assert.ok(globToRegExp("*.pem").test("x/y/a.pem"));
  assert.ok(globToRegExp("a/{b,c}/*.go").test("a/c/x.go"));
  assert.ok(!globToRegExp("a/*.go").test("a/b/x.go"));
});

test("サイズ: テスト以外60行は通り61行は落ちる", () => {
  const t = file("server/internal/feed/feed_test.go", 5);
  assert.equal(evaluate(base({ files: [file("server/internal/feed/a.go", 50, 10), t] }), rules).eligible, true);
  const r = evaluate(base({ files: [file("server/internal/feed/a.go", 50, 11), t] }), rules);
  assert.deepEqual(rulesOf(r), ["size.nonTestLines"]);
  assert.equal(r.class, "B");
  assert.match(r.failures[0].detail, /61 行（上限 60）/);
});

test("サイズ: テスト以外4ファイルは通り5ファイルは落ちる", () => {
  const nt = (n) => Array.from({ length: n }, (_, i) => file(`server/internal/feed/f${i}.go`, 1));
  const t = file("server/internal/feed/x_test.go", 1);
  assert.equal(evaluate(base({ files: [...nt(4), t] }), rules).eligible, true);
  assert.deepEqual(rulesOf(evaluate(base({ files: [...nt(5), t] }), rules)), ["size.nonTestFiles"]);
});

test("サイズ: 全体300行は通り301行は落ちる", () => {
  const a = file("server/internal/feed/a.go", 10);
  assert.equal(evaluate(base({ files: [a, file("server/internal/feed/a_test.go", 290)] }), rules).eligible, true);
  const r = evaluate(base({ files: [a, file("server/internal/feed/a_test.go", 291)] }), rules);
  assert.deepEqual(rulesOf(r), ["size.totalLines"]);
});

for (const [p, why] of [
  ["server/internal/auth/login.go", "auth"],
  ["server/internal/Auth/x.go", "大文字"],
  ["frontend/src/pages/PasskeyButton.tsx", "passkey"],
  ["server/internal/foo/session_store.go", "session"],
  ["server/internal/platform/config/c.go", "config"],
  ["server/internal/platform/httpx/h.go", "httpx"],
  ["server/internal/app/routes.go", "app"],
  ["server/cmd/api/main.go", "cmd"],
  ["server/db/migrations/001.sql", "db"],
  ["server/openapi/openapi.json", "openapi"],
  ["server/Dockerfile", "Dockerfile"],
  ["Dockerfile", "Dockerfile root"],
  ["ops/deploy.sh", "ops"],
  ["scripts/x.mjs", "scripts"],
  ["go.mod", "go.mod"],
  ["frontend/package-lock.json", "lock"],
  ["frontend/package.json", "package.json"],
  [".github/workflows/ci.yml", ".github"],
  [".github/automerge-rules.json", "rules"],
  [".claude/settings.json", ".claude"],
  ["AGENTS.md", "AGENTS"],
  ["scripts/automerge/eligibility-core.mjs", "automerge"],
  ["frontend/.env.local", ".env"],
  ["server/internal/x/key.pem", "pem"],
  ["docs/readme.md", "許可外"],
]) {
  test(`パス: ${why} (${p}) はクラスC`, () => {
    const r = evaluate(base({ files: [...base().files, file(p, 1)] }), rules);
    assert.equal(r.eligible, false);
    assert.equal(r.class, "C");
    assert.ok(rulesOf(r).some((x) => x.startsWith("path.")));
    assert.ok(r.failures.some((f) => f.detail.includes(p)));
  });
}

test("パス: auth の理由に部分一致が出る", () => {
  const r = evaluate(base({ files: [file("server/internal/auth/x.go", 1), file("server/internal/auth/x_test.go", 1)] }), rules);
  assert.match(r.failures[0].detail, /認証パス|認証・安全/);
});

test("パス: rename・削除・バイナリ・symlink は失敗", () => {
  const t = file("server/internal/feed/a_test.go", 3);
  let r = evaluate(base({ files: [t, file("server/internal/feed/b.go", 0, 0, { status: "renamed", previous_filename: "server/internal/feed/a.go" })] }), rules);
  assert.equal(r.class, "C");
  assert.ok(rulesOf(r).includes("path.status") && rulesOf(r).includes("path.renamed"));
  r = evaluate(base({ files: [t, file("server/internal/feed/b.go", 0, 5, { status: "removed" })] }), rules);
  assert.ok(rulesOf(r).includes("path.status"));
  r = evaluate(base({ files: [t, { filename: "frontend/src/logo.png", kind: "file", status: "added", additions: 0, deletions: 0, changes: 0 }, { filename: "frontend/src/big.ts", kind: "file", status: "modified", additions: 400, deletions: 0, changes: 400 }] }), rules);
  assert.deepEqual(rulesOf(r).filter((x) => x === "path.no-patch").length, 2);
  r = evaluate(base({ files: [t, file("frontend/src/link.ts", 1, 0, { status: "added", kind: "symlink" })] }), rules);
  assert.ok(rulesOf(r).includes("path.symlink"));
  r = evaluate(base({ files: [t, file("frontend/src/unk.ts", 1, 0, { kind: undefined })] }), rules);
  assert.ok(rulesOf(r).includes("path.kind"));
});

test("検査: 同名の check-run が複数あるときは全部 success が必要", () => {
  const name = rules.requiredChecks[0];
  const dup = (second) => base({ checks: [...goodChecks(), { name, ...second }] });
  assert.equal(evaluate(dup({ status: "completed", conclusion: "success" }), rules).eligible, true);
  assert.deepEqual(rulesOf(evaluate(dup({ status: "completed", conclusion: "failure" }), rules)), ["checks.required-failed"]);
  assert.deepEqual(rulesOf(evaluate(dup({ status: "in_progress", conclusion: null }), rules)), ["checks.running"]);
  // 失敗が先で成功が後でも見逃さない
  const rev = base({ checks: [{ name, status: "completed", conclusion: "failure" }, ...goodChecks()] });
  assert.deepEqual(rulesOf(evaluate(rev, rules)), ["checks.required-failed"]);
});

test("取得結果が欠けているときは 0 とみなさず失敗（fail closed）", () => {
  for (const key of ["unresolvedThreads", "files", "checks", "statuses", "reviews"]) {
    for (const v of [null, undefined]) {
      const r = evaluate(base({ [key]: v }), rules);
      assert.equal(r.eligible, false, `${key}=${v}`);
      assert.ok(rulesOf(r).includes("input.missing"), `${key}=${v}`);
    }
  }
});

test("import(Go): main の go.mod にある依存は通り、無ければ失敗", () => {
  const t = file("server/internal/feed/a_test.go", 3);
  const mk = (lines, extra = {}) => evaluate(base({ files: [withPatch("server/internal/feed/a_test.go", lines), t], ...extra }), rules);
  assert.equal(mk(['\t"github.com/jackc/pgx/v5/pgxpool"']).eligible, true);
  assert.equal(mk(['\t"github.com/pressly/goose/v3"']).eligible, true);
  assert.deepEqual(rulesOf(mk(['\t"github.com/jackc/pgx/v5x"'])), ["import.go"]);
  assert.deepEqual(rulesOf(mk(['\t"github.com/evil/x"'])), ["import.go"]);
  assert.deepEqual(rulesOf(mk(['\t"github.com/jackc/pgx/v5"'], { baseGoModules: null })), ["import.go"]);
});

test("テスト: テスト追加が無いと失敗、docsOnly は規則次第", () => {
  let r = evaluate(base({ files: [file("server/internal/feed/a.go", 3)] }), rules);
  assert.deepEqual(rulesOf(r), ["tests.missing"]);
  assert.equal(r.class, "B");
  r = evaluate(base({ files: [file("docs/a.md", 3)] }), rules);
  assert.ok(rulesOf(r).includes("tests.missing"));
  r = evaluate(base({ files: [file("docs/a.md", 3)] }), { ...rules, docsOnlyEligible: true });
  assert.ok(!rulesOf(r).includes("tests.missing"));
});

test("テスト削除: 削除ファイル・削除行が追加行より多い", () => {
  let r = evaluate(base({ files: [file("server/internal/feed/a.go", 3), file("server/internal/feed/a_test.go", 1), file("server/internal/feed/b_test.go", 0, 2, { status: "removed" })] }), rules);
  assert.ok(rulesOf(r).includes("tests.deleted"));
  r = evaluate(base({ files: [file("server/internal/feed/a.go", 3), file("server/internal/feed/a_test.go", 2, 3)] }), rules);
  assert.deepEqual(rulesOf(r), ["tests.shrunk"]);
  r = evaluate(base({ files: [file("server/internal/feed/a.go", 3), file("server/internal/feed/a_test.go", 3, 3)] }), rules);
  assert.equal(r.eligible, true);
});

for (const line of ["t.Skip(\"x\")", "it.skip('a', () => {})", "it.only('a', f)", "// @ts-ignore", "// @ts-expect-error", "// eslint-disable-next-line", "//nolint:errcheck", "// nolint", "exec.Command(\"ls\")", "<div dangerouslySetInnerHTML={x} />", "xit('a')", "xdescribe('a')", "it.todo('a')"]) {
  test(`禁止の書き方: ${line}`, () => {
    const files = [withPatch("frontend/src/a.ts", [line]), file("frontend/src/a.test.ts", 3)];
    const r = evaluate(base({ files }), rules);
    assert.deepEqual(rulesOf(r), ["content.forbidden"]);
  });
}

test("禁止の書き方: 削除行・文脈行は対象外", () => {
  const f = { ...file("frontend/src/a.ts", 1, 1), patch: "@@ -1,2 +1,2 @@\n-it.skip('a')\n context t.Skip(\n+ok()" };
  assert.equal(evaluate(base({ files: [f, file("frontend/src/a.test.ts", 3)] }), rules).eligible, true);
});

test("secret: 全ファイルを調べる（テスト・docsも）", () => {
  const lines = ["AKIA" + "ABCDEFGHIJKLMNOP", "ghp_" + "a".repeat(36), "github_pat_" + "A".repeat(30), "-----BEGIN RSA PRIVATE KEY-----", "xoxb-1234567890-abc", "sk-" + "a".repeat(24)];
  for (const l of lines) {
    const r = evaluate(base({ files: [withPatch("server/internal/feed/a_test.go", [`x := "${l}"`])] }), rules);
    assert.ok(rulesOf(r).includes("content.secret"), l);
  }
  const r = evaluate(base({ files: [...base().files, withPatch("docs/a.md", ["AKIA" + "ABCDEFGHIJKLMNOP"])] }), rules);
  assert.ok(rulesOf(r).includes("content.secret"));
});

test("import(Go): 外部は失敗、標準・自リポジトリは通る", () => {
  const t = file("server/internal/feed/a_test.go", 3);
  const mk = (lines) => evaluate(base({ files: [withPatch("server/internal/feed/a.go", lines), t] }), rules);
  assert.deepEqual(rulesOf(mk(['import "github.com/evil/x"'])), ["import.go"]);
  assert.deepEqual(rulesOf(mk(['\tfoo "golang.org/x/net/html"'])), ["import.go"]);
  assert.deepEqual(rulesOf(mk(['\t"github.com/gin-gonic/gin"'])), ["import.go"]);
  assert.equal(mk(['import "fmt"', '\t"net/http"', '\t"github.com/H4aruki/MyTechPulse/server/internal/x"', '\tx "github.com/H4aruki/MyTechPulse/server/internal/y"']).eligible, true);
});

test("import(TS): 外部は失敗、相対・@/・導入済みは通る", () => {
  const t = file("frontend/src/a.test.ts", 3);
  const mk = (lines, deps = ["react", "@tanstack/react-query"]) => evaluate(base({ files: [withPatch("frontend/src/a.tsx", lines), t], baseDeps: deps }), rules);
  assert.deepEqual(rulesOf(mk(["import x from 'lodash';"])), ["import.ts"]);
  assert.deepEqual(rulesOf(mk(["import 'lodash/fp';"])), ["import.ts"]);
  assert.deepEqual(rulesOf(mk(['const m = await import("evil-pkg");'])), ["import.ts"]);
  assert.deepEqual(rulesOf(mk(['const m = require("evil-pkg");'])), ["import.ts"]);
  assert.deepEqual(rulesOf(mk(["import q from '@tanstack/other';"])), ["import.ts"]);
  assert.equal(mk(["import a from './a';", "import b from '../b';", "import c from '@/c';", "import d from '/d';"]).eligible, true);
  assert.equal(mk(["import R from 'react';", "import { q } from '@tanstack/react-query/build';", "import 'react/jsx-runtime';"]).eligible, true);
  assert.deepEqual(rulesOf(mk(["import R from 'react';"], null)), ["import.ts"]);
});

test("検査: 必須欠け・失敗・実行中", () => {
  const miss = goodChecks().slice(1);
  assert.deepEqual(rulesOf(evaluate(base({ checks: miss }), rules)), ["checks.required-missing"]);
  const failed = goodChecks();
  failed[0].conclusion = "failure";
  assert.deepEqual(rulesOf(evaluate(base({ checks: failed }), rules)), ["checks.required-failed"]);
  const running = goodChecks();
  running[1] = { name: running[1].name, status: "in_progress", conclusion: null };
  assert.deepEqual(rulesOf(evaluate(base({ checks: running }), rules)), ["checks.running"]);
});

test("検査: 必須以外の失敗・実行中、skippedは通る", () => {
  const extra = (c) => base({ checks: [...goodChecks(), c] });
  assert.equal(evaluate(extra({ name: "配布物の作成", status: "completed", conclusion: "skipped" }), rules).eligible, true);
  assert.equal(evaluate(extra({ name: "x", status: "completed", conclusion: "neutral" }), rules).eligible, true);
  for (const c of ["failure", "cancelled", "timed_out", "action_required", "startup_failure"]) {
    assert.deepEqual(rulesOf(evaluate(extra({ name: "x", status: "completed", conclusion: c }), rules)), ["checks.failed"], c);
  }
  assert.deepEqual(rulesOf(evaluate(extra({ name: "x", status: "queued", conclusion: null }), rules)), ["checks.running"]);
});

test("検査: statuses の failure/error/pending", () => {
  for (const state of ["failure", "error", "pending"]) {
    assert.deepEqual(rulesOf(evaluate(base({ statuses: [{ context: "ci/x", state }] }), rules)), ["checks.status"]);
  }
  assert.equal(evaluate(base({ statuses: [{ context: "ci/x", state: "success" }] }), rules).eligible, true);
});

test("状態: behind / compare null / draft / closed / base / fork / mergeable", () => {
  assert.deepEqual(rulesOf(evaluate(base({ compare: { behind_by: 1 } }), rules)), ["state.behind"]);
  assert.deepEqual(rulesOf(evaluate(base({ compare: null }), rules)), ["state.behind"]);
  const p = (o) => ({ ...base().pr, ...o });
  assert.deepEqual(rulesOf(evaluate(base({ pr: p({ draft: true }) }), rules)), ["state.draft"]);
  assert.deepEqual(rulesOf(evaluate(base({ pr: p({ state: "closed" }) }), rules)), ["state.closed"]);
  assert.deepEqual(rulesOf(evaluate(base({ pr: p({ base: { ref: "dev", repo: { full_name: "H4aruki/MyTechPulse" } } }) }), rules)), ["state.base"]);
  assert.deepEqual(rulesOf(evaluate(base({ pr: p({ head: { sha: "a", ref: "b", repo: { full_name: "evil/MyTechPulse" } } }) }), rules)), ["state.fork"]);
  assert.deepEqual(rulesOf(evaluate(base({ pr: p({ mergeable_state: "blocked" }) }), rules)), ["state.mergeable"]);
});

test("人の拒否: ラベル・CHANGES_REQUESTED・未解決スレッド", () => {
  const p = { ...base().pr, labels: [{ name: "no-automerge" }] };
  assert.deepEqual(rulesOf(evaluate(base({ pr: p }), rules)), ["human.label"]);
  const rev = (state, at, login = "a") => ({ user: { login }, state, submitted_at: at });
  assert.deepEqual(rulesOf(evaluate(base({ reviews: [rev("CHANGES_REQUESTED", "2026-01-01")] }), rules)), ["human.changes-requested"]);
  assert.equal(evaluate(base({ reviews: [rev("CHANGES_REQUESTED", "2026-01-01"), rev("APPROVED", "2026-01-02")] }), rules).eligible, true);
  assert.equal(evaluate(base({ reviews: [rev("CHANGES_REQUESTED", "2026-01-01"), rev("DISMISSED", "2026-01-02")] }), rules).eligible, true);
  assert.deepEqual(rulesOf(evaluate(base({ reviews: [rev("CHANGES_REQUESTED", "2026-01-01"), rev("COMMENTED", "2026-01-02")] }), rules)), ["human.changes-requested"]);
  assert.deepEqual(rulesOf(evaluate(base({ reviews: [rev("APPROVED", "2026-01-01", "b"), rev("CHANGES_REQUESTED", "2026-01-02", "a")] }), rules)), ["human.changes-requested"]);
  assert.deepEqual(rulesOf(evaluate(base({ unresolvedThreads: 2 }), rules)), ["human.threads"]);
});

test("本文: 判断が必要なこと", () => {
  const body = (s) => ({ ...base().pr, body: s });
  const ok = (s) => evaluate(base({ pr: body(s) }), rules);
  assert.equal(ok("## 判断が必要なこと\nなし").eligible, true);
  assert.equal(ok("## 判断が必要なこと\r\n\r\nなし\r\n## 次").eligible, true);
  assert.equal(ok("## 判断が必要なこと\n<!-- 無いときは なし と書く\n複数行 -->\nなし\n").eligible, true);
  assert.equal(ok("## 判断が必要なこと\n<!-- c -->\n\nなし\n\n## 確認（Verification）\n要確認\n").eligible, true);
  assert.deepEqual(rulesOf(ok("## 判断が必要なこと\n要確認")), ["body.decision"]);
  assert.deepEqual(rulesOf(ok("## 判断が必要なこと\nなし。ただし要確認")), ["body.decision"]);
  assert.deepEqual(rulesOf(ok("## 判断が必要なこと\n<!-- なし -->\n")), ["body.decision"]);
  assert.deepEqual(rulesOf(ok("## なぜ（Why）\nなし")), ["body.decision"]);
  assert.deepEqual(rulesOf(ok(null)), ["body.decision"]);
  assert.deepEqual(rulesOf(ok("## 判断が必要なこと\n## 次\nなし")), ["body.decision"]);
});

test("merged-dryrun: 状態系は N/A、ほかは同じ", () => {
  const p = { ...base().pr, draft: true, state: "closed", merged: true, mergeable_state: "unknown" };
  let r = evaluate(base({ pr: p, compare: null, mode: "merged-dryrun" }), rules);
  assert.equal(r.eligible, true);
  assert.equal(r.class, "A");
  assert.ok(evaluate(base({ pr: p, compare: null }), rules).failures.length >= 4);
  r = evaluate(base({ pr: p, mode: "merged-dryrun", checks: goodChecks().slice(1) }), rules);
  assert.deepEqual(rulesOf(r), ["checks.required-missing"]);
});

test("class: パス系があればC、ほかだけならB、無ければA", () => {
  assert.equal(evaluate(base(), rules).class, "A");
  assert.equal(evaluate(base({ unresolvedThreads: 1 }), rules).class, "B");
  const r = evaluate(base({ unresolvedThreads: 1, files: [...base().files, file("ops/x.sh", 1)] }), rules);
  assert.equal(r.class, "C");
  assert.equal(r.eligible, false);
});

test("renderComment: 先頭行・判定・マージ・当たったルール", () => {
  const ok = renderComment(evaluate(base(), rules), { headSha: "3f1f9db0000", nowJst: "2026-11-13 10:12", mergeStatus: "保留（時間帯のため）" });
  const lines = ok.split("\n");
  assert.equal(lines[0], "<!-- automerge-eligibility -->");
  assert.equal(lines[1], "🤖 自動マージ判定（head 3f1f9db・2026-11-13 10:12 JST）");
  assert.ok(ok.includes("判定: ✅ 対象（クラスA）"));
  assert.ok(ok.includes("マージ: 保留（時間帯のため）"));
  assert.ok(ok.includes("| 項目 | 結果 |"));
  assert.ok(!ok.includes("❌ 問題"));
  const ng = renderComment(evaluate(base({ files: [...base().files, file("server/internal/auth/x.go", 1)] }), rules), { headSha: "abcdef123", nowJst: "t", mergeStatus: "しない" });
  assert.ok(ng.includes("判定: ❌ 対象外（クラスC）"));
  assert.ok(ng.includes("❌ `server/internal/auth/x.go`"));
});
