import assert from "node:assert/strict";
import { existsSync, mkdirSync, writeFileSync } from "node:fs";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test from "node:test";

import {
  assertOrchestratorRole,
  buildWorkerPrompt,
  defaultRun,
  executeWorker,
  parseArguments,
  resolveWindowsNpmShim,
  runCheck,
  validateWorktreeName,
} from "./spawn-agent.mjs";

test("明示的なmulti有効化なしでは拒否する", () => {
  assert.throws(
    () =>
      parseArguments([
        "--parent",
        "codex",
        "--agent",
        "claude",
        "--worktree",
        "ui",
        "--task",
        "画面を担当",
      ]),
    /--activate multi/,
  );
});

test("未知の引数と同じ引数の重複を拒否する", () => {
  assert.throws(() => parseArguments(["--unknown"]), /未知の引数/);
  assert.throws(
    () =>
      parseArguments([
        "--activate",
        "multi",
        "--parent",
        "codex",
        "--parent",
        "claude",
        "--agent",
        "claude",
        "--worktree",
        "ui",
        "--task",
        "画面を担当",
      ]),
    /重複/,
  );
});

test("checkは単独で解析でき、worker起動用引数との併用は拒否する", () => {
  assert.deepEqual(parseArguments(["--check"]), { check: true });
  assert.throws(
    () => parseArguments(["--check", "--activate", "multi"]),
    /単独/,
  );
});

test("workerは別workerを起動できない", () => {
  assert.throws(
    () => assertOrchestratorRole({ AGENT_HARNESS_ROLE: "worker" }),
    /worker.*起動できません/,
  );
});

test("危険なworktree名を拒否する", () => {
  assert.throws(() => validateWorktreeName("../main"), /英小文字/);
  assert.throws(() => validateWorktreeName("main"), /main以外/);
  assert.equal(validateWorktreeName("login-ui"), "login-ui");
});

test("worker promptは編集境界と返却契約を明示する", () => {
  const prompt = buildWorkerPrompt({
    parentAgent: "codex",
    task: "ログイン画面を担当する",
    worktreePath: "C:/workspace/mytechpulse-worktrees/login-ui",
  });

  assert.match(prompt, /ログイン画面を担当する/);
  assert.match(prompt, /mytechpulse-worktrees[\\/]login-ui/);
  assert.match(prompt, /別エージェントを起動せず/);
  assert.match(prompt, /\.envや認証情報を読まず/);
  assert.match(prompt, /通常コミット1つ/);
  assert.match(prompt, /status、summary、filesChanged、tests、issues、commit/);
});

test("checkはバージョン確認だけを実行する", () => {
  const calls = [];
  const run = (executable, args) => {
    calls.push([executable, args]);
    return { status: 0, stdout: `${executable} ok`, stderr: "" };
  };

  assert.deepEqual(runCheck(run, "win32"), {
    git: { status: 0, stdout: "git ok", stderr: "" },
    codex: { status: 0, stdout: "codex.cmd ok", stderr: "" },
    claude: { status: 0, stdout: "claude.cmd ok", stderr: "" },
  });
  assert.deepEqual(calls, [
    ["git", ["--version"]],
    ["codex.cmd", ["--version"]],
    ["claude.cmd", ["--version"]],
  ]);
});

test("Windowsのnpm cmd shimはshellを使わず実体を直接起動する", () => {
  const npmDirectory = "C:/Users/test/AppData/Roaming/npm";
  const codexScript = join(
    npmDirectory,
    "node_modules",
    "@openai",
    "codex",
    "bin",
    "codex.js",
  );
  const claudeBinary = join(
    npmDirectory,
    "node_modules",
    "@anthropic-ai",
    "claude-code",
    "bin",
    "claude.exe",
  );
  const existingPaths = new Set([
    join(npmDirectory, "codex.cmd"),
    join(npmDirectory, "claude.cmd"),
    codexScript,
    claudeBinary,
  ]);
  const runtime = {
    platform: "win32",
    pathValue: npmDirectory,
    fileExists: (filePath) => existingPaths.has(filePath),
    nodeExecutable: "C:/node/node.exe",
  };

  assert.deepEqual(resolveWindowsNpmShim("codex.cmd", runtime), {
    executable: "C:/node/node.exe",
    prefixArgs: [codexScript],
  });
  assert.deepEqual(resolveWindowsNpmShim("claude.cmd", runtime), {
    executable: claudeBinary,
    prefixArgs: [],
  });
});

