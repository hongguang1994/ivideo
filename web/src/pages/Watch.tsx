import { Link, useSearchParams } from "react-router-dom";
import Player from "../components/Player";

export default function Watch() {
  const [params] = useSearchParams();
  const path = params.get("path") || "";
  const name = path.split("/").pop() || "视频";

  if (!path) {
    return <p className="muted">缺少视频路径。</p>;
  }

  const streamUrl = `/api/stream?path=${encodeURIComponent(path)}`;

  return (
    <div>
      <div className="breadcrumb">
        <Link to="/" style={{ color: "var(--accent)" }}>
          ⬅ 返回列表
        </Link>
      </div>
      <Player src={streamUrl} name={name} />
      <h2 style={{ marginTop: 16 }}>{name}</h2>
      <p className="muted">{path}</p>
    </div>
  );
}
