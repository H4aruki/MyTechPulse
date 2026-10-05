import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { buildManifest, verifyManifest } from "./release-manifest.mjs";

const COMMIT = "0123456789abcdef0123456789abcdef01234567";
const OTHER_COMMIT = "fedcba9876543210fedcba9876543210fedcba98";
const DIGEST = "a".repeat(64);
const IMAGE = `ghcr.io/h4aruki/mytechpulse-api-go@sha256:${DIGEST}`;
const scriptPath = fileURLToPath(new URL("./release-manifest.mjs", import.meta.url));

function sha256(buffer) {
  return createHash("sha256").update(buffer).digest("hex");
}

// 3成果物の置き場を作る。archive本体の中身は検査に関係しないので適当な文字列にする
function makeFixture() {
  const dir = mkdtempSync(path.join(tmpdir(), "mtp-manifest-"));
  writeFileSync(path.join(dir, `frontend-${COMMIT}.tar.gz`), "frontend-bytes");
  writeFileSync(path.join(dir, `ops-${COMMIT}.tar.gz`), "ops-bytes");
  return dir;
}

function baseOptions(dir) {
  return {
    commitSha: COMMIT,
    apiImage: IMAGE,
    dir,
    runId: "123456789",
    runAttempt: "2",
    repository: "H4aruki/MyTechPulse",
    serverUrl: "https://github.com",
  };
}

// manifest本体とそのSHA256を、検証側が受け取る形で保存する
function writeManifest(dir, manifest, text = `${JSON.stringify(manifest, null, 2)}\n`) {
  const manifestPath = path.join(dir, "release-manifest.json");
  writeFileSync(manifestPath, text);
  return { manifestPath, manifestSha256: sha256(Buffer.from(text)) };
}

function verifyOptions(dir, manifestPath, manifestSha256, overrides = {}) {
  return {
    manifestPath,
    manifestSha256,
    dir,
    runId: "123456789",
    runAttempt: "2",
    commitSha: COMMIT,
    ...overrides,
  };
}

function cleanup(dir) {
  rmSync(dir, { recursive: true, force: true });
}

test("manifestは固定schemaで、実際のarchiveのhashとrun情報を持つ", () => {
  const dir = makeFixture();
  try {
    const manifest = buildManifest(baseOptions(dir));
    assert.deepEqual(manifest, {
      schema_version: 1,
      commit_sha: COMMIT,
      api_image: IMAGE,
      frontend: {
        artifact_name: `frontend-${COMMIT}`,
        file: `frontend-${COMMIT}.tar.gz`,
        sha256: sha256(Buffer.from("frontend-bytes")),
      },
      ops: {
        artifact_name: `ops-${COMMIT}`,
        file: `ops-${COMMIT}.tar.gz`,
        sha256: sha256(Buffer.from("ops-bytes")),
      },
      workflow: {
        run_id: "123456789",
        run_attempt: "2",
        url: "https://github.com/H4aruki/MyTechPulse/actions/runs/123456789/attempts/2",
      },
    });
  } finally {
    cleanup(dir);
  }
});

test("正しいmanifestとarchiveは検証を通る", () => {
  const dir = makeFixture();
  try {
    const { manifestPath, manifestSha256 } = writeManifest(dir, buildManifest(baseOptions(dir)));
    assert.doesNotThrow(() => verifyManifest(verifyOptions(dir, manifestPath, manifestSha256)));
  } finally {
    cleanup(dir);
  }
});

test("タグだけのimage、短いdigest、別repositoryのimageは作れない", () => {
  const dir = makeFixture();
  try {
    const bad = [
      "ghcr.io/h4aruki/mytechpulse-api-go:latest",
      `ghcr.io/h4aruki/mytechpulse-api-go:${COMMIT}`,
      `ghcr.io/h4aruki/mytechpulse-api-go@sha256:${DIGEST.slice(0, 63)}`,
      `ghcr.io/h4aruki/mytechpulse-api-go@sha256:${"A".repeat(64)}`,
      `ghcr.io/other/mytechpulse-api-go@sha256:${DIGEST}`,
      `docker.io/h4aruki/mytechpulse-api-go@sha256:${DIGEST}`,
    ];
    for (const apiImage of bad) {
      assert.throws(() => buildManifest({ ...baseOptions(dir), apiImage }), /api_image/, apiImage);
    }
  } finally {
    cleanup(dir);
  }
});

test("commit SHAが40桁の小文字16進数でなければ作れない", () => {
  const dir = makeFixture();
  try {
    for (const commitSha of [COMMIT.slice(0, 39), COMMIT.toUpperCase(), "main"]) {
      assert.throws(() => buildManifest({ ...baseOptions(dir), commitSha }), /commit_sha/);
    }
  } finally {
    cleanup(dir);
  }
});

test("archiveが欠けていると作れず、検証も通らない", () => {
  const dir = makeFixture();
  try {
    const { manifestPath, manifestSha256 } = writeManifest(dir, buildManifest(baseOptions(dir)));
    rmSync(path.join(dir, `ops-${COMMIT}.tar.gz`));
    assert.throws(() => buildManifest(baseOptions(dir)), /ops-/);
    assert.throws(
      () => verifyManifest(verifyOptions(dir, manifestPath, manifestSha256)),
      /ops-.*見つかりません/,
    );
  } finally {
    cleanup(dir);
  }
});

