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

func prepareToolContractRepair(payload *KiroPayload, observedText string) bool {
	if payload == nil || payload.ToolContract == nil {
		return false
	}
	contract := payload.ToolContract
	if contract.RepairAttempts >= maxToolContractRepairAttempts {
		return false
	}
	contract.RepairAttempts++

	msg := &payload.ConversationState.CurrentMessage.UserInputMessage
	msg.Content = strings.TrimSpace(msg.Content) + "\n\n" + buildToolRepairInstruction(contract, observedText)
	return true
}

func buildToolRepairInstruction(contract *ToolContract, observedText string) string {
	observedText = strings.TrimSpace(observedText)
	if len([]rune(observedText)) > 500 {
		runes := []rune(observedText)
		observedText = string(runes[:500]) + "..."
	}

	var b strings.Builder
	b.WriteString("[Kiro-Go repair: The previous assistant attempt violated the tool contract by returning text without a structured tool_use.")
	if observedText != "" {
		b.WriteString(" Previous text was: ")
		b.WriteString(observedText)
	}
	b.WriteString(" Retry now and emit a real structured tool_use")
	if contract.ToolName != "" {
		b.WriteString(" for `")
		b.WriteString(contract.ToolName)
		b.WriteString("`")
	}
	b.WriteString(" before any final answer.]")
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
