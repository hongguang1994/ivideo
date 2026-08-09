import { FormEvent, lazy, Suspense, useEffect, useState } from "react";
import {
  Archive,
  ArrowLeft,
  Code2,
  Globe2,
  HardDriveDownload,
  Home as HomeIcon,
  Menu,
  Search,
  Settings as SettingsIcon,
} from "lucide-react";
import { Navigate, NavLink, Route, Routes, useLocation, useNavigate } from "react-router-dom";

// 页面按路由加载，首页无需下载设置中心和媒体整理等大型模块。
const Browse = lazy(() => import("./pages/Browse"));
const CachePanel = lazy(() => import("./pages/CachePanel"));
const Discover = lazy(() => import("./pages/Discover"));
const GitHubResources = lazy(() => import("./pages/GitHubResources"));
const Home = lazy(() => import("./pages/Home"));
const Resources = lazy(() => import("./pages/Resources"));
const Settings = lazy(() => import("./pages/Settings"));
const SettingsLayout = lazy(() => import("./pages/settings/SettingsLayout"));
const ShareSettings = lazy(() => import("./pages/settings/ShareSettings"));
const SearchSettings = lazy(() => import("./pages/settings/SearchSettings"));
const RSSSettings = lazy(() => import("./pages/settings/RSSSettings"));
const Logs = lazy(() => import("./pages/settings/Logs"));
const ImportSettings = lazy(() => import("./pages/settings/ImportSettings"));
const MediaOrganizer = lazy(() => import("./pages/settings/MediaOrganizer"));
const Shares = lazy(() => import("./pages/Shares"));
const Watch = lazy(() => import("./pages/Watch"));

const NAV = [
  { to: "/", icon: HomeIcon, label: "首页", end: true },
  { to: "/discover", icon: Globe2, label: "资源搜索" },
  { to: "/github-resources", icon: Code2, label: "GitHub 资源" },
  { to: "/resources", icon: Archive, label: "资源库" },
  { to: "/cache", icon: HardDriveDownload, label: "缓存管理" },
];

export default function App() {
  const [sidebarOpen, setSidebarOpen] = useState(() => window.innerWidth >= 1080);
  const [query, setQuery] = useState("");
  const navigate = useNavigate();
  const location = useLocation();
  const isSettings = location.pathname.startsWith("/settings");

  useEffect(() => {
    if (window.innerWidth < 820) setSidebarOpen(false);
  }, [location.pathname]);

  const search = (event: FormEvent) => {
    event.preventDefault();
    const q = query.trim();
    navigate(q ? `/discover?q=${encodeURIComponent(q)}` : "/discover");
  };

  return (
    <div className={`app-shell ${sidebarOpen ? "sidebar-open" : "sidebar-closed"}${isSettings ? " settings-mode" : ""}`}>
      <header className="topbar">
        <div className="topbar-leading">
          {isSettings ? (
            <button className="topbar-icon" title="返回" aria-label="返回" onClick={() => navigate(-1)}>
              <ArrowLeft size={21} />
            </button>
          ) : (
            <button className="topbar-icon" title="切换导航" aria-label="切换导航" onClick={() => setSidebarOpen((open) => !open)}>
              <Menu size={21} />
            </button>
          )}
          <NavLink to="/" className="brand" aria-label="ivideo 首页">
            <span className="brand-mark">i</span>
            <span>ivideo</span>
          </NavLink>
        </div>

        <form className="topbar-search" role="search" onSubmit={search}>
          <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索公开资源" aria-label="搜索公开资源" />
          <button title="搜索" aria-label="搜索"><Search size={19} /></button>
        </form>

        <NavLink to="/settings" className="topbar-icon" title="设置" aria-label="打开设置中心">
          <SettingsIcon size={21} />
        </NavLink>
      </header>

      {!isSettings && <aside className="sidebar" aria-label="主导航">
        <nav className="main-nav">
          {NAV.map((item) => {
            const Icon = item.icon;
            return (
              <NavLink key={item.to} to={item.to} end={item.end} className={({ isActive }) => `nav-link${isActive ? " active" : ""}`} title={item.label}>
                <Icon size={20} />
                <span className="label">{item.label}</span>
              </NavLink>
            );
          })}
        </nav>
      </aside>}

      {!isSettings && sidebarOpen && <button className="nav-scrim" aria-label="关闭导航" onClick={() => setSidebarOpen(false)} />}

      <main className="content">
        <div className="container">
          <Suspense fallback={<div className="page-loading" role="status">加载中...</div>}>
          <Routes>
            <Route path="/" element={<Home />} />
            <Route path="/watch" element={<Watch />} />
            <Route path="/discover" element={<Discover />} />
            <Route path="/github-resources" element={<GitHubResources />} />
            <Route path="/resources" element={<Resources />} />
            <Route path="/browse" element={<Navigate to="/settings/shares/browse" replace />} />
            <Route path="/shares" element={<Navigate to="/settings/shares" replace />} />
            <Route path="/cache" element={<CachePanel />} />
            <Route path="/settings" element={<SettingsLayout />}>
              <Route index element={<Settings section="overview" />} />
              <Route path="accounts" element={<Settings section="providers" />} />
              <Route path="jellyfin" element={<Settings section="jellyfin" />} />
              <Route path="metadata" element={<Settings section="metadata" />} />
              <Route path="organizer" element={<MediaOrganizer />} />
              <Route path="import" element={<ImportSettings />} />
              <Route path="search" element={<SearchSettings />} />
              <Route path="rss" element={<RSSSettings />} />
              <Route path="shares" element={<Shares />} />
              <Route path="shares/browse" element={<Browse embedded />} />
              <Route path="share-preferences" element={<ShareSettings />} />
              <Route path="logs" element={<Logs />} />
            </Route>
            <Route path="*" element={<Home />} />
          </Routes>
          </Suspense>
        </div>
      </main>
    </div>
  );
}
