// 后端 API 封装。开发时经 Vite 代理，生产经 nginx 反代，均走同源 /api。

export interface VideoItem {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modified: string;
  thumb: string;
  streamUrl?: string;
}

export interface ListResp {
  path: string;
  items: VideoItem[];
}

export async function listVideos(path = "/"): Promise<ListResp> {
  const res = await fetch(`/api/videos?path=${encodeURIComponent(path)}`);
  if (!res.ok) {
    throw new Error(`加载失败: ${res.status}`);
  }
  return res.json();
}
