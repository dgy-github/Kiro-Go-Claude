package proxy

import "strings"

const maxToolContractRepairAttempts = 1

func shouldRepairToolContract(payload *KiroPayload, content string, toolUses []KiroToolUse) bool {
	if payload == nil || payload.ToolContract == nil {
		return false
	}
	contract := payload.ToolContract
	if !contract.RequiresTool || len(contract.AvailableTools) == 0 {
		return false
	}
	if len(toolUses) > 0 {
		return false
	}
	return strings.TrimSpace(content) != ""
}

func shouldHoldToolContractText(payload *KiroPayload) bool {
	if payload == nil || payload.ToolContract == nil {
		return false
	}
	contract := payload.ToolContract
	return contract.RequiresTool &&
		len(contract.AvailableTools) > 0 &&
		contract.RepairAttempts < maxToolContractRepairAttempts
}

func syntheticToolUse(payload *KiroPayload) (KiroToolUse, bool) {
	if payload == nil || payload.ToolContract == nil || payload.ToolContract.SyntheticToolUse == nil {
		return KiroToolUse{}, false
	}
	return *payload.ToolContract.SyntheticToolUse, true
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
