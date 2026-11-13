// 自動マージの判定を、GitHub のデータで動かす（読むだけ）。Node 標準だけで動く。
// PRのコードは checkout も実行もしない。取るのは `gh api` の JSON だけ。
//
//   node scripts/automerge/eligibility.mjs --pr N [--repo owner/repo] [--rules path] [--json] [--comment-markdown] [--merge-status "文字列"]
//   node scripts/automerge/eligibility.mjs --render-comment result.json [--merge-status "文字列"]   （ネットワークを使わない）
//   node scripts/automerge/eligibility.mjs --dry-run-merged 40 [--out file.md]
//
// 終了コード: 0=対象、1=対象外、2=取得失敗・引数エラー（取得できないときは対象外＝fail closed）
import { execFileSync } from "node:child_process";
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { evaluate, renderComment } from "./eligibility-core.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
export const DEFAULT_RULES = resolve(HERE, "../../.github/automerge-rules.json");

const ghRun = (args) =>
  execFileSync("gh", args, { encoding: "utf8", maxBuffer: 256 * 1024 * 1024, stdio: ["ignore", "pipe", "pipe"] });

const sleepSync = (ms) => Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);

// 一時的な失敗（タイムアウト・接続エラー・5xx）だけ再試行する。404 や引数エラーは再試行しない。
export function isTransient(e) {
  const msg = `${e?.message ?? ""}\n${e?.stderr ?? ""}`;
  if (/HTTP 4\d\d/.test(msg)) return false;
  return /timeout|timed out|ETIMEDOUT|ECONNRESET|ECONNREFUSED|EAI_AGAIN|connection (?:reset|refused)|TLS handshake|HTTP 5\d\d/i.test(msg);
}

// 最大3回（待ちは 2秒→5秒）。run と sleep は差し込める。
export function execWithRetry(run, args, { sleep = sleepSync, delays = [2000, 5000] } = {}) {
  for (let i = 0; ; i++) {
    try {
      return run(args);
    } catch (e) {
      if (i >= delays.length || !isTransient(e)) throw e;
      sleep(delays[i]);
    }
  }
}

// 既定の取得関数。gh を呼んで標準出力を返す。失敗したら例外。
export function ghExec(args) {
  return execWithRetry(ghRun, args);
}

// gh の出力（JSON）を返す。paginate のときはページを平らにする。
export function makeApi(exec = ghExec) {
  const get = (path, { paginate = false } = {}) => {
    const args = ["api", path];
    if (paginate) args.push("--paginate", "--slurp");
    const out = exec(args);
    const data = JSON.parse(out);
    if (!paginate) return data;
    return data.flatMap((page) => (Array.isArray(page) ? page : [page]));
  };
  const graphql = (query, vars) => {
    const args = ["api", "graphql", "-f", `query=${query}`];
    for (const [k, v] of Object.entries(vars)) args.push(typeof v === "number" ? "-F" : "-f", `${k}=${v}`);
    return JSON.parse(exec(args));
  };
  return { get, graphql };
}

const THREADS_QUERY = `query($owner:String!,$name:String!,$number:Int!,$after:String){repository(owner:$owner,name:$name){pullRequest(number:$number){reviewThreads(first:100,after:$after){pageInfo{hasNextPage endCursor}nodes{isResolved}}}}}`;

function countUnresolved(api, repo, number) {
  const [owner, name] = repo.split("/");
  let after = "";
  let n = 0;
  for (let i = 0; i < 50; i++) {
    const vars = { owner, name, number };
    if (after) vars.after = after;
    const r = api.graphql(THREADS_QUERY, vars);
    const t = r?.data?.repository?.pullRequest?.reviewThreads;
    if (!t) throw new Error("reviewThreads を取得できない");
    n += t.nodes.filter((x) => !x.isResolved).length;
    if (!t.pageInfo?.hasNextPage) return n;
    after = t.pageInfo.endCursor;
  }
  throw new Error("reviewThreads のページが多すぎる");
}

