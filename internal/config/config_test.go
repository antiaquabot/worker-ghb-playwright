package config

import (
	"os"
	"strings"
	"testing"
)

func TestInitConfigMessage_NoHTTPTypo(t *testing.T) {
	src, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatalf("read config.go: %v", err)
	}
	if strings.Contains(string(src), "worker-ghb-http") {
		t.Error("config.go still contains 'worker-ghb-http' — update to 'worker-ghb-playwright'")
	}
}
