package proxy

import (
	"encoding/json"
	"testing"
)

func TestProtocolBoundariesPreserveActiveToolTurn(t *testing.T) {
	cases := []struct {
		name    string
		payload *KiroPayload
	}{
		{name: "claude-messages", payload: ClaudeToKiro(&ClaudeRequest{
			Model: "claude-sonnet-4.5",
			Tools: []ClaudeTool{{
				Name:        "read_file",
				Description: "read a local file",
				InputSchema: map[string]interface{}{"type": "object"},
			}},
			Messages: []ClaudeMessage{
				{Role: "user", Content: "read the handoff"},
				{Role: "assistant", Content: []interface{}{
					map[string]interface{}{"type": "tool_use", "id": "toolu_shared", "name": "read_file", "input": map[string]interface{}{"path": "HANDOFF.md"}},
				}},
				{Role: "user", Content: []interface{}{
					map[string]interface{}{"type": "tool_result", "tool_use_id": "toolu_shared", "content": "handoff content"},
				}},
			},
		}, false)},
		{name: "openai-chat", payload: OpenAIToKiro(&OpenAIRequest{
			Model: "claude-sonnet-4.5",
			Tools: []OpenAITool{openAIToolForTest("read_file")},
			Messages: []OpenAIMessage{
				{Role: "user", Content: "read the handoff"},
				{Role: "assistant", ToolCalls: []ToolCall{toolCallForTest("toolu_shared", "read_file", `{"path":"HANDOFF.md"}`)}},
				{Role: "tool", ToolCallID: "toolu_shared", Content: "handoff content"},
			},
		}, false)},
		{name: "responses", payload: responsesActiveToolPayloadForTest(t)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertActiveToolPair(t, tc.payload, "toolu_shared")
		})
	}
}

func responsesActiveToolPayloadForTest(t *testing.T) *KiroPayload {
	t.Helper()
	msgs, err := parseResponsesInput(json.RawMessage(`[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"read the handoff"}]},
		{"type":"function_call","call_id":"toolu_shared","name":"read_file","arguments":{"path":"HANDOFF.md"}},
		{"type":"function_call_output","call_id":"toolu_shared","output":{"text":"handoff content"}}
	]`))
	if err != nil {
		t.Fatalf("parse responses input: %v", err)
	}
	return OpenAIToKiro(&OpenAIRequest{
		Model:    "claude-sonnet-4.5",
		Tools:    []OpenAITool{openAIToolForTest("read_file")},
		Messages: msgs,
	}, false)
}

func assertActiveToolPair(t *testing.T, payload *KiroPayload, toolUseID string) {
	t.Helper()
	history := payload.ConversationState.History
	if len(history) == 0 {
		t.Fatalf("expected history with active assistant tool_use")
	}
	last := history[len(history)-1].AssistantResponseMessage
	if last == nil || len(last.ToolUses) != 1 || last.ToolUses[0].ToolUseID != toolUseID {
		t.Fatalf("expected last history assistant tool_use %q, got %#v", toolUseID, history[len(history)-1])
	}
	ctx := payload.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext
	if ctx == nil || len(ctx.ToolResults) != 1 || ctx.ToolResults[0].ToolUseID != toolUseID {
		t.Fatalf("expected current tool_result %q, got %#v", toolUseID, ctx)
	}
}

func openAIToolForTest(name string) OpenAITool {
	raw := []byte(`{"type":"function","name":"` + name + `","description":"test tool","parameters":{"type":"object"}}`)
	var tool OpenAITool
	if err := json.Unmarshal(raw, &tool); err != nil {
		panic(err)
	}
	return tool
}

func toolCallForTest(id, name, args string) ToolCall {
	tc := ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}
