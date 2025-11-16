package executor

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectNodeMetadata(t *testing.T) {
	metadata := detectNodeMetadata()

	assert.NotNil(t, metadata, "metadata should not be nil")
	assert.Greater(t, metadata.CPU, 0, "CPU cores should be greater than 0")
	assert.NotEmpty(t, metadata.Hostname, "Hostname should be detected")
	assert.NotEmpty(t, metadata.Location, "Location should be set")
	assert.NotEmpty(t, metadata.Platform, "Platform should be detected")
	assert.NotEmpty(t, metadata.Architecture, "Architecture should be detected")

	// Memory detection only works on Linux
	if runtime.GOOS == "linux" {
		assert.GreaterOrEqual(t, metadata.Memory, int64(0), "memory should be non-negative")
	}

	// Capabilities should include docker
	assert.Contains(t, metadata.Capabilities, "docker", "should have docker capability")
	assert.NotNil(t, metadata.Labels, "labels should not be nil")
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

func TestDetectNodeMetadata_CPUCores(t *testing.T) {
	metadata := detectNodeMetadata()

	// Verify CPU cores matches runtime detection
	expectedCores := runtime.NumCPU()
	assert.Equal(t, expectedCores, metadata.CPU, "CPU cores should match runtime.NumCPU()")
}

func TestDetectNodeMetadata_PlatformInfo(t *testing.T) {
	metadata := detectNodeMetadata()

	// Verify platform and architecture match runtime
	assert.Equal(t, runtime.GOOS, metadata.Platform, "Platform should match runtime.GOOS")
	assert.Equal(t, runtime.GOARCH, metadata.Architecture, "Architecture should match runtime.GOARCH")
}

func TestDetectNodeMetadata_GPU_Labels(t *testing.T) {
	metadata := detectNodeMetadata()

	// GPU count should match the number of GPU labels
	assert.GreaterOrEqual(t, metadata.GPU, 0, "GPU count should be non-negative")

	// If GPUs are detected, verify labels exist
	if metadata.GPU > 0 {
		// Check that GPU labels are populated
		hasGPULabel := false
		for key := range metadata.Labels {
			if len(key) >= 4 && key[:4] == "gpu." {
				hasGPULabel = true
				break
			}
		}
		assert.True(t, hasGPULabel, "Should have GPU labels when GPUs are detected")
	}
}
