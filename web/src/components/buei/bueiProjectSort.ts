import type { BueiEmbyLibraryProjectEntry } from "@/api/bueiEmbyLibrary";

export type ProjectSortField = "name" | "type" | "size" | "time";
export type ProjectSortDir = "asc" | "desc" | "";

/**
 * 媒体库项目目录里的文件类型。排序与筛选项按下面的声明顺序展示：
 * 目录在前，其次按“媒体 → 播放 → 元数据”的顺序排列。
 */
export type ProjectEntryKind = "dir" | "strm" | "video" | "audio" | "subtitle" | "image" | "nfo" | "json" | "other";

const KIND_RANK: Record<ProjectEntryKind, number> = {
  dir: 0,
  strm: 1,
  video: 2,
  audio: 3,
  subtitle: 4,
  image: 5,
  nfo: 6,
  json: 7,
  other: 8,
};

export const PROJECT_ENTRY_KIND_LABELS: Record<ProjectEntryKind, string> = {
  dir: "文件夹",
  strm: "STRM",
  video: "视频",
  audio: "音频",
  subtitle: "字幕",
  image: "图片",
  nfo: "NFO",
  json: "JSON",
  other: "其他",
};

export const PROJECT_ENTRY_KIND_ICONS: Record<ProjectEntryKind, string> = {
  dir: "folder",
  strm: "film",
  video: "film",
  audio: "music",
  subtitle: "closed-captioning-regular",
  image: "image",
  nfo: "file-lines",
  json: "file",
  other: "file",
};

const MEDIA_EXTENSIONS: Record<Exclude<ProjectEntryKind, "dir" | "nfo" | "json" | "other">, Set<string>> = {
  strm: new Set(["strm"]),
  video: new Set(["mkv", "mp4", "m4v", "ts", "m2ts", "iso", "avi", "mov", "wmv", "flv", "rmvb", "rm", "webm", "mpg", "mpeg", "vob", "3gp"]),
  audio: new Set(["flac", "ape", "wav", "mp3", "aac", "m4a", "ogg", "opus", "wma", "dsf", "dff"]),
  subtitle: new Set(["srt", "ass", "ssa", "sub", "idx", "vtt", "sup", "smi", "ttml"]),
  image: new Set(["jpg", "jpeg", "png", "webp", "gif", "bmp", "avif", "tiff", "svg", "ico"]),
};

function extension(name: string) {
  const index = name.lastIndexOf(".");
  return index > 0 ? name.slice(index + 1).toLowerCase() : "";
}

/** 按名称与是否目录判断项目类型；媒体库里的播放文件、字幕、海报、元数据分开归类。 */
export function projectEntryKind(entry: BueiEmbyLibraryProjectEntry): ProjectEntryKind {
  if (entry.is_dir) return "dir";
  const ext = extension(entry.name);
  if (ext === "nfo") return "nfo";
  if (ext === "json") return "json";
  for (const kind of ["strm", "video", "audio", "subtitle", "image"] as const) {
    if (MEDIA_EXTENSIONS[kind].has(ext)) return kind;
  }
  return "other";
}

export function projectEntryKindLabel(kind: ProjectEntryKind): string {
  return PROJECT_ENTRY_KIND_LABELS[kind];
}

export function projectEntryKindIcon(kind: ProjectEntryKind): string {
  return PROJECT_ENTRY_KIND_ICONS[kind];
}

const collator = new Intl.Collator("zh-Hans-CN", { numeric: true, sensitivity: "base" });

function compareName(a: BueiEmbyLibraryProjectEntry, b: BueiEmbyLibraryProjectEntry) {
  return collator.compare(a.name, b.name);
}

function compareTime(a: BueiEmbyLibraryProjectEntry, b: BueiEmbyLibraryProjectEntry) {
  return (Date.parse(a.modified_at || "") || 0) - (Date.parse(b.modified_at || "") || 0);
}

/** 目录始终排在文件前面，组内再按选定字段排序；未选择字段时按名称升序。 */
export function sortProjectEntries(
  entries: BueiEmbyLibraryProjectEntry[],
  field: ProjectSortField,
  dir: ProjectSortDir,
): BueiEmbyLibraryProjectEntry[] {
  const list = [...entries];
  list.sort((a, b) => {
    if (field === "type" && dir) {
      const result = KIND_RANK[projectEntryKind(a)] - KIND_RANK[projectEntryKind(b)] || compareName(a, b);
      return dir === "desc" ? -result : result;
    }
    if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
    let result: number;
    switch (field) {
      case "size":
        result = a.size - b.size || compareName(a, b);
        break;
      case "time":
        result = compareTime(a, b) || compareName(a, b);
        break;
      case "type":
      case "name":
      default:
        result = compareName(a, b);
        break;
    }
    if (!dir) return compareName(a, b);
    return dir === "desc" ? -result : result;
  });
  return list;
}

/** 表头点击：正序 → 倒序 → 恢复默认。 */
export function nextProjectSort(
  currentField: ProjectSortField,
  currentDir: ProjectSortDir,
  field: ProjectSortField,
): { field: ProjectSortField; dir: ProjectSortDir } {
  if (currentField !== field || !currentDir) return { field, dir: "asc" };
  if (currentDir === "asc") return { field, dir: "desc" };
  return { field, dir: "" };
}
