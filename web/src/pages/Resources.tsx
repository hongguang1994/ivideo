import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { addResource, generateStrm, getResources, type Resource } from "../api";

export default function Resources() {
  const [items, setItems] = useState<Resource[]>([]);
  const [error, setError] = useState("");
  const [strmMsg, setStrmMsg] = useState("");
  const [show, setShow] = useState(false);
  const [form, setForm] = useState({
    title: "",
    provider: "aliyun",
    shareUrl: "",
    sharePwd: "",
    filePath: "",
  });

  const load = () => {
    getResources()
      .then(setItems)
      .catch((e) => setError(String(e.message || e)));
  };
  useEffect(load, []);

  const doGenerateStrm = async () => {
    setError("");
    setStrmMsg("生成中…");
    try {
      const r = await generateStrm();
      setStrmMsg(
        `✅ 已生成 ${r.written}/${r.total} 个 strm,清理 ${r.removed} 个 · 目录 ${r.mediaDir} · 指向 ${r.siteUrl}`
      );
    } catch (e) {
      setStrmMsg("");
      setError(String((e as Error).message || e));
    }
  };

  const submit = async () => {
    setError("");
    try {
      await addResource(form);
      setShow(false);
      setForm({ title: "", provider: "aliyun", shareUrl: "", sharePwd: "", filePath: "" });
      load();
    } catch (e) {
      setError(String((e as Error).message || e));
    }
  };

  return (
    <div>
      <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
        <h2 style={{ marginRight: "auto" }}>资源库 · 按需转存</h2>
        <button className="tab" onClick={doGenerateStrm}>
          生成 strm
        </button>
        <button className="tab" onClick={() => setShow((s) => !s)}>
          {show ? "取消" : "+ 添加资源"}
        </button>
      </div>
      {error && <p style={{ color: "#f87171" }}>出错了: {error}</p>}
      {strmMsg && <p className="muted" style={{ fontSize: 13 }}>{strmMsg}</p>}

      {show && (
        <div className="add-form">
          <input placeholder="标题" value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} />
          <select value={form.provider} onChange={(e) => setForm({ ...form, provider: e.target.value })}>
            <option value="aliyun">阿里云盘</option>
            <option value="115">115</option>
            <option value="quark">夸克</option>
          </select>
          <input placeholder="分享链接" value={form.shareUrl} onChange={(e) => setForm({ ...form, shareUrl: e.target.value })} />
          <input placeholder="提取码(可选)" value={form.sharePwd} onChange={(e) => setForm({ ...form, sharePwd: e.target.value })} />
          <input placeholder="分享内文件路径 如 /目录/片.mp4" value={form.filePath} onChange={(e) => setForm({ ...form, filePath: e.target.value })} />
          <button className="tab active" onClick={submit}>保存</button>
        </div>
      )}

      {items.length === 0 && <p className="muted">还没有资源。点「添加资源」贴一个网盘分享链接试试。</p>}
      <div className="grid">
        {items.map((r) => (
          <Link key={r.id} className="card" to={`/watch?rid=${r.id}`}>
            <div className="thumb">🎬</div>
            <div className="meta">
              <div className="title">{r.title}</div>
              <div className="sub">{r.provider}</div>
            </div>
          </Link>
        ))}
      </div>
    </div>
  );
}
