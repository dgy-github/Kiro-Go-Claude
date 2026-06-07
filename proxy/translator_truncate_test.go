package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestClaudeToKiroTruncatesOversizedHistory builds a conversation whose history
// far exceeds the upstream input limit and verifies the converted payload is
// trimmed below maxPayloadBytes, that a truncation placeholder is inserted, and
// that the current message is preserved.
func TestClaudeToKiroTruncatesOversizedHistory(t *testing.T) {
	// ~2KB chunk repeated across many turns to blow past the byte limit.
	big := strings.Repeat("lorem ipsum dolor sit amet ", 80) // ~2.1KB

	msgs := []ClaudeMessage{
		{Role: "user", Content: "start the long task"},
	}
	for i := 0; i < 800; i++ {
		msgs = append(msgs,
			ClaudeMessage{Role: "assistant", Content: "step result: " + big},
			ClaudeMessage{Role: "user", Content: "next: " + big},
		)
	}
	msgs = append(msgs, ClaudeMessage{Role: "user", Content: "FINAL: summarize everything above"})

	req := &ClaudeRequest{
		Model:    "claude-opus-4.8",
		System:   "You are a helpful assistant.",
		Messages: msgs,
	}

	payload := ClaudeToKiro(req, false)

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if len(raw) > maxPayloadBytes {
		t.Fatalf("payload size %d exceeds limit %d after truncation", len(raw), maxPayloadBytes)
	}

	// The current message must be preserved.
	cur := payload.ConversationState.CurrentMessage.UserInputMessage
	if !strings.Contains(cur.Content, "FINAL: summarize everything above") {
		t.Fatalf("current message lost after truncation, got %q", cur.Content[:min(80, len(cur.Content))])
	}

	// A deterministic compaction summary must be present in history.
	foundSummary := false
	for _, h := range payload.ConversationState.History {
		if h.UserInputMessage != nil && strings.Contains(h.UserInputMessage.Content, "compacted by Kiro-Go") {
			foundSummary = true
			break
		}
	}
	if !foundSummary {
		t.Fatalf("expected a compaction summary in history")
	}

	// System priming should still be at the front.
	if len(payload.ConversationState.History) < 2 {
		t.Fatalf("expected priming retained, history too short")
	}
	primingUser := payload.ConversationState.History[0].UserInputMessage
	if primingUser == nil || !strings.Contains(primingUser.Content, "helpful assistant") {
		t.Fatalf("expected system priming retained at front")
	}
}

func TestClaudeToKiroCompactionSummaryRetainsDroppedContext(t *testing.T) {
	big := strings.Repeat("padding ", 100)
	msgs := []ClaudeMessage{
		{Role: "user", Content: "EARLY_GOAL: build MCP GUI manager"},
		{Role: "assistant", Content: "EARLY_DECISION: keep edits reversible"},
	}
	for i := 0; i < 900; i++ {
		msgs = append(msgs,
			ClaudeMessage{Role: "user", Content: "middle user " + big},
			ClaudeMessage{Role: "assistant", Content: "middle assistant " + big},
		)
	}
	msgs = append(msgs, ClaudeMessage{Role: "user", Content: "FINAL: continue"})

	payload := ClaudeToKiro(&ClaudeRequest{
		Model:    "claude-opus-4.8",
		Messages: msgs,
	}, false)

	var summary string
	for _, h := range payload.ConversationState.History {
		if h.UserInputMessage != nil && strings.Contains(h.UserInputMessage.Content, "compacted by Kiro-Go") {
			summary = h.UserInputMessage.Content
			break
		}
	}
	if summary == "" {
		t.Fatalf("expected compaction summary")
	}
	if !strings.Contains(summary, "EARLY_GOAL: build MCP GUI manager") {
		t.Fatalf("expected summary to retain early user goal, got:\n%s", summary)
	}
	if !strings.Contains(summary, "EARLY_DECISION: keep edits reversible") {
		t.Fatalf("expected summary to retain early assistant decision, got:\n%s", summary)
	}
	if len(summary) > compactionSummaryMaxChars+len("\n[Compaction summary truncated.]") {
		t.Fatalf("summary exceeded cap: %d", len(summary))
	}
}

