package proxy

import (
	"fmt"
	"sort"
	"strings"
)

func formatKiroPayloadDiagnostics(payload *KiroPayload, endpoint string, bodyLen int) string {
	if payload == nil {
		return fmt.Sprintf("endpoint=%s payload=<nil> bytes=%d", endpoint, bodyLen)
	}

	current := payload.ConversationState.CurrentMessage.UserInputMessage
	ctx := current.UserInputMessageContext
	toolCount := 0
	resultCount := 0
	var toolNames []string
	var schemaIssues []string
	var resultIDs []string

	if ctx != nil {
		toolCount = len(ctx.Tools)
		resultCount = len(ctx.ToolResults)
		for _, tool := range ctx.Tools {
			name := strings.TrimSpace(tool.ToolSpecification.Name)
			if name == "" {
				name = "<empty>"
			}
			toolNames = append(toolNames, name)
			schemaIssues = append(schemaIssues, inspectSchemaIssues(name, tool.ToolSpecification.InputSchema.JSON)...)
		}
		for _, result := range ctx.ToolResults {
			resultIDs = append(resultIDs, result.ToolUseID)
		}
	}

	historyStats := summarizeHistory(payload.ConversationState.History)
	pairing := inspectToolPairing(payload.ConversationState.History, resultIDs)
	pairingIssues := pairing.Issues
	if len(pairingIssues) > 0 {
		schemaIssues = append(schemaIssues, pairingIssues...)
	}
	if resultCount > 0 && toolCount == 0 {
		schemaIssues = append(schemaIssues, "currentToolResults:no-current-tools")
	}

	sort.Strings(toolNames)
	sort.Strings(schemaIssues)
	sort.Strings(resultIDs)

	return fmt.Sprintf(
		"endpoint=%s bytes=%d model=%q contentLen=%d images=%d history=%s lastHistoryRole=%s lastAssistantToolUseIDs=%s currentTools=%d toolNames=%s currentToolResults=%d currentToolResultIDs=%s issues=%s",
		endpoint,
		bodyLen,
		current.ModelID,
		len(current.Content),
		len(current.Images),
		historyStats,
		lastHistoryRole(payload.ConversationState.History),
		compactList(pairing.LastAssistantToolUseIDs, 12),
		toolCount,
		compactList(toolNames, 12),
		resultCount,
		compactList(resultIDs, 12),
		compactList(schemaIssues, 16),
	)
}

func summarizeHistory(history []KiroHistoryMessage) string {
	var users, assistants, assistantToolTurns, userToolResultTurns int
	for _, msg := range history {
		if msg.UserInputMessage != nil {
			users++
			if ctx := msg.UserInputMessage.UserInputMessageContext; ctx != nil && len(ctx.ToolResults) > 0 {
				userToolResultTurns++
			}
		}
		if msg.AssistantResponseMessage != nil {
			assistants++
			if len(msg.AssistantResponseMessage.ToolUses) > 0 {
				assistantToolTurns++
			}
		}
	}
	return fmt.Sprintf("users=%d assistants=%d assistantToolTurns=%d userToolResultTurns=%d", users, assistants, assistantToolTurns, userToolResultTurns)
}

type toolPairingDiagnostics struct {
	LastAssistantToolUseIDs []string
	Issues                  []string
}

func inspectToolPairing(history []KiroHistoryMessage, currentResultIDs []string) toolPairingDiagnostics {
	diag := toolPairingDiagnostics{}
	if len(currentResultIDs) == 0 {
		return diag
	}

	lastAssistantIDs := map[string]bool{}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].AssistantResponseMessage == nil {
			continue
		}
		for _, tu := range history[i].AssistantResponseMessage.ToolUses {
			lastAssistantIDs[tu.ToolUseID] = true
			diag.LastAssistantToolUseIDs = append(diag.LastAssistantToolUseIDs, tu.ToolUseID)
		}
		break
	}
	sort.Strings(diag.LastAssistantToolUseIDs)

	for _, id := range currentResultIDs {
		if strings.TrimSpace(id) == "" {
			diag.Issues = append(diag.Issues, "toolResult:<empty-id>")
			continue
		}
		if !lastAssistantIDs[id] {
			diag.Issues = append(diag.Issues, "toolResult:orphan:"+id)
		}
	}
	return diag
}

func lastHistoryRole(history []KiroHistoryMessage) string {
	if len(history) == 0 {
		return "<empty>"
	}
	last := history[len(history)-1]
	switch {
	case last.UserInputMessage != nil && last.AssistantResponseMessage != nil:
		return "both"
	case last.UserInputMessage != nil:
		return "user"
	case last.AssistantResponseMessage != nil:
		return "assistant"
	default:
		return "empty"
	}
}

func inspectSchemaIssues(toolName string, schema interface{}) []string {
	if schema == nil {
		return []string{toolName + ":schema:nil"}
	}
	var issues []string
	walkSchema(toolName, "$", schema, &issues)
	return issues
}

func walkSchema(toolName, path string, value interface{}, issues *[]string) {
	switch v := value.(type) {
	case map[string]interface{}:
		if _, ok := v["additionalProperties"]; ok {
			*issues = append(*issues, fmt.Sprintf("%s:%s.additionalProperties", toolName, path))
		}
		if req, ok := v["required"]; ok {
			switch arr := req.(type) {
			case []interface{}:
				if len(arr) == 0 {
					*issues = append(*issues, fmt.Sprintf("%s:%s.required-empty", toolName, path))
				}
			case []string:
				if len(arr) == 0 {
					*issues = append(*issues, fmt.Sprintf("%s:%s.required-empty", toolName, path))
				}
			default:
				*issues = append(*issues, fmt.Sprintf("%s:%s.required-invalid", toolName, path))
			}
		}
		if typ, ok := v["type"]; !ok || strings.TrimSpace(fmt.Sprint(typ)) == "" {
			*issues = append(*issues, fmt.Sprintf("%s:%s.type-missing", toolName, path))
		}
		for k, child := range v {
			walkSchema(toolName, path+"."+k, child, issues)
		}
	case []interface{}:
		for i, child := range v {
			walkSchema(toolName, fmt.Sprintf("%s[%d]", path, i), child, issues)
		}
	}
}

func compactList(items []string, limit int) string {
	if len(items) == 0 {
		return "[]"
	}
	if len(items) <= limit {
		return "[" + strings.Join(items, ",") + "]"
	}
	return fmt.Sprintf("[%s,+%d more]", strings.Join(items[:limit], ","), len(items)-limit)
}
