package backends

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"ivideo/server/internal/cache"
	"ivideo/server/internal/config"
)

// 115 网盘适配器。
//
// 重要边界（实测确认）：115 开放平台**没有「转存他人分享」接口**（/open/share/* 全 404）。
// 所以 115 走「文件已在你自己盘里 → 浏览 + 播放」模型，而非阿里那种「收集分享 → 自动转存」。
// 转存需你在 115 App 里手动完成。
//
// 非会员限制（VIP 后解除）：原文件下载限速约 0.8Mbps；转码流(video/play)VIP 专属。
// 直链是 UA 绑定的，播放要经 ivideo 代理转发（不能 302 直跳，播放器 UA 不匹配会 403）。
const (
	pan115Base      = "https://proapi.115.com/open"
	pan115RenewURL  = "https://api.oplist.org/115cloud/renewapi"
	pan115DriverTxt = "115cloud_qr"
)

// Pan115 是 115 网盘缓存盘适配器。
type Pan115 struct {
	http   *http.Client
	tokens TokenStore
	ua     string

	mu      sync.Mutex
	openTok string
	openExp time.Time

	// 单飞锁：115 令牌同样会轮换，并发刷新会把令牌链搞断（见阿里同款注释）。
	refreshMu sync.Mutex
}

// Pan115UA 是取直链和播放代理拉流必须共用的固定 UA。
// 115 直链绑定「请求直链时的 UA」，取链与拉流两处必须完全一致，否则 403。
// 故这里用固定值、不读配置（配置里的 UA 是给阿里用的，可能不同）。
const Pan115UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131 Safari/537.36"

// NewPan115 从配置创建 115 适配器。refresh token 从 TokenStore(provider="115")读。
func NewPan115(cfg config.Config, tokens TokenStore) *Pan115 {
	return &Pan115{
		http:   &http.Client{Timeout: 30 * time.Second},
		tokens: tokens,
		ua:     Pan115UA,
	}
}

// Name 实现 CacheBackend。
func (p *Pan115) Name() string { return "115" }

// openAccessToken 取 115 开放接口 access_token（oplist 中转续期 + 单飞 + 轮换回写）。
func (p *Pan115) openAccessToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	if p.openTok != "" && time.Now().Before(p.openExp) {
		t := p.openTok
		p.mu.Unlock()
		return t, nil
	}
	p.mu.Unlock()

	p.refreshMu.Lock()
	defer p.refreshMu.Unlock()
	p.mu.Lock()
	if p.openTok != "" && time.Now().Before(p.openExp) {
		t := p.openTok
		p.mu.Unlock()
		return t, nil
	}
	p.mu.Unlock()

	rt := ""
	if p.tokens != nil {
		rt = p.tokens.GetToken("115")
	}
	if rt == "" {
		return "", fmt.Errorf("未配置 115 授权，请在设置页扫码存入 115 refresh token")
	}

	u := fmt.Sprintf("%s?client_uid=&client_key=&driver_txt=%s&server_use=true&refresh_ui=%s",
		pan115RenewURL, pan115DriverTxt, url.QueryEscape(rt))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", p.ua)
	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("115 令牌续期请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Text         string `json:"text"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("115 令牌续期响应解析失败: %s", truncateBody(raw))
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("未取到 115 access_token: %s", out.Text)
	}

	p.mu.Lock()
	p.openTok = out.AccessToken
	exp := out.ExpiresIn
	if exp <= 0 {
		exp = 7200
	}
	p.openExp = time.Now().Add(time.Duration(max(exp-60, 60)) * time.Second)
	p.mu.Unlock()

	// 115 的 refresh token 每次续期都会轮换，必须回写，否则令牌链断裂。
	if out.RefreshToken != "" && out.RefreshToken != rt && p.tokens != nil {
		_ = p.tokens.SaveToken("115", out.RefreshToken)
	}
	return out.AccessToken, nil
}

// apiGet 带 Bearer 调 115 开放接口(GET)并解 JSON。
func (p *Pan115) apiGet(ctx context.Context, path string, q url.Values, out any) error {
	tok, err := p.openAccessToken(ctx)
	if err != nil {
		return err
	}
	u := pan115Base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("User-Agent", p.ua)
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return json.Unmarshal(raw, out)
}

