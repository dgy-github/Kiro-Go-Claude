package proxy

import (
	"fmt"
	"strings"
	"testing"
)

// TestNoToolInvocationTextInAssistantHistory is a regression guard for the
// few-shot pollution bug: when historical tool calls were narrated as
// "[Called tool X with input ...]" inside assistant turns, the model learned to
// emit that literal text instead of issuing real structured tool calls.
//
// After the fix, assistant history turns must never contain tool-invocation
// syntax. Tool identity is attributed only on the user "Tool results" side.
func TestNoToolInvocationTextInAssistantHistory(t *testing.T) {
	// Build a long OpenAI conversation with many completed tool cycles.
	msgs := []OpenAIMessage{{Role: "user", Content: "start a multi-step task"}}
	for i := 0; i < 8; i++ {
		msgs = append(msgs,
			OpenAIMessage{Role: "assistant", Content: "", ToolCalls: []ToolCall{
				newPollToolCall(fmt.Sprintf("call_%d", i), "exec_command", fmt.Sprintf(`{"cmd":"step %d"}`, i)),
			}},
			OpenAIMessage{Role: "tool", ToolCallID: fmt.Sprintf("call_%d", i), Content: fmt.Sprintf("OUTPUT_%d", i)},
			OpenAIMessage{Role: "user", Content: fmt.Sprintf("continue %d", i)},
		)
	}
	msgs = append(msgs, OpenAIMessage{Role: "user", Content: "summarize"})

	payload := OpenAIToKiro(&OpenAIRequest{Model: "claude-opus-4.8", Messages: msgs}, false)

	for i, h := range payload.ConversationState.History {
		a := h.AssistantResponseMessage
		if a == nil {
			continue
		}
		// No assistant turn may contain tool-invocation-looking text.
		for _, bad := range []string{"[Called tool", "Called tool ", "with input {"} {
			if strings.Contains(a.Content, bad) {
				t.Fatalf("history[%d] assistant content contains mimicable tool text %q: %q", i, bad, a.Content)
			}
		}
		// No assistant turn may carry structured tool calls (rejected upstream).
		if len(a.ToolUses) > 0 {
			t.Fatalf("history[%d] assistant retains %d structured toolUses", i, len(a.ToolUses))
		}
	}

	// Tool outputs must still be preserved (on the user side) for context.
	var allText strings.Builder
	for _, h := range payload.ConversationState.History {
		if h.UserInputMessage != nil {
			allText.WriteString(h.UserInputMessage.Content)
			allText.WriteString("\n")
		}
	}
	combined := allText.String()
	for i := 0; i < 8; i++ {
		marker := fmt.Sprintf("OUTPUT_%d", i)
		if !strings.Contains(combined, marker) {
			t.Fatalf("tool output %q lost from history", marker)
		}
	}
	// Tool identity should be attributed on the user side.
	if !strings.Contains(combined, "[exec_command]") {
		t.Fatalf("expected tool results attributed to exec_command on the user side")
	}
}

func newPollToolCall(id, name, args string) ToolCall {
	tc := ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}

// TestCollapsesConsecutiveIdenticalToolResults covers a client retry loop that
// sends the same failing tool result many times. After hollow assistant turns
// are dropped, those identical user "Tool results" turns become adjacent
// duplicates; the proxy collapses each run to a single copy.
func TestCollapsesConsecutiveIdenticalToolResults(t *testing.T) {
	msgs := []OpenAIMessage{{Role: "user", Content: "start"}}
	// 5 identical failing cycles in a row (model retrying the same tool).
	for i := 0; i < 5; i++ {
		msgs = append(msgs,
			OpenAIMessage{Role: "assistant", Content: "", ToolCalls: []ToolCall{
				newPollToolCall(fmt.Sprintf("c%d", i), "exec_command", `{"cmd":"x"}`),
			}},
			OpenAIMessage{Role: "tool", ToolCallID: fmt.Sprintf("c%d", i), Content: "SAME_ERROR_OUTPUT"},
		)
	}
	msgs = append(msgs, OpenAIMessage{Role: "user", Content: "final"})

	payload := OpenAIToKiro(&OpenAIRequest{Model: "claude-opus-4.8", Messages: msgs}, false)

	count := 0
	for _, h := range payload.ConversationState.History {
		if h.UserInputMessage != nil && strings.Contains(h.UserInputMessage.Content, "SAME_ERROR_OUTPUT") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 5 identical tool-result turns collapsed to 1, got %d", count)
	}
}

// TestDropsDotPollutedAssistantTurns covers the second-order pollution: after
// stripping "[Called tool ...]" from assistant turns that held only that text,
// the turns become empty and must NOT be backfilled with ".". A history full of
// "." assistant turns trains the model to reply ".". Such hollow turns are
// dropped instead.
func TestDropsDotPollutedAssistantTurns(t *testing.T) {
	msgs := []ClaudeMessage{{Role: "user", Content: "start"}}
	for i := 0; i < 6; i++ {
		// Assistant turn that is pure replayed tool-call text (becomes empty after scrub).
		msgs = append(msgs,
			ClaudeMessage{Role: "assistant", Content: "[Called tool exec_command with input {\"cmd\":\"x\"}]"},
			ClaudeMessage{Role: "user", Content: "continue"},
		)
		// Also a turn that is already a bare "." (client-replayed prior placeholder).
		msgs = append(msgs,
			ClaudeMessage{Role: "assistant", Content: "."},
			ClaudeMessage{Role: "user", Content: "go on"},
		)
	}
	msgs = append(msgs, ClaudeMessage{Role: "user", Content: "final question"})

	payload := ClaudeToKiro(&ClaudeRequest{Model: "claude-opus-4.8", Messages: msgs}, false)

	for i, h := range payload.ConversationState.History {
		a := h.AssistantResponseMessage
		if a == nil {
			continue
		}
		c := strings.TrimSpace(a.Content)
		if c == "." || c == "" {
			t.Fatalf("history[%d] is a hollow/dot assistant turn that should have been dropped", i)
		}
		if strings.Contains(a.Content, "[Called tool") {
			t.Fatalf("history[%d] still contains replayed tool-call text", i)
		}
	}
}

