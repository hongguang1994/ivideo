export type BatchProvider = "aliyun" | "115" | "quark" | "";

export interface ShareDraft {
  key: string;
  provider: BatchProvider;
  shareUrl: string;
  sharePwd: string;
  title: string;
  category: string;
  error: string;
}

const URL_PATTERN = /https?:\/\/[^\s，,]+/i;
const CODE_PATTERN = /(?:提取码|访问码|密码|口令|pwd|code)\s*[:：]?\s*([a-z0-9]{2,12})/i;

export function detectShareProvider(rawUrl: string): BatchProvider {
  try {
    const host = new URL(rawUrl).hostname.toLowerCase();
    if (host === "alipan.com" || host.endsWith(".alipan.com") || host === "aliyundrive.com" || host.endsWith(".aliyundrive.com")) return "aliyun";
    if (host === "115.com" || host.endsWith(".115.com")) return "115";
    if (host === "quark.cn" || host.endsWith(".quark.cn")) return "quark";
  } catch {
    return "";
  }
  return "";
}

export function validateShareDraft(draft: ShareDraft): string {
  if (!draft.shareUrl) return "未找到分享链接";
  const detected = detectShareProvider(draft.shareUrl);
  if (!detected) return "链接不是支持的阿里云盘、115 或夸克分享";
  if (draft.provider && draft.provider !== detected) return "网盘类型与链接不匹配";
  return "";
}

export function parseShareBatch(text: string, defaultCategory = ""): ShareDraft[] {
  const drafts = text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line, index) => {
      const match = line.match(URL_PATTERN);
      const shareUrl = (match?.[0] || "").replace(/[。；;）)\]】]+$/, "");
      const provider = detectShareProvider(shareUrl);
      const beforeUrl = match ? line.slice(0, match.index).trim().replace(/[：:，,；;\-]+$/, "").trim() : "";
      const codeMatch = line.match(CODE_PATTERN);
      const afterUrl = match ? line.slice((match.index || 0) + match[0].length).trim() : "";
      const plainCode = afterUrl.match(/^(?:[-—|，,；;]\s*)?([a-z0-9]{2,12})$/i)?.[1] || "";
      const draft: ShareDraft = {
        key: `share-${index}-${shareUrl || line}`,
        provider,
        shareUrl,
        sharePwd: codeMatch?.[1] || plainCode,
        title: beforeUrl,
        category: defaultCategory,
        error: "",
      };
      draft.error = validateShareDraft(draft);
      return draft;
    });

  const seen = new Set<string>();
  return drafts.map((draft) => {
    if (draft.error) return draft;
    const key = `${draft.provider}\0${draft.shareUrl.replace(/#.*$/, "")}`;
    if (seen.has(key)) return { ...draft, error: "本次粘贴中有重复链接" };
    seen.add(key);
    return draft;
  });
}
