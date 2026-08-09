package resourcesearch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestEngineMergesDuplicateSharesAndKeepsRichMetadata(t *testing.T) {
	engine := NewEngine(EngineOptions{CacheTTL: time.Minute, MaxResults: 20},
		SourceFunc{
			Info: SourceDescriptor{ID: "structured", Name: "结构化来源", Priority: 90},
			SearchFunc: func(context.Context, string) ([]Result, Meta, error) {
				return []Result{{Provider: "aliyun", ShareURL: "https://www.alipan.com/s/ABC123?from=x", Title: "流浪地球", ResourceType: "电影", FileName: "流浪地球.2160p.mkv"}}, Meta{Scanned: 1}, nil
			},
		},
		SourceFunc{
			Info: SourceDescriptor{ID: "generic", Name: "通用来源", Priority: 50},
			SearchFunc: func(context.Context, string) ([]Result, Meta, error) {
				return []Result{{Provider: "aliyun", ShareURL: "https://www.aliyundrive.com/s/abc123", Title: "流浪地球", SharePwd: "a1b2"}}, Meta{Scanned: 2}, nil
			},
		},
	)

	items, meta, err := engine.Search(context.Background(), "流浪地球", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one merged item, got %d", len(items))
	}
	if items[0].ResourceType != "电影" || items[0].SharePwd != "a1b2" || len(items[0].Sources) != 2 {
		t.Fatalf("metadata was not merged: %#v", items[0])
	}
	if meta.Scanned != 3 || len(meta.Sources) != 2 {
		t.Fatalf("unexpected meta: %#v", meta)
	}
}

func TestEngineCachesQueries(t *testing.T) {
	var calls atomic.Int32
	engine := NewEngine(EngineOptions{CacheTTL: time.Minute}, SourceFunc{
		Info: SourceDescriptor{ID: "counted", Name: "计数来源"},
		SearchFunc: func(context.Context, string) ([]Result, Meta, error) {
			calls.Add(1)
			return []Result{{Provider: "quark", ShareURL: "https://pan.quark.cn/s/token", Title: "测试"}}, Meta{}, nil
		},
	})
	_, first, _ := engine.Search(context.Background(), "测试", false)
	_, second, _ := engine.Search(context.Background(), "测试", false)
	if first.Cached || !second.Cached || calls.Load() != 1 {
		t.Fatalf("cache was not used: first=%#v second=%#v calls=%d", first, second, calls.Load())
	}
	_, _, _ = engine.Search(context.Background(), "测试", true)
	if calls.Load() != 2 {
		t.Fatalf("refresh should bypass cache, calls=%d", calls.Load())
	}
}

