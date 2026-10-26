package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("TG_API_ID", "123")
	t.Setenv("TG_API_HASH", "hash")
	t.Setenv("TG_PHONE", "+100")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Backfill != 10 {
		t.Errorf("Backfill = %d, want 10", cfg.Backfill)
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("Interval = %v, want 30s", cfg.Interval)
	}
	if cfg.DataDir != "/data" {
		t.Errorf("DataDir = %q, want /data", cfg.DataDir)
	}
	if cfg.SessionPath() != "/data/session.json" || cfg.StatePath() != "/data/state.json" {
		t.Errorf("пути: %q, %q", cfg.SessionPath(), cfg.StatePath())
	}
}

func TestLoadRequiresCredentials(t *testing.T) {
	t.Setenv("TG_API_ID", "")
	t.Setenv("TG_API_HASH", "")
	t.Setenv("TG_PHONE", "")

	_, err := Load()
	if err == nil {
		t.Fatal("ожидали ошибку")
	}
	for _, want := range []string{"TG_API_ID", "TG_API_HASH", "TG_PHONE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в ошибке нет %s: %v", want, err)
		}
	}
}

func TestValidateRunRequiresLLM(t *testing.T) {
	cfg := Config{Channel: "@some_group", Recipient: "@me"}

	err := cfg.ValidateRun()
	if err == nil {
		t.Fatal("ожидали ошибку")
	}
	for _, want := range []string{"LLM_BASE_URL", "LLM_MODEL"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в ошибке нет %s: %v", want, err)
		}
	}

	cfg.LLMBaseURL = "http://localhost:11434/v1"
	cfg.LLMModel = "qwen"
	if err := cfg.ValidateRun(); err != nil {
		t.Errorf("заполненный конфиг не прошёл проверку: %v", err)
	}
}
