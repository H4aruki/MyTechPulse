// 変更されたファイルの一覧（標準入力。1行に1つ）から、本番への反映が要るかを判定する（#125）。
// 自動デプロイ（.github/workflows/ci.yml）が使う。Node標準APIだけで動く。
//
//   git diff --name-only <前> <後> | node scripts/deploy-needed.mjs
//   出力: needed=true または needed=false（GitHub Actionsの GITHUB_OUTPUT へそのまま追記できる）
//
// 反映が要るのは、本番で動くものや、その配布の作り方が変わったとき。
// 文書・試験だけの変更では、同じ箱を入れ替え直す意味がないので、反映しない。
import { fileURLToPath } from "node:url";

// 本番で動くもの・その配布の作り方
const DEPLOY_PATHS = [
  /^server\//, // Go版API
  /^frontend\//, // 画面
  /^ops\//, // 本番で実行する運用スクリプト
  /^docker-compose\.yml$/,
  /^Caddyfile$/,
  /^scripts\/(release-manifest|cutover-frontend|check-go-image)\.m?js$/,
  /^scripts\/check-go-image\.sh$/,
  /^\.github\/workflows\/(ci|release)\.yml$/,
];

// 上に当たっても、反映しないもの（試験だけの変更）
const IGNORED_PATHS = [/_test\.go$/, /\.test\.(ts|tsx|mjs|js)$/, /^ops\/.*_test\.sh$/, /^ops\/tests\//];

export function needsDeploy(files) {
  return files
    .map((file) => file.trim())
    .filter((file) => file.length > 0)
    .some((file) => DEPLOY_PATHS.some((pattern) => pattern.test(file)) && !IGNORED_PATHS.some((pattern) => pattern.test(file)));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const chunks = [];
  process.stdin.on("data", (chunk) => chunks.push(chunk));
  process.stdin.on("end", () => {
    const files = Buffer.concat(chunks).toString("utf8").split(/\r?\n/);
    console.log(`needed=${needsDeploy(files)}`);
  });
}
