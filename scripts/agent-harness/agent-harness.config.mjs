import { readFileSync } from "node:fs";

export const AGENT_HARNESS = Object.freeze({
  worktreeDirectorySuffix: "-worktrees",
  branchPrefix: "agent/",
  resultSchemaPath: "scripts/agent-harness/worker-result.schema.json",
  codex: Object.freeze({
    executable: "codex",
    model: "gpt-5.6-terra",
    reasoningEffort: "high",
  }),
  claude: Object.freeze({
    executable: "claude",
    model: "sonnet",
    effort: "high",
  }),
});

export function executableForPlatform(executable, platform = process.platform) {
  if (platform === "win32" && !/\.(?:cmd|exe)$/i.test(executable)) {
    return `${executable}.cmd`;
  }

  return executable;
}

export function buildAgentCommand({
  worker,
  worktreePath,
  schemaPath,
  outputLastMessagePath,
  prompt,
  platform = process.platform,
}) {
  if (worker === "codex") {
    const { executable, model, reasoningEffort } = AGENT_HARNESS.codex;

    return {
      executable: executableForPlatform(executable, platform),
      args: [
        "exec",
        "--model",
        model,
        "--config",
        `model_reasoning_effort=\"${reasoningEffort}\"`,
        "--sandbox",
        "workspace-write",
        "--cd",
        worktreePath,
        "--output-schema",
        schemaPath,
        "--output-last-message",
        outputLastMessagePath,
        prompt,
      ],
      cwd: worktreePath,
    };
  }

  if (worker === "claude") {
    const { executable, model, effort } = AGENT_HARNESS.claude;
    const workerResultSchema = JSON.parse(readFileSync(schemaPath, "utf8"));

    return {
      executable: executableForPlatform(executable, platform),
      args: [
        "--print",
        "--permission-mode",
        "dontAsk",
        "--permission-prompts",
        "none",
        "--no-session-persistence",
        "--output-format",
        "json",
        "--json-schema",
        JSON.stringify(workerResultSchema),
        "--model",
        model,
        "--effort",
        effort,
        prompt,
      ],
      cwd: worktreePath,
    };
  }

  throw new RangeError(`Unsupported worker: ${worker}`);
}
