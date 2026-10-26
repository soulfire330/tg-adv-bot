// Package netdial — единственная точка, где создаётся SOCKS5-диалер.
package netdial

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"

	"golang.org/x/net/proxy"
)

// DialFunc — то, что ждут и http.Transport, и gotd/td.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// Dialer строит диалер по socks5h-URL. Пустая строка — прямые соединения (nil).
// Имя хоста уходит в прокси, локального DNS нет — это и есть socks5h.
func Dialer(raw string) (DialFunc, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("SOCKS5_PROXY: %w", err)
	}
	switch parsed.Scheme {
	case "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("SOCKS5_PROXY: схема %q не поддерживается, нужен socks5h://", parsed.Scheme)
	}
	if parsed.Host == "" {
		return nil, errors.New("SOCKS5_PROXY: нет host:port")
	}

	var auth *proxy.Auth
	if parsed.User != nil {
		password, _ := parsed.User.Password()
		auth = &proxy.Auth{User: parsed.User.Username(), Password: password}
	}

	dialer, err := proxy.SOCKS5("tcp", parsed.Host, auth, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("SOCKS5_PROXY: %w", err)
	}
	ctxDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		return nil, errors.New("SOCKS5_PROXY: диалер без поддержки контекста")
	}
	return ctxDialer.DialContext, nil
}
