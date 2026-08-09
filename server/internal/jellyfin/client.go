// Package jellyfin 是 Jellyfin 媒体服务器 REST API 的最小客户端，
// 负责列出片库条目、取海报图、取播放流地址。鉴权用后台生成的 API Key。
package jellyfin

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Client 封装对 Jellyfin 的调用。
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New 创建 Jellyfin 客户端。
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Item 对应 Jellyfin 条目的部分字段。
type Item struct {
	ID             string            `json:"Id"`
	Name           string            `json:"Name"`
	Type           string            `json:"Type"`
	Overview       string            `json:"Overview"`
	ProductionYear int               `json:"ProductionYear"`
	ImageTags      map[string]string `json:"ImageTags"`
	Path           string            `json:"Path"`
}

// RefreshImagesByPath 要求 Jellyfin 丢弃指定媒体目录下的旧图片记录，重新读取本地图片。
func (c *Client) RefreshImagesByPath(pathPrefix string) (int, error) {
	return c.RefreshImagesByPaths([]string{pathPrefix})
}

// RefreshImagesByPaths 一次读取 Jellyfin 条目并批量刷新多个目录，避免每个作品
// 都重新拉取整库列表。一个条目命中多个父目录时也只会刷新一次。
func (c *Client) RefreshImagesByPaths(pathPrefixes []string) (int, error) {
	q := url.Values{}
	q.Set("Recursive", "true")
	q.Set("IncludeItemTypes", "Series,Season,Episode,Movie")
	q.Set("Fields", "Path")

	var out itemsResp
	if err := c.getJSON("/Items?"+q.Encode(), &out); err != nil {
		return 0, err
	}
	prefixes := make([]string, 0, len(pathPrefixes))
	for _, prefix := range pathPrefixes {
		if strings.TrimSpace(prefix) != "" {
			prefixes = append(prefixes, filepath.Clean(prefix))
		}
	}
	refreshed := 0
	for _, item := range out.Items {
		itemPath := filepath.Clean(item.Path)
		matched := false
		for _, prefix := range prefixes {
			if itemPath == prefix || strings.HasPrefix(itemPath, prefix+string(filepath.Separator)) {
				matched = true
				break
			}
		}
		if item.Path == "" || !matched {
			continue
		}
		params := url.Values{}
		params.Set("metadataRefreshMode", "None")
		params.Set("imageRefreshMode", "FullRefresh")
		params.Set("replaceAllImages", "true")
		if err := c.post("/Items/" + url.PathEscape(item.ID) + "/Refresh?" + params.Encode()); err != nil {
			return refreshed, fmt.Errorf("刷新 %s 图片: %w", item.Name, err)
		}
		refreshed++
	}
	return refreshed, nil
}

// RefreshCollectionImages 清除媒体库集合自身的旧封面，并要求 Jellyfin 根据
// 当前库内容重新生成。作品已经迁移后，集合封面不会随普通扫库自动失效。
func (c *Client) RefreshCollectionImages(name string) error {
	var folders []virtualFolder
	if err := c.getJSON("/Library/VirtualFolders", &folders); err != nil {
		return err
	}
	for _, folder := range folders {
		if folder.Name != name || folder.ItemID == "" {
			continue
		}
		for _, imageType := range []string{"Primary", "Backdrop"} {
			if err := c.delete("/Items/" + url.PathEscape(folder.ItemID) + "/Images/" + imageType); err != nil {
				return fmt.Errorf("清除 %s 媒体库封面: %w", name, err)
			}
		}
		params := url.Values{}
		params.Set("metadataRefreshMode", "FullRefresh")
		params.Set("imageRefreshMode", "FullRefresh")
		params.Set("replaceAllImages", "true")
		return c.post("/Items/" + url.PathEscape(folder.ItemID) + "/Refresh?" + params.Encode())
	}
	return fmt.Errorf("Jellyfin 中未找到 %q 媒体库", name)
}

// itemsResp 是 /Items 的响应结构。
type itemsResp struct {
	Items            []Item `json:"Items"`
	TotalRecordCount int    `json:"TotalRecordCount"`
}

