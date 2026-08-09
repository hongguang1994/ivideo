package resourcesearch

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrSourceNotConfigured = errors.New("来源未配置")

const sourceEnabledSettingPrefix = "discovery.source.enabled."

func SourceEnabledSettingKey(id string) string {
	return sourceEnabledSettingPrefix + strings.TrimSpace(id)
}

// SourceDescriptor 是资源来源插件的稳定身份和排序权重。
type SourceDescriptor struct {
	ID                string
	Name              string
	Kind              string
	Description       string
	Priority          int
	Timeout           time.Duration
	DisabledByDefault bool
}

// Source 是独立资源发现引擎的插件接口。
type Source interface {
	Descriptor() SourceDescriptor
	Search(ctx context.Context, query string) ([]Result, Meta, error)
}

// SourceFunc 让已有 Provider 或动态凭据来源无需额外类型即可接入引擎。
type SourceFunc struct {
	Info       SourceDescriptor
	SearchFunc func(context.Context, string) ([]Result, Meta, error)
}

func (s SourceFunc) Descriptor() SourceDescriptor { return s.Info }
func (s SourceFunc) Search(ctx context.Context, query string) ([]Result, Meta, error) {
	return s.SearchFunc(ctx, query)
}

type EngineOptions struct {
	Concurrency             int
	Timeout                 time.Duration
	CacheTTL                time.Duration
	MaxResults              int
	VerificationConcurrency int
	VerificationTimeout     time.Duration
	VerificationTTL         time.Duration
}

type cacheEntry struct {
	items     []Result
	meta      Meta
	expiresAt time.Time
}

// Snapshot 是渐进式搜索在某一时刻的完整视图。
type Snapshot struct {
	JobID   string   `json:"jobId,omitempty"`
	Query   string   `json:"query"`
	Items   []Result `json:"items"`
	Meta    Meta     `json:"meta"`
	Pending bool     `json:"pending"`
}

type searchJob struct {
	id          string
	key         string
	query       string
	mu          sync.RWMutex
	snapshot    Snapshot
	done        chan struct{}
	subscribers map[chan Snapshot]struct{}
	finishedAt  time.Time
}

// Engine 并发调度来源插件，并集中负责缓存、失败隔离、去重和排序。
type Engine struct {
	options     EngineOptions
	sources     []Source
	enabled     map[string]bool
	mu          sync.RWMutex
	cache       map[string]cacheEntry
	health      map[string]SourceHealth
	jobs        map[string]*searchJob
	inflight    map[string]*searchJob
	nextJob     atomic.Uint64
	verifier    ResultVerifier
	verifyMu    sync.RWMutex
	verifyCache map[string]verificationCacheEntry
}

type verificationCacheEntry struct {
	result    Verification
	expiresAt time.Time
}

func NewEngine(options EngineOptions, sources ...Source) *Engine {
	if options.Concurrency <= 0 {
		options.Concurrency = 4
	}
	if options.Timeout <= 0 {
		options.Timeout = 18 * time.Second
	}
	if options.CacheTTL <= 0 {
		options.CacheTTL = 10 * time.Minute
	}
	if options.MaxResults <= 0 {
		options.MaxResults = 200
	}
	if options.VerificationConcurrency <= 0 {
		options.VerificationConcurrency = 2
	}
	if options.VerificationTimeout <= 0 {
		options.VerificationTimeout = 12 * time.Second
	}
	if options.VerificationTTL <= 0 {
		options.VerificationTTL = 15 * time.Minute
	}
	e := &Engine{
		options: options, cache: make(map[string]cacheEntry), health: make(map[string]SourceHealth),
		enabled: make(map[string]bool),
		jobs:    make(map[string]*searchJob), inflight: make(map[string]*searchJob), verifyCache: make(map[string]verificationCacheEntry),
	}
	for _, source := range sources {
		e.Register(source)
	}
	return e
}

// SetVerifier 注入分享核验能力。未注入时引擎仍可用于纯索引搜索和单元测试。
func (e *Engine) SetVerifier(verifier ResultVerifier) { e.verifier = verifier }

