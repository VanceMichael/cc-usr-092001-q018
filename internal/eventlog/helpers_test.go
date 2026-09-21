package eventlog

import (
	"encoding/json"
	"os"
)

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func unmarshal(raw []byte, dst any) error {
	return json.Unmarshal(raw, dst)
}
