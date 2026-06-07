package proxy

import (
	"strings"
	"testing"
)

func TestExtractOpenAIMessageTextStructured(t *testing.T) {
	content := []interface{}{
		map[string]interface{}{"type": "text", "text": "alpha"},
		map[string]interface{}{"type": "input_text", "text": "beta"},
	}

	if got := extractOpenAIMessageText(content); got != "alphabeta" {
		t.Fatalf("expected concatenated structured text, got %q", got)
	}

	nested := map[string]interface{}{
		"content": []interface{}{map[string]interface{}{"type": "text", "text": "nested"}},
	}
	if got := extractOpenAIMessageText(nested); got != "nested" {
		t.Fatalf("expected nested content extraction, got %q", got)
	}
}

func TestOpenAIToKiroPreservesStructuredAssistantAndToolContent(t *testing.T) {
	req := &OpenAIRequest{
		Model: "claude-sonnet-4.5",
		Messages: []OpenAIMessage{
			{
				Role: "system",
				Content: []interface{}{
					map[string]interface{}{"type": "text", "text": "system-a"},
					map[string]interface{}{"type": "text", "text": "system-b"},
				},
			},
			{Role: "user", Content: "first-question"},
			{
				Role: "assistant",
				Content: []interface{}{
					map[string]interface{}{"type": "text", "text": "assistant-structured"},
				},
			},
			{
				Role:       "tool",
				ToolCallID: "call_1",
				Content: []interface{}{
					map[string]interface{}{"type": "text", "text": "tool-result-structured"},
				},
			},
		},
	}

	payload := OpenAIToKiro(req, false)

	// History starts with a priming pair.
	if len(payload.ConversationState.History) != 4 {
		t.Fatalf("expected 4 history items (2 priming + 2 conversation), got %d", len(payload.ConversationState.History))
	}

	// history[0]: priming user
	primingUser := payload.ConversationState.History[0].UserInputMessage
	if primingUser == nil {
		t.Fatalf("expected history[0] to be priming user message")
	}
	if !strings.Contains(primingUser.Content, "system-a") || !strings.Contains(primingUser.Content, "system-b") {
		t.Fatalf("expected priming user message to contain system prompt, got %q", primingUser.Content)
	}
	if strings.Contains(primingUser.Content, "first-question") {
		t.Fatalf("expected system prompt priming not to contain user question, got %q", primingUser.Content)
	}

	// history[1]: priming assistant
	primingAssistant := payload.ConversationState.History[1].AssistantResponseMessage
	if primingAssistant == nil {
		t.Fatalf("expected history[1] to be priming assistant message")
	}
	if primingAssistant.Content != "I will follow these instructions." {
		t.Fatalf("expected priming assistant ack, got %q", primingAssistant.Content)
	}

	// history[2]: first user turn
	firstConvUser := payload.ConversationState.History[2].UserInputMessage
	if firstConvUser == nil {
		t.Fatalf("expected history[2] to be first conversation user message")
	}
	if !strings.Contains(firstConvUser.Content, "first-question") {
		t.Fatalf("expected history[2] to contain first-question, got %q", firstConvUser.Content)
	}

	// history[3]: assistant reply
	historyAssistant := payload.ConversationState.History[3].AssistantResponseMessage
	if historyAssistant == nil {
		t.Fatalf("expected history[3] to be assistant message")
	}
	if historyAssistant.Content != "assistant-structured" {
		t.Fatalf("expected assistant structured content to be preserved, got %q", historyAssistant.Content)
	}

	// The tool result answers call_1, but the last history assistant has no
	// matching structured tool call (it is text-only), so the tool result is an
	// orphan. Kiro's upstream rejects structured tool results that do not answer
	// the immediately preceding assistant tool call, so it must be narrated into
	// the current message text rather than kept structured.
	cur := payload.ConversationState.CurrentMessage.UserInputMessage
	if !strings.Contains(cur.Content, "tool-result-structured") {
		t.Fatalf("expected tool-result continuation content, got %q", cur.Content)
	}
	if cur.UserInputMessageContext != nil && len(cur.UserInputMessageContext.ToolResults) != 0 {
		t.Fatalf("expected orphan tool result to be flattened into text, not kept structured")
	}
}

func TestClaudeContinuationAfterReadPlanGetsToolUseNudge(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Tools: []ClaudeTool{testClaudeReadTool()},
		Messages: []ClaudeMessage{
			{Role: "user", Content: "修一下"},
			{Role: "assistant", Content: "先读 `_run_scheduled_turn` 全文，看何种结局怎么落地。"},
			{Role: "user", Content: "继续"},
		},
	}

	payload := ClaudeToKiro(req, false)
	content := payload.ConversationState.CurrentMessage.UserInputMessage.Content
	if payload.ToolContract != nil {
		t.Fatalf("authorized continuation must not become a hard tool contract")
	}
	if strings.Contains(content, "Kiro-Go tool contract") {
		t.Fatalf("tool contract must not be injected into user content, got %q", content)
	}
	if !strings.Contains(content, "继续") {
		t.Fatalf("expected original continuation content preserved, got %q", content)
	}
}

func TestClaudeNormalContinuationDoesNotGetToolUseNudge(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "解释一下"},
			{Role: "assistant", Content: "这个概念可以分三层讲。"},
			{Role: "user", Content: "继续"},
		},
	}

	payload := ClaudeToKiro(req, false)
	if payload.ToolContract != nil {
		t.Fatalf("did not expect tool contract for normal continuation")
	}
}

