import { NavLink, Route, Routes } from "react-router-dom";
import Home from "./pages/Home";
import Watch from "./pages/Watch";
import Settings from "./pages/Settings";
import Resources from "./pages/Resources";
import Browse from "./pages/Browse";

type NavItem = { to: string; ico: string; label: string; end?: boolean };

const NAV: { section: string; items: NavItem[] }[] = [
  {
    section: "浏览",
    items: [
      { to: "/", ico: "🏠", label: "首页", end: true },
      { to: "/browse", ico: "🗂️", label: "分享浏览" },
      { to: "/resources", ico: "🎬", label: "资源库" },
    ],
  },
  {
    section: "管理",
    items: [{ to: "/settings", ico: "⚙️", label: "设置 · 授权" }],
  },
];

export default function App() {
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <NavLink to="/" className="logo" end>
          <span className="mark">▶</span>
          <span className="name">ivideo</span>
        </NavLink>

        {NAV.map((group) => (
          <div key={group.section}>
            <div className="nav-section">{group.section}</div>
            {group.items.map((it) => (
              <NavLink
                key={it.to}
                to={it.to}
                end={it.end}
                className={({ isActive }) =>
                  "nav-link" + (isActive ? " active" : "")
                }
              >
                <span className="ico">{it.ico}</span>
                <span className="label">{it.label}</span>
              </NavLink>
            ))}
          </div>
        ))}

        <div className="spacer" />
        <div className="foot">网盘转存 · 管理台</div>
      </aside>

      <main className="content">
        <div className="container">
          <Routes>
            <Route path="/" element={<Home />} />
            <Route path="/watch" element={<Watch />} />
            <Route path="/resources" element={<Resources />} />
            <Route path="/browse" element={<Browse />} />
            <Route path="/settings" element={<Settings />} />
          </Routes>
        </div>
      </main>
    </div>
  );
}
