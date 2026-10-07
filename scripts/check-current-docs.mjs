// 現行の説明に、Python版（FastAPI・JWT）時代の言葉が混ざっていないかを検査する（#129）。
// Node標準APIだけで動く。
//
//   node scripts/check-current-docs.mjs     対象の文書を検査する
//   終了コード: 0=混ざりなし、1=混ざりあり（「文書:行:言葉」を一覧する）
//
// 対象は「いまの姿」を書く文書だけ。過去の経緯を残す文書（ADR、docs/superpowers/、
// 切り戻し手順（docs/deploy/rollback-to-python.md）、取り込んだ外部Skill）は対象外。
// 行に「旧版」「Python版」「_Avoid_」のいずれかが含まれる行は、
// 過去との違いを説明しているので許す。
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const TARGETS = [
  /^README\.md$/,
  /^CONTRIBUTING\.md$/,
  /^CONTEXT\.md$/,
  /^AGENTS\.md$/,
  /^frontend\/README\.md$/,
  /^docs\/RequirementsSpecification\.md$/,
  /^docs\/BasicDesignSpecifications\//,
  /^docs\/deploy\/(?!rollback-to-python\.md$)/,
];

const STALE = [/\bJWT\b/, /access_token/, /localStorage/, /\bBearer\b/, /FastAPI/, /SQLAlchemy/, /uvicorn/, /backend\//, /Oracle/];
const ALLOWED_LINE = /旧版|Python版|_Avoid_/;

// 混ざっている行を {line, term} で返す。コードブロックの中も見る（例が古いのも誤りのため）
export function findStaleTerms(markdown) {
  const found = [];
  markdown.split(/\r?\n/).forEach((text, index) => {
    if (ALLOWED_LINE.test(text)) return;
    for (const pattern of STALE) {
      const match = text.match(pattern);
      if (match) found.push({ line: index + 1, term: match[0] });
    }
  });
  return found;
}

export function targetFiles(root) {
  const out = execFileSync("git", ["ls-files", "-z", "--", "*.md"], { cwd: root, encoding: "utf8" });
  return out.split("\0").filter((file) => file && TARGETS.some((pattern) => pattern.test(file)));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  let count = 0;
  for (const file of targetFiles(root)) {
    for (const { line, term } of findStaleTerms(readFileSync(path.join(root, file), "utf8"))) {
      console.log(`${file}:${line}: ${term}`);
      count += 1;
    }
  }
  if (count > 0) {
    console.error(`旧構成の言葉が ${count} か所に残っています。現行の説明なら直し、過去の説明なら「旧版」を添えてください`);
    process.exit(1);
  }
  console.log("旧構成の言葉の混ざりはありません");
}
