package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExecutorOptions(t *testing.T) {
	tests := []struct {
		name     string
		options  []ExecutorOption
		validate func(*testing.T, *Executor)
	}{
		{
			name: "WithVerbose sets verbose flag",
			options: []ExecutorOption{
				WithVerbose(true),
			},
			validate: func(t *testing.T, e *Executor) {
				assert.True(t, e.verbose, "verbose should be true")
			},
		},
		{
			name: "WithColoniesServerHost sets server host",
			options: []ExecutorOption{
				WithColoniesServerHost("test-host"),
			},
			validate: func(t *testing.T, e *Executor) {
				assert.Equal(t, "test-host", e.coloniesServerHost, "server host should be set")
			},
		},
		{
			name: "WithColoniesServerPort sets server port",
			options: []ExecutorOption{
				WithColoniesServerPort(8080),
			},
			validate: func(t *testing.T, e *Executor) {
				assert.Equal(t, 8080, e.coloniesServerPort, "server port should be set")
			},
		},
		{
			name: "WithColoniesInsecure sets insecure flag",
			options: []ExecutorOption{
				WithColoniesInsecure(true),
			},
			validate: func(t *testing.T, e *Executor) {
				assert.True(t, e.coloniesInsecure, "insecure should be true")
			},
		},
		{
			name: "WithColonyName sets colony name",
			options: []ExecutorOption{
				WithColonyName("test-colony"),
			},
			validate: func(t *testing.T, e *Executor) {
				assert.Equal(t, "test-colony", e.colonyName, "colony name should be set")
			},
		},
		{
			name: "WithColonyPrvKey sets colony private key",
			options: []ExecutorOption{
				WithColonyPrvKey("test-prv-key"),
			},
			validate: func(t *testing.T, e *Executor) {
				assert.Equal(t, "test-prv-key", e.colonyPrvKey, "colony private key should be set")
			},
		},
		{
			name: "WithExecutorName sets executor name",
			options: []ExecutorOption{
				WithExecutorName("test-executor"),
			},
			validate: func(t *testing.T, e *Executor) {
				assert.Equal(t, "test-executor", e.executorName, "executor name should be set")
			},
		},
		{
			name: "WithExecutorPrvKey sets executor private key",
			options: []ExecutorOption{
				WithExecutorPrvKey("test-exec-prv-key"),
			},
			validate: func(t *testing.T, e *Executor) {
				assert.Equal(t, "test-exec-prv-key", e.executorPrvKey, "executor private key should be set")
			},
		},
		{
			name: "WithExecutorType sets executor type",
			options: []ExecutorOption{
				WithExecutorType("deployment-controller"),
			},
			validate: func(t *testing.T, e *Executor) {
				assert.Equal(t, "deployment-controller", e.executorType, "executor type should be set")
			},
		},
		{
			name: "Multiple options applied correctly",
			options: []ExecutorOption{
				WithVerbose(true),
				WithColoniesServerHost("multi-host"),
				WithColoniesServerPort(9090),
				WithColonyName("multi-colony"),
				WithExecutorName("multi-executor"),
				WithExecutorType("multi-type"),
			},
			validate: func(t *testing.T, e *Executor) {
				assert.True(t, e.verbose)
				assert.Equal(t, "multi-host", e.coloniesServerHost)
				assert.Equal(t, 9090, e.coloniesServerPort)
				assert.Equal(t, "multi-colony", e.colonyName)
				assert.Equal(t, "multi-executor", e.executorName)
				assert.Equal(t, "multi-type", e.executorType)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &Executor{}
			for _, opt := range tt.options {
				opt(e)
			}
			tt.validate(t, e)
		})
	}
}

func TestExecutorOptionsDefaults(t *testing.T) {
	e := &Executor{}

	// Test default values
	assert.False(t, e.verbose, "verbose should default to false")
	assert.Equal(t, "", e.coloniesServerHost, "server host should default to empty")
	assert.Equal(t, 0, e.coloniesServerPort, "server port should default to 0")
	assert.False(t, e.coloniesInsecure, "insecure should default to false")
	assert.Equal(t, "", e.colonyName, "colony name should default to empty")
	assert.Equal(t, "", e.colonyPrvKey, "colony private key should default to empty")
	assert.Equal(t, "", e.executorName, "executor name should default to empty")
	assert.Equal(t, "", e.executorPrvKey, "executor private key should default to empty")
	assert.Equal(t, "", e.executorType, "executor type should default to empty")
}
