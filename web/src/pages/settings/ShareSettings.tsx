import { RotateCcw, Save } from "lucide-react";
import { useState } from "react";
import { defaultSharePreferences, getSharePreferences, saveSharePreferences } from "../../sharePreferences";

export default function ShareSettings() {
  const [preferences, setPreferences] = useState(getSharePreferences);
  const [saved, setSaved] = useState(false);

  const persist = () => {
    saveSharePreferences(preferences);
    setSaved(true);
    window.setTimeout(() => setSaved(false), 1800);
  };

  const reset = () => {
    setPreferences({ ...defaultSharePreferences });
    saveSharePreferences(defaultSharePreferences);
    setSaved(true);
  };

  return (
    <div className="settings-page settings-detail-page">
      <div className="page-head">
        <h1>分享库设置</h1>
        <p>设置收藏分享和手动转存时使用的默认值。</p>
      </div>
      {saved && <div className="settings-notice notice-success">分享库偏好已保存</div>}
      <section className="settings-form-section">
        <div className="settings-form-row">
          <div><strong>默认网盘</strong><span>新增收藏时预先选择的网盘。</span></div>
          <select value={preferences.defaultProvider} onChange={(event) => setPreferences({ ...preferences, defaultProvider: event.target.value })}>
            <option value="aliyun">阿里云盘</option>
            <option value="115">115 网盘</option>
            <option value="quark">夸克网盘</option>
          </select>
        </div>
        <div className="settings-form-row">
          <div><strong>默认分类</strong><span>新增收藏时自动填入，可在收藏时修改。</span></div>
          <input value={preferences.defaultCategory} onChange={(event) => setPreferences({ ...preferences, defaultCategory: event.target.value })} placeholder="例如：动漫" />
        </div>
        <div className="settings-form-row">
          <div><strong>默认转存目录</strong><span>在分享浏览中手动转存文件时使用。</span></div>
          <input value={preferences.targetFolder} onChange={(event) => setPreferences({ ...preferences, targetFolder: event.target.value })} placeholder="ivideo" />
        </div>
        <label className="settings-form-row settings-toggle-row">
          <div><strong>删除前确认</strong><span>删除收藏的分享前再次询问。</span></div>
          <input type="checkbox" checked={preferences.confirmDelete} onChange={(event) => setPreferences({ ...preferences, confirmDelete: event.target.checked })} />
        </label>
      </section>
      <div className="settings-form-actions">
        <button onClick={reset}><RotateCcw size={16} />恢复默认</button>
        <button className="primary" onClick={persist}><Save size={16} />保存更改</button>
      </div>
    </div>
  );
}
