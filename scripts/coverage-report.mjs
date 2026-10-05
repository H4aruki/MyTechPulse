// カバレッジの計測結果を、PRコメント用の表（Markdown）にまとめる。
// Node標準APIだけで動く。
//   node scripts/coverage-report.mjs go <go testの出力ファイル> <表題>
//   node scripts/coverage-report.mjs frontend <coverage-summary.json> <表題>
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

// 同じコメントを探して更新するための目印（表題ごとに分ける）
export const marker = (title) => `<!-- coverage-report:${title} -->`;

export function goTable(log) {
  const rows = [];
  for (const line of log.split(/\r?\n/)) {
    // 例: ok  \tgithub.com/x/server/internal/app\t0.5s\tcoverage: 71.2% of statements
    const m = /^ok\s+(\S+)\s.*coverage:\s+([\d.]+)% of statements/.exec(line);
    if (m) rows.push({ name: m[1].replace(/^.*\/server\//, ""), pct: Number(m[2]) });
  }
  return rows;
}

export function frontendTable(summary) {
  const rows = [];
  for (const [name, v] of Object.entries(summary)) {
    if (name === "total") continue;
    rows.push({ name: name.replaceAll("\\", "/").replace(/^.*\/frontend\//, ""), pct: v.lines.pct });
  }
  return { rows, total: summary.total?.lines?.pct };
}

export function render(title, rows, total) {
  const sorted = [...rows].sort((a, b) => a.pct - b.pct);
  const calc =
    total ?? (rows.length ? rows.reduce((s, r) => s + r.pct, 0) / rows.length : undefined);
  const head = [
    marker(title),
    `### テストカバレッジ: ${title}`,
    "",
    calc === undefined ? "計測結果がありません。" : `${total === undefined ? "パッケージ平均" : "全体（行）"}: **${calc.toFixed(1)}%**`,
  ];
  if (sorted.length === 0) return head.join("\n") + "\n";
  const body = sorted.map((r) => `| \`${r.name}\` | ${r.pct.toFixed(1)}% |`);
  return [
    ...head,
    "",
    "<details><summary>内訳（低い順）</summary>",
    "",
    "| 対象 | カバー率 |",
    "|---|---|",
    ...body,
    "",
    "</details>",
    "",
  ].join("\n");
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const [kind, file, title] = process.argv.slice(2);
  if (kind === "go") {
    console.log(render(title, goTable(readFileSync(file, "utf8"))));
  } else if (kind === "frontend") {
    const { rows, total } = frontendTable(JSON.parse(readFileSync(file, "utf8")));
    console.log(render(title, rows, total));
  } else {
    console.error("使い方: coverage-report.mjs go|frontend <ファイル> <表題>");
    process.exit(1);
  }
}
