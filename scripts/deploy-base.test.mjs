import assert from "node:assert/strict";
import test from "node:test";
import { DEPLOY_JOB_NAME, findLastDeployedSha } from "./deploy-base.mjs";

const sha = (c) => c.repeat(40);
const jobsById = (table) => async (id) => table[id] ?? [];
const deployed = [{ name: DEPLOY_JOB_NAME, conclusion: "success" }];

test("反映が成功した最後の実行の commit を返す（失敗・取り消し・スキップは飛ばす）", async () => {
  const runs = [
    { id: 4, head_sha: sha("d") },
    { id: 3, head_sha: sha("c") },
    { id: 2, head_sha: sha("b") },
    { id: 1, head_sha: sha("a") },
  ];
  const listJobs = jobsById({
    4: [{ name: DEPLOY_JOB_NAME, conclusion: "skipped" }],
    3: [{ name: DEPLOY_JOB_NAME, conclusion: "failure" }],
    2: [{ name: "他のジョブ", conclusion: "success" }],
    1: deployed,
  });
  assert.equal(await findLastDeployedSha(runs, listJobs), sha("a"));
});

test("一番新しい成功を選ぶ", async () => {
  const runs = [
    { id: 2, head_sha: sha("b") },
    { id: 1, head_sha: sha("a") },
  ];
  assert.equal(await findLastDeployedSha(runs, jobsById({ 2: deployed, 1: deployed })), sha("b"));
});

test("見つからない・shaが不正なときは空（安全側）", async () => {
  assert.equal(await findLastDeployedSha([], jobsById({})), "");
  assert.equal(await findLastDeployedSha([{ id: 1, head_sha: sha("a") }], jobsById({})), "");
  assert.equal(await findLastDeployedSha([{ id: 1, head_sha: "xyz" }], jobsById({ 1: deployed })), "");
});

test("取得に失敗したら例外のまま伝える（CLIが空にして安全側へ倒す）", async () => {
  await assert.rejects(
    findLastDeployedSha([{ id: 1, head_sha: sha("a") }], async () => {
      throw new Error("api");
    }),
  );
});
