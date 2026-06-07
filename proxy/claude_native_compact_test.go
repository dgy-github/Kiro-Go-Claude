package proxy

import (
	"os"
	"testing"
)

func TestIsClaudeNativeCompactRequest(t *testing.T) {
	req := &ClaudeRequest{Messages: []ClaudeMessage{{
		Role:    "user",
		Content: "<command-name>/compact</command-name>\n<command-message>compact</command-message>",
	}}}

	if !isClaudeNativeCompactRequest(req) {
		t.Fatalf("expected /compact command to be detected")
	}
}

func TestIsClaudeNativeCompactRequestPlainTextIsFalse(t *testing.T) {
	req := &ClaudeRequest{Messages: []ClaudeMessage{{
		Role:    "user",
		Content: "please summarize this project",
	}}}

	if isClaudeNativeCompactRequest(req) {
		t.Fatalf("plain text should not be treated as a native compact command")
	}
}

func TestCompactLogSnippet(t *testing.T) {
	got := compactLogSnippet("  abcdef  ", 3)
	if got != "abc..." {
		t.Fatalf("unexpected snippet %q", got)
	}
}

func TestWithEnvOverrideReplacesCaseInsensitive(t *testing.T) {
	env := []string{"Path=C:\\bin", "claude_code_max_output_tokens=20000", "OTHER=x"}
	got := withEnvOverride(env, "CLAUDE_CODE_MAX_OUTPUT_TOKENS", "64000")

	count := 0
	for _, entry := range got {
		if entry == "CLAUDE_CODE_MAX_OUTPUT_TOKENS=64000" {
			count++
		}
		if entry == "claude_code_max_output_tokens=20000" {
			t.Fatalf("old value was not replaced: %v", got)
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one overridden env entry, got %d in %v", count, got)
	}
}

func TestPowerShellQuote(t *testing.T) {
	got := powerShellQuote("abc'def")
	if got != "'abc''def'" {
		t.Fatalf("unexpected PowerShell quote: %q", got)
	}
}

func TestTranscriptAppendHasCompactSuccessIgnoresOldMarkers(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "session-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	old := []byte(`{"subtype":"compact_boundary"}` + "\n")
	if _, err := f.Write(old); err != nil {
		t.Fatal(err)
	}
	offset := int64(len(old))
	if _, err := f.WriteString(`{"content":"Error during compaction: API Error: nope"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	ok, reason := transcriptAppendHasCompactSuccess(path, offset)
	if ok {
		t.Fatalf("expected appended failure to override old success marker")
	}
	if reason == "" {
		t.Fatalf("expected failure reason")
	}
}

func TestTranscriptAppendHasCompactSuccessAcceptsNewMarker(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "session-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	if _, err := f.WriteString(`{"content":"old"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	offset, err := f.Seek(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"subtype":"compact_boundary"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	ok, reason := transcriptAppendHasCompactSuccess(path, offset)
	if !ok || reason != "" {
		t.Fatalf("expected appended success marker, ok=%v reason=%q", ok, reason)
	}
}
