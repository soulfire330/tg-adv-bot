package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBackfill = 10
	defaultInterval = 30 * time.Second
	defaultDataDir  = "/data"
)

// Config — всё, что приходит из окружения.
type Config struct {
	APIID        int
	APIHash      string
	Phone        string
	Channel      string
	Recipient    string
	LLMBaseURL   string
	LLMAPIKey    string
	LLMModel     string
	LLMHeaders   map[string]string
	Proxy        string
	Backfill     int
	Interval     time.Duration
	DataDir      string
	LogLevel     string
	KeywordsPath string
	PromptPath   string
}

// Load читает окружение и проверяет то, что нужно и сервису, и логину.
func Load() (Config, error) {
	apiID, err := envInt("TG_API_ID", 0)
	if err != nil {
		return Config{}, err
	}
	interval, err := envDuration("POLL_INTERVAL", defaultInterval)
	if err != nil {
		return Config{}, err
	}
	backfill, err := envInt("BACKFILL", defaultBackfill)
	if err != nil {
		return Config{}, err
	}
	if backfill < 1 {
		backfill = 1
	}

	cfg := Config{
		APIID:        apiID,
		APIHash:      os.Getenv("TG_API_HASH"),
		Phone:        os.Getenv("TG_PHONE"),
		Channel:      os.Getenv("TG_CHANNEL"),
		Recipient:    os.Getenv("TG_RECIPIENT"),
		LLMBaseURL:   os.Getenv("LLM_BASE_URL"),
		LLMAPIKey:    os.Getenv("LLM_API_KEY"),
		LLMModel:     os.Getenv("LLM_MODEL"),
		LLMHeaders:   parseHeaders(os.Getenv("LLM_HEADERS")),
		Proxy:        os.Getenv("SOCKS5_PROXY"),
		Backfill:     backfill,
		Interval:     interval,
		DataDir:      env("DATA_DIR", defaultDataDir),
		LogLevel:     env("LOG_LEVEL", "info"),
		KeywordsPath: env("KEYWORDS_PATH", "keywords.txt"),
		PromptPath:   env("PROMPT_PATH", "prompt.md"),
	}

	var missing []string
	for _, f := range []struct {
		name  string
		value string
	}{
		{"TG_API_HASH", cfg.APIHash},
		{"TG_PHONE", cfg.Phone},
	} {
		if f.value == "" {
			missing = append(missing, f.name)
		}
	}
	if cfg.APIID == 0 {
		missing = append(missing, "TG_API_ID")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("не заполнено: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

// ValidateRun проверяет настройки, без которых сервис не может работать.
func (c Config) ValidateRun() error {
	var missing []string
	for _, f := range []struct {
		name  string
		value string
	}{
		{"TG_CHANNEL", c.Channel},
		{"TG_RECIPIENT", c.Recipient},
		{"LLM_BASE_URL", c.LLMBaseURL},
		{"LLM_MODEL", c.LLMModel},
	} {
		if f.value == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("не заполнено: %s", strings.Join(missing, ", "))
	}
	return nil
}

// SessionPath — файл сессии MTProto, живёт на volume вместе со state.
func (c Config) SessionPath() string {
	return filepath.Join(c.DataDir, "session.json")
}

// StatePath — файл с id последнего обработанного поста.
func (c Config) StatePath() string {
	return filepath.Join(c.DataDir, "state.json")
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func envInt(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return v, nil
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", name, err)
	}
	return v, nil
}

// parseHeaders разбирает "name: value; name2: value2" из LLM_HEADERS.
func parseHeaders(raw string) map[string]string {
	headers := make(map[string]string)
	for _, pair := range strings.Split(raw, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(pair), ":")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		headers[name] = strings.TrimSpace(value)
	}
	return headers
}
