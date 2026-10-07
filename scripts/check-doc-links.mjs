// Markdown文書の、相対リンク（他のファイルへのリンク）が切れていないかを検査する（#129）。
// Node標準APIだけで動く。外部URL（https://）と、同じ文書内の見出しへのリンク（#...）は対象外。
//
//   node scripts/check-doc-links.mjs          追跡されている全Markdownを検査する
//   終了コード: 0=切れなし、1=切れあり（「文書:リンク先」を一覧する）
//
// 対象外（過去の実行計画・設計の記録で、削除済みのファイルへの言及が残るのが自然なもの）:
//   docs/superpowers/ 、取り込んだ外部Skillの本文（.claude/skills、.agents/skills）
import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const EXCLUDED = [/^docs\/superpowers\//, /^\.claude\/skills\//, /^\.agents\/skills\//, /^\.claude\/worktrees\//];

// `[文字](リンク先)` の、リンク先を取り出す。コードブロック（```）の中は、例示なので見ない
export function extractLinks(markdown) {
  const links = [];
  let inFence = false;
  for (const line of markdown.split(/\r?\n/)) {
    if (/^\s*```/.test(line)) {
      inFence = !inFence;
      continue;
    }
    if (inFence) continue;
    // インラインコード（`...`）の中も、例示なので見ない
    const withoutCode = line.replace(/`[^`]*`/g, "");
    for (const match of withoutCode.matchAll(/\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g)) {
      links.push(match[1]);
    }
  }
  return links;
}

// 切れている相対リンクを返す。存在確認は exists（テストで差し替える）に任せる
export function findBrokenLinks(file, markdown, exists) {
  const base = path.posix.dirname(file);
  const broken = [];
  for (const link of extractLinks(markdown)) {
    if (/^(?:[a-z][a-z0-9+.-]*:|#)/i.test(link)) continue; // https:// mailto: 文書内の見出し
    const target = link.split("#")[0].split("?")[0];
    if (target === "") continue;
    const resolved = target.startsWith("/") ? target.slice(1) : path.posix.normalize(path.posix.join(base, target));
    if (!exists(resolved)) broken.push(link);
  }
  return broken;
}

export function trackedMarkdown(root) {
  const out = execFileSync("git", ["ls-files", "-z", "--", "*.md"], { cwd: root, encoding: "utf8" });
  return out.split("\0").filter((file) => file && !EXCLUDED.some((pattern) => pattern.test(file)));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  let count = 0;
  for (const file of trackedMarkdown(root)) {
    const markdown = readFileSync(path.join(root, file), "utf8");
    for (const link of findBrokenLinks(file, markdown, (resolved) => existsSync(path.join(root, resolved)))) {
      console.log(`${file}: ${link}`);
      count += 1;
    }
  }
  if (count > 0) {
    console.error(`リンク切れが ${count} 件あります`);
    process.exit(1);
  }
  console.log("リンク切れはありません");
}
