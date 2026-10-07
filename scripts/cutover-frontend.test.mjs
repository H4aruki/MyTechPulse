// scripts/cutover-frontend.mjs と .github/workflows/cutover-frontend.yml の確認（#163）。
// 本物のCloudflare・GitHubには接続しない。トークン・アカウントIDは、すべて合成のダミー。
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { InputError, checkReleaseRun, createClient, findPreviousProduction, run } from "./cutover-frontend.mjs";

const TOKEN = "synthetic-token-0001-do-not-print";
const ACCOUNT = "0123456789abcdef0123456789abcdef";
const OLD_ID = "11111111-1111-4111-8111-111111111111";
const NEW_ID = "22222222-2222-4222-8222-222222222222";
const OLDER_ID = "33333333-3333-4333-8333-333333333333";
const SHA = "0123456789abcdef0123456789abcdef01234567";

const okRun = { workflowName: "Release artifacts", conclusion: "success", headBranch: "main", headSha: SHA };

function deployment(id, { environment = "production", name = "deploy", status = "success" } = {}) {
  return { id, environment, latest_stage: { name, status } };
}

// 偽のCloudflare。呼び出しを記録し、いまの本番（canonical）を持つ
function fakeCloudflare({ current, list = [], rollbackMovesTo = undefined, failOn = null } = {}) {
  const state = { current, calls: [] };
  const fetchImpl = async (url, init) => {
    state.calls.push({ url, method: init.method, auth: init.headers.Authorization, body: init.body });
    const respond = (result, ok = true, status = 200) => ({
      ok,
      status,
      json: async () => ({ success: ok, result }),
    });
    if (failOn && url.includes(failOn)) return respond(null, false, 500);
    if (init.method === "POST") {
      const id = url.split("/deployments/")[1].split("/")[0];
      state.current = rollbackMovesTo ?? id;
      return respond({ id });
    }
    if (url.includes("/deployments?")) return respond(list);
    return respond({ canonical_deployment: { id: state.current } });
  };
  return { state, fetchImpl };
}

function clientFor(fake) {
  return createClient({ token: TOKEN, accountId: ACCOUNT, project: "mytechpulse", fetchImpl: fake.fetchImpl });
}

// run() の標準出力を集める
async function capture(argv, { env = {}, clientFactory } = {}) {
  const lines = [];
  const original = console.log;
  console.log = (line) => lines.push(String(line));
  try {
    await run(argv, env, undefined, clientFactory);
  } finally {
    console.log = original;
  }
  return lines;
}

// ---- 配布の実行回の確認 ----

test("成功したmainの配布の実行回なら、commitを返す", () => {
  assert.equal(checkReleaseRun(okRun), SHA);
});

test("別のworkflow・失敗した回・main以外・不正なcommitは拒否する", () => {
  for (const bad of [
    { ...okRun, workflowName: "CI" },
    { ...okRun, conclusion: "failure" },
    { ...okRun, conclusion: null },
    { ...okRun, headBranch: "feature" },
    { ...okRun, headSha: "abc" },
    null,
  ]) {
    assert.throws(() => checkReleaseRun(bad), InputError);
  }
});

// ---- 切り戻し先の選び方 ----

test("いまの公開の1つ前の、成功した本番の公開を選ぶ", () => {
  const list = [deployment(NEW_ID), deployment(OLD_ID), deployment(OLDER_ID)];
  assert.equal(findPreviousProduction(list, NEW_ID), OLD_ID);
});

test("失敗した公開・preview・途中のものは、戻す先にしない", () => {
  const list = [
    deployment(NEW_ID),
    deployment("44444444-4444-4444-8444-444444444444", { status: "failure" }),
    deployment("55555555-5555-4555-8555-555555555555", { environment: "preview" }),
    deployment("66666666-6666-4666-8666-666666666666", { name: "build", status: "active" }),
    deployment(OLD_ID),
  ];
  assert.equal(findPreviousProduction(list, NEW_ID), OLD_ID);
});

test("戻す先が無ければnullを返す", () => {
  assert.equal(findPreviousProduction([deployment(NEW_ID)], NEW_ID), null);
  assert.equal(findPreviousProduction([], NEW_ID), null);
  assert.throws(() => findPreviousProduction("x", NEW_ID), InputError);
});

// ---- 入力の確認 ----

