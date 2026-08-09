package resourcesearch

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const RSSFeedsSettingKey = "discovery.rss.feeds"

const maxRSSArticleFetches = 20

// RSSFeedCandidate describes a feed found from a website and sampled before it
// is saved. It lets a user decide whether the source is worth subscribing to.
type RSSFeedCandidate struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	Format     string `json:"format"`
	EntryCount int    `json:"entryCount"`
	ShareCount int    `json:"shareCount"`
}

// RSSFeed is one public RSS or Atom feed configured by the user.
type RSSFeed struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	URL          string `json:"url"`
	Enabled      bool   `json:"enabled"`
	FetchArticle bool   `json:"fetchArticle"`
}

type RSSFeedLoader func(context.Context) ([]RSSFeed, error)

// RSSSource collects public RSS/Atom entries. It never needs credentials for
// ordinary public feeds, and only follows article links on the feed's own host.
type RSSSource struct {
	load   RSSFeedLoader
	client *http.Client
}

func NewRSSSource(load RSSFeedLoader) *RSSSource {
	return &RSSSource{
		load:   load,
		client: &http.Client{Timeout: 12 * time.Second},
	}
}

func (s *RSSSource) Descriptor() SourceDescriptor {
	return SourceDescriptor{
		ID: "rss-atom", Name: "RSS / Atom 订阅源", Kind: "feed", Priority: 70,
		Description: "定时读取公开订阅，提取文章中的网盘分享。", Timeout: 30 * time.Second,
		DisabledByDefault: true,
	}
}

func (s *RSSSource) Search(ctx context.Context, query string) ([]SourceResult, Meta, error) {
	return s.collect(ctx, query)
}

// Collect reads the latest entries without a query. It is used by the
// background collector before results are written into ivideo's local index.
func (s *RSSSource) Collect(ctx context.Context) ([]SourceResult, Meta, error) {
	return s.collect(ctx, "")
}

// Discover finds and samples RSS/Atom feeds advertised by a public website.
// It accepts either a feed URL directly or a normal site page containing a
// standard alternate-feed link. It intentionally does not search the web.
func (s *RSSSource) Discover(ctx context.Context, rawURL string) ([]RSSFeedCandidate, error) {
	rawURL = strings.TrimSpace(rawURL)
	if !validFeedURL(rawURL) {
		return nil, fmt.Errorf("请输入有效的 HTTP 或 HTTPS 地址")
	}
	body, err := s.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(body, 2<<20))
	body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if entries, err := parseFeed(data); err == nil {
		return []RSSFeedCandidate{sampleRSSFeed(rawURL, entries)}, nil
	}

	urls, err := advertisedFeedURLs(rawURL, data)
	if err != nil {
		return nil, err
	}
	candidates := make([]RSSFeedCandidate, 0, len(urls))
	for _, feedURL := range urls {
		entries, err := s.fetchFeed(ctx, RSSFeed{Name: feedURL, URL: feedURL})
		if err != nil {
			continue
		}
		candidates = append(candidates, sampleRSSFeed(feedURL, entries))
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("没有找到可读取的 RSS 或 Atom 订阅；请粘贴站点提供的订阅地址")
	}
	return candidates, nil
}

func sampleRSSFeed(feedURL string, entries []feedEntry) RSSFeedCandidate {
	parsed, _ := url.Parse(feedURL)
	name := parsed.Hostname()
	shares := 0
	for _, entry := range entries {
		shares += len(extractShareResults(entry.searchText(), entry.Title))
	}
	return RSSFeedCandidate{
		Name: name, URL: feedURL, Format: feedFormat(feedURL), EntryCount: len(entries), ShareCount: shares,
	}
}

func feedFormat(value string) string {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "atom") {
		return "Atom"
	}
	return "RSS / Atom"
}

