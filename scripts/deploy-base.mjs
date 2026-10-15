// 「本番への反映が要るか」の比較の起点を決める（#212）。
// 起点は、ジョブ「Go版の本番への反映」が成功した最後の main の実行の commit。
// 直前の push ではなくこれを使うことで、途中の実行が失敗・取り消しになっても、その間の変更が漏れない。
// 自動デプロイ（.github/workflows/ci.yml）が使う。Node標準APIだけで動く。読み取りだけで、何も書き込まない。
//
//   GH_TOKEN=... node scripts/deploy-base.mjs <owner/repo>
//   出力: base=<40桁のsha>（見つからなければ base= 。呼び出し側は安全側で needed=true にする）
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";

export const DEPLOY_JOB_NAME = "Go版の本番への反映";
const MAX_RUNS = 100; // 古い実行まで辿りすぎない。見つからなければ安全側（呼び出し側）へ

// runs: 新しい順の完了済みの実行 [{id, head_sha}]。listJobs(runId): その実行のジョブ [{name, conclusion}]
export async function findLastDeployedSha(runs, listJobs) {
  for (const run of runs.slice(0, MAX_RUNS)) {
    const jobs = await listJobs(run.id);
    if (jobs.some((job) => job.name === DEPLOY_JOB_NAME && job.conclusion === "success")) {
      return /^[0-9a-f]{40}$/.test(run.head_sha) ? run.head_sha : "";
    }
  }
  return "";
}

async function main() {
  const repo = process.argv[2];
  const run = promisify(execFile);
  const gh = async (path) => JSON.parse((await run("gh", ["api", path], { maxBuffer: 64 * 1024 * 1024 })).stdout);
  const runs = (await gh(`repos/${repo}/actions/workflows/ci.yml/runs?branch=main&event=push&status=completed&per_page=${MAX_RUNS}`)).workflow_runs;
  const listJobs = async (id) => (await gh(`repos/${repo}/actions/runs/${id}/jobs?per_page=100`)).jobs;
  console.log(`base=${await findLastDeployedSha(runs, listJobs)}`);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  main().catch(() => console.log("base=")); // 取得できないときも空にして、安全側へ倒す
}