func (e *Engine) Register(source Source) {
	if source == nil || strings.TrimSpace(source.Descriptor().ID) == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.sources = append(e.sources, source)
	info := source.Descriptor()
	enabled := !info.DisabledByDefault
	e.enabled[info.ID] = enabled
	e.health[info.ID] = SourceHealth{
		ID: info.ID, Name: info.Name, Kind: info.Kind, Description: info.Description,
		Priority: info.Priority, Enabled: enabled, TimeoutMS: sourceTimeout(info, e.options.Timeout).Milliseconds(),
	}
}

// SetSourceEnabled updates one plugin without rebuilding the engine. Running
// searches keep their snapshot; subsequent searches use the new registry state.
func (e *Engine) SetSourceEnabled(id string, enabled bool) error {
	id = strings.TrimSpace(id)
	e.mu.Lock()
	defer e.mu.Unlock()
	health, ok := e.health[id]
	if !ok {
		return fmt.Errorf("未知资源来源 %q", id)
	}
	e.enabled[id] = enabled
	health.Enabled = enabled
	e.health[id] = health
	e.cache = make(map[string]cacheEntry)
	return nil
}

func sourceTimeout(info SourceDescriptor, fallback time.Duration) time.Duration {
	if info.Timeout > 0 {
		return info.Timeout
	}
	return fallback
}

func (e *Engine) Search(ctx context.Context, query string, refresh bool) ([]Result, Meta, error) {
	query = strings.TrimSpace(query)
	cacheKey := strings.ToLower(query)
	if !refresh {
		if items, meta, ok := e.cached(cacheKey); ok {
			return items, meta, nil
		}
	}

	return e.searchAll(ctx, query, cacheKey, nil)
}

