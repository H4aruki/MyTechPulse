// 自動マージの判定（純粋関数）。ネットワーク・ファイルを使わない。
// 数字と一覧はすべて .github/automerge-rules.json から渡される。

function escapeRe(ch) {
  return /[.+^$()|\\\[\]{}?*]/.test(ch) ? `\\${ch}` : ch;
}

function globToSource(glob) {
  let out = "";
  for (let i = 0; i < glob.length; i++) {
    const c = glob[i];
    if (c === "*") {
      if (glob[i + 1] === "*") {
        if (glob[i + 2] === "/") {
          out += "(?:.*/)?";
          i += 2;
        } else {
          out += ".*";
          i += 1;
        }
      } else {
        out += "[^/]*";
      }
    } else if (c === "{") {
      const end = glob.indexOf("}", i);
      if (end < 0) {
        out += "\\{";
      } else {
        out += `(?:${glob.slice(i + 1, end).split(",").map(globToSource).join("|")})`;
        i = end;
      }
    } else {
      out += escapeRe(c);
    }
  }
  return out;
}

export function globToRegExp(glob) {
  // スラッシュを含まない書き方は、どの階層のファイル名にも当たる
  const g = glob.includes("/") ? glob : `**/${glob}`;
  return new RegExp(`^${globToSource(g)}$`);
}

function matcher(globs) {
  const res = (globs ?? []).map(globToRegExp);
  return (p) => res.some((r) => r.test(p));
}

function addedLines(patch) {
  if (!patch) return [];
  return patch
    .split("\n")
    .filter((l) => l.startsWith("+") && !l.startsWith("+++"))
    .map((l) => l.slice(1));
}

function goImportPath(line) {
  const s = line.trim();
  let m = /^import\s+(?:[\w.]+\s+)?"([^"]+)"/.exec(s);
  if (m) return m[1];
  m = /^(?:[\w.]+\s+)?"([^"\s]+)"\s*(?:\/\/.*)?$/.exec(s);
  return m ? m[1] : null;
}

