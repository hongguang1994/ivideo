// Package resourcesearch 提供可插拔的公开资源搜索来源。
package resourcesearch

import (
	"context"
	"time"
)

const (
	AvailabilityChecking  = "checking"
	AvailabilityAvailable = "available"
	AvailabilityEmpty     = "empty"
	AvailabilityInvalid   = "invalid"
	AvailabilityUnknown   = "unknown"
)

// Result 是从公开索引中识别出的一个网盘分享。
type Result struct {
	Provider      string   `json:"provider"`
	ShareURL      string   `json:"shareUrl"`
	SharePwd      string   `json:"sharePwd"`
	Title         string   `json:"title"`
	ResourceType  string   `json:"resourceType,omitempty"`
	FileName      string   `json:"fileName,omitempty"`
	UpdatedAt     string   `json:"updatedAt,omitempty"`
	Source        string   `json:"source"`
	SourceName    string   `json:"sourceName,omitempty"`
	Repository    string   `json:"repository"`
	Path          string   `json:"path"`
	SourceURL     string   `json:"sourceUrl"`
	Score         int      `json:"score,omitempty"`
	Sources       []string `json:"sources,omitempty"`
	Availability  string   `json:"availability,omitempty"`
	EntryCount    int      `json:"entryCount,omitempty"`
	VerifiedAt    int64    `json:"verifiedAt,omitempty"`
	VerifyMessage string   `json:"verifyMessage,omitempty"`
}

// Verification 是网盘适配器对分享根目录的核验结果。
type Verification struct {
	Status  string
	Count   int
	Message string
	At      time.Time
}

// ResultVerifier 隔离资源发现和具体网盘 API；引擎只关心分享是否真实含有内容。
type ResultVerifier interface {
	Verify(ctx context.Context, result Result) Verification
}

// SourceReport 描述一次查询中单个来源的执行情况。
type SourceReport struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Healthy     bool   `json:"healthy"`
	ResultCount int    `json:"resultCount"`
	Scanned     int    `json:"scanned"`
	DurationMS  int64  `json:"durationMs"`
	Error       string `json:"error,omitempty"`
}

// SourceHealth 描述引擎记忆的来源健康状态。
type SourceHealth struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind,omitempty"`
	Description   string `json:"description,omitempty"`
	Priority      int    `json:"priority"`
	Enabled       bool   `json:"enabled"`
	TimeoutMS     int64  `json:"timeoutMs"`
	LastHealthy   bool   `json:"lastHealthy"`
	LastError     string `json:"lastError,omitempty"`
	LastDuration  int64  `json:"lastDurationMs"`
	SuccessCount  int64  `json:"successCount"`
	FailureCount  int64  `json:"failureCount"`
	LastCheckedAt int64  `json:"lastCheckedAt"`
}

// Meta 描述资源发现引擎本轮搜索的汇总信息。
type Meta struct {
	Source     string         `json:"source"`
	Scanned    int            `json:"scanned"`
	Remaining  int            `json:"remaining"`
	ResetAt    int64          `json:"resetAt"`
	DurationMS int64          `json:"durationMs,omitempty"`
	Cached     bool           `json:"cached,omitempty"`
	Sources    []SourceReport `json:"sources,omitempty"`
	Warnings   []string       `json:"warnings,omitempty"`
	Verified   int            `json:"verified,omitempty"`
	Rejected   int            `json:"rejected,omitempty"`
	Unverified int            `json:"unverified,omitempty"`
}

// Provider 是网络资源搜索来源的统一抽象。
type Provider interface {
	Search(ctx context.Context, query string) ([]Result, Meta, error)
}
