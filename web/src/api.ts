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

// 列出视频。OpenList 传 path 做层级浏览；Jellyfin 忽略 path。
export async function listVideos(source: Source, path = "/"): Promise<ListResp> {
  const params = new URLSearchParams({ source });
  if (source === "openlist") params.set("path", path);
  const res = await fetch(`/api/videos?${params.toString()}`);
  if (!res.ok) throw new Error(`加载失败: ${res.status}`);
  return res.json();
}