test("archiveが1byteでも変わると検証を拒否する", () => {
  const dir = makeFixture();
  try {
    const { manifestPath, manifestSha256 } = writeManifest(dir, buildManifest(baseOptions(dir)));
    writeFileSync(path.join(dir, `frontend-${COMMIT}.tar.gz`), "frontend-bytez");
    assert.throws(
      () => verifyManifest(verifyOptions(dir, manifestPath, manifestSha256)),
      /frontend.*sha256/,
    );
  } finally {
    cleanup(dir);
  }
});

test("APIのimageだけ差し替えたmanifestは、保存済みSHA256と合わず拒否する", () => {
  const dir = makeFixture();
  try {
    const original = buildManifest(baseOptions(dir));
    const { manifestSha256 } = writeManifest(dir, original);
    const swapped = { ...original, api_image: `ghcr.io/h4aruki/mytechpulse-api-go@sha256:${"b".repeat(64)}` };
    const { manifestPath } = writeManifest(dir, swapped);
    assert.throws(
      () => verifyManifest(verifyOptions(dir, manifestPath, manifestSha256)),
      /manifest.*SHA256/,
    );
  } finally {
    cleanup(dir);
  }
});

test("mutable tagに書き換えたmanifestは、hashを合わせても拒否する", () => {
  const dir = makeFixture();
  try {
    const original = buildManifest(baseOptions(dir));
    const tampered = { ...original, api_image: "ghcr.io/h4aruki/mytechpulse-api-go:latest" };
    const { manifestPath, manifestSha256 } = writeManifest(dir, tampered);
    assert.throws(
      () => verifyManifest(verifyOptions(dir, manifestPath, manifestSha256)),
      /api_image/,
    );
  } finally {
    cleanup(dir);
  }
});

test("commitやrunが違う組合せを拒否する", () => {
  const dir = makeFixture();
  try {
    const { manifestPath, manifestSha256 } = writeManifest(dir, buildManifest(baseOptions(dir)));
    assert.throws(
      () => verifyManifest(verifyOptions(dir, manifestPath, manifestSha256, { commitSha: OTHER_COMMIT })),
      /commit_sha/,
    );
    assert.throws(
      () => verifyManifest(verifyOptions(dir, manifestPath, manifestSha256, { runId: "999" })),
      /run_id/,
    );
    assert.throws(
      () => verifyManifest(verifyOptions(dir, manifestPath, manifestSha256, { runAttempt: "3" })),
      /run_attempt/,
    );
  } finally {
    cleanup(dir);
  }
});

test("archive名がcommit SHAとずれたmanifestは拒否する", () => {
  const dir = makeFixture();
  try {
    const original = buildManifest(baseOptions(dir));
    const tampered = { ...original, frontend: { ...original.frontend, file: `frontend-${OTHER_COMMIT}.tar.gz` } };
    const { manifestPath, manifestSha256 } = writeManifest(dir, tampered);
    assert.throws(
      () => verifyManifest(verifyOptions(dir, manifestPath, manifestSha256)),
      /frontend\.file/,
    );
  } finally {
    cleanup(dir);
  }
});

test("余計な項目や整形違いのmanifestは拒否する", () => {
  const dir = makeFixture();
  try {
    const original = buildManifest(baseOptions(dir));
    const extra = writeManifest(dir, { ...original, note: "x" });
    assert.throws(() => verifyManifest(verifyOptions(dir, extra.manifestPath, extra.manifestSha256)), /項目/);
    const compact = writeManifest(dir, original, JSON.stringify(original));
    assert.throws(() => verifyManifest(verifyOptions(dir, compact.manifestPath, compact.manifestSha256)), /整形/);
  } finally {
    cleanup(dir);
  }
});

test("CLI: createで作ったmanifestをverifyが受け入れ、改変後は終了コード2で拒否する", () => {
  const dir = makeFixture();
  try {
    const create = spawnSync(
      process.execPath,
      [
        scriptPath, "create",
        "--commit-sha", COMMIT, "--api-image", IMAGE, "--dir", dir,
        "--run-id", "123456789", "--run-attempt", "2",
        "--repository", "H4aruki/MyTechPulse", "--server-url", "https://github.com",
        "--output", path.join(dir, "release-manifest.json"),
      ],
      { encoding: "utf8" },
    );
    assert.equal(create.status, 0, create.stderr);
    const shaFile = readFileSync(path.join(dir, "release-manifest.json.sha256"), "utf8");
    assert.match(shaFile, /^[0-9a-f]{64} {2}release-manifest\.json\n$/);

    const verifyArgs = [
      scriptPath, "verify",
      "--manifest", path.join(dir, "release-manifest.json"),
      "--manifest-sha256", shaFile.slice(0, 64),
      "--dir", dir, "--run-id", "123456789", "--run-attempt", "2", "--commit-sha", COMMIT,
    ];
    const ok = spawnSync(process.execPath, verifyArgs, { encoding: "utf8" });
    assert.equal(ok.status, 0, ok.stderr);

    writeFileSync(path.join(dir, `ops-${COMMIT}.tar.gz`), "ops-bytez");
    const ng = spawnSync(process.execPath, verifyArgs, { encoding: "utf8" });
    assert.equal(ng.status, 2);
    assert.match(ng.stderr, /ops.*sha256/);
  } finally {
    cleanup(dir);
  }
});
