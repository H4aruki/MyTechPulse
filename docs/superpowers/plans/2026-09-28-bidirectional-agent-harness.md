# 双方向 Agent Harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 明示的に要求された場合だけ、Codex と Claude Code を相互に専用worktreeのworkerとして起動できるようにする。

**Architecture:** `AGENT_HARNESS` にCLI差、workerのモデル上限、結果Schemaを集約する。Node.jsランナーが専用ブランチ・worktreeを作成し、workerを非対話実行して構造化結果を親へ返す。

**Tech Stack:** Node.js 22、Node.js標準ライブラリ、Git worktree、Codex CLI、Claude Code CLI、JSON Schema

**Spec:** `docs/superpowers/specs/2026-09-28-bidirectional-agent-harness-design.md`

## Global Constraints

- `/multi`、`/parallel`、または同等の明示的依頼がない限りworkerを起動しない。
- 通常起動では親エージェントを `--parent codex|claude` で明示する。`--check` は親指定を必要としない。
- Codex workerは `gpt-5.6-terra` と `model_reasoning_effort = "high"`、Claude Code workerは `sonnet` と `high` を固定する。
- 親エージェントのモデル・推論量は変更しない。
- workerは `agent/<worktree名>` の専用worktreeだけで変更・検証・コミットする。
- merge、rebase、worktree削除、git clean、強制push、秘密情報の読み取り、危険な権限スキップを自動化しない。
- 新しいnpm依存は追加しない。外部Skillはユーザー領域で管理する。

---

### Task 1: 共通設定とworker結果Schemaを追加する

**Files:**
- Create: `scripts/agent-harness/agent-harness.config.mjs`
- Create: `scripts/agent-harness/worker-result.schema.json`
- Test: `scripts/agent-harness/agent-harness.config.test.mjs`

**Interfaces:**
- Produces: `AGENT_HARNESS`, `executableForPlatform()`, `buildAgentCommand()`
- Consumes: Node.jsの`process.platform`、結果Schemaの絶対パス

- [ ] **Step 1: モデル上限の失敗テストを書く**

```js
import test from 'node:test';
import assert from 'node:assert/strict';
import { AGENT_HARNESS, buildAgentCommand, executableForPlatform } from './agent-harness.config.mjs';

test('workerのモデルと推論量を上限へ固定する', () => {
  assert.deepEqual(AGENT_HARNESS.agents.codex.worker, { model: 'gpt-5.6-terra', reasoningEffort: 'high' });
  assert.deepEqual(AGENT_HARNESS.agents.claude.worker, { model: 'sonnet', reasoningEffort: 'high' });
});

test('Windowsだけcmd拡張子を付ける', () => {
  assert.equal(executableForPlatform('codex', 'win32'), 'codex.cmd');
  assert.equal(executableForPlatform('claude', 'linux'), 'claude');
});
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `node --test scripts/agent-harness/agent-harness.config.test.mjs`
Expected: `ERR_MODULE_NOT_FOUND`。設定モジュールはまだ存在しない。

- [ ] **Step 3: 最小の設定とSchemaを実装する**

```js
export const AGENT_HARNESS = Object.freeze({
  worktreeDirectorySuffix: '-worktrees',
  branchPrefix: 'agent/',
  resultSchemaPath: 'scripts/agent-harness/worker-result.schema.json',
  agents: Object.freeze({
    codex: Object.freeze({ displayName: 'Codex', executable: 'codex', worker: Object.freeze({ model: 'gpt-5.6-terra', reasoningEffort: 'high' }) }),
    claude: Object.freeze({ displayName: 'Claude Code', executable: 'claude', worker: Object.freeze({ model: 'sonnet', reasoningEffort: 'high' }) }),
  }),
});

export const executableForPlatform = (name, platform = process.platform) =>
  platform === 'win32' && !/\.(cmd|exe)$/u.test(name) ? `${name}.cmd` : name;
