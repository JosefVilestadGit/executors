package executor

import (
	"testing"

	"github.com/colonyos/colonies/pkg/core"
	"github.com/stretchr/testify/assert"
)

func TestExtractForceFlag(t *testing.T) {
	tests := []struct {
		name     string
		kwargs   map[string]interface{}
		expected bool
	}{
		{
			name:     "force flag true",
			kwargs:   map[string]interface{}{"kind": "ExecutorDeployment", "force": true},
			expected: true,
		},
		{
			name:     "force flag false",
			kwargs:   map[string]interface{}{"kind": "ExecutorDeployment", "force": false},
			expected: false,
		},
		{
			name:     "force flag missing",
			kwargs:   map[string]interface{}{"kind": "ExecutorDeployment"},
			expected: false,
		},
		{
			name:     "force flag wrong type (string)",
			kwargs:   map[string]interface{}{"kind": "ExecutorDeployment", "force": "true"},
			expected: false,
		},
		{
			name:     "force flag wrong type (int)",
			kwargs:   map[string]interface{}{"kind": "ExecutorDeployment", "force": 1},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Extract force flag the same way it's done in handleConsolidatedReconcile
			force := false
			if forceVal, ok := tt.kwargs["force"].(bool); ok {
				force = forceVal
			}
			assert.Equal(t, tt.expected, force)
		})
	}
}

func TestProcessKwargsExtraction(t *testing.T) {
	// Test extracting kwargs from a process
	process := &core.Process{
		FunctionSpec: core.FunctionSpec{
			FuncName: "reconcile",
			KwArgs: map[string]interface{}{
				"kind":  "ExecutorDeployment",
				"force": true,
			},
		},
	}

	// Extract kind
	kind, kindOk := process.FunctionSpec.KwArgs["kind"].(string)
	assert.True(t, kindOk)
	assert.Equal(t, "ExecutorDeployment", kind)

	// Extract force
	force, forceOk := process.FunctionSpec.KwArgs["force"].(bool)
	assert.True(t, forceOk)
	assert.True(t, force)
}

func TestProcessKwargsWithoutForce(t *testing.T) {
	// Test extracting kwargs from a process without force flag
	process := &core.Process{
		FunctionSpec: core.FunctionSpec{
			FuncName: "reconcile",
			KwArgs: map[string]interface{}{
				"kind": "DockerDeployment",
			},
		},
	}

	// Extract kind
	kind, kindOk := process.FunctionSpec.KwArgs["kind"].(string)
	assert.True(t, kindOk)
	assert.Equal(t, "DockerDeployment", kind)

	// Force should not be present
	_, forceOk := process.FunctionSpec.KwArgs["force"].(bool)
	assert.False(t, forceOk)

	// Default force to false when not present
	force := false
	if forceVal, ok := process.FunctionSpec.KwArgs["force"].(bool); ok {
		force = forceVal
	}
	assert.False(t, force)
}
