import { useEffect, useRef, useState } from "react";
import QRCode from "qrcode";
import {
  aliyunQR,
  aliyunQRStatus,
  getProviders,
  type Provider,
} from "../api";

export default function Settings() {
  const [providers, setProviders] = useState<Provider[]>([]);
  const [qrDataUrl, setQrDataUrl] = useState("");
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const pollRef = useRef<number | null>(null);

  const loadProviders = () => {
    getProviders()
      .then(setProviders)
      .catch((e) => setError(String(e.message || e)));
  };

  useEffect(() => {
    loadProviders();
    return () => stopPoll();
  }, []);

  const stopPoll = () => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  };

  // 状态文案
  const statusText: Record<string, string> = {
    NEW: "请用手机阿里云盘 App 扫码",
    SCANED: "已扫描，请在手机上确认",
    CONFIRMED: "✅ 授权成功！",
    EXPIRED: "二维码已过期，请重新获取",
    CANCELED: "已取消",
  };

  const startAliyunQR = async () => {
    setError("");
    setStatus("");
    setQrDataUrl("");
    stopPoll();
    try {
      const sess = await aliyunQR();
      setQrDataUrl(await QRCode.toDataURL(sess.qrContent, { width: 220, margin: 1 }));
      setStatus("NEW");
      // 每 2 秒轮询一次
      pollRef.current = window.setInterval(async () => {
        try {
          const s = await aliyunQRStatus(sess.t, sess.ck);
          setStatus(s);
          if (s === "CONFIRMED") {
            stopPoll();
            loadProviders();
          } else if (s === "EXPIRED" || s === "CANCELED") {
            stopPoll();
          }
        } catch (e) {
          setError(String((e as Error).message || e));
          stopPoll();
        }
      }, 2000);
    } catch (e) {
      setError(String((e as Error).message || e));
    }
  };

  return (
    <div>
      <h2>设置 · 网盘授权</h2>
      {error && <p style={{ color: "#f87171" }}>出错了: {error}</p>}

      <div className="provider-list">
        {providers.map((p) => (
          <div key={p.provider} className="provider-row">
            <div>
              <div className="provider-name">{p.name}</div>
              <div className="sub">
                {p.authMethod === "qrcode" ? "扫码登录" : "Cookie 授权"} ·{" "}
                {p.authorized ? (
                  <span style={{ color: "#4ade80" }}>已授权</span>
                ) : (
                  <span className="muted">未授权</span>
                )}
              </div>
            </div>
            {p.provider === "aliyun" && (
              <button className="tab" onClick={startAliyunQR}>
                {p.authorized ? "重新授权" : "扫码授权"}
              </button>
            )}
            {p.authMethod === "cookie" && (
              <span className="muted" style={{ fontSize: 13 }}>
                (稍后支持)
              </span>
            )}
          </div>
        ))}
      </div>

      {qrDataUrl && (
        <div className="qr-box">
          <img src={qrDataUrl} alt="阿里云盘登录二维码" width={220} height={220} />
          <p style={{ marginTop: 12 }}>{statusText[status] || status}</p>
        </div>
      )}
    </div>
  );
}
