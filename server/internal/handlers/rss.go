package handlers

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ivideo/server/internal/resourcesearch"
	"ivideo/server/internal/resp"
	"ivideo/server/internal/store"
)

const (
	rssScheduleSetting = "discovery.rss.schedule"
	rssLastRunSetting  = "discovery.rss.last_run"
)

type rssSchedule struct {
	Enabled         bool `json:"enabled"`
	IntervalMinutes int  `json:"intervalMinutes"`
}

type rssCollectionStatus struct {
	Running    bool   `json:"running"`
	Feeds      int    `json:"feeds"`
	Discovered int    `json:"discovered"`
	Added      int    `json:"added"`
	Existing   int    `json:"existing"`
	LastError  string `json:"lastError"`
	StartedAt  int64  `json:"startedAt"`
	FinishedAt int64  `json:"finishedAt"`
}

func defaultRSSSchedule() rssSchedule { return rssSchedule{IntervalMinutes: 360} }

func (h *Handler) loadRSSFeeds() ([]resourcesearch.RSSFeed, error) {
	raw, found, err := h.store.GetSetting(resourcesearch.RSSFeedsSettingKey)
	if err != nil || !found || strings.TrimSpace(raw) == "" {
		return []resourcesearch.RSSFeed{}, err
	}
	var feeds []resourcesearch.RSSFeed
	if err := json.Unmarshal([]byte(raw), &feeds); err != nil {
		return nil, fmt.Errorf("RSS 订阅配置损坏: %w", err)
	}
	return feeds, nil
}

func (h *Handler) loadRSSSchedule() rssSchedule {
	schedule := defaultRSSSchedule()
	raw, found, err := h.store.GetSetting(rssScheduleSetting)
	if err != nil || !found || json.Unmarshal([]byte(raw), &schedule) != nil {
		return schedule
	}
	if schedule.IntervalMinutes < 15 {
		schedule.IntervalMinutes = 15
	}
	return schedule
}

func (h *Handler) saveRSSConfig(feeds []resourcesearch.RSSFeed, schedule rssSchedule) error {
	encoded, err := json.Marshal(feeds)
	if err != nil {
		return err
	}
	if err := h.store.SetSetting(resourcesearch.RSSFeedsSettingKey, string(encoded)); err != nil {
		return err
	}
	encoded, err = json.Marshal(schedule)
	if err != nil {
		return err
	}
	return h.store.SetSetting(rssScheduleSetting, string(encoded))
}

func (h *Handler) getRSSStatus() rssCollectionStatus {
	h.rssStatusMu.RLock()
	defer h.rssStatusMu.RUnlock()
	return h.rssStatus
}

func (h *Handler) setRSSStatus(status rssCollectionStatus) {
	h.rssStatusMu.Lock()
	h.rssStatus = status
	h.rssStatusMu.Unlock()
}

func (h *Handler) rssSettingsPayload() (gin.H, error) {
	feeds, err := h.loadRSSFeeds()
	if err != nil {
		return nil, err
	}
	return gin.H{"feeds": feeds, "schedule": h.loadRSSSchedule(), "status": h.getRSSStatus()}, nil
}

// RSSSettings returns feed configuration and the latest collection result.
func (h *Handler) RSSSettings(c *gin.Context) {
	payload, err := h.rssSettingsPayload()
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, payload)
}

