package security

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	t "github.com/marsstein/liteclaw/internal/types"
)

// SafetyChecker validates tool calls and outputs.
type SafetyChecker struct {
	toolDefs       map[string]t.ToolDef
	approvalFn     t.ApprovalFunc
	allowedDirs    []string
	strictApproval bool
	scanCreds      bool
	pathGuard      bool
}

// SafetyConfig configures the safety checker.
type SafetyConfig struct {
	StrictApproval     bool
	ScanCredentials    bool
	PathTraversalGuard bool
	AllowedDirs        []string
}

// NewSafetyChecker creates a safety checker from tool definitions.
func NewSafetyChecker(cfg SafetyConfig, tools []t.ToolDef, approvalFn t.ApprovalFunc) *SafetyChecker {
	defs := make(map[string]t.ToolDef, len(tools))
	for _, tool := range tools {
		defs[tool.Name] = tool
	}
	return &SafetyChecker{
		toolDefs:       defs,
		approvalFn:     approvalFn,
		allowedDirs:    cfg.AllowedDirs,
		strictApproval: cfg.StrictApproval,
		scanCreds:      cfg.ScanCredentials,
		pathGuard:      cfg.PathTraversalGuard,
	}
}

// SafetyError is returned when a safety check fails.
type SafetyError struct {
	Code    string
	Message string
}

func (e *SafetyError) Error() string {
	return fmt.Sprintf("safety/%s: %s", e.Code, e.Message)
}

// IsDenied returns true if this was a human denial.
func (e *SafetyError) IsDenied() bool {
	return e.Code == "human_denied"
}

// ValidateToolCall checks that the call is safe to execute.
func (sc *SafetyChecker) ValidateToolCall(call t.ToolCall) error {
	def, ok := sc.toolDefs[call.Name]
	if !ok {
		return &SafetyError{
			Code:    "unknown_tool",
			Message: fmt.Sprintf("tool %q is not registered", call.Name),
		}
	}

	if len(call.Arguments) > 0 {
		var parsed map[string]any
		if err := json.Unmarshal(call.Arguments, &parsed); err != nil {
			return &SafetyError{
				Code:    "invalid_args",
				Message: fmt.Sprintf("arguments for %q are not valid JSON: %v", call.Name, err),
			}
		}

		if sc.pathGuard {
			if err := sc.checkPathTraversal(parsed); err != nil {
				return err
			}
		}
	}

	if sc.approvalFn != nil {
		needsApproval := def.DangerLevel == t.DangerHigh ||
			(def.DangerLevel == t.DangerMedium && sc.strictApproval)

		if needsApproval {
			reason := fmt.Sprintf("tool %q has danger level %d", call.Name, def.DangerLevel)
			if !sc.approvalFn(call, reason) {
				return &SafetyError{
					Code:    "human_denied",
					Message: fmt.Sprintf("user denied execution of %q", call.Name),
				}
			}
		}
	}

	return nil
}

func (sc *SafetyChecker) checkPathTraversal(args map[string]any) error {
	if len(sc.allowedDirs) == 0 {
		return nil
	}

	for key, val := range args {
		strVal, ok := val.(string)
		if !ok || !looksLikePath(strVal) {
			continue
		}

		cleaned := filepath.Clean(strVal)

		if strings.Contains(cleaned, "..") {
			return &SafetyError{
				Code:    "path_traversal",
				Message: fmt.Sprintf("parameter %q contains path traversal: %q", key, strVal),
			}
		}

		allowed := false
		for _, dir := range sc.allowedDirs {
			cleanDir := filepath.Clean(dir)
			if cleaned == cleanDir || strings.HasPrefix(cleaned, cleanDir+string(filepath.Separator)) {
				allowed = true
				break
			}
		}
		if !allowed {
			return &SafetyError{
				Code:    "path_traversal",
				Message: fmt.Sprintf("parameter %q path %q is outside allowed directories", key, strVal),
			}
		}
	}

	return nil
}

func looksLikePath(s string) bool {
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return true
	}
	return strings.Contains(s, "/") && !strings.Contains(s, "://")
}

var credentialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(aws_secret_access_key|aws_access_key_id)\s*[=:]\s*\S+`),
	regexp.MustCompile(`(?i)(api[_-]?key|api[_-]?secret|access[_-]?token|auth[_-]?token)\s*[=:]\s*["']?\S{20,}`),
	regexp.MustCompile(`(?i)-----BEGIN\s+(RSA\s+)?PRIVATE\s+KEY-----`),
	regexp.MustCompile(`ghp_[0-9a-zA-Z]{36}`),
	regexp.MustCompile(`sk-[0-9a-zA-Z]{40,}`),
	regexp.MustCompile(`(?i)password\s*[=:]\s*["']?\S{8,}`),
}

// ScanCredentials checks tool output for leaked credentials.
func (sc *SafetyChecker) ScanCredentials(content string) (string, bool) {
	if !sc.scanCreds {
		return content, false
	}

	found := false
	redacted := content
	for _, pat := range credentialPatterns {
		if pat.MatchString(redacted) {
			found = true
			redacted = pat.ReplaceAllString(redacted, "[REDACTED_CREDENTIAL]")
		}
	}

	return redacted, found
}
