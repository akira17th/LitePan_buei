/**
 * buei:emby-library —— 项目目录浏览里的「关联文件」分组。
 * 与后端 internal/buei/embyarchive/project_delete.go 的前缀规则保持一致：
 * 只有勾选视频正片（网盘侧）或 .strm（STRM 侧）才算一组关联文件，
 * 同一组指「以主文件名开头」的文件（aaaa.strm → aaaa.nfo / aaaa.zh.srt / aaaa-thumb.jpg）。
 */

export interface ProjectAssocGroup {
  prefix: string;
  color: string;
}

export interface ProjectAssocSelection {
  side: "strm" | "cloud";
  names: string[];
}

// 多组关联同时存在时用来区分的高亮色：按黄金角展开色相，勾选很多互不关联的文件也不会撞色。
const ASSOC_HUE_START = 205;
const ASSOC_GOLDEN_ANGLE = 137.508;

export function projectAssocColor(index: number): string {
  const hue = (ASSOC_HUE_START + index * ASSOC_GOLDEN_ANGLE) % 360;
  return `hsl(${hue.toFixed(1)} 72% 52%)`;
}

const VIDEO_EXTENSIONS = new Set([
  ".mkv", ".mp4", ".m4v", ".ts", ".m2ts", ".iso",
  ".avi", ".mov", ".wmv", ".flv", ".rmvb", ".rm", ".webm", ".mpg", ".mpeg", ".vob", ".3gp",
]);

function fileExtension(name: string): string {
  const trimmed = name.trim();
  const dot = trimmed.lastIndexOf(".");
  if (dot <= 0) return "";
  return trimmed.slice(dot).toLowerCase();
}

export function isProjectVideoName(name: string): boolean {
  return VIDEO_EXTENSIONS.has(fileExtension(name));
}

export function isProjectStrmName(name: string): boolean {
  return fileExtension(name) === ".strm";
}

/** 主文件名前缀：去掉扩展名并小写。 */
export function projectMediaPrefix(name: string): string {
  const trimmed = name.trim();
  const dot = trimmed.lastIndexOf(".");
  const base = dot > 0 ? trimmed.slice(0, dot) : trimmed;
  return base.trim().toLowerCase();
}

/** 文件名是否属于某个主文件名前缀（前缀本身，或其后紧跟 . - _ 空格 (）。 */
export function projectMatchesPrefix(name: string, prefix: string): boolean {
  const lower = name.trim().toLowerCase();
  if (!lower || !prefix || !lower.startsWith(prefix)) return false;
  if (lower.length === prefix.length) return true;
  const next = lower.charAt(prefix.length);
  return next === "." || next === "-" || next === "_" || next === " " || next === "(";
}

/**
 * 由两侧的勾选项得到关联分组（按勾选顺序，颜色互不相同）。
 * 只有视频正片与 .strm 会产生分组，和删除时的连带规则一致。
 */
export function projectAssocGroups(selections: ProjectAssocSelection[]): ProjectAssocGroup[] {
  const groups: ProjectAssocGroup[] = [];
  const seen = new Set<string>();
  for (const selection of selections) {
    const trigger = selection.side === "cloud" ? isProjectVideoName : isProjectStrmName;
    for (const name of selection.names) {
      if (!trigger(name)) continue;
      const prefix = projectMediaPrefix(name);
      if (!prefix || seen.has(prefix)) continue;
      seen.add(prefix);
      groups.push({ prefix, color: projectAssocColor(groups.length) });
    }
  }
  return groups;
}

/** 返回文件名命中的第一个分组下标，未命中返回 -1。 */
export function projectAssocIndex(name: string, groups: ProjectAssocGroup[]): number {
  for (let i = 0; i < groups.length; i += 1) {
    if (projectMatchesPrefix(name, groups[i].prefix)) return i;
  }
  return -1;
}
