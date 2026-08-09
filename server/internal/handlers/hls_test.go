package handlers

import (
	"testing"

	"ivideo/server/internal/config"
)

func TestHLSHostAllowed(t *testing.T) {
	h := &Handler{cfg: config.Config{HLSAllowedHosts: []string{"aliyundrive.net", "alicdn.com"}}}
	cases := map[string]bool{
		"aliyundrive.net":              true,
		"cdn.aliyundrive.net":          true,
		"video.alicdn.com":             true,
		"aliyundrive.net.evil.example": false,
		"not-alicdn.com.evil.example":  false,
		"evilaliyundrive.net":          false,
	}
	for host, want := range cases {
		if got := h.hlsHostAllowed(host); got != want {
			t.Errorf("hlsHostAllowed(%q) = %v, want %v", host, got, want)
		}
	}
}
