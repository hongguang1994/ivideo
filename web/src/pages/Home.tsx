import { useEffect, useState } from "react";
import { getHealth, listVideos, type Source, type VideoItem } from "../api";
import VideoCard from "../components/VideoCard";

const SOURCE_LABEL: Record<Source, string> = {
  openlist: "网盘文件",
  jellyfin: "Jellyfin 影库",
};

export default function Home() {
  const [sources, setSources] = useState<Source[]>(["openlist"]);
  const [source, setSource] = useState<Source>("openlist");
  const [path, setPath] = useState("/");
  const [items, setItems] = useState<VideoItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  // 启动时查询启用了哪些来源。
  useEffect(() => {
    getHealth()
      .then((h) => setSources(h.sources))
      .catch(() => setSources(["openlist"]));
  }, []);

  // 来源或路径变化时加载列表。
  useEffect(() => {
    setLoading(true);
    setError("");
    listVideos(source, path)
      .then((r) => setItems(r.items))
      .catch((e) => setError(String(e.message || e)))
      .finally(() => setLoading(false));
  }, [source, path]);

  const switchSource = (s: Source) => {
    setSource(s);
    setPath("/"); // 切源时重置目录
  };

  const goUp = () => {
    if (path === "/" || path === "") return;
    setPath(path.replace(/\/[^/]+\/?$/, "") || "/");
  };

  return (
    <div>
      {/* 来源切换标签（只有多于一个源时才显示） */}
      {sources.length > 1 && (
        <div className="tabs">
          {sources.map((s) => (
            <button
              key={s}
              className={`tab ${s === source ? "active" : ""}`}
              onClick={() => switchSource(s)}
            >
              {SOURCE_LABEL[s]}
            </button>
          ))}
        </div>
      )}

      {source === "openlist" && (
        <div className="breadcrumb">
          当前目录: {path}{" "}
          {path !== "/" && (
            <a onClick={goUp} style={{ cursor: "pointer", color: "var(--accent)" }}>
              ⬆ 返回上级
            </a>
          )}
        </div>
      )}

      {loading && <p className="muted">加载中…</p>}
      {error && <p style={{ color: "#f87171" }}>出错了: {error}</p>}
      {!loading && !error && items.length === 0 && (
        <p className="muted">
          {source === "jellyfin"
            ? "Jellyfin 影库暂时没有内容。请先在 Jellyfin 中建立媒体库并完成刮削。"
            : "这个目录暂时没有视频。请在 OpenList 中挂载网盘并放入视频文件。"}
        </p>
      )}

      <div className="grid">
        {items.map((it) => (
          <VideoCard key={it.id || it.path} item={it} onOpenDir={setPath} />
        ))}
      </div>
    </div>
  );
}
