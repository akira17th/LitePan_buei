package guangyashare

import (
	"path"
	"strings"
)

// 文件大类，前端据此做快速勾选与筛选。与光鸭 fileType 枚举不完全一致：
// 字幕/字体/代码/种子/安装包等统一归入 other，避免工具栏过长。
const (
	KindFolder   = "folder"
	KindVideo    = "video"
	KindAudio    = "audio"
	KindImage    = "image"
	KindDocument = "document"
	KindArchive  = "archive"
	KindOther    = "other"
)

// 光鸭 fileType 枚举（来自 Web 端）：0 未知 1 图片 2 视频 3 音频 4 文档 5 压缩包
// 6 字幕 7 字体 8 安装包 9 种子 10 代码 11 其它。
const (
	fileTypeImage    = 1
	fileTypeVideo    = 2
	fileTypeAudio    = 3
	fileTypeDocument = 4
	fileTypeArchive  = 5
)

var kindByExtension = map[string]string{
	".mp4": KindVideo, ".mkv": KindVideo, ".avi": KindVideo, ".mov": KindVideo,
	".wmv": KindVideo, ".flv": KindVideo, ".webm": KindVideo, ".m4v": KindVideo,
	".ts": KindVideo, ".m2ts": KindVideo, ".rmvb": KindVideo, ".rm": KindVideo,
	".3gp": KindVideo, ".mpg": KindVideo, ".mpeg": KindVideo, ".vob": KindVideo,
	".iso": KindArchive, ".m3u8": KindVideo,

	".mp3": KindAudio, ".flac": KindAudio, ".wav": KindAudio, ".aac": KindAudio,
	".m4a": KindAudio, ".ogg": KindAudio, ".wma": KindAudio, ".ape": KindAudio,
	".opus": KindAudio, ".dts": KindAudio, ".amr": KindAudio,

	".jpg": KindImage, ".jpeg": KindImage, ".png": KindImage, ".gif": KindImage,
	".webp": KindImage, ".bmp": KindImage, ".svg": KindImage, ".heic": KindImage,
	".tif": KindImage, ".tiff": KindImage, ".avif": KindImage, ".raw": KindImage,

	".pdf": KindDocument, ".doc": KindDocument, ".docx": KindDocument, ".xls": KindDocument,
	".xlsx": KindDocument, ".ppt": KindDocument, ".pptx": KindDocument, ".txt": KindDocument,
	".md": KindDocument, ".epub": KindDocument, ".mobi": KindDocument, ".azw3": KindDocument,
	".csv": KindDocument, ".rtf": KindDocument,

	".zip": KindArchive, ".rar": KindArchive, ".7z": KindArchive, ".tar": KindArchive,
	".gz": KindArchive, ".bz2": KindArchive, ".xz": KindArchive, ".tgz": KindArchive,
}

// classifyKind 依据光鸭 fileType 与扩展名判断文件大类。
func classifyKind(name string, fileType int, isDir bool) string {
	if isDir {
		return KindFolder
	}
	switch fileType {
	case fileTypeImage:
		return KindImage
	case fileTypeVideo:
		return KindVideo
	case fileTypeAudio:
		return KindAudio
	case fileTypeDocument:
		return KindDocument
	case fileTypeArchive:
		return KindArchive
	}
	if kind, ok := kindByExtension[strings.ToLower(path.Ext(name))]; ok {
		return kind
	}
	return KindOther
}