func TestClaudeContinuationAfterGitStatusPlanSynthesizesCommand(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Tools: []ClaudeTool{testClaudeExecCommandTool()},
		Messages: []ClaudeMessage{
			{Role: "user", Content: "上传 nanocodex 到 GitLab"},
			{Role: "assistant", Content: "先侦察 git 状态：是否已 init，有无 commit，有无 remote。并行查 git 状态。"},
			{Role: "user", Content: "继续"},
		},
	}

	payload := ClaudeToKiro(req, false)
	if payload.ToolContract == nil || payload.ToolContract.SyntheticToolUse == nil {
		t.Fatalf("expected synthetic git status command")
	}
	if payload.ToolContract.Source != "authorized-continuation" {
		t.Fatalf("expected authorized-continuation source, got %q", payload.ToolContract.Source)
	}
	if payload.ToolContract.Mode != toolContractModeSyntheticToolUse {
		t.Fatalf("expected synthetic tool contract mode, got %q", payload.ToolContract.Mode)
	}
	tu := payload.ToolContract.SyntheticToolUse
	if tu.Name != "execCommand" {
		t.Fatalf("expected execCommand tool, got %q", tu.Name)
	}
	if got := tu.Input["cmd"]; got != "git status --short --branch" {
		t.Fatalf("expected git status command, got %#v", got)
	}
}

func TestClaudeContinuationAfterRepoAuditPlanSynthesizesReadonlyAuditCommand(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Tools: []ClaudeTool{testClaudeExecCommandTool()},
		Messages: []ClaudeMessage{
			{Role: "user", Content: "A"},
			{Role: "assistant", Content: "执行侦察：确认 git toplevel、D:\\agent_prac\\nanocodex 内有无独立 .git、有无敏感文件。"},
			{Role: "user", Content: "继续"},
		},
	}

	payload := ClaudeToKiro(req, false)
	if payload.ToolContract == nil || payload.ToolContract.SyntheticToolUse == nil {
		t.Fatalf("expected synthetic repo audit command")
	}
	if payload.ToolContract.Mode != toolContractModeSyntheticToolUse {
		t.Fatalf("expected synthetic tool contract mode, got %q", payload.ToolContract.Mode)
	}
	tu := payload.ToolContract.SyntheticToolUse
	cmd, _ := tu.Input["cmd"].(string)
	for _, want := range []string{
		"$repo = 'D:\\agent_prac\\nanocodex'",
		"Test-Path -LiteralPath (Join-Path $repo '.git')",
		"git -C $repo rev-parse --show-toplevel",
		"git -C $repo status --short --branch",
		"git -C $repo remote -v",
		"ls-files --cached --others --exclude-standard",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("expected repo audit command to contain %q, got %q", want, cmd)
		}
	}
	if got := tu.Input["description"]; got != "Inspect git repository status and sensitive filename candidates" {
		t.Fatalf("expected repo audit description, got %#v", got)
	}
}

func TestClaudeContinuationAfterGitStatusPlanWithoutShellToolDoesNotCreateContract(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Tools: []ClaudeTool{testClaudeReadTool()},
		Messages: []ClaudeMessage{
			{Role: "assistant", Content: "并行查 git 状态。"},
			{Role: "user", Content: "继续"},
		},
	}

	payload := ClaudeToKiro(req, false)
	if payload.ToolContract != nil {
		t.Fatalf("git status continuation without shell-like tool must not create hard contract")
	}
}

func TestClaudeReadonlyFileCheckGetsToolUseNudge(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Tools: []ClaudeTool{testClaudeReadTool()},
		Messages: []ClaudeMessage{
			{Role: "user", Content: "把临时探针文件 _probe_sched.py 的删除再确认一下"},
		},
	}

	payload := ClaudeToKiro(req, false)
	content := payload.ConversationState.CurrentMessage.UserInputMessage.Content
	if payload.ToolContract == nil || !payload.ToolContract.RequiresTool {
		t.Fatalf("expected tool contract for readonly file check")
	}
	if payload.ToolContract.Source != "readonly-inspection" {
		t.Fatalf("expected readonly-inspection source, got %q", payload.ToolContract.Source)
	}
	if payload.ToolContract.SyntheticToolUse == nil {
		t.Fatalf("expected synthetic read tool use for readonly file check")
	}
	if payload.ToolContract.SyntheticToolUse.Name != "read" {
		t.Fatalf("expected synthetic read tool, got %q", payload.ToolContract.SyntheticToolUse.Name)
	}
	if got := payload.ToolContract.SyntheticToolUse.Input["path"]; got != "_probe_sched.py" {
		t.Fatalf("expected synthetic path _probe_sched.py, got %#v", got)
	}
	if strings.Contains(content, "Kiro-Go tool contract") {
		t.Fatalf("tool contract must not be injected into user content, got %q", content)
	}
	if !strings.Contains(content, "_probe_sched.py") {
		t.Fatalf("expected original file check request preserved, got %q", content)
	}
}

func TestClaudeCasualConfirmQuestionDoesNotGetToolUseNudge(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "你确认这个思路是对的吗？"},
		},
	}

	payload := ClaudeToKiro(req, false)
	if payload.ToolContract != nil {
		t.Fatalf("did not expect tool contract for casual confirmation")
	}
}

