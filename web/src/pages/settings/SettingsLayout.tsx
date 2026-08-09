import { Database, Download, FolderHeart, KeyRound, LayoutGrid, ListChecks, ScrollText, SearchCode, Server, SlidersHorizontal } from "lucide-react";
import { NavLink, Outlet } from "react-router-dom";

const ITEMS = [
  { to: "/settings", label: "设置概览", icon: LayoutGrid, end: true },
  { to: "/settings/accounts", label: "网盘授权", icon: KeyRound },
  { to: "/settings/jellyfin", label: "Jellyfin", icon: Server },
  { to: "/settings/metadata", label: "媒体刮削", icon: Database },
  { to: "/settings/organizer", label: "媒体整理", icon: ListChecks },
  { to: "/settings/import", label: "资源导入", icon: Download },
  { to: "/settings/search", label: "搜索来源", icon: SearchCode },
  { to: "/settings/shares", label: "分享库管理", icon: FolderHeart },
  { to: "/settings/share-preferences", label: "分享偏好", icon: SlidersHorizontal },
  { to: "/settings/logs", label: "运行日志", icon: ScrollText },
];

export default function SettingsLayout() {
  return (
    <div className="settings-layout">
      <aside className="settings-nav" aria-label="设置分类">
        <div className="settings-nav-title">设置</div>
        {ITEMS.map((item) => {
          const Icon = item.icon;
          return (
            <NavLink key={item.to} to={item.to} end={item.end} className={({ isActive }) => `settings-nav-link${isActive ? " active" : ""}`}>
              <Icon size={18} />
              <span>{item.label}</span>
            </NavLink>
          );
        })}
      </aside>
      <div className="settings-content"><Outlet /></div>
    </div>
  );
}
