// .github/workflows 内の外部 `uses:` が、40桁commit SHAで固定されているかを検査する。
// Node標準APIだけで動く。違反時は「ファイル:行」とaction名だけを出力する
// （ファイル本文やsecret値は出さない）。
import { readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const USES_LINE = /^\s*(?:-\s+)?uses:\s*(.+?)\s*$/;
const COMMIT_SHA = /^[0-9a-f]{40}$/;
const DOCKER_DIGEST = /^docker:\/\/.+@sha256:[0-9a-f]{64}$/;

function stripValue(raw) {
  // 末尾コメントを除き、前後の引用符を外す
  let value = raw.replace(/\s+#.*$/, "").trim();
  const quote = value[0];
  if ((quote === '"' || quote === "'") && value.endsWith(quote) && value.length >= 2) {
    value = value.slice(1, -1);
  }
  return value;
}

export function findUnpinnedActions(text, file) {
  const violations = [];
  text.split(/\r?\n/).forEach((lineText, index) => {
    const match = USES_LINE.exec(lineText);
    if (!match) return;
    const value = stripValue(match[1]);
    if (value.startsWith("./") || value.startsWith("../")) return; // local action
    if (value.startsWith("docker://")) {
      if (!DOCKER_DIGEST.test(value)) {
        violations.push({ file, line: index + 1, action: value.split("@")[0] });
      }
      return;
    }
    const at = value.lastIndexOf("@");
    const action = at === -1 ? value : value.slice(0, at);
    const ref = at === -1 ? "" : value.slice(at + 1);
    if (!COMMIT_SHA.test(ref)) {
      violations.push({ file, line: index + 1, action });
    }
  });
  return violations;
}

function workflowFiles(root) {
  const dir = path.join(root, ".github", "workflows");
  let names;
  try {
    names = readdirSync(dir);
  } catch {
    return [];
  }
  return names
    .filter((name) => name.endsWith(".yml") || name.endsWith(".yaml"))
    .sort()
    .map((name) => path.join(dir, name));
}

function main() {
  const root = process.cwd();
  const violations = [];
  for (const file of workflowFiles(root)) {
    const relative = path.relative(root, file).split(path.sep).join("/");
    violations.push(...findUnpinnedActions(readFileSync(file, "utf8"), relative));
  }
  if (violations.length > 0) {
    for (const v of violations) {
      console.error(`${v.file}:${v.line}: ${v.action} が40桁のcommit SHAで固定されていません`);
    }
    process.exit(1);
  }
  console.log("すべての外部actionがcommit SHAで固定されています");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main();
}
