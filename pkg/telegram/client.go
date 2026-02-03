package telegram

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
	"golang.org/x/net/proxy"
)

// ParseProxyURL parses and validates a proxy URL
// Supported formats:
//   - socks5://host:port
//   - socks5://user:pass@host:port
//   - http://host:port
//   - http://user:pass@host:port
func ParseProxyURL(proxyURL string) (*url.URL, error) {
	if proxyURL == "" {
		return nil, nil
	}

	u, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" {
		return nil, fmt.Errorf("unsupported proxy type: %s (use http, https, or socks5)", u.Scheme)
	}

	if u.Host == "" {
		return nil, fmt.Errorf("proxy host is required")
	}

	return u, nil
}

// CreateDialer creates a dial function with optional proxy support
func CreateDialer(proxyURL string) (dcs.DialFunc, error) {
	if proxyURL == "" {
		return nil, nil // Use default dialer
	}

	u, err := ParseProxyURL(proxyURL)
	if err != nil {
		return nil, err
	}

	switch u.Scheme {
	case "socks5":
		return createSocks5Dialer(u)
	case "http", "https":
		return createHTTPProxyDialer(u)
	default:
		return nil, fmt.Errorf("unsupported proxy type: %s", u.Scheme)
	}
}

// createSocks5Dialer creates a SOCKS5 proxy dialer
func createSocks5Dialer(u *url.URL) (dcs.DialFunc, error) {
	var auth *proxy.Auth
	if u.User != nil {
		password, _ := u.User.Password()
		auth = &proxy.Auth{
			User:     u.User.Username(),
			Password: password,
		}
	}

	dialer, err := proxy.SOCKS5("tcp", u.Host, auth, &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create SOCKS5 dialer: %w", err)
	}

	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialer.Dial(network, addr)
	}, nil
}

// createHTTPProxyDialer creates an HTTP CONNECT proxy dialer
func createHTTPProxyDialer(u *url.URL) (dcs.DialFunc, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		// Connect to proxy
		conn, err := net.DialTimeout("tcp", u.Host, 30*time.Second)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to proxy: %w", err)
		}

		// Send CONNECT request
		connectReq := &http.Request{
			Method: "CONNECT",
			URL:    &url.URL{Opaque: addr},
			Host:   addr,
			Header: make(http.Header),
		}

		// Add proxy authentication if provided
		if u.User != nil {
			password, _ := u.User.Password()
			connectReq.SetBasicAuth(u.User.Username(), password)
		}

		if err := connectReq.Write(conn); err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to write CONNECT request: %w", err)
		}

		// Read response using buffered reader
		br := bufio.NewReader(conn)
		resp, err := http.ReadResponse(br, connectReq)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("failed to read CONNECT response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			conn.Close()
			return nil, fmt.Errorf("proxy CONNECT failed with status: %d", resp.StatusCode)
		}

		return conn, nil
	}, nil
}

// CreateClient creates a new Telegram client with optional proxy support
func CreateClient(appID int, appHash, sessionPath, proxyURL string) (*telegram.Client, error) {
	return CreateClientWithHandler(appID, appHash, sessionPath, proxyURL, nil)
}

// CreateClientWithHandler creates a new Telegram client with optional proxy and update handler
func CreateClientWithHandler(appID int, appHash, sessionPath, proxyURL string, handler telegram.UpdateHandler) (*telegram.Client, error) {
	opts := telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{
			Path: sessionPath,
		},
	}

	if handler != nil {
		opts.UpdateHandler = handler
	}

	// Configure proxy if provided
	if proxyURL != "" {
		dialFunc, err := CreateDialer(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("failed to create proxy dialer: %w", err)
		}

		if dialFunc != nil {
			opts.Resolver = dcs.Plain(dcs.PlainOptions{
				Dial: dialFunc,
			})
		}
	}

	return telegram.NewClient(appID, appHash, opts), nil
}

// TestProxy tests proxy connectivity by attempting to connect to a Telegram DC
func TestProxy(ctx context.Context, proxyURL string) error {
	if proxyURL == "" {
		return fmt.Errorf("proxy URL is required")
	}

	dialFunc, err := CreateDialer(proxyURL)
	if err != nil {
		return err
	}

	// Test connection to Telegram DC2 (149.154.167.40:443)
	// This is a well-known Telegram data center IP
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := dialFunc(testCtx, "tcp", "149.154.167.40:443")
	if err != nil {
		return fmt.Errorf("failed to connect through proxy: %w", err)
	}
	conn.Close()

	return nil
}
