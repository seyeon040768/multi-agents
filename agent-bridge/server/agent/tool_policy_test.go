package agent

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestToolPolicyValidation(t *testing.T) {
	for _, tc := range []struct {
		tools   AgentTools
		invalid bool
	}{
		{AgentTools{Allowed: []string{"debug-echo"}}, false},
		{AgentTools{Allowed: []string{"debug-echo"}, RequireConfirmation: []string{"debug-echo"}}, false},
		{AgentTools{Allowed: []string{"debug-echo"}, Denied: []string{"debug-echo"}}, true},
		{AgentTools{Denied: []string{"debug-echo"}, RequireConfirmation: []string{"debug-echo"}}, true},
		{AgentTools{RequireConfirmation: []string{"debug-echo"}}, true},
	} {
		a := DefaultAgent()
		a.ID = "researcher"
		a.Name = "Researcher"
		a.Prompts.Identity = "identity"
		a.Model.Provider = "google"
		a.Model.Name = "gemini-3.1-flash-lite"
		a.Tools = tc.tools
		err := Validate(&a)
		if tc.invalid {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
}
