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

// ---- 网盘授权 / 设置 ----

export interface Provider {
  provider: string;
  name: string;
  authMethod: "qrcode" | "cookie";
  authorized: boolean;
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
