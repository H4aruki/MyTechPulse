import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import {
  AGENT_HARNESS,
  buildAgentCommand,
  executableForPlatform,
} from "./agent-harness.config.mjs";

const worktreePath = "C:/workspace/task-1";
const schemaPath = "C:/workspace/worker-result.schema.json";
const outputLastMessagePath = "C:/workspace/last-message.txt";
const prompt = "Implement the assigned task.";

test("worker settings pin the approved model and high reasoning effort", () => {
  assert.equal(AGENT_HARNESS.worktreeDirectorySuffix, "-worktrees");
  assert.equal(AGENT_HARNESS.branchPrefix, "agent/");
  assert.equal(
    AGENT_HARNESS.resultSchemaPath,
    "scripts/agent-harness/worker-result.schema.json",
  );
  assert.deepEqual(AGENT_HARNESS.codex, {
    executable: "codex",
    model: "gpt-5.6-terra",
    reasoningEffort: "high",
  });
  assert.deepEqual(AGENT_HARNESS.claude, {
    executable: "claude",
    model: "sonnet",
    effort: "high",
  });
});

test("bare worker executables receive a cmd suffix only on Windows", () => {
  assert.equal(executableForPlatform("codex", "win32"), "codex.cmd");
  assert.equal(executableForPlatform("claude", "win32"), "claude.cmd");
  assert.equal(executableForPlatform("codex.cmd", "win32"), "codex.cmd");
  assert.equal(executableForPlatform("codex.exe", "win32"), "codex.exe");
  assert.equal(executableForPlatform("codex", "linux"), "codex");
  assert.equal(executableForPlatform("claude", "darwin"), "claude");
});

test("Codex command pins the safe workspace execution contract", () => {
  const command = buildAgentCommand({
    worker: "codex",
    platform: "win32",
    worktreePath,
    schemaPath,
    outputLastMessagePath,
    prompt,
  });

  assert.equal(command.executable, "codex.cmd");
  assert.deepEqual(command.args, [
    "exec",
    "--model",
    "gpt-5.6-terra",
    "--config",
    'model_reasoning_effort="high"',
    "--sandbox",
    "workspace-write",
    "--cd",
    worktreePath,
    "--output-schema",
    schemaPath,
    "--output-last-message",
    outputLastMessagePath,
    prompt,
  ]);
  assert.equal(command.args.includes("--full-auto"), false);
  assert.equal(
    command.args.includes("--dangerously-bypass-approvals-and-sandbox"),
    false,
  );
});

test("Claude Code command uses non-interactive safe JSON output", async () => {
  const customSchema = { type: "object", title: "caller supplied schema" };
  const schemaDirectory = await mkdtemp(join(tmpdir(), "agent-harness-"));
  const customSchemaPath = join(schemaDirectory, "custom.schema.json");
  await writeFile(customSchemaPath, JSON.stringify(customSchema));

  try {
    const command = buildAgentCommand({
      worker: "claude",
      platform: "linux",
      worktreePath,
      schemaPath: customSchemaPath,
      outputLastMessagePath,
      prompt,
    });

    assert.equal(command.executable, "claude");
    assert.deepEqual(command.args.slice(0, 14), [
      "--print",
      "--permission-mode",
      "dontAsk",
      "--permission-prompts",
      "none",
      "--no-session-persistence",
      "--output-format",
      "json",
      "--json-schema",
      JSON.stringify(customSchema),
      "--model",
      "sonnet",
      "--effort",
      "high",
    ]);
    assert.deepEqual(command.args.slice(14), [prompt]);
    assert.equal(command.args.includes("--dangerously-skip-permissions"), false);
  } finally {
    await rm(schemaDirectory, { force: true, recursive: true });
  }
});

test("worker result schema rejects undeclared fields and constrains status", async () => {
  const schema = JSON.parse(
    await readFile(new URL("./worker-result.schema.json", import.meta.url)),
  );

  assert.equal(schema.type, "object");
  assert.equal(schema.additionalProperties, false);
  assert.deepEqual(schema.required, [
    "status",
    "summary",
    "filesChanged",
    "tests",
    "issues",
    "commit",
  ]);
  assert.deepEqual(Object.keys(schema.properties), schema.required);
  assert.deepEqual(schema.properties.status.enum, [
    "completed",
    "blocked",
    "failed",
  ]);
  assert.equal(schema.properties.commit.type, "string");
});
