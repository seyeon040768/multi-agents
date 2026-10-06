package agent

import (
	"regexp"
	"strings"
)

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{2,31}$`)

// Keep this catalog aligned with webapp/src/models.ts; it does not invoke providers.
var modelCatalog = map[string]map[string]bool{
	"openai":    {"gpt-5.4": true},
	"anthropic": {"claude-sonnet-4-6": true},
	"google":    {"gemini-3-flash-preview": true, "gemini-3.1-flash-lite": true},
	"ollama":    {"qwen3:8b": true},
}

func ValidateID(id string) error {
	if !idPattern.MatchString(id) {
		return &ValidationError{"INVALID_AGENT_ID", "agent ID must start with a lowercase letter and contain 3–32 lowercase letters, digits, dots, underscores or hyphens"}
	}
	return nil
}
func Validate(a *Agent) error {
	if err := ValidateID(a.ID); err != nil {
		return err
	}
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" {
		return &ValidationError{"INVALID_AGENT_NAME", "agent name is required"}
	}
	if strings.TrimSpace(a.Prompts.Identity) == "" {
		return &ValidationError{"INVALID_PROMPT", "identity prompt is required"}
	}
	denied := map[string]bool{}
	allowed := map[string]bool{}
	for _, id := range a.Tools.Denied {
		denied[id] = true
	}
	for _, id := range a.Tools.Allowed {
		if denied[id] {
			return &ValidationError{"INVALID_TOOL_POLICY", "allowed and denied tools must not overlap"}
		}
		allowed[id] = true
	}
	for _, id := range a.Tools.RequireConfirmation {
		if denied[id] || !allowed[id] {
			return &ValidationError{"INVALID_TOOL_POLICY", "confirmation tools must be allowed and must not be denied"}
		}
	}
	if !modelCatalog[a.Model.Provider][a.Model.Name] {
		return &ValidationError{"INVALID_MODEL", "select a registered provider and model"}
	}
	return nil
}
