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
