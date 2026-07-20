import { Link, Route, Routes } from "react-router-dom";
import Home from "./pages/Home";
import Watch from "./pages/Watch";
import Settings from "./pages/Settings";
import Resources from "./pages/Resources";
import Browse from "./pages/Browse";

export default function App() {
  return (
    <>
      <header className="header">
        <Link to="/" className="logo">
          ▶ ivideo
        </Link>
        <span className="muted">网盘视频平台</span>
        <Link to="/browse" className="header-link" style={{ marginLeft: "auto" }}>
          分享浏览
        </Link>
        <Link to="/resources" className="header-link" style={{ marginLeft: 16 }}>
          资源库
        </Link>
        <Link to="/settings" className="header-link" style={{ marginLeft: 16 }}>
          设置
        </Link>
      </header>
      <main className="container">
        <Routes>
          <Route path="/" element={<Home />} />
          <Route path="/watch" element={<Watch />} />
          <Route path="/resources" element={<Resources />} />
          <Route path="/browse" element={<Browse />} />
          <Route path="/settings" element={<Settings />} />
        </Routes>
      </main>
    </>
  );
}