// Items 列出片库中的条目。itemTypes 例如 "Movie" 或 "Movie,Series"。
func (c *Client) Items(itemTypes string) ([]Item, error) {
	if itemTypes == "" {
		itemTypes = "Movie"
	}
	q := url.Values{}
	q.Set("Recursive", "true")
	q.Set("IncludeItemTypes", itemTypes)
	q.Set("Fields", "Overview,ProductionYear,Path")
	q.Set("SortBy", "SortName")
	q.Set("SortOrder", "Ascending")

	var out itemsResp
	if err := c.getJSON("/Items?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// RemoveMissingItems 删除 Jellyfin 中指向项目媒体目录、但本地文件已不存在的旧索引。
func (c *Client) RemoveMissingItems(mediaDir string) (int, error) {
	items, err := c.Items("Movie,Series,Season,Episode")
	if err != nil {
		return 0, err
	}
	prefix := filepath.Clean(mediaDir)
	removed := 0
	for _, item := range items {
		p := filepath.Clean(item.Path)
		if item.Path == "" || (p != prefix && !strings.HasPrefix(p, prefix+string(filepath.Separator))) {
			continue
		}
		if _, err := os.Stat(p); err == nil || !os.IsNotExist(err) {
			continue
		}
		if err := c.delete("/Items/" + url.PathEscape(item.ID)); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// ImageURL 返回某条目主海报的内部访问地址（带 api_key，供后端代理拉取）。
func (c *Client) ImageURL(id string) string {
	return fmt.Sprintf("%s/Items/%s/Images/Primary?api_key=%s", c.baseURL, id, url.QueryEscape(c.apiKey))
}

// StreamURL 返回某条目的直连播放地址（供后端代理转发）。
func (c *Client) StreamURL(id string) string {
	return fmt.Sprintf("%s/Videos/%s/stream?static=true&api_key=%s", c.baseURL, id, url.QueryEscape(c.apiKey))
}

// PlayingItem 是某个会话里正在播放/暂停的条目。
type PlayingItem struct {
	ItemID   string // Jellyfin 条目 ID
	Name     string
	Path     string // 条目文件路径（通常是 .strm 文件），可能为空
	MediaURL string // 媒体源地址（strm 里那条 URL），可能为空
	Paused   bool   // 是否暂停（暂停也算“在会话中”，不该被清理）
}

// NowPlaying 拉取当前所有会话里正在播放/暂停的条目。
// 需要有效的 API Key（管理员令牌），否则 /Sessions 返回 401。
func (c *Client) NowPlaying() ([]PlayingItem, error) {
	var sessions []struct {
		NowPlayingItem *struct {
			ID           string `json:"Id"`
			Name         string `json:"Name"`
			Path         string `json:"Path"`
			MediaSources []struct {
				Path string `json:"Path"`
			} `json:"MediaSources"`
		} `json:"NowPlayingItem"`
		PlayState struct {
			IsPaused bool `json:"IsPaused"`
		} `json:"PlayState"`
	}
	if err := c.getJSON("/Sessions", &sessions); err != nil {
		return nil, err
	}
	var out []PlayingItem
	for _, s := range sessions {
		if s.NowPlayingItem == nil {
			continue
		}
		pi := PlayingItem{
			ItemID: s.NowPlayingItem.ID,
			Name:   s.NowPlayingItem.Name,
			Path:   s.NowPlayingItem.Path,
			Paused: s.PlayState.IsPaused,
		}
		if len(s.NowPlayingItem.MediaSources) > 0 {
			pi.MediaURL = s.NowPlayingItem.MediaSources[0].Path
		}
		out = append(out, pi)
	}
	return out, nil
}

// RefreshLibrary 触发 Jellyfin 扫描媒体库（异步，立即返回）。
// strm 有新增/删除后调用，新剧集才会出现在库里。
func (c *Client) RefreshLibrary() error {
	return c.post("/Library/Refresh")
}

func (c *Client) post(path string) error {
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("jellyfin: 扫库接口返回 %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (c *Client) delete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// 扫库期间 Jellyfin 可能已经自动删除该旧条目，视为清理成功。
		return nil
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("jellyfin: 删除旧条目返回 %d", resp.StatusCode)
	}
	return nil
}

// getJSON 带 API Key 发起 GET 并解 JSON。
func (c *Client) getJSON(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Emby-Token", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("jellyfin: 接口返回 %d: %s", resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
