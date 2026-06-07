package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"kiro-go/config"
	"kiro-go/logger"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const claudeNativeCompactTailBytes = 512 * 1024
const claudeTranscriptCwdScanLines = 200
const claudeTranscriptCwdMaxLineBytes = 2 * 1024 * 1024

type claudeNativeCompactCoordination struct {
	Enabled       bool
	Triggered     bool
	InFlight      bool
	RecentSuccess bool
}

func (s claudeNativeCompactCoordination) payloadTruncationOptions() payloadTruncationOptions {
	if !s.Enabled || (!s.Triggered && !s.InFlight && !s.RecentSuccess) {
		return payloadTruncationOptions{}
	}
	reasons := make([]string, 0, 3)
	if s.Triggered {
		reasons = append(reasons, "native compact triggered")
	}
	if s.InFlight {
		reasons = append(reasons, "native compact in flight")
	}
	if s.RecentSuccess {
		reasons = append(reasons, "native compact cooldown")
	}
	return payloadTruncationOptions{
		RequestOnlySummary: true,
		Reason:             strings.Join(reasons, ", "),
	}
}

func (h *Handler) maybeTriggerClaudeNativeCompact(req *ClaudeRequest, rawBodyBytes int, estimatedTokens int) claudeNativeCompactCoordination {
	cfg := config.GetClaudeNativeCompactConfig()
	status := claudeNativeCompactCoordination{Enabled: cfg.Enabled}
	if !cfg.Enabled {
		return status
	}
	if isClaudeNativeCompactRequest(req) {
		return status
	}

	bodyTooLarge := cfg.BodyThresholdKB > 0 && rawBodyBytes >= cfg.BodyThresholdKB*1024
	tokensTooLarge := cfg.TokenThreshold > 0 && estimatedTokens >= cfg.TokenThreshold

	h.compactMu.Lock()
	cooldown := time.Duration(cfg.CooldownSeconds) * time.Second
	status.InFlight = h.compactInFlight
	status.RecentSuccess = !h.lastCompactAt.IsZero() && cooldown > 0 && time.Since(h.lastCompactAt) < cooldown
	if !bodyTooLarge && !tokensTooLarge {
		h.compactMu.Unlock()
		return status
	}
	if h.compactInFlight {
		h.compactMu.Unlock()
		logger.Infof("[ClaudeCompact] skip: already running")
		return status
	}
	if status.RecentSuccess {
		remaining := cooldown - time.Since(h.lastCompactAt)
		h.compactMu.Unlock()
		logger.Infof("[ClaudeCompact] skip: cooldown remaining=%s", remaining.Round(time.Second))
		return status
	}
	h.compactInFlight = true
	status.Triggered = true
	status.InFlight = true
	h.compactMu.Unlock()

	logger.Infof("[ClaudeCompact] trigger body=%d tokens=%d bodyThresholdKB=%d tokenThreshold=%d", rawBodyBytes, estimatedTokens, cfg.BodyThresholdKB, cfg.TokenThreshold)
	go func() {
		started := time.Now()
		err := runClaudeNativeCompact(cfg)
		h.compactMu.Lock()
		h.compactInFlight = false
		if err == nil {
			h.lastCompactAt = time.Now()
		}
		h.compactMu.Unlock()

		if err != nil {
			logger.Warnf("[ClaudeCompact] failed after=%s body=%d tokens=%d err=%v", time.Since(started).Round(time.Millisecond), rawBodyBytes, estimatedTokens, err)
			return
		}
		logger.Infof("[ClaudeCompact] done after=%s body=%d tokens=%d", time.Since(started).Round(time.Millisecond), rawBodyBytes, estimatedTokens)
	}()
	return status
}

func isClaudeNativeCompactRequest(req *ClaudeRequest) bool {
	if req == nil || len(req.Messages) == 0 {
		return false
	}
	last := req.Messages[len(req.Messages)-1]
	text, _, _ := extractClaudeUserContent(last.Content)
	text = strings.ToLower(text)
	return strings.Contains(text, "<command-name>/compact</command-name>") ||
		strings.Contains(text, "/compact") && strings.Contains(text, "command-message")
}

func runClaudeNativeCompact(cfg config.ClaudeNativeCompactConfig) error {
	sessionFile, err := resolveClaudeSessionFile(cfg)
	if err != nil {
		return err
	}
	sessionID := strings.TrimSuffix(filepath.Base(sessionFile), filepath.Ext(sessionFile))
	projectDir := claudeCompactProjectDirForSession(cfg, sessionFile)
	if projectDir == "" {
		projectDir = "."
	}
	if _, err := os.Stat(projectDir); err != nil {
		return fmt.Errorf("project dir %q: %w", projectDir, err)
	}
	beforeInfo, err := os.Stat(sessionFile)
	if err != nil {
		return fmt.Errorf("stat transcript: %w", err)
	}
	beforeSize := beforeInfo.Size()

	backup := fmt.Sprintf("%s.before-kiro-go-native-compact-%s.bak", sessionFile, time.Now().Format("20060102-150405"))
	if err := copyFile(sessionFile, backup); err != nil {
		return fmt.Errorf("backup transcript: %w", err)
	}

	maxOutput := cfg.MaxOutputTokens
	if maxOutput <= 0 {
		maxOutput = 64000
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		script := fmt.Sprintf(
			"$env:CLAUDE_CODE_MAX_OUTPUT_TOKENS=%s; claude -p --resume %s '/compact'",
			powerShellQuote(fmt.Sprintf("%d", maxOutput)),
			powerShellQuote(sessionID),
		)
		cmd = exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	} else {
		cmd = exec.Command("claude", "-p", "--resume", sessionID, "/compact")
		cmd.Env = withEnvOverride(os.Environ(), "CLAUDE_CODE_MAX_OUTPUT_TOKENS", fmt.Sprintf("%d", maxOutput))
	}
	cmd.Dir = projectDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude compact command failed: %w output=%s", err, compactLogSnippet(string(out), 1200))
	}
	if ok, reason := transcriptAppendHasCompactSuccess(sessionFile, beforeSize); !ok {
		if reason != "" {
			return fmt.Errorf("compact command returned but transcript verification failed: %s; output=%s", reason, compactLogSnippet(string(out), 1200))
		}
		return fmt.Errorf("compact command returned but transcript did not verify success; output=%s", compactLogSnippet(string(out), 1200))
	}

	logger.Infof("[ClaudeCompact] verified session=%s backup=%s", sessionID, backup)
	return nil
}

