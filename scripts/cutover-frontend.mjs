// 本番切り替えで、画面（Cloudflare Pages）の公開と切り戻しに使う部品（#163）。
// .github/workflows/cutover-frontend.yml から呼ばれる。Node標準APIだけで動く。
//
//   node scripts/cutover-frontend.mjs check-run <run.json>      配布の実行回が、使ってよいものか確かめる
//   node scripts/cutover-frontend.mjs current                   いま本番に公開されている画面の識別子を出す
//   node scripts/cutover-frontend.mjs after-publish <公開前のID>  公開で本番が入れ替わったか確かめる
//   node scripts/cutover-frontend.mjs rollback [--to <ID>]      本番を、直前の公開（または指定した公開）へ戻す
//
// 入力（環境変数）: CLOUDFLARE_API_TOKEN / CLOUDFLARE_ACCOUNT_ID（必須。値は出力しない）、
//   MTP_PAGES_PROJECT（任意。既定 mytechpulse）
// 出力は「項目=値」の行だけ（GitHub Actionsの GITHUB_OUTPUT へそのまま追記できる）。トークンは出さない。
// 終了コード: 0=成功、1=失敗（通信・API・状態の不一致）、2=入力の拒否
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const API_BASE = "https://api.cloudflare.com/client/v4";
const PROJECT_NAME = /^[a-z0-9][a-z0-9-]*$/;
const ACCOUNT_ID = /^[0-9a-f]{32}$/;
const DEPLOYMENT_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const RELEASE_WORKFLOW_NAME = "Release artifacts";

// 入力を拒否するときの例外。CLIでは終了コード2にする
export class InputError extends Error {}

function reject(message) {
  throw new InputError(message);
}

// 配布の実行回が、使ってよいものか。別のworkflow・失敗した回・main以外の回の成果物では公開しない
export function checkReleaseRun(run) {
  if (run === null || typeof run !== "object") reject("実行回の情報を読み取れません");
  if (run.workflowName !== RELEASE_WORKFLOW_NAME) reject("配布のworkflowの実行回ではありません");
  if (run.conclusion !== "success") reject("成功した実行回ではありません");
  if (run.headBranch !== "main") reject("main の実行回ではありません");
  if (typeof run.headSha !== "string" || !/^[0-9a-f]{40}$/.test(run.headSha)) reject("実行回のcommitを読み取れません");
  return run.headSha;
}

function isSuccessfulDeploy(deployment) {
  return deployment?.latest_stage?.name === "deploy" && deployment?.latest_stage?.status === "success";
}

// 一覧（新しい順）から、いまの公開の1つ前の「成功した本番の公開」を選ぶ
export function findPreviousProduction(deployments, currentId) {
  if (!Array.isArray(deployments)) reject("公開の一覧を読み取れません");
  const previous = deployments.find(
    (deployment) =>
      deployment?.environment === "production" && deployment.id !== currentId && isSuccessfulDeploy(deployment),
  );
  return previous?.id ?? null;
}

