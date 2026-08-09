import { useEffect, useRef, useState } from "react";
import QRCode from "qrcode";
import {
  Activity,
  AlertCircle,
  Box,
  Check,
  CheckCircle2,
  ChevronRight,
  Circle,
  CircleHelp,
  Cloud,
  Database,
  Download,
  ExternalLink,
  Film,
  FolderHeart,
  Gauge,
  HardDrive,
  Info,
  KeyRound,
  LoaderCircle,
  Play,
  QrCode,
  RefreshCw,
  Save,
  ScrollText,
  Server,
  SlidersHorizontal,
  X,
} from "lucide-react";
import { Link } from "react-router-dom";
import {
  aliyunQR,
	aliyunQRStatus,
	checkProvider,
	getProviders,
	getJellyfinSetup,
	getMetadataStatus,
	initializeJellyfin,
	saveMetadataToken,
	scrapeMetadata,
  pan115QR,
  pan115QRStatus,
  quarkQR,
  quarkQRStatus,
  saveProviderToken,
  type HealthResult,
  type JellyfinBootstrapResult,
  type JellyfinSetupStatus,
	type MetadataResult,
	type MetadataStatus,
	type Provider,
} from "../api";

function ProviderIcon({ provider }: { provider: string }) {
  switch (provider) {
    case "aliyun": return <Cloud size={20} />;
    case "aliyun_open_tv":
    case "aliyun_open_oauth": return <Film size={20} />;
    case "115": return <Box size={20} />;
    case "quark": return <HardDrive size={20} />;
    default: return <Database size={20} />;
  }
}

