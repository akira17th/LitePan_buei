import { http } from "./client";

// buei 定制：后台「Emby 媒体库」浏览接口。

export interface BueiEmbyLibraryConfig {
  id: string;
  name: string;
}

export interface BueiEmbyLibrary {
  id: string;
  name: string;
  collection_type?: string;
}

export interface BueiEmbyLibraryUser {
  id: string;
  name: string;
  primary_image_tag?: string;
}

export interface BueiEmbyUserStatus {
  user_id: string;
  user_name: string;
  primary_image_tag?: string;
  played: boolean;
  watched_episodes?: number;
  remaining_episodes?: number;
  total_episodes?: number;
}

export interface BueiEmbyLibraryItem {
  id: string;
  name: string;
  type: "Movie" | "Series" | string;
  production_year?: number;
  overview?: string;
  poster_url?: string;
  backdrop_url?: string;
  total_episodes?: number;
  users: BueiEmbyUserStatus[];
  strm_task_id?: number;
  strm_task_name?: string;
  strm_item_id?: string;
  rel_dir?: string;
  strm_name?: string;
  media_type?: string;
  status?: string;
  has_nfo?: boolean;
  has_poster?: boolean;
  file_count?: number;
  ep_local?: number;
  ep_tmdb?: number;
  ep_scraped?: number;
  tv_state?: string;
  tmdb_id?: string;
}

export interface BueiEmbyLibraryState {
  config: BueiEmbyLibraryConfig;
  libraries: BueiEmbyLibrary[];
  users: BueiEmbyLibraryUser[];
}

export interface BueiEmbyLibraryPage {
  items: BueiEmbyLibraryItem[];
  total_record_count: number;
  start_index: number;
  limit: number;
}

export interface BueiEmbyLibrarySyncItem {
  key: string;
  total_episodes?: number;
  users: BueiEmbyUserStatus[];
}

export interface BueiEmbyLibrarySyncState {
  matched_tasks: Array<{ id: number; name: string }>;
  items: BueiEmbyLibrarySyncItem[];
}

export interface BueiEmbyLibrarySyncResult {
  config_id: string;
  library_id: string;
  library_name: string;
  matched_tasks: Array<{ id: number; name: string }>;
  items?: BueiEmbyLibrarySyncItem[];
  emby_items: number;
  strm_items: number;
  matched_items: number;
  saved_items: number;
  unmatched_items: number;
  failed_items?: string[];
}

export interface BueiEmbyLibraryArchiveResult {
  moved: boolean;
  strm_moved: boolean;
  emby_refreshed: boolean;
  emby_library_name?: string;
  item_name: string;
  source_task_id: number;
  source_task_name: string;
  target_task_id: number;
  target_task_name: string;
  source_path: string;
  target_path: string;
}

export interface BueiEmbyLibraryRestoreResult {
  moved: boolean;
  strm_moved: boolean;
  emby_refreshed: boolean;
  item_name: string;
  source_task_name: string;
  target_task_name: string;
  source_path: string;
  target_path: string;
}

export interface BueiEmbyLibraryPerson {
  name: string;
  role?: string;
  type?: string;
}

export interface BueiEmbyLibrarySeason {
  id: string;
  name: string;
  index_number?: number;
  production_year?: number;
  poster_url?: string;
}

export interface BueiEmbyLibraryDetail {
  item: BueiEmbyLibraryItem;
  premiere_date?: string;
  tagline?: string;
  official_rating?: string;
  community_rating?: number;
  runtime_minutes?: number;
  genres?: string[];
  studios?: string[];
  people?: BueiEmbyLibraryPerson[];
  seasons?: BueiEmbyLibrarySeason[];
}

const basePath = "/admin/buei/emby-library";

export function fetchBueiEmbyLibraryState(configId = "") {
  return http.get<BueiEmbyLibraryState>(`${basePath}/libraries`, configId ? { config_id: configId } : undefined);
}

export function fetchBueiEmbyLibraryItems(libraryId: string, startIndex = 0, limit = 200, configId = "") {
  return http.get<BueiEmbyLibraryPage>(`${basePath}/items`, {
    config_id: configId || undefined,
    library_id: libraryId,
    start_index: startIndex,
    limit,
  });
}

export function fetchBueiEmbyLibraryDetail(itemId: string, configId = "") {
  return http.get<BueiEmbyLibraryDetail>(`${basePath}/items/${encodeURIComponent(itemId)}`, configId ? { config_id: configId } : undefined);
}


export function syncBueiEmbyLibrary(input: { config_id?: string; library_id: string; library_name?: string }) {
  return http.postWithTimeout<BueiEmbyLibrarySyncResult>(`${basePath}/sync`, input, 10 * 60 * 1000);
}

export function archiveBueiEmbyLibraryItem(itemId: string, configId = "", libraryId = "") {
  return http.postWithTimeout<BueiEmbyLibraryArchiveResult>(
    `${basePath}/archive`,
    { item_id: itemId, config_id: configId || undefined, library_id: libraryId || undefined },
    5 * 60 * 1000,
  );
}

export function restoreBueiEmbyLibraryItem(itemId: string, configId = "") {
  return http.postWithTimeout<BueiEmbyLibraryRestoreResult>(
    `${basePath}/restore`,
    { item_id: itemId, config_id: configId || undefined },
    5 * 60 * 1000,
  );
}

export function setBueiEmbyLibraryTVState(input: { item_id: string; ended: boolean }) {
  return http.post<{ item_id: string; tv_state: string }>(`${basePath}/tv-state`, input);
}

export function setBueiEmbyLibraryUserPlayed(input: {
  item_id: string;
  user_id: string;
  played: boolean;
  config_id?: string;
}) {
  return http.post<{ user: BueiEmbyUserStatus }>(`${basePath}/played`, input);
}

export function fetchBueiEmbyLibrarySyncState(libraryId: string, configId = "") {
  return http.get<BueiEmbyLibrarySyncState>(`${basePath}/sync-state`, {
    config_id: configId || undefined,
    library_id: libraryId,
  });
}
