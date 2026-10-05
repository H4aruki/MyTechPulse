import assert from "node:assert/strict";
import { test } from "node:test";
import { frontendTable, goTable, marker, render } from "./coverage-report.mjs";

test("go testの出力からパッケージごとの割合を取り出す", () => {
  const log = [
    "ok  \tgithub.com/x/server/internal/app\t0.5s\tcoverage: 71.2% of statements",
    "?   \tgithub.com/x/server/cmd/api\t[no test files]",
    "FAIL\tgithub.com/x/server/internal/bad\t0.1s",
  ].join("\n");
  assert.deepEqual(goTable(log), [{ name: "internal/app", pct: 71.2 }]);
});

test("画面側は全体の行カバー率と各ファイルを取り出す", () => {
  const { rows, total } = frontendTable({
    total: { lines: { pct: 50 } },
    "/w/frontend/src/a.ts": { lines: { pct: 40 } },
  });
  assert.equal(total, 50);
  assert.deepEqual(rows, [{ name: "src/a.ts", pct: 40 }]);
});

test("表には目印と低い順の内訳が入る", () => {
  const md = render("Go", [
    { name: "b", pct: 90 },
    { name: "a", pct: 10 },
  ]);
  assert.ok(md.startsWith(marker("Go")));
  assert.ok(md.indexOf("`a`") < md.indexOf("`b`"));
  assert.match(md, /パッケージ平均: \*\*50\.0%\*\*/);
});

test("計測結果が空でも壊れない", () => {
  assert.match(render("Go", []), /計測結果がありません/);
});