// SearchProgressive 最多等待 wait；超时后先返回当前结果，慢来源继续在后台完成。
func (e *Engine) SearchProgressive(ctx context.Context, query string, refresh bool, wait time.Duration) (Snapshot, error) {
	query = strings.TrimSpace(query)
	key := strings.ToLower(query)
	if !refresh {
		if items, meta, ok := e.cached(key); ok {
			return Snapshot{Query: query, Items: items, Meta: meta}, nil
		}
	}
	job := e.getOrStartJob(query, key, refresh)
	if wait <= 0 {
		wait = 4 * time.Second
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-job.done:
		return job.current(), nil
	case <-timer.C:
		return job.current(), nil
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}

// Subscribe 订阅指定渐进式任务；返回当前快照，避免 WebSocket 建连期间漏掉更新。
func (e *Engine) Subscribe(jobID string) (Snapshot, <-chan Snapshot, func(), error) {
	e.mu.RLock()
	job := e.jobs[jobID]
	e.mu.RUnlock()
	if job == nil {
		return Snapshot{}, nil, nil, errors.New("搜索任务不存在或已过期")
	}
	stream := make(chan Snapshot, 8)
	job.mu.Lock()
	current := cloneSnapshot(job.snapshot)
	if current.Pending {
		job.subscribers[stream] = struct{}{}
	} else {
		close(stream)
	}
	job.mu.Unlock()
	unsubscribe := func() {
		job.mu.Lock()
		if _, ok := job.subscribers[stream]; ok {
			delete(job.subscribers, stream)
			close(stream)
		}
		job.mu.Unlock()
	}
	return current, stream, unsubscribe, nil
}

func (e *Engine) getOrStartJob(query, key string, refresh bool) *searchJob {
	e.mu.Lock()
	e.cleanupJobsLocked(time.Now())
	if !refresh {
		if running := e.inflight[key]; running != nil {
			e.mu.Unlock()
			return running
		}
	}
	id := fmt.Sprintf("search-%d-%d", time.Now().UnixMilli(), e.nextJob.Add(1))
	job := &searchJob{
		id: id, key: key, query: query, done: make(chan struct{}), subscribers: make(map[chan Snapshot]struct{}),
		snapshot: Snapshot{JobID: id, Query: query, Items: []Result{}, Meta: Meta{Source: "ivideo-discovery", Remaining: -1}, Pending: true},
	}
	e.jobs[id] = job
	e.inflight[key] = job
	e.mu.Unlock()

	go func() {
		items, meta, err := e.searchAll(context.Background(), query, key, func(items []Result, meta Meta) {
			job.publish(Snapshot{JobID: id, Query: query, Items: items, Meta: meta, Pending: true})
		})
		if err != nil {
			meta.Warnings = append(meta.Warnings, err.Error())
		}
		job.finish(Snapshot{JobID: id, Query: query, Items: items, Meta: meta, Pending: false})
		e.mu.Lock()
		if e.inflight[key] == job {
			delete(e.inflight, key)
		}
		e.mu.Unlock()
	}()
	return job
}

func (e *Engine) searchAll(ctx context.Context, query, cacheKey string, onUpdate func([]Result, Meta)) ([]Result, Meta, error) {
	e.mu.RLock()
	sources := make([]Source, 0, len(e.sources))
	for _, source := range e.sources {
		if e.enabled[source.Descriptor().ID] {
			sources = append(sources, source)
		}
	}
	e.mu.RUnlock()
	if len(sources) == 0 {
		return nil, Meta{Source: "ivideo-discovery"}, errors.New("资源发现引擎没有已启用的来源")
	}

	started := time.Now()
	type sourceResult struct {
		info  SourceDescriptor
		items []Result
		meta  Meta
		err   error
		spent time.Duration
	}
	results := make(chan sourceResult, len(sources))
	semaphore := make(chan struct{}, e.options.Concurrency)
	var wg sync.WaitGroup
	for _, source := range sources {
		wg.Add(1)
		go func(source Source) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			sourceCtx, cancel := context.WithTimeout(ctx, sourceTimeout(source.Descriptor(), e.options.Timeout))
			defer cancel()
			begin := time.Now()
			items, meta, err := source.Search(sourceCtx, query)
			results <- sourceResult{info: source.Descriptor(), items: items, meta: meta, err: err, spent: time.Since(begin)}
		}(source)
	}
	go func() { wg.Wait(); close(results) }()

	all := make([]Result, 0)
	meta := Meta{Source: "ivideo-discovery", Remaining: -1}
	for result := range results {
		report := SourceReport{
			ID: result.info.ID, Name: result.info.Name, Healthy: result.err == nil,
			ResultCount: len(result.items), Scanned: result.meta.Scanned, DurationMS: result.spent.Milliseconds(),
		}
		if result.err != nil {
			report.Error = result.err.Error()
			meta.Warnings = append(meta.Warnings, result.info.Name+": "+result.err.Error())
		} else {
			for i := range result.items {
				if result.items[i].Source == "" {
					result.items[i].Source = result.info.ID
				}
				if result.items[i].SourceName == "" {
					result.items[i].SourceName = result.info.Name
				}
				result.items[i].Score += result.info.Priority
			}
			all = append(all, result.items...)
		}
		meta.Scanned += result.meta.Scanned
		if result.meta.Remaining >= 0 && (meta.Remaining < 0 || result.meta.Remaining < meta.Remaining) {
			meta.Remaining = result.meta.Remaining
			meta.ResetAt = result.meta.ResetAt
		}
		meta.Sources = append(meta.Sources, report)
		e.recordHealth(result.info, result.err, result.spent)
		if onUpdate != nil {
			partialMeta := meta
			partialMeta.DurationMS = time.Since(started).Milliseconds()
			partial := rankAndMerge(all, query, e.options.MaxResults)
			if e.verifier != nil {
				for i := range partial {
					partial[i].Availability = AvailabilityChecking
				}
			}
			onUpdate(partial, partialMeta)
		}
	}
	sort.Slice(meta.Sources, func(i, j int) bool { return meta.Sources[i].ID < meta.Sources[j].ID })
	items := rankAndMerge(all, query, e.options.MaxResults)
	items, meta = e.verifyResults(ctx, items, meta)
	meta.DurationMS = time.Since(started).Milliseconds()
	e.storeCache(cacheKey, items, meta)
	return cloneResults(items), meta, nil
}

