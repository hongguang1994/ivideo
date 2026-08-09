import { CheckSquare, Code2, ExternalLink, RefreshCw, Search, Square } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import {
  addSharesBatch,
  getGitHubResources,
  getGitHubSources,
  syncGitHubResources,
  type BatchShareResponse,
  type GitHubResourceSource,
  type SearchResource,
} from "../api";

const PROVIDER_LABEL: Record<string, string> = {
  aliyun: "阿里云盘",
  "115": "115 网盘",
  quark: "夸克网盘",
};

function resourceKey(item: SearchResource, index: number) {
  return `${item.shareUrl}\0${item.fileName || ""}\0${item.path}\0${index}`;
}

export default function GitHubResources() {
  const [sources, setSources] = useState<GitHubResourceSource[]>([]);
  const [items, setItems] = useState<SearchResource[]>([]);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [filter, setFilter] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<BatchShareResponse | null>(null);
  const [syncing, setSyncing] = useState(false);

  useEffect(() => {
    getGitHubSources().then((data) => setSources(data.items || [])).catch((e) => setError(String((e as Error).message || e)));
  }, []);

  useEffect(() => {
    setLoading(true);
    getGitHubResources(page, 100)
      .then((data) => {
        setItems(data.items || []);
        setTotal(data.total || 0);
        setSelected(new Set((data.items || []).map(resourceKey)));
      })
      .catch((e) => setError(String((e as Error).message || e)))
      .finally(() => setLoading(false));
  }, [page]);

  const visibleItems = useMemo(() => {
    const query = filter.trim().toLowerCase();
    if (!query) return items;
    return items.filter((item) => [item.title, item.fileName, item.resourceType, item.shareUrl, item.repository].some((value) => value?.toLowerCase().includes(query)));
  }, [filter, items]);

  const toggle = (item: SearchResource, index: number) => {
    const key = resourceKey(item, index);
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const collect = async () => {
    setSaving(true);
    setError("");
    setResult(null);
    try {
      const saved = await addSharesBatch(items.filter((item, index) => selected.has(resourceKey(item, index))).map((item) => ({
        provider: item.provider,
        shareUrl: item.shareUrl,
        sharePwd: item.sharePwd,
        title: item.title,
      })));
      setResult(saved);
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setSaving(false);
    }
  };

  const syncAll = async () => {
    setSyncing(true);
    setError("");
    try {
      const synced = await syncGitHubResources();
      setResult({ added: synced.added, duplicates: synced.existing, failed: 0, results: [] });
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setSyncing(false);
    }
  };

  return (
    <div className="github-sources-page">
      <div className="page-head">
        <div>
          <h1>GitHub 资源</h1>
          <p>自动读取已接入 GitHub 资源源中的全部网盘分享资源。</p>
        </div>
        <button className="primary" onClick={syncAll} disabled={syncing || loading}>
          <RefreshCw size={17} /> {syncing ? "同步中…" : "同步全部入库"}
        </button>
      </div>

      {error && <div className="settings-notice notice-error">{error}</div>}
      {result && <div className="settings-notice notice-success">同步完成：新增 {result.added} 条，已存在 {result.duplicates} 条，失败 {result.failed} 条。</div>}

      <section className="github-source-list" aria-label="GitHub 资源源列表">
        {sources.map((source) => (
          <article className="github-source-card" key={source.id}>
            <div className="github-source-icon"><Code2 size={24} /></div>
            <div className="github-source-body">
              <div className="github-source-title"><strong>{source.name}</strong><span className="badge badge-ok">已接入</span></div>
              <div className="github-source-repository">{source.repository}</div>
              <p>{source.description}</p>
              <div className="github-source-meta">解析方式：{source.parser}</div>
            </div>
            <a className="btn" href={source.url} target="_blank" rel="noreferrer" title="打开 GitHub 来源"><ExternalLink size={16} /></a>
          </article>
        ))}
      </section>

      <section className="github-resource-results">
        <div className="section-title"><h2>全部资源</h2><span>{loading ? "正在读取资源…" : `共 ${total} 条 · 第 ${page} / ${Math.max(1, Math.ceil(total / 100))} 页`}</span></div>
        <div className="github-resource-toolbar">
          <div className="github-resource-filter"><Search size={17} /><input value={filter} onChange={(event) => setFilter(event.target.value)} placeholder="筛选名称、文件名或网盘" aria-label="筛选 GitHub 资源" /></div>
          <span>已选择 {selected.size} 条</span>
          <button className="primary" onClick={collect} disabled={saving || selected.size === 0}>{saving ? "收藏中…" : "收藏所选"}</button>
        </div>

        {loading && <div className="panel muted">正在解析 GitHub 资源…</div>}
        {!loading && visibleItems.length === 0 && <div className="panel muted">暂时没有解析到资源。</div>}
        {!loading && visibleItems.length > 0 && <div className="github-resource-table">
          {visibleItems.map((item) => {
            const originalIndex = items.indexOf(item);
            const key = resourceKey(item, originalIndex);
            return <div className={`github-resource-row${selected.has(key) ? " selected" : ""}`} key={key}>
              <button className="discover-check" onClick={() => toggle(item, originalIndex)} aria-label={selected.has(key) ? "取消选择" : "选择"}>{selected.has(key) ? <CheckSquare size={20} /> : <Square size={20} />}</button>
              <div className="github-resource-info">
                <div className="discover-result-title"><strong>{item.title || item.fileName || "未命名资源"}</strong><span className="badge badge-off">{PROVIDER_LABEL[item.provider] || item.provider}</span></div>
                <div className="github-resource-details">{item.resourceType || "未分类"}{item.fileName && ` · ${item.fileName}`}{item.updatedAt && ` · ${item.updatedAt}`}</div>
                <div className="github-resource-source">{item.shareUrl} · {item.repository}/{item.path}</div>
              </div>
            </div>;
          })}
        </div>}
        {!loading && total > 100 && <div className="github-resource-pagination">
          <button onClick={() => setPage((value) => Math.max(1, value - 1))} disabled={page <= 1}>上一页</button>
          <span>第 {page} / {Math.ceil(total / 100)} 页</span>
          <button onClick={() => setPage((value) => Math.min(Math.ceil(total / 100), value + 1))} disabled={page >= Math.ceil(total / 100)}>下一页</button>
        </div>}
      </section>
    </div>
  );
}
