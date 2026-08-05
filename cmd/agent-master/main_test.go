package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/Ken-Chy129/agent-master/internal/config"
)

func TestWriteConnectInfoIncludesEmbeddedWebURL(t *testing.T) {
	var out bytes.Buffer
	cfg := &config.Config{Host: "0.0.0.0", Port: 18888, Token: "test-token"}

	writeConnectInfo(&out, cfg)

	text := out.String()
	if !strings.Contains(text, "Web 界面  http://127.0.0.1:18888") {
		t.Fatalf("missing Web UI URL:\n%s", text)
	}
	if !strings.Contains(text, "令牌    test-token") {
		t.Fatalf("missing pairing token:\n%s", text)
	}
}

// Help text is read in a terminal, where CJK glyphs are double-width. A line
// that overflows wraps mid-token — `agent-master help --all` once broke into
// "help -" / "-all", which is both unreadable and un-copyable.
func TestHelpLinesFitATerminal(t *testing.T) {
	const maxColumns = 72

	for _, tc := range []struct {
		name  string
		write func(io.Writer)
	}{
		{"usage", writeUsage},
		{"usageAll", writeUsageAll},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			tc.write(&out)

			text := out.String()
			if text == "" {
				t.Fatal("help output is empty")
			}
			for i, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
				if w := displayWidth(line); w > maxColumns {
					t.Errorf("line %d is %d columns wide (max %d):\n%s", i+1, w, maxColumns, line)
				}
			}
		})
	}
}

// Every command the dispatcher accepts should be discoverable from `help --all`,
// or it exists only for whoever reads the source.
func TestUsageAllListsEveryCommand(t *testing.T) {
	var out bytes.Buffer
	writeUsageAll(&out)
	text := out.String()

	for _, cmd := range []string{
		"start", "stop", "restart", "status", "doctor",
		"uninstall", "pair", "token", "serve", "version",
	} {
		if !strings.Contains(text, cmd) {
			t.Errorf("help --all does not mention %q", cmd)
		}
	}
}
