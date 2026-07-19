import { Link } from "react-router-dom";
import type { VideoItem } from "../api";

// 格式化文件大小
function humanSize(n?: number): string {
  if (!n) return "";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(1)} ${units[i]}`;
}

// 根据条目构造播放页链接（区分 openlist / jellyfin）。
function watchLink(item: VideoItem): string {
  const params = new URLSearchParams({ source: item.source, name: item.name });
  if (item.source === "jellyfin" && item.id) params.set("id", item.id);
  if (item.source === "openlist" && item.path) params.set("path", item.path);
  return `/watch?${params.toString()}`;
}

export default function VideoCard({
  item,
  onOpenDir,
}: {
  item: VideoItem;
  onOpenDir: (path: string) => void;
}) {
  const inner = (
    <>
      <div className="thumb">
        {item.poster ? (
          <img className="thumb" src={item.poster} alt={item.name} loading="lazy" />
        ) : item.isDir ? (
          "📁"
        ) : (
          "🎬"
        )}
      </div>
      <div className="meta">
        <div className="title">{item.name}</div>
        <div className="sub">
          {item.isDir
            ? "目录"
            : item.source === "jellyfin"
              ? item.year || "影片"
              : humanSize(item.size)}
        </div>
      </div>
    </>
  );

  // OpenList 目录：点击进入下一层
  if (item.isDir && item.path) {
    return (
      <div className="card" onClick={() => onOpenDir(item.path!)} style={{ cursor: "pointer" }}>
        {inner}
      </div>
    );
  }

  return (
    <Link className="card" to={watchLink(item)}>
      {inner}
    </Link>
  );
}
