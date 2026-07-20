import { useEffect, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import Player from "../components/Player";
import { playResource, type Source } from "../api";

export default function Watch() {
  const [params] = useSearchParams();
  const rid = params.get("rid"); // 资源库(按需转存)模式
  const source = (params.get("source") || "openlist") as Source;
  const path = params.get("path") || "";
  const id = params.get("id") || "";
  const name = params.get("name") || path.split("/").pop() || "视频";

  // ---- 资源库模式:轮询转存状态 ----
  const [status, setStatus] = useState("");
  const [streamUrl, setStreamUrl] = useState("");
  const [msg, setMsg] = useState("");
  const pollRef = useRef<number | null>(null);

  useEffect(() => {
    if (!rid) return;
    const tick = async () => {
      try {
        const r = await playResource(Number(rid));
        setStatus(r.status);
        setMsg(r.message || "");
        if (r.status === "ready" && r.streamUrl) {
          setStreamUrl(r.streamUrl);
          if (pollRef.current) clearInterval(pollRef.current);
        }
      } catch (e) {
        setMsg(String((e as Error).message || e));
      }
    };
    tick();
    pollRef.current = window.setInterval(tick, 2500);
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [rid]);

  if (rid) {
    return (
      <div>
        <div className="breadcrumb">
          <Link to="/resources" style={{ color: "var(--accent)" }}>⬅ 返回资源库</Link>
        </div>
        {streamUrl ? (
          <Player src={streamUrl} name={name} />
        ) : (
          <div className="qr-box" style={{ background: "var(--surface)", color: "var(--text)" }}>
            <p>{status === "failed" ? "❌ " : "⏳ "}{msg || "正在转存到你的网盘…"}</p>
            <p className="muted" style={{ fontSize: 13 }}>状态: {status || "请求中"}</p>
          </div>
        )}
      </div>
    );
  }

  // ---- OpenList / Jellyfin 直读模式 ----
  let directUrl = "";
  if (source === "jellyfin" && id) {
    directUrl = `/api/stream?source=jellyfin&id=${encodeURIComponent(id)}`;
  } else if (source === "openlist" && path) {
    directUrl = `/api/stream?source=openlist&path=${encodeURIComponent(path)}`;
  }
  if (!directUrl) return <p className="muted">缺少视频参数。</p>;

  return (
    <div>
      <div className="breadcrumb">
        <Link to="/" style={{ color: "var(--accent)" }}>⬅ 返回列表</Link>
      </div>
      <Player src={directUrl} name={name} />
      <h2 style={{ marginTop: 16 }}>{name}</h2>
      <p className="muted">来源：{source === "jellyfin" ? "Jellyfin 影库" : "网盘文件"}</p>
    </div>
  );
}
