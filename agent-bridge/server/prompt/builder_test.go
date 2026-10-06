package prompt

import (
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPromptSections(t *testing.T) {
	a := agent.DefaultAgent()
	a.Prompts.Identity = "identity"
	a.Role.Title = "researcher"
	a.Prompts.TaskInstruction = "task"
	a.Prompts.CommunicationInstruction = "concise"
	a.Prompts.OutputInstruction = "markdown"
	a.Context.Instructions = "context"
	s := (Builder{}).BuildSystemPrompt(&a)
	for _, text := range []string{"identity", "Role:\nresearcher", "Task instructions:\ntask", "Communication instructions:\nconcise", "Output instructions:\nmarkdown", "Context instructions:\ncontext", "Use only tools supplied by the runtime", "Current date (UTC):"} {
		require.Contains(t, s, text)
	}
}

func TestPromptDoesNotProhibitRegisteredTools(t *testing.T) {
	a := agent.DefaultAgent()
	a.Tools = agent.AgentTools{Enabled: true, Allowed: []string{"web-search"}}
	prompt := (Builder{}).BuildSystemPrompt(&a)
	require.NotContains(t, prompt, "Tools, delegation, external research and persistent memory are unavailable")
	require.Contains(t, prompt, "use an available appropriate tool before answering")
	require.Contains(t, prompt, "without a successful tool result")
}
