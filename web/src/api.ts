// 后端 API 封装。所有接口统一前缀 /api/v1，统一返回结构 {code, msg, data}。
// 开发时经 Vite 代理，生产经 nginx 反代。

const BASE = "/api/v1";

// apiFetch 统一发请求并拆包：成功返回 data，失败抛出 msg。
async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(BASE + path, init);
  let body: { code?: number; msg?: string; data?: unknown } = {};
  try {
    body = await res.json();
  } catch {
    // 非 JSON 响应
  }
  if (!res.ok) {
    throw new Error(body.msg || `请求失败: ${res.status}`);
  }
  return body.data as T;
}

// post 是带 JSON body 的 POST 简写。
function post<T>(path: string, data?: unknown): Promise<T> {
  return apiFetch<T>(path, {
    method: "POST",
    headers: data !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: data !== undefined ? JSON.stringify(data) : undefined,
  });
}

// ---- 直读源(OpenList / Jellyfin)----

export type Source = "openlist" | "jellyfin";

export interface VideoItem {
  source: Source;
  name: string;
  path?: string;
  id?: string;
  isDir: boolean;
  size?: number;
  modified?: string;
  poster?: string;
  overview?: string;
  year?: number;
  streamUrl?: string;
}

export interface ListResp {
  source: Source;
  path?: string;
  items: VideoItem[];
}

export interface Health {
  status: string;
  sources: Source[];
  jellyfin: boolean;
}

export function getHealth(): Promise<Health> {
  return apiFetch<Health>("/health");
}

// 列出视频。OpenList 传 path 做层级浏览；Jellyfin 忽略 path。
export function listVideos(source: Source, path = "/"): Promise<ListResp> {
  const params = new URLSearchParams({ source });
  if (source === "openlist") params.set("path", path);
  return apiFetch<ListResp>(`/videos?${params.toString()}`);
}

// ---- 资源库 / 按需转存 ----

export interface Resource {
  id: number;
  title: string;
  poster?: string;
  overview?: string;
  provider: string;
  shareUrl: string;
  sharePwd?: string;
  filePath?: string;
}

export async function getResources(): Promise<Resource[]> {
  const d = await apiFetch<{ items: Resource[] }>("/resources");
  return d.items || [];
}

export function addResource(r: Partial<Resource>): Promise<Resource> {
  return post<Resource>("/resources", r);
}

export interface PlayResp {
  status: "uncached" | "transferring" | "ready" | "failed" | "cleaned";
  streamUrl?: string;
  type?: "hls" | "direct";
  message?: string;
}

// 触发/查询转存;就绪返回 streamUrl。
export function playResource(id: number): Promise<PlayResp> {
  return apiFetch<PlayResp>(`/play?resource=${id}`);
}

export interface StrmResult {
  total: number;
  written: number;
  unchanged: number;
  removed: number;
  errors?: string[];
  mediaDir: string;
  siteUrl: string;
}

// 全量重建 strm 媒体库(给 Emby/Jellyfin 扫描)。
export function generateStrm(): Promise<StrmResult> {
  return post<StrmResult>("/strm/generate");
}

// ---- 分享浏览(只读列目录,不涉及转存/播放)----

export interface ShareEntry {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
}

export async function browseShare(
  shareUrl: string,
  sharePwd = "",
  path = "",
  provider = "aliyun"
): Promise<ShareEntry[]> {
  const params = new URLSearchParams({ shareUrl, provider });
  if (sharePwd) params.set("sharePwd", sharePwd);
  if (path) params.set("path", path);
  const d = await apiFetch<{ items: ShareEntry[] }>(`/share/browse?${params.toString()}`);
  return d.items || [];
}

// 手动转存分享内某文件/文件夹到自己阿里盘的指定目录(默认 ivideo,永久留存)。
export function saveShareItem(args: {
  shareUrl: string;
  sharePwd?: string;
  path: string;
  targetFolder?: string;
  provider?: string;
}): Promise<unknown> {
  return post("/share/save", args);
}

// ---- 网盘授权 / 设置 ----

export interface Provider {
  provider: string;
  name: string;
  authMethod: "qrcode" | "cookie" | "token";
  authorized: boolean;
  extra?: string;
  updatedAt: number; // 上次授权/更新时间(unix 秒,0=从未)
  diagnostic?: ProviderDiagnostic;
}

