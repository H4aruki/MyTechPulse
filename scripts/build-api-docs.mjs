// server/openapi/openapi.json から、サーバーを起動せずに開ける読み取り専用のAPI仕様書（HTML 1ファイル）を作る。
// Node標準APIだけで動く。外部ファイル・CDN・外部フォント・スクリプト・fetch は一切使わない。
// 使い方: node scripts/build-api-docs.mjs [--spec <openapi.json>] [--out <出力先.html>] [--help]
// --spec / --out の相対パスは、このファイルの場所ではなく、実行したときのカレントディレクトリを起点に解決する。
import { closeSync, mkdirSync, openSync, readFileSync, readSync, realpathSync, statSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
export const DEFAULT_SPEC = path.join(REPO_ROOT, "server", "openapi", "openapi.json");
export const DEFAULT_OUT = path.join(REPO_ROOT, "api-docs", "index.html");

// 入れ子をたどる深さの上限。これを超えた下は「省略」と表示する（循環参照はこれより前に打ち切る）
export const MAX_DEPTH = 8;
// 1ブロック（スキーマの表、または1つのJSON例）に出す行数・ノード数の上限。入れ子が指数的に増える仕様でも出力を有界にする
export const MAX_BLOCK_ITEMS = 2000;
const OMITTED = "…(省略)";
// 生成物の目印。既存ファイルを上書きしてよいかの判定に使う（<head> の先頭付近に置く）
export const GENERATOR_MARKER = '<meta name="generator" content="build-api-docs">';

const METHODS = ["get", "put", "post", "delete", "options", "head", "patch", "trace"];
const OTHER_TAG = "その他";

const isObject = (value) => value !== null && typeof value === "object" && !Array.isArray(value);

// ---------- HTMLエスケープ ----------

const ESCAPES = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };

