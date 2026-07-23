import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { addShare, deleteShare, getShares, importShare, type Share } from "../api";

const PROVIDERS = [
  { value: "aliyun", label: "阿里云盘" },
  { value: "115", label: "115" },
  { value: "quark", label: "夸克" },
  { value: "pikpak", label: "PikPak" },
  { value: "thunder", label: "迅雷" },
];
const PROVIDER_LABEL: Record<string, string> = Object.fromEntries(
  PROVIDERS.map((p) => [p.value, p.label])
);

const STATUS: Record<string, { label: string; cls: string }> = {
  valid: { label: "有效", cls: "badge-ok" },
  invalid: { label: "失效", cls: "badge-bad" },
  unknown: { label: "未校验", cls: "badge-off" },
};

function fmtSize(bytes: number): string {
  if (!bytes) return "";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let n = bytes;
  let i = 0;
  while (n >= 1024 && i < u.length - 1) {
    n /= 1024;
    i++;
  }
  return n.toFixed(i > 0 && n < 10 ? 1 : 0) + " " + u[i];
}

const emptyForm = { provider: "aliyun", shareUrl: "", sharePwd: "", title: "", category: "" };

export default function Shares() {
  const [items, setItems] = useState<Share[]>([]);
  const [error, setError] = useState("");
  const [show, setShow] = useState(false);
  const [form, setForm] = useState({ ...emptyForm });
  const [busy, setBusy] = useState(false);
  const [importing, setImporting] = useState<number | null>(null);
  const [msg, setMsg] = useState("");
  const navigate = useNavigate();

  const doImport = async (s: Share) => {
    setImporting(s.id);
    setError("");
    setMsg("");
    try {
      const r = await importShare(s.shareUrl, s.sharePwd, s.provider);
      setMsg(`✅ 「${s.title || s.shareId || s.shareUrl}」导入 ${r.added} 个视频（跳过 ${r.skipped} 个已存在）`);
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setImporting(null);
    }
  };

  const load = () =>
    getShares()
      .then(setItems)
      .catch((e) => setError(String(e.message || e)));

  useEffect(() => {
    load();
  }, []);

  const submit = async () => {
    setError("");
    setBusy(true);
    try {
      await addShare(form);
      setForm({ ...emptyForm });
      setShow(false);
      load();
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: number) => {
    if (!window.confirm("删除这个收藏的分享？")) return;
    setError("");
    try {
      await deleteShare(id);
      load();
    } catch (e) {
      setError(String((e as Error).message || e));
    }
  };

  return (
    <div>
      <div style={{ display: "flex", alignItems: "flex-end", gap: 12, flexWrap: "wrap" }}>
        <div className="page-head" style={{ marginRight: "auto", marginBottom: 0 }}>
          <h1>分享库</h1>
          <p>收藏各网盘的分享链接，随时浏览、转存。分享会失效，可跟踪有效性。</p>
        </div>
        <button className="primary" onClick={() => setShow((s) => !s)}>
          {show ? "取消" : "+ 收藏分享"}
        </button>
      </div>

      {error && (
        <div
          className="panel"
          style={{ borderColor: "rgba(248,113,113,.4)", margin: "16px 0", color: "#fca5a5" }}
        >
          出错了: {error}
        </div>
      )}
      {msg && (
        <div
          className="panel"
          style={{ borderColor: "rgba(74,222,128,.4)", margin: "16px 0", color: "#86efac" }}
        >
          {msg}
        </div>
      )}

      {show && (
        <div className="add-form">
          <select
            value={form.provider}
            onChange={(e) => setForm({ ...form, provider: e.target.value })}
            style={{ flex: "0 0 140px" }}
          >
            {PROVIDERS.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
          </select>
          <input
            placeholder="分享链接 如 https://www.alipan.com/s/xxxx"
            value={form.shareUrl}
            onChange={(e) => setForm({ ...form, shareUrl: e.target.value })}
            style={{ flex: "2 1 280px" }}
          />
          <input
            placeholder="提取码(可选)"
            value={form.sharePwd}
            onChange={(e) => setForm({ ...form, sharePwd: e.target.value })}
            style={{ flex: "0 1 120px" }}
          />
          <input
            placeholder="名称(可选)"
            value={form.title}
            onChange={(e) => setForm({ ...form, title: e.target.value })}
            style={{ flex: "1 1 140px" }}
          />
          <input
            placeholder="分类(可选)"
            value={form.category}
            onChange={(e) => setForm({ ...form, category: e.target.value })}
            style={{ flex: "0 1 120px" }}
          />
          <button className="primary" onClick={submit} disabled={busy || !form.shareUrl.trim()}>
            {busy ? "收藏中…" : "收藏"}
          </button>
        </div>
      )}

      {items.length === 0 ? (
        <div className="panel muted" style={{ marginTop: 16 }}>
          还没有收藏的分享。点「收藏分享」贴一个网盘分享链接。
        </div>
      ) : (
        <div className="provider-list" style={{ maxWidth: 900, marginTop: 16 }}>
          {items.map((s) => {
            const st = STATUS[s.status] || STATUS.unknown;
            return (
              <div key={s.id} className="provider-row">
                <div style={{ flex: "1 1 auto", minWidth: 0 }}>
                  <div
                    className="provider-name"
                    style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}
                  >
                    <span>{s.title || s.shareId || s.shareUrl}</span>
                    <span className={`badge ${st.cls}`}>● {st.label}</span>
                    {s.category && <span className="badge badge-off">{s.category}</span>}
                  </div>
                  <div className="sub" style={{ marginTop: 4, wordBreak: "break-all" }}>
                    {PROVIDER_LABEL[s.provider] || s.provider}
                    {" · "}
                    <a
                      href={s.shareUrl}
                      target="_blank"
                      rel="noreferrer"
                      style={{ color: "var(--accent-2)" }}
                    >
                      {s.shareUrl}
                    </a>
                    {s.sharePwd && ` · 提取码 ${s.sharePwd}`}
                    {s.fileCount > 0 && ` · ${s.fileCount} 项`}
                    {s.totalSize > 0 && ` · ${fmtSize(s.totalSize)}`}
                  </div>
                </div>
                <div style={{ display: "flex", gap: 8 }}>
                  <button onClick={() => doImport(s)} disabled={importing === s.id}>
                    {importing === s.id ? "导入中…" : "导入到资源库"}
                  </button>
                  <button
                    className="primary"
                    onClick={() =>
                      navigate("/browse", {
                        state: { shareUrl: s.shareUrl, sharePwd: s.sharePwd, provider: s.provider },
                      })
                    }
                  >
                    浏览
                  </button>
                  <button onClick={() => remove(s.id)}>删除</button>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
