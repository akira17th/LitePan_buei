import type { LocationQuery, LocationQueryRaw } from "vue-router";
import type { Crumb } from "@/stores/browser";

export interface BrowserUrlLocation {
  accountId: number;
  crumbs: Crumb[];
}

export type BrowserLocationParseResult =
  | { status: "none" }
  | { status: "invalid"; reason: string }
  | { status: "valid"; location: BrowserUrlLocation };

export const BROWSER_URL_ACCOUNT_KEY = "account";
export const BROWSER_URL_DIR_KEY = "dir";

const DIR_VERSION = "v1";
const MAX_DIR_LENGTH = 8192;
const MAX_CRUMBS = 64;
const MAX_NAME_LENGTH = 1024;
const MAX_ID_LENGTH = 1024;
const ROOT: Crumb = { id: "", name: "根目录" };

function encodeBase64Url(bytes: Uint8Array): string {
  let binary = "";
  const chunkSize = 0x8000;
  for (let offset = 0; offset < bytes.length; offset += chunkSize) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + chunkSize));
  }
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function decodeBase64Url(value: string): Uint8Array | null {
  // 兼容外部工具生成的带 padding 的 base64url（本函数输出不带 padding）。
  const normalized = value.replace(/=+$/, "");
  if (!normalized || !/^[A-Za-z0-9_-]+$/.test(normalized)) return null;
  try {
    const base64 = normalized.replace(/-/g, "+").replace(/_/g, "/");
    const padding = (4 - (base64.length % 4)) % 4;
    const binary = atob(base64 + "=".repeat(padding));
    return Uint8Array.from(binary, (char) => char.charCodeAt(0));
  } catch {
    return null;
  }
}

function encodeCrumbs(crumbs: Crumb[]): string {
  const rest = crumbs[0]?.id === "" ? crumbs.slice(1) : crumbs;
  const payload = rest.map((crumb) => [crumb.id, crumb.name]);
  const json = JSON.stringify(payload);
  return `${DIR_VERSION}.${encodeBase64Url(new TextEncoder().encode(json))}`;
}

function decodeCrumbs(raw: string): Crumb[] | null {
  if (!raw.startsWith(`${DIR_VERSION}.`) || raw.length > MAX_DIR_LENGTH) return null;
  const encoded = raw.slice(DIR_VERSION.length + 1);
  const bytes = decodeBase64Url(encoded);
  if (!bytes) return null;

  let parsed: unknown;
  try {
    parsed = JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(bytes));
  } catch {
    return null;
  }
  if (!Array.isArray(parsed) || parsed.length > MAX_CRUMBS) return null;

  const crumbs: Crumb[] = [];
  for (const entry of parsed) {
    if (!Array.isArray(entry) || entry.length !== 2) return null;
    const [id, name] = entry;
    if (typeof id !== "string" || typeof name !== "string") return null;
    if (id.length > MAX_ID_LENGTH || name.length > MAX_NAME_LENGTH) return null;
    crumbs.push({ id, name });
  }
  if (!crumbs.length) return [ROOT];
  if (crumbs.some((crumb, index) => crumb.id === "" && index !== 0)) return null;
  if (crumbs[0].id === "") {
    if (crumbs.length > MAX_CRUMBS) return null;
    if (crumbs.length > 1 && crumbs[crumbs.length - 1].id === "") return null;
    // 根节点名称只是展示文案，统一用当前 UI 的“根目录”，避免外部链接带入其它语言的根名。
    return [{ ...ROOT }, ...crumbs.slice(1)];
  }
  if (crumbs.length + 1 > MAX_CRUMBS) return null;
  return [ROOT, ...crumbs];
}

export function parseBrowserLocationQuery(query: LocationQuery): BrowserLocationParseResult {
  const rawAccount = query[BROWSER_URL_ACCOUNT_KEY];
  const rawDir = query[BROWSER_URL_DIR_KEY];
  const hasAccount = rawAccount !== undefined && rawAccount !== null && rawAccount !== "";
  const hasDir = rawDir !== undefined && rawDir !== null && rawDir !== "";
  if (!hasAccount && !hasDir) return { status: "none" };
  if (!hasAccount) return { status: "invalid", reason: "缺少 account 参数" };
  if (typeof rawAccount !== "string" || !/^\d+$/.test(rawAccount)) {
    return { status: "invalid", reason: "account 参数不合法" };
  }

  const accountId = Number(rawAccount);
  if (!Number.isSafeInteger(accountId) || accountId <= 0) {
    return { status: "invalid", reason: "account 参数不合法" };
  }
  if (!hasDir) return { status: "valid", location: { accountId, crumbs: [ROOT] } };
  if (typeof rawDir !== "string") {
    return { status: "invalid", reason: "dir 参数不合法" };
  }

  const crumbs = decodeCrumbs(rawDir);
  if (!crumbs || crumbs.length > MAX_CRUMBS) {
    return { status: "invalid", reason: "dir 参数不合法" };
  }
  return { status: "valid", location: { accountId, crumbs } };
}

export function buildBrowserLocationQuery(
  location: BrowserUrlLocation,
  baseQuery: LocationQuery = {},
): LocationQueryRaw {
  const next: LocationQueryRaw = { ...baseQuery };
  const crumbs = location.crumbs.length ? location.crumbs : [ROOT];
  next[BROWSER_URL_ACCOUNT_KEY] = String(location.accountId);
  if (crumbs.length > 1 || crumbs[0]?.id !== "") {
    next[BROWSER_URL_DIR_KEY] = encodeCrumbs(crumbs);
  } else {
    delete next[BROWSER_URL_DIR_KEY];
  }
  return next;
}

export function stripBrowserLocationQuery(query: LocationQuery): LocationQueryRaw {
  const next: LocationQueryRaw = { ...query };
  delete next[BROWSER_URL_ACCOUNT_KEY];
  delete next[BROWSER_URL_DIR_KEY];
  return next;
}

export function browserLocationKey(location: BrowserUrlLocation | null): string {
  if (!location) return "";
  return JSON.stringify([
    location.accountId,
    location.crumbs.map((crumb) => [crumb.id, crumb.name]),
  ]);
}