export function escapeHtml(value) {
  return String(value ?? "").replace(/[&<>"']/g, (c) => ESCAPES[c]);
}

// 説明文用。先にエスケープしてから `code` だけをcodeタグにする（エスケープ後の文字列に < は残らない）
function text(value) {
  return escapeHtml(value).replace(/`([^`\n]+)`/g, "<code>$1</code>");
}

// ---------- 仕様の読み込みと検証 ----------

export function validateSpec(spec) {
  if (!isObject(spec)) throw new Error("仕様がJSONのオブジェクトではありません");
  if (typeof spec.openapi !== "string" || !spec.openapi.startsWith("3.")) {
    throw new Error("`openapi` が 3.x ではありません");
  }
  if (!isObject(spec.info) || typeof spec.info.title !== "string" || typeof spec.info.version !== "string") {
    throw new Error("`info.title` と `info.version` が必要です");
  }
  if (!isObject(spec.paths)) throw new Error("`paths` がありません");
  return spec;
}

export function loadSpec(file) {
  let raw;
  try {
    raw = readFileSync(file, "utf8");
  } catch (error) {
    throw new Error(`仕様ファイルを読めません: ${file} (${error.code ?? error.message})`);
  }
  let spec;
  try {
    spec = JSON.parse(raw);
  } catch (error) {
    throw new Error(`仕様ファイルがJSONとして不正です: ${file} (${error.message})`);
  }
  return validateSpec(spec);
}

// ---------- $ref と スキーマの解決 ----------

const refName = (ref) => ref.slice(ref.lastIndexOf("/") + 1);

function pointerGet(spec, ref) {
  if (!ref.startsWith("#/")) throw new Error(`外部参照は使えません: ${ref}`);
  let node = spec;
  for (const raw of ref.slice(2).split("/")) {
    const key = decodeURIComponent(raw).replace(/~1/g, "/").replace(/~0/g, "~");
    if (!isObject(node) || !Object.hasOwn(node, key)) throw new Error(`$ref を解決できません: ${ref}`);
    node = node[key];
  }
  return node;
}

// $ref だけをたどる（parameters・responses・requestBody など、スキーマ以外にも使う）
function deref(spec, node) {
  const seen = new Set();
  let current = node;
  while (isObject(current) && typeof current.$ref === "string") {
    if (seen.has(current.$ref)) throw new Error(`$ref が循環しています: ${current.$ref}`);
    seen.add(current.$ref);
    current = pointerGet(spec, current.$ref);
  }
  return isObject(current) ? current : {};
}

function mergeInto(target, source) {
  const { properties, required, ...rest } = source;
  Object.assign(target, rest);
  if (properties) target.properties = { ...target.properties, ...properties };
  if (Array.isArray(required)) target.required = [...new Set([...(target.required ?? []), ...required])];
  return target;
}

const EXAMPLE_KEYS = ["example", "examples", "default"];
const withoutKeys = (object, keys) => Object.fromEntries(Object.entries(object).filter(([k]) => !keys.includes(k)));

// $ref・allOf（結合）・oneOf/anyOf（最初の候補）をたどって、1つのスキーマにまとめる。
// stack は「いま展開中の $ref」の一覧（根からの経路）。すでに入っている $ref に再び来たら循環として打ち切る。
// allOf・oneOf の部品は経路の枝として解決するだけで、stack には積まない（兄弟の部品どうしを循環と誤判定しないため）。
export function resolveSchema(spec, schema, stack = []) {
  let current = schema;
  let name = null;
  let refs = stack;
  while (isObject(current) && typeof current.$ref === "string") {
    const ref = current.$ref;
    if (refs.includes(ref)) return { schema: {}, name: refName(ref), stack: refs, circular: true };
    refs = [...refs, ref];
    name = refName(ref);
    current = pointerGet(spec, ref);
  }
  if (!isObject(current)) return { schema: {}, name, stack: refs, circular: false };

  let result = current;
  let variants;
  if (Array.isArray(current.allOf) && current.allOf.length > 0) {
    result = {};
    for (const part of current.allOf) {
      const resolved = resolveSchema(spec, part, refs);
      // 部品ごとの例は一部分しか持たないので、結合後は各プロパティの例から作り直す
      mergeInto(result, withoutKeys(resolved.schema, EXAMPLE_KEYS));
    }
    mergeInto(result, withoutKeys(current, ["allOf"]));
  }
  const options = result.oneOf ?? result.anyOf;
  if (Array.isArray(options) && options.length > 0) {
    const first = resolveSchema(spec, options[0], refs);
    if (first.circular) return { ...first, stack: refs };
    variants = options.map((o) => (isObject(o) && typeof o.$ref === "string" ? refName(o.$ref) : (o?.type ?? "object")));
    result = mergeInto(mergeInto({}, first.schema), withoutKeys(result, ["oneOf", "anyOf"]));
  }
  return { schema: result, name, stack: refs, circular: false, variants };
}

// ---------- JSON例の生成 ----------

const FORMAT_EXAMPLES = {
  "date-time": "2026-01-01T00:00:00Z",
  date: "2026-01-01",
  time: "00:00:00Z",
  email: "user@example.com",
  uuid: "00000000-0000-0000-0000-000000000000",
  uri: "https://example.com/",
  url: "https://example.com/",
  hostname: "example.com",
  ipv4: "192.0.2.1",
  ipv6: "2001:db8::1",
  byte: "c3RyaW5n",
};

function declaredExample(schema) {
  if (schema.example !== undefined) return schema.example;
  const { examples } = schema;
  if (Array.isArray(examples) && examples.length > 0) return examples[0];
  if (isObject(examples)) {
    const first = Object.values(examples)[0];
    if (isObject(first) && "value" in first) return first.value;
    if (first !== undefined) return first;
  }
  return schema.default;
}

function pickType(schema) {
  const { type } = schema;
  if (Array.isArray(type)) return type.find((t) => t !== "null") ?? "null";
  if (type) return type;
  if (schema.properties || schema.additionalProperties) return "object";
  if (schema.items) return "array";
  return undefined;
}

const createBudget = () => ({ left: MAX_BLOCK_ITEMS, truncated: false });

// JSONのキーが __proto__ でも、プロトタイプを書き換えずに普通のプロパティとして入れる
function setOwn(object, key, value) {
  Object.defineProperty(object, key, { value, enumerable: true, writable: true, configurable: true });
}

// 例を作れない（循環・深さ超過・上限超過・型が分からない）ときは undefined を返す。
// budget は1つの例の中で作るノード数の残り。尽きたら budget.truncated を立てて打ち切る。
export function exampleFromSchema(spec, schema, budget = createBudget(), stack = [], depth = 0) {
  if (depth > MAX_DEPTH) return undefined;
  if (budget.left <= 0) {
    budget.truncated = true;
    return undefined;
  }
  budget.left -= 1;
  const resolved = resolveSchema(spec, schema, stack);
  if (resolved.circular) return undefined;
  const s = resolved.schema;
  const declared = declaredExample(s);
  if (declared !== undefined) return declared;
  if (Array.isArray(s.enum) && s.enum.length > 0) return s.enum[0];
  if ("const" in s) return s.const;

  switch (pickType(s)) {
    case "string":
      return Object.hasOwn(FORMAT_EXAMPLES, s.format) ? FORMAT_EXAMPLES[s.format] : "string";
    case "integer":
    case "number":
      return typeof s.minimum === "number" && s.minimum > 0 ? s.minimum : 0;
    case "boolean":
      return true;
    case "null":
      return null;
    case "array": {
      const item = s.items === undefined ? undefined : exampleFromSchema(spec, s.items, budget, resolved.stack, depth + 1);
      return item === undefined ? [] : [item];
    }
    case "object": {
      const out = {};
      const properties = s.properties ?? {};
      for (const [key, value] of Object.entries(properties)) {
        const example = exampleFromSchema(spec, value, budget, resolved.stack, depth + 1);
        if (example !== undefined) setOwn(out, key, example);
      }
      if (Object.keys(properties).length === 0 && isObject(s.additionalProperties)) {
        const example = exampleFromSchema(spec, s.additionalProperties, budget, resolved.stack, depth + 1);
        if (example !== undefined) out.key = example;
      }
      return out;
    }
    default:
      return undefined;
  }
}

// 戻り値は { value, truncated }。truncated は、上限に達して例の一部を省いたことを表す
function mediaExample(spec, media) {
  if (media.example !== undefined) return { value: media.example, truncated: false };
  if (isObject(media.examples)) {
    const first = Object.values(media.examples)[0];
    const resolved = deref(spec, first);
    if (resolved.value !== undefined) return { value: resolved.value, truncated: false };
  }
  if (media.schema === undefined) return { value: undefined, truncated: false };
  const budget = createBudget();
  const value = exampleFromSchema(spec, media.schema, budget);
  return { value, truncated: budget.truncated };
}

// ---------- スキーマの表 ----------

function typeLabel(spec, resolved) {
  const s = resolved.schema;
  let types = [];
  if (Array.isArray(s.type)) types = s.type;
  else if (s.type) types = [s.type];
  else if (s.properties || s.additionalProperties) types = ["object"];
  else if (s.items) types = ["array"];
  if (types.length === 0) return resolved.name ?? "any";
  return types
    .map((t) => {
      if (t === "array") {
        const items = resolveSchema(spec, s.items ?? {}, resolved.stack);
        return `${items.circular ? items.name : typeLabel(spec, items)}[]`;
      }
      if (t === "object") return resolved.name ?? "object";
      return s.format ? `${t} (${s.format})` : t;
    })
    .join(" | ");
}

function constraintsOf(s) {
  const out = [];
  const show = (value) => JSON.stringify(value);
  if (Array.isArray(s.enum)) out.push(`enum: ${s.enum.map(show).join(", ")}`);
  if ("const" in s) out.push(`const: ${show(s.const)}`);
  if (s.default !== undefined) out.push(`既定: ${show(s.default)}`);
  for (const key of ["minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems", "pattern"]) {
    if (s[key] !== undefined) out.push(`${key}: ${s[key]}`);
  }
  return out;
}

// 行を足す。上限に達したら「…(省略)」の行を1つだけ足して、以後は受け付けない（false を返す）
function emit(ctx, row) {
  if (ctx.full) return false;
  if (ctx.rows.length >= MAX_BLOCK_ITEMS) {
    ctx.full = true;
    ctx.rows.push({ name: OMITTED, type: "", required: false, description: "", constraints: [], notes: [`${OMITTED} 行数の上限（${MAX_BLOCK_ITEMS}行）に達したため、これより下は表示しません`] });
    return false;
  }
  ctx.rows.push(row);
  return true;
}

function addRow(ctx, name, schema, required, stack, depth) {
  const { spec } = ctx;
  const resolved = resolveSchema(spec, schema, stack);
  const notes = [];
  if (resolved.circular) notes.push(`循環参照のため ${resolved.name} の中身は展開しません`);
  if (resolved.variants) notes.push(`候補 ${resolved.variants.join(" / ")} のうち最初を表示`);
  const added = emit(ctx, {
    name,
    type: resolved.circular ? resolved.name : typeLabel(spec, resolved),
    required,
    description: resolved.schema.description ?? "",
    constraints: constraintsOf(resolved.schema),
    notes,
  });
  if (!added || resolved.circular) return;
  expandChildren(ctx, name, resolved, depth + 1);
}

function expandChildren(ctx, prefix, resolved, depth) {
  if (ctx.full) return;
  const s = resolved.schema;
  const hasChildren = s.properties || isObject(s.additionalProperties) || s.items;
  if (!hasChildren) return;
  if (depth > MAX_DEPTH) {
    emit(ctx, { name: `${prefix}…`, type: "", required: false, description: "", constraints: [], notes: [`深さ制限（${MAX_DEPTH}階層）のため、これより下は省略します`] });
    return;
  }
  const required = new Set(Array.isArray(s.required) ? s.required : []);
  const join = (key) => (prefix ? `${prefix}.${key}` : key);
  for (const [key, value] of Object.entries(s.properties ?? {})) {
    if (ctx.full) return;
    addRow(ctx, join(key), value, required.has(key), resolved.stack, depth);
  }
  if (isObject(s.additionalProperties) && !ctx.full) {
    addRow(ctx, join("*"), s.additionalProperties, false, resolved.stack, depth);
  }
  if (s.items && !ctx.full) {
    const items = resolveSchema(ctx.spec, s.items, resolved.stack);
    if (items.circular) {
      emit(ctx, { name: `${prefix}[]`, type: items.name, required: false, description: "", constraints: [], notes: [`循環参照のため ${items.name} の中身は展開しません`] });
    } else {
      expandChildren(ctx, `${prefix}[]`, items, depth + 1);
    }
  }
}

export function schemaRows(spec, schema) {
  const ctx = { spec, rows: [], full: false };
  expandChildren(ctx, "", resolveSchema(spec, schema), 0);
  return ctx.rows;
}

// ---------- HTMLの部品 ----------

const requiredMark = (required) => (required ? "必須" : "任意");

function renderTable(headers, bodyRows) {
  const head = headers.map((h) => `<th>${escapeHtml(h)}</th>`).join("");
  return `<div class="table-wrap"><table><thead><tr>${head}</tr></thead><tbody>${bodyRows.join("")}</tbody></table></div>`;
}

function renderSchemaBlock(spec, schema) {
  const resolved = resolveSchema(spec, schema);
  const rows = schemaRows(spec, schema);
  let html = `<p class="type">型: <code>${escapeHtml(typeLabel(spec, resolved))}</code></p>`;
  if (resolved.schema.description) html += `<p class="desc">${text(resolved.schema.description)}</p>`;
  if (resolved.variants) {
    html += `<p class="note">候補 ${escapeHtml(resolved.variants.join(" / "))} のうち最初を表示しています</p>`;
  }
  if (rows.length > 0) {
    const body = rows.map(
      (row) =>
        `<tr><td><code>${escapeHtml(row.name)}</code></td><td>${escapeHtml(row.type)}</td><td>${requiredMark(row.required)}</td>` +
        `<td class="desc">${text(row.description)}${row.notes.map((n) => `<div class="note">${escapeHtml(n)}</div>`).join("")}</td>` +
        `<td>${row.constraints.map((c) => `<div>${escapeHtml(c)}</div>`).join("")}</td></tr>`,
    );
    html += renderTable(["項目", "型", "必須", "説明", "制約"], body);
  }
  return html;
}

function renderExample({ value, truncated }, contentType) {
  if (value === undefined) return `<p class="note">例を生成できませんでした</p>`;
  const isJson = /json/i.test(contentType);
  const body = typeof value === "string" && !isJson ? value : JSON.stringify(value, null, 2);
  const omitted = truncated ? `<p class="note">${OMITTED} 上限（${MAX_BLOCK_ITEMS}項目）に達したため、例の一部を省いています</p>` : "";
  return `<p class="label">例</p><pre><code>${escapeHtml(body)}</code></pre>${omitted}`;
}

function renderContent(spec, content) {
  if (!isObject(content) || Object.keys(content).length === 0) return `<p class="note">本文なし</p>`;
  return Object.entries(content)
    .map(([contentType, media]) => {
      const m = isObject(media) ? media : {};
      const schemaHtml = m.schema === undefined ? "" : renderSchemaBlock(spec, m.schema);
      return `<div class="media"><p class="label">Content-Type: <code>${escapeHtml(contentType)}</code></p>${schemaHtml}${renderExample(mediaExample(spec, m), contentType)}</div>`;
    })
    .join("");
}

function renderParameters(spec, pathItem, operation) {
  const merged = new Map();
  for (const raw of [...(pathItem.parameters ?? []), ...(operation.parameters ?? [])]) {
    const p = deref(spec, raw);
    merged.set(`${p.in}:${p.name}`, p);
  }
  if (merged.size === 0) return "";
  const body = [...merged.values()].map((p) => {
    const schema = resolveSchema(spec, p.schema ?? {});
    const description = p.description ?? schema.schema.description ?? "";
    const constraints = constraintsOf(schema.schema);
    return (
      `<tr><td>${escapeHtml(p.in)}</td><td><code>${escapeHtml(p.name)}</code></td><td>${requiredMark(p.required === true || p.in === "path")}</td>` +
      `<td>${escapeHtml(typeLabel(spec, schema))}</td><td class="desc">${text(description)}${constraints.map((c) => `<div class="note">${escapeHtml(c)}</div>`).join("")}</td></tr>`
    );
  });
  return `<h4>パラメーター</h4>${renderTable(["場所", "名前", "必須", "型", "説明"], body)}`;
}

function renderRequestBody(spec, operation) {
  if (operation.requestBody === undefined) return "";
  const body = deref(spec, operation.requestBody);
  const note = body.required ? "必須" : "任意";
  const description = body.description ? `<p class="desc">${text(body.description)}</p>` : "";
  return `<h4>リクエストボディ（${note}）</h4>${description}${renderContent(spec, body.content)}`;
}

function statusOrder(code) {
  const n = Number(code);
  return Number.isNaN(n) ? Number.MAX_SAFE_INTEGER : n;
}

function renderResponses(spec, operation) {
  const entries = Object.entries(operation.responses ?? {}).sort(([a], [b]) => statusOrder(a) - statusOrder(b));
  if (entries.length === 0) return "";
  const items = entries.map(([code, raw]) => {
    const response = deref(spec, raw);
    const open = /^2/.test(code) ? " open" : "";
    const headers = Object.entries(response.headers ?? {});
    let headerHtml = "";
    if (headers.length > 0) {
      const body = headers.map(([name, rawHeader]) => {
        const header = deref(spec, rawHeader);
        const schema = resolveSchema(spec, header.schema ?? {});
        return (
          `<tr><td><code>${escapeHtml(name)}</code></td><td>${requiredMark(header.required === true)}</td>` +
          `<td>${escapeHtml(typeLabel(spec, schema))}</td><td class="desc">${text(header.description ?? schema.schema.description ?? "")}</td></tr>`
        );
      });
      headerHtml = `<p class="label">ヘッダー</p>${renderTable(["名前", "必須", "型", "説明"], body)}`;
    }
    return (
      `<details class="response"${open}><summary><span class="status s${escapeHtml(String(code)[0])}">${escapeHtml(code)}</span> ${escapeHtml(response.description ?? "")}</summary>` +
      `${headerHtml}${renderContent(spec, response.content)}</details>`
    );
  });
  return `<h4>レスポンス</h4>${items.join("")}`;
}

function renderSecurity(spec, operation) {
  const requirements = operation.security ?? spec.security;
  if (!Array.isArray(requirements) || requirements.length === 0 || requirements.every((r) => Object.keys(r).length === 0)) {
    return `<p class="auth">認証: 不要</p>`;
  }
  const schemes = spec.components?.securitySchemes ?? {};
  const parts = requirements.map((requirement) =>
    Object.keys(requirement)
      .map((name) => {
        const scheme = Object.hasOwn(schemes, name) && isObject(schemes[name]) ? deref(spec, schemes[name]) : {};
        const where = [scheme.type, scheme.in, scheme.name].filter(Boolean).join(" / ");
        const detail = [where, scheme.description].filter(Boolean).join("。");
        return `<code>${escapeHtml(name)}</code>${detail ? `（${text(detail)}）` : ""}`;
      })
      .join(" と "),
  );
  return `<p class="auth">認証: 必要 — ${parts.join(" または ")}</p>`;
}

// ---------- ページ全体 ----------

const STYLE = `
:root { color-scheme: light; }
* { box-sizing: border-box; }
body { margin: 0; font-family: "Hiragino Sans", "Noto Sans JP", "Yu Gothic", Meiryo, sans-serif; font-size: 15px; line-height: 1.7; color: #1f2933; background: #ffffff; }
code, pre { font-family: ui-monospace, "SFMono-Regular", Menlo, Consolas, monospace; font-size: 0.9em; }
code { background: #eef1f4; padding: 0.1em 0.35em; border-radius: 3px; }
.layout { display: flex; align-items: flex-start; }
nav { position: sticky; top: 0; flex: 0 0 22rem; max-height: 100vh; overflow-y: auto; padding: 1rem; border-right: 1px solid #d8dee4; background: #f6f8fa; }
nav h2 { margin: 0 0 0.5rem; font-size: 1rem; }
nav ul { list-style: none; margin: 0; padding: 0; }
nav li { margin: 0.2rem 0; }
nav a { color: #1f2933; text-decoration: none; word-break: break-all; }
nav a:hover { text-decoration: underline; }
nav summary { cursor: pointer; font-weight: bold; }
main { flex: 1 1 auto; min-width: 0; max-width: 62rem; padding: 1rem 2rem 4rem; }
h1 { margin: 0.5rem 0 0; font-size: 1.7rem; }
h2.tag { margin: 2.5rem 0 0.5rem; padding-bottom: 0.3rem; border-bottom: 2px solid #1f2933; font-size: 1.3rem; }
h4 { margin: 1.2rem 0 0.4rem; font-size: 1rem; }
.version { color: #52606d; }
.desc, .info-desc { white-space: pre-line; }
.operation { margin: 1.5rem 0; padding: 1rem 1.2rem; border: 1px solid #d8dee4; border-radius: 6px; scroll-margin-top: 0.5rem; }
.operation h3 { margin: 0 0 0.3rem; font-size: 1.05rem; word-break: break-all; }
.method { display: inline-block; min-width: 4.5rem; margin-right: 0.5rem; padding: 0.05rem 0.5rem; border-radius: 3px; color: #ffffff; text-align: center; font-size: 0.85rem; font-weight: bold; }
.m-get { background: #1b7f3b; } .m-post { background: #0b66c3; } .m-put { background: #a85d00; }
.m-patch { background: #7a3fb5; } .m-delete { background: #c0262d; } .m-other { background: #52606d; }
.summary { font-weight: bold; }
.auth, .note, .type, .label { margin: 0.4rem 0; }
.note { color: #52606d; font-size: 0.9em; }
.label { font-weight: bold; }
.table-wrap { overflow-x: auto; }
table { border-collapse: collapse; width: 100%; margin: 0.4rem 0 0.8rem; }
th, td { padding: 0.3rem 0.6rem; border: 1px solid #d8dee4; text-align: left; vertical-align: top; }
th { background: #f0f3f6; white-space: nowrap; }
pre { margin: 0.3rem 0 0.8rem; padding: 0.8rem; overflow-x: auto; border: 1px solid #d8dee4; border-radius: 4px; background: #f6f8fa; }
pre code { background: none; padding: 0; }
details.response { margin: 0.5rem 0; padding: 0.3rem 0.8rem; border: 1px solid #d8dee4; border-radius: 4px; }
details.response > summary { cursor: pointer; font-weight: bold; }
.status { display: inline-block; min-width: 2.8rem; padding: 0 0.4rem; border-radius: 3px; background: #52606d; color: #ffffff; text-align: center; }
.s2 { background: #1b7f3b; } .s4 { background: #a85d00; } .s5 { background: #c0262d; }
footer { margin-top: 3rem; color: #52606d; font-size: 0.85em; }
@media (max-width: 60rem) { .layout { display: block; } nav { position: static; max-height: none; border-right: 0; border-bottom: 1px solid #d8dee4; } main { padding: 1rem; } }
`;

const CSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'";

function collectOperations(spec) {
  const operations = [];
  const usedIds = new Set();
  for (const [route, rawItem] of Object.entries(spec.paths)) {
    const pathItem = deref(spec, rawItem);
    for (const method of METHODS) {
      if (!isObject(pathItem[method])) continue;
      const operation = pathItem[method];
      const base = `op-${String(operation.operationId ?? `${method}${route}`).replace(/[^A-Za-z0-9_-]+/g, "-")}`;
      let id = base;
      for (let n = 2; usedIds.has(id); n += 1) id = `${base}-${n}`;
      usedIds.add(id);
      const tags = Array.isArray(operation.tags) && operation.tags.length > 0 ? operation.tags.map(String) : [OTHER_TAG];
      operations.push({ id, method, route, pathItem, operation, tags });
    }
  }
  return operations;
}

function orderedTags(spec, operations) {
  const declared = Array.isArray(spec.tags) ? spec.tags.filter((t) => isObject(t) && typeof t.name === "string") : [];
  const names = declared.map((t) => t.name);
  for (const op of operations) for (const tag of op.tags) if (!names.includes(tag)) names.push(tag);
  return names.filter((name) => operations.some((op) => op.tags.includes(name))).map((name) => ({ name, description: declared.find((t) => t.name === name)?.description }));
}

const methodClass = (method) => (["get", "post", "put", "patch", "delete"].includes(method) ? `m-${method}` : "m-other");

function renderOperation(spec, op) {
  const { operation, pathItem } = op;
  const heading =
    `<h3><span class="method ${methodClass(op.method)}">${escapeHtml(op.method.toUpperCase())}</span><code>${escapeHtml(op.route)}</code></h3>` +
    (operation.deprecated ? `<p class="note">非推奨</p>` : "");
  return (
    `<article class="operation" id="${escapeHtml(op.id)}">${heading}` +
    (operation.summary ? `<p class="summary">${text(operation.summary)}</p>` : "") +
    (operation.operationId ? `<p class="note">operationId: <code>${escapeHtml(operation.operationId)}</code></p>` : "") +
    (operation.description ? `<p class="desc">${text(operation.description)}</p>` : "") +
    renderSecurity(spec, operation) +
    renderParameters(spec, pathItem, operation) +
    renderRequestBody(spec, operation) +
    renderResponses(spec, operation) +
    `</article>`
  );
}

export function buildHtml(spec) {
  validateSpec(spec);
  const operations = collectOperations(spec);
  const tags = orderedTags(spec, operations);

  const nav = tags
    .map(({ name }) => {
      const links = operations
        .filter((op) => op.tags.includes(name))
        .map(
          (op) =>
            `<li><a href="#${escapeHtml(op.id)}"><strong>${escapeHtml(op.method.toUpperCase())}</strong> ${escapeHtml(op.route)}</a>` +
            (op.operation.summary ? `<br><small>${escapeHtml(op.operation.summary)}</small>` : "") +
            `</li>`,
        )
        .join("");
      return `<details open><summary>${escapeHtml(name)}</summary><ul>${links}</ul></details>`;
    })
    .join("");

  const sections = tags
    .map(({ name, description }) => {
      const own = operations.filter((op) => op.tags[0] === name);
      if (own.length === 0) return "";
      return (
        `<section><h2 class="tag">${escapeHtml(name)}</h2>` +
        (description ? `<p class="desc">${text(description)}</p>` : "") +
        own.map((op) => renderOperation(spec, op)).join("") +
        `</section>`
      );
    })
    .join("");

  const servers = Array.isArray(spec.servers)
    ? spec.servers.filter((s) => isObject(s) && typeof s.url === "string").map((s) => `<code>${escapeHtml(s.url)}</code>`)
    : [];

  return (
    `<!DOCTYPE html>\n<html lang="ja"><head><meta charset="utf-8">${GENERATOR_MARKER}` +
    `<meta http-equiv="Content-Security-Policy" content="${escapeHtml(CSP)}">` +
    `<meta name="viewport" content="width=device-width, initial-scale=1"><meta name="color-scheme" content="light">` +
    `<title>${escapeHtml(spec.info.title)} ${escapeHtml(spec.info.version)}</title><style>${STYLE}</style></head>\n` +
    `<body><div class="layout"><nav aria-label="目次"><h2>目次</h2>${nav}</nav><main>` +
    `<header><h1>${escapeHtml(spec.info.title)}</h1>` +
    `<p class="version">バージョン ${escapeHtml(spec.info.version)}（OpenAPI ${escapeHtml(spec.openapi)}）</p>` +
    (spec.info.description ? `<p class="info-desc">${text(spec.info.description)}</p>` : "") +
    (servers.length > 0 ? `<p class="note">サーバー: ${servers.join("、")}</p>` : "") +
    `<p class="note">操作は ${operations.length} 件です。この文書は読み取り専用で、実際のリクエストは送れません。</p></header>` +
    `${sections}<footer>このページは <code>server/openapi/openapi.json</code> から <code>scripts/build-api-docs.mjs</code> が生成した静的ファイルです。直接編集しないでください。</footer>` +
    `</main></div></body></html>\n`
  );
}

// ---------- コマンドライン ----------

export const USAGE = `使い方: node scripts/build-api-docs.mjs [--spec <openapi.json>] [--out <出力先.html>] [--help]

  --spec  読み込むOpenAPI仕様（既定: server/openapi/openapi.json）
  --out   書き出すHTML（既定: api-docs/index.html）。拡張子は .html のみ
  --help  この説明を表示する

相対パスは、スクリプトの場所ではなく、実行したときのカレントディレクトリを起点に解決します。
--out が --spec と同じファイルのとき、またこのスクリプトが作ったものではない既存ファイルのときは、上書きせずに終了します。`;

export function parseArgs(argv) {
  const options = { spec: DEFAULT_SPEC, out: DEFAULT_OUT };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--help" || arg === "-h") {
      options.help = true;
      continue;
    }
    const match = /^--(spec|out)(?:=(.*))?$/.exec(arg);
    if (!match) throw new Error(`不明な引数です: ${arg}（使い方: --spec <openapi.json> --out <出力先.html>。--help で説明を表示）`);
    let value = match[2];
    if (value === undefined) {
      i += 1;
      value = argv[i];
    }
    if (!value || value.startsWith("--")) throw new Error(`--${match[1]} には値が必要です`);
    options[match[1]] = path.resolve(value);
  }
  return options;
}

// 実在するパスは realpath にする。まだ無いパスは、実在する一番近い親の realpath に残りをつなぐ
function realPathOrParent(target) {
  try {
    return realpathSync(target);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
    const parent = path.dirname(target);
    if (parent === target) return target;
    return path.join(realPathOrParent(parent), path.basename(target));
  }
}

function hasGeneratorMarker(file) {
  const fd = openSync(file, "r");
  try {
    const buffer = Buffer.alloc(4096);
    const length = readSync(fd, buffer, 0, buffer.length, 0);
    return buffer.toString("utf8", 0, length).includes(GENERATOR_MARKER);
  } finally {
    closeSync(fd);
  }
}

// 出力先が安全か確かめる。仕様ファイルや、このスクリプトが作ったものではないファイルを壊さないため、書く前に必ず通す
export function assertSafeOutput(specFile, out) {
  if (path.extname(out).toLowerCase() !== ".html") throw new Error(`--out の拡張子は .html にしてください: ${out}`);
  if (realPathOrParent(out) === realPathOrParent(specFile)) {
    throw new Error(`--out が --spec と同じファイルです。仕様ファイルを上書きしないよう中止しました: ${out}`);
  }
  let stat;
  try {
    stat = statSync(out);
  } catch (error) {
    if (error.code === "ENOENT") return;
    throw error;
  }
  if (!stat.isFile()) throw new Error(`--out が通常のファイルではありません: ${out}`);
  if (!hasGeneratorMarker(out)) {
    throw new Error(`--out の既存ファイルは、このスクリプトが作ったものではないため上書きしません: ${out}（不要なら先に削除してください）`);
  }
}

export function build({ spec: specFile, out }) {
  assertSafeOutput(specFile, out);
  const spec = loadSpec(specFile);
  const html = buildHtml(spec);
  mkdirSync(path.dirname(out), { recursive: true });
  writeFileSync(out, html, "utf8");
  return { out, operations: collectOperations(spec).length };
}

function main() {
  try {
    const options = parseArgs(process.argv.slice(2));
    if (options.help) {
      console.log(USAGE);
      return;
    }
    const result = build(options);
    console.log(`${path.relative(process.cwd(), result.out) || result.out} を出力しました（操作 ${result.operations} 件）`);
  } catch (error) {
    console.error(`エラー: ${error.message}`);
    process.exit(1);
  }
}

// シンボリックリンク経由の起動でも動くよう、実体のパスどうしで比較する
function isDirectRun() {
  if (!process.argv[1]) return false;
  try {
    return realpathSync(process.argv[1]) === realpathSync(fileURLToPath(import.meta.url));
  } catch {
    return false;
  }
}

if (isDirectRun()) {
  main();
}
