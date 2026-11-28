import { http } from "./client";

// buei:douban-score —— 辅助工具「豆瓣评分」前端接口。

export type BueiDoubanScoreStatus = "matched" | "not_found" | "error" | "";
export type BueiDoubanFilter = "all" | "unscored" | "scored" | "pending";
export type BueiDoubanSort = "default" | "title_asc" | "year_desc" | "added_desc" | "rating_asc";

export interface BueiDoubanScoreTask {
  id: number;
  name: string;
  status: string;
  recorded: number;
  scored: number;
}

export interface BueiDoubanScoreView {
  rating?: number;
  votes: number;
  douban_id?: string;
  douban_title?: string;
  douban_year?: number;
  is_tv: boolean;
  status: BueiDoubanScoreStatus;
  message?: string;
  query_title?: string;
  checked_at?: string;
  needs_query: boolean;
  /** buei:douban-override —— 手动编辑的豆瓣 ID / 评分 / 不再自动查询。 */
  manual_douban_id?: string;
  manual_rating?: number;
  no_auto_query: boolean;
}

export interface BueiDoubanWallItem {
  id: string;
  strm_task_id: number;
  task_name: string;
  title: string;
  year?: number;
  media_type: string;
  tv_state?: string;
  poster_url?: string;
  status: string;
  added_at?: string;
  /** 作品 NFO / 目录名里的 TMDB ID，用于 TMDB 跳转。 */
  tmdb_id?: string;
  douban?: BueiDoubanScoreView;
}

export interface BueiDoubanItemStats {
  total: number;
  scored: number;
  unscored: number;
  pending: number;
  tv: number;
  movie: number;
}

export interface BueiDoubanItemPage {
  items: BueiDoubanWallItem[];
  total: number;
  offset: number;
  limit: number;
  has_more: boolean;
  stats: BueiDoubanItemStats;
}

export interface BueiDoubanRunStatus {
  title: string;
  state: "queried" | "skipped" | "failed";
  detail?: string;
}

export interface BueiDoubanProgress {
  running: boolean;
  scope: string;
  strm_task_id?: number;
  total: number;
  done: number;
  queried: number;
  skipped: number;
  failed: number;
  current_title?: string;
  current_item_id?: string;
  message?: string;
  error?: string;
  started_at?: string;
  finished_at?: string;
  items?: Record<string, BueiDoubanRunStatus>;
}

export interface BueiDoubanPlanItem {
  id: string;
  title: string;
  year?: number;
  reason: string;
}

export interface BueiDoubanPlan {
  strm_task_id: number;
  task_name: string;
  run: BueiDoubanPlanItem[];
  skip: BueiDoubanPlanItem[];
}

export interface BueiDoubanSummary {
  total: number;
  queried: number;
  skipped: number;
  failed: number;
  started_at?: string;
  finished_at?: string;
}

export interface BueiDoubanState {
  progress: BueiDoubanProgress;
  run_at: string;
  last_run_at?: string;
  next_run_at?: string;
  last_summary?: BueiDoubanSummary;
  /** 最近一次每日自动查询的执行日志（只保留最近一次）。 */
  run_log?: BueiDoubanRunLog;
}

/** buei:run-log —— 每日自动查询日志：各任务待查/成功/失败统计与失败原因。 */
export interface BueiDoubanRunLog {
  finished_at: string;
  duration_ms: number;
  tasks: BueiDoubanRunLogTask[];
  total: number;
  success: number;
  failed: number;
}

export interface BueiDoubanRunLogTask {
  task_id: number;
  task_name: string;
  pending: number;
  success: number;
  failed: number;
  reasons?: string[];
  /** 本轮真正查过的媒体项目（名称 + 结果 + 失败原因）。 */
  items?: BueiDoubanRunLogItem[];
}

export interface BueiDoubanRunLogItem {
  title: string;
  state: "success" | "failed";
  detail?: string;
}

/** buei:douban-library —— 任务 ↔ Emby 媒体库关联。 */
export interface BueiDoubanLibraryOption {
  library_id: string;
  library_name: string;
  updated_at?: string;
}

export interface BueiDoubanLibrariesResponse {
  libraries: BueiDoubanLibraryOption[];
  bindings: Record<string, BueiDoubanLibraryOption>;
}

export interface BueiDoubanItemsQuery {
  offset?: number;
  limit?: number;
  keyword?: string;
  filter?: BueiDoubanFilter;
  media_type?: "movie" | "tv" | "";
  sort?: BueiDoubanSort;
}

export interface BueiDoubanQueryBody {
  strm_task_id?: number;
  item_ids?: string[];
  all_tasks?: boolean;
  force?: boolean;
}

export function fetchBueiDoubanScoreTasks() {
  return http.get<{ tasks: BueiDoubanScoreTask[] }>("/admin/buei/douban-score/tasks");
}

export function fetchBueiDoubanScoreItems(strmTaskId: number, query: BueiDoubanItemsQuery = {}) {
  return http.get<BueiDoubanItemPage>("/admin/buei/douban-score/items", {
    strm_task_id: strmTaskId,
    offset: query.offset ?? 0,
    limit: query.limit ?? 60,
    keyword: query.keyword || undefined,
    filter: query.filter ?? "all",
    media_type: query.media_type || undefined,
    sort: query.sort ?? "default",
  });
}

export function fetchBueiDoubanScorePlan(strmTaskId: number) {
  return http.get<BueiDoubanPlan>("/admin/buei/douban-score/plan", {
    strm_task_id: strmTaskId,
  });
}

export function fetchBueiDoubanScoreState() {
  return http.get<BueiDoubanState>("/admin/buei/douban-score/state");
}

export function queryBueiDoubanScore(body: BueiDoubanQueryBody) {
  return http.post<BueiDoubanProgress>("/admin/buei/douban-score/query", body);
}

export function stopBueiDoubanScore() {
  return http.post<BueiDoubanState>("/admin/buei/douban-score/stop");
}

/** buei:douban-override —— 保存手动编辑项：豆瓣 ID / 豆瓣评分 / 不再自动查询。 */
export function saveBueiDoubanOverride(body: {
  strm_task_id: number;
  item_id: string;
  douban_id: string;
  rating: number | null;
  no_auto_query: boolean;
}) {
  return http.post<{ saved: boolean }>("/admin/buei/douban-score/override", body);
}

/** 读取 Emby 媒体库列表与各任务的关联。 */
export function fetchBueiDoubanLibraries() {
  return http.get<BueiDoubanLibrariesResponse>("/admin/buei/douban-score/libraries");
}

/** 保存任务的关联媒体库；libraryId 为空表示解除关联。 */
export function saveBueiDoubanLibrary(strmTaskId: number, libraryId: string, libraryName: string) {
  return http.post<{ saved: boolean }>("/admin/buei/douban-score/library", {
    strm_task_id: strmTaskId,
    library_id: libraryId,
    library_name: libraryName,
  });
}
