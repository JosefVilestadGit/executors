package executor

import (
	"github.com/colonyos/colonies/pkg/core"
	"github.com/colonyos/executors/docker-reconciler/pkg/hwdetect"
)

// GPUInfo is an alias for hwdetect.GPUInfo for backward compatibility
type GPUInfo = hwdetect.GPUInfo

// PopulateExecutorCapabilities delegates to hwdetect package
func PopulateExecutorCapabilities(executor *core.Executor) {
	hwdetect.PopulateExecutorCapabilities(executor)
}

// detectMemoryMB delegates to hwdetect package (for tests)
func detectMemoryMB() int64 {
	return hwdetect.DetectMemoryMB()
}

// detectGPUs delegates to hwdetect package (for tests)
func detectGPUs() []GPUInfo {
	return hwdetect.DetectGPUs()
}
