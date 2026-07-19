import { useEffect, useState } from "react";
import { listVideos, type VideoItem } from "../api";
import VideoCard from "../components/VideoCard";

export default function Home() {
  const [path, setPath] = useState("/");
  const [items, setItems] = useState<VideoItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    setLoading(true);
    setError("");
    listVideos(path)
      .then((r) => setItems(r.items))
      .catch((e) => setError(String(e.message || e)))
      .finally(() => setLoading(false));
  }, [path]);

  const goUp = () => {
    if (path === "/" || path === "") return;
    const parent = path.replace(/\/[^/]+\/?$/, "") || "/";
    setPath(parent);
  };

  return (
    <div>
      <div className="breadcrumb">
        当前目录: {path}{" "}
        {path !== "/" && (
          <a onClick={goUp} style={{ cursor: "pointer", color: "var(--accent)" }}>
            ⬆ 返回上级
          </a>
        )}
      </div>

      {loading && <p className="muted">加载中…</p>}
      {error && <p style={{ color: "#f87171" }}>出错了: {error}</p>}
      {!loading && !error && items.length === 0 && (
        <p className="muted">这个目录暂时没有视频。请在 OpenList 中挂载网盘并放入视频文件。</p>
      )}

      <div className="grid">
        {items.map((it) => (
          <VideoCard key={it.path} item={it} onOpenDir={setPath} />
        ))}
      </div>
    </div>
  );
}