func TestClaudeToKiroNativeCompactUsesRequestOnlyTruncationSummary(t *testing.T) {
	big := strings.Repeat("padding ", 100)
	msgs := []ClaudeMessage{
		{Role: "user", Content: "EARLY_GOAL: build MCP GUI manager"},
		{Role: "assistant", Content: "EARLY_DECISION: keep edits reversible"},
	}
	for i := 0; i < 900; i++ {
		msgs = append(msgs,
			ClaudeMessage{Role: "user", Content: "middle user " + big},
			ClaudeMessage{Role: "assistant", Content: "middle assistant " + big},
		)
	}
	msgs = append(msgs, ClaudeMessage{Role: "user", Content: "FINAL: continue"})

	payload := ClaudeToKiroWithTruncation(&ClaudeRequest{
		Model:    "claude-opus-4.8",
		Messages: msgs,
	}, false, payloadTruncationOptions{
		RequestOnlySummary: true,
		Reason:             "native compact in flight",
	})

	var summary string
	for _, h := range payload.ConversationState.History {
		if h.UserInputMessage != nil && strings.Contains(h.UserInputMessage.Content, "request-size guard") {
			summary = h.UserInputMessage.Content
			break
		}
	}
	if summary == "" {
		t.Fatalf("expected request-only truncation summary")
	}
	if !strings.Contains(summary, "request-only truncation") {
		t.Fatalf("summary should explicitly say it is request-only, got:\n%s", summary)
	}
	if !strings.Contains(summary, "Claude Code /compact") {
		t.Fatalf("summary should name Claude Code /compact as transcript owner, got:\n%s", summary)
	}
	if strings.Contains(summary, "EARLY_GOAL") || strings.Contains(summary, "Important retained snippets") {
		t.Fatalf("request-only summary should not add a second rich transcript summary, got:\n%s", summary)
	}
}

func TestTruncatePayloadPreservesActiveToolTurnWhenTailStartsWithAssistant(t *testing.T) {
	payload := &KiroPayload{}
	payload.ConversationState.ChatTriggerType = "MANUAL"
	payload.ConversationState.ConversationID = "test"
	payload.ConversationState.History = []KiroHistoryMessage{{
		AssistantResponseMessage: &KiroAssistantResponseMessage{
			ToolUses: []KiroToolUse{{
				ToolUseID: "toolu_active",
				Name:      "read_file",
				Input:     map[string]interface{}{"path": "D:\\agent_prac\\README.md"},
			}},
		},
	}}
	payload.ConversationState.CurrentMessage.UserInputMessage = KiroUserInputMessage{
		Content: strings.Repeat("current oversized content ", 40000),
		ModelID: "claude-opus-4.8",
		Origin:  "AI_EDITOR",
		UserInputMessageContext: &UserInputMessageContext{
			ToolResults: []KiroToolResult{{
				ToolUseID: "toolu_active",
				Content:   []KiroResultContent{{Text: "file contents"}},
				Status:    "success",
			}},
		},
	}

	truncatePayloadToLimit(payload, false)

	history := payload.ConversationState.History
	if len(history) != 1 {
		t.Fatalf("expected active assistant tool turn to survive, history len=%d", len(history))
	}
	active := history[0].AssistantResponseMessage
	if active == nil || len(active.ToolUses) != 1 || active.ToolUses[0].ToolUseID != "toolu_active" {
		t.Fatalf("active tool_use was not preserved: %#v", history)
	}
	ctx := payload.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext
	if ctx == nil || len(ctx.ToolResults) != 1 || ctx.ToolResults[0].ToolUseID != "toolu_active" {
		t.Fatalf("active tool_result was not preserved: %#v", ctx)
	}
}

// TestClaudeToKiroSmallPayloadNotTruncated ensures normal-sized conversations
// are left untouched (no placeholder inserted).
func TestClaudeToKiroSmallPayloadNotTruncated(t *testing.T) {
	req := &ClaudeRequest{
		Model:  "claude-opus-4.8",
		System: "You are helpful.",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
			{Role: "user", Content: "how are you?"},
		},
	}
	payload := ClaudeToKiro(req, false)
	for _, h := range payload.ConversationState.History {
		if h.UserInputMessage != nil && strings.Contains(h.UserInputMessage.Content, "truncated to fit") {
			t.Fatalf("small payload should not be truncated")
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
