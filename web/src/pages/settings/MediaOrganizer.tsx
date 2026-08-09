import { useEffect, useMemo, useState } from "react";
import { Check, ChevronRight, CircleAlert, FileVideo, FolderTree, LoaderCircle, RotateCcw, Search } from "lucide-react";
import { confirmMediaGroup, getMediaGroups, MediaCandidate, MediaGroupDetail, reviewMediaGroup, searchMediaGroupCandidates } from "../../api";

const FILTERS = [
  { value: "review", label: "待审核" },
  { value: "verified", label: "已验证" },
  { value: "published", label: "已发布" },
  { value: "all", label: "全部" },
];

const LIBRARIES = [
  { value: "anime", label: "动漫" },
  { value: "tv", label: "剧集" },
  { value: "movies", label: "电影" },
  { value: "variety", label: "综艺" },
];

type CandidateEvidence = {
  titleExact?: boolean;
  yearMatch?: boolean;
  typeMatch?: boolean;
  summary?: string;
  content?: { status?: string; reason?: string; frameDistance?: number; ocrMatch?: boolean };
};

function evidence(candidate: MediaCandidate) {
	try { return JSON.parse(candidate.evidenceJson) as CandidateEvidence; }
  catch { return {}; }
}

export default function MediaOrganizer() {
  const [filter, setFilter] = useState("review");
  const [items, setItems] = useState<MediaGroupDetail[]>([]);
  const [total, setTotal] = useState(0);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [candidateId, setCandidateId] = useState<number | null>(null);
  const [library, setLibrary] = useState("anime");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
	const [candidateQuery, setCandidateQuery] = useState("");

  const selected = useMemo(() => items.find((item) => item.group.id === selectedId) || items[0], [items, selectedId]);

  const load = async (nextFilter = filter) => {
    setLoading(true); setError("");
    try {
      const data = await getMediaGroups(nextFilter);
      setItems(data.items); setTotal(data.total);
      const first = data.items[0];
      setSelectedId(first?.group.id ?? null);
      setCandidateId(first?.candidates[0]?.id ?? null);
      setLibrary(first?.candidates[0]?.library === "review" ? "anime" : first?.candidates[0]?.library || first?.group.suggestedLibrary || "anime");
    } catch (e) { setError(e instanceof Error ? e.message : String(e)); }
    finally { setLoading(false); }
  };

  useEffect(() => { void load(filter); }, [filter]);

  const chooseGroup = (item: MediaGroupDetail) => {
    setSelectedId(item.group.id);
    setCandidateId(item.candidates[0]?.id ?? null);
    const suggested = item.candidates[0]?.library || item.group.suggestedLibrary;
    setLibrary(suggested === "review" ? "anime" : suggested || "anime");
  };

  const confirm = async () => {
    if (!selected || !candidateId) return;
    setSaving(true); setError("");
    try { await confirmMediaGroup(selected.group.id, candidateId, library); await load(filter); }
    catch (e) { setError(e instanceof Error ? e.message : String(e)); }
    finally { setSaving(false); }
  };

  const sendBack = async () => {
    if (!selected) return;
    setSaving(true); setError("");
    try { await reviewMediaGroup(selected.group.id); await load(filter); }
    catch (e) { setError(e instanceof Error ? e.message : String(e)); }
    finally { setSaving(false); }
  };

	const searchCandidates = async () => {
		if (!selected) return;
		setSaving(true); setError("");
		try {
			const detail = await searchMediaGroupCandidates(selected.group.id, candidateQuery || selected.group.rawTitle);
			setItems((current) => current.map((item) => item.group.id === detail.group.id ? detail : item));
			setCandidateId(detail.candidates[0]?.id ?? null);
		} catch (e) { setError(e instanceof Error ? e.message : String(e)); }
		finally { setSaving(false); }
	};

  return <div className="settings-page organizer-page">
    <div className="page-head"><h1>媒体整理</h1><p>按作品组核对路径、候选和证据，确认后才发布到 Jellyfin。</p></div>
    <div className="organizer-toolbar">
      <div className="segmented-control" aria-label="作品状态">
        {FILTERS.map((item) => <button key={item.value} className={filter === item.value ? "active" : ""} onClick={() => setFilter(item.value)}>{item.label}</button>)}
      </div>
      <span>{total} 个作品组</span>
    </div>
    {error && <div className="settings-notice notice-error"><CircleAlert size={17} />{error}</div>}
    {loading ? <div className="organizer-empty"><LoaderCircle className="spin" size={22} />正在读取作品组</div> : !selected ? <div className="organizer-empty">当前没有作品组</div> :
      <div className="organizer-workspace">
        <div className="organizer-list" aria-label="作品组列表">
          {items.map((item) => <button key={item.group.id} className={`organizer-list-row${item.group.id === selected.group.id ? " active" : ""}`} onClick={() => chooseGroup(item)}>
            <span className="organizer-kind"><FolderTree size={18} /></span>
            <span><strong>{item.group.canonicalTitle || item.group.rawTitle || "未命名作品"}</strong><small>{item.members.length} 个文件 · {item.group.mediaKind === "movie" ? "电影" : "剧集"} · {item.group.confidence}%</small></span>
            <ChevronRight size={17} />
          </button>)}
        </div>
        <section className="organizer-inspector">
          <div className="organizer-inspector-head"><div><h2>{selected.group.rawTitle || "未命名作品"}</h2><p>{selected.group.reason || "等待核对候选"}</p></div><span className={`group-status status-${selected.group.status}`}>{selected.group.status}</span></div>
          <div className="organizer-block"><h3>候选作品</h3><div className="candidate-search"><input value={candidateQuery} onChange={(event) => setCandidateQuery(event.target.value)} placeholder="输入更准确的作品名" /><button title="搜索候选" onClick={searchCandidates} disabled={saving}><Search size={16} />搜索</button></div>
            {selected.candidates.length === 0 ? <p className="sub">没有可靠候选，需要重新整理路径或补充内容证据。</p> : selected.candidates.map((candidate) => {
              const facts = evidence(candidate);
              return <label key={candidate.id} className={`candidate-row${candidateId === candidate.id ? " selected" : ""}`}>
                <input type="radio" name="candidate" checked={candidateId === candidate.id} onChange={() => { setCandidateId(candidate.id); setLibrary(candidate.library === "review" ? "anime" : candidate.library); }} />
                <span className="candidate-main"><strong>{candidate.title}</strong><small>{candidate.source.toUpperCase()} {candidate.providerId} · {candidate.year || "年份未知"}</small><span className="evidence-list">{facts.titleExact && <em>标题一致</em>}{facts.yearMatch && <em>年份一致</em>}{facts.typeMatch && <em>类型一致</em>}{facts.content?.status === "supporting" && <em title={facts.content.reason}>内容支持</em>}{facts.content?.status === "conflicting" && <em className="evidence-conflict" title={facts.content.reason}>内容冲突</em>}{facts.content?.status === "unavailable" && <em className="evidence-muted" title={facts.content.reason}>内容未读取</em>}</span>{facts.content?.reason && <small className="content-evidence-reason">{facts.content.reason}</small>}</span>
                <b>{candidate.score}</b>
              </label>;
            })}
          </div>
          <div className="organizer-block"><h3>发布分类</h3><select value={library} onChange={(event) => setLibrary(event.target.value)}>{LIBRARIES.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select></div>
          <details className="organizer-members" open><summary><FileVideo size={17} />包含 {selected.members.length} 个文件</summary><div>{selected.members.slice(0, 100).map((member) => <div key={member.resourceId}><span>{member.season > 0 ? `S${String(member.season).padStart(2, "0")}E${String(member.episode).padStart(2, "0")}` : "电影"}</span><code>{member.resource.filePath}</code></div>)}</div></details>
          <div className="organizer-actions"><button onClick={sendBack} disabled={saving}><RotateCcw size={16} />退回待整理</button><button className="primary" onClick={confirm} disabled={saving || !candidateId}>{saving ? <LoaderCircle className="spin" size={16} /> : <Check size={16} />}确认并发布整组</button></div>
        </section>
      </div>}
  </div>;
}
