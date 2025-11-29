package executor

import (
	"runtime"
	"testing"

	"github.com/colonyos/colonies/pkg/core"
	"github.com/stretchr/testify/assert"
)

func TestPopulateExecutorCapabilities(t *testing.T) {
	executor := &core.Executor{}
	PopulateExecutorCapabilities(executor)

	assert.NotEmpty(t, executor.Location.Description, "Location should be set")
	assert.Len(t, executor.Capabilities.Hardware, 1, "Should have one hardware entry")
	assert.NotEmpty(t, executor.Capabilities.Hardware[0].Platform, "Platform should be detected")
	assert.NotEmpty(t, executor.Capabilities.Hardware[0].Architecture, "Architecture should be detected")
	assert.NotEmpty(t, executor.Capabilities.Hardware[0].CPU, "CPU should be detected")

	// Memory detection only works on Linux
	if runtime.GOOS == "linux" {
		assert.NotEmpty(t, executor.Capabilities.Hardware[0].Memory, "Memory should be detected on Linux")
	}

	// Software info should be set
	assert.Len(t, executor.Capabilities.Software, 1, "Should have one software entry")
	assert.Equal(t, "docker-reconciler", executor.Capabilities.Software[0].Name)
	assert.Equal(t, "reconciler", executor.Capabilities.Software[0].Type)
}

func TestDetectMemoryMB(t *testing.T) {
	memMB := detectMemoryMB()

	if runtime.GOOS == "linux" {
		// On Linux, we should detect some memory
		assert.GreaterOrEqual(t, memMB, int64(0), "memory should be non-negative on Linux")
	} else {
		// On non-Linux, the function returns 0
		assert.Equal(t, int64(0), memMB, "memory should be 0 on non-Linux systems")
	}
}

func TestDetectGPUs(t *testing.T) {
	gpus := detectGPUs()

	// This test will vary based on the system
	// On systems without GPUs or on non-Linux, should return empty slice
	assert.NotNil(t, gpus, "GPU slice should not be nil")

	// If GPUs are detected, validate their structure
	for _, gpu := range gpus {
		assert.GreaterOrEqual(t, gpu.Index, 0, "GPU index should be non-negative")
		assert.NotEmpty(t, gpu.Name, "GPU name should not be empty")
		assert.GreaterOrEqual(t, gpu.Memory, int64(0), "GPU memory should be non-negative")
	}

	if runtime.GOOS != "linux" {
		assert.Empty(t, gpus, "GPUs should be empty on non-Linux systems")
	}
}

func TestGPUInfo(t *testing.T) {
	tests := []struct {
		name string
		gpu  GPUInfo
	}{
		{
			name: "Valid GPU with all fields",
			gpu: GPUInfo{
				Index:  0,
				Name:   "NVIDIA GeForce RTX 3090",
				Memory: 24576,
			},
		},
		{
			name: "GPU with zero memory",
			gpu: GPUInfo{
				Index:  1,
				Name:   "Unknown GPU",
				Memory: 0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.GreaterOrEqual(t, tt.gpu.Index, 0, "Index should be non-negative")
			assert.NotEmpty(t, tt.gpu.Name, "Name should not be empty")
			assert.GreaterOrEqual(t, tt.gpu.Memory, int64(0), "Memory should be non-negative")
		})
	}
}

func TestPopulateExecutorCapabilities_CPUCores(t *testing.T) {
	executor := &core.Executor{}
	PopulateExecutorCapabilities(executor)

	// Verify CPU is set (either to model name or core count)
	assert.Len(t, executor.Capabilities.Hardware, 1, "Should have one hardware entry")
	assert.NotEmpty(t, executor.Capabilities.Hardware[0].CPU, "CPU should be set")
}

func TestPopulateExecutorCapabilities_PlatformInfo(t *testing.T) {
	executor := &core.Executor{}
	PopulateExecutorCapabilities(executor)

	// Verify platform and architecture match runtime
	assert.Len(t, executor.Capabilities.Hardware, 1, "Should have one hardware entry")
	assert.Equal(t, runtime.GOOS, executor.Capabilities.Hardware[0].Platform, "Platform should match runtime.GOOS")
	assert.Equal(t, runtime.GOARCH, executor.Capabilities.Hardware[0].Architecture, "Architecture should match runtime.GOARCH")
}

func TestPopulateExecutorCapabilities_GPU(t *testing.T) {
	executor := &core.Executor{}
	PopulateExecutorCapabilities(executor)

	// GPU count should be non-negative
	assert.Len(t, executor.Capabilities.Hardware, 1, "Should have one hardware entry")
	assert.GreaterOrEqual(t, executor.Capabilities.Hardware[0].GPU.Count, 0, "GPU count should be non-negative")
}
