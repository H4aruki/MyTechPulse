import assert from "node:assert/strict";
import test from "node:test";
import { findStaleTerms } from "./check-current-docs.mjs";

test("旧構成の言葉が混ざった行を、行番号つきで返す", () => {
  const markdown = ["きれいな行", "認証はJWTで行う", "", "フロントは localStorage に保存する"].join("\n");
  assert.deepEqual(findStaleTerms(markdown), [
    { line: 2, term: "JWT" },
    { line: 4, term: "localStorage" },
  ]);
});

test("旧版との違いを説明する行は許す", () => {
  assert.deepEqual(findStaleTerms("旧版（Python）では FastAPI と JWT を使っていた"), []);
  assert.deepEqual(findStaleTerms("Python版のBearerトークンは廃止した"), []);
  assert.deepEqual(findStaleTerms("_Avoid_: 合言葉、アクセストークン、JWT"), []);
});

test("単語の一部は拾わない（JWTの前後が英字なら別の語）", () => {
  assert.deepEqual(findStaleTerms("NOTJWTS の話ではない"), []);
});

test("コードブロックの中の古い例も見つける", () => {
  assert.deepEqual(findStaleTerms(["```", "Authorization: Bearer xxx", "```"].join("\n")), [{ line: 2, term: "Bearer" }]);
});

test("backend/ やFastAPIなど、他の旧語も見つける", () => {
  const found = findStaleTerms("cd backend/app\nFastAPIで動く\nuvicornを起動");
  assert.deepEqual(found.map((f) => f.term), ["backend/", "FastAPI", "uvicorn"]);
});
