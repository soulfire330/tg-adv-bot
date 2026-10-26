package filter

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMatch(t *testing.T) {
	keywords := []string{"аренда", "сдам"}
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"точное вхождение", "Сдам квартиру", true},
		{"регистр не важен", "АРЕНДА студии", true},
		{"внутри слова", "пересдам", true},
		{"нет вхождений", "Продам диван", false},
		{"пустой текст", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Match(keywords, tt.text); got != tt.want {
				t.Errorf("Match(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keywords.txt")
	content := "# комментарий\n\n Аренда \nсдам\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	keywords, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"аренда", "сдам"}
	if !reflect.DeepEqual(keywords, want) {
		t.Errorf("Load() = %q, want %q", keywords, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "нет.txt")); err == nil {
		t.Fatal("ожидали ошибку для отсутствующего файла")
	}
}
