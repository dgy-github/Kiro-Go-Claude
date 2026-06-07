package proxy

import (
	"fmt"
	"sort"
	"strings"
)

type toolStateMachine struct {
	payload            *KiroPayload
	contract           *ToolContract
	action             toolRunnerAction
	availableTools     map[string]bool
	watchAssistantPlan bool
	toolResultState    toolResultMatchState
	observedToolUses   []KiroToolUse
	observedText       strings.Builder
}

type toolStateDecision struct {
	OK           bool
	Repair       bool
	Violation    bool
	Reason       string
	ObservedText string
	Message      string
}

type toolResultMatchState struct {
	HasCurrentResults bool
	Matched           bool
	LastAssistantIDs  []string
	CurrentResultIDs  []string
	MissingResultIDs  []string
	OrphanResultIDs   []string
}

func newToolStateMachine(payload *KiroPayload) *toolStateMachine {
	m := &toolStateMachine{
		payload:            payload,
		action:             decideToolRunnerAction(payload),
		watchAssistantPlan: shouldWatchAssistantToolPlan(payload),
		toolResultState:    computePayloadToolResultState(payload),
		availableTools:     make(map[string]bool),
	}
	if payload != nil {
		m.contract = payload.ToolContract
		for _, name := range availableToolNamesFromPayload(payload) {
			m.availableTools[strings.ToLower(strings.TrimSpace(name))] = true
		}
	}
	return m
}

func (m *toolStateMachine) ObserveText(text string, isThinking bool) {
	if m == nil || isThinking || strings.TrimSpace(text) == "" {
		return
	}
	m.observedText.WriteString(text)
}

func (m *toolStateMachine) ObserveToolUse(tu KiroToolUse) {
	if m == nil {
		return
	}
	m.observedToolUses = append(m.observedToolUses, tu)
}

func (m *toolStateMachine) ShouldSuppressVisibleText() bool {
	if m == nil {
		return false
	}
	return m.action.EnforceTool
}

func (m *toolStateMachine) ShouldHoldContractText() bool {
	if m == nil {
		return false
	}
	return m.action.HoldText
}

func (m *toolStateMachine) ShouldWatchAssistantPlan() bool {
	return m != nil && m.watchAssistantPlan
}

func (m *toolStateMachine) ShouldSuppressFinalText(toolUses []KiroToolUse) bool {
	if m == nil {
		return false
	}
	return m.action.EnforceTool && len(m.effectiveToolUses(toolUses)) > 0
}

func (m *toolStateMachine) Finalize(content string, toolUses []KiroToolUse) toolStateDecision {
	if m == nil {
		return toolStateDecision{OK: true}
	}
	uses := m.effectiveToolUses(toolUses)
	observed := strings.TrimSpace(content)
	if observed == "" {
		observed = strings.TrimSpace(m.observedText.String())
	}

	if m.action.EnforceTool {
		if len(uses) == 0 {
			return m.repairableDecision("missing_tool_use", observed)
		}
		if required := strings.TrimSpace(m.contractToolName()); required != "" && !toolUsesContainName(uses, required) {
			return m.repairableDecision("wrong_tool_name", observedToolSummary(uses))
		}
		if maxUses := m.maxToolUses(); maxUses > 0 && len(uses) > maxUses {
			return m.repairableDecision("too_many_tool_uses", observedToolSummary(uses))
		}
		if unknown := m.firstUnknownTool(uses); unknown != "" {
			return m.repairableDecision("unknown_tool_name", unknown)
		}
		return toolStateDecision{OK: true}
	}

	if m.watchAssistantPlan && len(uses) == 0 && looksLikeAssistantToolPlanPlaceholder(content) {
		return m.repairableDecision("assistant_tool_plan_text", observed)
	}

	return toolStateDecision{OK: true}
}

func (m *toolStateMachine) PrepareRepair(decision toolStateDecision) bool {
	if m == nil || !decision.Repair {
		return false
	}
	if decision.Reason == "assistant_tool_plan_text" && m.payload != nil && m.payload.ToolContract == nil {
		m.payload.ToolContract = &ToolContract{
			RequiresTool:   true,
			Source:         "assistant-tool-plan",
			Mode:           toolContractModeRequireUpstreamTool,
			AvailableTools: availableToolNamesFromPayload(m.payload),
		}
		m.contract = m.payload.ToolContract
		m.action = decideToolRunnerAction(m.payload)
	}
	return prepareToolContractRepair(m.payload, decision.ObservedText)
}

func (m *toolStateMachine) ToolResultState() toolResultMatchState {
	if m == nil {
		return toolResultMatchState{}
	}
	return m.toolResultState
}

func (m *toolStateMachine) effectiveToolUses(toolUses []KiroToolUse) []KiroToolUse {
	if len(toolUses) > 0 {
		return toolUses
	}
	return m.observedToolUses
}