func TestClaudeWriteHandoffAuthorizationGetsToolUseNudge(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Tools: []ClaudeTool{testClaudeReadTool()},
		Messages: []ClaudeMessage{
			{Role: "user", Content: "这轮要写进 HANDOFF 吗"},
			{Role: "assistant", Content: "这一轮（问题一 MCP 重连 + 问题二失败自动禁用）要写进 HANDOFF 吗？"},
			{Role: "user", Content: "写吧"},
		},
	}

	payload := ClaudeToKiro(req, false)
	content := payload.ConversationState.CurrentMessage.UserInputMessage.Content
	if payload.ToolContract != nil {
		t.Fatalf("handoff authorization must not become a hard tool contract")
	}
	if strings.Contains(content, "Kiro-Go tool contract") {
		t.Fatalf("tool contract must not be injected into user content, got %q", content)
	}
	if !strings.Contains(content, "写吧") {
		t.Fatalf("expected original authorization content preserved, got %q", content)
	}
}

func TestClaudeToolChoiceAnyBuildsToolContract(t *testing.T) {
	req := &ClaudeRequest{
		Model:      "claude-opus-4.8",
		Tools:      []ClaudeTool{testClaudeReadTool()},
		ToolChoice: map[string]interface{}{"type": "any"},
		Messages: []ClaudeMessage{
			{Role: "user", Content: "check the file"},
		},
	}

	payload := ClaudeToKiro(req, false)
	if payload.ToolContract == nil || !payload.ToolContract.RequiresTool {
		t.Fatalf("expected tool_choice any to require a tool")
	}
	if payload.ToolContract.Source != "tool_choice" {
		t.Fatalf("expected tool_choice source, got %q", payload.ToolContract.Source)
	}
	if payload.ToolContract.Mode != toolContractModeRequireUpstreamTool {
		t.Fatalf("expected require-upstream-tool mode, got %q", payload.ToolContract.Mode)
	}
}

func TestDelegatedExecutionBuildsUpstreamToolContract(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Tools: []ClaudeTool{testClaudeExecCommandTool()},
		Messages: []ClaudeMessage{
			{Role: "user", Content: "你自己建仓库，我在背面试题目"},
		},
	}

	payload := ClaudeToKiro(req, false)
	if payload.ToolContract == nil || !payload.ToolContract.RequiresTool {
		t.Fatalf("expected delegated execution to require upstream tool use")
	}
	if payload.ToolContract.Source != "delegated-execution" {
		t.Fatalf("expected delegated-execution source, got %q", payload.ToolContract.Source)
	}
	if payload.ToolContract.Mode != toolContractModeRequireUpstreamTool {
		t.Fatalf("expected require-upstream-tool mode, got %q", payload.ToolContract.Mode)
	}
	if payload.ToolContract.SyntheticToolUse != nil {
		t.Fatalf("delegated execution must not synthesize risky tool calls")
	}
}

func TestDelegatedExecutionDoesNotTriggerForQuestion(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Tools: []ClaudeTool{testClaudeExecCommandTool()},
		Messages: []ClaudeMessage{
			{Role: "user", Content: "这个跟 nanocodex 有什么关系"},
		},
	}

	payload := ClaudeToKiro(req, false)
	if payload.ToolContract != nil {
		t.Fatalf("question-only message should not require tool use")
	}
}

func TestLocalLocationLookupRequiresToolUse(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Tools: []ClaudeTool{testClaudeExecCommandTool()},
		Messages: []ClaudeMessage{
			{Role: "user", Content: "面试题入口你知道在哪里"},
		},
	}

	payload := ClaudeToKiro(req, false)
	if payload.ToolContract == nil || !payload.ToolContract.RequiresTool {
		t.Fatalf("expected local location lookup to require upstream tool use")
	}
	if payload.ToolContract.Source != "local-location-lookup" {
		t.Fatalf("expected local-location-lookup source, got %q", payload.ToolContract.Source)
	}
	if payload.ToolContract.Mode != toolContractModeRequireUpstreamTool {
		t.Fatalf("expected require-upstream-tool mode, got %q", payload.ToolContract.Mode)
	}
}

func TestLocalLocationLookupDoesNotTriggerForConceptQuestion(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Tools: []ClaudeTool{testClaudeExecCommandTool()},
		Messages: []ClaudeMessage{
			{Role: "user", Content: "RAG 的入口层是什么意思"},
		},
	}

	payload := ClaudeToKiro(req, false)
	if payload.ToolContract != nil {
		t.Fatalf("concept question should not require tool use")
	}
}

func TestFileBackedWorkRequiresToolUseForNamedHTML(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Tools: []ClaudeTool{testClaudeExecCommandTool()},
		Messages: []ClaudeMessage{
			{Role: "user", Content: "interview-rag-day-plan.html 从这个入口重新整理下面试题，先读全文"},
		},
	}

	payload := ClaudeToKiro(req, false)
	if payload.ToolContract == nil || !payload.ToolContract.RequiresTool {
		t.Fatalf("expected file-backed work to require upstream tool use")
	}
	if payload.ToolContract.Source != "file-backed-work" {
		t.Fatalf("expected file-backed-work source, got %q", payload.ToolContract.Source)
	}
	if payload.ToolContract.Mode != toolContractModeRequireUpstreamTool {
		t.Fatalf("expected require-upstream-tool mode, got %q", payload.ToolContract.Mode)
	}
}

func TestFileBackedWorkDoesNotTriggerForConceptQuestion(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Tools: []ClaudeTool{testClaudeExecCommandTool()},
		Messages: []ClaudeMessage{
			{Role: "user", Content: "index.html 是什么文件格式"},
		},
	}

	payload := ClaudeToKiro(req, false)
	if payload.ToolContract != nil {
		t.Fatalf("conceptual file question should not require tool use")
	}
}