// SaveRSSSettings replaces the configured public feeds. Article fetching only
// follows same-host article URLs, which avoids turning a feed into an open crawler.
func (h *Handler) SaveRSSSettings(c *gin.Context) {
	var req struct {
		Feeds    []resourcesearch.RSSFeed `json:"feeds"`
		Schedule rssSchedule              `json:"schedule"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Fail(c, http.StatusBadRequest, "RSS 设置格式不正确")
		return
	}
	feeds, err := normalizeRSSFeeds(req.Feeds)
	if err != nil {
		resp.Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Schedule.IntervalMinutes < 15 || req.Schedule.IntervalMinutes > 10080 {
		resp.Fail(c, http.StatusBadRequest, "采集周期必须在 15 分钟到 7 天之间")
		return
	}
	if err := h.saveRSSConfig(feeds, req.Schedule); err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	hasEnabledFeed := false
	for _, feed := range feeds {
		hasEnabledFeed = hasEnabledFeed || feed.Enabled
	}
	_ = h.discovery.SetSourceEnabled("rss-atom", hasEnabledFeed)
	_ = h.store.SetSetting(resourcesearch.SourceEnabledSettingKey("rss-atom"), strconv.FormatBool(hasEnabledFeed))
	payload, err := h.rssSettingsPayload()
	if err != nil {
		resp.Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.OK(c, payload)
}

func normalizeRSSFeeds(input []resourcesearch.RSSFeed) ([]resourcesearch.RSSFeed, error) {
	if len(input) > 30 {
		return nil, fmt.Errorf("最多配置 30 个 RSS / Atom 订阅源")
	}
	seen := make(map[string]struct{}, len(input))
	feeds := make([]resourcesearch.RSSFeed, 0, len(input))
	for _, feed := range input {
		feed.URL = strings.TrimSpace(feed.URL)
		feed.Name = strings.TrimSpace(feed.Name)
		parsed, err := url.Parse(feed.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return nil, fmt.Errorf("订阅地址必须是有效的 HTTP 或 HTTPS 地址")
		}
		key := strings.ToLower(parsed.String())
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("存在重复的订阅地址: %s", feed.URL)
		}
		seen[key] = struct{}{}
		if feed.Name == "" {
			feed.Name = parsed.Hostname()
		}
		sum := sha1.Sum([]byte(key))
		feed.ID = "rss-" + hex.EncodeToString(sum[:])[:12]
		feeds = append(feeds, feed)
	}
	sort.Slice(feeds, func(i, j int) bool { return feeds[i].Name < feeds[j].Name })
	return feeds, nil
}

// RunRSSCollection starts an asynchronous feed collection immediately.
func (h *Handler) RunRSSCollection(c *gin.Context) {
	if !h.startRSSCollection("manual") {
		resp.Fail(c, http.StatusConflict, "RSS 采集任务正在运行")
		return
	}
	resp.OK(c, gin.H{"started": true, "status": h.getRSSStatus()})
}

func (h *Handler) StartRSSScheduler() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			schedule := h.loadRSSSchedule()
			if !schedule.Enabled || h.getRSSStatus().Running {
				continue
			}
			lastRun := h.lastRSSRun()
			if lastRun != 0 && time.Now().Unix()-lastRun < int64(schedule.IntervalMinutes)*60 {
				continue
			}
			h.startRSSCollection("scheduled")
		}
	}()
}

func (h *Handler) lastRSSRun() int64 {
	raw, found, err := h.store.GetSetting(rssLastRunSetting)
	if err != nil || !found {
		return 0
	}
	value, _ := strconv.ParseInt(raw, 10, 64)
	return value
}

func (h *Handler) startRSSCollection(reason string) bool {
	if h.rssSource == nil || !h.rssRunMu.TryLock() {
		return false
	}
	h.setRSSStatus(rssCollectionStatus{Running: true, StartedAt: time.Now().Unix()})
	go func() {
		defer h.rssRunMu.Unlock()
		h.runRSSCollection(reason)
	}()
	return true
}

func (h *Handler) runRSSCollection(reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	items, meta, err := h.rssSource.Collect(ctx)
	status := rssCollectionStatus{Feeds: meta.Scanned, Discovered: len(items), StartedAt: h.getRSSStatus().StartedAt, FinishedAt: time.Now().Unix()}
	if err != nil {
		status.LastError = err.Error()
		h.setRSSStatus(status)
		return
	}
	shares := make([]store.Share, 0, len(items))
	for _, raw := range items {
		item, ok := resourcesearch.NormalizeSourceResult(raw, "", h.rssSource.Descriptor())
		if !ok {
			continue
		}
		shares = append(shares, store.Share{
			Provider: item.Provider, ShareURL: item.ShareURL, SharePwd: item.SharePwd,
			ShareID: extractShareID(item.ShareURL), Title: item.Title, Remark: item.SourceURL,
			Category: item.ResourceType, Status: "unknown",
		})
	}
	added, existing, err := h.store.SyncShares(shares)
	if err != nil {
		status.LastError = err.Error()
	} else {
		status.Added, status.Existing = added, existing
		_ = h.store.SetSetting(rssLastRunSetting, strconv.FormatInt(status.FinishedAt, 10))
	}
	h.setRSSStatus(status)
}