func (m *toolStateMachine) repairableDecision(reason, observed string) toolStateDecision {
	repair := m.canRepair()
	decision := toolStateDecision{
		OK:           false,
		Repair:       repair,
		Violation:    !repair,
		Reason:       reason,
		ObservedText: observed,
	}
	decision.Message = toolStateViolationMessage(m.contract, reason, observed)
	return decision
}

func (m *toolStateMachine) canRepair() bool {
	if m == nil || m.payload == nil {
		return false
	}
	if m.payload.ToolContract == nil {
		return m.watchAssistantPlan
	}
	return m.payload.ToolContract.RepairAttempts < maxToolContractRepairAttempts
}

func (m *toolStateMachine) contractToolName() string {
	if m == nil || m.contract == nil {
		return ""
	}
	return strings.TrimSpace(m.contract.ToolName)
}

func (m *toolStateMachine) maxToolUses() int {
	if m == nil || m.contract == nil {
		return 0
	}
	return m.contract.MaxToolUses
}

func (m *toolStateMachine) firstUnknownTool(toolUses []KiroToolUse) string {
	if m == nil || len(m.availableTools) == 0 {
		return ""
	}
	for _, tu := range toolUses {
		name := strings.ToLower(strings.TrimSpace(tu.Name))
		if name != "" && !m.availableTools[name] {
			return tu.Name
		}
	}
	return ""
}

func evaluateToolState(payload *KiroPayload, content string, toolUses []KiroToolUse) toolStateDecision {
	return newToolStateMachine(payload).Finalize(content, toolUses)
}

func evaluateToolContractState(payload *KiroPayload, content string, toolUses []KiroToolUse) toolStateDecision {
	m := newToolStateMachine(payload)
	m.watchAssistantPlan = false
	return m.Finalize(content, toolUses)
}

func evaluateAssistantToolPlanState(payload *KiroPayload, content string, toolUses []KiroToolUse) toolStateDecision {
	m := newToolStateMachine(payload)
	m.action = toolRunnerAction{}
	return m.Finalize(content, toolUses)
}

func toolUsesContainName(toolUses []KiroToolUse, required string) bool {
	required = strings.ToLower(strings.TrimSpace(required))
	for _, tu := range toolUses {
		if strings.ToLower(strings.TrimSpace(tu.Name)) == required {
			return true
		}
	}
	return false
}

func observedToolSummary(toolUses []KiroToolUse) string {
	if len(toolUses) == 0 {
		return ""
	}
	parts := make([]string, 0, len(toolUses))
	for _, tu := range toolUses {
		name := strings.TrimSpace(tu.Name)
		if name == "" {
			name = "<unnamed>"
		}
		id := strings.TrimSpace(tu.ToolUseID)
		if id == "" {
			parts = append(parts, name)
		} else {
			parts = append(parts, fmt.Sprintf("%s(%s)", name, id))
		}
	}
	return strings.Join(parts, ", ")
}

func toolStateViolationMessage(contract *ToolContract, reason, observedText string) string {
	base := toolContractViolationMessage(contract, observedText)
	if reason == "" || reason == "missing_tool_use" || reason == "assistant_tool_plan_text" {
		return base
	}
	return base + "; reason: " + reason
}

func computePayloadToolResultState(payload *KiroPayload) toolResultMatchState {
	if payload == nil {
		return toolResultMatchState{}
	}
	ctx := payload.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext
	if ctx == nil || len(ctx.ToolResults) == 0 {
		return toolResultMatchState{}
	}
	return computeToolResultMatchState(payload.ConversationState.History, ctx.ToolResults)
}

func computeToolResultMatchState(history []KiroHistoryMessage, currentToolResults []KiroToolResult) toolResultMatchState {
	state := toolResultMatchState{HasCurrentResults: len(currentToolResults) > 0}
	if len(currentToolResults) == 0 {
		return state
	}

	currentIDs := make(map[string]bool)
	for _, result := range currentToolResults {
		id := strings.TrimSpace(result.ToolUseID)
		if id == "" {
			continue
		}
		currentIDs[id] = true
	}
	state.CurrentResultIDs = sortedKeys(currentIDs)

	lastIDs := make(map[string]bool)
	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		if msg.AssistantResponseMessage == nil {
			continue
		}
		for _, tu := range msg.AssistantResponseMessage.ToolUses {
			id := strings.TrimSpace(tu.ToolUseID)
			if id != "" {
				lastIDs[id] = true
			}
		}
		break
	}
	state.LastAssistantIDs = sortedKeys(lastIDs)

	for id := range lastIDs {
		if !currentIDs[id] {
			state.MissingResultIDs = append(state.MissingResultIDs, id)
		}
	}
	for id := range currentIDs {
		if !lastIDs[id] {
			state.OrphanResultIDs = append(state.OrphanResultIDs, id)
		}
	}
	sort.Strings(state.MissingResultIDs)
	sort.Strings(state.OrphanResultIDs)
	state.Matched = len(currentIDs) > 0 && len(lastIDs) > 0 &&
		len(state.MissingResultIDs) == 0 && len(state.OrphanResultIDs) == 0
	return state
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