func TestSyntheticReadToolUsesFilePathSchemaKey(t *testing.T) {
	tool := testClaudeReadTool()
	tool.InputSchema = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"file_path": map[string]interface{}{"type": "string"},
		},
	}
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Tools: []ClaudeTool{tool},
		Messages: []ClaudeMessage{
			{Role: "user", Content: "检查 `src/main.go` 是否存在"},
		},
	}

	payload := ClaudeToKiro(req, false)
	if payload.ToolContract == nil || payload.ToolContract.SyntheticToolUse == nil {
		t.Fatalf("expected synthetic tool use")
	}
	if got := payload.ToolContract.SyntheticToolUse.Input["file_path"]; got != "src/main.go" {
		t.Fatalf("expected file_path src/main.go, got %#v", got)
	}
	if _, exists := payload.ToolContract.SyntheticToolUse.Input["path"]; exists {
		t.Fatalf("did not expect fallback path key when file_path exists")
	}
}

func TestReadonlyFileCheckWithoutReadToolDoesNotCreateHardContract(t *testing.T) {
	var tool ClaudeTool
	tool.Name = "write"
	tool.Description = "Write a file"
	tool.InputSchema = map[string]interface{}{"type": "object"}
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Tools: []ClaudeTool{tool},
		Messages: []ClaudeMessage{
			{Role: "user", Content: "检查 `_probe_sched.py` 是否存在"},
		},
	}

	payload := ClaudeToKiro(req, false)
	if payload.ToolContract != nil {
		t.Fatalf("readonly file check without read-like tool must not create hard contract")
	}
}

func TestToolContractRepairIsBounded(t *testing.T) {
	payload := &KiroPayload{
		ToolContract: &ToolContract{
			RequiresTool:   true,
			Mode:           toolContractModeRequireUpstreamTool,
			AvailableTools: []string{"read"},
		},
	}
	payload.ConversationState.CurrentMessage.UserInputMessage.Content = "check file"

	if !shouldRepairToolContract(payload, "Verifying file now.", nil) {
		t.Fatalf("expected text-only response to violate tool contract")
	}
	if !prepareToolContractRepair(payload, "Verifying file now.") {
		t.Fatalf("expected first repair to be prepared")
	}
	content := payload.ConversationState.CurrentMessage.UserInputMessage.Content
	if !strings.Contains(content, "check file") {
		t.Fatalf("repair must preserve original user content, got %q", content)
	}
	if !strings.Contains(content, "[Kiro-Go backend tool repair]") {
		t.Fatalf("expected backend repair instruction in retry payload, got %q", content)
	}
	if !strings.Contains(content, "Available tool names: read") {
		t.Fatalf("expected repair to include available tools, got %q", content)
	}
	if !strings.Contains(content, "Verifying file now.") {
		t.Fatalf("expected repair to include observed text-only response, got %q", content)
	}
	if prepareToolContractRepair(payload, "Still checking.") {
		t.Fatalf("second repair must be blocked to avoid infinite loops")
	}
	if strings.Count(payload.ConversationState.CurrentMessage.UserInputMessage.Content, "[Kiro-Go backend tool repair]") != 1 {
		t.Fatalf("repair instruction must be injected at most once")
	}
	msg := toolContractViolationMessage(payload.ToolContract, "Still checking.")
	if !strings.Contains(msg, "tool_contract") && !strings.Contains(msg, "tool") {
		t.Fatalf("expected explicit violation message, got %q", msg)
	}
}

func TestToolRunnerActionSeparatesSyntheticFromUpstreamRepair(t *testing.T) {
	synthetic := &KiroPayload{
		ToolContract: &ToolContract{
			RequiresTool:   true,
			Mode:           toolContractModeSyntheticToolUse,
			AvailableTools: []string{"read"},
			SyntheticToolUse: &KiroToolUse{
				ToolUseID: "toolu_test",
				Name:      "read",
				Input:     map[string]interface{}{"path": "HANDOFF.md"},
			},
		},
	}
	if _, ok := syntheticToolUse(synthetic); !ok {
		t.Fatalf("expected synthetic contract to produce direct tool use")
	}
	if shouldHoldToolContractText(synthetic) {
		t.Fatalf("synthetic contract should bypass upstream text holding")
	}
	if shouldRepairToolContract(synthetic, "I'll read it now.", nil) {
		t.Fatalf("synthetic contract should not enter upstream repair loop")
	}

	upstream := &KiroPayload{
		ToolContract: &ToolContract{
			RequiresTool:   true,
			Mode:           toolContractModeRequireUpstreamTool,
			AvailableTools: []string{"read"},
		},
	}
	if _, ok := syntheticToolUse(upstream); ok {
		t.Fatalf("upstream-required contract must not synthesize a tool")
	}
	if !shouldHoldToolContractText(upstream) {
		t.Fatalf("upstream-required contract should hold placeholder text before repair")
	}
	if !shouldRepairToolContract(upstream, "I'll read it now.", nil) {
		t.Fatalf("upstream-required contract should repair text-only responses")
	}
}