export interface ProviderDiagnostic {
  provider: string;
  tokenHealthy: boolean;
  playable: boolean;
  speedMbps: number;
  sampleBytes: number;
  durationMs: number;
  checkedAt: number;
  message: string;
}

export async function getProviders(): Promise<Provider[]> {
  const d = await apiFetch<{ providers: Provider[] }>("/settings/providers");
  return d.providers;
}

export interface HealthResult {
  healthy: boolean;
  playable: boolean;
  speedMbps: number;
  sampleBytes: number;
  durationMs: number;
  checkedAt: number;
  message: string;
}

// 实测校验某网盘令牌是否仍有效(真去 ping 网盘)。
export function checkProvider(provider: string): Promise<HealthResult> {
  return post<HealthResult>("/settings/providers/check", { provider });
}

export interface CacheEntry {
  resourceId: number;
  title: string;
  size: number;
  lastAccess: number;
  status: string;
}
export interface CacheList {
  items: CacheEntry[];
  totalCount: number;
  totalBytes: number;
}

// 已缓存(转存进自己网盘)的资源列表 + 总量。
export function getCacheItems(): Promise<CacheList> {
  return apiFetch<CacheList>("/cache");
}

// 手动删除某资源的缓存(释放自己网盘空间)。
export function evictCache(resource: number): Promise<unknown> {
  return post("/cache/evict", { resource });
}

// ---- 分享库(收藏的网盘分享)----

export interface Share {
  id: number;
  provider: string;
  shareUrl: string;
  sharePwd: string;
  shareId: string;
  title: string;
  remark: string;
  category: string;
  status: string; // unknown / valid / invalid
  lastCheckedAt: number;
  fileCount: number;
  totalSize: number;
  createdAt: number;
  updatedAt: number;
}

export async function getShares(): Promise<Share[]> {
  const d = await apiFetch<{ shares: Share[] }>("/shares");
  return d.shares || [];
}

export function addShare(s: Partial<Share>): Promise<Share> {
  return post<Share>("/shares", s);
}

export interface BatchShareInput {
  provider: string;
  shareUrl: string;
  sharePwd?: string;
  title?: string;
  category?: string;
}

export interface BatchShareResult {
  index: number;
  id?: number;
  provider: string;
  shareUrl: string;
  status: "added" | "duplicate" | "failed";
  message?: string;
}

export interface BatchShareResponse {
  added: number;
  duplicates: number;
  failed: number;
  results: BatchShareResult[];
}

export function addSharesBatch(items: BatchShareInput[]): Promise<BatchShareResponse> {
  return post<BatchShareResponse>("/shares/batch", { items });
}

export interface GitHubResourceSyncResponse {
  discovered: number;
  added: number;
  existing: number;
}

export function syncGitHubResources(): Promise<GitHubResourceSyncResponse> {
  return post<GitHubResourceSyncResponse>("/search/github/resources/sync");
}

export interface ShareCheckResponse {
	started?: boolean;
	message?: string;
	checked?: number;
	valid?: number;
	invalid?: number;
}

export function checkAllShares(): Promise<ShareCheckResponse> {
  return post<ShareCheckResponse>("/shares/check");
}

export function checkShare(id: number): Promise<{ status: string; message?: string }> {
  return post<{ status: string; message?: string }>(`/shares/${id}/check`);
}

export interface SearchResource {
  provider: string;
  shareUrl: string;
  sharePwd: string;
  title: string;
  resourceType?: string;
  fileName?: string;
  updatedAt?: string;
  source: string;
  sourceName?: string;
  repository: string;
  path: string;
  sourceUrl: string;
  score?: number;
  sources?: string[];
}

export interface SearchSourceReport {
  id: string;
  name: string;
  healthy: boolean;
  resultCount: number;
  scanned: number;
  durationMs: number;
  error?: string;
}

export interface SearchMeta {
  source: string;
  scanned: number;
  remaining: number;
  resetAt: number;
  durationMs?: number;
  cached?: boolean;
  sources?: SearchSourceReport[];
  warnings?: string[];
}

export interface SearchResponse {
  items: SearchResource[];
  meta: SearchMeta;
  jobId?: string;
  query?: string;
  pending?: boolean;
}

export function searchResources(query: string): Promise<SearchResponse> {
  return apiFetch<SearchResponse>(`/search/resources?q=${encodeURIComponent(query)}`);
}

export interface GitHubResourceSource {
  id: string;
  name: string;
  repository: string;
  url: string;
  parser: string;
  description: string;
  configured: boolean;
}

