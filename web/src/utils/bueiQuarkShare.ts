// buei 定制：夸克「来自：分享」临时目录识别。
// 夸克会把分享转存内容放在根目录的「来自：分享」文件夹中；
// 只有打开该文件夹时才展示「入库」入口。
export function normalizeQuarkShareInboxName(name: string): string {
  return String(name ?? "")
    .replace(/\s+/g, "")
    .replace(/[:：]/g, ":")
    .trim();
}

export function isQuarkShareInboxName(name: string): boolean {
  return normalizeQuarkShareInboxName(name) === "来自:分享";
}