function fmtTime(unix: number): string {
  if (!unix) return "从未";
  const d = new Date(unix * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

type CheckState = "checking" | HealthResult | undefined;

type OpenTokenType = "alicloud_tv" | "alicloud_qr";

type ProviderView = Provider & {
  key: string;
  backendProvider: string;
  help: string;
  tokenType?: OpenTokenType;
};

const OPLIST_TOKEN_URL = "https://api.oplist.org.cn/";

const providerHelp: Record<string, string> = {
  aliyun: "用于浏览阿里云盘、访问分享内容并把资源转存到自己的网盘。点击扫码授权后，使用阿里云盘 App 确认登录。",
  aliyun_open_tv: "用于获取高配额原画直链，适合高码率视频。点击“获取 Token”，在 OPLIST 选择阿里云盘（Client）TV 版扫码，完成登录后复制 Refresh Token。",
  aliyun_open_oauth: "用于通过阿里开放接口获取原画直链。点击“获取 Token”，在 OPLIST 选择 OAuth2 授权，完成登录后复制 Refresh Token。它与 TV 版共用一个原画通道，保存后会切换当前通道。",
  "115": "用于浏览 115 分享并把资源转存到自己的网盘。扫码登录后，系统会保存转存所需的登录凭据。",
  quark: "用于浏览夸克分享并把资源转存到自己的网盘。扫码登录后，系统会保存访问分享所需的登录凭据。",
};

type SettingsSection = "overview" | "providers" | "jellyfin" | "metadata";

export default function Settings({ section = "overview" }: { section?: SettingsSection }) {
  const [providers, setProviders] = useState<Provider[]>([]);
  const [checks, setChecks] = useState<Record<string, CheckState>>({});
  const [qrDataUrl, setQrDataUrl] = useState("");
  const [qrStatus, setQrStatus] = useState("");
  const [error, setError] = useState("");
  const [saved, setSaved] = useState("");
  const [openTokens, setOpenTokens] = useState<Record<OpenTokenType, string>>({
    alicloud_tv: "",
    alicloud_qr: "",
  });
  const [openInfo, setOpenInfo] = useState<string | null>(null);
  const [jellyfin, setJellyfin] = useState<JellyfinSetupStatus | null>(null);
  const [jellyfinResult, setJellyfinResult] = useState<JellyfinBootstrapResult | null>(null);
  const [initializingJellyfin, setInitializingJellyfin] = useState(false);
	const [metadata, setMetadata] = useState<MetadataStatus | null>(null);
	const [metadataToken, setMetadataToken] = useState("");
	const [metadataResult, setMetadataResult] = useState<MetadataResult | null>(null);
	const [savingMetadata, setSavingMetadata] = useState(false);
	const [scrapingMetadata, setScrapingMetadata] = useState(false);
  const pollRef = useRef<number | null>(null);
	const metadataPollRef = useRef<number | null>(null);

  const loadProviders = () =>
    getProviders()
      .then(setProviders)
      .catch((e) => setError(String(e.message || e)));

  useEffect(() => {
    loadProviders();
		getJellyfinSetup().then(setJellyfin).catch((e) => setError(String(e.message || e)));
		getMetadataStatus().then(setMetadata).catch((e) => setError(String(e.message || e)));
    return () => stopPoll();
  }, []);

	useEffect(() => {
		if (!metadata?.running) return;
		metadataPollRef.current = window.setInterval(async () => {
			try {
				const status = await getMetadataStatus();
				setMetadata(status);
				if (!status.running) {
					if (metadataPollRef.current) clearInterval(metadataPollRef.current);
					metadataPollRef.current = null;
					if (status.lastResult) setMetadataResult(status.lastResult);
					if (status.lastError) setError(status.lastError);
					else setSaved("媒体资料已写入，Jellyfin 正在重新扫描");
				}
			} catch (e) { setError(String((e as Error).message || e)); }
		}, 2000);
		return () => {
			if (metadataPollRef.current) clearInterval(metadataPollRef.current);
			metadataPollRef.current = null;
		};
	}, [metadata?.running]);

  const setupJellyfin = async () => {
    setError("");
    setJellyfinResult(null);
    setInitializingJellyfin(true);
    try {
      const result = await initializeJellyfin();
      setJellyfinResult(result);
      setJellyfin(await getJellyfinSetup());
    } catch (e) {
      setError(String((e as Error).message || e));
    } finally {
      setInitializingJellyfin(false);
    }
  };

	const saveTMDb = async () => {
		setError(""); setSaved(""); setSavingMetadata(true);
		try {
			await saveMetadataToken(metadataToken.trim());
			setMetadataToken("");
			setMetadata(await getMetadataStatus());
			setSaved("TMDb Token 已校验并保存");
		} catch (e) { setError(String((e as Error).message || e)); }
		finally { setSavingMetadata(false); }
	};

	const runMetadataScrape = async () => {
		setError(""); setMetadataResult(null); setScrapingMetadata(true);
		try {
			setMetadata(await scrapeMetadata());
		} catch (e) { setError(String((e as Error).message || e)); }
		finally { setScrapingMetadata(false); }
	};

  const stopPoll = () => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  };

  // 实测校验某网盘令牌
  const runCheck = async (key: string, provider: string) => {
    setChecks((c) => ({ ...c, [key]: "checking" }));
    try {
      const r = await checkProvider(provider);
      setChecks((c) => ({ ...c, [key]: r }));
      loadProviders(); // 校验可能刷新了 updatedAt
    } catch (e) {
      setChecks((c) => ({
        ...c,
        [key]: {
          healthy: false,
          playable: false,
          speedMbps: 0,
          sampleBytes: 0,
          durationMs: 0,
          checkedAt: Math.floor(Date.now() / 1000),
          message: String((e as Error).message || e),
        },
      }));
    }
  };

  const saveOpenToken = async (tokenType: OpenTokenType) => {
    setError("");
    setSaved("");
    try {
      await saveProviderToken("aliyun_open", openTokens[tokenType].trim(), tokenType);
      setOpenTokens((tokens) => ({ ...tokens, [tokenType]: "" }));
      setSaved(`${tokenType === "alicloud_tv" ? "TV 版" : "OAuth2"} Token 已保存并设为当前原画通道`);
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
    QuarkWait: "请用夸克 App 扫码登录",
    QuarkOK: "✅ 夸克已登录，cookie 已保存",
    P115_0: "请用手机 115 App 扫码",
    P115_1: "已扫描，请在手机上确认",
    P115_2: "登录成功！转存 cookie 已保存",
    "P115_-1": "二维码已过期，请重新获取",
    "P115_-2": "已取消",
  };

  // 115 网页扫码登录（拿 cookie 用于转存分享）。状态：0 等待/1 已扫/2 已确认/负数 过期。
  // 夸克扫码登录：拿网页 cookie（夸克开放 API 需 secret 签名，走不通）
  const startQuarkQR = async () => {
    setError("");
    setQrStatus("");
    setQrDataUrl("");
    stopPoll();
    try {
      const sess = await quarkQR();
      setQrDataUrl(await QRCode.toDataURL(sess.qrcode, { width: 220, margin: 1 }));
      setQrStatus("QuarkWait");
      pollRef.current = window.setInterval(async () => {
        try {
          const st = await quarkQRStatus(sess);
          if (st === 2000000) {
            setQrStatus("QuarkOK");
            stopPoll();
            loadProviders();
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
        <span className="badge badge-ok"><CheckCircle2 size={12} />有效</span>
      ) : (
        <span className="badge badge-bad"><AlertCircle size={12} />失效</span>
      );
    }
    if (p.diagnostic) {
      return p.diagnostic.tokenHealthy ? (
        <span className="badge badge-ok"><CheckCircle2 size={12} />有效</span>
      ) : (
        <span className="badge badge-bad"><AlertCircle size={12} />失效</span>
      );
    }
    return p.authorized ? (
      <span className="badge badge-warn"><Check size={12} />已授权</span>
    ) : (
      <span className="badge badge-off"><Circle size={11} />未配置</span>
    );
  };

  const providerViews: ProviderView[] = providers.flatMap((p) => {
    if (p.provider !== "aliyun_open") {
      return [{
        ...p,
        key: p.provider,
        backendProvider: p.provider,
        help: providerHelp[p.provider],
      }];
    }

    return ([
      ["aliyun_open_tv", "阿里云盘 · TV 版原画", "alicloud_tv"],
      ["aliyun_open_oauth", "阿里云盘 · OAuth2 原画", "alicloud_qr"],
    ] as const).map(([key, name, tokenType]) => {
      const active = p.authorized && p.extra === tokenType;
      return {
        ...p,
        provider: key,
        key,
        name,
        backendProvider: "aliyun_open",
        tokenType,
        authorized: active,
        diagnostic: active ? p.diagnostic : undefined,
        help: providerHelp[key],
      };
    });
  });

  const authorizedCount = providerViews.filter((p) => p.authorized).length;
  const healthyCount = providerViews.filter((p) => {
    const check = checks[p.provider];
    if (check && check !== "checking") return check.healthy;
    return p.diagnostic?.tokenHealthy ?? false;
  }).length;

  const headings = {
    overview: ["设置概览", "分别管理连接、媒体服务和分享库偏好。"],
    providers: ["网盘授权", "管理登录凭据、令牌状态和原画链路测速。"],
    jellyfin: ["Jellyfin 设置", "管理 ivideo 与 Jellyfin 的连接和首次初始化。"],
    metadata: ["媒体刮削", "配置 TMDb 并管理后端媒体资料任务。"],
  } as const;

  return (
    <div className="settings-page settings-detail-page">
      <div className="page-head">
        <h1>{headings[section][0]}</h1>
        <p>{headings[section][1]}</p>
      </div>

      {section === "overview" && <div className="settings-overview" aria-label="服务状态概览">
        <div className="overview-item">
          <KeyRound size={17} />
          <div><strong>{authorizedCount}/{providerViews.length || 5}</strong><span>连接已授权</span></div>
        </div>
        <div className="overview-item">
          <Activity size={17} />
          <div><strong>{healthyCount}</strong><span>连接健康</span></div>
        </div>
        <div className="overview-item">
          <Server size={17} />
          <div><strong>{jellyfin?.connected ? "已连接" : "待连接"}</strong><span>Jellyfin</span></div>
        </div>
        <div className="overview-item">
          <Database size={17} />
          <div><strong>{metadata?.configured ? "已配置" : "待配置"}</strong><span>TMDb 刮削</span></div>
        </div>
      </div>}

      {error && (
        <div
          className="settings-notice notice-error"
        >
          出错了: {error}
        </div>
      )}
      {saved && (
        <div
          className="settings-notice notice-success"
        >
          {saved}
        </div>
      )}

      {section === "overview" && (
        <div className="settings-hub-list">
          <Link to="/settings/accounts" className="settings-hub-item">
            <span className="settings-hub-icon"><KeyRound size={20} /></span>
            <span><strong>网盘授权</strong><small>{authorizedCount}/{providerViews.length || 5} 个连接已授权</small></span>
            <ChevronRight size={19} />
          </Link>
          <Link to="/settings/jellyfin" className="settings-hub-item">
            <span className="settings-hub-icon"><Server size={20} /></span>
            <span><strong>Jellyfin</strong><small>{jellyfin?.connected ? "连接正常" : "等待连接"}</small></span>
            <ChevronRight size={19} />
          </Link>
          <Link to="/settings/metadata" className="settings-hub-item">
            <span className="settings-hub-icon"><Database size={20} /></span>
            <span><strong>媒体刮削</strong><small>{metadata?.configured ? "TMDb 已配置" : "等待配置 TMDb"}</small></span>
            <ChevronRight size={19} />
          </Link>
          <Link to="/settings/import" className="settings-hub-item">
            <span className="settings-hub-icon"><Download size={20} /></span>
            <span><strong>资源导入</strong><small>定时导入分享内容并查看任务进度</small></span>
            <ChevronRight size={19} />
          </Link>
          <Link to="/settings/shares" className="settings-hub-item">
            <span className="settings-hub-icon"><FolderHeart size={20} /></span>
            <span><strong>分享库管理</strong><small>收藏分享、浏览和导入资源</small></span>
            <ChevronRight size={19} />
          </Link>
          <Link to="/settings/share-preferences" className="settings-hub-item">
            <span className="settings-hub-icon"><SlidersHorizontal size={20} /></span>
            <span><strong>分享偏好</strong><small>默认网盘、分类与转存目录</small></span>
            <ChevronRight size={19} />
          </Link>
          <Link to="/settings/logs" className="settings-hub-item">
            <span className="settings-hub-icon"><ScrollText size={20} /></span>
            <span><strong>运行日志</strong><small>后端实时日志与运行事件</small></span>
            <ChevronRight size={19} />
          </Link>
        </div>
      )}

      {section === "providers" && <section className="settings-section provider-section">
        <div className="settings-section-head">
          <div>
            <h2>网盘连接</h2>
            <p>授权状态、令牌诊断与原画链路测速</p>
          </div>
          <KeyRound size={19} />
        </div>
        <div className="provider-list settings-provider-grid">
        {providerViews.map((p) => {
          const chk = checks[p.provider];
          const checking = chk === "checking";
          const result = chk && chk !== "checking"
            ? chk
            : p.diagnostic
              ? {
                  healthy: p.diagnostic.tokenHealthy,
                  playable: p.diagnostic.playable,
                  speedMbps: p.diagnostic.speedMbps,
                  sampleBytes: p.diagnostic.sampleBytes,
                  durationMs: p.diagnostic.durationMs,
                  checkedAt: p.diagnostic.checkedAt,
                  message: p.diagnostic.message,
                }
              : null;
          return (
            <div key={p.key} className={`provider-row provider-${p.provider}`}>
              <div className="provider-identity">
                <span className="provider-icon"><ProviderIcon provider={p.provider} /></span>
                <div className="provider-copy">
                  <div className="provider-name">
                    <span>{p.name}</span>
                    {badge(p)}
                    <button
                      className="provider-info-button"
                      title="查看授权说明"
                      aria-label={`查看${p.name}授权说明`}
                      aria-expanded={openInfo === p.key}
                      onClick={() => setOpenInfo((current) => current === p.key ? null : p.key)}
                    >
                      <CircleHelp size={16} />
                    </button>
                  </div>
                  <div className="sub provider-meta">
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
                      className={`sub provider-result ${result.healthy ? "result-ok" : "result-bad"}`}
                    >
                      {result.message} · 检测于 {fmtTime(result.checkedAt)}
                    </div>
                  )}
                </div>
              </div>

              <div className="provider-actions">
                {p.authorized && (
                  <button onClick={() => runCheck(p.key, p.backendProvider)} disabled={checking}>
                    {checking ? <LoaderCircle className="spin" size={15} /> : p.tokenType ? <Gauge size={15} /> : <Activity size={15} />}
                    {checking ? "检测中" : p.tokenType ? "检测测速" : "检测"}
                  </button>
                )}
                {p.provider === "aliyun" && (
                  <button className="primary" onClick={startAliyunQR}>
                    <QrCode size={15} />
                    {p.authorized ? "重新授权" : "扫码授权"}
                  </button>
                )}
                {p.tokenType && (
                  <a className="btn btn-primary" href={OPLIST_TOKEN_URL} target="_blank" rel="noreferrer">
                    <ExternalLink size={15} />
                    获取 Token
                  </a>
                )}
                {p.authMethod === "cookie" && p.provider === "115" && (
                  <button className="primary" onClick={startPan115QR}>
                    <QrCode size={15} />
                    {p.authorized ? "重新扫码登录" : "扫码登录(转存)"}
                  </button>
                )}
                {p.authMethod === "cookie" && p.provider === "quark" && (
                  <button className="primary" onClick={startQuarkQR}>
                    <QrCode size={15} />
                    {p.authorized ? "重新扫码登录" : "扫码登录"}
                  </button>
                )}
                {p.authMethod === "cookie" && p.provider !== "115" && p.provider !== "quark" && (
                  <span className="muted" style={{ fontSize: 13 }}>
                    (稍后支持)
                  </span>
                )}
              </div>

              {openInfo === p.key && (
                <div className="provider-help" role="status">
                  <Info size={16} />
                  <p>{p.help}</p>
                </div>
              )}

              {p.tokenType && (
                <div className="provider-token-editor">
                  <div className="provider-token-form">
                  <input
                    type="password"
                    aria-label={`粘贴${p.name} Refresh Token`}
                    placeholder={`粘贴${p.tokenType === "alicloud_tv" ? "TV 版" : "OAuth2"} Refresh Token`}
                    value={openTokens[p.tokenType]}
                    onChange={(e) => setOpenTokens((tokens) => ({ ...tokens, [p.tokenType!]: e.target.value }))}
                  />
                  <button className="primary" onClick={() => saveOpenToken(p.tokenType!)} disabled={!openTokens[p.tokenType].trim()}>
                    <Save size={15} />保存
                  </button>
                  </div>
                </div>
              )}
            </div>
          );
        })}
        </div>
      </section>}

      {section === "jellyfin" && jellyfin && (
        <section className="settings-section service-section">
          <div className="service-heading">
            <div>
              <div className="service-title"><Server size={18} />Jellyfin</div>
              <div className="sub" style={{ marginTop: 4 }}>
                {jellyfin.connected ? "ivideo 专用连接已就绪" : jellyfin.canInitialize ? "等待首次初始化" : "已初始化，尚未连接 ivideo"}
              </div>
            </div>
            {jellyfin.canInitialize && (
              <button className="primary" onClick={setupJellyfin} disabled={initializingJellyfin}>
                {initializingJellyfin ? <LoaderCircle className="spin" size={15} /> : <Play size={15} />}
                {initializingJellyfin ? "初始化中…" : "初始化 Jellyfin"}
              </button>
            )}
          </div>
          {jellyfinResult && (
            <div style={{ marginTop: 14, paddingTop: 14, borderTop: "1px solid var(--line)" }}>
              <div className="sub">管理员用户名</div>
              <div style={{ marginTop: 4, fontWeight: 650 }}>{jellyfinResult.username}</div>
              <div className="sub" style={{ marginTop: 12 }}>一次性初始密码</div>
              <code style={{ display: "block", marginTop: 4, padding: "9px 10px", background: "var(--surface-soft)", borderRadius: 6, overflowWrap: "anywhere" }}>
                {jellyfinResult.password}
              </code>
            </div>
          )}
        </section>
      )}

      {section === "metadata" && metadata && (
			<section className="settings-section service-section">
				<div className="service-heading">
					<div>
						<div className="service-title"><Database size={18} />媒体资料刮削</div>
						<div className="sub" style={{ marginTop: 4 }}>
							{metadata.configured ? "后端使用 TMDb 生成 NFO、海报和分集资料" : "配置 TMDb 后由 ivideo 后端独立刮削"}
						</div>
					</div>
					{metadata.configured && (
						<button className="primary" onClick={runMetadataScrape} disabled={scrapingMetadata || metadata.running}>
							{scrapingMetadata || metadata.running ? <LoaderCircle className="spin" size={15} /> : <RefreshCw size={15} />}
							{scrapingMetadata || metadata.running ? "刮削中…" : "刮削全部"}
						</button>
					)}
				</div>
				<div className="metadata-token-form">
					<input
						type="password"
						placeholder="TMDb API Read Access Token"
						value={metadataToken}
						onChange={(e) => setMetadataToken(e.target.value)}
					/>
					<button onClick={saveTMDb} disabled={!metadataToken.trim() || savingMetadata}>
						{savingMetadata ? <LoaderCircle className="spin" size={15} /> : <Save size={15} />}
						{savingMetadata ? "校验中…" : metadata.configured ? "更新 Token" : "保存 Token"}
					</button>
				</div>
				{metadataResult && (
					<div className="sub" style={{ marginTop: 12 }}>
						完成 {metadataResult.items} 部，{metadataResult.episodes} 集，下载 {metadataResult.images} 张海报，跳过 {metadataResult.skipped} 项
						{metadataResult.errors?.length ? `；${metadataResult.errors.slice(0, 3).join("；")}` : ""}
					</div>
				)}
				<div className="metadata-organizer-link">
					<div>
						<div className="service-title"><SlidersHorizontal size={18} />媒体整理</div>
						<div className="sub" style={{ marginTop: 4 }}>按作品分组核对名称、候选资料和发布状态，待确认内容不会进入 Jellyfin 正式媒体库。</div>
					</div>
					<Link className="btn" to="/settings/organizer">
						打开媒体整理 <ChevronRight size={16} />
					</Link>
				</div>
			</section>
		)}
      {section === "providers" && qrDataUrl && (
        <div className="qr-overlay" role="dialog" aria-modal="true" aria-label="扫码授权">
          <div className="qr-box">
          <button className="icon-button qr-close" title="关闭" onClick={() => { stopPoll(); setQrDataUrl(""); setQrStatus(""); }}><X size={18} /></button>
          <img src={qrDataUrl} alt="阿里云盘登录二维码" width={220} height={220} />
          <p>{qrText[qrStatus] || qrStatus}</p>
          </div>
        </div>
      )}

    </div>
  );
}
