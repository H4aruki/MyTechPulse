import { existsSync, mkdirSync, readFileSync, unlinkSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { basename, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import {
  AGENT_HARNESS,
  buildAgentCommand,
  executableForPlatform,
} from "./agent-harness.config.mjs";

const REQUIRED_OPTIONS = new Set([
  "--activate",
  "--parent",
  "--agent",
  "--worktree",
  "--task",
]);
const VALID_AGENTS = new Set(["codex", "claude"]);

export function validateWorktreeName(name) {
  if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/u.test(name) || name === "main") {
    throw new Error("worktree名はmain以外の英小文字・数字・ハイフンで指定してください。");
  }

  return name;
}

export function assertOrchestratorRole(env = process.env) {
  if (env.AGENT_HARNESS_ROLE === "worker") {
    throw new Error(
      "worker環境から別のエージェントは起動できません。親へ結果を返してください。",
    );
  }
}

export function parseArguments(args) {
  if (args.length === 1 && args[0] === "--check") {
    return { check: true };
  }

  if (args.includes("--check")) {
    throw new Error("--checkは単独で指定してください。");
  }

  const options = {};
  for (let index = 0; index < args.length; index += 2) {
    const option = args[index];
    const value = args[index + 1];

    if (!REQUIRED_OPTIONS.has(option)) {
      throw new Error(`未知の引数です: ${option}`);
    }
    if (Object.hasOwn(options, option)) {
      throw new Error(`引数が重複しています: ${option}`);
    }
    if (value === undefined || value.startsWith("--")) {
      throw new Error(`${option}には値が必要です。`);
    }
    options[option] = value;
  }

  for (const option of REQUIRED_OPTIONS) {
    if (!Object.hasOwn(options, option)) {
      if (option === "--activate") {
        throw new Error("通常実行には--activate multiが必要です。");
      }
      throw new Error(`${option}が必要です。`);
    }
  }
  if (options["--activate"] !== "multi") {
    throw new Error("通常実行には--activate multiが必要です。");
  }
  if (!VALID_AGENTS.has(options["--parent"])) {
    throw new Error("--parentはcodexまたはclaudeで指定してください。");
  }
  if (!VALID_AGENTS.has(options["--agent"])) {
    throw new Error("--agentはcodexまたはclaudeで指定してください。");
  }
  if (!options["--task"].trim()) {
    throw new Error("--taskは空にできません。");
  }

  return {
    activate: "multi",
    parentAgent: options["--parent"],
    agent: options["--agent"],
    worktree: validateWorktreeName(options["--worktree"]),
    task: options["--task"].trim(),
  };
}

export function buildWorkerPrompt({ parentAgent, task, worktreePath }) {
  return `あなたは ${parentAgent} が起動したworkerです。\n担当タスク:\n${task}\n\n` +
    `編集できるのは ${worktreePath} 内で、担当タスクに必要なファイルだけです。\n` +
    "別エージェントを起動せず、.envや認証情報を読まず、危険なGit操作をしないでください。\n" +
    "関連テストを実行し、通常コミット1つに変更をまとめます。\n" +
    "最後に指定されたJSON Schemaだけに従い、status、summary、filesChanged、tests、issues、commitを返してください。";
}

export function runCheck(run, platform = process.platform) {
  return {
    git: run("git", ["--version"]),
    codex: run(executableForPlatform("codex", platform), ["--version"]),
    claude: run(executableForPlatform("claude", platform), ["--version"]),
  };
}

function defaultRun(executable, args, options = {}) {
  return spawnSync(executable, args, {
    ...options,
    encoding: "utf8",
    shell: false,
  });
}

function commandFailed(result) {
  return result.error || result.status !== 0;
}

function resultSummary(result) {
  return String(result.stderr || result.error?.message || "workerの実行に失敗しました。")
    .trim()
    .slice(0, 1000);
}

function failedResult(summary) {
  return {
    status: "failed",
    summary,
    filesChanged: [],
    tests: [],
    issues: [],
    commit: "",
  };
}

function validateWorkerResult(value) {
  const keys = ["status", "summary", "filesChanged", "tests", "issues", "commit"];
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error("worker結果はJSON objectである必要があります。");
  }
  if (Object.keys(value).length !== keys.length || keys.some((key) => !Object.hasOwn(value, key))) {
    throw new Error("worker結果が指定Schemaに一致しません。");
  }
  if (!new Set(["completed", "blocked", "failed"]).has(value.status)) {
    throw new Error("worker結果のstatusが不正です。");
  }
  if (typeof value.summary !== "string" || typeof value.commit !== "string") {
    throw new Error("worker結果の文字列項目が不正です。");
  }
  if (![value.filesChanged, value.tests, value.issues].every(
    (items) => Array.isArray(items) && items.every((item) => typeof item === "string"),
  )) {
    throw new Error("worker結果の配列項目が不正です。");
  }

  return value;
}