// apiPost 带 Bearer POST 表单到 115 开放接口并解 JSON。
func (p *Pan115) apiPost(ctx context.Context, path string, form url.Values, out any) error {
	tok, err := p.openAccessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pan115Base+path,
		strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("User-Agent", p.ua)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return json.Unmarshal(raw, out)
}

// Verify 实测校验 115 令牌是否有效（换一次 access_token）。
func (p *Pan115) Verify(ctx context.Context, provider string) error {
	if provider != "115" {
		return fmt.Errorf("115 适配器不支持校验 provider: %s", provider)
	}
	_, err := p.openAccessToken(ctx)
	return err
}

// RefreshTokens 令牌保活（供定时器调用；未过期命中缓存不真刷）。
func (p *Pan115) RefreshTokens(ctx context.Context) error {
	if p.tokens == nil || p.tokens.GetToken("115") == "" {
		return nil
	}
	_, err := p.openAccessToken(ctx)
	return err
}

// Transfer 对 115 是 no-op：115 开放平台没有「转存他人分享」，文件须已在你自己盘里
// （你在 115 App 手动转存、再经导入建成资源）。这里把导入时记录的 pick_code 当缓存路径返回。
func (p *Pan115) Transfer(ctx context.Context, share cache.ShareRef) (cache.TransferResult, error) {
	pickCode := strings.TrimSpace(share.FilePath)
	if pickCode == "" {
		return cache.TransferResult{}, fmt.Errorf("115 资源缺少 pick_code")
	}
	return cache.TransferResult{CachePath: pickCode}, nil
}

// DirectURL 取 115 文件的可播直链（原文件）。cachePath 即 pick_code。
// 注意：115 直链 UA 绑定，播放需经 ivideo 代理转发；转码流 VIP 专属，非会员这里只能拿原文件。
func (p *Pan115) DirectURL(ctx context.Context, cachePath string) (string, error) {
	var out struct {
		State bool   `json:"state"`
		Msg   string `json:"message"`
		Data  map[string]struct {
			FileName string `json:"file_name"`
			URL      struct {
				URL string `json:"url"`
			} `json:"url"`
		} `json:"data"`
	}
	form := url.Values{"pick_code": {cachePath}}
	if err := p.apiPost(ctx, "/ufile/downurl", form, &out); err != nil {
		return "", err
	}
	if !out.State {
		return "", fmt.Errorf("115 取直链失败: %s", out.Msg)
	}
	for _, v := range out.Data {
		if v.URL.URL != "" {
			return v.URL.URL, nil
		}
	}
	return "", fmt.Errorf("115 未返回直链")
}

// Delete 删除 115 文件（进回收站）。cachePath 即 pick_code。
func (p *Pan115) Delete(ctx context.Context, cachePath string) error {
	fid, err := p.fileIDByPickCode(ctx, cachePath)
	if err != nil {
		return err
	}
	var out struct {
		State bool   `json:"state"`
		Msg   string `json:"message"`
	}
	form := url.Values{"file_ids": {fid}}
	if err := p.apiPost(ctx, "/ufile/delete", form, &out); err != nil {
		return err
	}
	if !out.State {
		return fmt.Errorf("115 删除失败: %s", out.Msg)
	}
	return nil
}

// fileIDByPickCode 由 pick_code 反查 file_id（删除接口需要）。
func (p *Pan115) fileIDByPickCode(ctx context.Context, pickCode string) (string, error) {
	var out struct {
		State bool `json:"state"`
		Data  []struct {
			Fid string `json:"fid"`
		} `json:"data"`
	}
	q := url.Values{"pick_code": {pickCode}, "limit": {"1"}, "show_dir": {"0"}}
	if err := p.apiGet(ctx, "/ufile/files", q, &out); err != nil {
		return "", err
	}
	for _, it := range out.Data {
		if it.Fid != "" {
			return it.Fid, nil
		}
	}
	return "", fmt.Errorf("115 未找到 pick_code=%s 对应文件", pickCode)
}

// EmptyTrash 115 暂不主动清空回收站（no-op）。
func (p *Pan115) EmptyTrash(ctx context.Context) error { return nil }

// Quota 暂不查 115 空间（返回未知）。
func (p *Pan115) Quota(ctx context.Context) (used, total int64, err error) { return 0, 0, nil }
