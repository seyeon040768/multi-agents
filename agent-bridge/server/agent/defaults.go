package agent

import (
	_ "embed"
	"encoding/json"
)

// defaultJSON follows the initial settings in webapp/src/defaults.ts.
//
//go:embed defaults.json
var defaultJSON []byte

// DefaultAgent returns an independent copy of the initial configuration.
func DefaultAgent() Agent {
	var a Agent
	if err := json.Unmarshal(defaultJSON, &a); err != nil {
		panic(err)
	}
	return a
}
