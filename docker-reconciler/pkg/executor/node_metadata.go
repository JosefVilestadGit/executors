package executor

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/colonyos/colonies/pkg/core"
	log "github.com/sirupsen/logrus"
)

// GPUInfo contains information about a GPU
type GPUInfo struct {
	Index  int
	Name   string
	Memory int64 // Memory in MB
}

// detectMemoryMB detects total system memory in MB
func detectMemoryMB() int64 {
	if runtime.GOOS != "linux" {
		return 0
	}

	file, err := os.Open("/proc/meminfo")
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Warn("Failed to open /proc/meminfo")
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				memKB, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					return memKB / 1024 // Convert KB to MB
				}
			}
		}
	}
	return 0
}

// detectGPUs detects GPUs and their properties (NVIDIA only for now)
func detectGPUs() []GPUInfo {
	gpus := []GPUInfo{}

	if runtime.GOOS != "linux" {
		return gpus
	}

	// Try nvidia-smi first (most reliable)
	gpus = detectGPUsViaNvidiaSmi()
	if len(gpus) > 0 {
		return gpus
	}

	// Fallback: check for NVIDIA GPUs by counting devices in /proc/driver/nvidia/gpus/
	gpuDir := "/proc/driver/nvidia/gpus"
	entries, err := os.ReadDir(gpuDir)
	if err == nil {
		index := 0
		for _, entry := range entries {
			if entry.IsDir() {
				// Try to read GPU information
				infoPath := filepath.Join(gpuDir, entry.Name(), "information")
				name := "Unknown GPU"

				if data, err := os.ReadFile(infoPath); err == nil {
					// Parse the information file for Model name
					lines := strings.Split(string(data), "\n")
					for _, line := range lines {
						if strings.HasPrefix(line, "Model:") {
							parts := strings.SplitN(line, ":", 2)
							if len(parts) == 2 {
								name = strings.TrimSpace(parts[1])
							}
						}
					}
				}

				gpus = append(gpus, GPUInfo{
					Index:  index,
					Name:   name,
					Memory: 0, // Memory not available from /proc
				})
				index++
			}
		}
		return gpus
	}

	// Alternative: check for /dev/nvidia* devices
	devices, err := filepath.Glob("/dev/nvidia[0-9]*")
	if err == nil {
		for i := range devices {
			gpus = append(gpus, GPUInfo{
				Index:  i,
				Name:   "Unknown GPU",
				Memory: 0,
			})
		}
	}

	return gpus
}

// detectGPUsViaNvidiaSmi uses nvidia-smi to detect GPUs
func detectGPUsViaNvidiaSmi() []GPUInfo {
	gpus := []GPUInfo{}

	// Check if nvidia-smi exists
	nvidiaSmiPath, err := exec.LookPath("nvidia-smi")
	if err != nil {
		// nvidia-smi not found
		return gpus
	}

	// Run nvidia-smi to get GPU info in CSV format
	// Format: index, name, memory.total
	cmd := exec.Command(nvidiaSmiPath, "--query-gpu=index,name,memory.total", "--format=csv,noheader,nounits")
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Debug("Failed to run nvidia-smi")
		return gpus
	}

	// Parse the output
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ", ")
		if len(parts) >= 3 {
			index, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
			name := strings.TrimSpace(parts[1])
			memory, err2 := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)

			if err1 == nil && err2 == nil {
				gpus = append(gpus, GPUInfo{
					Index:  index,
					Name:   name,
					Memory: memory, // Memory is in MB from nvidia-smi
				})
			}
		}
	}

	return gpus
}

// detectNodeMetadata detects and returns node metadata including hardware information
func detectNodeMetadata() *core.NodeMetadata {
	// Use COLONIES_NODE_NAME if set, otherwise fall back to hostname
	nodeName := os.Getenv("COLONIES_NODE_NAME")
	if nodeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			nodeName = "unknown"
		} else {
			nodeName = hostname
		}
	}

	location := os.Getenv("COLONIES_NODE_LOCATION")
	if location == "" {
		location = "default"
	}

	// Detect GPUs and populate labels
	gpus := detectGPUs()
	labels := make(map[string]string)

	for _, gpu := range gpus {
		indexStr := strconv.Itoa(gpu.Index)
		labels["gpu."+indexStr+".name"] = gpu.Name
		if gpu.Memory > 0 {
			labels["gpu."+indexStr+".memory"] = strconv.FormatInt(gpu.Memory, 10)
		}
	}

	metadata := &core.NodeMetadata{
		Hostname:     nodeName,
		Location:     location,
		Platform:     runtime.GOOS,
		Architecture: runtime.GOARCH,
		CPU:          runtime.NumCPU(),
		Memory:       detectMemoryMB(),
		GPU:          len(gpus),
		Capabilities: []string{"docker"},
		Labels:       labels,
	}

	return metadata
}
