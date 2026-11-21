import { http } from "./client";

// buei 定制：归档媒体库的自动归档配置、立即执行与执行日志。

export interface BueiEmbyArchiveSource {
  id: string;
  name: string;
}

export interface BueiEmbyArchiveRule {
  archive_library_id: string;
  archive_library_name: string;
  config_id?: string;
  source_library_ids: string[];
  source_libraries: BueiEmbyArchiveSource[];
  interval_hours: number;
  enabled: boolean;
  last_run_at?: string;
  next_run_at?: string;
}

export interface BueiEmbyArchiveSourceOption {
  id: string;
  name: string;
  collection_type?: string;
  claimed_by_library_id?: string;
  claimed_by_library_name?: string;
}

export interface BueiEmbyArchiveRuleView {
  rule: BueiEmbyArchiveRule;
  source_libraries: BueiEmbyArchiveSourceOption[];
  next_run_at?: string;
}

export type BueiEmbyArchiveRunStatus = "running" | "success" | "partial" | "failed";

export interface BueiEmbyArchiveRunItem {
  name: string;
  media_type?: string;
  source_library?: string;
  source_task?: string;
  target_path?: string;
  result: "archived" | "failed" | "skipped" | string;
  message?: string;
}

export interface BueiEmbyArchiveRun {
  id: number;
  archive_library_id: string;
  archive_library_name: string;
  trigger: "auto" | "manual" | string;
  status: BueiEmbyArchiveRunStatus | string;
  started_at?: string;
  finished_at?: string;
  scanned: number;
  archived: number;
  skipped: number;
  failed: number;
  message?: string;
  items: BueiEmbyArchiveRunItem[];
}

const basePath = "/admin/buei/emby-library";

export function fetchBueiEmbyArchiveRule(libraryId: string, configId = "") {
  return http.get<BueiEmbyArchiveRuleView>(`${basePath}/archive-rule`, {
    library_id: libraryId,
    config_id: configId || undefined,
  });
}

// 只读取已保存的配置，不访问 Emby，用于页面标题展示执行计划。
export function fetchBueiEmbyArchiveRuleSummary(libraryId: string) {
  return http.get<BueiEmbyArchiveRuleView>(`${basePath}/archive-rule`, {
    library_id: libraryId,
    summary: 1,
  });
}

export function saveBueiEmbyArchiveRule(input: {
  config_id?: string;
  archive_library_id: string;
  source_library_ids: string[];
  interval_hours: number;
  enabled: boolean;
}) {
  return http.put<BueiEmbyArchiveRule>(`${basePath}/archive-rule`, input);
}

export function runBueiEmbyArchiveNow(input: { config_id?: string; archive_library_id: string }) {
  return http.post<BueiEmbyArchiveRun>(`${basePath}/archive-run`, input);
}

export function fetchBueiEmbyArchiveLogs(libraryId: string, limit = 10) {
  return http.get<{ items: BueiEmbyArchiveRun[] }>(`${basePath}/archive-logs`, {
    library_id: libraryId,
    limit,
  });
}
