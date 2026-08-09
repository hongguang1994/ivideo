import { useEffect, useState } from "react";
import { Activity, Code2, ExternalLink, Trash2 } from "lucide-react";
import { deleteGitHubToken, getSearchSettings, saveGitHubToken, updateSearchSource, type SearchSettingsStatus } from "../../api";

export default function SearchSettings() {
  const [status, setStatus] = useState<SearchSettingsStatus | null>(null);
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState(false);
  const [busySource, setBusySource] = useState("");
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  const load = () => getSearchSettings().then(setStatus).catch((e) => setError(String((e as Error).message || e)));
  useEffect(() => { load(); }, []);

  const save = async () => {
    setBusy(true); setError(""); setMessage("");
    try {
      setStatus(await saveGitHubToken(token.trim()));
      setToken("");
      setMessage("GitHub 搜索来源已连接");
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally { setBusy(false); }
  };

  const remove = async () => {
    if (!window.confirm("清除 GitHub Token？清除后将无法搜索 GitHub 公开资源。")) return;
    setBusy(true); setError(""); setMessage("");
    try {
      setStatus(await deleteGitHubToken());
      setMessage("GitHub Token 已清除");
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally { setBusy(false); }
  };

  const toggleSource = async (id: string, enabled: boolean) => {
    setBusySource(id); setError(""); setMessage("");
    try {
      setStatus(await updateSearchSource(id, enabled));
      setMessage(enabled ? "搜索来源已启用" : "搜索来源已停用");
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally { setBusySource(""); }
  };

  return (
    <div className="settings-page">
      <div className="page-head"><h1>资源发现引擎</h1><p>管理独立搜索引擎的来源、健康状态和连接凭据。</p></div>
      {error && <div className="settings-notice notice-error">{error}</div>}
      {message && <div className="settings-notice notice-success">{message}</div>}
      <section className="settings-section search-source-section">
        <div className="settings-section-head">
          <div><h2>{status?.engine?.name || "ivideo 资源发现引擎"}</h2><p>并行查询 · 规范化去重 · 相关度排序 · 查询缓存</p></div>
          <span className="badge badge-ok">独立运行</span>
        </div>
        <div className="search-source-health-list">
          {status?.engine?.sources.map((source) => (
            <div className="search-source-health" key={source.id}>
              <Activity size={18} />
              <div><strong>{source.name}</strong><span>{source.description || source.kind || "独立搜索来源"}</span><span>{source.lastCheckedAt ? `最近耗时 ${source.lastDurationMs} ms · 超时 ${source.timeoutMs} ms` : `等待首次查询 · 超时 ${source.timeoutMs} ms`}</span></div>
              <span className={`badge ${source.enabled && (!source.lastCheckedAt || source.lastHealthy) ? "badge-ok" : "badge-off"}`}>{!source.enabled ? "已停用" : !source.lastCheckedAt ? "待检测" : source.lastHealthy ? "正常" : "异常"}</span>
              <label className="source-toggle"><input type="checkbox" checked={source.enabled} disabled={busySource === source.id} onChange={(event) => toggleSource(source.id, event.target.checked)} /><span>启用</span></label>
              {source.lastError && <span className="sub">{source.lastError}</span>}
            </div>
          ))}
        </div>
      </section>
      <section className="settings-section search-source-section">
        <div className="settings-section-head">
          <div><h2>GitHub 公开仓库</h2><p>代码搜索接口 · 只读访问</p></div>
          <span className={`badge ${status?.githubConfigured ? "badge-ok" : "badge-off"}`}>{status?.githubConfigured ? "已连接" : "未配置"}</span>
        </div>
        <div className="search-source-body">
          <span className="search-source-icon"><Code2 size={25} /></span>
          <div className="search-source-copy">
            <strong>Personal Access Token</strong>
            <span>使用只允许读取公开仓库的 Token，不需要仓库写权限。</span>
          </div>
          <a className="btn" href="https://github.com/settings/personal-access-tokens/new" target="_blank" rel="noreferrer"><ExternalLink size={16} />创建 Token</a>
          {status?.githubConfigured && <button title="清除 Token" aria-label="清除 GitHub Token" onClick={remove} disabled={busy}><Trash2 size={17} /></button>}
          <div className="search-token-form">
            <input type="password" value={token} onChange={(event) => setToken(event.target.value)} placeholder={status?.githubConfigured ? "粘贴新 Token 以替换" : "粘贴 GitHub Token"} />
            <button className="primary" onClick={save} disabled={busy || !token.trim()}>{busy ? "验证中…" : status?.githubConfigured ? "更新" : "验证并保存"}</button>
          </div>
        </div>
      </section>
    </div>
  );
}
