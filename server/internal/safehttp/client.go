// Package safehttp provides a constrained HTTP client for user supplied URLs.
package safehttp

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"time"
)

// NewExternalClient only connects to globally routable addresses. The check is
// performed at dial time so a hostname cannot bypass it through DNS rebinding.
func NewExternalClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("解析外部地址失败: %w", err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("外部地址没有可用 IP")
		}
		for _, ip := range ips {
			if !isPublicIP(ip) {
				return nil, fmt.Errorf("不允许访问非公网地址")
			}
		}
		// Dial a resolved address directly after validation. TLS still uses the
		// original request host for SNI and certificate verification.
		return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("重定向次数过多")
			}
			if request.URL.Scheme != "http" && request.URL.Scheme != "https" {
				return fmt.Errorf("只允许 HTTP 或 HTTPS 重定向")
			}
			return nil
		},
	}
}

func isPublicIP(ip netip.Addr) bool {
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	// Carrier-grade NAT is not public Internet space and should not be probed.
	return !netip.MustParsePrefix("100.64.0.0/10").Contains(ip)
}
