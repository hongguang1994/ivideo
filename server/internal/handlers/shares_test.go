package handlers

import (
	"testing"

	"ivideo/server/internal/store"
)

func TestDetectShareProvider(t *testing.T) {
	tests := map[string]string{
		"https://www.alipan.com/s/abc":      "aliyun",
		"https://www.aliyundrive.com/s/abc": "aliyun",
		"https://115.com/s/abc":             "115",
		"https://pan.quark.cn/s/abc":        "quark",
		"https://example.com/s/abc":         "",
	}
	for rawURL, want := range tests {
		if got := detectShareProvider(rawURL); got != want {
			t.Errorf("detectShareProvider(%q) = %q, want %q", rawURL, got, want)
		}
	}
}

func TestValidateBatchShare(t *testing.T) {
	valid := store.Share{Provider: "quark", ShareURL: "https://pan.quark.cn/s/abc"}
	if got := validateBatchShare(valid); got != "" {
		t.Fatalf("valid share rejected: %s", got)
	}

	wrongProvider := store.Share{Provider: "aliyun", ShareURL: "https://pan.quark.cn/s/abc"}
	if got := validateBatchShare(wrongProvider); got == "" {
		t.Fatal("provider mismatch was accepted")
	}

	unsupported := store.Share{Provider: "pikpak", ShareURL: "https://example.com/s/abc"}
	if got := validateBatchShare(unsupported); got == "" {
		t.Fatal("unsupported provider was accepted")
	}
}

func TestBatchShareKeyIgnoresFragmentAndHostCase(t *testing.T) {
	a := batchShareKey("Aliyun", "https://WWW.ALIPAN.COM/s/abc#folder")
	b := batchShareKey("aliyun", "https://www.alipan.com/s/abc")
	if a != b {
		t.Fatalf("equivalent links produced different keys: %q != %q", a, b)
	}
}
