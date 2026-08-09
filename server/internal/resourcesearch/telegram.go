package resourcesearch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

// TelegramSource 从 Telegram 公开频道网页检索分享链接，不需要机器人或用户凭据。
type TelegramSource struct {
	channels []string
	client   *http.Client
}

func NewTelegramSource(channels []string) *TelegramSource {
	cleaned := make([]string, 0, len(channels))
	seen := make(map[string]struct{})
	for _, channel := range channels {
		channel = strings.TrimPrefix(strings.TrimSpace(channel), "@")
		if channel == "" {
			continue
		}
		if _, ok := seen[channel]; ok {
			continue
		}
		seen[channel] = struct{}{}
		cleaned = append(cleaned, channel)
	}
	return &TelegramSource{
		channels: cleaned,
		client: &http.Client{
			Timeout:   15 * time.Second,
			Transport: &http.Transport{Proxy: http.ProxyFromEnvironment},
		},
	}
}

func (s *TelegramSource) Descriptor() SourceDescriptor {
	return SourceDescriptor{ID: "telegram-public", Name: "Telegram 公开频道", Priority: 55}
}

func (s *TelegramSource) Search(ctx context.Context, query string) ([]Result, Meta, error) {
	if len(s.channels) == 0 {
		return nil, Meta{Source: "telegram-public"}, ErrSourceNotConfigured
	}
	type channelResult struct {
		items []Result
		err   error
	}
	results := make(chan channelResult, len(s.channels))
	semaphore := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, channel := range s.channels {
		wg.Add(1)
		go func(channel string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			items, err := s.searchChannel(ctx, channel, query)
			results <- channelResult{items: items, err: err}
		}(channel)
	}
	go func() { wg.Wait(); close(results) }()

	items := make([]Result, 0)
	failed := 0
	var lastErr error
	for result := range results {
		if result.err != nil {
			failed++
			lastErr = result.err
			continue
		}
		items = append(items, result.items...)
	}
	if failed == len(s.channels) && lastErr != nil {
		return nil, Meta{Source: "telegram-public", Scanned: len(s.channels)}, lastErr
	}
	return items, Meta{Source: "telegram-public", Scanned: len(s.channels)}, nil
}

func (s *TelegramSource) searchChannel(ctx context.Context, channel, query string) ([]Result, error) {
	endpoint := "https://t.me/s/" + url.PathEscape(channel) + "?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ivideo-resource-discovery/1.0)")
	response, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("读取 @%s 失败: %w", channel, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("读取 @%s 返回 %s", channel, response.Status)
	}
	return parseTelegramHTML(io.LimitReader(response.Body, 4<<20), channel, query)
}

func parseTelegramHTML(reader io.Reader, channel, query string) ([]Result, error) {
	document, err := html.Parse(reader)
	if err != nil {
		return nil, fmt.Errorf("解析 Telegram 页面失败: %w", err)
	}
	queryKey := normalizeSearchText(query)
	items := make([]Result, 0)
	seen := make(map[string]struct{})
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "div" && hasClass(node, "tgme_widget_message_wrap") {
			text, links, post, updatedAt := telegramMessage(node)
			if queryKey != "" && !strings.Contains(normalizeSearchText(text), queryKey) {
				return
			}
			content := text + "\n" + strings.Join(links, "\n")
			for _, item := range extractShareResults(content, telegramTitle(text, query)) {
				if item.SharePwd == "" {
					if match := shareCodePattern.FindStringSubmatch(text); len(match) > 1 {
						item.SharePwd = match[1]
					}
				}
				if item.SharePwd == "" {
					if parsed, err := url.Parse(item.ShareURL); err == nil {
						item.SharePwd = parsed.Query().Get("password")
					}
				}
				key := canonicalShareKey(item)
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				item.Source = "telegram-public"
				item.SourceName = "@" + channel
				item.Repository = "Telegram"
				item.Path = post
				item.UpdatedAt = updatedAt
				if post != "" {
					item.SourceURL = "https://t.me/" + post
				} else {
					item.SourceURL = "https://t.me/s/" + channel
				}
				items = append(items, item)
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	sort.SliceStable(items, func(i, j int) bool { return items[i].UpdatedAt > items[j].UpdatedAt })
	return items, nil
}

func telegramMessage(root *html.Node) (string, []string, string, string) {
	texts := make([]string, 0)
	links := make([]string, 0)
	post := ""
	updatedAt := ""
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			if node.Data == "div" && hasClass(node, "tgme_widget_message") {
				post = attr(node, "data-post")
			}
			if node.Data == "div" && hasClass(node, "tgme_widget_message_text") {
				messageTexts, messageLinks := telegramMessageContent(node)
				texts = append(texts, messageTexts...)
				links = append(links, messageLinks...)
				return
			}
			if node.Data == "time" {
				updatedAt = attr(node, "datetime")
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return strings.Join(texts, "\n"), links, post, updatedAt
}

func telegramMessageContent(root *html.Node) ([]string, []string) {
	texts := make([]string, 0)
	links := make([]string, 0)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			if href := attr(node, "href"); href != "" {
				links = append(links, href)
			}
		}
		if node.Type == html.TextNode {
			if value := strings.TrimSpace(node.Data); value != "" {
				texts = append(texts, value)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return texts, links
}

func telegramTitle(text, fallback string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") || shareCodePattern.MatchString(line) {
			continue
		}
		for _, prefix := range []string{"资源名称：", "资源名称:", "名称：", "名称:"} {
			if strings.HasPrefix(line, prefix) {
				value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
				if validTelegramTitle(value) {
					return truncateTitle(value)
				}
				for next := index + 1; next < len(lines); next++ {
					value = strings.TrimSpace(lines[next])
					if validTelegramTitle(value) {
						return truncateTitle(value)
					}
				}
			}
		}
		if !validTelegramTitle(line) {
			continue
		}
		return truncateTitle(line)
	}
	return fallback
}

func validTelegramTitle(value string) bool {
	value = strings.TrimSpace(value)
	if strings.Trim(value, "：:|_-— ") == "" {
		return false
	}
	for _, label := range []string{"名称", "资源名称", "链接", "描述", "大小", "格式"} {
		if value == label || value == label+":" || value == label+"：" {
			return false
		}
	}
	return !strings.HasPrefix(value, "http://") && !strings.HasPrefix(value, "https://")
}

func truncateTitle(value string) string {
	if len([]rune(value)) > 120 {
		return string([]rune(value)[:120])
	}
	return value
}

func hasClass(node *html.Node, class string) bool {
	for _, value := range strings.Fields(attr(node, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func attr(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}
