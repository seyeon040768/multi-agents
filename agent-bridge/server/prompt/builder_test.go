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
	for _, text := range []string{"identity", "Role:\nresearcher", "Task instructions:\ntask", "Communication instructions:\nconcise", "Output instructions:\nmarkdown", "Context instructions:\ncontext", "Tools, delegation"} {
		require.Contains(t, s, text)
	}
}
