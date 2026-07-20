// 后端 API 封装。开发时经 Vite 代理，生产经 nginx 反代，均走同源 /api。

export type Source = "openlist" | "jellyfin";

export interface VideoItem {
  source: Source;
  name: string;
  path?: string; // OpenList：相对路径
  id?: string; // Jellyfin：条目 ID
  isDir: boolean;
  size?: number;
  modified?: string;
  poster?: string; // 海报/缩略图地址（已由后端代理）
  overview?: string;
  year?: number;
  streamUrl?: string;
}

export interface ListResp {
  source: Source;
  path?: string;
  items: VideoItem[];
}

export interface Health {
  status: string;
  sources: Source[];
  jellyfin: boolean;
}

// 查询启用了哪些来源（openlist / jellyfin）。
export async function getHealth(): Promise<Health> {
  const res = await fetch("/api/health");
  if (!res.ok) throw new Error(`健康检查失败: ${res.status}`);
  return res.json();
}

// ---- 资源库 / 按需转存 ----

export interface Resource {
  id: number;
  title: string;
  poster?: string;
  overview?: string;
  provider: string;
  shareUrl: string;
  sharePwd?: string;
  filePath?: string;
}

export async function getResources(): Promise<Resource[]> {
  const res = await fetch("/api/resources");
  if (!res.ok) throw new Error(`加载失败: ${res.status}`);
  return (await res.json()).items || [];
}

export async function addResource(r: Partial<Resource>): Promise<Resource> {
  const res = await fetch("/api/resources", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(r),
  });
  if (!res.ok) throw new Error((await res.json()).error || `添加失败: ${res.status}`);
  return res.json();
}

// ---- 分享浏览(只读列目录,不涉及转存/播放)----

export interface ShareEntry {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
}

// 通过分享链接+提取码浏览目录内容(匿名,只读)。
export async function browseShare(
  shareUrl: string,
  sharePwd = "",
  path = "",
  provider = "aliyun"
): Promise<ShareEntry[]> {
  const params = new URLSearchParams({ shareUrl, provider });
  if (sharePwd) params.set("sharePwd", sharePwd);
  if (path) params.set("path", path);
  const res = await fetch(`/api/share/browse?${params.toString()}`);
  if (!res.ok) throw new Error((await res.json()).error || `浏览失败: ${res.status}`);
  return (await res.json()).items || [];
}

export interface PlayResp {
  status: "uncached" | "transferring" | "ready" | "failed" | "cleaned";
  streamUrl?: string;
  type?: "hls" | "direct";
  message?: string;
}

// 触发/查询转存;就绪返回 streamUrl。
export async function playResource(id: number): Promise<PlayResp> {
  const res = await fetch(`/api/play?resource=${id}`);
  if (!res.ok) throw new Error(`播放请求失败: ${res.status}`);
  return res.json();
}

export interface StrmResult {
  total: number;
  written: number;
  removed: number;
  errors?: string[];
  mediaDir: string;
  siteUrl: string;
}

// 全量重建 strm 媒体库(给 Emby/Jellyfin 扫描)。
export async function generateStrm(): Promise<StrmResult> {
  const res = await fetch("/api/strm/generate", { method: "POST" });
  if (!res.ok) throw new Error((await res.json()).error || `生成失败: ${res.status}`);
  return res.json();
}

// ---- 网盘授权 / 设置 ----

export interface Provider {
  provider: string;
  name: string;
  authMethod: "qrcode" | "cookie" | "token";
  authorized: boolean;
}

// 保存某网盘凭据(阿里开放接口 refresh token / 115、夸克 cookie)。
export async function saveProviderToken(
  provider: string,
  token: string,
  extra?: string
): Promise<void> {
  const res = await fetch("/api/settings/token", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ provider, token, extra }),
  });
  if (!res.ok) throw new Error((await res.json()).error || `保存失败: ${res.status}`);
}

export async function getProviders(): Promise<Provider[]> {
  const res = await fetch("/api/settings/providers");
  if (!res.ok) throw new Error(`加载失败: ${res.status}`);
  return (await res.json()).providers;
}

export interface QRSession {
  t: string;
  ck: string;
  qrContent: string;
}

// 申请阿里云盘登录二维码。
export async function aliyunQR(): Promise<QRSession> {
  const res = await fetch("/api/auth/aliyun/qr", { method: "POST" });
  if (!res.ok) throw new Error(`申请二维码失败: ${res.status}`);
  return res.json();
}

// 轮询扫码状态：NEW / SCANED / CONFIRMED / EXPIRED / CANCELED。
export async function aliyunQRStatus(t: string, ck: string): Promise<string> {
  const res = await fetch("/api/auth/aliyun/qr/status", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ t, ck }),
  });
  if (!res.ok) throw new Error(`轮询失败: ${res.status}`);
  return (await res.json()).status;
}

// 列出视频。OpenList 传 path 做层级浏览；Jellyfin 忽略 path。
export async function listVideos(source: Source, path = "/"): Promise<ListResp> {
  const params = new URLSearchParams({ source });
  if (source === "openlist") params.set("path", path);
  const res = await fetch(`/api/videos?${params.toString()}`);
  if (!res.ok) throw new Error(`加载失败: ${res.status}`);
  return res.json();
}
