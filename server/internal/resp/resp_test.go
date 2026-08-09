package resp

import (
	"strings"
	"testing"
)

func TestSafeMessageHidesUpstreamURLsAndSecrets(t *testing.T) {
	message := `请求 https://example.com/play?access_token=secret-123 返回失败，cookie=abc; Authorization: Bearer top-secret`
	safe := SafeMessage(message)
	for _, secret := range []string{"example.com", "secret-123", "abc", "top-secret"} {
		if strings.Contains(safe, secret) {
			t.Fatalf("safe message leaked %q: %s", secret, safe)
		}
	}
	if !strings.Contains(safe, "上游服务") || !strings.Contains(safe, "已隐藏") {
		t.Fatalf("safe message lost useful context: %s", safe)
	}
}

func TestSafeMessageBoundsLargeUpstreamBody(t *testing.T) {
	safe := SafeMessage(strings.Repeat("x", 801))
	if safe != "上游服务返回了过长的错误信息，请查看后端日志" {
		t.Fatalf("unexpected message: %s", safe)
	}
}
