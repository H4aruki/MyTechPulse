import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { writeFileSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { inBlackout, secondsUntilEnd } from "./guards.mjs";

const guards = fileURLToPath(new URL("./guards.mjs", import.meta.url));

test("5:30の窓の境界", () => {
  assert.equal(inBlackout("05:29"), false);
  assert.equal(inBlackout("05:30"), true);
  assert.equal(inBlackout("06:19"), true);
  assert.equal(inBlackout("06:20"), false);
});

test("バックアップの窓 3:50-4:20", () => {
  assert.equal(inBlackout("03:49"), false);
  assert.equal(inBlackout("03:50"), true);
  assert.equal(inBlackout("04:19"), true);
  assert.equal(inBlackout("04:20"), false);
});

test("11:00と16:00の窓", () => {
  assert.equal(inBlackout("11:10"), true);
  assert.equal(inBlackout("11:50"), false);
  assert.equal(inBlackout("16:00"), true);
  assert.equal(inBlackout("16:50"), false);
  assert.equal(inBlackout("12:00"), false);
});

test("日付をまたぐ窓", () => {
  const w = [["23:30", "00:30"]];
  assert.equal(inBlackout("23:29", w), false);
  assert.equal(inBlackout("23:30", w), true);
  assert.equal(inBlackout("00:29", w), true);
  assert.equal(inBlackout("00:30", w), false);
  assert.equal(secondsUntilEnd("23:50", w), 40 * 60);
});

test("窓の終わりまでの秒数", () => {
  assert.equal(secondsUntilEnd("11:10"), 40 * 60);
  assert.equal(secondsUntilEnd("12:00"), 0);
});

test("不正な時刻はエラー", () => {
  assert.throws(() => inBlackout("25:00"));
  assert.throws(() => inBlackout("abc"));
});

test("CLI: 終了コードとルールファイル", () => {
  const run = (...a) => spawnSync("node", [guards, ...a], { encoding: "utf8" });
  assert.equal(run("in-blackout", "--now", "05:30").status, 0);
  assert.equal(run("in-blackout", "--now", "05:29").status, 1);
  assert.equal(run("seconds-until-end", "--now", "11:10").stdout.trim(), "2400");
  assert.equal(run("in-blackout", "--now", "xx").status, 2);
  const f = join(mkdtempSync(join(tmpdir(), "g-")), "r.json");
  writeFileSync(f, JSON.stringify({ mergeGuards: { blackoutWindowsJst: [["09:00", "09:10"]] } }));
  assert.equal(run("in-blackout", "--now", "09:05", "--rules", f).status, 0);
  assert.equal(run("in-blackout", "--now", "05:30", "--rules", f).status, 1);
});
