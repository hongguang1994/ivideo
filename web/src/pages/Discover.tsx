import { FormEvent, useEffect, useMemo, useState } from "react";
import { CheckSquare, DatabaseZap, ExternalLink, LoaderCircle, Search, Square } from "lucide-react";
import { Link, useSearchParams } from "react-router-dom";
import {
  addSharesBatch,
  searchResources,
  type BatchShareResponse,
  type SearchResource,
  type SearchResponse,
} from "../api";

const PROVIDER_LABEL: Record<string, string> = {
  aliyun: "阿里云盘",
  "115": "115网盘",
  quark: "夸克网盘",
};

export default function Discover({ variant = "search", showHeader = true }: { variant?: "search" | "github"; showHeader?: boolean }) {
  const [searchParams, setSearchParams] = useSearchParams();
  const query = (searchParams.get("q") || "").trim();
  const [input, setInput] = useState(query);
  const [response, setResponse] = useState<SearchResponse | null>(null);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState<BatchShareResponse | null>(null);

  useEffect(() => {
	let disposed = false;
	let socket: WebSocket | undefined;
	let reconnect: number | undefined;
	let jobPending = false;
    setInput(query);
    setResponse(null);
    setSelected(new Set());
    setResult(null);
    if (!query) return;
    setLoading(true);
    setError("");

	const applySnapshot = (data: SearchResponse) => {
		if (disposed) return;
		const rawMeta = data.meta as SearchResponse["meta"] | null | undefined;
		const normalized: SearchResponse = {
			...data,
			items: Array.isArray(data.items) ? data.items.filter(Boolean) : [],
			meta: {
				...(rawMeta || {}),
				source: rawMeta?.source || "ivideo-discovery",
				scanned: rawMeta?.scanned ?? 0,
				remaining: rawMeta?.remaining ?? 0,
				resetAt: rawMeta?.resetAt ?? 0,
				sources: Array.isArray(rawMeta?.sources) ? rawMeta.sources : [],
				warnings: Array.isArray(rawMeta?.warnings) ? rawMeta.warnings : [],
			},
		};
		jobPending = Boolean(normalized.pending);
		setResponse((current) => {
			const previousItems = current?.items || [];
			setSelected((selection) => {
				const allPreviousSelected = previousItems.length === 0 || previousItems.every((item) => selection.has(item.shareUrl));
				const available = new Set(normalized.items.map((item) => item.shareUrl));
				const next = new Set([...selection].filter((shareUrl) => available.has(shareUrl)));
				if (allPreviousSelected) normalized.items.forEach((item) => next.add(item.shareUrl));
				return next;
			});
			return normalized;
		});
	};

	const connect = (jobId: string) => {
		if (disposed || !jobPending) return;
		const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
		const connection = new WebSocket(`${protocol}//${window.location.host}/api/v1/search/resources/ws?jobId=${encodeURIComponent(jobId)}`);
		socket = connection;
		connection.onmessage = (event) => {
			const envelope = JSON.parse(event.data) as { type: "snapshot"; data?: SearchResponse };
			if (envelope.data) applySnapshot(envelope.data);
		};
		connection.onclose = () => {
			if (!disposed && jobPending) reconnect = window.setTimeout(() => connect(jobId), 1000);
		};
		connection.onerror = () => connection.close();
	};

    searchResources(query)
      .then((data) => {
		applySnapshot(data);
		if (data.pending && data.jobId) connect(data.jobId);
      })
      .catch((e) => setError(String((e as Error).message || e)))
      .finally(() => setLoading(false));
	return () => {
		disposed = true;
		if (reconnect) window.clearTimeout(reconnect);
		socket?.close();
	};
  }, [query]);

  const items = response?.items || [];
  const selectedItems = useMemo(() => items.filter((item) => selected.has(item.shareUrl)), [items, selected]);
  const allSelected = items.length > 0 && selectedItems.length === items.length;

  const submitSearch = (event: FormEvent) => {
    event.preventDefault();
    const value = input.trim();
    if (value.length < 2) {
      setError("搜索关键词至少需要 2 个字符");
      return;
    }
    setSearchParams({ q: value });
  };

  const toggle = (shareUrl: string) => {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(shareUrl)) next.delete(shareUrl);
      else next.add(shareUrl);
      return next;
    });
  };

  const toggleAll = () => setSelected(allSelected ? new Set() : new Set(items.map((item) => item.shareUrl)));

  const collect = async () => {
    setSaving(true);
    setError("");
    setResult(null);
    try {
      const saved = await addSharesBatch(selectedItems.map((item) => ({
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

  return (
    <div className="discover-page">
      {showHeader && <div className="page-head">
        <h1>{variant === "github" ? "GitHub 资源" : "资源搜索"}</h1>
        <p>{variant === "github" ? "从 GitHub 公开资源库中解析网盘分享和资源信息。" : "由 ivideo 资源发现引擎并行查找、去重并排序公开分享。"}</p>
      </div>}

      <form id="github-resource-search" className="discover-search" role="search" onSubmit={submitSearch}>
        <input value={input} onChange={(event) => setInput(event.target.value)} placeholder="输入电影、剧集或动漫名称" aria-label="资源搜索关键词" autoFocus />
        <button className="primary" disabled={loading}><Search size={18} />{loading ? "搜索中…" : "搜索"}</button>
      </form>

      {error && <div className="settings-notice notice-error">{error}<Link to="/settings/search">查看来源设置</Link></div>}
      {result && <div className="settings-notice notice-success">收藏完成：新增 {result.added} 条，重复 {result.duplicates} 条，失败 {result.failed} 条。</div>}

      {response && (
        <div className="discover-summary">
          <span><DatabaseZap size={16} /> ivideo 资源发现引擎</span>
          <span>{response.meta.sources?.filter((source) => source.healthy).length || 0}/{response.meta.sources?.length || 0} 个来源完成</span>
          <span>找到 {items.length} 条分享</span>
          <span>{response.meta.cached ? "命中缓存" : `耗时 ${response.meta.durationMs || 0} ms`}</span>
		  {response.pending && <span className="discover-pending"><LoaderCircle size={16} />后台继续搜索</span>}
        </div>
      )}

      {response?.meta.warnings?.map((warning) => <div className="settings-notice notice-warning" key={warning}>{warning}</div>)}

      {response && items.length === 0 && <div className="panel muted">{response.pending ? "快速来源暂未返回结果，其他来源仍在后台搜索。" : "没有找到包含可识别网盘链接的公开结果。"}</div>}

      {items.length > 0 && (
        <section className="discover-results">
          <div className="discover-result-toolbar">
            <button onClick={toggleAll}>{allSelected ? <CheckSquare size={17} /> : <Square size={17} />}{allSelected ? "取消全选" : "全选"}</button>
            <span>已选择 {selectedItems.length} 条</span>
            <button className="primary" onClick={collect} disabled={saving || selectedItems.length === 0}>{saving ? "收藏中…" : "收藏所选"}</button>
          </div>
          <div className="discover-result-list">
            {items.map((item) => <SearchResultRow key={item.shareUrl} item={item} checked={selected.has(item.shareUrl)} onToggle={() => toggle(item.shareUrl)} />)}
          </div>
        </section>
      )}
    </div>
  );
}

function SearchResultRow({ item, checked, onToggle }: { item: SearchResource; checked: boolean; onToggle: () => void }) {
  return (
    <div className={`discover-result-row${checked ? " selected" : ""}`}>
      <button className="discover-check" onClick={onToggle} aria-label={checked ? "取消选择" : "选择"}>{checked ? <CheckSquare size={20} /> : <Square size={20} />}</button>
      <div className="discover-result-main">
        <div className="discover-result-title"><strong>{item.title}</strong><span className="badge badge-off">{PROVIDER_LABEL[item.provider] || item.provider}</span></div>
        <a href={item.shareUrl} target="_blank" rel="noreferrer">{item.shareUrl}</a>
        <div className="sub">
          {item.sourceName && `${item.sourceName} · `}
          {item.resourceType && `${item.resourceType} · `}
          {item.fileName && `${item.fileName} · `}
          {item.updatedAt && `${item.updatedAt} · `}
          {[item.repository, item.path].filter(Boolean).join(" · ")}{item.sharePwd && ` · 提取码 ${item.sharePwd}`}
          {item.sources && item.sources.length > 1 && ` · ${item.sources.length} 个来源收录`}
        </div>
      </div>
      {item.sourceUrl && <a className="btn" href={item.sourceUrl} target="_blank" rel="noreferrer" title="查看原始来源"><ExternalLink size={17} /></a>}
    </div>
  );
}