test("WindowsのCodex shimはpromptをshellへ渡さない", () => {
  const npmDirectory = "C:/Users/test/AppData/Roaming/npm";
  const codexScript = join(
    npmDirectory,
    "node_modules",
    "@openai",
    "codex",
    "bin",
    "codex.js",
  );
  const calls = [];

  defaultRun("codex.cmd", ["exec", "task & unexpected"], { cwd: "C:/repo" }, {
    spawn(executable, args, options) {
      calls.push({ executable, args, options });
      return { status: 0, stdout: "", stderr: "" };
    },
    platform: "win32",
    pathValue: npmDirectory,
    fileExists: (filePath) => filePath === join(npmDirectory, "codex.cmd") || filePath === codexScript,
    nodeExecutable: "C:/node/node.exe",
  });

  assert.deepEqual(calls, [{
    executable: "C:/node/node.exe",
    args: [codexScript, "exec", "task & unexpected"],
    options: { cwd: "C:/repo", encoding: "utf8", shell: false },
  }]);
});

test("runnerは隔離worktree、worker環境、Codex結果を使う", async () => {
  const sandbox = await mkdtemp(join(tmpdir(), "spawn-agent-test-"));
  const repoRoot = join(sandbox, "mytechpulse");
  const calls = [];
  const options = parseArguments([
    "--activate",
    "multi",
    "--parent",
    "claude",
    "--agent",
    "codex",
    "--worktree",
    "login-ui",
    "--task",
    "ログイン画面だけを担当する",
  ]);

  try {
    const result = executeWorker(options, {
      repoRoot,
      platform: "linux",
      pathExists: () => false,
      run(executable, args, runOptions) {
        calls.push({ executable, args, runOptions });
        if (executable === "git" && args[0] === "show-ref") {
          return { status: 1, stdout: "", stderr: "" };
        }
        if (executable === "codex" && args[0] !== "--version") {
          const resultPath = args[args.indexOf("--output-last-message") + 1];
          mkdirSync(dirname(resultPath), { recursive: true });
          writeFileSync(
            resultPath,
            JSON.stringify({
              status: "completed",
              summary: "完了",
              filesChanged: ["frontend/src/Login.tsx"],
              tests: ["npm test"],
              issues: [],
              commit: "abc1234",
            }),
          );
        }
        return { status: 0, stdout: "", stderr: "" };
      },
    });

    assert.deepEqual(result, {
      status: "completed",
      summary: "完了",
      filesChanged: ["frontend/src/Login.tsx"],
      tests: ["npm test"],
      issues: [],
      commit: "abc1234",
    });
    assert.deepEqual(calls[0], {
      executable: "codex",
      args: ["--version"],
      runOptions: { cwd: repoRoot },
    });
    assert.deepEqual(calls[1], {
      executable: "git",
      args: ["show-ref", "--verify", "--quiet", "refs/heads/agent/login-ui"],
      runOptions: { cwd: repoRoot },
    });
    assert.deepEqual(calls[2], {
      executable: "git",
      args: [
        "worktree",
        "add",
        "-b",
        "agent/login-ui",
        join(sandbox, "mytechpulse-worktrees", "login-ui"),
        "HEAD",
      ],
      runOptions: { cwd: repoRoot },
    });
    assert.equal(calls[3].executable, "codex");
    assert.equal(calls[3].runOptions.env.AGENT_HARNESS_ROLE, "worker");
    assert.equal(calls[3].runOptions.env.AGENT_HARNESS_PARENT, "claude");
  } finally {
    await rm(sandbox, { force: true, recursive: true });
  }
});

