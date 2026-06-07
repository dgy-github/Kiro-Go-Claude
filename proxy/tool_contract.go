package proxy

import "strings"

const maxToolContractRepairAttempts = 1
const assistantToolPlanHoldMaxRunes = 500

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
	decision := evaluateToolContractState(payload, content, toolUses)
	return !decision.OK
}

func shouldWatchAssistantToolPlan(payload *KiroPayload) bool {
	if payload == nil || payload.ToolContract != nil {
		return false
	}
	return len(availableToolNamesFromPayload(payload)) > 0
}

func shouldRepairAssistantToolPlan(payload *KiroPayload, content string, toolUses []KiroToolUse) bool {
	decision := evaluateAssistantToolPlanState(payload, content, toolUses)
	return !decision.OK
}

func prepareAssistantToolPlanRepair(payload *KiroPayload, observedText string) bool {
	decision := evaluateAssistantToolPlanState(payload, observedText, nil)
	return newToolStateMachine(payload).PrepareRepair(decision)
}

func availableToolNamesFromPayload(payload *KiroPayload) []string {
	if payload == nil {
		return nil
	}
	ctx := payload.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext
	if ctx == nil || len(ctx.Tools) == 0 {
		return nil
	}
	return toolNamesFromWrappers(ctx.Tools)
}

func looksLikeAssistantToolPlanPlaceholder(content string) bool {
	text := strings.ToLower(strings.TrimSpace(content))
	if text == "" {
		return false
	}
	for _, marker := range []string{
		"先 grep", "先grep", "先 rg", "先rg", "先 read", "先read",
		"先读", "先查", "先看", "先打开", "先扫描", "先检查", "先验证",
		"我先 grep", "我先读", "我先查", "我先看", "我先跑", "我先执行",
		"让我先", "先用 grep", "先用rg", "先用 rg", "先执行", "先运行", "先跑",
		"let me grep", "let me read", "let me inspect", "let me check", "let me run",
		"i'll grep", "i'll read", "i'll inspect", "i'll check", "i'll run",
		"i will grep", "i will read", "i will inspect", "i will check", "i will run",
		"first i'll", "first i will",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	if strings.Contains(text, "需要先") {
		for _, marker := range []string{"grep", "rg", "读", "查", "看", "打开", "扫描", "检查", "验证", "执行", "运行"} {
			if strings.Contains(text, marker) {
				return true
			}
		}
	}
	return false
}

func shouldHoldToolContractText(payload *KiroPayload) bool {
	return newToolStateMachine(payload).ShouldHoldContractText()
}

func shouldSuppressToolContractVisibleText(payload *KiroPayload) bool {
	return newToolStateMachine(payload).ShouldSuppressVisibleText()
}

func suppressToolContractFinalText(payload *KiroPayload, content string, toolUses []KiroToolUse) string {
	if newToolStateMachine(payload).ShouldSuppressFinalText(toolUses) {
		return ""
	}
	return content
}

func syntheticToolUse(payload *KiroPayload) (KiroToolUse, bool) {
	tu := decideToolRunnerAction(payload).DirectToolUse
	if tu == nil {
		return KiroToolUse{}, false
	}
	return *tu, true
}

func restoreToolUseName(tu KiroToolUse, nameMap map[string]string) KiroToolUse {
	if nameMap == nil {
		return tu
	}
	if original, ok := nameMap[tu.Name]; ok && strings.TrimSpace(original) != "" {
		tu.Name = original
	}
	return tu
}

func restoreToolUseNames(toolUses []KiroToolUse, nameMap map[string]string) []KiroToolUse {
	if len(toolUses) == 0 || len(nameMap) == 0 {
		return toolUses
	}
	out := make([]KiroToolUse, len(toolUses))
	for i, tu := range toolUses {
		out[i] = restoreToolUseName(tu, nameMap)
	}
	return out
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
	appendToolContractRepairInstruction(payload, observedText)
	return true
}

func appendToolContractRepairInstruction(payload *KiroPayload, observedText string) {
	if payload == nil || payload.ToolContract == nil {
		return
	}
	msg := &payload.ConversationState.CurrentMessage.UserInputMessage
	if strings.Contains(msg.Content, "[Kiro-Go backend tool repair]") {
		return
	}
	contract := payload.ToolContract
	tools := strings.Join(contract.AvailableTools, ", ")
	if tools == "" {
		tools = "(tools were advertised, but names were unavailable)"
	}
	observedText = strings.TrimSpace(observedText)
	if len([]rune(observedText)) > 300 {
		runes := []rune(observedText)
		observedText = string(runes[:300]) + "..."
	}

	var b strings.Builder
	b.WriteString(strings.TrimRight(msg.Content, "\r\n "))
	b.WriteString("\n\n[Kiro-Go backend tool repair]\n")
	b.WriteString("The previous upstream response was plain text, but this turn requires a real structured tool_use/tool call. ")
	b.WriteString("Do not describe the action in prose. Emit exactly the appropriate tool call now.\n")
	b.WriteString("Available tool names: ")
	b.WriteString(tools)
	b.WriteString("\n")
	if contract.ToolName != "" {
		b.WriteString("Required tool name: ")
		b.WriteString(contract.ToolName)
		b.WriteString("\n")
	}
	if observedText != "" {
		b.WriteString("Previous text-only response to repair: ")
		b.WriteString(observedText)
		b.WriteString("\n")
	}
	b.WriteString("If the work involves shell, git, curl, or an API call, use the shell/bash/exec command tool exposed in this request. Do not print secrets.")
	msg.Content = b.String()
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
