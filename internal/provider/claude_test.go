package provider

import (
	"slices"
	"testing"
)

func TestBuildClaudeArgsKeepsModelOverrideWhenResuming(t *testing.T) {
	args := buildClaudeArgs(RunOptions{
		NativeSessionID: "native-1",
		Model:           "sonnet",
		Effort:          "high",
	}, "hello", nil)

	for _, want := range []string{"--resume", "native-1", "--model", "sonnet", "--effort", "high"} {
		if !slices.Contains(args, want) {
			t.Fatalf("args %q missing %q", args, want)
		}
	}
}
