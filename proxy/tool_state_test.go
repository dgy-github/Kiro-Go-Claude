package proxy

import "testing"

func TestToolStateRequiresNamedToolChoice(t *testing.T) {
	payload := testToolStatePayload("read", "execCommand")
	payload.ToolContract = &ToolContract{
		RequiresTool:   true,
		ToolName:       "read",
		Source:         "tool_choice",
		Mode:           toolContractModeRequireUpstreamTool,
		AvailableTools: []string{"read", "execCommand"},
		MaxToolUses:    1,
	}

	state := newToolStateMachine(payload)
	decision := state.Finalize("", []KiroToolUse{{
		ToolUseID: "toolu_wrong",
		Name:      "execCommand",
		Input:     map[string]interface{}{"cmd": "dir"},
	}})

	if decision.OK {
		t.Fatalf("expected wrong tool name to violate tool state")
	}
	if decision.Reason != "wrong_tool_name" {
		t.Fatalf("expected wrong_tool_name, got %q", decision.Reason)
	}
	if !decision.Repair {
		t.Fatalf("expected first violation to be repairable")
	}
}

func TestToolStateAllowsMultipleToolUsesByDefault(t *testing.T) {
	payload := testToolStatePayload("read", "execCommand")
	payload.ToolContract = &ToolContract{
		RequiresTool:   true,
		Source:         "delegated-execution",
		Mode:           toolContractModeRequireUpstreamTool,
		AvailableTools: []string{"read", "execCommand"},
	}

	decision := newToolStateMachine(payload).Finalize("", []KiroToolUse{
		{ToolUseID: "toolu_1", Name: "read", Input: map[string]interface{}{"path": "a.go"}},
		{ToolUseID: "toolu_2", Name: "execCommand", Input: map[string]interface{}{"cmd": "go test ./..."}},
	})

	if !decision.OK {
		t.Fatalf("expected multiple tool uses to be allowed by default, got %#v", decision)
	}
}

func TestToolStateHonorsMaxToolUses(t *testing.T) {
	payload := testToolStatePayload("read", "execCommand")
	payload.ToolContract = &ToolContract{
		RequiresTool:   true,
		Source:         "tool_choice",
		Mode:           toolContractModeRequireUpstreamTool,
		AvailableTools: []string{"read", "execCommand"},
		MaxToolUses:    1,
	}

	decision := newToolStateMachine(payload).Finalize("", []KiroToolUse{
		{ToolUseID: "toolu_1", Name: "read", Input: map[string]interface{}{"path": "a.go"}},
		{ToolUseID: "toolu_2", Name: "read", Input: map[string]interface{}{"path": "b.go"}},
	})

	if decision.OK {
		t.Fatalf("expected too many tool uses to violate max")
	}
	if decision.Reason != "too_many_tool_uses" {
		t.Fatalf("expected too_many_tool_uses, got %q", decision.Reason)
	}
}

func TestToolResultMatchStateRequiresExactPairing(t *testing.T) {
	history := []KiroHistoryMessage{{
		AssistantResponseMessage: &KiroAssistantResponseMessage{
			ToolUses: []KiroToolUse{
				{ToolUseID: "toolu_1", Name: "read"},
				{ToolUseID: "toolu_2", Name: "read"},
			},
		},
	}}

	missing := computeToolResultMatchState(history, []KiroToolResult{{ToolUseID: "toolu_1"}})
	if missing.Matched {
		t.Fatalf("expected missing tool result to fail exact matching")
	}
	if len(missing.MissingResultIDs) != 1 || missing.MissingResultIDs[0] != "toolu_2" {
		t.Fatalf("expected toolu_2 missing, got %#v", missing.MissingResultIDs)
	}

	orphan := computeToolResultMatchState(history, []KiroToolResult{
		{ToolUseID: "toolu_1"},
		{ToolUseID: "toolu_2"},
		{ToolUseID: "toolu_extra"},
	})
	if orphan.Matched {
		t.Fatalf("expected orphan tool result to fail exact matching")
	}
	if len(orphan.OrphanResultIDs) != 1 || orphan.OrphanResultIDs[0] != "toolu_extra" {
		t.Fatalf("expected toolu_extra orphan, got %#v", orphan.OrphanResultIDs)
	}

	matched := computeToolResultMatchState(history, []KiroToolResult{
		{ToolUseID: "toolu_2"},
		{ToolUseID: "toolu_1"},
	})
	if !matched.Matched {
		t.Fatalf("expected exact set match, got %#v", matched)
	}
}

func TestCurrentToolResultsMatchLastAssistantRejectsExtraResults(t *testing.T) {
	history := []KiroHistoryMessage{{
		AssistantResponseMessage: &KiroAssistantResponseMessage{
			ToolUses: []KiroToolUse{{ToolUseID: "toolu_1", Name: "read"}},
		},
	}}

	if currentToolResultsMatchLastAssistant(history, map[string]bool{"toolu_1": true, "toolu_extra": true}) {
		t.Fatalf("expected extra current tool result id to be rejected")
	}
	if !currentToolResultsMatchLastAssistant(history, map[string]bool{"toolu_1": true}) {
		t.Fatalf("expected exact current tool result id to match")
	}
}

func testToolStatePayload(toolNames ...string) *KiroPayload {
	payload := &KiroPayload{}
	payload.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext = &UserInputMessageContext{}
	for _, name := range toolNames {
		wrapper := KiroToolWrapper{}
		wrapper.ToolSpecification.Name = name
		wrapper.ToolSpecification.Description = "test tool"
		wrapper.ToolSpecification.InputSchema = InputSchema{JSON: map[string]interface{}{"type": "object"}}
		payload.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools = append(
			payload.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools,
			wrapper,
		)
	}
	return payload
}
