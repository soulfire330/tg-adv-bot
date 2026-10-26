package judge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const validVerdict = `{"matches":true,"reason":"подходит"}`

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client := New(Config{
		BaseURL: server.URL,
		Key:     "test-key",
		Model:   "test-model",
		Prompt:  "критерий",
		HTTP:    server.Client(),
	})
	client.backoff = time.Millisecond
	return client
}

func verdictResponse(content string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": content}},
			},
		})
	}
}

func TestEvaluateSendsStrictJSONSchema(t *testing.T) {
	var (
		captured request
		path     string
		auth     string
		agent    string
	)
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		auth = r.Header.Get("Authorization")
		agent = r.Header.Get("User-Agent")
		_ = json.NewDecoder(r.Body).Decode(&captured)
		verdictResponse(validVerdict)(w, r)
	})

	verdict, err := client.Evaluate(t.Context(), "Сдам квартиру")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !verdict.Matches || verdict.Reason != "подходит" {
		t.Errorf("вердикт = %+v", verdict)
	}

	if path != "/chat/completions" {
		t.Errorf("путь = %q", path)
	}
	if auth != "Bearer test-key" {
		t.Errorf("Authorization = %q", auth)
	}
	if agent != userAgent {
		t.Errorf("User-Agent = %q, want %q", agent, userAgent)
	}
	if captured.Model != "test-model" {
		t.Errorf("model = %q", captured.Model)
	}
	if captured.Temperature != 0 {
		t.Errorf("temperature = %v", captured.Temperature)
	}
	if len(captured.Messages) != 2 {
		t.Fatalf("сообщений = %d, want 2", len(captured.Messages))
	}
	if captured.Messages[0].Role != "system" || captured.Messages[0].Content != "критерий" {
		t.Errorf("system = %+v", captured.Messages[0])
	}
	if captured.Messages[1].Content != "Сдам квартиру" {
		t.Errorf("user = %q", captured.Messages[1].Content)
	}
	if captured.ResponseFormat.Type != "json_schema" {
		t.Errorf("response_format.type = %q", captured.ResponseFormat.Type)
	}
	if !captured.ResponseFormat.JSONSchema.Strict {
		t.Error("json_schema.strict должен быть true")
	}
	schema := string(captured.ResponseFormat.JSONSchema.Schema)
	for _, want := range []string{`"matches"`, `"reason"`, `"additionalProperties":false`} {
		if !strings.Contains(schema, want) {
			t.Errorf("в схеме нет %s: %s", want, schema)
		}
	}
}

func TestEvaluateNoMatch(t *testing.T) {
	client := newTestClient(t, verdictResponse(`{"matches":false,"reason":"это продажа"}`))

	verdict, err := client.Evaluate(t.Context(), "Продам диван")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if verdict.Matches {
		t.Error("ожидали matches=false")
	}
}

func TestEvaluateRetriesServerError(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("boom"))
			return
		}
		verdictResponse(validVerdict)(w, r)
	})

	if _, err := client.Evaluate(t.Context(), "текст"); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("запросов = %d, want 2", got)
	}
}

func TestEvaluateDoesNotRetryBadRequest(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"response_format unsupported"}}`))
	})

	if _, err := client.Evaluate(t.Context(), "текст"); err == nil {
		t.Fatal("ожидали ошибку")
	} else if !strings.Contains(err.Error(), "response_format unsupported") {
		t.Errorf("ошибка без тела ответа: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("запросов = %d, want 1", got)
	}
}

func TestEvaluateRejectsBrokenContent(t *testing.T) {
	client := newTestClient(t, verdictResponse("это не json"))

	if _, err := client.Evaluate(t.Context(), "текст"); err == nil {
		t.Fatal("ожидали ошибку разбора")
	}
}

func TestEvaluateTruncatesLongText(t *testing.T) {
	var captured request
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		verdictResponse(validVerdict)(w, r)
	})

	if _, err := client.Evaluate(t.Context(), strings.Repeat("я", maxTextRunes+500)); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got := len([]rune(captured.Messages[1].Content)); got != maxTextRunes {
		t.Errorf("длина текста = %d, want %d", got, maxTextRunes)
	}
}

func TestProbe(t *testing.T) {
	ok := newTestClient(t, verdictResponse(validVerdict))
	if err := ok.Probe(t.Context()); err != nil {
		t.Fatalf("Probe: %v", err)
	}

	broken := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("no json_schema"))
	})
	if err := broken.Probe(t.Context()); err == nil {
		t.Fatal("ожидали ошибку probe")
	}
}

func TestSendsConfiguredHeaders(t *testing.T) {
	var session, agent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session = r.Header.Get("x-opencode-session")
		agent = r.Header.Get("User-Agent")
		verdictResponse(validVerdict)(w, r)
	}))
	t.Cleanup(server.Close)

	client := New(Config{
		BaseURL: server.URL,
		Key:     "test-key",
		Model:   "test-model",
		Prompt:  "критерий",
		Headers: map[string]string{
			"x-opencode-session": "86a09657-3fbe-4448-abf8-8cae0cea360c",
			"User-Agent":         "custom/2",
		},
		HTTP: server.Client(),
	})

	if _, err := client.Evaluate(t.Context(), "текст"); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if session != "86a09657-3fbe-4448-abf8-8cae0cea360c" {
		t.Errorf("x-opencode-session = %q", session)
	}
	if agent != "custom/2" {
		t.Errorf("User-Agent = %q, want custom/2", agent)
	}
}
