package backends

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 阿里云盘 web 接口地址（纯 web 单套，配合扫码拿到的 refresh_token）。
const (
	aliWebTokenURL   = "https://auth.alipan.com/v2/account/token"             // web refresh → access token
	aliShareTokenURL = "https://api.alipan.com/v2/share_link/get_share_token" // 分享 token
	aliShareListURL  = "https://api.alipan.com/adrive/v3/file/list"           // 列分享内文件
	aliCopyURL         = "https://api.alipan.com/adrive/v2/file/copy"                  // 转存(分享→自己盘)
	aliVideoPreviewURL = "https://api.alipan.com/v2/file/get_video_preview_play_info" // 转码 HLS 播放地址
	aliDeleteURL       = "https://api.alipan.com/v3/file/delete"                       // 删除(进回收站)
	aliClearTrashURL = "https://api.alipan.com/v2/recyclebin/clear"          // 清空回收站

	// 开放接口(取原画直链)
	aliOpenDownloadURL = "https://openapi.alipan.com/adrive/v1.0/openFile/getDownloadUrl"
)

// browserUA 用于绕过 Cloudflare 对非浏览器客户端的拦截(见 openAccessToken)。
const browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// doJSON 发一个 JSON 请求并把响应解到 out；非 2xx 返回带响应体的错误。
func (a *Aliyun) doJSON(ctx context.Context, url string, headers map[string]string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := a.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("阿里接口 %s 返回 %d: %s", url, resp.StatusCode, truncateBody(raw))
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// currentRefreshToken 优先用库里(扫码写入)的 token，回退到配置。
func (a *Aliyun) currentRefreshToken() string {
	if a.tokens != nil {
		if t := a.tokens.GetToken("aliyun"); t != "" {
			return t
		}
	}
	return a.webRT
}

// webToken 用 refresh token 换 access token，并记录自己盘 drive_id。
func (a *Aliyun) webToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	if a.accessTok != "" && time.Now().Before(a.accessExp) {
		tok := a.accessTok
		a.mu.Unlock()
		return tok, nil
	}
	a.mu.Unlock()

	rt := a.currentRefreshToken()
	if rt == "" {
		return "", fmt.Errorf("阿里云盘未授权，请先在设置页扫码登录")
	}

	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	body := map[string]string{"grant_type": "refresh_token", "refresh_token": rt}
	if err := a.doJSON(ctx, aliWebTokenURL, nil, body, &out); err != nil {
		return "", fmt.Errorf("刷新 token 失败: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("刷新 token 未返回 access_token")
	}

	a.mu.Lock()
	a.accessTok = out.AccessToken
	a.accessExp = time.Now().Add(time.Duration(max(out.ExpiresIn-60, 60)) * time.Second)
	a.mu.Unlock()

	// refresh token 会轮换，回写库/内存。
	if out.RefreshToken != "" && out.RefreshToken != rt {
		a.webRT = out.RefreshToken
		if a.tokens != nil {
			_ = a.tokens.SaveToken("aliyun", out.RefreshToken)
		}
	}

	// 确保 drive_id 就绪(优先资源盘)。
	if err := a.ensureDrive(ctx, out.AccessToken); err != nil {
		return "", err
	}
	return out.AccessToken, nil
}

// ensureDrive 若 driveID 未定,用 user/get 取资源盘(备份盘兜底)。
func (a *Aliyun) ensureDrive(ctx context.Context, accessTok string) error {
	a.mu.Lock()
	if a.driveID != "" {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()

	var out struct {
		ResourceDriveID string `json:"resource_drive_id"`
		BackupDriveID   string `json:"backup_drive_id"`
		DefaultDriveID  string `json:"default_drive_id"`
	}
	headers := map[string]string{"Authorization": "Bearer " + accessTok}
	if err := a.doJSON(ctx, "https://user.alipan.com/v2/user/get", headers, map[string]any{}, &out); err != nil {
		return fmt.Errorf("获取网盘信息失败: %w", err)
	}
	d := out.ResourceDriveID // 优先资源盘（转存/HLS 更稳）
	if d == "" {
		d = out.BackupDriveID
	}
	if d == "" {
		d = out.DefaultDriveID
	}
	if d == "" {
		return fmt.Errorf("未获取到 drive_id")
	}
	a.mu.Lock()
	a.driveID = d
	a.mu.Unlock()
	return nil
}

// ---- 开放接口(取原画直链)----

// openAccessToken 取开放接口 access token。
// 默认走在线 token 服务(OpenList 的 api.oplist.org);若配置了自己的 client_id/secret 则走官方端点。
func (a *Aliyun) openAccessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	if a.openTok != "" && time.Now().Before(a.openExp) {
		tok := a.openTok
		a.mu.Unlock()
		return tok, nil
	}
	a.mu.Unlock()

	rt := a.openRT
	if a.tokens != nil {
		if t := a.tokens.GetToken("aliyun_open"); t != "" {
			rt = t
		}
	}
	if rt == "" {
		return "", fmt.Errorf("未配置开放接口授权，请先在设置页填入开放接口 refresh token")
	}

	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Text         string `json:"text"`
	}

	if a.openClientID != "" {
		// 自己的开放平台应用：走官方端点
		body := map[string]string{
			"client_id":     a.openClientID,
			"client_secret": a.openClientSecret,
			"grant_type":    "refresh_token",
			"refresh_token": rt,
		}
		if err := a.doJSON(ctx, a.openTokenURL, nil, body, &out); err != nil {
			return "", fmt.Errorf("开放接口换 token 失败: %w", err)
		}
	} else {
		// 在线 token 服务：GET ?refresh_ui=<rt>&server_use=true&driver_txt=alicloud_qr
		driverTxt := "alicloud_qr"
		if a.tokens != nil {
			if e := a.tokens.GetTokenExtra("aliyun_open"); e != "" {
				driverTxt = e
			}
		}
		u := fmt.Sprintf("%s?refresh_ui=%s&server_use=true&driver_txt=%s",
			a.openRenewURL, url.QueryEscape(rt), url.QueryEscape(driverTxt))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return "", err
		}
		// api.oplist.org 在 Cloudflare 后面，默认的 Go-http-client UA 会被
		// 浏览器完整性检查拦掉(HTTP 403 / error code: 1010)，故伪装成常规浏览器。
		req.Header.Set("User-Agent", browserUA)
		req.Header.Set("Accept", "application/json, text/plain, */*")
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
		resp, err := a.http.Do(req)
		if err != nil {
			return "", fmt.Errorf("在线 token 服务请求失败: %w", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if err := json.Unmarshal(raw, &out); err != nil {
			return "", fmt.Errorf("在线 token 服务响应解析失败: %w (%s)", err, truncateBody(raw))
		}
	}

	// 在线服务没给到 token 时，回退到本地 TV 连接器(小雅的 aliyuntvtoken_connector)。
	if out.AccessToken == "" && a.openConnectorURL != "" {
		var alt struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			ExpiresIn    int    `json:"expires_in"`
		}
		if err := a.doJSON(ctx, a.openConnectorURL, nil, map[string]string{"refresh_token": rt}, &alt); err != nil {
			log.Printf("[aliyun] TV 连接器回退失败: %v", err)
		} else if alt.AccessToken != "" {
			log.Printf("[aliyun] 已通过本地 TV 连接器取得开放接口 token")
			out.AccessToken, out.RefreshToken, out.ExpiresIn = alt.AccessToken, alt.RefreshToken, alt.ExpiresIn
		}
	}

	if out.AccessToken == "" {
		return "", fmt.Errorf("未取到开放接口 access_token: %s", out.Text)
	}

	a.mu.Lock()
	a.openTok = out.AccessToken
	exp := out.ExpiresIn
	if exp <= 0 {
		exp = 7200
	}
	a.openExp = time.Now().Add(time.Duration(max(exp-60, 60)) * time.Second)
	a.mu.Unlock()

	// 开放接口的 refresh token 同样会轮换，回写。
	if out.RefreshToken != "" && out.RefreshToken != rt {
		a.openRT = out.RefreshToken
		if a.tokens != nil {
			_ = a.tokens.SaveToken("aliyun_open", out.RefreshToken)
		}
	}
	return out.AccessToken, nil
}

