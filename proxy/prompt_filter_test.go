package proxy

import (
	"path/filepath"
	"strings"
	"testing"

	"kiro-go/config"
)

func initPromptFilterTestConfig(t *testing.T, filterClaudeCode, filterEnvNoise, filterStripBoundaries bool) {
	t.Helper()
	if err := config.Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("config init: %v", err)
	}
	if err := config.UpdatePromptFilterConfig(filterClaudeCode, filterEnvNoise, filterStripBoundaries, nil); err != nil {
		t.Fatalf("update prompt filter config: %v", err)
	}
}

func TestApplyPromptFiltersReplacesClaudeCodeSystemPrompt(t *testing.T) {
	initPromptFilterTestConfig(t, true, true, true)

	in := `You are an interactive agent that helps users with software engineering tasks.
# Doing tasks
# Using your tools
gitStatus: dirty
Keep this giant CLI prompt.`

	got := applyPromptFilters(in)

	if got != claudeCodeBackendPrompt {
		t.Fatalf("expected Claude Code prompt replacement, got:\n%s", got)
	}
	if strings.Contains(got, "gitStatus:") || strings.Contains(got, "giant CLI prompt") {
		t.Fatalf("expected noisy original prompt to be replaced, got:\n%s", got)
	}
}

func TestApplyPromptFiltersStripsBoundariesAndEnvNoise(t *testing.T) {
	initPromptFilterTestConfig(t, false, true, true)

	in := `--- SYSTEM PROMPT ---
Keep this instruction.
# Environment
gitStatus: dirty
Recent commits: abc123
This line is environment noise.
# Task
Do the useful thing.
--- END SYSTEM PROMPT ---`

	got := applyPromptFilters(in)

	for _, forbidden := range []string{
		"--- SYSTEM PROMPT ---",
		"--- END SYSTEM PROMPT ---",
		"gitStatus:",
		"Recent commits:",
		"This line is environment noise.",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("expected %q to be stripped, got:\n%s", forbidden, got)
		}
	}
	if !strings.Contains(got, "Keep this instruction.") || !strings.Contains(got, "# Task") {
		t.Fatalf("expected useful prompt content to remain, got:\n%s", got)
	}
}

func TestApplyPromptFiltersPreservesProjectRootFromEnvNoise(t *testing.T) {
	initPromptFilterTestConfig(t, false, true, true)

	in := `Keep this instruction.
# Environment
cwd: D:\agent_prac\nanocodex
gitStatus: dirty
This line is environment noise.
# Task
Do the useful thing.`

	got := applyPromptFilters(in)

	if !strings.Contains(got, `Current project root: D:\agent_prac\nanocodex`) {
		t.Fatalf("expected project root to survive env filtering, got:\n%s", got)
	}
	if strings.Contains(got, "gitStatus:") || strings.Contains(got, "This line is environment noise.") {
		t.Fatalf("expected env noise to be stripped, got:\n%s", got)
	}
}

func TestApplyPromptFiltersPreservesProjectRootWhenReplacingClaudeCodePrompt(t *testing.T) {
	initPromptFilterTestConfig(t, true, true, true)

	in := `You are an interactive agent that helps users with software engineering tasks.
# Doing tasks
# Using your tools
# Environment
cwd: D:\agent_prac\nanocodex
gitStatus: dirty`

	got := applyPromptFilters(in)

	if !strings.Contains(got, claudeCodeBackendPrompt) {
		t.Fatalf("expected Claude Code backend prompt replacement, got:\n%s", got)
	}
	if !strings.Contains(got, `Current project root: D:\agent_prac\nanocodex`) {
		t.Fatalf("expected project root to survive Claude Code prompt replacement, got:\n%s", got)
	}
	if strings.Contains(got, "gitStatus:") {
		t.Fatalf("expected noisy original prompt to be replaced, got:\n%s", got)
	}
}

func TestApplyPromptFiltersConfiguredCompactProjectRootDoesNotOverridePromptRoot(t *testing.T) {
	initPromptFilterTestConfig(t, false, true, true)
	t.Setenv("KIRO_GO_CLAUDE_NATIVE_COMPACT_PROJECT_DIR", `D:\agent_prac\nanocodex`)

	in := `Keep this instruction.
Project root: D:\agent_prac
# Environment
cwd: D:\agent_prac
gitStatus: dirty
# Task
Do the useful thing.`

	got := applyPromptFilters(in)

	if !strings.Contains(got, `Current project root: D:\agent_prac`) {
		t.Fatalf("expected prompt project root to survive, got:\n%s", got)
	}
	if strings.Contains(got, `Current project root: D:\agent_prac\nanocodex`) {
		t.Fatalf("compact project dir must not override live prompt root, got:\n%s", got)
	}
}

func TestApplyPromptFiltersCanBeDisabled(t *testing.T) {
	initPromptFilterTestConfig(t, false, false, false)

	in := "--- SYSTEM PROMPT ---\ngitStatus: dirty\nKeep this instruction."
	got := applyPromptFilters(in)

	if !strings.Contains(got, "--- SYSTEM PROMPT ---") || !strings.Contains(got, "gitStatus: dirty") {
		t.Fatalf("expected disabled filters to preserve input, got:\n%s", got)
	}
}
