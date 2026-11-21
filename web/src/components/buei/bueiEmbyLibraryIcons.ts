// buei 定制：Emby 媒体库分类图标。
// 依据 Emby 返回的 collection_type 区分媒体库类型，避免所有媒体库共用文件夹图标。
// 图标统一为 24×24 线性风格，颜色由外部 currentColor 控制，便于跟随主题与激活态。

export type BueiEmbyLibraryKind =
  | "movies"
  | "series"
  | "music"
  | "photos"
  | "homevideos"
  | "books"
  | "boxsets"
  | "mixed";

function frame(body: string) {
  return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" xmlns="http://www.w3.org/2000/svg">${body}</svg>`;
}

const ICONS: Record<BueiEmbyLibraryKind, string> = {
  // 电影：胶片画格
  movies: frame(
    '<rect x="2.9" y="5.9" width="18.2" height="12.2" rx="2.6"/><path d="M8.1 5.9v12.2M15.9 5.9v12.2"/>',
  ),
  // 剧集：电视 + 天线 + 底座
  series: frame(
    '<rect x="2.9" y="6.4" width="18.2" height="11.6" rx="2.4"/><path d="M8.6 3.1 12 6.4l3.4-3.3"/><path d="M12 18v2.3"/><path d="M8.8 21.3h6.4"/>',
  ),
  // 音乐：连音符
  music: frame(
    '<path d="M9.3 17.5V6.7l9.4-2.1v10.5"/><circle cx="6.6" cy="17.5" r="2.7"/><circle cx="16" cy="15.1" r="2.7"/>',
  ),
  // 照片：相框 + 太阳 + 山峦
  photos: frame(
    '<rect x="2.9" y="4.9" width="18.2" height="14.2" rx="2.6"/><circle cx="8.6" cy="9.7" r="1.6"/><path d="M4.6 17.3l4-4.3a1.5 1.5 0 0 1 2.2 0l2.7 2.9 1.9-2a1.4 1.4 0 0 1 2.1 0l1.9 2.1"/>',
  ),
  // 家庭视频：摄像机
  homevideos: frame(
    '<rect x="2.9" y="7.3" width="12.1" height="9.4" rx="2.3"/><path d="M15.2 11.4l4.7-2.7a1 1 0 0 1 1.5.9v4.8a1 1 0 0 1-1.5.9l-4.7-2.7z"/>',
  ),
  // 书籍：书脊 + 封面
  books: frame(
    '<path d="M19.2 2.9H6.7A2.5 2.5 0 0 0 4.2 5.4v13.3a2.5 2.5 0 0 0 2.5 2.5h12.5z"/><path d="M4.2 18.7a2.5 2.5 0 0 1 2.5-2.5h12.5"/>',
  ),
  // 合集：叠层
  boxsets: frame(
    '<path d="M12 2.9 2.9 7.5 12 12.1l9.1-4.6z"/><path d="M3.1 12.1 12 16.5l8.9-4.4"/><path d="M3.1 16.6 12 21l8.9-4.4"/>',
  ),
  // 混合：文件夹
  mixed: frame(
    '<path d="M5.5 5h3.2l1.9 2.2h7.9a2.4 2.4 0 0 1 2.4 2.4v8.5a2.4 2.4 0 0 1-2.4 2.4H5.5a2.4 2.4 0 0 1-2.4-2.4V7.4A2.4 2.4 0 0 1 5.5 5z"/>',
  ),
};

const LABELS: Record<BueiEmbyLibraryKind, string> = {
  movies: "电影",
  series: "剧集",
  music: "音乐",
  photos: "照片",
  homevideos: "家庭视频",
  books: "书籍",
  boxsets: "合集",
  mixed: "混合内容",
};

// Emby CollectionType 取值见服务器 /Library/VirtualFolders 返回。
const COLLECTION_KINDS: Record<string, BueiEmbyLibraryKind> = {
  movies: "movies",
  movie: "movies",
  tvshows: "series",
  tvshow: "series",
  series: "series",
  music: "music",
  musicvideos: "music",
  musicvideo: "music",
  homevideos: "homevideos",
  homevideo: "homevideos",
  photos: "photos",
  photo: "photos",
  books: "books",
  book: "books",
  boxsets: "boxsets",
  boxset: "boxsets",
  collections: "boxsets",
  collection: "boxsets",
  mixed: "mixed",
};

export function bueiEmbyLibraryKind(collectionType?: string, name?: string): BueiEmbyLibraryKind {
  const type = (collectionType || "").trim().toLowerCase();
  const matched = type ? COLLECTION_KINDS[type] : undefined;
  if (matched) return matched;
  // 部分 Emby 版本不返回 collection_type，再按媒体库名称兜底判断。
  const text = (name || "").trim().toLowerCase();
  if (/电影|影片|movie|film/.test(text)) return "movies";
  if (/剧|电视|动漫|动画|综艺|纪录片|记录片|variety|documentary|tv|series|show|anime/.test(text)) return "series";
  if (/音乐|歌|music/.test(text)) return "music";
  if (/照片|相册|图片|photo|image|picture/.test(text)) return "photos";
  if (/家庭|录像|home\s?video/.test(text)) return "homevideos";
  if (/书|book|有声/.test(text)) return "books";
  if (/合集|box\s?set|collection/.test(text)) return "boxsets";
  return "mixed";
}

export function bueiEmbyLibraryKindLabel(collectionType?: string, name?: string): string {
  return LABELS[bueiEmbyLibraryKind(collectionType, name)];
}

export function bueiEmbyLibraryIcon(kind: BueiEmbyLibraryKind): string {
  return ICONS[kind] || ICONS.mixed;
}
