// 后端 API 封装。所有接口统一前缀 /api/v1，统一返回结构 {code, msg, data}。
// 开发时经 Vite 代理，生产经 nginx 反代。

const BASE = "/api/v1";

// apiFetch 统一发请求并拆包：成功返回 data，失败抛出 msg。
async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, init);
  let body: { code?: number; msg?: string; data?: unknown } = {};
  try {
    body = await res.json();
  } catch {
    // 非 JSON 响应
  }
  if (!res.ok) {
    throw new Error(body.msg || `请求失败: ${res.status}`);
  }
  return body.data as T;
}

// post 是带 JSON body 的 POST 简写。
function post<T>(path: string, data?: unknown): Promise<T> {
  return apiFetch<T>(path, {
    method: "POST",
    headers: data !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: data !== undefined ? JSON.stringify(data) : undefined,
  });
}

// ---- 直读源(OpenList / Jellyfin)----

export type Source = "openlist" | "jellyfin";

export interface VideoItem {
  source: Source;
  name: string;
  path?: string;
  id?: string;
  isDir: boolean;
  size?: number;
  modified?: string;
  poster?: string;
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

export function getHealth(): Promise<Health> {
  return apiFetch<Health>("/health");
}

// 列出视频。OpenList 传 path 做层级浏览；Jellyfin 忽略 path。
export function listVideos(source: Source, path = "/"): Promise<ListResp> {
  const params = new URLSearchParams({ source });
  if (source === "openlist") params.set("path", path);
  return apiFetch<ListResp>(`/videos?${params.toString()}`);
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
  const d = await apiFetch<{ items: Resource[] }>("/resources");
  return d.items || [];
}

export function addResource(r: Partial<Resource>): Promise<Resource> {
  return post<Resource>("/resources", r);
}

export interface PlayResp {
  status: "uncached" | "transferring" | "ready" | "failed" | "cleaned";
  streamUrl?: string;
  type?: "hls" | "direct";
  message?: string;
}

// 触发/查询转存;就绪返回 streamUrl。
export function playResource(id: number): Promise<PlayResp> {
  return apiFetch<PlayResp>(`/play?resource=${id}`);
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
export function generateStrm(): Promise<StrmResult> {
  return post<StrmResult>("/strm/generate");
}

// ---- 分享浏览(只读列目录,不涉及转存/播放)----

export interface ShareEntry {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
}

export async function browseShare(
  shareUrl: string,
  sharePwd = "",
  path = "",
  provider = "aliyun"
): Promise<ShareEntry[]> {
  const params = new URLSearchParams({ shareUrl, provider });
  if (sharePwd) params.set("sharePwd", sharePwd);
  if (path) params.set("path", path);
  const d = await apiFetch<{ items: ShareEntry[] }>(`/share/browse?${params.toString()}`);
  return d.items || [];
}

// 手动转存分享内某文件/文件夹到自己阿里盘的指定目录(默认 ivideo,永久留存)。
export function saveShareItem(args: {
  shareUrl: string;
  sharePwd?: string;
  path: string;
  targetFolder?: string;
  provider?: string;
}): Promise<unknown> {
  return post("/share/save", args);
}

// ---- 网盘授权 / 设置 ----

export interface Provider {
  provider: string;
  name: string;
  authMethod: "qrcode" | "cookie" | "token";
  authorized: boolean;
}

export async function getProviders(): Promise<Provider[]> {
  const d = await apiFetch<{ providers: Provider[] }>("/settings/providers");
  return d.providers;
}

// 保存某网盘凭据(阿里开放接口 refresh token / 115、夸克 cookie)。
export function saveProviderToken(provider: string, token: string, extra?: string): Promise<unknown> {
  return post("/settings/token", { provider, token, extra });
}

export interface QRSession {
  t: string;
  ck: string;
  qrContent: string;
}

// 申请阿里云盘登录二维码。
export function aliyunQR(): Promise<QRSession> {
  return post<QRSession>("/auth/aliyun/qr");
}

// 轮询扫码状态：NEW / SCANED / CONFIRMED / EXPIRED / CANCELED。
export async function aliyunQRStatus(t: string, ck: string): Promise<string> {
  const d = await post<{ status: string }>("/auth/aliyun/qr/status", { t, ck });
  return d.status;
}