test("認証情報・アカウントID・プロジェクト名が不正なら、通信せずに拒否する", () => {
  const fetchImpl = async () => assert.fail("通信してはいけない");
  assert.throws(() => createClient({ token: "", accountId: ACCOUNT, project: "mytechpulse", fetchImpl }), InputError);
  assert.throws(() => createClient({ token: TOKEN, accountId: "short", project: "mytechpulse", fetchImpl }), InputError);
  assert.throws(() => createClient({ token: TOKEN, accountId: ACCOUNT, project: "../x", fetchImpl }), InputError);
  assert.throws(() => createClient({ token: TOKEN, accountId: ACCOUNT, project: "UPPER", fetchImpl }), InputError);
});

test("使い方が違えば拒否する", async () => {
  await assert.rejects(() => run([], {}), InputError);
  await assert.rejects(() => run(["unknown"], {}), InputError);
  await assert.rejects(() => run(["after-publish"], {}), InputError);
  await assert.rejects(() => run(["after-publish", "not-a-uuid"], {}), InputError);
  await assert.rejects(() => run(["rollback", "--to"], {}), InputError);
  await assert.rejects(() => run(["rollback", "--to", "not-a-uuid"], {}), InputError);
  await assert.rejects(() => run(["rollback", "extra"], {}), InputError);
  await assert.rejects(() => run(["check-run"], {}), InputError);
});

// ---- 各コマンド ----

test("check-run: 実行回の情報から、commitだけを出力する", async () => {
  const out = [];
  const original = console.log;
  console.log = (line) => out.push(line);
  try {
    await run(["check-run", "run.json"], {}, () => JSON.stringify(okRun));
  } finally {
    console.log = original;
  }
  assert.deepEqual(out, [`commit_sha=${SHA}`]);
  await assert.rejects(() => run(["check-run", "run.json"], {}, () => "{broken"), InputError);
  await assert.rejects(() => run(["check-run", "run.json"], {}, () => JSON.stringify({ ...okRun, conclusion: "failure" })), InputError);
});

test("current: いまの本番の識別子を出力する（トークンは出さない）", async () => {
  const fake = fakeCloudflare({ current: OLD_ID });
  const lines = await capture(["current"], { clientFactory: () => clientFor(fake) });
  assert.deepEqual(lines, [`current_id=${OLD_ID}`]);
  assert.equal(fake.state.calls.length, 1);
  assert.equal(fake.state.calls[0].method, "GET");
  assert.equal(fake.state.calls[0].url, `https://api.cloudflare.com/client/v4/accounts/${ACCOUNT}/pages/projects/mytechpulse`);
  assert.equal(fake.state.calls[0].auth, `Bearer ${TOKEN}`);
  assert.ok(!lines.join("\n").includes(TOKEN));
});

test("after-publish: 本番が入れ替わっていれば、切り戻し先（公開前のID）を出す", async () => {
  const fake = fakeCloudflare({ current: NEW_ID });
  const lines = await capture(["after-publish", OLD_ID], { clientFactory: () => clientFor(fake) });
  assert.deepEqual(lines, [`current_id=${NEW_ID}`, `previous_id=${OLD_ID}`]);
});

test("after-publish: 本番が入れ替わっていなければ失敗する", async () => {
  const fake = fakeCloudflare({ current: OLD_ID });
  await assert.rejects(() => capture(["after-publish", OLD_ID], { clientFactory: () => clientFor(fake) }), /入れ替わっていません/);
});

test("rollback: 指定が無ければ1つ前の公開へ戻し、戻った後の状態を出す", async () => {
  const fake = fakeCloudflare({ current: NEW_ID, list: [deployment(NEW_ID), deployment(OLD_ID)] });
  const lines = await capture(["rollback"], { clientFactory: () => clientFor(fake) });
  assert.deepEqual(lines, [`before_id=${NEW_ID}`, `target_id=${OLD_ID}`, `current_id=${OLD_ID}`]);
  const post = fake.state.calls.find((call) => call.method === "POST");
  assert.ok(post, "切り戻しのPOSTが無い");
  assert.equal(
    post.url,
    `https://api.cloudflare.com/client/v4/accounts/${ACCOUNT}/pages/projects/mytechpulse/deployments/${OLD_ID}/rollback`,
  );
  assert.equal(post.auth, `Bearer ${TOKEN}`);
});

