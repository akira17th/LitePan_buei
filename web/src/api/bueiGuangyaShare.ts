import { http } from "./client";

// buei 定制：后台「辅助工具 / 增强工具 / 光鸭分享收藏」。

export type GuangyaShareKind =
  | "folder"
  | "video"
  | "audio"
  | "image"
  | "subtitle"
  | "document"
  | "archive"
  | "other";

export interface GuangyaShareItem {
  id: string;
  parent_id: string;
  name: string;
  is_dir: boolean;
  size: number;
  mod_time?: string;
  file_type: number;
  kind: GuangyaShareKind;
  path: string;
  depth: number;
}

export interface GuangyaShareDestination {
  account_id: number;
  parent_id: string;
  base_path: string;
  title: string;
  season: number;
  is_series: boolean;
  base_dirs?: string[];
}

export interface GuangyaShareRenameReplace {
  enabled: boolean;
  find: string;
  replace_with: string;
}

export interface GuangyaShareBrowseCrumb {
  id: string;
  name: string;
  path: string;
}

export interface GuangyaShareTransferredFile {
  file_id: string;
  original_name: string;
  new_name: string;
  account_id: number;
  target_parent_id: string;
  status: "transferred" | "discarded" | string;
  updated_at: string;
}

export interface GuangyaShareDestinationBase {
  account_id: number;
  parent_id: string;
  path: string;
  dirs?: string[];
}

export interface GuangyaShareParseResult {
  share_id: string;
  title: string;
  owner: string;
  need_code: boolean;
  need_password: boolean;
  items: GuangyaShareItem[];
  last_destination?: GuangyaShareDestination | null;
  last_base?: GuangyaShareDestinationBase | null;
  transferred_files?: GuangyaShareTransferredFile[];
  last_browse_path?: GuangyaShareBrowseCrumb[];
  rename_replace?: GuangyaShareRenameReplace | null;
}

export interface GuangyaShareSavedLink {
  id: number;
  share_id: string;
  url: string;
  password: string;
  title: string;
  owner: string;
  created_at: string;
  updated_at: string;
  last_used_at: string;
}

export interface GuangyaShareListPayload {
  url: string;
  share_id: string;
  password: string;
  parent_id: string;
  parent_path: string;
}

export interface GuangyaShareTransferFile {
  file_id: string;
  original_name: string;
  new_name: string;
  is_dir: boolean;
}

export interface GuangyaShareTransferPayload {
  url: string;
  share_id: string;
  password: string;
  file_ids: string[];
  account_id: number;
  parent_id: string;
  target_path: string[];
  base_path: string;
  base_dirs: string[];
  title: string;
  season: number;
  is_series: boolean;
  files: GuangyaShareTransferFile[];
  browse_path: GuangyaShareBrowseCrumb[];
  rename_replace: GuangyaShareRenameReplace;
}

export interface GuangyaShareTransferResult {
  task_id: string;
  completed: boolean;
  renamed: number;
  rename_skipped: number;
  rename_scheduled: boolean;
}

export const bueiGuangyaShareApi = {
  parse: (payload: { url: string; password: string }, signal?: AbortSignal) =>
    http.postWithTimeout<GuangyaShareParseResult>(
      "/admin/tools/guangya-share/parse",
      payload,
      180_000,
      signal,
    ),
  list: (payload: GuangyaShareListPayload, signal?: AbortSignal) =>
    http.post<{ items: GuangyaShareItem[] }>(
      "/admin/tools/guangya-share/list",
      payload,
      undefined,
      signal,
    ),
  saved: () => http.get<{ items: GuangyaShareSavedLink[] }>("/admin/tools/guangya-share/saved"),
  discard: (shareId: string, files: { file_id: string; original_name: string }[]) =>
    http.post<{ discarded: number }>("/admin/tools/guangya-share/discard", {
      share_id: shareId,
      files,
    }),
  deleteSaved: (id: number) =>
    http.del<{ deleted: boolean }>(`/admin/tools/guangya-share/saved/${id}`),
  // 大文件夹转存需要等待服务端任务，给足超时时间。
  transfer: (payload: GuangyaShareTransferPayload, signal?: AbortSignal) =>
    http.postWithTimeout<GuangyaShareTransferResult>(
      "/admin/tools/guangya-share/transfer",
      payload,
      180_000,
      signal,
    ),
};
