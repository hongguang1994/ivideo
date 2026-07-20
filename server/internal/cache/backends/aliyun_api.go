package backends

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
)

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
	// 优先高清(HD > SD),取已完成且有 url 的清晰度。
	tasks := out.VideoPreviewPlayInfo.LiveTranscodingTaskList
	best := ""
	for _, t := range tasks {
		if t.Status != "finished" || t.URL == "" {
			continue
		}
		if t.TemplateID == "HD" {
			return t.URL, nil
		}
		best = t.URL
	}
	if best == "" {
		return "", fmt.Errorf("未取到可播的转码地址(转码可能未就绪)")
	}
	return best, nil
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
