import { Link, useSearchParams } from "react-router-dom";
import Player from "../components/Player";
import type { Source } from "../api";

export default function Watch() {
  const [params] = useSearchParams();
  const source = (params.get("source") || "openlist") as Source;
  const path = params.get("path") || "";
  const id = params.get("id") || "";
  const name = params.get("name") || path.split("/").pop() || "视频";

  // 按来源构造后端播放地址。
  let streamUrl = "";
  if (source === "jellyfin" && id) {
    streamUrl = `/api/stream?source=jellyfin&id=${encodeURIComponent(id)}`;
  } else if (source === "openlist" && path) {
    streamUrl = `/api/stream?source=openlist&path=${encodeURIComponent(path)}`;
  }

  if (!streamUrl) {
    return <p className="muted">缺少视频参数。</p>;
  }

  return (
    <div>
      <div className="breadcrumb">
        <Link to="/" style={{ color: "var(--accent)" }}>
          ⬅ 返回列表
        </Link>
      </div>
      <Player src={streamUrl} name={name} />
      <h2 style={{ marginTop: 16 }}>{name}</h2>
      <p className="muted">来源：{source === "jellyfin" ? "Jellyfin 影库" : "网盘文件"}</p>
    </div>
  );
}