func TestToolContractSuppressesVisibleTextAfterToolUse(t *testing.T) {
	payload := &KiroPayload{
		ToolContract: &ToolContract{
			RequiresTool:   true,
			Mode:           toolContractModeRequireUpstreamTool,
			AvailableTools: []string{"Bash"},
		},
	}
	toolUses := []KiroToolUse{{ToolUseID: "t1", Name: "Bash"}}

	if got := suppressToolContractFinalText(payload, "internal repair leaked", toolUses); got != "" {
		t.Fatalf("expected tool-contract text to be suppressed after tool_use, got %q", got)
	}
	if got := suppressToolContractFinalText(payload, "still need a tool", nil); got != "still need a tool" {
		t.Fatalf("text must stay available before tool_use for repair detection, got %q", got)
	}
	if got := suppressToolContractFinalText(&KiroPayload{}, "normal answer", toolUses); got != "normal answer" {
		t.Fatalf("normal non-contract text must not be suppressed, got %q", got)
	}
}

func TestRestoreToolUseNamesUsesClientToolNames(t *testing.T) {
	toolUses := []KiroToolUse{
		{ToolUseID: "t1", Name: "bash", Input: map[string]interface{}{"command": "git status"}},
		{ToolUseID: "t2", Name: "execCommand", Input: map[string]interface{}{"cmd": "git status"}},
		{ToolUseID: "t3", Name: "read", Input: map[string]interface{}{"path": "README.md"}},
	}

	restored := restoreToolUseNames(toolUses, map[string]string{
		"bash":        "Bash",
		"execCommand": "exec_command",
	})

	if restored[0].Name != "Bash" {
		t.Fatalf("expected Bash, got %q", restored[0].Name)
	}
	if restored[1].Name != "exec_command" {
		t.Fatalf("expected exec_command, got %q", restored[1].Name)
	}
	if restored[2].Name != "read" {
		t.Fatalf("unexpected change for unmapped tool: %q", restored[2].Name)
	}
	if toolUses[0].Name != "bash" {
		t.Fatalf("restore must not mutate caller slice")
	}
}

func testClaudeReadTool() ClaudeTool {
	return ClaudeTool{
		Name:        "read",
		Description: "Read a file",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string"},
			},
			"required": []interface{}{"path"},
		},
	}
}

func testClaudeExecCommandTool() ClaudeTool {
	return ClaudeTool{
		Name:        "exec_command",
		Description: "Run a shell command",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"cmd":         map[string]interface{}{"type": "string"},
				"description": map[string]interface{}{"type": "string"},
			},
			"required": []interface{}{"cmd"},
		},
	}
}

func TestOpenAIToKiroAssistantMapContentInHistory(t *testing.T) {
	req := &OpenAIRequest{
		Model: "claude-sonnet-4.5",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "u1"},
			{Role: "assistant", Content: map[string]interface{}{"type": "text", "text": "assistant-map"}},
			{Role: "user", Content: "u2"},
		},
	}

	payload := OpenAIToKiro(req, false)

	if len(payload.ConversationState.History) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(payload.ConversationState.History))
	}
	assistant := payload.ConversationState.History[1].AssistantResponseMessage
	if assistant == nil {
		t.Fatalf("expected second history entry to be assistant")
	}
	if assistant.Content != "assistant-map" {
		t.Fatalf("expected assistant map content preserved, got %q", assistant.Content)
	}
}

func TestOpenAIToKiroAssistantToolCallsDoNotInjectPlaceholder(t *testing.T) {
	req := &OpenAIRequest{
		Model: "claude-sonnet-4.5",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "find weather"},
			{
				Role:    "assistant",
				Content: nil,
				ToolCalls: []ToolCall{{
					ID:   "call_1",
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: "get_weather", Arguments: "{}"},
				}},
			},
			{Role: "user", Content: "continue"},
		},
	}

	payload := OpenAIToKiro(req, false)

	// The mid-history assistant turn carried ONLY a tool call (no text) and is
	// not the active tool turn, so its structured toolUses are cleared. That
	// leaves it hollow, and a hollow assistant turn is dropped entirely rather
	// than backfilled with a "." placeholder (which the model would imitate).
	// No surviving turn may contain tool-invocation text or structured toolUses.
	for i, h := range payload.ConversationState.History {
		a := h.AssistantResponseMessage
		if a == nil {
			continue
		}
		if len(a.ToolUses) != 0 {
			t.Fatalf("history[%d] retains structured toolUses", i)
		}
		if strings.Contains(a.Content, "get_weather") || strings.Contains(a.Content, "[Called tool") {
			t.Fatalf("history[%d] assistant contains tool-invocation text: %q", i, a.Content)
		}
		if strings.TrimSpace(a.Content) == "." || strings.TrimSpace(a.Content) == "" {
			t.Fatalf("history[%d] is a hollow assistant turn that should have been dropped", i)
		}
	}
}

func TestOpenAIConversationIDStableFromAnchor(t *testing.T) {
	baseMessages := []OpenAIMessage{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "Build calculator"},
		{Role: "assistant", Content: "Sure"},
		{Role: "user", Content: "Continue"},
	}

	reqA := &OpenAIRequest{Model: "claude-sonnet-4.5", Messages: baseMessages}
	reqB := &OpenAIRequest{Model: "claude-sonnet-4.5", Messages: append(baseMessages, OpenAIMessage{Role: "assistant", Content: "Next step"})}

	payloadA := OpenAIToKiro(reqA, false)
	payloadB := OpenAIToKiro(reqB, false)

	if payloadA.ConversationState.ConversationID == "" || payloadB.ConversationState.ConversationID == "" {
		t.Fatalf("expected non-empty conversation IDs")
	}
	if payloadA.ConversationState.ConversationID != payloadB.ConversationState.ConversationID {
		t.Fatalf("expected stable conversation ID across turns, got %q vs %q", payloadA.ConversationState.ConversationID, payloadB.ConversationState.ConversationID)
	}
}

