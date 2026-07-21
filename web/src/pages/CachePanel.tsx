import { useEffect, useState } from "react";
import { evictCache, getCacheItems, type CacheEntry } from "../api";

function fmtSize(bytes: number): string {
  if (bytes < 1024) return bytes + " B";
  const u = ["KB", "MB", "GB", "TB"];
  let n = bytes / 1024;
  let i = 0;
  while (n >= 1024 && i < u.length - 1) {
    n /= 1024;
    i++;
  }
  return n.toFixed(n < 10 ? 2 : 1) + " " + u[i];
}

function fmtTime(unix: number): string {
  if (!unix) return "—";
  const d = new Date(unix * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getMonth() + 1}-${d.getDate()} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

function ago(unix: number): string {
  if (!unix) return "";
  const s = Math.floor(Date.now() / 1000) - unix;
  if (s < 60) return "刚刚";
  if (s < 3600) return Math.floor(s / 60) + " 分钟前";
  if (s < 86400) return Math.floor(s / 3600) + " 小时前";
  return Math.floor(s / 86400) + " 天前";
}

export default function CachePanel() {
  const [items, setItems] = useState<CacheEntry[]>([]);
  const [totalBytes, setTotalBytes] = useState(0);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<number | null>(null);

  const load = () =>
    getCacheItems()
      .then((d) => {
        setItems(d.items || []);
        setTotalBytes(d.totalBytes || 0);
      })
      .catch((e) => setError(String(e.message || e)));

  useEffect(() => {
    load();
  }, []);

  const remove = async (id: number) => {
    if (!window.confirm("确认删除这个缓存？会从你网盘删除该文件。")) return;
    setBusy(id);
    setError("");
    try {
      await evictCache(id);
      await load();
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setBusy(null);
    }
  };

  return (
    <div>
      <div className="page-head">
        <h1>缓存管理</h1>
        <p>已转存进你网盘的资源。闲置会自动即删，也可在此手动删除释放空间。</p>
      </div>

      {error && (
        <div
          className="panel"
          style={{ borderColor: "rgba(248,113,113,.4)", marginBottom: 16, color: "#fca5a5" }}
        >
          出错了: {error}
        </div>
      )}

      <div style={{ display: "flex", gap: 14, marginBottom: 22, flexWrap: "wrap" }}>
        <div className="panel" style={{ flex: "1 1 180px" }}>
          <div className="sub">已缓存</div>
          <div style={{ fontSize: 26, fontWeight: 750, marginTop: 4 }}>
            {items.length} <span className="sub">项</span>
          </div>
        </div>
        <div className="panel" style={{ flex: "1 1 180px" }}>
          <div className="sub">占用空间</div>
          <div style={{ fontSize: 26, fontWeight: 750, marginTop: 4 }}>{fmtSize(totalBytes)}</div>
        </div>
      </div>

      {items.length === 0 ? (
        <div className="panel muted">
          暂无缓存 —— 播放视频时会自动转存进你的网盘，已缓存的项会显示在这里。
        </div>
      ) : (
        <div className="provider-list" style={{ maxWidth: 840 }}>
          {items.map((it) => (
            <div key={it.resourceId} className="provider-row">
              <div style={{ flex: "1 1 auto", minWidth: 0 }}>
                <div
                  className="provider-name"
                  style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}
                >
                  {it.title || `资源 #${it.resourceId}`}
                </div>
                <div className="sub" style={{ marginTop: 4 }}>
                  {fmtSize(it.size)} · 上次观看 {ago(it.lastAccess)}（{fmtTime(it.lastAccess)}）
                </div>
              </div>
              <button onClick={() => remove(it.resourceId)} disabled={busy === it.resourceId}>
                {busy === it.resourceId ? "删除中…" : "删除"}
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
