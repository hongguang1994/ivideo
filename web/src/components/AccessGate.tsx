import { KeyRound, LoaderCircle } from "lucide-react";
import { FormEvent, useEffect, useState } from "react";
import { clearAccessKey, getAccessKey, setAccessKey, verifyAccessKey } from "../api";

export default function AccessGate({ children }: { children: React.ReactNode }) {
  const [ready, setReady] = useState(false);
  const [locked, setLocked] = useState(false);
  const [value, setValue] = useState("");
  const [error, setError] = useState("");
  const [checking, setChecking] = useState(true);

  useEffect(() => {
    void verifyAccessKey(getAccessKey()).then(() => setReady(true)).catch(() => {
      clearAccessKey();
      setLocked(true);
    }).finally(() => setChecking(false));
  }, []);

  const unlock = async (event: FormEvent) => {
    event.preventDefault();
    setChecking(true); setError("");
    try {
      await verifyAccessKey(value.trim());
      setAccessKey(value.trim());
      setReady(true);
    } catch {
      setError("访问密钥不正确，请检查后重试。");
    } finally { setChecking(false); }
  };

  if (ready) return <>{children}</>;
  if (checking && !locked) return <div className="access-gate-loading"><LoaderCircle className="spin" size={21} />正在连接 ivideo…</div>;
  return <main className="access-gate"><form onSubmit={unlock}><div className="access-gate-icon"><KeyRound size={25} /></div><h1>访问 ivideo</h1><p>这是受保护的内网媒体工作台。请输入服务器配置的访问密钥。</p><input type="password" autoFocus value={value} onChange={(event) => setValue(event.target.value)} placeholder="访问密钥" aria-label="访问密钥" /><button className="primary" disabled={checking || !value.trim()}>{checking ? <LoaderCircle className="spin" size={16} /> : <KeyRound size={16} />}{checking ? "验证中" : "进入工作台"}</button>{error && <span className="access-gate-error">{error}</span>}</form></main>;
}