// main の server/go.mod の require にあるモジュールパス
export function parseGoMod(text) {
  const out = [];
  let inBlock = false;
  for (const raw of String(text).split(/\r?\n/)) {
    const line = raw.replace(/\/\/.*$/, "").trim();
    if (/^require\s*\($/.test(line)) inBlock = true;
    else if (inBlock && line === ")") inBlock = false;
    else {
      const m = inBlock ? /^(\S+)\s+v\S+/.exec(line) : /^require\s+(\S+)\s+v\S+/.exec(line);
      if (m) out.push(m[1]);
    }
  }
  return out;
}

function baseGoModules(api, repo) {
  try {
    const c = api.get(`repos/${repo}/contents/server/go.mod?ref=main`);
    return parseGoMod(Buffer.from(c.content ?? "", c.encoding === "base64" ? "base64" : "utf8").toString("utf8"));
  } catch {
    return null; // 取れなければ外部 import は全部失敗（fail closed）
  }
}

// 評価したSHAでの実体の種類（file / symlink / submodule / dir）。取れなければ null。
function fileKind(api, repo, path, sha) {
  try {
    const enc = path.split("/").map(encodeURIComponent).join("/");
    const c = api.get(`repos/${repo}/contents/${enc}?ref=${sha}`);
    return Array.isArray(c) ? "dir" : (c?.type ?? null);
  } catch {
    return null;
  }
}

function baseDeps(api, repo) {
  const c = api.get(`repos/${repo}/contents/frontend/package.json?ref=main`);
  const pkg = JSON.parse(Buffer.from(c.content ?? "", c.encoding === "base64" ? "base64" : "utf8").toString("utf8"));
  return [...Object.keys(pkg.dependencies ?? {}), ...Object.keys(pkg.devDependencies ?? {})];
}

// evaluate に渡す入力を組み立てる。失敗したら例外を投げる（呼び出し側が対象外にする）。
export function gatherInput(api, repo, number, mode = "open") {
  const pr = api.get(`repos/${repo}/pulls/${number}`);
  const headSha = pr?.head?.sha;
  if (!headSha) throw new Error("head SHA を取得できない");
  const rawFiles = api.get(`repos/${repo}/pulls/${number}/files?per_page=100`, { paginate: true });
  const files = rawFiles.map((f) => (f.status === "removed" ? f : { ...f, kind: fileKind(api, repo, f.filename, headSha) }));
  const checks = api.get(`repos/${repo}/commits/${headSha}/check-runs?per_page=100`, { paginate: true }).flatMap((p) => p.check_runs ?? []);
  const st = api.get(`repos/${repo}/commits/${headSha}/status`);
  const statuses = st?.statuses ?? [];
  const compare = mode === "open" ? api.get(`repos/${repo}/compare/main...${headSha}`) : null;
  const reviews = api.get(`repos/${repo}/pulls/${number}/reviews?per_page=100`, { paginate: true });
  const unresolvedThreads = countUnresolved(api, repo, number);
  const deps = baseDeps(api, repo);
  const goModules = baseGoModules(api, repo);
  return {
    headSha,
    input: {
      mode,
      pr,
      files,
      checks,
      statuses,
      compare: compare ? { behind_by: compare.behind_by } : null,
      reviews,
      unresolvedThreads,
      baseDeps: deps,
      baseGoModules: goModules,
    },
  };
}

export function nowJst(date = new Date()) {
  return new Date(date.getTime() + 9 * 3600 * 1000).toISOString().replace("T", " ").slice(0, 16) + " JST";
}

export function summaryLine(number, result, headSha) {
  const ids = result.failures.map((f) => f.rule).join(",") || "-";
  return `PR #${number} ${result.eligible ? "対象" : "対象外"}（クラス${result.class}） head=${String(headSha).slice(0, 7)} 当たったルール: ${ids}`;
}

export function runSingle({ api, repo, number, rules, json = false, commentMarkdown = false, mergeStatus = null, now = new Date() }) {
  const { headSha, input } = gatherInput(api, repo, number, "open");
  const result = evaluate(input, rules);
  let text;
  if (commentMarkdown) text = renderComment(result, { headSha, nowJst: nowJst(now).replace(/ JST$/, ""), mergeStatus });
  else if (json) text = JSON.stringify({ ...result, headSha }, null, 2) + "\n";
  else text = summaryLine(number, result, headSha) + "\n";
  return { text, code: result.eligible ? 0 : 1 };
}

const cell = (s) => String(s ?? "").replace(/\|/g, "\\|").replace(/\r?\n/g, " ");

// マージ済みPRの直近 count 本に判定をかけ、markdown を返す。読むだけ。
export function dryRunMerged({ api, repo, count, rules, listMerged }) {
  const numbers = listMerged(count);
  const rows = [];
  for (const number of numbers) {
    try {
      const pr = api.get(`repos/${repo}/pulls/${number}`);
      const { input } = gatherInput(api, repo, number, "merged-dryrun");
      const result = evaluate(input, rules);
      rows.push({ number, title: pr.title ?? "", result });
    } catch (e) {
      rows.push({ number, title: "", error: String(e.message ?? e) });
    }
  }
  const ok = rows.filter((r) => r.result);
  const byClass = { A: 0, B: 0, C: 0 };
  const ruleCount = new Map();
  let eligible = 0;
  let eligibleWithoutBody = 0;
  for (const r of ok) {
    byClass[r.result.class]++;
    if (r.result.eligible) eligible++;
    for (const f of new Set(r.result.failures.map((x) => x.rule))) ruleCount.set(f, (ruleCount.get(f) ?? 0) + 1);
    const fs = r.result.failures;
    if (fs.length > 0 && fs.every((f) => f.rule === "body.decision")) eligibleWithoutBody++;
  }
  const out = [];
  out.push(`# 自動マージ判定のドライラン（マージ済みPR 直近 ${rows.length} 本）`, "");
  out.push("読むだけの試算です。コメントもラベルも付けていません。", "");
  out.push("## 1. PRごとの結果", "");
  out.push("| PR | 題 | クラス | 対象か | 当たったルール |", "|---|---|---|---|---|");
  for (const r of rows) {
    if (r.error) out.push(`| #${r.number} | - | - | 取得失敗（対象外） | ${cell(r.error)} |`);
    else {
      const ids = [...new Set(r.result.failures.map((f) => f.rule))].join(", ") || "-";
      out.push(`| #${r.number} | ${cell(r.title)} | ${r.result.class} | ${r.result.eligible ? "対象" : "対象外"} | ${ids} |`);
    }
  }
  out.push("", "## 2. 集計", "");
  out.push(`- クラス別: A ${byClass.A} 本 / B ${byClass.B} 本 / C ${byClass.C} 本`);
  out.push(`- 対象になった本数: ${eligible} / ${ok.length}`);
  const failed = rows.length - ok.length;
  if (failed) out.push(`- 取得失敗: ${failed} 本（集計に含めない）`);
  out.push("", "| ルールid | 当たった本数 |", "|---|---|");
  const sorted = [...ruleCount.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
  for (const [id, n] of sorted) out.push(`| ${id} | ${n} |`);
  if (sorted.length === 0) out.push("| （なし） | 0 |");
  out.push("", "## 3. 本文ルール（判断が必要なこと）を除いた場合", "");
  out.push(`過去のPRは旧テンプレートなので、本文ルール（body.decision）だけで落ちたものを対象に数え直すと、対象は ${eligible + eligibleWithoutBody} 本（本文ルールを含めた場合は ${eligible} 本）です。`, "");
  return { markdown: out.join("\n"), eligible, eligibleWithoutBody, byClass, rules: sorted };
}

function parseArgs(argv) {
  const o = { json: false, commentMarkdown: false };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    const val = () => {
      if (i + 1 >= argv.length) throw new Error(`${a} に値がない`);
      return argv[++i];
    };
    if (a === "--pr") o.pr = Number(val());
    else if (a === "--repo") o.repo = val();
    else if (a === "--rules") o.rules = val();
    else if (a === "--json") o.json = true;
    else if (a === "--comment-markdown") o.commentMarkdown = true;
    else if (a === "--merge-status") o.mergeStatus = val();
    else if (a === "--render-comment") o.renderComment = val();
    else if (a === "--dry-run-merged") o.dry = Number(val());
    else if (a === "--out") o.out = val();
    else throw new Error(`知らない引数: ${a}`);
  }
  if (o.dry === undefined && o.renderComment === undefined && !(Number.isInteger(o.pr) && o.pr > 0)) throw new Error("--pr N（正の整数）か --dry-run-merged N が必要");
  if (o.dry !== undefined && !(Number.isInteger(o.dry) && o.dry > 0)) throw new Error("--dry-run-merged は正の整数");
  return o;
}

export function main(argv, { exec = ghExec, write = (s) => process.stdout.write(s), err = (s) => process.stderr.write(s) } = {}) {
  let o;
  try {
    o = parseArgs(argv);
  } catch (e) {
    err(`引数エラー: ${e.message}\n`);
    return 2;
  }
  if (o.renderComment !== undefined) {
    try {
      const j = JSON.parse(readFileSync(o.renderComment, "utf8"));
      if (!j || !Array.isArray(j.failures) || !j.stats || typeof j.eligible !== "boolean" || !j.class || !j.headSha) {
        throw new Error("判定結果のJSONに stats・failures・class・eligible・headSha が揃っていない");
      }
      write(renderComment(j, { headSha: j.headSha, nowJst: nowJst().replace(/ JST$/, ""), mergeStatus: o.mergeStatus ?? null }));
      return 0;
    } catch (e) {
      err(`コメントを作れない: ${e.message}\n`);
      return 2;
    }
  }
  try {
    const rules = JSON.parse(readFileSync(o.rules ?? DEFAULT_RULES, "utf8"));
    const api = makeApi(exec);
    const repo = o.repo ?? JSON.parse(exec(["repo", "view", "--json", "nameWithOwner"])).nameWithOwner;
    if (o.dry !== undefined) {
      const listMerged = (n) =>
        JSON.parse(exec(["pr", "list", "--repo", repo, "--state", "merged", "-L", String(n), "--json", "number"])).map((x) => x.number);
      const r = dryRunMerged({ api, repo, count: o.dry, rules, listMerged });
      if (o.out) writeFileSync(o.out, r.markdown);
      else write(r.markdown);
      return 0;
    }
    const r = runSingle({ api, repo, number: o.pr, rules, json: o.json, commentMarkdown: o.commentMarkdown, mergeStatus: o.mergeStatus ?? null });
    write(r.text);
    return r.code;
  } catch (e) {
    err(`取得失敗（対象外として扱う）: ${e.message}\n`);
    return 2;
  }
}

if (process.argv[1] && fileURLToPath(import.meta.url) === resolve(process.argv[1])) {
  process.exitCode = main(process.argv.slice(2));
}