func TestEngineSerializesEmptyResultsAsArray(t *testing.T) {
	engine := NewEngine(EngineOptions{CacheTTL: time.Minute}, SourceFunc{
		Info: SourceDescriptor{ID: "empty", Name: "空来源"},
		SearchFunc: func(context.Context, string) ([]Result, Meta, error) {
			return nil, Meta{}, nil
		},
	})

	snapshot, err := engine.SearchProgressive(context.Background(), "不存在", false, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"items":[]`) {
		t.Fatalf("empty results must serialize as an array: %s", payload)
	}
}

func TestEngineKeepsPartialResultsWhenSourceFails(t *testing.T) {
	engine := NewEngine(EngineOptions{},
		SourceFunc{
			Info: SourceDescriptor{ID: "failed", Name: "失败来源"},
			SearchFunc: func(context.Context, string) ([]Result, Meta, error) {
				return nil, Meta{}, errors.New("temporary failure")
			},
		},
		SourceFunc{
			Info: SourceDescriptor{ID: "working", Name: "正常来源"},
			SearchFunc: func(context.Context, string) ([]Result, Meta, error) {
				return []Result{{Provider: "115", ShareURL: "https://115.com/s/shareid", Title: "测试"}}, Meta{}, nil
			},
		},
	)
	items, meta, err := engine.Search(context.Background(), "测试", false)
	if err != nil || len(items) != 1 || len(meta.Warnings) != 1 {
		t.Fatalf("partial result was lost: items=%#v meta=%#v err=%v", items, meta, err)
	}
	health := engine.Status()
	if len(health) != 2 {
		t.Fatalf("unexpected health count: %d", len(health))
	}
}

type verifierFunc func(context.Context, Result) Verification

func (f verifierFunc) Verify(ctx context.Context, result Result) Verification { return f(ctx, result) }

func TestEngineFiltersEmptyAndInvalidSharesAfterVerification(t *testing.T) {
	engine := NewEngine(EngineOptions{CacheTTL: time.Minute}, SourceFunc{
		Info: SourceDescriptor{ID: "source", Name: "测试来源"},
		SearchFunc: func(context.Context, string) ([]Result, Meta, error) {
			return []Result{
				{Provider: "aliyun", ShareURL: "https://www.alipan.com/s/available", Title: "测试"},
				{Provider: "aliyun", ShareURL: "https://www.alipan.com/s/empty", Title: "测试"},
				{Provider: "quark", ShareURL: "https://pan.quark.cn/s/unknown", Title: "测试"},
			}, Meta{}, nil
		},
	})
	engine.SetVerifier(verifierFunc(func(_ context.Context, result Result) Verification {
		switch {
		case strings.Contains(result.ShareURL, "available"):
			return Verification{Status: AvailabilityAvailable, Count: 2}
		case strings.Contains(result.ShareURL, "empty"):
			return Verification{Status: AvailabilityEmpty}
		default:
			return Verification{Status: AvailabilityUnknown, Message: "限流"}
		}
	}))

	items, meta, err := engine.Search(context.Background(), "测试", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || meta.Verified != 1 || meta.Rejected != 1 || meta.Unverified != 1 {
		t.Fatalf("unexpected verification result: items=%#v meta=%#v", items, meta)
	}
	for _, item := range items {
		if strings.Contains(item.ShareURL, "empty") {
			t.Fatalf("empty share was not filtered: %#v", item)
		}
	}
}

func TestEngineCachesShareVerificationAcrossQueries(t *testing.T) {
	var checks atomic.Int32
	engine := NewEngine(EngineOptions{CacheTTL: time.Millisecond, VerificationTTL: time.Minute}, SourceFunc{
		Info: SourceDescriptor{ID: "source", Name: "测试来源"},
		SearchFunc: func(context.Context, string) ([]Result, Meta, error) {
			return []Result{{Provider: "aliyun", ShareURL: "https://www.alipan.com/s/same", Title: "测试"}}, Meta{}, nil
		},
	})
	engine.SetVerifier(verifierFunc(func(context.Context, Result) Verification {
		checks.Add(1)
		return Verification{Status: AvailabilityAvailable, Count: 1}
	}))
	_, _, _ = engine.Search(context.Background(), "测试一", true)
	_, _, _ = engine.Search(context.Background(), "测试二", true)
	if checks.Load() != 1 {
		t.Fatalf("verification cache was not used: %d", checks.Load())
	}
}

func TestProgressiveSearchReturnsFastThenPublishesCompletion(t *testing.T) {
	engine := NewEngine(EngineOptions{Timeout: time.Second, CacheTTL: time.Minute},
		SourceFunc{
			Info: SourceDescriptor{ID: "fast", Name: "快速来源", Priority: 20},
			SearchFunc: func(context.Context, string) ([]Result, Meta, error) {
				time.Sleep(5 * time.Millisecond)
				return []Result{{Provider: "quark", ShareURL: "https://pan.quark.cn/s/fast", Title: "测试"}}, Meta{}, nil
			},
		},
		SourceFunc{
			Info: SourceDescriptor{ID: "slow", Name: "慢速来源", Priority: 10},
			SearchFunc: func(context.Context, string) ([]Result, Meta, error) {
				time.Sleep(80 * time.Millisecond)
				return []Result{{Provider: "115", ShareURL: "https://115.com/s/slow", Title: "测试"}}, Meta{}, nil
			},
		},
	)

	snapshot, err := engine.SearchProgressive(context.Background(), "测试", false, 25*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Pending || len(snapshot.Items) != 1 || snapshot.JobID == "" {
		t.Fatalf("expected one fast partial result: %#v", snapshot)
	}
	current, stream, unsubscribe, err := engine.Subscribe(snapshot.JobID)
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	if len(current.Items) != 1 || !current.Pending {
		t.Fatalf("subscription did not start from current snapshot: %#v", current)
	}
	deadline := time.After(time.Second)
	for {
		select {
		case update, ok := <-stream:
			if !ok {
				t.Fatal("stream closed before final snapshot")
			}
			if !update.Pending {
				if len(update.Items) != 2 {
					t.Fatalf("final result is incomplete: %#v", update)
				}
				cached, err := engine.SearchProgressive(context.Background(), "测试", false, time.Millisecond)
				if err != nil || cached.Pending || !cached.Meta.Cached || len(cached.Items) != 2 {
					t.Fatalf("completed result was not cached: %#v err=%v", cached, err)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for final snapshot")
		}
	}
}

func TestProgressiveSearchSharesInflightJob(t *testing.T) {
	var calls atomic.Int32
	engine := NewEngine(EngineOptions{Timeout: time.Second}, SourceFunc{
		Info: SourceDescriptor{ID: "slow", Name: "慢速来源"},
		SearchFunc: func(context.Context, string) ([]Result, Meta, error) {
			calls.Add(1)
			time.Sleep(70 * time.Millisecond)
			return []Result{{Provider: "aliyun", ShareURL: "https://www.alipan.com/s/shared", Title: "测试"}}, Meta{}, nil
		},
	})
	first, _ := engine.SearchProgressive(context.Background(), "测试", false, 5*time.Millisecond)
	second, _ := engine.SearchProgressive(context.Background(), "测试", false, 5*time.Millisecond)
	if first.JobID == "" || first.JobID != second.JobID || calls.Load() != 1 {
		t.Fatalf("inflight search was not shared: first=%#v second=%#v calls=%d", first, second, calls.Load())
	}
}