export function getGitHubSources(): Promise<{ items: GitHubResourceSource[] }> {
  return apiFetch<{ items: GitHubResourceSource[] }>("/search/github/sources");
}

export interface GitHubResourceListResponse {
  items: SearchResource[];
  total: number;
  page: number;
  pageSize: number;
  meta: SearchMeta;
}

export function getGitHubResources(page = 1, pageSize = 50): Promise<GitHubResourceListResponse> {
  return apiFetch<GitHubResourceListResponse>(`/search/github/resources?page=${page}&pageSize=${pageSize}`);
}

export interface SearchSettingsStatus {
  githubConfigured: boolean;
  updatedAt: number;
  engine?: {
    name: string;
    cacheMinutes: number;
    maxResults: number;
    sources: Array<{
      id: string;
      name: string;
      priority: number;
      lastHealthy: boolean;
      lastError?: string;
      lastDurationMs: number;
      successCount: number;
      failureCount: number;
      lastCheckedAt: number;
    }>;
  };
}

export function getSearchSettings(): Promise<SearchSettingsStatus> {
  return apiFetch<SearchSettingsStatus>("/settings/search");
}

export function saveGitHubToken(token: string): Promise<SearchSettingsStatus> {
  return post<SearchSettingsStatus>("/settings/search/github", { token });
}

export function deleteGitHubToken(): Promise<SearchSettingsStatus> {
  return apiFetch<SearchSettingsStatus>("/settings/search/github", { method: "DELETE" });
}

