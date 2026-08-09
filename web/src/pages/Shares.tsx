import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ListPlus, Plus, RefreshCw, Trash2, X } from "lucide-react";
import {
  addShare,
  addSharesBatch,
  deleteShare,
  getShares,
  checkAllShares,
  importShare,
  type BatchShareResponse,
  type Share,
} from "../api";
import { detectShareProvider, parseShareBatch, validateShareDraft, type ShareDraft } from "../shareBatch";
import { getSharePreferences } from "../sharePreferences";

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
const BATCH_PROVIDERS = PROVIDERS.filter((p) => ["aliyun", "115", "quark"].includes(p.value));

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

const emptyForm = () => {
  const preferences = getSharePreferences();
  return { provider: preferences.defaultProvider, shareUrl: "", sharePwd: "", title: "", category: preferences.defaultCategory };
};

export default function Shares() {
  const [items, setItems] = useState<Share[]>([]);
  const [error, setError] = useState("");
  const [editor, setEditor] = useState<"single" | "batch" | null>(null);
  const [form, setForm] = useState(emptyForm);
  const [busy, setBusy] = useState(false);
  const [batchText, setBatchText] = useState("");
  const [batchCategory, setBatchCategory] = useState(() => getSharePreferences().defaultCategory);
  const [drafts, setDrafts] = useState<ShareDraft[]>([]);
  const [batchResult, setBatchResult] = useState<BatchShareResponse | null>(null);
  const [importing, setImporting] = useState<number | null>(null);
  const [msg, setMsg] = useState("");
  const [checking, setChecking] = useState(false);
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
      setForm(emptyForm());
      setEditor(null);
      load();
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setBusy(false);
    }
  };

  const parseBatch = () => {
    setError("");
    setBatchResult(null);
    const parsed = parseShareBatch(batchText, batchCategory.trim());
    if (parsed.length === 0) {
      setError("请先粘贴至少一条分享链接");
      return;
    }
    setDrafts(parsed);
  };

  const updateDraft = (index: number, patch: Partial<ShareDraft>) => {
    setDrafts((current) =>
      current.map((draft, i) => {
        if (i !== index) return draft;
        const next = { ...draft, ...patch };
        if (patch.shareUrl !== undefined) next.provider = detectShareProvider(next.shareUrl) || next.provider;
        next.error = validateShareDraft(next);
        return next;
      })
    );
  };

  const submitBatch = async () => {
    const invalid = drafts.find((draft) => draft.error);
    if (invalid) {
      setError("请先修正或删除标红的条目");
      return;
    }
    setBusy(true);
    setError("");
    setBatchResult(null);
    try {
      const result = await addSharesBatch(
        drafts.map(({ provider, shareUrl, sharePwd, title, category }) => ({
          provider,
          shareUrl: shareUrl.trim(),
          sharePwd: sharePwd.trim(),
          title: title.trim(),
          category: category.trim(),
        }))
      );
      setBatchResult(result);
      await load();
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setBusy(false);
    }
  };

  const closeEditor = () => {
    setEditor(null);
    setBatchText("");
    setDrafts([]);
    setBatchResult(null);
  };

  const remove = async (id: number) => {
    if (getSharePreferences().confirmDelete && !window.confirm("删除这个收藏的分享？")) return;
    setError("");
    try {
      await deleteShare(id);
      load();
    } catch (e) {
      setError(String((e as Error).message || e));
    }
  };

  const checkAll = async () => {
    setChecking(true);
    setError("");
    setMsg("");
    try {
      const result = await checkAllShares();
      setMsg(result.message || "检查已在后台开始，请稍后刷新分享库查看状态。");
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setChecking(false);
    }
  };

  return (
    <div>
      <div style={{ display: "flex", alignItems: "flex-end", gap: 12, flexWrap: "wrap" }}>
        <div className="page-head" style={{ marginRight: "auto", marginBottom: 0 }}>
          <h1>分享库</h1>
          <p>收藏各网盘的分享链接，随时浏览、转存。分享会失效，可跟踪有效性。</p>
        </div>
        <div className="share-actions">
          <button onClick={() => (editor === "single" ? closeEditor() : setEditor("single"))}>
            <Plus size={17} /> 收藏分享
          </button>
          <button className="primary" onClick={() => (editor === "batch" ? closeEditor() : setEditor("batch"))}>
            <ListPlus size={17} /> 批量收藏
          </button>
          <button onClick={checkAll} disabled={checking || items.length === 0}>
            <RefreshCw size={17} /> {checking ? "检查中…" : "检查可用性"}
          </button>
        </div>
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

      {editor === "single" && (
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

      {editor === "batch" && (
        <section className="batch-share-panel">
          <div className="batch-share-heading">
            <div>
              <h2>批量收藏</h2>
              <p>每行一条，支持直接粘贴包含名称、链接和提取码的分享文本。</p>
            </div>
            <button className="icon-button" title="关闭" aria-label="关闭批量收藏" onClick={closeEditor}>
              <X size={19} />
            </button>
          </div>

          {drafts.length === 0 ? (
            <div className="batch-share-paste">
              <textarea
                value={batchText}
                onChange={(event) => setBatchText(event.target.value)}
                placeholder={"示例：\n凡人修仙传 https://www.alipan.com/s/xxxx\nhttps://pan.quark.cn/s/xxxx 提取码：1234\nhttps://115.com/s/xxxx 8abc"}
                autoFocus
              />
              <div className="batch-share-paste-actions">
                <label>
                  默认分类
                  <input
                    value={batchCategory}
                    onChange={(event) => setBatchCategory(event.target.value)}
                    placeholder="可选"
                  />
                </label>
                <button className="primary" onClick={parseBatch} disabled={!batchText.trim()}>
                  解析并预览
                </button>
              </div>
            </div>
          ) : (
            <>
              <div className="batch-share-toolbar">
                <span>已识别 {drafts.length} 条</span>
                <button onClick={() => setDrafts([])}>返回修改原文</button>
              </div>
              <div className="batch-share-table-wrap">
                <div className="batch-share-row batch-share-row-head" aria-hidden="true">
                  <span>网盘</span><span>分享链接</span><span>提取码</span><span>名称</span><span>分类</span><span />
                </div>
                {drafts.map((draft, index) => (
                  <div className={`batch-share-row${draft.error ? " has-error" : ""}`} key={draft.key}>
                    <select value={draft.provider} onChange={(event) => updateDraft(index, { provider: event.target.value as ShareDraft["provider"] })}>
                      <option value="">请选择</option>
                      {BATCH_PROVIDERS.map((provider) => <option key={provider.value} value={provider.value}>{provider.label}</option>)}
                    </select>
                    <input value={draft.shareUrl} onChange={(event) => updateDraft(index, { shareUrl: event.target.value })} placeholder="分享链接" />
                    <input value={draft.sharePwd} onChange={(event) => updateDraft(index, { sharePwd: event.target.value })} placeholder="可选" />
                    <input value={draft.title} onChange={(event) => updateDraft(index, { title: event.target.value })} placeholder="可选" />
                    <input value={draft.category} onChange={(event) => updateDraft(index, { category: event.target.value })} placeholder="可选" />
                    <button className="icon-button" title="删除此条" aria-label="删除此条" onClick={() => setDrafts((current) => current.filter((_, i) => i !== index))}>
                      <Trash2 size={17} />
                    </button>
                    {draft.error && <div className="batch-share-error">{draft.error}</div>}
                  </div>
                ))}
              </div>
              <div className="batch-share-submit">
                <span className="muted">提交后会自动合并已经收藏的相同链接。</span>
                <button className="primary" onClick={submitBatch} disabled={busy || drafts.length === 0 || drafts.some((draft) => Boolean(draft.error))}>
                  {busy ? "收藏中…" : `收藏 ${drafts.length} 条`}
                </button>
              </div>
            </>
          )}

          {batchResult && (
            <div className="batch-share-result">
              <strong>处理完成</strong>
              <span>新增 {batchResult.added} 条</span>
              <span>重复 {batchResult.duplicates} 条</span>
              <span>失败 {batchResult.failed} 条</span>
              {batchResult.failed > 0 && batchResult.results.filter((result) => result.status === "failed").map((result) => (
                <div key={result.index}>{result.shareUrl || `第 ${result.index + 1} 条`}：{result.message}</div>
              ))}
            </div>
          )}
        </section>
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
                      navigate("/settings/shares/browse", {
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
