// Package filter — дешёвый префильтр по вхождениям подстрок.
package filter

import (
	"bufio"
	"os"
	"strings"
)

// Load читает кейворды: одна подстрока в строке, пустые строки и #-комментарии
// игнорируются. Регистр снимается сразу, чтобы Match был дешёвым.
func Load(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var keywords []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keywords = append(keywords, strings.ToLower(line))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return keywords, nil
}

// Match сообщает, есть ли в тексте хоть один кейворд.
func Match(keywords []string, text string) bool {
	text = strings.ToLower(text)
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}
