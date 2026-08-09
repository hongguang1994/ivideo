import { Download, LoaderCircle, Play, Save } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { getImportSettings, runImportTask, saveImportSettings, type ImportSchedule, type ImportTaskStatus } from "../../api";

const emptyStatus: ImportTaskStatus = { running: false, total: 0, processed: 0, imported: 0, skipped: 0, failed: 0, current: "", lastError: "", startedAt: 0, finishedAt: 0 };

export default function ImportSettings() {
  const [schedule, setSchedule] = useState<ImportSchedule>({ enabled: false, intervalMinutes: 360 });
  const [status, setStatus] = useState<ImportTaskStatus>(emptyStatus);
  const [busy, setBusy] = useState(true);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  const refresh = useCallback(async () => {
    try {
      const data = await getImportSettings();
      setSchedule(data.schedule);
      setStatus(data.status);
      setError("");
    } catch (err) { setError(err instanceof Error ? err.message : "读取导入设置失败"); }
    finally { setBusy(false); }
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);
  useEffect(() => {
    if (!status.running) return;
    const timer = window.setInterval(() => { void refresh(); }, 2000);
    return () => window.clearInterval(timer);
  }, [status.running, refresh]);

  const save = async () => {
    setBusy(true); setError("");
    try { const data = await saveImportSettings(schedule); setSchedule(data.schedule); setStatus(data.status); setMessage("导入设置已保存"); window.setTimeout(() => setMessage(""), 1800); }
    catch (err) { setError(err instanceof Error ? err.message : "保存失败"); }
    finally { setBusy(false); }
  };

  const run = async () => {
    setError("");
    try { await runImportTask(); await refresh(); }
    catch (err) { setError(err instanceof Error ? err.message : "启动导入失败"); }
  };

  const percent = status.total > 0 ? Math.min(100, Math.round((status.processed / status.total) * 100)) : 0;
  return (
    <div className="settings-page settings-detail-page">
      <div className="page-head"><h1>资源导入</h1><p>自动遍历收藏的分享，将视频登记到资源库，并同步到 Jellyfin。</p></div>
      {error && <div className="settings-notice notice-error">出错了: {error}</div>}
      {message && <div className="settings-notice notice-success">{message}</div>}
      <section className="settings-form-section">
        <div className="settings-form-row settings-toggle-row">
          <div><strong>启用定时导入</strong><span>按周期处理收藏的分享。首次启用不会立即打满网盘接口。</span></div>
          <input type="checkbox" checked={schedule.enabled} onChange={(event) => setSchedule({ ...schedule, enabled: event.target.checked })} />
        </div>
        <div className="settings-form-row">
          <div><strong>导入周期</strong><span>每轮最多处理 20 个分享，最短周期为 5 分钟。</span></div>
          <select value={schedule.intervalMinutes} onChange={(event) => setSchedule({ ...schedule, intervalMinutes: Number(event.target.value) })}>
            <option value={5}>每 5 分钟</option><option value={15}>每 15 分钟</option><option value={60}>每小时</option><option value={360}>每 6 小时</option><option value={1440}>每天</option>
          </select>
        </div>
      </section>
      <div className="settings-form-actions">
        <button onClick={run} disabled={status.running}><Play size={16} />{status.running ? "导入中" : "立即导入"}</button>
        <button className="primary" onClick={save} disabled={busy}><Save size={16} />保存设置</button>
      </div>
      <section className="settings-section import-progress-panel">
        <div className="settings-section-head"><div><h2>任务进度</h2><p>{status.running ? `正在处理：${status.current || "准备中"}` : status.finishedAt ? "最近一次任务已完成" : "尚未运行导入任务"}</p></div><Download size={19} /></div>
        <div className="import-progress-body">
          <div className="import-progress-track"><span style={{ width: `${percent}%` }} /></div>
          <div className="import-progress-summary"><strong>已处理分享 {status.processed}/{status.total || 0}</strong><span>{percent}%</span></div>
          <div className="import-stat-grid"><div><strong>{status.imported}</strong><span>新增资源</span></div><div><strong>{status.skipped}</strong><span>已跳过</span></div><div><strong>{status.failed}</strong><span>失败</span></div></div>
          {status.lastError && <div className="settings-notice notice-error">最近错误：{status.lastError}</div>}
          {busy && <div className="import-loading"><LoaderCircle className="spin" size={16} />正在读取设置</div>}
        </div>
      </section>
    </div>
  );
}