// TestDropsAssistantAuthoredToolResultsTurns covers the failure mode where the
// upstream model starts authoring Claude Code's rendered tool transcript as
// plain assistant text ("Tool results:\n\n[Read] ...") instead of emitting real
// structured tool_use blocks. Replaying that text poisons the next request, so
// Kiro-Go must drop it from assistant history.
func TestDropsAssistantAuthoredToolResultsTurns(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "continue"},
			{Role: "assistant", Content: "Tool results:\n\n[Read] 750    def _init_loop(self) -> None:\n\nContinue reading the full implementation."},
			{Role: "user", Content: "确定"},
		},
	}

	payload := ClaudeToKiro(req, false)

	for i, h := range payload.ConversationState.History {
		if a := h.AssistantResponseMessage; a != nil {
			if strings.Contains(a.Content, "Tool results:") || strings.Contains(a.Content, "[Read]") {
				t.Fatalf("history[%d] still contains assistant-authored fake tool results: %q", i, a.Content)
			}
		}
	}
}

// TestScrubsClientReplayedToolCallText covers the recovery path: a polluted
// client stored the model's "[Called tool ...]" text output as assistant
// history and replays it. The proxy must strip that text from assistant turns
// so the pattern is not reinforced.
func TestScrubsClientReplayedToolCallText(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "do the task"},
			// Assistant text the client captured from the model's polluted output.
			{Role: "assistant", Content: "Let me check.\n\n[Called tool exec_command with input {\"cmd\":\"pwd\"}]"},
			{Role: "user", Content: "continue"},
			{Role: "assistant", Content: "[Called tool exec_command with input {\"cmd\":\"ls\"}]"},
			{Role: "user", Content: "continue"},
		},
	}

	payload := ClaudeToKiro(req, false)

	for i, h := range payload.ConversationState.History {
		if a := h.AssistantResponseMessage; a != nil {
			if strings.Contains(a.Content, "[Called tool") {
				t.Fatalf("history[%d] still contains replayed tool-call text: %q", i, a.Content)
			}
		}
	}

	// The natural prose around the stripped marker must be preserved.
	var combined strings.Builder
	for _, h := range payload.ConversationState.History {
		if h.AssistantResponseMessage != nil {
			combined.WriteString(h.AssistantResponseMessage.Content)
			combined.WriteString("\n")
		}
	}
	if !strings.Contains(combined.String(), "Let me check.") {
		t.Fatalf("expected surrounding assistant prose to survive scrubbing, got:\n%s", combined.String())
	}
}

// TestStripsVisibleInjectionDefenseNarration covers another visible pollution
// pattern: the upstream model sometimes explains that it is ignoring Claude
// Agent SDK / c.entrypoint injected text and declares "I am Kiro". That is
// gateway/system-defense narration, not user-facing answer content.
func TestStripsVisibleInjectionDefenseNarration(t *testing.T) {
	input := "开头那些 Claude Agent SDK / c.entrypoint 注入文本照旧不管——我是 Kiro。\nindex.html 已确认有两个新页的卡片。\n"

	got := stripVisibleAssistantPollutionText(input)
	if strings.Contains(got, "Claude Agent SDK") || strings.Contains(got, "我是 Kiro") {
		t.Fatalf("visible injection narration was not stripped: %q", got)
	}
	if !strings.Contains(got, "index.html 已确认") {
		t.Fatalf("normal answer content was lost: %q", got)
	}

	claudeResp := KiroToClaudeResponse(input, "", false, nil, 10, 10, "claude-sonnet-4.5")
	if len(claudeResp.Content) != 1 || strings.Contains(claudeResp.Content[0].Text, "c.entrypoint") {
		t.Fatalf("Claude response still contains visible injection narration: %#v", claudeResp.Content)
	}

	openAIResp := KiroToOpenAIResponseWithReasoning(input, "", nil, 10, 10, "claude-sonnet-4.5", "reasoning_content")
	choices := openAIResp["choices"].([]map[string]interface{})
	message := choices[0]["message"].(map[string]interface{})
	if content, _ := message["content"].(string); strings.Contains(content, "c.entrypoint") {
		t.Fatalf("OpenAI response still contains visible injection narration: %q", content)
	}
}

func TestVisibleInjectionDefenseNarrationStreamFilterHandlesSplitChunks(t *testing.T) {
	var filter visibleAssistantPollutionStreamFilter
	chunks := []string{
		"开头那些 Claude Agent SDK / c.entry",
		"point 注入文本照旧不管——我是 Kiro。\n",
		"index.html 已确认有两个新页的卡片。\n",
	}

	var out strings.Builder
	for _, chunk := range chunks {
		out.WriteString(filter.Filter(chunk, false))
	}
	out.WriteString(filter.Filter("", true))

	got := out.String()
	if strings.Contains(got, "Claude Agent SDK") || strings.Contains(got, "我是 Kiro") {
		t.Fatalf("split stream pollution leaked: %q", got)
	}
	if !strings.Contains(got, "index.html 已确认") {
		t.Fatalf("normal streamed content was lost: %q", got)
	}
}
