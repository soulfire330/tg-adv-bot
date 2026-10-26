// Package tg — всё, что говорит с Telegram через MTProto.
package tg

import (
	"context"
	"net"

	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/dcs"
)

// Options — то, что нужно клиенту, чтобы подключиться.
type Options struct {
	APIID   int
	APIHash string
	Session string
	Dial    func(ctx context.Context, network, addr string) (net.Conn, error)
}

// NewClient собирает MTProto-клиент: сессия на диске, апдейты не нужны —
// источник постов опрашивает историю.
func NewClient(opts Options) *telegram.Client {
	options := telegram.Options{
		SessionStorage: &session.FileStorage{Path: opts.Session},
		NoUpdates:      true,
	}
	if opts.Dial != nil {
		options.Resolver = dcs.Plain(dcs.PlainOptions{Dial: opts.Dial})
	}
	return telegram.NewClient(opts.APIID, opts.APIHash, options)
}
