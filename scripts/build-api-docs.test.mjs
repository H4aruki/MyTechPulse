import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  DEFAULT_OUT,
  DEFAULT_SPEC,
  GENERATOR_MARKER,
  MAX_BLOCK_ITEMS,
  MAX_DEPTH,
  buildHtml,
  escapeHtml,
  exampleFromSchema,
  parseArgs,
  resolveSchema,
  schemaRows,
} from "./build-api-docs.mjs";

const scriptPath = fileURLToPath(new URL("./build-api-docs.mjs", import.meta.url));
const realSpec = JSON.parse(readFileSync(DEFAULT_SPEC, "utf8"));
const METHODS = ["get", "put", "post", "delete", "options", "head", "patch", "trace"];

function withTempDir(fn) {
  const dir = mkdtempSync(path.join(tmpdir(), "build-api-docs-"));
  try {
    return fn(dir);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

function runCli(args, options = {}) {
  return spawnSync(process.execPath, [scriptPath, ...args], { encoding: "utf8", ...options });
}

// シンボリックリンクを作れない環境（権限の無いWindowsなど）では、そのテストを飛ばす
function trySymlink(t, target, link) {
  try {
    symlinkSync(target, link);
    return true;
  } catch (error) {
    t.skip(`シンボリックリンクを作れません: ${error.code}`);
    return false;
  }
}

// 合成spec。schemas だけ差し替えて使う
function specWith(schemas, extra = {}) {
  return { openapi: "3.1.0", info: { title: "T", version: "1" }, paths: {}, components: { schemas }, ...extra };
}

test("実際の openapi.json の全ての操作（operationId・メソッド・パス）が出力に含まれる", () => {
  const html = buildHtml(realSpec);
  let count = 0;
  for (const [route, item] of Object.entries(realSpec.paths)) {
    for (const method of METHODS) {
      if (!item[method]) continue;
      count += 1;
      const op = item[method];
      assert.ok(html.includes(`id="op-${op.operationId}"`), `${op.operationId} の見出しがある`);
      assert.ok(html.includes(`>${method.toUpperCase()}</span><code>${escapeHtml(route)}</code>`), `${method} ${route} がある`);
      assert.ok(html.includes(escapeHtml(op.summary)), `${op.operationId} のsummaryがある`);
    }
  }
  assert.ok(count >= 20, `操作を ${count} 件読めている`);
  assert.ok(html.includes(`操作は ${count} 件`));
});

test("実際のspecの出力に、スクリプト・外部URLの読み込み・fetchが無い", () => {
  const html = buildHtml(realSpec);
  assert.doesNotMatch(html, /<script/i);
  assert.doesNotMatch(html, /\b(?:src|href|action|data)\s*=\s*["']?\s*(?:https?:)?\/\//i);
  assert.doesNotMatch(html, /<(?:link|iframe|img|form|object|embed)\b/i);
  assert.doesNotMatch(html, /\bon[a-z]+\s*=/i);
  assert.doesNotMatch(html, /javascript:|@import|url\(/i);
  assert.doesNotMatch(html, /fetch\(/);
  assert.doesNotMatch(html, /https?:\/\//i, "出力のどこにも http(s):// が無い");
  assert.match(html, /<meta http-equiv="Content-Security-Policy" content="default-src &#39;none&#39;; style-src &#39;unsafe-inline&#39;;/);
  assert.match(html, /<html lang="ja">/);
});

test("実際のspecの出力に、認証・パラメーター・ボディ・レスポンスヘッダー・JSON例が出る", () => {
  const html = buildHtml(realSpec);
  assert.match(html, /認証: 必要 — <code>cookieAuth<\/code>/);
  assert.match(html, /認証: 不要/);
  assert.match(html, /<td>path<\/td><td><code>id<\/code><\/td><td>必須<\/td><td>integer \(int64\)<\/td>/);
  assert.match(html, /Content-Type: <code>application\/problem\+json<\/code>/);
  assert.match(html, /<code>Set-Cookie<\/code>/);
  assert.match(html, /<h4>リクエストボディ（必須）<\/h4>/);
  assert.match(html, /&quot;username&quot;: &quot;synthetic-user&quot;/);
});

test("descriptionに入れた <img onerror> や <script> はエスケープされ、タグとして出ない", () => {
  const spec = {
    openapi: "3.1.0",
    info: { title: "<b>題</b>", version: "1", description: "概要 <img src=x onerror=alert(1)>" },
    tags: [{ name: "t<script>", description: "<script>tag()</script>" }],
    paths: {
      "/x/{p}": {
        get: {
          operationId: 'a"><img onerror=1>',
          tags: ["t<script>"],
          summary: "<img src=x onerror=alert(2)>",
          description: "説明 <img src=x onerror=alert(3)> と `<code-ish>`",
          parameters: [{ in: "path", name: "p<i>", required: true, description: "<svg onload=1>", schema: { type: "string", enum: ["<a>"] } }],
          responses: {
            "200": {
              description: "<iframe src=//evil>",
              headers: { "X-<h>": { description: "<u>h</u>", schema: { type: "string" } } },
              content: { "application/json": { schema: { type: "object", properties: { "k<k>": { type: "string", description: "<img onerror=4>" } } } } },
            },
          },
        },
      },
    },
    components: { securitySchemes: { s: { type: "apiKey", in: "header", name: "X", description: "<img onerror=5>" } } },
    security: [{ s: [] }],
  };
  const html = buildHtml(spec);
  assert.doesNotMatch(html, /<img/i);
  assert.doesNotMatch(html, /<script/i);
  assert.doesNotMatch(html, /<svg|<iframe|<b>|<u>|<h>|<i>/i);
  assert.ok(html.includes("&lt;img src=x onerror=alert(1)&gt;"));
  assert.ok(html.includes("概要 &lt;img"));
  assert.ok(html.includes("<code>&lt;code-ish&gt;</code>"), "`...` だけがcodeタグになり、中身はエスケープされる");
  assert.ok(html.includes('id="op-a-img-onerror-1-"'), "属性値に使うIDは安全な文字だけにする");
  assert.ok(html.includes("&lt;script&gt;tag()&lt;/script&gt;"));
});

test("escapeHtml は5文字をエスケープし、null と undefined は空文字にする", () => {
  assert.equal(escapeHtml(`<a href="x">&'</a>`), "&lt;a href=&quot;x&quot;&gt;&amp;&#39;&lt;/a&gt;");
  assert.equal(escapeHtml(null), "");
  assert.equal(escapeHtml(undefined), "");
  assert.equal(escapeHtml(12), "12");
});

test("$ref は components から解決され、スキーマの表に子の項目が出る", () => {
  const spec = specWith({
    Outer: { type: "object", required: ["inner"], properties: { inner: { $ref: "#/components/schemas/Inner" }, list: { type: "array", items: { $ref: "#/components/schemas/Inner" } } } },
    Inner: { type: "object", required: ["id"], properties: { id: { type: "integer", description: "識別子", minimum: 1 }, name: { type: "string" } } },
  });
  const rows = schemaRows(spec, { $ref: "#/components/schemas/Outer" });
  assert.deepEqual(
    rows.map((r) => [r.name, r.type, r.required]),
    [
      ["inner", "Inner", true],
      ["inner.id", "integer", true],
      ["inner.name", "string", false],
      ["list", "Inner[]", false],
      ["list[].id", "integer", true],
      ["list[].name", "string", false],
    ],
  );
  assert.equal(rows[1].description, "識別子");
  assert.deepEqual(rows[1].constraints, ["minimum: 1"]);
});

test("解決できない $ref と外部 $ref はエラーになる", () => {
  const spec = specWith({});
  assert.throws(() => resolveSchema(spec, { $ref: "#/components/schemas/Missing" }), /解決できません/);
  assert.throws(() => resolveSchema(spec, { $ref: "https://example.com/a.json" }), /外部参照/);
});

test("循環参照は打ち切られ、表に注記が出て、JSON例も無限に続かない", () => {
  const spec = specWith({
    Node: { type: "object", properties: { value: { type: "string" }, next: { $ref: "#/components/schemas/Node" }, children: { type: "array", items: { $ref: "#/components/schemas/Node" } } } },
  });
  const rows = schemaRows(spec, { $ref: "#/components/schemas/Node" });
  assert.deepEqual(rows.map((r) => r.name), ["value", "next", "children", "children[]"], "要素が循環する配列は、要素の注記行まで出して止まる");
  assert.match(rows[1].notes[0], /循環参照のため Node/);
  assert.equal(rows[1].type, "Node");
  assert.equal(rows[2].type, "Node[]");
  assert.match(rows[3].notes[0], /循環参照のため Node/);
  assert.deepEqual(exampleFromSchema(spec, { $ref: "#/components/schemas/Node" }), { value: "string", children: [] });

  const mutual = specWith({
    A: { type: "object", properties: { b: { $ref: "#/components/schemas/B" } } },
    B: { type: "object", properties: { a: { $ref: "#/components/schemas/A" } } },
  });
  assert.deepEqual(exampleFromSchema(mutual, { $ref: "#/components/schemas/A" }), { b: {} });
  assert.match(buildHtml({ ...mutual, paths: { "/a": { get: { responses: { 200: { description: "ok", content: { "application/json": { schema: { $ref: "#/components/schemas/A" } } } } } } } } }), /循環参照のため A の中身は展開しません/);
});

test("深すぎる入れ子は深さ制限で打ち切られる", () => {
  let schema = { type: "string" };
  for (let i = 0; i < MAX_DEPTH + 4; i += 1) schema = { type: "object", properties: { n: schema } };
  const rows = schemaRows(specWith({}), schema);
  const last = rows.at(-1);
  assert.match(last.notes[0], new RegExp(`深さ制限（${MAX_DEPTH}階層）`));
  assert.ok(rows.length <= MAX_DEPTH + 2);
  let example = exampleFromSchema(specWith({}), schema);
  let depth = 0;
  while (example && typeof example === "object") {
    example = example.n;
    depth += 1;
  }
  assert.ok(depth <= MAX_DEPTH + 1, `例の深さ ${depth} が制限内`);
});

test("JSON例: 型ごとの既定値とformat", () => {
  const spec = specWith({});
  const ex = (schema) => exampleFromSchema(spec, schema);
  assert.equal(ex({ type: "string" }), "string");
  assert.equal(ex({ type: "string", format: "date-time" }), "2026-01-01T00:00:00Z");
  assert.equal(ex({ type: "string", format: "date" }), "2026-01-01");
  assert.equal(ex({ type: "string", format: "email" }), "user@example.com");
  assert.equal(ex({ type: "string", format: "uuid" }), "00000000-0000-0000-0000-000000000000");
  assert.equal(ex({ type: "string", format: "uri" }), "https://example.com/");
  assert.equal(ex({ type: "integer" }), 0);
  assert.equal(ex({ type: "integer", minimum: 1 }), 1);
  assert.equal(ex({ type: "number" }), 0);
  assert.equal(ex({ type: "boolean" }), true);
  assert.deepEqual(ex({ type: "array", items: { type: "string" } }), ["string"]);
  assert.deepEqual(ex({ type: "array" }), []);
  assert.deepEqual(ex({ type: "object", properties: { a: { type: "integer" }, b: { type: "boolean" } } }), { a: 0, b: true });
  assert.deepEqual(ex({ type: "object", additionalProperties: { type: "integer" } }), { key: 0 });
  assert.deepEqual(ex({ type: "object" }), {});
  assert.equal(ex({ type: ["null", "string"] }), "string", "nullを除いた最初の型を使う");
  assert.deepEqual(ex({ type: ["array", "null"], items: { type: "integer" } }), [0]);
  assert.equal(ex({ type: "null" }), null);
  assert.equal(ex({}), undefined);
});

test("JSON例: example・examples・default・enum の優先順位", () => {
  const spec = specWith({});
  const ex = (schema) => exampleFromSchema(spec, schema);
  assert.equal(ex({ type: "string", example: "E", examples: ["X"], default: "D", enum: ["N"] }), "E");
  assert.equal(ex({ type: "string", examples: ["X", "Y"], default: "D", enum: ["N"] }), "X");
  assert.equal(ex({ type: "integer", examples: [401] }), 401);
  assert.equal(ex({ type: "string", examples: { first: { value: "V" } } }), "V");
  assert.equal(ex({ type: "string", default: "D", enum: ["N"] }), "D");
  assert.equal(ex({ type: "string", enum: ["Qiita", "Zenn"] }), "Qiita");
  assert.equal(ex({ type: "integer", const: 7 }), 7);
  assert.deepEqual(ex({ type: "object", example: { z: 1 }, properties: { a: { type: "string" } } }), { z: 1 }, "objectのexampleは、プロパティからの生成より優先");
  assert.deepEqual(ex({ type: "object", properties: { s: { enum: ["a", "b"] }, n: { type: "integer", examples: [5] } } }), { s: "a", n: 5 });
});

test("JSON例とスキーマ表: allOf は結合、oneOf/anyOf は最初の候補", () => {
  const spec = specWith({
    Base: { type: "object", required: ["id"], properties: { id: { type: "integer", examples: [9] } }, example: { id: 100 } },
    Extra: { type: "object", required: ["tag"], properties: { tag: { type: "string", enum: ["x", "y"] } } },
    A: { type: "object", properties: { a: { type: "string" } } },
    B: { type: "object", properties: { b: { type: "string" } } },
  });
  const all = { allOf: [{ $ref: "#/components/schemas/Base" }, { $ref: "#/components/schemas/Extra" }, { properties: { own: { type: "boolean" } } }] };
  assert.deepEqual(exampleFromSchema(spec, all), { id: 9, tag: "x", own: true }, "部品のexampleではなく、結合後のプロパティから作る");
  const rows = schemaRows(spec, all);
  assert.deepEqual(rows.map((r) => [r.name, r.required]), [["id", true], ["tag", true], ["own", false]]);

  const one = { oneOf: [{ $ref: "#/components/schemas/A" }, { $ref: "#/components/schemas/B" }] };
  assert.deepEqual(exampleFromSchema(spec, one), { a: "string" });
  assert.deepEqual(exampleFromSchema(spec, { anyOf: [{ $ref: "#/components/schemas/B" }, { $ref: "#/components/schemas/A" }] }), { b: "string" });
  assert.deepEqual(schemaRows(spec, one).map((r) => r.name), ["a"]);
  assert.deepEqual(resolveSchema(spec, one).variants, ["A", "B"]);
});

test("メディアタイプのexample/examplesはスキーマからの生成より優先され、HTMLに出る", () => {
  const spec = specWith({}, {
    paths: {
      "/e": {
        post: {
          operationId: "e",
          requestBody: { required: false, content: { "application/json": { schema: { type: "object", properties: { a: { type: "string" } } }, example: { a: "from-media" } } } },
          responses: { "200": { description: "ok", content: { "text/plain": { schema: { type: "string" }, example: "plain <body>" } } } },
        },
      },
    },
  });
  const html = buildHtml(spec);
  assert.ok(html.includes("&quot;a&quot;: &quot;from-media&quot;"));
  assert.ok(html.includes("リクエストボディ（任意）"));
  assert.ok(html.includes("<pre><code>plain &lt;body&gt;</code></pre>"), "JSON以外の文字列はそのまま（エスケープのみ）");
});

test("--out と --spec: 指定先に出力し、親ディレクトリも作る", () => {
  withTempDir((dir) => {
    const out = path.join(dir, "deep", "nested", "docs.html");
    const specFile = path.join(dir, "spec.json");
    writeFileSync(specFile, JSON.stringify({ ...realSpec, info: { ...realSpec.info, title: "Custom Title" } }));
    const result = runCli(["--spec", specFile, "--out", out]);
    assert.equal(result.status, 0, result.stderr);
    assert.match(result.stdout, /を出力しました（操作 \d+ 件）/);
    const html = readFileSync(out, "utf8");
    assert.match(html, /<title>Custom Title 1\.0\.0<\/title>/);
    assert.ok(html.includes("op-auth-login"));
    const equalsForm = path.join(dir, "eq.html");
    assert.equal(runCli([`--spec=${specFile}`, `--out=${equalsForm}`]).status, 0);
    assert.equal(readFileSync(equalsForm, "utf8"), html, "同じ入力なら同じ出力（決定的）");
  });
});

test("--out を省くと既定の api-docs/index.html を使う（コード上の既定値の確認）", () => {
  assert.equal(DEFAULT_OUT, path.join(path.resolve(path.dirname(scriptPath), ".."), "api-docs", "index.html"));
  assert.deepEqual(parseArgs([]), { spec: DEFAULT_SPEC, out: DEFAULT_OUT });
  assert.equal(parseArgs(["--out", "x/y.html"]).out, path.resolve("x/y.html"));
  assert.throws(() => parseArgs(["--out"]), /値が必要/);
  assert.throws(() => parseArgs(["--out", "--spec", "a"]), /値が必要/);
  assert.throws(() => parseArgs(["--bogus"]), /不明な引数/);
});

test("不正なspecでは、非0で終了し、出力ファイルを作らない", () => {
  withTempDir((dir) => {
    const out = path.join(dir, "out.html");
    const cases = {
      "JSONでない": "{ not json",
      "openapiが無い": JSON.stringify({ info: { title: "t", version: "1" }, paths: {} }),
      "pathsが無い": JSON.stringify({ openapi: "3.1.0", info: { title: "t", version: "1" } }),
      "infoが無い": JSON.stringify({ openapi: "3.1.0", paths: {} }),
      "配列": "[]",
      "解決できない$ref": JSON.stringify({
        openapi: "3.1.0",
        info: { title: "t", version: "1" },
        paths: { "/x": { get: { responses: { 200: { description: "ok", content: { "application/json": { schema: { $ref: "#/components/schemas/Nope" } } } } } } } },
      }),
    };
    for (const [label, body] of Object.entries(cases)) {
      const specFile = path.join(dir, "bad.json");
      writeFileSync(specFile, body);
      const result = runCli(["--spec", specFile, "--out", out]);
      assert.equal(result.status, 1, `${label}: ${result.stdout}`);
      assert.match(result.stderr, /^エラー: /, label);
      assert.equal(existsSync(out), false, `${label}: 出力しない`);
    }
    const missing = runCli(["--spec", path.join(dir, "none.json"), "--out", out]);
    assert.equal(missing.status, 1);
    assert.match(missing.stderr, /仕様ファイルを読めません/);
    const unknown = runCli(["--nope"]);
    assert.equal(unknown.status, 1);
    assert.match(unknown.stderr, /不明な引数/);
  });
});

// ---------- 上限・循環判定・出力先の安全・XSS ----------

// 各スキーマが次のスキーマを fanout 回参照する levels 段の仕様。上限が無いと fanout^levels 件に増える
function explosiveSpec(levels = 8, fanout = 6) {
  const schemas = {};
  for (let level = 0; level < levels; level += 1) {
    const properties = {};
    for (let k = 0; k < fanout; k += 1) {
      properties[`p${k}`] = level === levels - 1 ? { type: "string" } : { $ref: `#/components/schemas/L${level + 1}` };
    }
    schemas[`L${level}`] = { type: "object", properties };
  }
  const content = { "application/json": { schema: { $ref: "#/components/schemas/L0" } } };
  return specWith(schemas, { paths: { "/boom": { get: { operationId: "boom", responses: { 200: { description: "ok", content } } } } } });
}

function countNodes(value) {
  if (value === null || typeof value !== "object") return 1;
  return 1 + Object.values(value).reduce((sum, child) => sum + countNodes(child), 0);
}

test("入れ子が指数的に増える仕様でも、表の行数・JSON例のノード数・処理時間に上限がある", () => {
  const spec = explosiveSpec();
  const root = { $ref: "#/components/schemas/L0" };
  const started = performance.now();

  const rows = schemaRows(spec, root);
  assert.equal(rows.length, MAX_BLOCK_ITEMS + 1, "上限までの行と、省略を知らせる最後の1行");
  assert.match(rows.at(-1).name, /省略/);
  assert.match(rows.at(-1).notes[0], /省略/);

  const example = exampleFromSchema(spec, root);
  assert.ok(countNodes(example) <= MAX_BLOCK_ITEMS + 1, `例のノード数 ${countNodes(example)}`);

  const html = buildHtml(spec);
  const elapsed = performance.now() - started;
  assert.ok(elapsed < 5000, `${Math.round(elapsed)}ms かかった`);
  assert.ok(html.length < 3_000_000, `出力が ${html.length} バイトになった`);
  assert.ok((html.match(/…\(省略\)/g) ?? []).length >= 2, "表と例の両方で省略を知らせる");
});

test("上限に収まる小さな仕様では、省略の表示を出さない", () => {
  assert.doesNotMatch(buildHtml(realSpec), /…\(省略\)/);
  assert.ok(schemaRows(explosiveSpec(2, 3), { $ref: "#/components/schemas/L0" }).every((r) => !r.name.includes("省略")));
});

test("allOf の部品どうしが同じ $ref を使っても循環扱いにならず、本当の循環は打ち切る", () => {
  const spec = specWith({
    Base: { type: "object", required: ["id"], properties: { id: { type: "integer" } } },
    Extra: { type: "object", properties: { base: { $ref: "#/components/schemas/Base" } } },
    Loop: { allOf: [{ $ref: "#/components/schemas/Base" }, { type: "object", properties: { again: { $ref: "#/components/schemas/Loop" } } }] },
  });
  const merged = { allOf: [{ $ref: "#/components/schemas/Base" }, { $ref: "#/components/schemas/Extra" }] };
  const rows = schemaRows(spec, merged);
  assert.deepEqual(rows.map((r) => r.name), ["id", "base", "base.id"]);
  assert.ok(rows.every((r) => r.notes.length === 0), "循環の注記が出ない");
  assert.deepEqual(exampleFromSchema(spec, merged), { id: 0, base: { id: 0 } });

  const loop = schemaRows(spec, { $ref: "#/components/schemas/Loop" });
  assert.deepEqual(loop.map((r) => r.name), ["id", "again"]);
  assert.match(loop[1].notes[0], /循環参照のため Loop/);
  assert.deepEqual(exampleFromSchema(spec, { $ref: "#/components/schemas/Loop" }), { id: 0 });

  const cyclicParts = specWith({
    A: { allOf: [{ $ref: "#/components/schemas/B" }] },
    B: { allOf: [{ $ref: "#/components/schemas/A" }], properties: { x: { type: "string" } } },
  });
  assert.deepEqual(exampleFromSchema(cyclicParts, { $ref: "#/components/schemas/A" }), { x: "string" }, "部品どうしの循環でも止まる");
});

test("oneOf で選んだ候補の中の $ref が、兄弟の候補と誤って循環扱いにならない", () => {
  const spec = specWith({
    Base: { type: "object", properties: { id: { type: "integer" } } },
    Choice: { oneOf: [{ $ref: "#/components/schemas/Wrapper" }, { $ref: "#/components/schemas/Base" }] },
    Wrapper: { type: "object", properties: { inner: { $ref: "#/components/schemas/Base" } } },
  });
  const choice = { $ref: "#/components/schemas/Choice" };
  assert.deepEqual(schemaRows(spec, choice).map((r) => r.name), ["inner", "inner.id"]);
  assert.deepEqual(exampleFromSchema(spec, choice), { inner: { id: 0 } });
});

test("format に Object.prototype の名前を入れても、安全に string の例になる", () => {
  const spec = specWith({});
  for (const format of ["constructor", "__proto__", "toString", "hasOwnProperty", "valueOf"]) {
    assert.equal(exampleFromSchema(spec, { type: "string", format }), "string", format);
  }
});

test("プロパティ名が __proto__ でも、例のキーとして残り、プロトタイプは書き換わらない", () => {
  const properties = JSON.parse('{"__proto__": {"type": "string"}, "ok": {"type": "integer"}}');
  const example = exampleFromSchema(specWith({}), { type: "object", properties });
  assert.deepEqual(Object.keys(example), ["__proto__", "ok"]);
  assert.equal(Object.getPrototypeOf(example), Object.prototype);
});

test("--out が --spec と同じ実ファイルなら、拒否して仕様を壊さない", (t) => {
  withTempDir((dir) => {
    const specFile = path.join(dir, "spec.html");
    const body = JSON.stringify(realSpec);
    writeFileSync(specFile, body);

    const same = runCli(["--spec", specFile, "--out", specFile]);
    assert.equal(same.status, 1);
    assert.match(same.stderr, /^エラー: .*同じファイル/);

    mkdirSync(path.join(dir, "sub"));
    const viaDots = runCli(["--spec", specFile, "--out", path.join(dir, "sub", "..", "spec.html")]);
    assert.equal(viaDots.status, 1, "別の書き方でも同じファイルと判定する");
    assert.match(viaDots.stderr, /同じファイル/);

    const link = path.join(dir, "link.html");
    if (trySymlink(t, specFile, link)) {
      const viaLink = runCli(["--spec", specFile, "--out", link]);
      assert.equal(viaLink.status, 1, "シンボリックリンク経由でも同じ実ファイルと判定する");
      assert.match(viaLink.stderr, /同じファイル/);
    }
    assert.equal(readFileSync(specFile, "utf8"), body, "仕様ファイルは変わらない");
  });
});

test("--out は .html だけ。別の拡張子や拡張子なしは拒否する", () => {
  withTempDir((dir) => {
    for (const name of ["out.json", "out.txt", "out", "out.html.bak"]) {
      const out = path.join(dir, name);
      const result = runCli(["--out", out]);
      assert.equal(result.status, 1, name);
      assert.match(result.stderr, /\.html/, name);
      assert.equal(existsSync(out), false, `${name}: 作らない`);
    }
    assert.equal(runCli(["--out", path.join(dir, "OUT.HTML")]).status, 0, "大文字の拡張子は許す");
  });
});

test("--out の既存ファイルは、生成物の目印があるときだけ上書きする", () => {
  withTempDir((dir) => {
    const out = path.join(dir, "docs.html");
    assert.equal(runCli(["--out", out]).status, 0);
    assert.ok(readFileSync(out, "utf8").includes(GENERATOR_MARKER), "生成物に目印が入る");
    assert.equal(runCli(["--out", out]).status, 0, "自分が作ったファイルは上書きできる");

    const precious = path.join(dir, "precious.html");
    const content = "<!doctype html><p>大事な手書きのページ</p>";
    writeFileSync(precious, content);
    const refused = runCli(["--out", precious]);
    assert.equal(refused.status, 1);
    assert.match(refused.stderr, /このスクリプトが作ったものではない/);
    assert.equal(readFileSync(precious, "utf8"), content, "既存ファイルは変わらない");

    const empty = path.join(dir, "empty.html");
    writeFileSync(empty, "");
    assert.equal(runCli(["--out", empty]).status, 1, "空ファイルも目印が無いので拒否");

    const directory = path.join(dir, "dir.html");
    mkdirSync(directory);
    const dirResult = runCli(["--out", directory]);
    assert.equal(dirResult.status, 1);
    assert.match(dirResult.stderr, /通常のファイルではありません/);
  });
});

test("相対パスは、実行時のカレントディレクトリを起点に解決される。--help にもそう書いてある", () => {
  withTempDir((dir) => {
    const result = runCli(["--out", "rel/docs.html"], { cwd: dir });
    assert.equal(result.status, 0, result.stderr);
    assert.ok(existsSync(path.join(dir, "rel", "docs.html")), "スクリプトの場所ではなく cwd に出る");
    assert.match(result.stdout, /rel[\\/]docs\.html を出力しました/);

    const help = runCli(["--help"]);
    assert.equal(help.status, 0);
    assert.match(help.stdout, /カレントディレクトリを起点/);
    assert.match(help.stdout, /--out/);
    assert.equal(runCli(["-h"]).status, 0);
  });
});

test("シンボリックリンク経由で起動しても、出力される", (t) => {
  withTempDir((dir) => {
    const link = path.join(dir, "link-to-script.mjs");
    if (!trySymlink(t, scriptPath, link)) return;
    const out = path.join(dir, "via-link.html");
    const result = spawnSync(process.execPath, [link, "--out", out], { encoding: "utf8" });
    assert.equal(result.status, 0, result.stderr);
    assert.ok(existsSync(out), "main が実行された");
  });
});

test("あらゆる場所に <x-evil> を入れても、出力にタグとして現れない", () => {
  const evil = "<x-evil>";
  const ref = (name) => ({ $ref: `#/components/schemas/${name}` });
  const spec = {
    openapi: "3.1.0",
    info: { title: "T", version: "1" },
    servers: [{ url: `https://${evil}.example/` }],
    tags: [{ name: evil, description: evil }],
    security: [{ [evil]: [] }],
    paths: {
      [`/${evil}`]: {
        post: {
          operationId: evil,
          tags: [evil],
          parameters: [{ in: evil, name: evil, schema: { type: "string", pattern: evil, format: evil, enum: [evil], default: evil } }],
          requestBody: { content: { [`${evil}/json`]: { schema: ref(evil) } } },
          responses: {
            [evil]: {
              description: evil,
              headers: { [evil]: { description: evil, schema: { type: "string", pattern: evil, format: evil } } },
              content: { [evil]: { schema: ref(`${evil}Choice`) } },
            },
            200: { description: "ok", content: { "application/json": { schema: ref(evil) } } },
          },
        },
      },
    },
    components: {
      securitySchemes: { [evil]: { type: "apiKey", in: evil, name: evil, description: evil } },
      schemas: {
        [evil]: {
          type: "object",
          properties: {
            self: ref(evil),
            list: { type: "array", items: ref(evil) },
            pat: { type: "string", pattern: evil, enum: [evil], format: evil, default: evil },
            [evil]: { type: "string" },
          },
        },
        [`${evil}A`]: { type: "object", properties: { a: { type: "string" } } },
        [`${evil}B`]: { type: "object", properties: { b: { type: "string" } } },
        [`${evil}Choice`]: { oneOf: [ref(`${evil}A`), ref(`${evil}B`)] },
      },
    },
  };
  const html = buildHtml(spec);
  assert.doesNotMatch(html, /<x-evil/i);
  for (const rendered of [
    "https://&lt;x-evil&gt;.example/",
    "pattern: &lt;x-evil&gt;",
    "Content-Type: <code>&lt;x-evil&gt;/json</code>",
    "Content-Type: <code>&lt;x-evil&gt;</code>",
    '<span class="status s&lt;">&lt;x-evil&gt;</span>',
    "<code>&lt;x-evil&gt;</code>",
    "候補 &lt;x-evil&gt;A / &lt;x-evil&gt;B",
    "循環参照のため &lt;x-evil&gt; の中身",
    "string (&lt;x-evil&gt;)",
  ]) {
    assert.ok(html.includes(rendered), `${rendered} がエスケープされて出ている`);
  }
});
