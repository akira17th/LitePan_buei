import type { FileItem } from "@/api/types";
import type { GuangyaShareKind } from "@/api/bueiGuangyaShare";
import { fileKind } from "@/utils/fileIcon";

// buei 定制：影视入库文件类型识别。源列表与目标文件夹浏览共用同一套标签和图标。
const SUBTITLE_EXTENSIONS = new Set(["srt", "ass", "ssa", "vtt", "sub", "idx"]);

export const INGEST_KIND_LABELS: Record<GuangyaShareKind, string> = {
  folder: "文件夹",
  video: "视频",
  audio: "音频",
  image: "图片",
  subtitle: "字幕",
  document: "文档",
  archive: "压缩包",
  other: "其他",
};

export const INGEST_KIND_ICONS: Record<GuangyaShareKind, string> = {
  folder: "folder",
  video: "film",
  audio: "music",
  image: "image",
  subtitle: "closed-captioning-regular",
  document: "file-lines",
  archive: "file-zipper",
  other: "file",
};

export function ingestKindFromFile(file: FileItem): GuangyaShareKind {
  if (file.is_dir) return "folder";
  const ext = file.name.includes(".")
    ? file.name.slice(file.name.lastIndexOf(".") + 1).toLowerCase()
    : "";
  if (SUBTITLE_EXTENSIONS.has(ext)) return "subtitle";
  switch (fileKind(file)) {
    case "video":
      return "video";
    case "audio":
      return "audio";
    case "image":
      return "image";
    case "archive":
      return "archive";
    case "pdf":
    case "doc":
    case "sheet":
    case "slide":
    case "text":
    case "code":
      return "document";
    default:
      return "other";
  }
}