func claudeCompactProjectDirForSession(cfg config.ClaudeNativeCompactConfig, sessionFile string) string {
	inferred := inferClaudeProjectDir(sessionFile)
	configured := strings.TrimSpace(cfg.ProjectDir)
	if strings.TrimSpace(cfg.SessionID) == "" && inferred != "" {
		return inferred
	}
	if configured != "" {
		return configured
	}
	return inferred
}

func resolveClaudeSessionFile(cfg config.ClaudeNativeCompactConfig) (string, error) {
	root := strings.TrimSpace(cfg.ProjectsRoot)
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".claude", "projects")
	}
	if cfg.SessionID != "" {
		target := cfg.SessionID + ".jsonl"
		var matches []string
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() && filepath.Base(path) == target {
				matches = append(matches, path)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		if len(matches) == 0 {
			return "", fmt.Errorf("claude session %q not found under %s", cfg.SessionID, root)
		}
		if len(matches) > 1 {
			return "", fmt.Errorf("claude session %q matched multiple files", cfg.SessionID)
		}
		return matches[0], nil
	}

	var newest string
	var newestTime time.Time
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if newest == "" || info.ModTime().After(newestTime) {
			newest = path
			newestTime = info.ModTime()
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if newest == "" {
		return "", fmt.Errorf("no Claude JSONL sessions found under %s", root)
	}
	return newest, nil
}

func inferClaudeProjectDir(sessionFile string) string {
	if cwd := readClaudeTranscriptCwd(sessionFile); cwd != "" {
		return cwd
	}

	dir := filepath.Dir(sessionFile)
	base := filepath.Base(dir)
	if strings.HasPrefix(base, "D--") && runtime.GOOS == "windows" {
		return existingDirOrEmpty("D:\\" + strings.ReplaceAll(strings.TrimPrefix(base, "D--"), "-", "\\"))
	}
	if strings.HasPrefix(base, "C--") && runtime.GOOS == "windows" {
		return existingDirOrEmpty("C:\\" + strings.ReplaceAll(strings.TrimPrefix(base, "C--"), "-", "\\"))
	}
	return ""
}

func readClaudeTranscriptCwd(sessionFile string) string {
	f, err := os.Open(sessionFile)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), claudeTranscriptCwdMaxLineBytes)
	for lines := 0; scanner.Scan() && lines < claudeTranscriptCwdScanLines; lines++ {
		if cwd := extractClaudeTranscriptCwdLine(scanner.Bytes()); cwd != "" {
			return cwd
		}
	}
	return ""
}

func extractClaudeTranscriptCwdLine(line []byte) string {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(line, &obj); err != nil {
		return ""
	}
	raw, ok := obj["cwd"]
	if !ok {
		return ""
	}
	var cwd string
	if err := json.Unmarshal(raw, &cwd); err != nil {
		return ""
	}
	return strings.TrimSpace(cwd)
}

func existingDirOrEmpty(path string) string {
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return ""
	}
	return path
}

func transcriptAppendHasCompactSuccess(path string, offset int64) (bool, string) {
	data, err := readFromOffset(path, offset, claudeNativeCompactTailBytes)
	if err != nil {
		return false, err.Error()
	}
	appended := string(data)
	if strings.Contains(appended, "Error during compaction") {
		return false, compactLogSnippet(extractCompactionError(appended), 500)
	}
	if strings.Contains(appended, `"subtype":"compact_boundary"`) ||
		strings.Contains(appended, `"isCompactSummary":true`) ||
		strings.Contains(appended, "Compacted (ctrl+o to see full summary)") {
		return true, ""
	}
	return false, "no compact success marker in appended transcript"
}

func readFromOffset(path string, offset int64, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	if offset > info.Size() {
		offset = info.Size()
	}
	if maxBytes > 0 && info.Size()-offset > maxBytes {
		offset = info.Size() - maxBytes
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(f)
}

func extractCompactionError(s string) string {
	idx := strings.LastIndex(s, "Error during compaction")
	if idx < 0 {
		return ""
	}
	return s[idx:]
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func withEnvOverride(env []string, key, value string) []string {
	prefix := strings.ToUpper(key) + "="
	out := make([]string, 0, len(env)+1)
	found := false
	for _, entry := range env {
		if strings.HasPrefix(strings.ToUpper(entry), prefix) {
			if !found {
				out = append(out, key+"="+value)
				found = true
			}
			continue
		}
		out = append(out, entry)
	}
	if !found {
		out = append(out, key+"="+value)
	}
	return out
}

func powerShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func compactLogSnippet(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