```

`worker-result.schema.json` には `status`（`completed`、`blocked`、`failed`）、`summary`、`filesChanged`、`tests`、`issues`、`commit` を必須で定義し、余分なプロパティを拒否する。

- [ ] **Step 4: CLIコマンド組み立てを実装する**

```js
export function buildAgentCommand({ agentName, worktreePath, prompt, schemaPath, resultPath }) {
  if (agentName === 'codex') {
    return { executable: 'codex', args: ['exec', '--model', 'gpt-5.6-terra', '--config', 'model_reasoning_effort="high"', '--sandbox', 'workspace-write', '-C', worktreePath, '--output-schema', schemaPath, '--output-last-message', resultPath, prompt] };
  }
  return { executable: 'claude', args: ['-p', '--model', 'sonnet', '--effort', 'high', '--permission-mode', 'dontAsk', '--permission-prompts', 'none', '--no-session-persistence', '--output-format', 'json', '--json-schema', JSON.stringify(JSON.parse(readFileSync(schemaPath, 'utf8'))), prompt] };
}
```

Codexコマンドに `--full-auto` と危険なバイパス指定を加えない。Claude Codeコマンドに `--dangerously-skip-permissions` を加えない。

- [ ] **Step 5: テストを通してコミットする**

Run: `node --test scripts/agent-harness/agent-harness.config.test.mjs`
Expected: PASS。CodexにTerra/High、Claude CodeにSonnet/Highが含まれる。

```bash
git add scripts/agent-harness/agent-harness.config.mjs scripts/agent-harness/worker-result.schema.json scripts/agent-harness/agent-harness.config.test.mjs
git commit -m "feat(harness): worker実行設定を共通化" -m "CodexとClaude Codeのworker設定、モデル上限、構造化結果Schemaを一箇所へ集約する。" -m "Refs #132"
```

### Task 2: 安全な双方向worktreeランナーを実装する

**Files:**
- Create: `scripts/agent-harness/spawn-agent.mjs`
- Create: `scripts/agent-harness/spawn-agent.test.mjs`
- Modify: `.claude/settings.json`

**Interfaces:**
- Consumes: `AGENT_HARNESS`, `buildAgentCommand()`, `worker-result.schema.json`
- Produces: `parseArguments()`, `validateWorktreeName()`, `assertOrchestratorRole()`, `buildWorkerPrompt()`, `runCheck()`

- [ ] **Step 1: 有効化境界と再帰拒否の失敗テストを書く**

```js
test('明示的なmulti有効化なしでは拒否する', () => {
  assert.throws(() => parseArguments(['--parent', 'codex', '--agent', 'claude', '--worktree', 'ui', '--task', '画面を担当']), /--activate multi/);
});

test('workerは別workerを起動できない', () => {
  assert.throws(() => assertOrchestratorRole({ AGENT_HARNESS_ROLE: 'worker' }), /worker.*起動できません/);
});

test('危険なworktree名を拒否する', () => {
  assert.throws(() => validateWorktreeName('../main'), /英小文字/);
  assert.throws(() => validateWorktreeName('main'), /予約/);
});
```

- [ ] **Step 2: テストが失敗することを確認する**

Run: `node --test scripts/agent-harness/spawn-agent.test.mjs`
Expected: `ERR_MODULE_NOT_FOUND`。ランナーはまだ存在しない。

- [ ] **Step 3: 引数・role・worktree検証を実装する**

```js
export function validateWorktreeName(name) {
  if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/u.test(name) || name === 'main') {
    throw new Error('worktree名はmain以外の英小文字・数字・ハイフンで指定してください。');
  }
  return name;
}

export function assertOrchestratorRole(env = process.env) {
  if (env.AGENT_HARNESS_ROLE === 'worker') {
    throw new Error('worker環境から別のエージェントは起動できません。親へ結果を返してください。');
  }
}
```

`--check` は単独で許可し、それ以外では `--activate multi`、`--parent codex|claude`、`--agent codex|claude`、`--worktree`、空でない`--task`を必須にする。未知の引数と同一オプションの重複を拒否する。

- [ ] **Step 4: workerプロンプトとGit worktree作成を実装する**

```js
export function buildWorkerPrompt({ parentAgent, task, worktreePath }) {
  return `あなたは ${parentAgent} が起動したworkerです。\n担当タスク:\n${task}\n\n` +
    `編集できるのは ${worktreePath} 内で、担当タスクに必要なファイルだけです。\n` +
    '別エージェントを起動せず、.envや認証情報を読まず、危険なGit操作をしないでください。\n' +
    '関連テストを実行し、変更を1つの通常コミットにします。\n' +
    '最後に指定されたJSON Schemaだけに従い、status、summary、filesChanged、tests、issues、commitを返してください。';
}

const worktreePath = join(dirname(repoRoot), `${basename(repoRoot)}-worktrees`, options.worktree);
runRequired('git', ['worktree', 'add', '-b', `agent/${options.worktree}`, worktreePath, 'HEAD']);
```

既存パスまたは `agent/<worktree名>` があるときは失敗する。`--force`、削除系・統合系Gitコマンドを呼ばない。

- [ ] **Step 5: CLI実行、結果正規化、check-onlyを実装する**

```js
export function runCheck(run) {
  return { git: run('git', ['--version']), codex: run(executableForPlatform('codex'), ['--version']), claude: run(executableForPlatform('claude'), ['--version']) };
}