export function updateShare(id: number, s: Partial<Share>): Promise<unknown> {
  return apiFetch(`/shares/${id}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(s),
  });
}

export function deleteShare(id: number): Promise<unknown> {
  return apiFetch(`/shares/${id}`, { method: "DELETE" });
}

export interface ImportResult {
  added: number;
  skipped: number;
  errors?: string[];
}

// 把一个分享里的视频递归导入成资源(不转存,只登记来源)。
export function importShare(
  shareUrl: string,
  sharePwd: string,
  provider = "aliyun",
  path = ""
): Promise<ImportResult> {
  return post<ImportResult>("/resources/import", { shareUrl, sharePwd, provider, path });
}

export interface ImportSchedule {
  enabled: boolean;
  intervalMinutes: number;
}

export interface ImportTaskStatus {
  running: boolean;
  total: number;
  processed: number;
  imported: number;
  skipped: number;
  failed: number;
  current: string;
  lastError: string;
  startedAt: number;
  finishedAt: number;
}

export interface ImportSettingsStatus {
  schedule: ImportSchedule;
  status: ImportTaskStatus;
}

export function getImportSettings(): Promise<ImportSettingsStatus> {
  return apiFetch<ImportSettingsStatus>("/settings/import");
}

export function saveImportSettings(schedule: ImportSchedule): Promise<ImportSettingsStatus> {
  return apiFetch<ImportSettingsStatus>("/settings/import", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(schedule),
  });
}

export function runImportTask(): Promise<{ started: boolean; status: ImportTaskStatus }> {
  return post<{ started: boolean; status: ImportTaskStatus }>("/imports/run");
}

// 保存某网盘凭据(阿里开放接口 refresh token / 115、夸克 cookie)。
export function saveProviderToken(provider: string, token: string, extra?: string): Promise<unknown> {
  return post("/settings/token", { provider, token, extra });
}

export interface JellyfinSetupStatus {
  ready: boolean;
  firstUser: string;
  connected: boolean;
  canInitialize: boolean;
  baseUrl: string;
}

export interface JellyfinBootstrapResult {
  username: string;
  password: string;
  baseUrl: string;
}

export function getJellyfinSetup(): Promise<JellyfinSetupStatus> {
  return apiFetch<JellyfinSetupStatus>("/settings/jellyfin");
}

export function initializeJellyfin(): Promise<JellyfinBootstrapResult> {
  return post<JellyfinBootstrapResult>("/settings/jellyfin/initialize");
}

export interface MetadataStatus {
  configured: boolean;
  running: boolean;
	startedAt?: number;
	finishedAt?: number;
	lastResult?: MetadataResult;
	lastError?: string;
}

export interface MetadataResult {
  items: number;
  episodes: number;
  images: number;
  skipped: number;
  errors?: string[];
}

export function getMetadataStatus(): Promise<MetadataStatus> {
  return apiFetch<MetadataStatus>("/settings/metadata");
}

export function saveMetadataToken(token: string): Promise<MetadataStatus> {
  return post<MetadataStatus>("/settings/metadata/token", { token });
}

export function scrapeMetadata(): Promise<MetadataStatus> {
	return post<MetadataStatus>("/metadata/scrape");
}

export interface MediaGroup {
  id: number;
  rawTitle: string;
  normalizedTitle: string;
  mediaKind: string;
  suggestedLibrary: string;
  year: number;
  status: string;
  decisionSource: string;
  selectedSource: string;
  selectedId: string;
  canonicalTitle: string;
  confidence: number;
  reason: string;
  updatedAt: number;
}

export interface MediaGroupMember {
  resourceId: number;
  season: number;
  episode: number;
  confidence: number;
  resource: { id: number; title: string; filePath: string; provider: string };
}

export interface MediaCandidate {
  id: number;
  source: string;
  providerId: string;
  title: string;
  originalTitle: string;
  year: number;
  mediaKind: string;
  library: string;
  score: number;
  evidenceJson: string;
}

export interface MediaGroupDetail {
  group: MediaGroup;
  members: MediaGroupMember[];
  candidates: MediaCandidate[];
}

export function getMediaGroups(status = "review", limit = 100, offset = 0): Promise<{ items: MediaGroupDetail[]; total: number }> {
  return apiFetch<{ items: MediaGroupDetail[]; total: number }>(`/media-groups?status=${encodeURIComponent(status)}&limit=${limit}&offset=${offset}`);
}

export function confirmMediaGroup(groupId: number, candidateId: number, library: string): Promise<{ confirmed: boolean; members: number }> {
  return post(`/media-groups/${groupId}/confirm`, { candidateId, library });
}

export function reviewMediaGroup(groupId: number): Promise<{ review: boolean }> {
  return post(`/media-groups/${groupId}/review`);
}

export function searchMediaGroupCandidates(groupId: number, query: string): Promise<MediaGroupDetail> {
  return post(`/media-groups/${groupId}/candidates`, { query });
}

export interface QRSession {
  t: string;
  ck: string;
  qrContent: string;
}

// 申请阿里云盘登录二维码。
export function aliyunQR(): Promise<QRSession> {
  return post<QRSession>("/auth/aliyun/qr");
}

// 轮询扫码状态：NEW / SCANED / CONFIRMED / EXPIRED / CANCELED。
export async function aliyunQRStatus(t: string, ck: string): Promise<string> {
  const d = await post<{ status: string }>("/auth/aliyun/qr/status", { t, ck });
  return d.status;
}

export interface OpenQRSession {
  sid: string;
  qrCodeUrl: string; // 待编码成二维码的授权地址
}

// 开放接口(原画直链)扫码授权 —— 阿里官方 OAuth，需服务端配了 client_id/secret。
export function aliyunOpenQR(): Promise<OpenQRSession> {
  return post<OpenQRSession>("/auth/aliyun/open/qr");
}

// 轮询开放接口扫码状态：WaitLogin / ScanSuccess / LoginSuccess / QRCodeExpired。
export async function aliyunOpenQRStatus(sid: string): Promise<string> {
  const d = await post<{ status: string }>("/auth/aliyun/open/qr/status", { sid });
  return d.status;
}

// 115 网页扫码登录会话（拿网页态 cookie，用于转存分享）。
export interface Pan115Session {
  uid: string;
  time: number;
  sign: string;
  qrcode: string; // 待编码成二维码的扫码地址
}

export function pan115QR(): Promise<Pan115Session> {
  return post<Pan115Session>("/auth/115/qr");
}

// 轮询 115 扫码状态：0 等待 / 1 已扫 / 2 已确认 / 负数 过期。
export async function pan115QRStatus(s: Pan115Session): Promise<number> {
  const d = await post<{ status: number }>("/auth/115/qr/status", s);
  return d.status;
}

// 夸克扫码登录会话（拿网页态 cookie；夸克开放 API 需 secret 签名，走不通）。
export interface QuarkSession {
  token: string;
  qrcode: string;
}

export function quarkQR(): Promise<QuarkSession> {
  return post<QuarkSession>("/auth/quark/qr");
}

// 轮询夸克扫码状态：50004001 等待 / 2000000 已确认。
export async function quarkQRStatus(s: QuarkSession): Promise<number> {
  const d = await post<{ status: number }>("/auth/quark/qr/status", s);
  return d.status;
}
