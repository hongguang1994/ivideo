import { useEffect, useMemo, useRef, useState } from "react";
import { Download, Pause, Play, RotateCcw, Trash2 } from "lucide-react";

type LogLevel = "debug" | "info" | "warn" | "error";

interface LogEntry {
  time: string;
  level: string;
  message: string;
  attrs?: Record<string, unknown>;
}

interface LogEnvelope {
  type: "history" | "entry";
  entries?: LogEntry[];
  entry?: LogEntry;
}

const LEVELS: Array<{ value: LogLevel; label: string }> = [
  { value: "debug", label: "调试" },
  { value: "info", label: "信息" },
  { value: "warn", label: "警告" },
  { value: "error", label: "错误" },
];

function normalizedLevel(level: string): LogLevel {
  const value = level.toLowerCase();
  if (value.startsWith("debug")) return "debug";
  if (value.startsWith("warn")) return "warn";
  if (value.startsWith("error")) return "error";
  return "info";
}

function formatAttrs(attrs?: Record<string, unknown>): string {
  if (!attrs) return "";
  return Object.entries(attrs)
    .map(([key, value]) => `${key}=${typeof value === "string" ? value : JSON.stringify(value)}`)
    .join("  ");
}

export default function Logs() {
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [status, setStatus] = useState<"connecting" | "connected" | "disconnected">("connecting");
  const [paused, setPaused] = useState(false);
  const [pending, setPending] = useState(0);
  const [autoScroll, setAutoScroll] = useState(true);
  const [levels, setLevels] = useState<Set<LogLevel>>(new Set(LEVELS.map((item) => item.value)));
  const [filter, setFilter] = useState("");
  const pausedRef = useRef(false);
  const pendingRef = useRef<LogEntry[]>([]);
  const viewportRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let disposed = false;
    let reconnect: number | undefined;
    let socket: WebSocket | undefined;

    const connect = () => {
      if (disposed) return;
      setStatus("connecting");
      const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
      const connection = new WebSocket(`${protocol}//${window.location.host}/api/v1/logs/ws`);
      socket = connection;
      connection.onopen = () => setStatus("connected");
      connection.onmessage = (event) => {
        const envelope = JSON.parse(event.data) as LogEnvelope;
        if (envelope.type === "history") {
          const history = (envelope.entries || []).slice(-1000);
          if (pausedRef.current) {
            pendingRef.current = history;
            setPending(history.length);
          } else {
            setEntries(history);
          }
          return;
        }
        if (!envelope.entry) return;
        if (pausedRef.current) {
          pendingRef.current.push(envelope.entry);
          setPending(pendingRef.current.length);
        } else {
          setEntries((current) => [...current, envelope.entry!].slice(-1000));
        }
      };
      connection.onclose = () => {
        if (disposed) return;
        setStatus("disconnected");
        reconnect = window.setTimeout(connect, 2000);
      };
      connection.onerror = () => connection.close();
    };

    connect();
    return () => {
      disposed = true;
      if (reconnect) window.clearTimeout(reconnect);
      socket?.close();
    };
  }, []);

  useEffect(() => {
    if (autoScroll && !paused && viewportRef.current) {
      viewportRef.current.scrollTop = viewportRef.current.scrollHeight;
    }
  }, [entries, autoScroll, paused]);

  const visible = useMemo(() => {
    const keyword = filter.trim().toLocaleLowerCase();
    return entries.filter((entry) => {
      if (!levels.has(normalizedLevel(entry.level))) return false;
      if (!keyword) return true;
      return `${entry.message} ${formatAttrs(entry.attrs)}`.toLocaleLowerCase().includes(keyword);
    });
  }, [entries, filter, levels]);

  const togglePause = () => {
    const next = !pausedRef.current;
    pausedRef.current = next;
    setPaused(next);
    if (!next && pendingRef.current.length > 0) {
      setEntries((current) => [...current, ...pendingRef.current].slice(-1000));
      pendingRef.current = [];
      setPending(0);
    }
  };

  const toggleLevel = (level: LogLevel) => setLevels((current) => {
    const next = new Set(current);
    if (next.has(level)) next.delete(level);
    else next.add(level);
    return next;
  });

  const download = () => {
    const blob = new Blob([entries.map((entry) => `${entry.time} ${entry.level.toUpperCase()} ${entry.message} ${formatAttrs(entry.attrs)}`.trim()).join("\n")], { type: "text/plain;charset=utf-8" });
    const link = document.createElement("a");
    link.href = URL.createObjectURL(blob);
    link.download = `ivideo-logs-${new Date().toISOString().replace(/[:.]/g, "-")}.txt`;
    link.click();
    URL.revokeObjectURL(link.href);
  };

  const statusLabel = status === "connected" ? "实时连接" : status === "connecting" ? "连接中" : "正在重连";

  return (
    <div className="settings-page logs-page">
      <div className="page-head"><h1>运行日志</h1><p>查看后端运行状态与实时事件。</p></div>
      <section className="log-console">
        <div className="log-toolbar">
          <span className={`log-connection ${status}`}>{statusLabel}</span>
          <div className="log-level-filter" aria-label="日志级别筛选">
            {LEVELS.map((item) => <button key={item.value} className={levels.has(item.value) ? "active" : ""} onClick={() => toggleLevel(item.value)}>{item.label}</button>)}
          </div>
          <input value={filter} onChange={(event) => setFilter(event.target.value)} placeholder="筛选日志" aria-label="筛选日志" />
          <label className="log-auto-scroll"><input type="checkbox" checked={autoScroll} onChange={(event) => setAutoScroll(event.target.checked)} />自动滚动</label>
          <button title={paused ? "继续" : "暂停"} aria-label={paused ? "继续日志" : "暂停日志"} onClick={togglePause}>{paused ? <Play size={17} /> : <Pause size={17} />}{paused && pending > 0 && <span>{pending}</span>}</button>
          <button title="下载日志" aria-label="下载日志" onClick={download} disabled={entries.length === 0}><Download size={17} /></button>
          <button title="清空当前视图" aria-label="清空当前视图" onClick={() => setEntries([])} disabled={entries.length === 0}><Trash2 size={17} /></button>
        </div>
        <div className="log-stats"><span>当前 {visible.length} 条</span><span>缓存 {entries.length}/1000</span>{paused && <span>已暂停</span>}</div>
        <div className="log-viewport" ref={viewportRef}>
          {visible.length === 0 ? <div className="log-empty"><RotateCcw size={18} />等待日志</div> : visible.map((entry, index) => {
            const level = normalizedLevel(entry.level);
            return <div className={`log-row level-${level}`} key={`${entry.time}-${index}`}>
              <time>{new Date(entry.time).toLocaleTimeString("zh-CN", { hour12: false })}</time>
              <span className="log-level">{level.toUpperCase()}</span>
              <div><span className="log-message">{entry.message}</span>{entry.attrs && <span className="log-attrs">{formatAttrs(entry.attrs)}</span>}</div>
            </div>;
          })}
        </div>
      </section>
    </div>
  );
}