const childEnvironment = { ...process.env, AGENT_HARNESS_ROLE: 'worker', AGENT_HARNESS_PARENT: options.parentAgent };
const child = run(command.executable, command.args, { cwd: worktreePath, env: childEnvironment });
console.log(JSON.stringify(normalizeWorkerResult(options.agent, child, temporaryResultPath)));
```

Codexの最終メッセージ一時ファイルはworktree内の`.agent-harness/`に置き、読み取り後に削除する。Claude Codeの`result`文字列をJSONとして読み、両方を同じSchemaで検証する。失敗時も `status: "failed"`、標準エラー要約、空配列、空`commit`を返す。

- [ ] **Step 6: Claude workerの最小Git許可を追加する**

`.claude/settings.json` の `permissions.allow` に次の2項目だけを追加する。

```json
"Bash(git add*)",
"Bash(git commit*)"
```

force push、mainへのpush、merge、reset、cleanを許可しない。

- [ ] **Step 7: テストを通してコミットする**

Run: `node --test scripts/agent-harness/spawn-agent.test.mjs`
Expected: PASS。テストは注入した偽の`run`関数を使い、実際の`git worktree add`、`codex exec`、`claude -p`を呼ばない。

```bash
git add scripts/agent-harness/spawn-agent.mjs scripts/agent-harness/spawn-agent.test.mjs .claude/settings.json
git commit -m "feat(harness): 双方向workerランナーを追加" -m "明示的有効化、worktree分離、再帰防止、構造化結果を共通ランナーで扱う。" -m "Refs #132"
```

### Task 3: 共通安全フックとSkill同期を復旧する

**Files:**
- Restore: `.codex/hooks.json`
- Delete: `.agents/skills/brainstorming/`, `.agents/skills/dispatching-parallel-agents/`, `.agents/skills/executing-plans/`, `.agents/skills/finishing-a-development-branch/`, `.agents/skills/receiving-code-review/`
- Delete: `.agents/skills/requesting-code-review/`, `.agents/skills/subagent-driven-development/`, `.agents/skills/systematic-debugging/`, `.agents/skills/test-driven-development/`, `.agents/skills/using-git-worktrees/`
- Delete: `.agents/skills/using-superpowers/`, `.agents/skills/verification-before-completion/`, `.agents/skills/writing-plans/`, `.agents/skills/writing-skills/`
- Test: `.codex/hooks/config.test.mjs`, `scripts/agent-harness/check-skill-parity.test.mjs`

**Interfaces:**
- Consumes: `.claude/hooks/*.mjs`
- Produces: Codex / Claude Codeが同じ安全フックを使い、追跡済みSkillだけが同期対象の状態

- [ ] **Step 1: 復元前の失敗を確認する**

Run: `node --test .codex/hooks/config.test.mjs scripts/agent-harness/check-skill-parity.test.mjs`
Expected: FAIL。`hooks.json`不在とCodex側だけにある未追跡Skillsが検出される。

- [ ] **Step 2: フックを復元し、未追跡外部Skillを削除する**

HEADの`.codex/hooks.json`を復元し、`guard-command.mjs`、`loop-guard.mjs`、`mark-verified.mjs`、`session-start.mjs`、`verify-gate.mjs`を`.claude/hooks/`から呼ぶ設定とWindows用`commandWindows`を保持する。

列挙した14ディレクトリだけを削除する。これらは片側へ混入した外部Skillの複製であり、ユーザー領域で管理する。追跡済みSkillは変更しない。

- [ ] **Step 3: 整合テストを通してコミットする**

Run: `node --test .codex/hooks/config.test.mjs scripts/agent-harness/check-skill-parity.test.mjs`
Expected: PASS。Codexフック設定を読み、Skillツリーに差がない。

```bash
git add .codex/hooks.json
git add -u .agents/skills
git commit -m "chore(harness): 共有フックとSkill同期を復旧" -m "Codexの共有安全フックを復元し、リポジトリへ混入した外部Skillをユーザー領域管理へ戻す。" -m "Refs #132"
```

### Task 4: 共通ルールと運用文書を更新する

**Files:**
- Modify: `AGENTS.md`, `CLAUDE.md`, `.codex/README.md`
- Modify: `docs/agent-harness/README.md`, `docs/agent-harness/evaluation-cases.md`, `docs/agent-harness/threat-model.md`, `docs/agent-harness/tool-parity.md`, `docs/agent-harness/changelog.md`
- Create: `docs/agent-harness/multi-agent.md`

**Interfaces:**
- Consumes: `spawn-agent.mjs --help`、`spawn-agent.mjs --check`
- Produces: 両エージェントが同じ発動条件、worker制約、統合手順を参照できる文書

- [ ] **Step 1: `AGENTS.md`に短い共通規則を追加する**

```markdown
## 双方向サブエージェント

- 通常の依頼では、現在のエージェントだけで作業する。
- `/multi`、`/parallel`、または明確な並列委譲の依頼がある場合だけ、親は共通ランナーを使える。
- workerは別エージェントを起動せず、専用worktree内で担当範囲だけを変更・検証・コミットし、構造化結果を親へ返す。
- 親は担当重複を避け、結果と差分をレビューする。worker結果の自動統合、マージ、worktree削除は行わない。
```

- [ ] **Step 2: 詳細文書と各入口を更新する**

`CLAUDE.md`と`.codex/README.md`は規則を複製せず、共通規則・詳細文書・各CLIの起動例だけを案内する。`multi-agent.md`には通常モード、明示的有効化、モデル上限、結果JSON、worktree確認、統合前レビュー、CLI未導入時の対処、削除を自動化しない理由を記載する。

```powershell
node scripts/agent-harness/spawn-agent.mjs --check
node scripts/agent-harness/spawn-agent.mjs --activate multi --parent codex --agent claude --worktree login-ui --task "frontend/src/配下のログイン画面だけを担当し、テストとコミットを行う"
node scripts/agent-harness/spawn-agent.mjs --activate multi --parent claude --agent codex --worktree auth-api --task "backend/app/routes/配下の認証APIだけを担当し、テストとコミットを行う"
```

- [ ] **Step 3: 評価・脅威・対応表を更新する**

`evaluation-cases.md`に有効化なしの拒否、worker再帰拒否、Terra/High、Sonnet/High、`--check`非起動性を追加する。`threat-model.md`に無断委譲、再帰委譲、高性能モデル使用、worktree衝突と対策を追加する。`tool-parity.md`に共通ランナーと構造化出力を対応付ける。

- [ ] **Step 4: 文書導線を検査してコミットする**

Run: `rg -n 'spawn-agent\.mjs|multi-agent\.md|/multi|/parallel' AGENTS.md CLAUDE.md .codex/README.md docs/agent-harness`
Expected: 共通規則、Codex案内、Claude案内、詳細文書、評価ケースがすべて見つかる。

```bash
git add AGENTS.md CLAUDE.md .codex/README.md docs/agent-harness
git commit -m "docs(harness): 双方向workerの運用を案内" -m "明示的起動、モデル上限、worktree分離、親によるレビュー手順をCodexとClaude Codeへ共通化する。" -m "Refs #132"
```

### Task 5: 全体検証と差分レビューを行う

**Files:**
- Test: `.claude/hooks/**/*.test.mjs`, `.codex/hooks/**/*.test.mjs`, `scripts/agent-harness/**/*.test.mjs`

**Interfaces:**
- Consumes: Tasks 1–4の設定、ランナー、文書
- Produces: 実workerを起動せずに安全境界・CLI存在確認・既存ハーネスを検証した結果

- [ ] **Step 1: 全ハーネステストを実行する**

Run: `node --test ".claude/hooks/**/*.test.mjs" ".codex/hooks/**/*.test.mjs" "scripts/agent-harness/**/*.test.mjs"`
Expected: PASS。既存フック、Skill同期、設定、ランナーの単体テストがすべて成功する。

- [ ] **Step 2: Skill同期とCLI確認を実行する**

Run: `node scripts/agent-harness/check-skill-parity.mjs`
Expected: `CodexとClaude CodeのSkillsは一致しています。`

Run: `node scripts/agent-harness/spawn-agent.mjs --check`
Expected: Git、Codex、Claude Codeの状態だけをJSONで返す。`git worktree add`、`codex exec`、`claude -p`は実行しない。

- [ ] **Step 3: Codex規則と最終差分を確認する**

Run: `codex execpolicy check --pretty --rules .codex/rules/default.rules -- git push origin main`
Expected: `forbidden`。

Run: `git diff main...HEAD --check`
Expected: 出力なし。

Run: `git status --short --branch`
Expected: 今回の変更がコミット済みで、依頼範囲外の差分を含まない。