func (e *Engine) verifyResults(ctx context.Context, items []Result, meta Meta) ([]Result, Meta) {
	if e.verifier == nil || len(items) == 0 {
		return items, meta
	}
	type checked struct {
		index int
		value Verification
	}
	results := make(chan checked, len(items))
	semaphore := make(chan struct{}, e.options.VerificationConcurrency)
	var wg sync.WaitGroup
	for index, item := range items {
		wg.Add(1)
		go func(index int, item Result) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			results <- checked{index: index, value: e.verifyOne(ctx, item)}
		}(index, item)
	}
	go func() { wg.Wait(); close(results) }()
	verified := make([]Verification, len(items))
	for result := range results {
		verified[result.index] = result.value
	}
	kept := make([]Result, 0, len(items))
	for index, item := range items {
		verification := verified[index]
		item.Availability = verification.Status
		item.EntryCount = verification.Count
		item.VerifyMessage = verification.Message
		if !verification.At.IsZero() {
			item.VerifiedAt = verification.At.Unix()
		}
		switch verification.Status {
		case AvailabilityAvailable:
			meta.Verified++
			kept = append(kept, item)
		case AvailabilityEmpty, AvailabilityInvalid:
			meta.Rejected++
		default:
			meta.Unverified++
			kept = append(kept, item)
		}
	}
	if meta.Rejected > 0 {
		meta.Warnings = append(meta.Warnings, fmt.Sprintf("已隐藏 %d 条空分享或失效分享", meta.Rejected))
	}
	return kept, meta
}

func (e *Engine) verifyOne(ctx context.Context, item Result) Verification {
	key := canonicalShareKey(item)
	now := time.Now()
	e.verifyMu.RLock()
	cached, ok := e.verifyCache[key]
	e.verifyMu.RUnlock()
	if ok && now.Before(cached.expiresAt) {
		return cached.result
	}
	verifyCtx, cancel := context.WithTimeout(ctx, e.options.VerificationTimeout)
	defer cancel()
	result := e.verifier.Verify(verifyCtx, item)
	if result.Status == "" {
		result.Status = AvailabilityUnknown
	}
	if result.At.IsZero() {
		result.At = now
	}
	ttl := e.options.VerificationTTL
	if result.Status == AvailabilityUnknown && ttl > time.Minute {
		ttl = time.Minute
	}
	e.verifyMu.Lock()
	e.verifyCache[key] = verificationCacheEntry{result: result, expiresAt: now.Add(ttl)}
	e.verifyMu.Unlock()
	return result
}

func (e *Engine) cleanupJobsLocked(now time.Time) {
	for id, job := range e.jobs {
		job.mu.RLock()
		finishedAt := job.finishedAt
		job.mu.RUnlock()
		if !finishedAt.IsZero() && now.Sub(finishedAt) > 15*time.Minute {
			delete(e.jobs, id)
		}
	}
}

func (j *searchJob) current() Snapshot {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return cloneSnapshot(j.snapshot)
}

func (j *searchJob) publish(snapshot Snapshot) {
	j.mu.Lock()
	j.snapshot = cloneSnapshot(snapshot)
	for stream := range j.subscribers {
		select {
		case stream <- cloneSnapshot(snapshot):
		default:
		}
	}
	j.mu.Unlock()
}

func (j *searchJob) finish(snapshot Snapshot) {
	j.mu.Lock()
	j.snapshot = cloneSnapshot(snapshot)
	j.finishedAt = time.Now()
	for stream := range j.subscribers {
		select {
		case stream <- cloneSnapshot(snapshot):
		default:
		}
		close(stream)
		delete(j.subscribers, stream)
	}
	close(j.done)
	j.mu.Unlock()
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Items = cloneResults(snapshot.Items)
	snapshot.Meta.Sources = append([]SourceReport(nil), snapshot.Meta.Sources...)
	snapshot.Meta.Warnings = append([]string(nil), snapshot.Meta.Warnings...)
	return snapshot
}

func (e *Engine) Status() []SourceHealth {
	e.mu.RLock()
	defer e.mu.RUnlock()
	items := make([]SourceHealth, 0, len(e.health))
	for _, health := range e.health {
		items = append(items, health)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority > items[j].Priority
		}
		return items[i].Name < items[j].Name
	})
	return items
}

func (e *Engine) cached(key string) ([]Result, Meta, bool) {
	e.mu.RLock()
	entry, ok := e.cache[key]
	e.mu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			e.mu.Lock()
			delete(e.cache, key)
			e.mu.Unlock()
		}
		return nil, Meta{}, false
	}
	meta := entry.meta
	meta.Cached = true
	return cloneResults(entry.items), meta, true
}

