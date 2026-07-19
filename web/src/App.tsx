import { Link, Route, Routes } from "react-router-dom";
import Home from "./pages/Home";
import Watch from "./pages/Watch";

export default function App() {
  return (
    <>
      <header className="header">
        <Link to="/" className="logo">
          ▶ ivideo
        </Link>
        <span className="muted">网盘视频平台</span>
      </header>
      <main className="container">
        <Routes>
          <Route path="/" element={<Home />} />
          <Route path="/watch" element={<Watch />} />
        </Routes>
      </main>
    </>
  );
}