func TestClaudeConversationIDStableFromAnchor(t *testing.T) {
	reqA := &ClaudeRequest{
		Model:  "claude-sonnet-4.5",
		System: "sys",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "hello"},
		},
	}
	reqB := &ClaudeRequest{
		Model:  "claude-sonnet-4.5",
		System: "sys",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "ok"},
			{Role: "user", Content: "next"},
		},
	}

	payloadA := ClaudeToKiro(reqA, false)
	payloadB := ClaudeToKiro(reqB, false)

	if payloadA.ConversationState.ConversationID == "" || payloadB.ConversationState.ConversationID == "" {
		t.Fatalf("expected non-empty conversation IDs")
	}
	if payloadA.ConversationState.ConversationID != payloadB.ConversationState.ConversationID {
		t.Fatalf("expected stable conversation ID across turns, got %q vs %q", payloadA.ConversationState.ConversationID, payloadB.ConversationState.ConversationID)
	}
}

func TestOpenAIConversationIDRandomForSyntheticAnchor(t *testing.T) {
	req := &OpenAIRequest{
		Model: "claude-sonnet-4.5",
		Messages: []OpenAIMessage{
			{Role: "assistant", Content: "prefill"},
		},
	}

	payloadA := OpenAIToKiro(req, false)
	payloadB := OpenAIToKiro(req, false)

	if payloadA.ConversationState.ConversationID == payloadB.ConversationState.ConversationID {
		t.Fatalf("expected synthetic anchor to generate non-deterministic conversation IDs")
	}
}

func TestClaudeToKiroDropsLeadingAssistantHistory(t *testing.T) {
	req := &ClaudeRequest{
		Model: "claude-sonnet-4.5",
		Messages: []ClaudeMessage{
			{Role: "assistant", Content: "prefill"},
			{Role: "user", Content: "real user message"},
		},
	}

	payload := ClaudeToKiro(req, false)

	if len(payload.ConversationState.History) != 0 {
		t.Fatalf("expected leading assistant-only history to be dropped, got %d entries", len(payload.ConversationState.History))
	}

	if strings.Contains(payload.ConversationState.CurrentMessage.UserInputMessage.Content, "Begin conversation") {
		t.Fatalf("unexpected synthetic Begin conversation injection in current content: %q", payload.ConversationState.CurrentMessage.UserInputMessage.Content)
	}
}

func TestKiroToClaudeResponseCanEmitEmptyThinkingBlock(t *testing.T) {
	resp := KiroToClaudeResponse("final answer", "", true, nil, 10, 20, "claude-sonnet-4.6")

	if len(resp.Content) != 2 {
		t.Fatalf("expected empty thinking block plus text block, got %d blocks", len(resp.Content))
	}
	if resp.Content[0].Type != "thinking" {
		t.Fatalf("expected first block to be thinking, got %#v", resp.Content[0])
	}
	if resp.Content[0].Thinking != "" {
		t.Fatalf("expected omitted thinking block to have empty content, got %#v", resp.Content[0].Thinking)
	}
	if resp.Content[1].Type != "text" || resp.Content[1].Text != "final answer" {
		t.Fatalf("expected text block to be preserved, got %#v", resp.Content[1])
	}
}

func TestToolResultsContinuationIncludesInstructionPrefix(t *testing.T) {
	req := &OpenAIRequest{
		Model: "claude-sonnet-4.5",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "find data"},
			{Role: "assistant", ToolCalls: []ToolCall{{
				ID:   "call_1",
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "fetch", Arguments: "{}"},
			}}},
			{Role: "tool", ToolCallID: "call_1", Content: "result-1"},
		},
	}

	payload := OpenAIToKiro(req, false)
	content := payload.ConversationState.CurrentMessage.UserInputMessage.Content

	if !strings.Contains(content, toolResultsContinuationPrefix) {
		t.Fatalf("expected tool continuation prefix, got %q", content)
	}
	if !strings.Contains(content, "result-1") {
		t.Fatalf("expected tool result text in continuation content, got %q", content)
	}
}

func TestEnsureObjectSchemaRemovesKiroRejectedFieldsRecursively(t *testing.T) {
	input := map[string]interface{}{
		"type":                 "object",
		"required":             []interface{}{},
		"additionalProperties": false,
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":                 "string",
				"required":             nil,
				"additionalProperties": map[string]interface{}{"type": "string"},
			},
			"options": map[string]interface{}{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]interface{}{
					"force": map[string]interface{}{"type": "boolean"},
				},
			},
		},
		"anyOf": []interface{}{
			map[string]interface{}{
				"type":                 "object",
				"required":             []interface{}{},
				"additionalProperties": false,
			},
		},
	}

	got := ensureObjectSchema(input).(map[string]interface{})
	if schemaContainsKey(got, "additionalProperties") {
		t.Fatalf("expected additionalProperties to be removed recursively, got %#v", got)
	}
	if schemaContainsKey(got, "required") {
		t.Fatalf("expected empty/nil required fields to be removed recursively, got %#v", got)
	}
	if _, stillPresent := input["additionalProperties"]; !stillPresent {
		t.Fatalf("expected sanitizer not to mutate caller schema")
	}
}

