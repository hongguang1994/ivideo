import { ExternalLink, LoaderCircle, Play, Plus, Rss, Save, Trash2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { getRSSSettings, runRSSCollection, saveRSSSettings, type RSSCollectionStatus, type RSSFeed, type RSSSchedule } from "../../api";

const emptyStatus: RSSCollectionStatus = { running: false, feeds: 0, discovered: 0, added: 0, existing: 0, lastError: "", startedAt: 0, finishedAt: 0 };

function newFeed(): RSSFeed {
  return { id: "", name: "", url: "", enabled: true, fetchArticle: false };
}

function formatTime(value: number) {
  return value ? new Date(value * 1000).toLocaleString() : "尚未采集";
}

export default function RSSSettings() {
  const [feeds, setFeeds] = useState<RSSFeed[]>([]);
  const [schedule, setSchedule] = useState<RSSSchedule>({ enabled: false, intervalMinutes: 360 });
  const [status, setStatus] = useState<RSSCollectionStatus>(emptyStatus);
  const [busy, setBusy] = useState(true);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  const refresh = useCallback(async () => {
    try {
      const data = await getRSSSettings();
      setFeeds(data.feeds);
      setSchedule(data.schedule);
      setStatus(data.status);
      setError("");
    } catch (err) { setError(err instanceof Error ? err.message : "读取 RSS 设置失败"); }
    finally { setBusy(false); }
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);
  useEffect(() => {
    if (!status.running) return;
    const timer = window.setInterval(() => { void refresh(); }, 2000);
    return () => window.clearInterval(timer);
  }, [refresh, status.running]);

  const updateFeed = (index: number, patch: Partial<RSSFeed>) => {
    setFeeds((current) => current.map((feed, itemIndex) => itemIndex === index ? { ...feed, ...patch } : feed));
  };

  const save = async () => {
    setBusy(true); setError(""); setMessage("");
    try {
      const data = await saveRSSSettings(feeds, schedule);
      setFeeds(data.feeds); setSchedule(data.schedule); setStatus(data.status);
      setMessage("RSS 订阅设置已保存");
    } catch (err) { setError(err instanceof Error ? err.message : "保存失败"); }
    finally { setBusy(false); }
  };

  const collect = async () => {
    setError(""); setMessage("");
    try {
      await runRSSCollection();
      await refresh();
      setMessage("RSS 采集已启动，结果会自动写入分享库");
    } catch (err) { setError(err instanceof Error ? err.message : "启动采集失败"); }
  };

  return (
    <div className="settings-page settings-detail-page">
      <div className="page-head"><h1>RSS / Atom 订阅源</h1><p>订阅公开资源频道，自动提取网盘分享并写入本地分享库。</p></div>
      {error && <div className="settings-notice notice-error">出错了: {error}</div>}
      {message && <div className="settings-notice notice-success">{message}</div>}
      <section className="settings-section">
        <div className="settings-section-head"><div><h2>订阅列表</h2><p>使用公开的 RSS 或 Atom 地址。无须账号或 Token。</p></div><Rss size={19} /></div>
        <div className="rss-feed-list">
          {feeds.map((feed, index) => (
            <div className="rss-feed-row" key={feed.id || `new-${index}`}>
              <div className="rss-feed-fields">
                <input value={feed.name} onChange={(event) => updateFeed(index, { name: event.target.value })} placeholder="订阅名称，例如：资源频道" aria-label="订阅名称" />
                <input value={feed.url} onChange={(event) => updateFeed(index, { url: event.target.value })} placeholder="https://example.com/feed.xml" aria-label="RSS 或 Atom 地址" />
              </div>
              <label className="source-toggle"><input type="checkbox" checked={feed.enabled} onChange={(event) => updateFeed(index, { enabled: event.target.checked })} /><span>启用</span></label>
              <label className="source-toggle rss-article-toggle"><input type="checkbox" checked={feed.fetchArticle} onChange={(event) => updateFeed(index, { fetchArticle: event.target.checked })} /><span>读取文章页</span></label>
              <button title="删除订阅" aria-label="删除订阅" onClick={() => setFeeds((current) => current.filter((_, itemIndex) => itemIndex !== index))}><Trash2 size={17} /></button>
            </div>
          ))}
          {feeds.length === 0 && <div className="rss-empty">还没有订阅源。添加一个公开 RSS 或 Atom 地址后，采集到的分享会进入资源库。</div>}
        </div>
        <div className="rss-feed-actions"><button onClick={() => setFeeds((current) => [...current, newFeed()])} disabled={feeds.length >= 30}><Plus size={16} />添加订阅源</button></div>
      </section>
      <section className="settings-form-section">
        <div className="settings-form-row settings-toggle-row"><div><strong>定时采集</strong><span>后台按周期读取启用的订阅源；每篇文章只会跟随同站链接，避免无边界抓取。</span></div><input type="checkbox" checked={schedule.enabled} onChange={(event) => setSchedule({ ...schedule, enabled: event.target.checked })} /></div>
        <div className="settings-form-row"><div><strong>采集周期</strong><span>最短 15 分钟。首次保存后可立即手动采集。</span></div><select value={schedule.intervalMinutes} onChange={(event) => setSchedule({ ...schedule, intervalMinutes: Number(event.target.value) })}><option value={15}>每 15 分钟</option><option value={60}>每小时</option><option value={360}>每 6 小时</option><option value={1440}>每天</option><option value={10080}>每周</option></select></div>
      </section>
      <div className="settings-form-actions"><button onClick={collect} disabled={status.running || feeds.every((feed) => !feed.enabled)}><Play size={16} />{status.running ? "采集中" : "立即采集"}</button><button className="primary" onClick={save} disabled={busy}><Save size={16} />保存设置</button></div>
      <section className="settings-section rss-status-panel"><div className="settings-section-head"><div><h2>最近一次采集</h2><p>{status.running ? "正在读取订阅并同步分享库" : formatTime(status.finishedAt)}</p></div>{status.running ? <LoaderCircle className="spin" size={19} /> : <Rss size={19} />}</div><div className="rss-status-body"><div><strong>{status.feeds}</strong><span>订阅源</span></div><div><strong>{status.discovered}</strong><span>发现分享</span></div><div><strong>{status.added}</strong><span>新增入库</span></div><div><strong>{status.existing}</strong><span>已有分享</span></div>{status.lastError && <div className="settings-notice notice-error">最近错误：{status.lastError}</div>}<a href="https://validator.w3.org/feed/" target="_blank" rel="noreferrer"><ExternalLink size={15} />检查订阅格式</a></div></section>
    </div>
  );
}
