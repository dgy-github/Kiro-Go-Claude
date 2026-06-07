package proxy

import "strings"

const maxToolContractRepairAttempts = 1

const (
	toolContractModeRequireUpstreamTool = "require_upstream_tool"
	toolContractModeSyntheticToolUse    = "synthetic_tool_use"
)

type toolRunnerAction struct {
	DirectToolUse *KiroToolUse
	HoldText      bool
	EnforceTool   bool
}

func decideToolRunnerAction(payload *KiroPayload) toolRunnerAction {
	if payload == nil || payload.ToolContract == nil {
		return toolRunnerAction{}
	}
	contract := payload.ToolContract
	if !contract.RequiresTool || len(contract.AvailableTools) == 0 {
		return toolRunnerAction{}
	}
	mode := contract.Mode
	if mode == "" && contract.SyntheticToolUse != nil {
		mode = toolContractModeSyntheticToolUse
	}
	if mode == "" {
		mode = toolContractModeRequireUpstreamTool
	}

	switch mode {
	case toolContractModeSyntheticToolUse:
		return toolRunnerAction{DirectToolUse: contract.SyntheticToolUse}
	case toolContractModeRequireUpstreamTool:
		return toolRunnerAction{
			HoldText:    contract.RepairAttempts < maxToolContractRepairAttempts,
			EnforceTool: true,
		}
	default:
		return toolRunnerAction{}
	}
}

func shouldRepairToolContract(payload *KiroPayload, content string, toolUses []KiroToolUse) bool {
	action := decideToolRunnerAction(payload)
	if !action.EnforceTool {
		return false
	}
	if len(toolUses) > 0 {
		return false
	}
	return strings.TrimSpace(content) != ""
}

func shouldHoldToolContractText(payload *KiroPayload) bool {
	return decideToolRunnerAction(payload).HoldText
}

func syntheticToolUse(payload *KiroPayload) (KiroToolUse, bool) {
	tu := decideToolRunnerAction(payload).DirectToolUse
	if tu == nil {
		return KiroToolUse{}, false
	}
	return *tu, true
}

func prepareToolContractRepair(payload *KiroPayload, observedText string) bool {
	if payload == nil || payload.ToolContract == nil {
		return false
	}
	contract := payload.ToolContract
	if contract.RepairAttempts >= maxToolContractRepairAttempts {
		return false
	}
	contract.RepairAttempts++
	return true
}

func toolContractViolationMessage(contract *ToolContract, observedText string) string {
	observedText = strings.TrimSpace(observedText)
	if len([]rune(observedText)) > 500 {
		runes := []rune(observedText)
		observedText = string(runes[:500]) + "..."
	}

	var b strings.Builder
	b.WriteString("Upstream returned text without a structured tool_use even though this turn requires tool use")
	if contract != nil && contract.Source != "" {
		b.WriteString(" (source: ")
		b.WriteString(contract.Source)
		b.WriteString(")")
	}
	if contract != nil && contract.ToolName != "" {
		b.WriteString("; required tool: ")
		b.WriteString(contract.ToolName)
	}
	if observedText != "" {
		b.WriteString(". Last text-only response: ")
		b.WriteString(observedText)
	}
	return b.String()
}

func toolUsesFromToolCalls(calls []ToolCall) []KiroToolUse {
	if len(calls) == 0 {
		return nil
	}
	uses := make([]KiroToolUse, 0, len(calls))
	for _, call := range calls {
		uses = append(uses, KiroToolUse{
			ToolUseID: call.ID,
			Name:      call.Function.Name,
		})
	}
	return uses
}