func TestConvertOpenAIToolsSanitizesSchemaAndDescription(t *testing.T) {
	var tool OpenAITool
	tool.Type = "function"
	tool.Function.Name = "read_file"
	tool.Function.Parameters = map[string]interface{}{
		"type":                 "object",
		"required":             []string{},
		"additionalProperties": false,
	}

	tools := convertOpenAITools([]OpenAITool{tool})
	if len(tools) != 1 {
		t.Fatalf("expected one converted tool, got %d", len(tools))
	}
	if strings.TrimSpace(tools[0].ToolSpecification.Description) == "" {
		t.Fatalf("expected fallback tool description")
	}
	schema := tools[0].ToolSpecification.InputSchema.JSON.(map[string]interface{})
	if schemaContainsKey(schema, "additionalProperties") {
		t.Fatalf("expected OpenAI tool schema to be sanitized, got %#v", schema)
	}
	if schemaContainsKey(schema, "required") {
		t.Fatalf("expected empty required field to be removed, got %#v", schema)
	}
}

func TestConvertClaudeToolsDropsAskUserQuestion(t *testing.T) {
	tools, _ := convertClaudeTools([]ClaudeTool{
		{
			Name:        "AskUserQuestion",
			Description: "Ask the user a question",
			InputSchema: map[string]interface{}{"type": "object"},
		},
		{
			Name:        "Read",
			Description: "Read a file",
			InputSchema: map[string]interface{}{"type": "object"},
		},
	})

	if len(tools) != 1 {
		t.Fatalf("expected one tool after filtering AskUserQuestion, got %d", len(tools))
	}
	if tools[0].ToolSpecification.Name != "read" {
		t.Fatalf("expected Read tool to remain, got %q", tools[0].ToolSpecification.Name)
	}
}

func schemaContainsKey(value interface{}, key string) bool {
	switch v := value.(type) {
	case map[string]interface{}:
		if _, ok := v[key]; ok {
			return true
		}
		for _, child := range v {
			if schemaContainsKey(child, key) {
				return true
			}
		}
	case []interface{}:
		for _, child := range v {
			if schemaContainsKey(child, key) {
				return true
			}
		}
	}
	return false
}

func TestParseModelAndThinking(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantModel    string
		wantThinking bool
	}{
		// Format normalization: dash → dot for new versions without code changes.
		{"new opus dash form", "claude-opus-4-8", "claude-opus-4.8", false},
		{"new opus dot form", "claude-opus-4.8", "claude-opus-4.8", false},
		{"existing opus dash form", "claude-opus-4-7", "claude-opus-4.7", false},
		{"existing opus dot form", "claude-opus-4.7", "claude-opus-4.7", false},
		{"sonnet dash form", "claude-sonnet-4-6", "claude-sonnet-4.6", false},
		{"sonnet dot form", "claude-sonnet-4.6", "claude-sonnet-4.6", false},
		{"haiku dash form", "claude-haiku-4-5", "claude-haiku-4.5", false},
		{"haiku dot form", "claude-haiku-4.5", "claude-haiku-4.5", false},
		{"future major bump", "claude-sonnet-5-0", "claude-sonnet-5.0", false},

		// Bare family name passes through (no minor to normalize).
		{"bare sonnet 4", "claude-sonnet-4", "claude-sonnet-4", false},

		// Dated snapshot must hit the alias before the regex rewrites it.
		{"dated sonnet snapshot", "claude-sonnet-4-20250514", "claude-sonnet-4", false},

		// Cross-family legacy IDs.
		{"claude 3.5 sonnet", "claude-3-5-sonnet", "claude-sonnet-4.5", false},
		{"claude 3 opus", "claude-3-opus", "claude-sonnet-4.5", false},
		{"claude 3 sonnet", "claude-3-sonnet", "claude-sonnet-4", false},
		{"claude 3 haiku", "claude-3-haiku", "claude-haiku-4.5", false},

		// Non-Anthropic fallbacks.
		{"gpt-4-turbo", "gpt-4-turbo", "claude-sonnet-4.5", false},
		{"gpt-4o", "gpt-4o", "claude-sonnet-4.5", false},
		{"gpt-4", "gpt-4", "claude-sonnet-4.5", false},
		{"gpt-3.5-turbo", "gpt-3.5-turbo", "claude-sonnet-4.5", false},

		// Thinking suffix is stripped before mapping.
		{"thinking suffix on dash form", "claude-opus-4-8-thinking", "claude-opus-4.8", true},
		{"thinking suffix on dot form", "claude-sonnet-4.5-thinking", "claude-sonnet-4.5", true},
		{"thinking suffix on legacy alias", "claude-3-5-sonnet-thinking", "claude-sonnet-4.5", true},

		// Unknown models pass through unchanged.
		{"unknown model", "some-other-model", "some-other-model", false},
		{"misspelled claude family", "claude-opux-4-8", "claude-opux-4-8", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotModel, gotThinking := ParseModelAndThinking(tc.input, "-thinking")
			if gotModel != tc.wantModel {
				t.Errorf("model: got %q, want %q", gotModel, tc.wantModel)
			}
			if gotThinking != tc.wantThinking {
				t.Errorf("thinking: got %v, want %v", gotThinking, tc.wantThinking)
			}
		})
	}
}

func TestParseModelAndThinkingDoesNotRewriteDatedSnapshotMinor(t *testing.T) {
	// Guards the \b boundary in claudeVersionPattern: without it, the regex would
	// rewrite "claude-sonnet-4-20250514" to "claude-sonnet-4.20250514" before the
	// alias table could redirect it.
	got, _ := ParseModelAndThinking("claude-sonnet-4-20250514", "-thinking")
	if got != "claude-sonnet-4" {
		t.Fatalf("dated snapshot must alias to claude-sonnet-4, got %q", got)
	}
	if strings.Contains(got, ".") {
		t.Fatalf("dated snapshot must not be rewritten with a dot, got %q", got)
	}
}

