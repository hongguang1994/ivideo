import { Archive, ChevronRight, Globe2, ListChecks } from "lucide-react";
import { Link, Navigate, useSearchParams } from "react-router-dom";

const HOME_ACTIONS = [
  {
    to: "/discover",
    icon: Globe2,
    title: "发现资源",
    description: "搜索公开分享，并将需要的内容批量收藏。",
  },
  {
    to: "/resources",
    icon: Archive,
    title: "资源库",
    description: "查看已入库资源、有效状态和整理结果。",
  },
  {
    to: "/settings/import",
    icon: ListChecks,
    title: "导入与整理",
    description: "查看自动导入进度和等待复核的媒体。",
  },
];

export default function Home() {
  const [searchParams] = useSearchParams();
  const query = (searchParams.get("q") || "").trim();

  // 兼容旧的首页搜索链接，搜索结果统一交给资源发现引擎。
  if (query) return <Navigate to={`/discover?q=${encodeURIComponent(query)}`} replace />;

  return (
    <div className="home-page">
      <div className="page-head">
        <h1>媒体工作台</h1>
        <p>搜索公开分享、收藏资源，并跟踪后续导入与整理。</p>
      </div>

      <section className="home-workflow" aria-label="常用操作">
        {HOME_ACTIONS.map((action) => {
          const Icon = action.icon;
          return (
            <Link className="home-workflow-row" to={action.to} key={action.to}>
              <span className="home-workflow-icon"><Icon size={21} /></span>
              <span className="home-workflow-copy">
                <strong>{action.title}</strong>
                <span>{action.description}</span>
              </span>
              <ChevronRight size={19} />
            </Link>
          );
        })}
      </section>
    </div>
  );
}
