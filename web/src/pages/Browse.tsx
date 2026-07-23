import { useEffect, useState } from "react";
import { useLocation } from "react-router-dom";
import { browseShare, importShare, saveShareItem, type ShareEntry } from "../api";

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
  const [targetFolder, setTargetFolder] = useState("ivideo"); // 转存目标目录
  const [saving, setSaving] = useState(""); // 正在转存的条目 path
  const [saved, setSaved] = useState<Record<string, string>>({}); // path -> 状态文案
  const [importMsg, setImportMsg] = useState("");
  const [importingDir, setImportingDir] = useState(false);

  // 把【当前目录】递归导入成资源(不转存),用于精简导入某个子目录而非整个分享。
  const importDir = async () => {
    setImportingDir(true);
    setImportMsg("");
    setError("");
    try {
      const r = await importShare(shareUrl.trim(), sharePwd.trim(), "aliyun", path);
      setImportMsg(`✅ 导入 ${r.added} 个视频（跳过 ${r.skipped} 个已存在）`);
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setImportingDir(false);
    }
  };

  const save = async (e: ShareEntry) => {
    setSaving(e.path);
    setSaved((s) => ({ ...s, [e.path]: "" }));
    try {
      await saveShareItem({
        shareUrl: shareUrl.trim(),
        sharePwd: sharePwd.trim(),
        path: e.path,
        targetFolder: targetFolder.trim() || "ivideo",
      });
      setSaved((s) => ({ ...s, [e.path]: "✅ 已转存" }));
    } catch (err) {
      setSaved((s) => ({ ...s, [e.path]: "❌ " + String((err as Error).message || err) }));
    } finally {
      setSaving("");
    }
  };

  const list = async (p: string, url = shareUrl, pwd = sharePwd) => {
    if (!url.trim()) {
      setError("请先填分享链接");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const entries = await browseShare(url.trim(), pwd.trim(), p);
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

  // 从「分享库」点「浏览」跳转过来：预填链接/提取码并自动加载。
  const location = useLocation();
  useEffect(() => {
    const st = location.state as { shareUrl?: string; sharePwd?: string } | null;
    if (st?.shareUrl) {
      setShareUrl(st.shareUrl);
      setSharePwd(st.sharePwd || "");
      list("", st.shareUrl, st.sharePwd || "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div>
      <div className="page-head">
        <h1>分享浏览</h1>
        <p>用分享链接 + 提取码浏览网盘目录（只读），可手动转存到自己的盘。</p>
      </div>

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
        <button className="primary" onClick={() => list("")} disabled={loading}>
          {loading ? "加载中…" : "浏览"}
        </button>
      </div>

      {error && <p style={{ color: "#f87171" }}>出错了: {error}</p>}

      {opened && (
        <>
          <div className="breadcrumb" style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
            <span>
              当前目录: {path || "/"}{" "}
              {path && (
                <a onClick={goUp} style={{ cursor: "pointer", color: "var(--accent)" }}>
                  ⬆ 返回上级
                </a>
              )}
            </span>
            <span style={{ marginLeft: "auto", display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
              <button className="primary" onClick={importDir} disabled={importingDir}>
                {importingDir ? "导入中…" : "导入此目录到资源库"}
              </button>
              <span style={{ display: "flex", alignItems: "center", gap: 6 }}>
                转存到我的阿里盘:
                <input
                  value={targetFolder}
                  onChange={(e) => setTargetFolder(e.target.value)}
                  className="token-input"
                  style={{ flex: "0 0 160px" }}
                  placeholder="ivideo"
                />
              </span>
            </span>
          </div>
          {importMsg && (
            <p style={{ color: "#86efac", margin: "4px 0 12px" }}>{importMsg}</p>
          )}
          {items.length === 0 && !loading && <p className="muted">这个目录是空的。</p>}
          <div className="browse-list">
            {items.map((it) => (
              <div key={it.path} className="browse-row">
                <span
                  className="browse-icon"
                  onClick={() => it.isDir && list(it.path)}
                  style={{ cursor: it.isDir ? "pointer" : "default" }}
                >
                  {it.isDir ? "📁" : "🎬"}
                </span>
                <span
                  className="browse-name"
                  onClick={() => it.isDir && list(it.path)}
                  style={{ cursor: it.isDir ? "pointer" : "default" }}
                >
                  {it.name}
                </span>
                <span className="muted browse-size">
                  {it.isDir ? "目录" : humanSize(it.size)}
                </span>
                {saved[it.path] ? (
                  <span className="browse-size" style={{ fontSize: 12 }}>{saved[it.path]}</span>
                ) : (
                  <button
                    className="tab"
                    style={{ padding: "3px 10px", fontSize: 12 }}
                    disabled={saving === it.path}
                    onClick={() => save(it)}
                  >
                    {saving === it.path ? "转存中…" : "转存"}
                  </button>
                )}
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
}