func TestClaudeToolResultImageAttachedToCurrentMessage(t *testing.T) {
	const imgData = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Messages: []ClaudeMessage{
			{Role: "user", Content: "read this image"},
			{
				Role: "assistant",
				Content: []interface{}{
					map[string]interface{}{"type": "tool_use", "id": "tool_1", "name": "read", "input": map[string]interface{}{"path": "a.png"}},
				},
			},
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "tool_1",
						"content": []interface{}{
							map[string]interface{}{
								"type": "image",
								"source": map[string]interface{}{
									"type":       "base64",
									"media_type": "image/png",
									"data":       imgData,
								},
							},
						},
					},
				},
			},
		},
	}

	payload := ClaudeToKiro(req, false)
	cur := payload.ConversationState.CurrentMessage.UserInputMessage
	if len(cur.Images) != 1 {
		t.Fatalf("expected tool_result image attached to current message, got %d images", len(cur.Images))
	}
	if cur.Images[0].Format != "png" || cur.Images[0].Source.Bytes != imgData {
		t.Fatalf("unexpected image payload: %+v", cur.Images[0])
	}
	if cur.UserInputMessageContext == nil || len(cur.UserInputMessageContext.ToolResults) != 1 {
		t.Fatalf("expected one tool result preserved")
	}
	if strings.TrimSpace(cur.UserInputMessageContext.ToolResults[0].Content[0].Text) == "" {
		t.Fatalf("expected non-empty placeholder text for image-only tool result")
	}
}

func TestClaudeToolResultMixedTextAndImage(t *testing.T) {
	const imgData = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	req := &ClaudeRequest{
		Model: "claude-opus-4.8",
		Messages: []ClaudeMessage{
			{
				Role: "user",
				Content: []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": "tool_2",
						"content": []interface{}{
							map[string]interface{}{"type": "text", "text": "here is the screenshot"},
							map[string]interface{}{
								"type": "image",
								"source": map[string]interface{}{
									"type":       "base64",
									"media_type": "image/png",
									"data":       imgData,
								},
							},
						},
					},
				},
			},
		},
	}

	payload := ClaudeToKiro(req, false)
	cur := payload.ConversationState.CurrentMessage.UserInputMessage
	if len(cur.Images) != 1 {
		t.Fatalf("expected one image extracted, got %d", len(cur.Images))
	}
	if cur.UserInputMessageContext == nil || len(cur.UserInputMessageContext.ToolResults) != 1 {
		t.Fatalf("expected one tool result")
	}
	gotText := cur.UserInputMessageContext.ToolResults[0].Content[0].Text
	if gotText != "here is the screenshot" {
		t.Fatalf("expected original tool text preserved, got %q", gotText)
	}
}

func TestOpenAIToolResultImageAttachedToCurrentMessage(t *testing.T) {
	const dataURL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	req := &OpenAIRequest{
		Model: "claude-sonnet-4.5",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "look at the file"},
			{
				Role:       "tool",
				ToolCallID: "call_img",
				Content: []interface{}{
					map[string]interface{}{
						"type":      "image_url",
						"image_url": map[string]interface{}{"url": dataURL},
					},
				},
			},
		},
	}

	payload := OpenAIToKiro(req, false)
	cur := payload.ConversationState.CurrentMessage.UserInputMessage
	if len(cur.Images) != 1 {
		t.Fatalf("expected tool image attached to current message, got %d", len(cur.Images))
	}
	if cur.Images[0].Format != "png" {
		t.Fatalf("expected png format, got %q", cur.Images[0].Format)
	}
}

func TestOpenAIToolResultImageCarriedWhenFollowedByUser(t *testing.T) {
	const dataURL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="
	req := &OpenAIRequest{
		Model: "claude-sonnet-4.5",
		Messages: []OpenAIMessage{
			{Role: "user", Content: "look at the file"},
			{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{
						ID:   "call_img",
						Type: "function",
						Function: struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						}{Name: "read", Arguments: `{"path":"a.png"}`},
					},
				},
			},
			{
				Role:       "tool",
				ToolCallID: "call_img",
				Content: []interface{}{
					map[string]interface{}{"type": "image_url", "image_url": map[string]interface{}{"url": dataURL}},
				},
			},
			{Role: "user", Content: "what do you see?"},
		},
	}

	payload := OpenAIToKiro(req, false)

	var toolHistImages int
	var structuredToolResults int
	for _, h := range payload.ConversationState.History {
		if h.UserInputMessage != nil {
			toolHistImages += len(h.UserInputMessage.Images)
			if h.UserInputMessage.UserInputMessageContext != nil {
				structuredToolResults += len(h.UserInputMessage.UserInputMessageContext.ToolResults)
			}
		}
	}
	if toolHistImages != 1 {
		t.Fatalf("expected tool image carried on the flushed tool-result history entry, got %d", toolHistImages)
	}
	if structuredToolResults != 0 {
		t.Fatalf("history tool results should be narrated, not kept structured; got %d", structuredToolResults)
	}

	cur := payload.ConversationState.CurrentMessage.UserInputMessage
	if len(cur.Images) != 0 {
		t.Fatalf("tool image should not leak into a later user message, got %d on current", len(cur.Images))
	}
}