// originalURL 用开放接口取「原画直链」(mkv/mp4 本体，支持 Range)。
func (a *Aliyun) originalURL(ctx context.Context, openTok, fileID string) (string, error) {
	headers := map[string]string{"Authorization": "Bearer " + openTok}
	body := map[string]any{"drive_id": a.driveID, "file_id": fileID, "expire_sec": 14400}
	var out struct {
		URL string `json:"url"`
	}
	if err := a.doJSON(ctx, aliOpenDownloadURL, headers, body, &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", fmt.Errorf("开放接口未返回原画直链")
	}
	return out.URL, nil
}

// shareToken 取某分享的访问 token。
func (a *Aliyun) shareToken(ctx context.Context, shareID, sharePwd string) (string, error) {
	var out struct {
		ShareToken string `json:"share_token"`
	}
	body := map[string]string{"share_id": shareID, "share_pwd": sharePwd}
	if err := a.doJSON(ctx, aliShareTokenURL, nil, body, &out); err != nil {
		return "", err
	}
	if out.ShareToken == "" {
		return "", fmt.Errorf("未取到 share_token")
	}
	return out.ShareToken, nil
}

// shareItem 是分享目录项。
type shareItem struct {
	FileID string `json:"file_id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Size   int64  `json:"size"`
}

// listShare 列出分享内某目录，自动翻页。
func (a *Aliyun) listShare(ctx context.Context, shareID, shareTok, parentID string) ([]shareItem, error) {
	if parentID == "" {
		parentID = "root"
	}
	headers := map[string]string{"x-share-token": shareTok}
	var items []shareItem
	marker := ""
	for {
		body := map[string]any{
			"share_id":        shareID,
			"parent_file_id":  parentID,
			"limit":           200,
			"order_by":        "name",
			"order_direction": "ASC",
		}
		if marker != "" {
			body["marker"] = marker
		}
		var out struct {
			Items      []shareItem `json:"items"`
			NextMarker string      `json:"next_marker"`
		}
		if err := a.doJSON(ctx, aliShareListURL, headers, body, &out); err != nil {
			return nil, err
		}
		items = append(items, out.Items...)
		if out.NextMarker == "" {
			break
		}
		marker = out.NextMarker
	}
	return items, nil
}

// resolveFileID 把分享内相对路径解析成 file_id，并返回大小。
func (a *Aliyun) resolveFileID(ctx context.Context, shareID, shareTok, path string) (string, int64, error) {
	segs := splitPath(path)
	if len(segs) == 0 {
		return "", 0, fmt.Errorf("分享内文件路径为空")
	}
	parent := "root"
	for i, seg := range segs {
		items, err := a.listShare(ctx, shareID, shareTok, parent)
		if err != nil {
			return "", 0, err
		}
		var hit *shareItem
		for j := range items {
			if items[j].Name == seg {
				hit = &items[j]
				break
			}
		}
		if hit == nil {
			return "", 0, fmt.Errorf("分享内找不到: %s", seg)
		}
		if i == len(segs)-1 {
			return hit.FileID, hit.Size, nil
		}
		parent = hit.FileID
	}
	return "", 0, fmt.Errorf("路径解析失败")
}

// copyFromShare 把分享内文件转存到自己盘临时目录，返回新 file_id。
func (a *Aliyun) copyFromShare(ctx context.Context, accessTok, shareID, shareTok, fileID string) (string, error) {
	headers := map[string]string{
		"Authorization": "Bearer " + accessTok,
		"x-share-token": shareTok,
	}
	body := map[string]any{
		"file_id":           fileID,
		"share_id":          shareID,
		"auto_rename":       true,
		"to_parent_file_id": a.tempFolderID,
		"to_drive_id":       a.driveID,
	}
	var out struct {
		FileID      string `json:"file_id"`
		AsyncTaskID string `json:"async_task_id"`
	}
	if err := a.doJSON(ctx, aliCopyURL, headers, body, &out); err != nil {
		return "", err
	}
	if out.FileID != "" {
		return out.FileID, nil // 秒传/同步完成
	}
	return "", fmt.Errorf("转存返回异步任务(async_task_id=%s)，该文件非秒传，首版暂未支持轮询", out.AsyncTaskID)
}

// templateRank 给转码档位排序，数值越大画质越高。未知档位排在最低(0)。
func templateRank(id string) int {
	switch strings.ToUpper(id) {
	case "4K", "UHD":
		return 5
	case "QHD":
		return 4
	case "FHD":
		return 3
	case "HD":
		return 2
	case "SD":
		return 1
	case "LD":
		return 0
	}
	return 0
}

// playURL 取自己盘视频文件的转码 HLS 播放地址(m3u8)。
// 阿里对视频不放原画直链(get_download_url 返回空),转码 HLS 才可用。
func (a *Aliyun) playURL(ctx context.Context, accessTok, fileID string) (string, error) {
	headers := map[string]string{"Authorization": "Bearer " + accessTok}
	body := map[string]any{
		"drive_id":       a.driveID,
		"file_id":        fileID,
		"category":       "live_transcoding",
		"url_expire_sec": 14400,
	}
	var out struct {
		VideoPreviewPlayInfo struct {
			LiveTranscodingTaskList []struct {
				TemplateID string `json:"template_id"`
				Status     string `json:"status"`
				URL        string `json:"url"`
			} `json:"live_transcoding_task_list"`
		} `json:"video_preview_play_info"`
	}
	if err := a.doJSON(ctx, aliVideoPreviewURL, headers, body, &out); err != nil {
		return "", err
	}
	// 取【画质最高】且已完成的档位。阿里按源分辨率提供:
	// 4K > QHD(2K) > FHD(1080p) > HD(720p) > SD(480p) > LD(360p)
	tasks := out.VideoPreviewPlayInfo.LiveTranscodingTaskList
	bestURL, bestRank := "", -1
	for _, t := range tasks {
		if t.Status != "finished" || t.URL == "" {
			continue
		}
		if r := templateRank(t.TemplateID); r > bestRank {
			bestURL, bestRank = t.URL, r
		}
	}
	if bestURL == "" {
		return "", fmt.Errorf("未取到可播的转码地址(转码可能未就绪)")
	}
	return bestURL, nil
}

// deleteFile 删除自己盘文件（进回收站）。
func (a *Aliyun) deleteFile(ctx context.Context, accessTok, fileID string) error {
	headers := map[string]string{"Authorization": "Bearer " + accessTok}
	body := map[string]any{"drive_id": a.driveID, "file_id": fileID}
	return a.doJSON(ctx, aliDeleteURL, headers, body, nil)
}

// clearTrash 清空回收站，真正释放配额。
func (a *Aliyun) clearTrash(ctx context.Context, accessTok string) error {
	headers := map[string]string{"Authorization": "Bearer " + accessTok}
	body := map[string]any{"drive_id": a.driveID}
	return a.doJSON(ctx, aliClearTrashURL, headers, body, nil)
}

// splitPath 把 "/a/b/c" 切成 [a b c]。
func splitPath(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func truncateBody(b []byte) string {
	if len(b) > 300 {
		return string(b[:300]) + "…"
	}
	return string(b)
}