export function createClient({ token, accountId, project, fetchImpl = fetch }) {
  if (typeof token !== "string" || token.length === 0) reject("CLOUDFLARE_API_TOKEN が未設定です");
  if (!ACCOUNT_ID.test(accountId ?? "")) reject("CLOUDFLARE_ACCOUNT_ID の形式が正しくありません");
  if (!PROJECT_NAME.test(project ?? "")) reject("Pagesのプロジェクト名の形式が正しくありません");
  const base = `${API_BASE}/accounts/${accountId}/pages/projects/${project}`;

  async function call(method, url, body) {
    let response;
    try {
      response = await fetchImpl(url, {
        method,
        headers: {
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/json",
        },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    } catch {
      // 通信の失敗は、宛先や認証情報を含みうる詳細を出さずに知らせる
      throw new Error("Cloudflareへの接続に失敗しました");
    }
    let payload = null;
    try {
      payload = await response.json();
    } catch {
      // 本文を読めない場合は、下の成否の判定で失敗にする
    }
    if (!response.ok || payload?.success !== true) {
      throw new Error(`Cloudflare APIが失敗しました（HTTP ${response.status}）`);
    }
    return payload.result;
  }

  return {
    // いま本番に公開されている画面（canonical）の識別子
    async currentProductionId() {
      const result = await call("GET", base);
      const id = result?.canonical_deployment?.id;
      if (typeof id !== "string" || !DEPLOYMENT_ID.test(id)) throw new Error("いまの公開の識別子を読み取れません");
      return id;
    },
    async listProduction() {
      return call("GET", `${base}/deployments?env=production&per_page=20`);
    },
    async rollback(deploymentId) {
      if (!DEPLOYMENT_ID.test(deploymentId)) reject("切り戻し先の識別子の形式が正しくありません");
      await call("POST", `${base}/deployments/${deploymentId}/rollback`, {});
    },
  };
}

function printPairs(pairs) {
  for (const [key, value] of Object.entries(pairs)) {
    console.log(`${key}=${value}`);
  }
}

function clientFromEnv(env) {
  return createClient({
    token: env.CLOUDFLARE_API_TOKEN,
    accountId: env.CLOUDFLARE_ACCOUNT_ID,
    project: env.MTP_PAGES_PROJECT ?? "mytechpulse",
  });
}

export async function run(argv, env, readFile = (file) => readFileSync(file, "utf8"), clientFactory = clientFromEnv) {
  const [command, ...rest] = argv;
  switch (command) {
    case "check-run": {
      if (rest.length !== 1) reject("使い方: check-run <run.json>");
      let parsed;
      try {
        parsed = JSON.parse(readFile(rest[0]));
      } catch {
        reject("実行回の情報を読み取れません");
      }
      printPairs({ commit_sha: checkReleaseRun(parsed) });
      return;
    }
    case "current": {
      printPairs({ current_id: await clientFactory(env).currentProductionId() });
      return;
    }
    case "after-publish": {
      const before = rest[0];
      if (rest.length !== 1 || !DEPLOYMENT_ID.test(before ?? "")) reject("使い方: after-publish <公開前のID>");
      const current = await clientFactory(env).currentProductionId();
      if (current === before) throw new Error("公開しても、本番の画面が入れ替わっていません");
      // 切り戻しで戻す先は、公開前のID
      printPairs({ current_id: current, previous_id: before });
      return;
    }
    case "rollback": {
      let target = null;
      if (rest.length === 2 && rest[0] === "--to") target = rest[1];
      else if (rest.length !== 0) reject("使い方: rollback [--to <ID>]");
      if (target !== null && !DEPLOYMENT_ID.test(target)) reject("--to の識別子の形式が正しくありません");
      const client = clientFactory(env);
      const before = await client.currentProductionId();
      if (target === null) {
        target = findPreviousProduction(await client.listProduction(), before);
        if (target === null) throw new Error("戻す先になる、1つ前の成功した公開が見つかりません（--to で指定してください）");
      }
      if (target === before) reject("戻す先が、いまの公開と同じです");
      await client.rollback(target);
      const after = await client.currentProductionId();
      printPairs({ before_id: before, target_id: target, current_id: after });
      if (after !== target) {
        // 戻したはずの先と、いまの公開が違う。そのまま成功扱いにしない
        // 戻した後に識別子が付け替わる仕様の可能性もあるため、失敗にしたうえで、画面での確認を求める
        throw new Error(
          "切り戻しの後の公開が、戻す先と一致しません。Cloudflareの管理画面（Deployments）で、本番が戻す先になっているか確認してください",
        );
      }
      return;
    }
    default:
      reject("使い方: check-run | current | after-publish | rollback");
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  run(process.argv.slice(2), process.env).catch((error) => {
    console.error(`cutover-frontend: ${error.message}`);
    process.exit(error instanceof InputError ? 2 : 1);
  });
}
