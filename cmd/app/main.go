package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gotd/td/telegram"

	"tg-adv-bot/internal/config"
	"tg-adv-bot/internal/filter"
	"tg-adv-bot/internal/judge"
	"tg-adv-bot/internal/netdial"
	"tg-adv-bot/internal/service"
	"tg-adv-bot/internal/state"
	"tg-adv-bot/internal/tg"
)

const (
	commandRun   = "run"
	commandLogin = "login"
	llmTimeout   = 60 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ошибка:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)

	// gotd пишет session.json обычным WriteFile без создания каталога:
	// без этой строки сервис молча уходит в бесконечный реконнект.
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("DATA_DIR %s: %w", cfg.DataDir, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dial, err := netdial.Dialer(cfg.Proxy)
	if err != nil {
		return err
	}
	client := tg.NewClient(tg.Options{
		APIID:   cfg.APIID,
		APIHash: cfg.APIHash,
		Session: cfg.SessionPath(),
		Dial:    dial,
		Log:     log,
	})

	switch command(os.Args) {
	case commandLogin:
		return tg.Login(ctx, client, cfg.Phone)
	case commandRun:
		if err := cfg.ValidateRun(); err != nil {
			return err
		}
		return serve(ctx, cfg, client, dial, log)
	default:
		return fmt.Errorf("неизвестная команда %q, есть %s и %s", command(os.Args), commandRun, commandLogin)
	}
}

func serve(
	ctx context.Context,
	cfg config.Config,
	client *telegram.Client,
	dial netdial.DialFunc,
	log *slog.Logger,
) error {
	keywords, err := filter.Load(cfg.KeywordsPath)
	if err != nil {
		return fmt.Errorf("кейворды %s: %w", cfg.KeywordsPath, err)
	}
	if len(keywords) == 0 {
		return fmt.Errorf("%s: ни одного кейворда", cfg.KeywordsPath)
	}
	prompt, err := os.ReadFile(cfg.PromptPath)
	if err != nil {
		return fmt.Errorf("промпт %s: %w", cfg.PromptPath, err)
	}

	llm := judge.New(judge.Config{
		BaseURL: cfg.LLMBaseURL,
		Key:     cfg.LLMAPIKey,
		Model:   cfg.LLMModel,
		Prompt:  string(prompt),
		Headers: cfg.LLMHeaders,
		HTTP:    httpClient(dial),
	})
	if err := llm.Probe(ctx); err != nil {
		return err
	}

	return client.Run(ctx, func(ctx context.Context) error {
		// Без сессии gotd молча переподключается вечно, поэтому проверяем явно.
		status, err := client.Auth().Status(ctx)
		if err != nil {
			return fmt.Errorf("проверка сессии: %w", err)
		}
		if !status.Authorized {
			return errors.New("сессия не авторизована — выполни ./run.sh login")
		}
		log.Info("авторизован", "user", status.User.Username)

		api := client.API()
		channel, err := tg.ResolveChannel(ctx, api, cfg.Channel)
		if err != nil {
			return err
		}
		recipient, err := tg.ResolveRecipient(ctx, api, cfg.Recipient)
		if err != nil {
			return err
		}
		log.Info("слушаю группу",
			"channel", cfg.Channel,
			"recipient", cfg.Recipient,
			"interval", cfg.Interval.String(),
			"backfill", cfg.Backfill,
			"dedup", cfg.DedupSize,
		)

		pipeline := service.New(service.Deps{
			Source:    tg.NewSource(api, channel, log),
			Judge:     llm,
			Forward:   tg.NewForwarder(api, channel, recipient),
			Store:     state.New(cfg.StatePath()),
			Seen:      state.NewSeen(cfg.DedupPath()),
			Keywords:  keywords,
			Backfill:  cfg.Backfill,
			DedupSize: cfg.DedupSize,
			Interval:  cfg.Interval,
			Log:       log,
		})
		return pipeline.Run(ctx)
	})
}

func command(args []string) string {
	if len(args) < 2 {
		return commandRun
	}
	return args[1]
}

func httpClient(dial netdial.DialFunc) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if dial != nil {
		transport.DialContext = dial
	}
	return &http.Client{Transport: transport, Timeout: llmTimeout}
}

func newLogger(level string) *slog.Logger {
	var parsed slog.Level
	if err := parsed.UnmarshalText([]byte(level)); err != nil {
		parsed = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: parsed}))
}
