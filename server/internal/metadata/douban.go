package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// doubanClient 是 TMDb 的兜底元数据源。豆瓣没有稳定的公开 API，所有请求都必须
// 限速，并且只在 TMDb 找不到时调用，避免把豆瓣当成高频搜索接口使用。
type doubanClient struct {
	http       *http.Client
	suggestURL string
	subjectURL string
	mu         sync.Mutex
	lastFetch  time.Time
	cache      map[string]doubanItem
}

func (c *doubanClient) download(ctx context.Context, rawURL, dst string) error {
	if rawURL == "" {
		return nil
	}
	if err := c.waitRateLimit(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ivideo metadata)")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Referer", "https://movie.douban.com/")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("豆瓣图片返回 %d", res.StatusCode)
	}
	if contentType := strings.ToLower(res.Header.Get("Content-Type")); !strings.HasPrefix(contentType, "image/") {
		return fmt.Errorf("豆瓣图片返回了无效内容 %q", contentType)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 12<<20))
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return errors.New("豆瓣图片内容为空")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

type doubanItem struct {
	ID            string
	Title         string
	OriginalTitle string
	Year          int
	Plot          string
	Poster        string
	Rating        float64
	URL           string
}

type doubanSuggest struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Year  string `json:"year"`
	URL   string `json:"url"`
	Pic   string `json:"pic"`
}

var (
	doubanMetaRe    = regexp.MustCompile(`(?is)<meta[^>]+(?:property|name)=["']([^"']+)["'][^>]+content=["']([^"']*)["'][^>]*>`)
	doubanRatingRe  = regexp.MustCompile(`(?is)(?:ratingValue|rating_num)["'=:>\s]+([0-9]+(?:\.[0-9]+)?)`)
	doubanYearRe    = regexp.MustCompile(`(?:19|20)\d{2}`)
	doubanSubjectRe = regexp.MustCompile(`/subject/(\d+)`)
)

func newDouban() *doubanClient {
	return &doubanClient{
		http:       &http.Client{Timeout: 20 * time.Second},
		suggestURL: "https://movie.douban.com/j/subject_suggest",
		subjectURL: "https://movie.douban.com/subject/",
		cache:      make(map[string]doubanItem),
	}
}

func (c *doubanClient) waitRateLimit(ctx context.Context) error {
	c.mu.Lock()
	delay := time.Second - time.Since(c.lastFetch)
	if delay < 0 {
		delay = 0
	}
	c.lastFetch = time.Now().Add(delay)
	c.mu.Unlock()
	if delay <= 0 {
		return nil
	}
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *doubanClient) get(ctx context.Context, rawURL string) ([]byte, error) {
	if err := c.waitRateLimit(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ivideo metadata)")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("豆瓣返回 %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, 4<<20))
}

func (c *doubanClient) search(ctx context.Context, title string, year int) (doubanItem, error) {
	key := normalizeTitle(title) + "\x00" + strconv.Itoa(year)
	c.mu.Lock()
	if item, ok := c.cache[key]; ok {
		c.mu.Unlock()
		return item, nil
	}
	c.mu.Unlock()

	rawURL := c.suggestURL + "?q=" + url.QueryEscape(title)
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return doubanItem{}, err
	}
	var suggestions []doubanSuggest
	if err := json.Unmarshal(body, &suggestions); err != nil {
		return doubanItem{}, fmt.Errorf("豆瓣搜索结果解析失败: %w", err)
	}
	want := normalizeTitle(title)
	for _, suggestion := range suggestions {
		if normalizeTitle(suggestion.Title) != want {
			continue
		}
		if year > 0 && suggestion.Year != "" && !strings.Contains(suggestion.Year, strconv.Itoa(year)) {
			continue
		}
		id := suggestion.ID
		if id == "" {
			match := doubanSubjectRe.FindStringSubmatch(suggestion.URL)
			if len(match) > 1 {
				id = match[1]
			}
		}
		if id == "" {
			continue
		}
		item, err := c.details(ctx, id, suggestion)
		if err != nil {
			// 豆瓣详情页经常跳转到 sec.douban.com 验证页。搜索接口本身
			// 已提供稳定的 ID、片名、年份和海报，详情受限时用这些字段降级。
			item = itemFromDoubanSuggestion(id, suggestion)
		}
		c.mu.Lock()
		c.cache[key] = item
		c.mu.Unlock()
		return item, nil
	}
	return doubanItem{}, fmt.Errorf("豆瓣未找到同名作品 %q", title)
}

func (c *doubanClient) details(ctx context.Context, id string, suggestion doubanSuggest) (doubanItem, error) {
	body, err := c.get(ctx, c.subjectURL+id+"/")
	if err != nil {
		return doubanItem{}, err
	}
	item := doubanItem{ID: id, Title: suggestion.Title, Poster: suggestion.Pic, URL: suggestion.URL}
	foundMetadata := false
	for _, match := range doubanMetaRe.FindAllSubmatch(body, -1) {
		name := strings.ToLower(html.UnescapeString(string(match[1])))
		value := html.UnescapeString(string(match[2]))
		switch name {
		case "og:title":
			foundMetadata = true
			item.Title = strings.TrimSpace(value)
		case "og:description", "description":
			foundMetadata = true
			item.Plot = strings.TrimSpace(value)
		case "og:image":
			foundMetadata = true
			item.Poster = strings.TrimSpace(value)
		}
	}
	if !foundMetadata {
		return doubanItem{}, fmt.Errorf("豆瓣详情页被验证页面拦截")
	}
	if match := doubanRatingRe.FindStringSubmatch(string(body)); len(match) > 1 {
		item.Rating, _ = strconv.ParseFloat(match[1], 64)
	}
	if match := doubanYearRe.FindString(item.Title); match != "" {
		item.Year, _ = strconv.Atoi(match)
	} else if suggestion.Year != "" {
		item.Year, _ = strconv.Atoi(doubanYearRe.FindString(suggestion.Year))
	}
	if item.Title == "" {
		return doubanItem{}, fmt.Errorf("豆瓣详情缺少标题")
	}
	return item, nil
}

func itemFromDoubanSuggestion(id string, suggestion doubanSuggest) doubanItem {
	item := doubanItem{ID: id, Title: strings.TrimSpace(suggestion.Title), Poster: suggestion.Pic, URL: suggestion.URL}
	if value := doubanYearRe.FindString(suggestion.Year); value != "" {
		item.Year, _ = strconv.Atoi(value)
	}
	return item
}