function normalizeWorkerResult(agent, child, outputLastMessagePath) {
  try {
    if (commandFailed(child)) {
      return failedResult(resultSummary(child));
    }

    if (agent === "codex") {
      const output = readFileSync(outputLastMessagePath, "utf8");
      return validateWorkerResult(JSON.parse(output));
    }

    const output = JSON.parse(String(child.stdout));
    return validateWorkerResult(JSON.parse(output.result));
  } catch (error) {
    return failedResult(error instanceof Error ? error.message : "worker結果を読み取れませんでした。");
  } finally {
    if (agent === "codex" && existsSync(outputLastMessagePath)) {
      unlinkSync(outputLastMessagePath);
    }
  }
}

export function executeWorker(options, dependencies = {}) {
  const run = dependencies.run ?? defaultRun;
  const repoRoot = dependencies.repoRoot ?? findRepositoryRoot(run);
  const pathExists = dependencies.pathExists ?? existsSync;
  const env = dependencies.env ?? process.env;
  const platform = dependencies.platform ?? process.platform;

  assertOrchestratorRole(env);
  const workerExecutable = executableForPlatform(
    AGENT_HARNESS[options.agent].executable,
    platform,
  );
  const workerCheck = run(workerExecutable, ["--version"], { cwd: repoRoot });
  if (commandFailed(workerCheck)) {
    throw new Error(`worker CLIの確認に失敗しました: ${resultSummary(workerCheck)}`);
  }

  const worktreePath = join(
    dirname(repoRoot),
    `${basename(repoRoot)}${AGENT_HARNESS.worktreeDirectorySuffix}`,
    options.worktree,
  );
  const branchName = `${AGENT_HARNESS.branchPrefix}${options.worktree}`;

  if (pathExists(worktreePath)) {
    throw new Error(`worktreeパスが既に存在します: ${worktreePath}`);
  }
  const branchCheck = run("git", ["show-ref", "--verify", "--quiet", `refs/heads/${branchName}`], {
    cwd: repoRoot,
  });
  if (branchCheck.status === 0) {
    throw new Error(`branchが既に存在します: ${branchName}`);
  }
  if (branchCheck.error || (branchCheck.status !== 1 && branchCheck.status !== 0)) {
    throw new Error(`branchの確認に失敗しました: ${resultSummary(branchCheck)}`);
  }

  const worktreeAdd = run(
    "git",
    ["worktree", "add", "-b", branchName, worktreePath, "HEAD"],
    { cwd: repoRoot },
  );
  if (commandFailed(worktreeAdd)) {
    throw new Error(`worktreeの作成に失敗しました: ${resultSummary(worktreeAdd)}`);
  }

  const outputDirectory = join(worktreePath, ".agent-harness");
  const outputLastMessagePath = join(outputDirectory, "last-message.json");
  mkdirSync(outputDirectory, { recursive: true });
  const schemaPath = join(repoRoot, AGENT_HARNESS.resultSchemaPath);
  const command = buildAgentCommand({
    worker: options.agent,
    platform,
    worktreePath,
    schemaPath,
    outputLastMessagePath,
    prompt: buildWorkerPrompt({
      parentAgent: options.parentAgent,
      task: options.task,
      worktreePath,
    }),
  });
  const child = run(command.executable, command.args, {
    cwd: worktreePath,
    env: {
      ...env,
      AGENT_HARNESS_ROLE: "worker",
      AGENT_HARNESS_PARENT: options.parentAgent,
    },
  });

  return normalizeWorkerResult(options.agent, child, outputLastMessagePath);
}

function findRepositoryRoot(run) {
  const result = run("git", ["rev-parse", "--show-toplevel"], { cwd: process.cwd() });
  if (commandFailed(result)) {
    throw new Error(`リポジトリの確認に失敗しました: ${resultSummary(result)}`);
  }

  return String(result.stdout).trim();
}

function main() {
  try {
    const options = parseArguments(process.argv.slice(2));
    if (options.check) {
      console.log(JSON.stringify(runCheck(defaultRun)));
      return;
    }

    console.log(JSON.stringify(executeWorker(options)));
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exitCode = 1;
  }
}

if (process.argv[1] && fileURLToPath(import.meta.url) === process.argv[1]) {
  main();
}
