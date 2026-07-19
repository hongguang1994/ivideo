import { Link } from "react-router-dom";
import type { VideoItem } from "../api";

// 格式化文件大小
function humanSize(n: number): string {
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
        {item.thumb ? (
          <img className="thumb" src={item.thumb} alt={item.name} />
        ) : item.isDir ? (
          "📁"
        ) : (
          "🎬"
        )}
      </div>
      <div className="meta">
        <div className="title">{item.name}</div>
        <div className="sub">
          {item.isDir ? "目录" : humanSize(item.size)}
        </div>
      </div>
    </>
  );

  if (item.isDir) {
    return (
      <div className="card" onClick={() => onOpenDir(item.path)} style={{ cursor: "pointer" }}>
        {inner}
      </div>
    );
  }

  return (
    <Link className="card" to={`/watch?path=${encodeURIComponent(item.path)}`}>
      {inner}
    </Link>
  );
}