test("worker実行失敗は同一の失敗結果へ正規化する", async () => {
  const sandbox = await mkdtemp(join(tmpdir(), "spawn-agent-test-"));
  const options = parseArguments([
    "--activate",
    "multi",
    "--parent",
    "codex",
    "--agent",
    "claude",
    "--worktree",
    "auth-api",
    "--task",
    "認証APIだけを担当する",
  ]);

  try {
    const repoRoot = join(sandbox, "mytechpulse");
    const schemaPath = join(
      repoRoot,
      "scripts",
      "agent-harness",
      "worker-result.schema.json",
    );
    await mkdir(dirname(schemaPath), { recursive: true });
    await writeFile(schemaPath, '{"type":"object"}');

    const result = executeWorker(options, {
      repoRoot,
      platform: "linux",
      pathExists: () => false,
      run(executable, args) {
        if (executable === "git" && args[0] === "show-ref") {
          return { status: 1, stdout: "", stderr: "" };
        }
        if (executable === "claude" && args[0] !== "--version") {
          return { status: 1, stdout: "", stderr: "worker failed" };
        }
        return { status: 0, stdout: "", stderr: "" };
      },
    });

    assert.deepEqual(result, {
      status: "failed",
      summary: "worker failed",
      filesChanged: [],
      tests: [],
      issues: [],
      commit: "",
    });
  } finally {
    await rm(sandbox, { force: true, recursive: true });
  }
});

test("Claudeの成功stdoutは共通Schema結果へ正規化する", async () => {
  const sandbox = await mkdtemp(join(tmpdir(), "spawn-agent-test-"));
  const repoRoot = join(sandbox, "mytechpulse");
  const options = parseArguments([
    "--activate",
    "multi",
    "--parent",
    "codex",
    "--agent",
    "claude",
    "--worktree",
    "auth-api",
    "--task",
    "認証APIだけを担当する",
  ]);

  try {
    const schemaPath = join(
      repoRoot,
      "scripts",
      "agent-harness",
      "worker-result.schema.json",
    );
    await mkdir(dirname(schemaPath), { recursive: true });
    await writeFile(schemaPath, '{"type":"object"}');

    const result = executeWorker(options, {
      repoRoot,
      platform: "linux",
      pathExists: () => false,
      run(executable, args) {
        if (executable === "git" && args[0] === "show-ref") {
          return { status: 1, stdout: "", stderr: "" };
        }
        if (executable === "claude") {
          return {
            status: 0,
            stdout: JSON.stringify({
              result: JSON.stringify({
                status: "completed",
                summary: "Claudeが完了",
                filesChanged: ["backend/app/routes/auth.py"],
                tests: ["pytest backend/tests"],
                issues: [],
                commit: "def5678",
              }),
            }),
            stderr: "",
          };
        }
        return { status: 0, stdout: "", stderr: "" };
      },
    });

    assert.deepEqual(result, {
      status: "completed",
      summary: "Claudeが完了",
      filesChanged: ["backend/app/routes/auth.py"],
      tests: ["pytest backend/tests"],
      issues: [],
      commit: "def5678",
    });
  } finally {
    await rm(sandbox, { force: true, recursive: true });
  }
});