test("rollback: --to で戻す先を指定できる（一覧は見ない）", async () => {
  const fake = fakeCloudflare({ current: NEW_ID });
  const lines = await capture(["rollback", "--to", OLDER_ID], { clientFactory: () => clientFor(fake) });
  assert.deepEqual(lines, [`before_id=${NEW_ID}`, `target_id=${OLDER_ID}`, `current_id=${OLDER_ID}`]);
  assert.ok(!fake.state.calls.some((call) => call.url.includes("/deployments?")));
});

test("rollback: 戻す先が無い・いまと同じ場合は、何も変えずに失敗する", async () => {
  const none = fakeCloudflare({ current: NEW_ID, list: [deployment(NEW_ID)] });
  await assert.rejects(() => capture(["rollback"], { clientFactory: () => clientFor(none) }), /見つかりません/);
  assert.ok(!none.state.calls.some((call) => call.method === "POST"));

  const same = fakeCloudflare({ current: NEW_ID });
  await assert.rejects(() => capture(["rollback", "--to", NEW_ID], { clientFactory: () => clientFor(same) }), InputError);
  assert.ok(!same.state.calls.some((call) => call.method === "POST"));
});

test("rollback: 戻した後の公開が戻す先と違えば、成功扱いにしない", async () => {
  const fake = fakeCloudflare({ current: NEW_ID, list: [deployment(NEW_ID), deployment(OLD_ID)], rollbackMovesTo: NEW_ID });
  await assert.rejects(() => capture(["rollback"], { clientFactory: () => clientFor(fake) }), /一致しません/);
});

test("APIが失敗したときは、トークンを含まないメッセージで失敗する", async () => {
  const fake = fakeCloudflare({ current: NEW_ID, failOn: "/pages/projects/mytechpulse" });
  await assert.rejects(
    () => capture(["current"], { clientFactory: () => clientFor(fake) }),
    (error) => /HTTP 500/.test(error.message) && !error.message.includes(TOKEN),
  );
  const broken = createClient({
    token: TOKEN,
    accountId: ACCOUNT,
    project: "mytechpulse",
    fetchImpl: async () => {
      throw new Error(`connect failed with ${TOKEN}`);
    },
  });
  await assert.rejects(
    () => broken.currentProductionId(),
    (error) => !error.message.includes(TOKEN),
  );
});

// ---- workflow の文面 ----

const workflow = readFileSync(fileURLToPath(new URL("../.github/workflows/cutover-frontend.yml", import.meta.url)), "utf8");

test("workflow: 手動実行だけで、mainでしか動かない", () => {
  assert.match(workflow, /^on:\s*\n\s+workflow_dispatch:/m);
  for (const trigger of ["push:", "pull_request", "schedule:"]) {
    assert.ok(!new RegExp(`^\\s{2}${trigger}`, "m").test(workflow), `${trigger} を起動条件にしてはいけません`);
  }
  const conditions = workflow.split(/\r?\n/).filter((line) => /^\s{4}if:/.test(line));
  assert.ok(conditions.length >= 2);
  for (const condition of conditions) assert.match(condition, /github\.ref == 'refs\/heads\/main'/);
});

test("workflow: 入力は環境変数へ渡してから使い、run の中へ直接埋め込まない", () => {
  const lines = workflow.split(/\r?\n/);
  lines.forEach((line, index) => {
    if (!/\$\{\{\s*(inputs\.|github\.event\.inputs)/.test(line)) return;
    const allowed = /^\s+[A-Z0-9_]+:\s+\$\{\{\s*inputs\.[a-z0-9_]+\s*\}\}\s*$/.test(line) || /^\s{4}if:/.test(line);
    assert.ok(allowed, `入力を直接使っています（${index + 1}行目）: ${line.trim()}`);
  });
});

test("workflow: 権限は必要最小限で、秘密はこのworkflowの公開・切り戻しの工程だけが使う", () => {
  assert.match(workflow, /^permissions:\s*\{\}\s*$/m);
  assert.ok(!/packages:\s*write/.test(workflow));
  assert.ok(!/contents:\s*write/.test(workflow));
  assert.ok(!/ssh|LIGHTSAIL/i.test(workflow), "本番サーバーへの接続は含めません");
});