function tsImportPaths(line) {
  const out = [];
  const res = [
    /\b(?:from|import)\s*['"]([^'"]+)['"]/g,
    /\bimport\s*\(\s*['"]([^'"]+)['"]/g,
    /\brequire\s*\(\s*['"]([^'"]+)['"]/g,
  ];
  for (const re of res) for (const m of line.matchAll(re)) out.push(m[1]);
  return out;
}

function packageName(spec) {
  const parts = spec.split("/");
  return spec.startsWith("@") ? parts.slice(0, 2).join("/") : parts[0];
}

function decisionSection(body, heading) {
  const lines = (body ?? "").split(/\r?\n/);
  const start = lines.findIndex((l) => new RegExp(`^##\\s+${escapeRe(heading)}\\s*$`).test(l));
  if (start < 0) return null;
  let end = lines.length;
  for (let i = start + 1; i < lines.length; i++) {
    if (/^##\s/.test(lines[i])) {
      end = i;
      break;
    }
  }
  return lines
    .slice(start + 1, end)
    .join("\n")
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/\s+/g, "");
}

export function evaluate(input, rules) {
  const { pr = {}, compare = null } = input;
  const mode = input.mode ?? "open";
  const failures = [];
  const fail = (rule, detail) => failures.push({ rule, detail });
  // 取得結果が欠けているものは 0 件とみなさず失敗にする（fail closed）
  for (const key of ["files", "checks", "statuses", "reviews"]) {
    if (!Array.isArray(input[key])) fail("input.missing", `取得結果「${key}」が欠けている`);
  }
  if (!Number.isInteger(input.unresolvedThreads) || input.unresolvedThreads < 0) {
    fail("input.missing", "取得結果「未解決のレビュー会話の数」が欠けている");
  }
  const files = Array.isArray(input.files) ? input.files : [];
  const checks = Array.isArray(input.checks) ? input.checks : [];
  const statuses = Array.isArray(input.statuses) ? input.statuses : [];
  const reviews = Array.isArray(input.reviews) ? input.reviews : [];

  const isAllowed = matcher(rules.allowedPaths);
  const isExcluded = matcher(rules.excludedPaths);
  const isTest = matcher(rules.testFilePatterns);
  const subs = (rules.excludedPathSubstrings ?? []).map((s) => s.toLowerCase());
  const forbidden = (rules.forbiddenAddedPatterns ?? []).map((s) => new RegExp(s));
  const secrets = (rules.secretPatterns ?? []).map((s) => new RegExp(s));
  const lim = rules.limits;

  // ---- stats
  const stats = {
    files: files.length,
    nonTestFiles: 0,
    testFiles: 0,
    additions: 0,
    deletions: 0,
    nonTestAdditions: 0,
    nonTestDeletions: 0,
    testAdditions: 0,
    testDeletions: 0,
  };
  for (const f of files) {
    const a = f.additions ?? 0;
    const d = f.deletions ?? 0;
    stats.additions += a;
    stats.deletions += d;
    if (isTest(f.filename)) {
      stats.testFiles++;
      stats.testAdditions += a;
      stats.testDeletions += d;
    } else {
      stats.nonTestFiles++;
      stats.nonTestAdditions += a;
      stats.nonTestDeletions += d;
    }
  }

  // ---- パス
  if (files.length === 0) fail("path.empty", "変更ファイルが 0 件");
  for (const f of files) {
    const p = f.filename;
    const low = p.toLowerCase();
    const sub = subs.find((s) => low.includes(s));
    if (sub) fail("path.excluded", `\`${p}\` が認証・安全のパス（\`${sub}\` を含む）`);
    else if (isExcluded(p)) fail("path.excluded", `\`${p}\` が対象外のパス`);
    else if (!isAllowed(p)) fail("path.not-allowed", `\`${p}\` が許可パスの外`);
    if (["renamed", "removed", "copied"].includes(f.status)) {
      fail("path.status", `\`${p}\` の変更の種類が ${f.status}（名前の変更・削除・コピーは対象外）`);
    }
    if (f.previous_filename) fail("path.renamed", `\`${p}\` は \`${f.previous_filename}\` からの名前変更`);
    // 種類（file / symlink / submodule）は、評価したSHAでの実体を gatherInput が付ける
    if (f.status !== "removed") {
      if (f.kind === "symlink") fail("path.symlink", `\`${p}\` がシンボリックリンク`);
      else if (f.kind !== "file") fail("path.kind", `\`${p}\` の種類が確認できない（${f.kind ?? "不明"}）`);
    }
    // patch が無いものは、行数に関係なく対象外（バイナリ・大きすぎる差分）
    if (!f.patch) fail("path.no-patch", `\`${p}\` の差分が読めない（バイナリか大きすぎる）`);
  }

  // ---- サイズ
  const nonTestLines = stats.nonTestAdditions + stats.nonTestDeletions;
  const totalLines = stats.additions + stats.deletions;
  if (nonTestLines > lim.nonTestLines) {
    fail("size.nonTestLines", `テスト以外の変更が ${nonTestLines} 行（上限 ${lim.nonTestLines}）`);
  }
  if (stats.nonTestFiles > lim.nonTestFiles) {
    fail("size.nonTestFiles", `テスト以外の変更が ${stats.nonTestFiles} ファイル（上限 ${lim.nonTestFiles}）`);
  }
  if (totalLines > lim.totalLines) {
    fail("size.totalLines", `全体の変更が ${totalLines} 行（上限 ${lim.totalLines}）`);
  }

  // ---- テスト
  const hasTestAdd = files.some((f) => isTest(f.filename) && (f.additions ?? 0) > 0);
  const docsOnly = files.length > 0 && files.every((f) => /^docs\//.test(f.filename));
  if (!hasTestAdd && !(rules.docsOnlyEligible && docsOnly)) {
    fail("tests.missing", "テストの追加がない");
  }
  for (const f of files) {
    if (isTest(f.filename) && f.status === "removed") fail("tests.deleted", `テスト \`${f.filename}\` が削除されている`);
  }
  if (stats.testDeletions > stats.testAdditions) {
    fail("tests.shrunk", `テストの削除行（${stats.testDeletions}）が追加行（${stats.testAdditions}）より多い`);
  }

  // ---- 追加行の検査・import
  for (const f of files) {
    const lines = addedLines(f.patch);
    for (const re of forbidden) {
      if (lines.some((l) => re.test(l))) fail("content.forbidden", `\`${f.filename}\` の追加行に禁止の書き方 \`${re.source}\``);
    }
    for (const re of secrets) {
      if (lines.some((l) => re.test(l))) fail("content.secret", `\`${f.filename}\` の追加行に鍵・トークンの形（\`${re.source}\`）`);
    }
    if (f.filename.endsWith(".go")) {
      for (const l of lines) {
        const imp = goImportPath(l);
        if (!imp) continue;
        const first = imp.split("/")[0];
        const mods = input.baseGoModules ?? null;
        const known = Array.isArray(mods) && mods.some((m) => imp === m || imp.startsWith(`${m}/`));
        if (first.includes(".") && !imp.startsWith(rules.goModulePrefix) && !known) {
          fail("import.go", `\`${f.filename}\` が外部パッケージ \`${imp}\` を import`);
        }
      }
    } else if (/\.tsx?$/.test(f.filename)) {
      const deps = input.baseDeps ?? null;
      for (const l of lines) {
        for (const spec of tsImportPaths(l)) {
          if (spec.startsWith(".") || spec.startsWith("/") || spec.startsWith("@/")) continue;
          const name = packageName(spec);
          if (!deps || !deps.includes(name)) {
            fail("import.ts", `\`${f.filename}\` が未導入のパッケージ \`${name}\` を import`);
          }
        }
      }
    }
  }

  // ---- 検査
  // 同じ名前が複数ある（push と pull_request の2回など）ときは、全部が completed かつ success
  for (const name of rules.requiredChecks) {
    const same = checks.filter((c) => c.name === name);
    if (same.length === 0) {
      fail("checks.required-missing", `必須の検査「${name}」が見つからない`);
      continue;
    }
    const notDone = same.find((c) => c.status !== "completed");
    if (notDone) {
      fail("checks.running", `必須の検査「${name}」が実行中（${notDone.status}）`);
      continue;
    }
    const bad = same.find((c) => c.conclusion !== "success");
    if (bad) fail("checks.required-failed", `必須の検査「${name}」が ${bad.conclusion}`);
  }
  const badConclusions = ["failure", "cancelled", "timed_out", "action_required", "startup_failure"];
  for (const c of checks) {
    if (rules.requiredChecks.includes(c.name)) continue;
    if (c.status !== "completed") fail("checks.running", `検査「${c.name}」が実行中（${c.status}）`);
    else if (badConclusions.includes(c.conclusion)) fail("checks.failed", `検査「${c.name}」が ${c.conclusion}`);
  }
  for (const s of statuses ?? []) {
    if (["failure", "error", "pending"].includes(s.state)) fail("checks.status", `ステータス「${s.context}」が ${s.state}`);
  }

  // ---- 状態（マージ済みの振り返りでは対象外 N/A）
  if (mode === "open") {
    if (pr.draft) fail("state.draft", "下書き（draft）のPR");
    if (pr.state !== "open") fail("state.closed", `PRの状態が ${pr.state}（open ではない）`);
    if (pr.base?.ref !== "main") fail("state.base", `マージ先が ${pr.base?.ref}（main ではない）`);
    if (!pr.head?.repo || pr.head.repo.full_name !== pr.base?.repo?.full_name) {
      fail("state.fork", "別のリポジトリ（fork）からのPR");
    }
    if (pr.mergeable_state !== "clean") fail("state.mergeable", `mergeable_state が ${pr.mergeable_state}（clean ではない）`);
    if (!compare) fail("state.behind", "mainとの差が取得できない");
    else if (compare.behind_by !== 0) fail("state.behind", `main に ${compare.behind_by} コミット遅れている`);
  }

  // ---- 人の拒否
  if ((pr.labels ?? []).some((l) => l.name === rules.blockLabel)) {
    fail("human.label", `ラベル \`${rules.blockLabel}\` が付いている`);
  }
  const latest = new Map();
  const sorted = [...reviews]
    .filter((r) => ["APPROVED", "CHANGES_REQUESTED", "DISMISSED"].includes(r.state))
    .sort((a, b) => String(a.submitted_at).localeCompare(String(b.submitted_at)));
  for (const r of sorted) latest.set(r.user?.login, r);
  for (const [login, r] of latest) {
    if (r.state === "CHANGES_REQUESTED") fail("human.changes-requested", `${login} が変更を求めている`);
  }
  if (input.unresolvedThreads > 0) fail("human.threads", `未解決のレビュー会話が ${input.unresolvedThreads} 件`);

  // ---- 本文
  const sec = decisionSection(pr.body, rules.decisionSection);
  if (sec === null) fail("body.decision", `本文に「## ${rules.decisionSection}」の節がない`);
  else if (sec !== rules.decisionNoneValue) {
    fail("body.decision", sec === "" ? `「${rules.decisionSection}」が空` : `「${rules.decisionSection}」が「${rules.decisionNoneValue}」ではない`);
  }

  const cls = failures.length === 0 ? "A" : failures.some((f) => f.rule.startsWith("path.")) ? "C" : "B";
  return { eligible: failures.length === 0, class: cls, failures, stats };
}

export function renderComment(result, { headSha, nowJst, mergeStatus }) {
  const s = result.stats;
  const has = (prefix) => result.failures.some((f) => f.rule.startsWith(prefix));
  const mark = (prefix, ok = "OK", ng = "問題あり") => (has(prefix) ? `❌ ${ng}` : `✅ ${ok}`);
  const verdict = result.eligible ? "✅ 対象（クラスA）" : `❌ 対象外（クラス${result.class}）`;
  const out = [
    "<!-- automerge-eligibility -->",
    `🤖 自動マージ判定（head ${String(headSha).slice(0, 7)}・${nowJst} JST）`,
    "",
    `判定: ${verdict}`,
    `マージ: ${mergeStatus ?? "未実行（判定のみ）"}`,
    "",
    "| 項目 | 結果 |",
    "|---|---|",
    `| 変更 | ${s.files}ファイル（テスト以外 ${s.nonTestFiles}）、+${s.additions} −${s.deletions}（テスト以外 +${s.nonTestAdditions} −${s.nonTestDeletions}） |`,
    `| パス | ${mark("path.", "すべて許可")} |`,
    `| サイズ | ${mark("size.", "上限内")} |`,
    `| テスト | ${has("tests.") ? "❌ 問題あり" : `✅ 追加あり（${s.testFiles}ファイル）`} |`,
    `| 追加行・import | ${mark("content.")}${has("import.") ? " ❌ import あり" : ""} |`,
    `| 検査 | ${mark("checks.", "必須すべて success、ほか失敗 0")} |`,
    `| main への追従・状態 | ${mark("state.", "最新")} |`,
    `| 人の判断 | ${mark("human.", "拒否なし")} |`,
    `| 本文「判断が必要なこと」 | ${mark("body.", "なし")} |`,
  ];
  if (!result.eligible) {
    out.push("");
    for (const f of result.failures) out.push(`❌ ${f.detail}`);
  }
  return out.join("\n") + "\n";
}