func advertisedFeedURLs(pageURL string, data []byte) ([]string, error) {
	document, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	urls := make([]string, 0, 8)
	appendURL := func(value string) {
		if len(urls) >= 10 || strings.TrimSpace(value) == "" {
			return
		}
		parsed, err := url.Parse(strings.TrimSpace(value))
		if err != nil {
			return
		}
		resolved := base.ResolveReference(parsed)
		if !validFeedURL(resolved.String()) {
			return
		}
		key := strings.ToLower(resolved.String())
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		urls = append(urls, resolved.String())
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			href := attr(node, "href")
			if href != "" && isFeedLink(node, href) {
				appendURL(href)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	for _, suffix := range []string{"/feed", "/rss", "/feed.xml", "/rss.xml", "/atom.xml", "/index.xml"} {
		appendURL(suffix)
	}
	return urls, nil
}

func isFeedLink(node *html.Node, href string) bool {
	if node.Data == "link" {
		rel := strings.ToLower(attr(node, "rel"))
		kind := strings.ToLower(attr(node, "type"))
		if strings.Contains(rel, "alternate") && (strings.Contains(kind, "rss") || strings.Contains(kind, "atom") || strings.Contains(kind, "xml")) {
			return true
		}
	}
	lower := strings.ToLower(href)
	return strings.Contains(lower, "rss") || strings.Contains(lower, "atom") || strings.Contains(lower, "feed") || strings.HasSuffix(lower, ".xml")
}

func (s *RSSSource) collect(ctx context.Context, query string) ([]SourceResult, Meta, error) {
	if s == nil || s.load == nil {
		return nil, Meta{Source: "rss-atom"}, ErrSourceNotConfigured
	}
	feeds, err := s.load(ctx)
	if err != nil {
		return nil, Meta{Source: "rss-atom"}, err
	}
	enabled := make([]RSSFeed, 0, len(feeds))
	for _, feed := range feeds {
		if feed.Enabled && validFeedURL(feed.URL) {
			enabled = append(enabled, normalizeFeed(feed))
		}
	}
	if len(enabled) == 0 {
		return nil, Meta{Source: "rss-atom"}, ErrSourceNotConfigured
	}

	items := make([]SourceResult, 0)
	failed := 0
	articleFetches := 0
	var lastErr error
	for _, feed := range enabled {
		entries, fetchErr := s.fetchFeed(ctx, feed)
		if fetchErr != nil {
			failed++
			lastErr = fetchErr
			continue
		}
		for _, entry := range entries {
			if query != "" && !strings.Contains(normalizeSearchText(entry.searchText()), normalizeSearchText(query)) {
				continue
			}
			found := sourceResultsForFeed(entry.searchText(), entry, feed)
			var parseErr error
			if len(found) == 0 && feed.FetchArticle && articleFetches < maxRSSArticleFetches && sameFeedHost(feed.URL, entry.Link) {
				articleFetches++
				found, parseErr = s.articleResults(ctx, feed, entry)
			}
			if parseErr != nil {
				lastErr = parseErr
				continue
			}
			items = append(items, found...)
		}
	}
	if failed == len(enabled) && lastErr != nil {
		return nil, Meta{Source: "rss-atom", Scanned: len(enabled)}, lastErr
	}
	return dedupeSourceResults(items), Meta{Source: "rss-atom", Scanned: len(enabled)}, nil
}

func (s *RSSSource) fetchFeed(ctx context.Context, feed RSSFeed) ([]feedEntry, error) {
	body, err := s.get(ctx, feed.URL)
	if err != nil {
		return nil, fmt.Errorf("读取订阅 %s 失败: %w", feed.Name, err)
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, 2<<20))
	if err != nil {
		return nil, err
	}
	entries, err := parseFeed(data)
	if err != nil {
		return nil, fmt.Errorf("解析订阅 %s 失败: %w", feed.Name, err)
	}
	if len(entries) > 100 {
		entries = entries[:100]
	}
	return entries, nil
}

func (s *RSSSource) articleResults(ctx context.Context, feed RSSFeed, entry feedEntry) ([]SourceResult, error) {
	body, err := s.get(ctx, entry.Link)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	article, err := htmlTextAndLinks(io.LimitReader(body, 2<<20))
	if err != nil {
		return nil, err
	}
	return sourceResultsForFeed(entry.Title+"\n"+article, entry, feed), nil
}

func (s *RSSSource) get(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ivideo-resource-discovery/1.0")
	response, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		response.Body.Close()
		return nil, fmt.Errorf("HTTP %s", response.Status)
	}
	return response.Body, nil
}

type feedEntry struct {
	Title     string
	Link      string
	Summary   string
	Content   string
	Published string
}

func (e feedEntry) searchText() string {
	return strings.Join([]string{e.Title, e.Summary, e.Content}, "\n")
}

