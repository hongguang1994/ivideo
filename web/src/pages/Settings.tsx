import { useEffect, useRef, useState } from "react";
import QRCode from "qrcode";
import {
  aliyunOpenQR,
  aliyunOpenQRStatus,
  aliyunQR,
  aliyunQRStatus,
  checkProvider,
  getProviders,
  pan115QR,
  pan115QRStatus,
  saveProviderToken,
  type HealthResult,
  type Provider,
} from "../api";

const ICONS: Record<string, string> = {
  aliyun: "☁️",
  aliyun_open: "🎞️",
  "115": "📦",
  quark: "🐟",
};

function fmtTime(unix: number): string {
  if (!unix) return "从未";
  const d = new Date(unix * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

type CheckState = "checking" | HealthResult | undefined;

export default function Settings() {
  const [providers, setProviders] = useState<Provider[]>([]);
  const [checks, setChecks] = useState<Record<string, CheckState>>({});
  const [qrDataUrl, setQrDataUrl] = useState("");
  const [qrStatus, setQrStatus] = useState("");
  const [error, setError] = useState("");
  const [saved, setSaved] = useState("");
  const [openToken, setOpenToken] = useState("");
  // 令牌来源。必须和实际签发它的服务一致 —— refresh_token 绑定签发它的应用，
  // 选错了续期时会被对方拒掉。
  const [openType, setOpenType] = useState("alicloud_tv");
  const pollRef = useRef<number | null>(null);

  const loadProviders = () =>
    getProviders()
      .then(setProviders)
      .catch((e) => setError(String(e.message || e)));

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

  // 实测校验某网盘令牌
  const runCheck = async (provider: string) => {
    setChecks((c) => ({ ...c, [provider]: "checking" }));
    try {
      const r = await checkProvider(provider);
      setChecks((c) => ({ ...c, [provider]: r }));
      loadProviders(); // 校验可能刷新了 updatedAt
    } catch (e) {
      setChecks((c) => ({
        ...c,
        [provider]: { healthy: false, message: String((e as Error).message || e) },
      }));
    }
  };

  const saveOpenToken = async () => {
    setError("");
    setSaved("");
    try {
      await saveProviderToken("aliyun_open", openToken.trim(), openType);
      setOpenToken("");
      setSaved("✅ 已保存，原画直链可用了");
      loadProviders();
    } catch (e) {
      setError(String((e as Error).message || e));
    }
  };

  const qrText: Record<string, string> = {
    // 网页版扫码
    NEW: "请用手机阿里云盘 App 扫码",
    SCANED: "已扫描，请在手机上确认",
    CONFIRMED: "✅ 授权成功！",
    EXPIRED: "二维码已过期，请重新获取",
    CANCELED: "已取消",
    // 开放接口扫码
    WaitLogin: "请用手机阿里云盘 App 扫码",
    ScanSuccess: "已扫描，请在手机上确认",
    LoginSuccess: "✅ 授权成功！原画直链可用了",
    QRCodeExpired: "二维码已过期，请重新获取",
    P115_0: "请用手机 115 App 扫码",
    P115_1: "已扫描，请在手机上确认",
    P115_2: "登录成功！转存 cookie 已保存",
    "P115_-1": "二维码已过期，请重新获取",
    "P115_-2": "已取消",
  };

  // 开放接口(原画直链)扫码授权：阿里官方 OAuth，服务端需配 client_id/secret。
  const startOpenQR = async () => {
    setError("");
    setQrStatus("");
    setQrDataUrl("");
    stopPoll();
    try {
      const sess = await aliyunOpenQR();
      setQrDataUrl(await QRCode.toDataURL(sess.qrCodeUrl, { width: 220, margin: 1 }));
      setQrStatus("WaitLogin");
      pollRef.current = window.setInterval(async () => {
        try {
          const s = await aliyunOpenQRStatus(sess.sid);
          setQrStatus(s);
          if (s === "LoginSuccess") {
            stopPoll();
            loadProviders();
          } else if (s === "QRCodeExpired") {
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

  // 115 网页扫码登录（拿 cookie 用于转存分享）。状态：0 等待/1 已扫/2 已确认/负数 过期。
  const startPan115QR = async () => {
    setError("");
    setQrStatus("");
    setQrDataUrl("");
    stopPoll();
    try {
      const sess = await pan115QR();
      setQrDataUrl(await QRCode.toDataURL(sess.qrcode, { width: 220, margin: 1 }));
      setQrStatus("P115_0");
      pollRef.current = window.setInterval(async () => {
        try {
          const st = await pan115QRStatus(sess);
          setQrStatus("P115_" + st);
          if (st === 2) {
            stopPoll();
            loadProviders();
          } else if (st < 0) {
            stopPoll();
          }
        } catch (e) {
          setError(String((e as Error).message || e));
          stopPoll();
        }
      }, 2000);
    } catch (e) {
      setError(String((e as Error).message || e));
      stopPoll();
    }
  };

  const startAliyunQR = async () => {
    setError("");
    setQrStatus("");
    setQrDataUrl("");
    stopPoll();
    try {
      const sess = await aliyunQR();
      setQrDataUrl(await QRCode.toDataURL(sess.qrContent, { width: 220, margin: 1 }));
      setQrStatus("NEW");
      pollRef.current = window.setInterval(async () => {
        try {
          const s = await aliyunQRStatus(sess.t, sess.ck);
          setQrStatus(s);
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

  // 徽章：检测过用检测结果，否则用授权状态
  const badge = (p: Provider) => {
    const chk = checks[p.provider];
    if (chk && chk !== "checking") {
      return chk.healthy ? (
        <span className="badge badge-ok">● 有效</span>
      ) : (
        <span className="badge badge-bad">● 失效</span>
      );
    }
    return p.authorized ? (
      <span className="badge badge-warn">● 已授权</span>
    ) : (
      <span className="badge badge-off">○ 未配置</span>
    );
  };

  return (
    <div>
      <div className="page-head">
        <h1>网盘授权 · 令牌健康</h1>
        <p>管理各网盘的登录凭据，随时检测令牌是否还有效。</p>
      </div>

      {error && (
        <div
          className="panel"
          style={{ borderColor: "rgba(248,113,113,.4)", marginBottom: 16, color: "#fca5a5" }}
        >
          出错了: {error}
        </div>
      )}
      {saved && (
        <div
          className="panel"
          style={{ borderColor: "rgba(74,222,128,.4)", marginBottom: 16, color: "#86efac" }}
        >
          {saved}
        </div>
      )}

      <div className="provider-list" style={{ maxWidth: 740 }}>
        {providers.map((p) => {
          const chk = checks[p.provider];
          const checking = chk === "checking";
          const result = chk && chk !== "checking" ? chk : null;
          return (
            <div key={p.provider} className="provider-row" style={{ flexWrap: "wrap" }}>
              <div style={{ display: "flex", alignItems: "center", gap: 14, flex: "1 1 260px" }}>
                <span style={{ fontSize: 26 }}>{ICONS[p.provider] || "💾"}</span>
                <div>
                  <div className="provider-name">
                    {p.name} {badge(p)}
                  </div>
                  <div className="sub" style={{ marginTop: 4 }}>
                    {p.authMethod === "qrcode"
                      ? "扫码登录"
                      : p.authMethod === "token"
                      ? "令牌授权"
                      : "Cookie 授权"}
                    {" · 更新于 "}
                    {fmtTime(p.updatedAt)}
                  </div>
                  {result && (
                    <div
                      className="sub"
                      style={{ marginTop: 4, color: result.healthy ? "#86efac" : "#fca5a5" }}
                    >
                      检测结果：{result.message}
                    </div>
                  )}
                </div>
              </div>

              <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
                {p.authorized && (
                  <button onClick={() => runCheck(p.provider)} disabled={checking}>
                    {checking ? "检测中…" : "检测"}
                  </button>
                )}
                {p.provider === "aliyun" && (
                  <button className="primary" onClick={startAliyunQR}>
                    {p.authorized ? "重新授权" : "扫码授权"}
                  </button>
                )}
                {p.authMethod === "token" && (
                  <button className="primary" onClick={startOpenQR}>
                    {p.authorized ? "重新扫码" : "扫码授权"}
                  </button>
                )}
                {p.authMethod === "cookie" && p.provider === "115" && (
                  <button className="primary" onClick={startPan115QR}>
                    {p.authorized ? "重新扫码登录" : "扫码登录(转存)"}
                  </button>
                )}
                {p.authMethod === "cookie" && p.provider !== "115" && (
                  <span className="muted" style={{ fontSize: 13 }}>
                    (稍后支持)
                  </span>
                )}
              </div>

              {p.authMethod === "token" && (
                <div style={{ display: "flex", gap: 8, flex: "1 1 100%", marginTop: 8 }}>
                  <select
                    value={openType}
                    onChange={(e) => setOpenType(e.target.value)}
                    style={{ flex: "0 0 180px" }}
                  >
                    <option value="alicloud_tv">oplist · TV版(高配额)</option>
                    <option value="alist">AList 扫码</option>
                    <option value="alicloud_qr">oplist · OAuth2</option>
                  </select>
                  <input
                    placeholder="粘贴开放接口 refresh token"
                    value={openToken}
                    onChange={(e) => setOpenToken(e.target.value)}
                    style={{ flex: 1, minWidth: 160 }}
                  />
                  <button className="primary" onClick={saveOpenToken} disabled={!openToken.trim()}>
                    保存
                  </button>
                </div>
              )}
            </div>
          );
        })}
      </div>

      {qrDataUrl && (
        <div className="qr-box">
          <img src={qrDataUrl} alt="阿里云盘登录二维码" width={220} height={220} />
          <p style={{ marginTop: 12 }}>{qrText[qrStatus] || qrStatus}</p>
        </div>
      )}

      <div className="panel" style={{ maxWidth: 740, marginTop: 22 }}>
        <b>关于「开放接口(原画直链)」</b>
        <p className="muted" style={{ fontSize: 13, lineHeight: 1.7, margin: "8px 0 0" }}>
          用于取<b>原画直链</b>。阿里<b>按应用(client_id)限速</b>，令牌从哪家拿的直接决定下载速度。
          同一部片实测：
        </p>
        <ul className="muted" style={{ fontSize: 13, lineHeight: 1.8, margin: "6px 0 0 18px" }}>
          <li>
            <b>oplist · TV版</b> —— 高配额，<b>推荐</b>。够喂高码率片源
          </li>
          <li>AList / oplist · OAuth2 —— 约 0.5 MB/s(4 Mbps)，只够低码率片源</li>
        </ul>
        <p className="muted" style={{ fontSize: 13, lineHeight: 1.7, margin: "8px 0 0" }}>
          取 TV 版 token：打开{" "}
          <a
            href="https://api.oplist.org"
            target="_blank"
            rel="noreferrer"
            style={{ color: "var(--accent-2)" }}
          >
            api.oplist.org
          </a>{" "}
          → 选「阿里云盘 (Client) TV版扫码」→ 勾「使用 OpenList 提供的参数」→ 扫码 → 复制 Refresh
          Token 粘到上面，<b>类型必须选对</b>（续期要回到签发它的那家，选错会被拒）。
        </p>
        <p className="muted" style={{ fontSize: 13, lineHeight: 1.7, margin: "8px 0 0" }}>
          原画喂不动的片会<b>自动降级到转码流</b>（阈值 <code>stream.original_max_mbps</code>）。
          换上高配额令牌后可把阈值调大，让更多片走原画。
        </p>
      </div>
    </div>
  );
}
