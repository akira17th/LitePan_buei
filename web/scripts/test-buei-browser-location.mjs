import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import ts from "typescript";

// 用 TypeScript 转译后再执行，避免依赖 Node 22 的 .ts 直读能力（Docker 前端阶段是 Node 20）。
function loadModule(file) {
  const { outputText } = ts.transpileModule(readFileSync(new URL(file, import.meta.url), "utf8"), {
    compilerOptions: {
      module: ts.ModuleKind.CommonJS,
      target: ts.ScriptTarget.ES2022,
    },
  });
  const exports = {};
  new Function("exports", "module", outputText)(exports, { exports });
  return exports;
}

const {
  browserLocationKey,
  buildBrowserLocationQuery,
  parseBrowserLocationQuery,
  stripBrowserLocationQuery,
} = loadModule("../src/utils/bueiBrowserLocation.ts");

const ROOT = { id: "", name: "根目录" };
const MOVIES = { id: "folder-电影", name: "电影 & 2024 #1" };
const EXTRAS = { id: "folder-😀", name: "花絮 😀 / 特别篇" };

function roundTrip(accountId, crumbs) {
  const query = buildBrowserLocationQuery({ accountId, crumbs });
  const parsed = parseBrowserLocationQuery(query);
  assert.equal(parsed.status, "valid", `应能解析：${JSON.stringify(query)}`);
  assert.equal(parsed.location.accountId, accountId);
  assert.equal(
    browserLocationKey(parsed.location),
    browserLocationKey({ accountId, crumbs }),
    "编解码后位置应一致",
  );
  return parsed.location;
}

// 根目录只写 account，不写 dir。
const rootQuery = buildBrowserLocationQuery({ accountId: 7, crumbs: [ROOT] }, { taskPanel: "relay" });
assert.equal(rootQuery.account, "7");
assert.equal(rootQuery.dir, undefined);
assert.equal(rootQuery.taskPanel, "relay");
const parsedRoot = parseBrowserLocationQuery(rootQuery);
assert.equal(parsedRoot.status, "valid");
assert.deepEqual(parsedRoot.location.crumbs, [ROOT]);

// 中文、空格、&、#、emoji、斜杠都要能原样往返。
const directoryQuery = buildBrowserLocationQuery({ accountId: 7, crumbs: [ROOT, MOVIES, EXTRAS] });
assert.match(String(directoryQuery.dir), /^v1\.[A-Za-z0-9_-]+$/);
const directory = roundTrip(7, [ROOT, MOVIES, EXTRAS]);
assert.deepEqual(directory.crumbs, [ROOT, MOVIES, EXTRAS]);

// 只带 account 的 URL 表示根目录。
assert.deepEqual(parseBrowserLocationQuery({ account: "42" }), {
  status: "valid",
  location: { accountId: 42, crumbs: [ROOT] },
});

// 保留其它 query 参数，只替换 account/dir。
const preserved = buildBrowserLocationQuery(
  { accountId: 1, crumbs: [ROOT, MOVIES] },
  { taskPanel: "offline", keep: "yes", account: "99", dir: "stale" },
);
assert.equal(preserved.account, "1");
assert.equal(preserved.taskPanel, "offline");
assert.equal(preserved.keep, "yes");
assert.notEqual(preserved.dir, "stale");

const stripped = stripBrowserLocationQuery({ account: "1", dir: "v1.abc", taskPanel: "relay" });
assert.equal(stripped.account, undefined);
assert.equal(stripped.dir, undefined);
assert.equal(stripped.taskPanel, "relay");

// 无位置参数：交给 localStorage / 默认账号逻辑。
assert.deepEqual(parseBrowserLocationQuery({ taskPanel: "relay" }), { status: "none" });
assert.deepEqual(parseBrowserLocationQuery({}), { status: "none" });

// 非法参数：不能抛异常，要返回 invalid。
for (const query of [
  { dir: "v1.abc" },
  { account: "abc" },
  { account: "0" },
  { account: "-1" },
  { account: "1.5" },
  { account: ["1", "2"] },
  { account: "1", dir: "v2.abc" },
  { account: "1", dir: "v1.not base64" },
  { account: "1", dir: `v1.${"A".repeat(9000)}` },
  { account: "1", dir: "v1." + Buffer.from("{", "utf8").toString("base64url") },
  { account: "1", dir: "v1." + Buffer.from(JSON.stringify([["", "根目录"], ["id", "x"], ["", "根目录"]])).toString("base64url") },
]) {
  const parsed = parseBrowserLocationQuery(query);
  assert.equal(parsed.status, "invalid", `应判为非法：${JSON.stringify(query)}`);
}

// 空数组按根目录处理，兼容外部生成的简化链接。
const emptyDir = parseBrowserLocationQuery({
  account: "1",
  dir: "v1." + Buffer.from("[]", "utf8").toString("base64url"),
});
assert.equal(emptyDir.status, "valid");
assert.deepEqual(emptyDir.location.crumbs, [ROOT]);

// 面包屑数量上限。
const tooDeep = [ROOT, ...Array.from({ length: 64 }, (_, index) => ({ id: `id-${index}`, name: `目录 ${index}` }))];
assert.equal(parseBrowserLocationQuery(buildBrowserLocationQuery({ accountId: 1, crumbs: tooDeep })).status, "invalid");

// 支持不带根节点的紧凑编码。
const compactPayload = Buffer.from(JSON.stringify([[MOVIES.id, MOVIES.name]]), "utf8").toString("base64url");
const compact = parseBrowserLocationQuery({ account: "3", dir: `v1.${compactPayload}` });
assert.equal(compact.status, "valid");
assert.deepEqual(compact.location.crumbs, [ROOT, MOVIES]);

// 外部工具可能输出带 padding 的 base64url，也应接受。
const padded = parseBrowserLocationQuery({ account: "3", dir: `v1.${compactPayload}=` });
assert.equal(padded.status, "valid");
assert.deepEqual(padded.location.crumbs, [ROOT, MOVIES]);

console.log("buei browser location ok: base64url roundtrip, validation, query preservation");
