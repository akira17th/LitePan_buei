export type ShareLinkProvider = "guangya" | "quark";

// buei 定制：影视入库的 + 收藏弹窗根据链接自动识别网盘。
export function detectShareLinkProvider(input: string): ShareLinkProvider | null {
  const text = String(input ?? "").trim().toLowerCase();
  if (!text) return null;
  if (text.includes("guangyapan.com") || text.includes("guangya") || text.includes("光鸭")) {
    return "guangya";
  }
  if (text.includes("quark.cn") || text.includes("夸克")) {
    return "quark";
  }
  return null;
}
