// release-manifest.json の生成と検証（Node標準APIだけで動く）。
//
// manifestは、同じcommitから作ったAPI image・画面の成果物・運用一式を1つに結び付ける。
// 本番側は同じ形式を ops/verify_release.sh（bashと標準コマンドだけ）で検証するため、
// ここで出力する形式は1文字単位で固定している。形式を変えるときは両方を同時に直すこと。
//
//   node scripts/release-manifest.mjs create --commit-sha ... --api-image ... --dir ... \
//        --run-id ... --run-attempt ... --repository OWNER/REPO --server-url https://github.com \
//        --output release-manifest.json
//   node scripts/release-manifest.mjs verify --manifest ... --manifest-sha256 ... --dir ... \
//        --run-id ... --run-attempt ... [--commit-sha ...]
import { createHash } from "node:crypto";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const COMMIT_SHA = /^[0-9a-f]{40}$/;
const SHA256_HEX = /^[0-9a-f]{64}$/;
const DIGITS = /^[1-9][0-9]*$/;
const API_IMAGE = /^ghcr\.io\/h4aruki\/mytechpulse-api-go@sha256:[0-9a-f]{64}$/;
const REPOSITORY = /^[A-Za-z0-9._-]+\/[A-Za-z0-9._-]+$/;
const WORKFLOW_URL = /^https:\/\/github\.com\/[A-Za-z0-9._-]+\/[A-Za-z0-9._-]+\/actions\/runs\/([1-9][0-9]*)\/attempts\/([1-9][0-9]*)$/;

// 検証で拒否した場合の例外。CLIでは終了コード2にする
export class ManifestError extends Error {}

function fail(message) {
  throw new ManifestError(message);
}

export function sha256File(file) {
  return createHash("sha256").update(readFileSync(file)).digest("hex");
}

function requireMatch(name, value, pattern) {
  if (typeof value !== "string" || !pattern.test(value)) {
    fail(`${name} の形式が正しくありません`);
  }
}

function artifactEntry(kind, commitSha, sha256) {
  return {
    artifact_name: `${kind}-${commitSha}`,
    file: `${kind}-${commitSha}.tar.gz`,
    sha256,
  };
}

function archivePath(dir, file) {
  const full = path.join(dir, file);
  if (!existsSync(full)) {
    fail(`成果物 ${file} が見つかりません`);
  }
  return full;
}

export function buildManifest({ commitSha, apiImage, dir, runId, runAttempt, repository, serverUrl }) {
  requireMatch("commit_sha", commitSha, COMMIT_SHA);
  requireMatch("api_image", apiImage, API_IMAGE);
  requireMatch("run_id", runId, DIGITS);
  requireMatch("run_attempt", runAttempt, DIGITS);
  requireMatch("repository", repository, REPOSITORY);
  if (serverUrl !== "https://github.com") {
    fail("server_url は https://github.com だけを受け付けます");
  }
  const frontend = artifactEntry("frontend", commitSha, "");
  const ops = artifactEntry("ops", commitSha, "");
  frontend.sha256 = sha256File(archivePath(dir, frontend.file));
  ops.sha256 = sha256File(archivePath(dir, ops.file));
  return {
    schema_version: 1,
    commit_sha: commitSha,
    api_image: apiImage,
    frontend,
    ops,
    workflow: {
      run_id: runId,
      run_attempt: runAttempt,
      url: `${serverUrl}/${repository}/actions/runs/${runId}/attempts/${runAttempt}`,
    },
  };
}

export function serializeManifest(manifest) {
  return `${JSON.stringify(manifest, null, 2)}\n`;
}

function exactKeys(name, value, keys) {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    fail(`${name} はオブジェクトである必要があります`);
  }
  const actual = Object.keys(value);
  if (actual.length !== keys.length || actual.some((key, index) => key !== keys[index])) {
    fail(`${name} の項目が決められた構成と一致しません`);
  }
}

function checkArtifact(kind, entry, commitSha) {
  exactKeys(kind, entry, ["artifact_name", "file", "sha256"]);
  const expected = artifactEntry(kind, commitSha, entry.sha256);
  if (entry.artifact_name !== expected.artifact_name) {
    fail(`${kind}.artifact_name が commit_sha と対応していません`);
  }
  if (entry.file !== expected.file) {
    fail(`${kind}.file が commit_sha と対応していません`);
  }
  requireMatch(`${kind}.sha256`, entry.sha256, SHA256_HEX);
}