func sourceResultsForFeed(content string, entry feedEntry, feed RSSFeed) []SourceResult {
	items := extractShareResults(content, entry.Title)
	for i := range items {
		items[i].TitleBasis = TitleBasisSource
		items[i].SourceName = feed.Name
		items[i].Repository = feed.Name
		items[i].Path = entry.Link
		items[i].SourceURL = entry.Link
		items[i].UpdatedAt = entry.Published
		items[i].Evidence = append(items[i].Evidence, "rss:feed-entry", "rss:feed:"+feed.ID)
	}
	return items
}

func dedupeSourceResults(items []SourceResult) []SourceResult {
	seen := make(map[string]struct{}, len(items))
	result := make([]SourceResult, 0, len(items))
	for _, item := range items {
		key := canonicalShareValues(item.Provider, item.ShareURL)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].UpdatedAt > result[j].UpdatedAt })
	return result
}

type rssDocument struct {
	Channel struct {
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			Encoded     string `xml:"encoded"`
			PubDate     string `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
}

type atomDocument struct {
	Entries []struct {
		Title     string `xml:"title"`
		Summary   string `xml:"summary"`
		Content   string `xml:"content"`
		Updated   string `xml:"updated"`
		Published string `xml:"published"`
		Links     []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

func parseFeed(data []byte) ([]feedEntry, error) {
	var rss rssDocument
	if err := xml.Unmarshal(data, &rss); err == nil && len(rss.Channel.Items) > 0 {
		items := make([]feedEntry, 0, len(rss.Channel.Items))
		for _, item := range rss.Channel.Items {
			items = append(items, feedEntry{Title: strings.TrimSpace(item.Title), Link: strings.TrimSpace(item.Link), Summary: item.Description, Content: item.Encoded, Published: strings.TrimSpace(item.PubDate)})
		}
		return items, nil
	}
	var atom atomDocument
	if err := xml.Unmarshal(data, &atom); err != nil {
		return nil, err
	}
	if len(atom.Entries) == 0 {
		return nil, fmt.Errorf("不是包含条目的 RSS 或 Atom 文档")
	}
	items := make([]feedEntry, 0, len(atom.Entries))
	for _, entry := range atom.Entries {
		link := ""
		for _, candidate := range entry.Links {
			if candidate.Rel == "" || candidate.Rel == "alternate" {
				link = candidate.Href
				break
			}
		}
		if link == "" && len(entry.Links) > 0 {
			link = entry.Links[0].Href
		}
		published := entry.Updated
		if entry.Published != "" {
			published = entry.Published
		}
		items = append(items, feedEntry{Title: strings.TrimSpace(entry.Title), Link: strings.TrimSpace(link), Summary: entry.Summary, Content: entry.Content, Published: strings.TrimSpace(published)})
	}
	return items, nil
}

func htmlTextAndLinks(reader io.Reader) (string, error) {
	document, err := html.Parse(reader)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0)
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			if href := attr(node, "href"); href != "" {
				parts = append(parts, href)
			}
		}
		if node.Type == html.TextNode {
			if text := strings.TrimSpace(node.Data); text != "" {
				parts = append(parts, text)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(document)
	return strings.Join(parts, "\n"), nil
}

func normalizeFeed(feed RSSFeed) RSSFeed {
	feed.ID = strings.TrimSpace(feed.ID)
	feed.Name = strings.TrimSpace(feed.Name)
	feed.URL = strings.TrimSpace(feed.URL)
	if feed.Name == "" {
		feed.Name = feed.URL
	}
	if feed.ID == "" {
		parsed, _ := url.Parse(feed.URL)
		feed.ID = "rss-" + strings.NewReplacer(".", "-", "/", "-", ":", "").Replace(parsed.Hostname())
	}
	return feed
}

func validFeedURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func sameFeedHost(feedURL, articleURL string) bool {
	feed, feedErr := url.Parse(feedURL)
	article, articleErr := url.Parse(articleURL)
	if feedErr != nil || articleErr != nil || article.Scheme == "" || article.Host == "" {
		return false
	}
	feedHost := strings.TrimPrefix(strings.ToLower(feed.Hostname()), "www.")
	articleHost := strings.TrimPrefix(strings.ToLower(article.Hostname()), "www.")
	return feedHost == articleHost
}