test("Codex失敗時も一時結果ファイルを削除する", async () => {
  const sandbox = await mkdtemp(join(tmpdir(), "spawn-agent-test-"));
  const repoRoot = join(sandbox, "mytechpulse");
  const options = parseArguments([
    "--activate",
    "multi",
    "--parent",
    "claude",
    "--agent",
    "codex",
    "--worktree",
    "login-ui",
    "--task",
    "ログイン画面だけを担当する",
  ]);
  let resultPath;

  try {
    const result = executeWorker(options, {
      repoRoot,
      platform: "linux",
      pathExists: () => false,
      run(executable, args) {
        if (executable === "git" && args[0] === "show-ref") {
          return { status: 1, stdout: "", stderr: "" };
        }
        if (executable === "codex" && args[0] !== "--version") {
          resultPath = args[args.indexOf("--output-last-message") + 1];
          mkdirSync(dirname(resultPath), { recursive: true });
          writeFileSync(resultPath, "stale result");
          return { status: 1, stdout: "", stderr: "worker failed" };
        }
        return { status: 0, stdout: "", stderr: "" };
      },
    });

    assert.match(result.summary, /^worker failed\n\(ログ全文: .*codex\.log\)$/);
    assert.deepEqual({ ...result, summary: "" }, {
      status: "failed",
      summary: "",
      filesChanged: [],
      tests: [],
      issues: [],
      commit: "",
    });
    assert.equal(existsSync(resultPath), false);
  } finally {
    await rm(sandbox, { force: true, recursive: true });
  }
});

test("worker CLI確認に失敗するとworktree作成前に停止する", async () => {
  const sandbox = await mkdtemp(join(tmpdir(), "spawn-agent-test-"));
  const repoRoot = join(sandbox, "mytechpulse");
  const worktreePath = join(sandbox, "mytechpulse-worktrees", "auth-api");
  const calls = [];
  const options = parseArguments([
    "--activate",
    "multi",
    "--parent",
    "codex",
    "--agent",
    "claude",
    "--worktree",
    "auth-api",
    "--task",
    "認証APIだけを担当する",
  ]);

  try {
    assert.throws(
      () =>
        executeWorker(options, {
          repoRoot,
          platform: "win32",
          pathExists: () => false,
          run(executable, args, runOptions) {
            calls.push({ executable, args, runOptions });
            return { status: 1, stdout: "", stderr: "CLIが見つかりません" };
          },
        }),
      /worker CLIの確認に失敗しました/,
    );
    assert.deepEqual(calls, [
      {
        executable: "claude.cmd",
        args: ["--version"],
        runOptions: { cwd: repoRoot },
      },
    ]);
    assert.equal(existsSync(worktreePath), false);
  } finally {
    await rm(sandbox, { force: true, recursive: true });
  }
});

test("Codexの出力はログファイルへ流し、失敗時は末尾だけを返す", async () => {
  const sandbox = await mkdtemp(join(tmpdir(), "spawn-agent-test-"));
  const repoRoot = join(sandbox, "mytechpulse");
  const options = parseArguments([
    "--activate",
    "multi",
    "--parent",
    "claude",
    "--agent",
    "codex",
    "--worktree",
    "login-ui",
    "--task",
    "ログイン画面だけを担当する",
  ]);
  let workerStdio;

  try {
    const result = executeWorker(options, {
      repoRoot,
      platform: "linux",
      pathExists: () => false,
      run(executable, args, runOptions) {
        if (executable === "git" && args[0] === "show-ref") {
          return { status: 1, stdout: "", stderr: "" };
        }
        if (executable === "codex" && args[0] !== "--version") {
          workerStdio = runOptions.stdio;
          const lines = Array.from({ length: 100 }, (_, index) => `行${index + 1}`);
          writeFileSync(workerStdio[1], `${lines.join("\n")}\n`);
          return { status: null, error: { code: "ENOBUFS" } };
        }
        return { status: 0, stdout: "", stderr: "" };
      },
    });

    assert.equal(workerStdio[0], "ignore");
    assert.equal(workerStdio[1], workerStdio[2]);
    assert.equal(result.status, "failed");
    assert.match(result.summary, /行100/);
    assert.doesNotMatch(result.summary, /行50\b/);
    assert.match(result.summary, /login-ui\.codex\.log/);
  } finally {
    await rm(sandbox, { force: true, recursive: true });
  }
});
