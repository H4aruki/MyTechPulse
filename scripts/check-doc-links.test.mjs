import assert from "node:assert/strict";
import test from "node:test";
import { extractLinks, findBrokenLinks } from "./check-doc-links.mjs";

const existing = new Set(["README.md", "docs/a.md", "docs/deploy/b.md", "CONTRIBUTING.md"]);
const exists = (file) => existing.has(file);

test("リンク先を取り出す", () => {
  assert.deepEqual(extractLinks("見て [A](./docs/a.md) と [B](https://example.com/x) と ![画像](img/x.png)"), [
    "./docs/a.md",
    "https://example.com/x",
    "img/x.png",
  ]);
});

test("コードブロックとインラインコードの中は、例示なので見ない", () => {
  const markdown = ["```", "[例](./no.md)", "```", "文中の `[例](./no2.md)` も見ない", "[本物](./real.md)"].join("\n");
  assert.deepEqual(extractLinks(markdown), ["./real.md"]);
});

test("存在するファイルへの相対リンクは、切れていない", () => {
  assert.deepEqual(findBrokenLinks("README.md", "[a](./docs/a.md) [b](docs/deploy/b.md#見出し)", exists), []);
  assert.deepEqual(findBrokenLinks("docs/deploy/c.md", "[a](../a.md) [r](../../README.md)", exists), []);
});

test("存在しないファイルへのリンクを検出する", () => {
  assert.deepEqual(findBrokenLinks("README.md", "[x](./docs/missing.md)", exists), ["./docs/missing.md"]);
  assert.deepEqual(findBrokenLinks("docs/deploy/c.md", "[x](../missing.md#a)", exists), ["../missing.md#a"]);
});

test("外部URL・メール・文書内の見出しへのリンクは対象外", () => {
  assert.deepEqual(findBrokenLinks("README.md", "[a](https://example.com) [b](mailto:a@b.c) [c](#見出し)", exists), []);
});

test("ルートからの絶対パス（/で始まる）も、リポジトリの直下として確かめる", () => {
  assert.deepEqual(findBrokenLinks("docs/deploy/c.md", "[a](/README.md) [b](/nothing.md)", exists), ["/nothing.md"]);
});

test("題名付きのリンクも読める", () => {
  assert.deepEqual(extractLinks('[a](./x.md "題名")'), ["./x.md"]);
});
