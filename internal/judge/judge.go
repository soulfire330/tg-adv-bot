// Package judge — вызов OpenAI-совместимого API со strict json_schema.
package judge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	attempts       = 3
	backoffBase    = time.Second
	maxTextRunes   = 4000
	maxResponseRaw = 2048
)

// Verdict — единственный источник решения о совпадении.
type Verdict struct {
	Matches bool   `json:"matches"`
	Reason  string `json:"reason"`
}

// Client держит настройки одного LLM-эндпоинта и промпт-критерий.
type Client struct {
	url     string
	key     string
	model   string
	prompt  string
	http    *http.Client
	backoff time.Duration
}

// New собирает клиент: baseURL — корень API без /chat/completions.
func New(baseURL, key, model, prompt string, hc *http.Client) *Client {
	return &Client{
		url:     strings.TrimSuffix(baseURL, "/") + "/chat/completions",
		key:     key,
		model:   model,
		prompt:  prompt,
		http:    hc,
		backoff: backoffBase,
	}
}

// Probe проверяет, что эндпоинт принимает наш response_format, ещё до первого поста.
func (c *Client) Probe(ctx context.Context) error {
	const system = "Отвечай строго по схеме."
	if _, err := c.ask(ctx, system, "проверка"); err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	return nil
}

// Evaluate получает вердикт по тексту поста.
func (c *Client) Evaluate(ctx context.Context, text string) (Verdict, error) {
	raw, err := c.ask(ctx, c.prompt, truncate(text, maxTextRunes))
	if err != nil {
		return Verdict{}, err
	}
	var v Verdict
	if err := json.Unmarshal(raw, &v); err != nil {
		return Verdict{}, fmt.Errorf("разбор вердикта: %w (%s)", err, short(raw))
	}
	return v, nil
}

func (c *Client) ask(ctx context.Context, system, user string) ([]byte, error) {
	body, err := json.Marshal(request{
		Model:       c.model,
		Temperature: 0,
		Messages: []message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		ResponseFormat: responseFormat{
			Type: "json_schema",
			JSONSchema: jsonSchema{
				Name:   "verdict",
				Strict: true,
				Schema: verdictSchema,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := range attempts {
		if attempt > 0 {
			if err := sleep(ctx, c.backoff<<(attempt-1)); err != nil {
				return nil, err
			}
		}
		content, retryable, err := c.once(ctx, body)
		if err == nil {
			return content, nil
		}
		lastErr = err
		if !retryable {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) once(ctx context.Context, body []byte) (content []byte, retryable bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.key)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf("запрос к LLM: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, true, fmt.Errorf("чтение ответа LLM: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return nil, retryable, fmt.Errorf("LLM ответил %d: %s", resp.StatusCode, short(raw))
	}

	var parsed response
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, false, fmt.Errorf("разбор ответа LLM: %w (%s)", err, short(raw))
	}
	if parsed.Error != nil {
		return nil, false, fmt.Errorf("LLM вернул ошибку: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return nil, false, errors.New("LLM вернул пустой ответ")
	}
	return []byte(parsed.Choices[0].Message.Content), false, nil
}

func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

func short(raw []byte) string {
	if len(raw) > maxResponseRaw {
		raw = raw[:maxResponseRaw]
	}
	return string(raw)
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type request struct {
	Model          string         `json:"model"`
	Temperature    float64        `json:"temperature"`
	Messages       []message      `json:"messages"`
	ResponseFormat responseFormat `json:"response_format"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type responseFormat struct {
	Type       string     `json:"type"`
	JSONSchema jsonSchema `json:"json_schema"`
}

type jsonSchema struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type response struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

var verdictSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "matches": {"type": "boolean", "description": "Пост соответствует критерию"},
    "reason": {"type": "string", "description": "Короткое объяснение на русском, до 200 символов"}
  },
  "required": ["matches", "reason"],
  "additionalProperties": false
}`)
