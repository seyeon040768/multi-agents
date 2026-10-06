package prompt

import (
	"github.com/seyeon/agent-bridge/server/agent"
	"strings"
)

type Builder struct{}

func (Builder) BuildSystemPrompt(a *agent.Agent) string {
	sections := []string{strings.TrimSpace(a.Prompts.Identity)}
	for _, s := range []struct{ title, text string }{
		{"Role", a.Role.Title}, {"Task instructions", a.Prompts.TaskInstruction},
		{"Reasoning instructions", a.Prompts.ReasoningInstruction},
		{"Collaboration instructions", a.Prompts.CollaborationInstruction},
		{"Communication instructions", a.Prompts.CommunicationInstruction},
		{"Output instructions", a.Prompts.OutputInstruction}, {"Context instructions", a.Context.Instructions},
		{"Output format", a.Output.Format}, {"Output language", a.Output.Language},
	} {
		if strings.TrimSpace(s.text) != "" {
			sections = append(sections, s.title+":\n"+strings.TrimSpace(s.text))
		}
	}
	sections = append(sections, "Only respond with text. Tools, delegation, external research and persistent memory are unavailable. Do not claim to have performed these actions.")
	return strings.Join(sections, "\n\n")
}