// 形式だけの検査（ファイルは見ない）。通ったmanifestだけが以降の検査へ進める
export function validateManifest(manifest) {
  exactKeys("manifest", manifest, [
    "schema_version",
    "commit_sha",
    "api_image",
    "frontend",
    "ops",
    "workflow",
  ]);
  if (manifest.schema_version !== 1) {
    fail("schema_version は 1 だけを受け付けます");
  }
  requireMatch("commit_sha", manifest.commit_sha, COMMIT_SHA);
  requireMatch("api_image", manifest.api_image, API_IMAGE);
  checkArtifact("frontend", manifest.frontend, manifest.commit_sha);
  checkArtifact("ops", manifest.ops, manifest.commit_sha);
  exactKeys("workflow", manifest.workflow, ["run_id", "run_attempt", "url"]);
  requireMatch("run_id", manifest.workflow.run_id, DIGITS);
  requireMatch("run_attempt", manifest.workflow.run_attempt, DIGITS);
  const url = WORKFLOW_URL.exec(manifest.workflow.url ?? "");
  if (!url || url[1] !== manifest.workflow.run_id || url[2] !== manifest.workflow.run_attempt) {
    fail("workflow.url が run_id・run_attempt と対応していません");
  }
}

export function verifyManifest({ manifestPath, manifestSha256, dir, runId, runAttempt, commitSha }) {
  requireMatch("manifest_sha256", manifestSha256, SHA256_HEX);
  const text = readFileSync(manifestPath, "utf8");
  if (sha256File(manifestPath) !== manifestSha256) {
    fail("manifestのSHA256が保存済みの値と一致しません");
  }
  let manifest;
  try {
    manifest = JSON.parse(text);
  } catch {
    fail("manifestがJSONとして読めません");
  }
  validateManifest(manifest);
  if (serializeManifest(manifest) !== text) {
    fail("manifestの整形が決められた形式ではありません");
  }
  if (commitSha !== undefined && manifest.commit_sha !== commitSha) {
    fail("manifestの commit_sha が期待するcommitと一致しません");
  }
  if (manifest.workflow.run_id !== runId) {
    fail("manifestの run_id が期待する値と一致しません");
  }
  if (manifest.workflow.run_attempt !== runAttempt) {
    fail("manifestの run_attempt が期待する値と一致しません");
  }
  for (const kind of ["frontend", "ops"]) {
    const entry = manifest[kind];
    if (sha256File(archivePath(dir, entry.file)) !== entry.sha256) {
      fail(`${kind}のsha256が一致しません（${entry.file}）`);
    }
  }
  return manifest;
}

function parseArgs(argv) {
  const options = {};
  for (let index = 0; index < argv.length; index += 2) {
    const key = argv[index];
    const value = argv[index + 1];
    if (!key.startsWith("--") || value === undefined) {
      fail(`引数が正しくありません: ${key}`);
    }
    options[key.slice(2)] = value;
  }
  return options;
}

function need(options, name) {
  if (!options[name]) {
    fail(`--${name} が必要です`);
  }
  return options[name];
}

function main(argv) {
  const [command, ...rest] = argv;
  const options = parseArgs(rest);
  if (command === "create") {
    const dir = need(options, "dir");
    const manifest = buildManifest({
      commitSha: need(options, "commit-sha"),
      apiImage: need(options, "api-image"),
      dir,
      runId: need(options, "run-id"),
      runAttempt: need(options, "run-attempt"),
      repository: need(options, "repository"),
      serverUrl: need(options, "server-url"),
    });
    const output = need(options, "output");
    const text = serializeManifest(manifest);
    writeFileSync(output, text, { flag: "wx" });
    // manifest自身のhashは自己参照にせず、同じ場所の .sha256 に残す
    const hash = createHash("sha256").update(text).digest("hex");
    writeFileSync(`${output}.sha256`, `${hash}  ${path.basename(output)}\n`, { flag: "wx" });
    console.log(hash);
    return;
  }
  if (command === "verify") {
    verifyManifest({
      manifestPath: need(options, "manifest"),
      manifestSha256: need(options, "manifest-sha256"),
      dir: need(options, "dir"),
      runId: need(options, "run-id"),
      runAttempt: need(options, "run-attempt"),
      commitSha: options["commit-sha"],
    });
    console.log("release manifestの検証に合格しました");
    return;
  }
  fail("使い方: release-manifest.mjs create|verify ...");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    main(process.argv.slice(2));
  } catch (error) {
    if (error instanceof ManifestError) {
      console.error(`拒否: ${error.message}`);
      process.exit(2);
    }
    throw error;
  }
}
