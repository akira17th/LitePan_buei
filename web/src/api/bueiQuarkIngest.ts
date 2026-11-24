import { http } from "./client";

// buei 定制：后台「辅助工具 / 影视入库」的夸克接口。

export interface QuarkIngestRenameRule {
  find: string;
  replace_with: string;
}

export interface QuarkIngestRenameReplace {
  enabled: boolean;
  /** 第一条规则的镜像，兼容旧版本。 */
  find: string;
  replace_with: string;
  rules?: QuarkIngestRenameRule[];
}

export interface QuarkIngestFavorite {
  id: number;
  account_id: number;
  link: string;
  folder_id: string;
  parent_folder_id: string;
  folder_name: string;
  created_at?: string;
  updated_at?: string;
}

export interface QuarkIngestFavoritePayload {
  account_id: number;
  link: string;
  folder_id: string;
  parent_folder_id: string;
  folder_name: string;
}

export interface QuarkIngestPreset {
  account_id: number;
  parent_id: string;
  base_path: string;
  base_dirs?: string[];
  title: string;
  season: number;
  is_series: boolean;
  auto_strm: boolean;
  auto_scrape: boolean;
  auto_scrape_task_id: number;
  refresh_emby: boolean;
  emby_config_id: string;
  emby_mode: string;
  emby_library_id: string;
  rename_replace?: QuarkIngestRenameReplace | null;
  updated_at?: string;
  // 秒传到光鸭的相关配置：目标账号与账号内保存位置单独保存，不覆盖夸克保存位置。
  flash_to_guangya?: boolean;
  flash_account_id?: number;
  flash_parent_id?: string;
  flash_base_path?: string;
  flash_base_dirs?: string[];
}

export interface QuarkIngestFile {
  file_id: string;
  original_name: string;
  new_name: string;
  is_dir: boolean;
}

export interface QuarkIngestPayload {
  account_id: number;
  favorite_id: number;
  source_parent_id: string;
  source_folder_parent_id: string;
  source_folder_name: string;
  file_ids: string[];
  parent_id: string;
  target_path: string[];
  base_path: string;
  base_dirs: string[];
  title: string;
  season: number;
  is_series: boolean;
  files: QuarkIngestFile[];
  rename_replace?: QuarkIngestRenameReplace;
  auto_strm: boolean;
  auto_scrape: boolean;
  auto_scrape_task_id: number;
  refresh_emby: boolean;
  emby_config_id: string;
  emby_mode: string;
  emby_library_id: string;
  progress_id: string;
  /** 秒传到光鸭：先把勾选文件下载到服务器，再仅做秒传到光鸭账号。 */
  flash_to_guangya?: boolean;
  flash_account_id?: number;
}

export interface QuarkIngestFlattenResult {
  moved: number;
  renamed: number;
  deleted_folders: number;
}

export interface QuarkIngestResult {
  moved: number;
  renamed: number;
  rename_skipped: number;
  flashed: number;
  /** 未命中秒传被跳过的文件数（有文件成功时整体仍按成功返回）。 */
  missed: number;
  miss_message: string;
  strm_generated: number;
  strm_matched: boolean;
  strm_error: string;
  scrape_started: boolean;
  scrape_error: string;
  emby_refreshed: boolean;
  emby_scheduled: boolean;
  emby_error: string;
}

export interface QuarkIngestStagedFile {
  file_id: string;
  file_name: string;
  original_name: string;
  size: number;
  updated_at?: string;
}

export type QuarkIngestPhase =
  | "transfer"
  | "rename"
  | "download"
  | "flash"
  | "strm"
  | "scrape"
  | "emby"
  | "done"
  | "failed"
  | "";

export interface QuarkIngestProgressFile {
  /** queued | downloading | downloaded | flashed | failed */
  stage: string;
  name: string;
  downloaded: number;
  total: number;
  speed: number;
  message?: string;
}

export interface QuarkIngestProgress {
  found: boolean;
  phase: QuarkIngestPhase;
  error: string;
  /** 下载阶段：当前文件名、已下载/总量（字节）、实时速度（字节/秒）。 */
  file?: string;
  downloaded?: number;
  total?: number;
  speed?: number;
  /** 当前处理的源文件 id。 */
  file_id?: string;
  /** 各源文件的状态（key 为夸克文件 id）。 */
  files?: Record<string, QuarkIngestProgressFile>;
}

export const bueiQuarkIngestApi = {
  // 预设按收藏隔离：新收藏没有自己的预设时返回 null，页面完全使用默认值。
  preset: (accountId: number, favoriteId: number) =>
    http.get<QuarkIngestPreset | null>("/admin/tools/quark-ingest/preset", {
      account_id: accountId,
      favorite_id: favoriteId,
    }),
  favorites: (accountId: number) =>
    http.get<{ items: QuarkIngestFavorite[] }>("/admin/tools/quark-ingest/favorites", {
      account_id: accountId,
    }),
  createFavorite: (payload: QuarkIngestFavoritePayload) =>
    http.post<QuarkIngestFavorite>("/admin/tools/quark-ingest/favorites", payload),
  updateFavorite: (id: number, payload: QuarkIngestFavoritePayload) =>
    http.put<QuarkIngestFavorite>(`/admin/tools/quark-ingest/favorites/${id}`, payload),
  deleteFavorite: (id: number, accountId: number) =>
    http.del<{ deleted: boolean }>(`/admin/tools/quark-ingest/favorites/${id}`, undefined, {
      account_id: accountId,
    }),
  flatten: (accountId: number, favoriteId: number) =>
    http.postWithTimeout<QuarkIngestFlattenResult>(
      "/admin/tools/quark-ingest/flatten",
      { account_id: accountId, favorite_id: favoriteId },
      300_000,
    ),
  progress: (id: string) =>
    http.get<QuarkIngestProgress>("/admin/tools/quark-ingest/progress", { id }),
  // 取消进行中的入库：中断下载、删除临时文件。
  cancel: (progressId: string) =>
    http.post<{ canceled: boolean }>("/admin/tools/quark-ingest/cancel", { progress_id: progressId }),
  // 已下载到服务器临时目录的源文件（秒传到光鸭重试时无需重新下载）。
  staged: (accountId: number, favoriteId: number) =>
    http.get<{ items: QuarkIngestStagedFile[] }>("/admin/tools/quark-ingest/staged", {
      account_id: accountId,
      favorite_id: favoriteId,
    }),
  // 入库本身可能包含大量文件移动与重命名；秒传到光鸭还要下载文件，给足超时。
  transfer: (payload: QuarkIngestPayload, signal?: AbortSignal) =>
    http.postWithTimeout<QuarkIngestResult>(
      "/admin/tools/quark-ingest/transfer",
      payload,
      21_600_000,
      signal,
    ),
};