func (e *Engine) storeCache(key string, items []Result, meta Meta) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cache[key] = cacheEntry{items: cloneResults(items), meta: meta, expiresAt: time.Now().Add(e.options.CacheTTL)}
}

func (e *Engine) recordHealth(info SourceDescriptor, err error, spent time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	health := e.health[info.ID]
	health.ID, health.Name, health.Kind, health.Description, health.Priority = info.ID, info.Name, info.Kind, info.Description, info.Priority
	health.Enabled = e.enabled[info.ID]
	health.TimeoutMS = sourceTimeout(info, e.options.Timeout).Milliseconds()
	health.LastCheckedAt = time.Now().Unix()
	health.LastDuration = spent.Milliseconds()
	health.LastHealthy = err == nil
	if err == nil {
		health.SuccessCount++
		health.LastError = ""
	} else {
		health.FailureCount++
		health.LastError = err.Error()
	}
	e.health[info.ID] = health
}

func rankAndMerge(items []Result, query string, limit int) []Result {
	queryKey := normalizeSearchText(query)
	merged := make(map[string]Result)
	order := make([]string, 0, len(items))
	for _, item := range items {
		key := canonicalShareKey(item)
		if key == "" || item.Provider == "" {
			continue
		}
		item.Score += relevanceScore(item, queryKey)
		item.Sources = uniqueStrings(append(item.Sources, item.Source))
		if current, ok := merged[key]; ok {
			merged[key] = mergeRankedResult(current, item)
			continue
		}
		merged[key] = item
		order = append(order, key)
	}
	result := make([]Result, 0, len(order))
	for _, key := range order {
		result = append(result, merged[key])
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}
		if result[i].UpdatedAt != result[j].UpdatedAt {
			return result[i].UpdatedAt > result[j].UpdatedAt
		}
		return result[i].Title < result[j].Title
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result
}

func relevanceScore(item Result, query string) int {
	title := normalizeSearchText(item.Title)
	fileName := normalizeSearchText(item.FileName)
	score := 0
	if title == query && query != "" {
		score += 120
	} else if strings.Contains(title, query) {
		score += 70
	} else if strings.Contains(fileName, query) {
		score += 45
	}
	if item.ResourceType != "" {
		score += 12
	}
	if item.FileName != "" {
		score += 8
	}
	if item.SharePwd != "" {
		score += 3
	}
	if item.UpdatedAt != "" {
		score += 2
	}
	return score
}

func mergeRankedResult(a, b Result) Result {
	best, other := a, b
	if b.Score > a.Score {
		best, other = b, a
	}
	if best.Title == "" {
		best.Title = other.Title
	}
	if best.SharePwd == "" {
		best.SharePwd = other.SharePwd
	}
	if best.ResourceType == "" {
		best.ResourceType = other.ResourceType
	}
	if best.FileName == "" {
		best.FileName = other.FileName
	}
	if best.UpdatedAt == "" {
		best.UpdatedAt = other.UpdatedAt
	}
	if best.Repository == "" {
		best.Repository = other.Repository
	}
	if best.Path == "" {
		best.Path = other.Path
	}
	if best.SourceURL == "" {
		best.SourceURL = other.SourceURL
	}
	best.Sources = uniqueStrings(append(best.Sources, other.Sources...))
	return best
}

func canonicalShareKey(item Result) string {
	parsed, err := url.Parse(strings.TrimSpace(item.ShareURL))
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == "s" && parts[i+1] != "" {
			return item.Provider + ":" + strings.ToLower(parts[i+1])
		}
	}
	parsed.RawQuery, parsed.Fragment = "", ""
	return item.Provider + ":" + strings.ToLower(parsed.String())
}

func normalizeSearchText(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), "")
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}

func cloneResults(items []Result) []Result {
	// JSON 中空结果必须是 []，避免前端把 null 当数组处理时崩溃。
	cloned := make([]Result, len(items))
	copy(cloned, items)
	for i := range cloned {
		cloned[i].Sources = append([]string(nil), cloned[i].Sources...)
	}
	return cloned
}
