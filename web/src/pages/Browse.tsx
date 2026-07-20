import { useState } from "react";
import { browseShare, type ShareEntry } from "../api";

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

export default function Browse() {
  const [shareUrl, setShareUrl] = useState("");
  const [sharePwd, setSharePwd] = useState("");
  const [path, setPath] = useState(""); // 当前所在的分享内路径
  const [items, setItems] = useState<ShareEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [opened, setOpened] = useState(false);

  const list = async (p: string) => {
    if (!shareUrl.trim()) {
      setError("请先填分享链接");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const entries = await browseShare(shareUrl.trim(), sharePwd.trim(), p);
      setItems(entries);
      setPath(p);
      setOpened(true);
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setLoading(false);
    }
  };

  const goUp = () => {
    const parent = path.replace(/\/[^/]+\/?$/, "");
    list(parent);
  };

  return (
    <div>
      <h2>分享浏览器</h2>
      <p className="muted" style={{ fontSize: 13 }}>
        用分享链接 + 提取码浏览目录内容(只读,不下载/不转存/不播放)。
      </p>

      <div className="add-form">
        <input
          placeholder="分享链接 如 https://www.alipan.com/s/xxxxx"
          value={shareUrl}
          onChange={(e) => setShareUrl(e.target.value)}
          style={{ flex: "1 1 320px" }}
        />
        <input
          placeholder="提取码(没有可留空)"
          value={sharePwd}
          onChange={(e) => setSharePwd(e.target.value)}
          style={{ flex: "0 1 160px" }}
        />
        <button className="tab active" onClick={() => list("")} disabled={loading}>
          {loading ? "加载中…" : "浏览"}
        </button>
      </div>

      {error && <p style={{ color: "#f87171" }}>出错了: {error}</p>}

      {opened && (
        <>
          <div className="breadcrumb">
            当前目录: {path || "/"}{" "}
            {path && (
              <a onClick={goUp} style={{ cursor: "pointer", color: "var(--accent)" }}>
                ⬆ 返回上级
              </a>
            )}
          </div>
          {items.length === 0 && !loading && <p className="muted">这个目录是空的。</p>}
          <div className="browse-list">
            {items.map((it) => (
              <div
                key={it.path}
                className="browse-row"
                onClick={() => it.isDir && list(it.path)}
                style={{ cursor: it.isDir ? "pointer" : "default" }}
              >
                <span className="browse-icon">{it.isDir ? "📁" : "🎬"}</span>
                <span className="browse-name">{it.name}</span>
                <span className="muted browse-size">
                  {it.isDir ? "目录" : humanSize(it.size)}
                </span>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
