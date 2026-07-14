package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Ken-Chy129/agent-master/internal/config"
)

func TestWriteConnectInfoIncludesEmbeddedWebURL(t *testing.T) {
	var out bytes.Buffer
	cfg := &config.Config{Host: "0.0.0.0", Port: 18888, Token: "test-token"}

	writeConnectInfo(&out, cfg)

	text := out.String()
	if !strings.Contains(text, "Web UI  http://127.0.0.1:18888") {
		t.Fatalf("missing Web UI URL:\n%s", text)
	}
	if !strings.Contains(text, "Token   test-token") {
		t.Fatalf("missing pairing token:\n%s", text)
	}
}
