package hwdetect

import (
	"bufio"
	"bytes"
	"net"
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

// DetectMemoryMB detects total system memory in MB
func DetectMemoryMB() int64 {
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("sysctl", "-n", "hw.memsize")
		out, err := cmd.Output()
		if err == nil {
			memBytes, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
			if err == nil {
				return memBytes / (1024 * 1024) // Convert bytes to MB
			}
		}
		return 0
	}

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

// DetectGPUs detects GPUs and their properties (NVIDIA only for now)
func DetectGPUs() []GPUInfo {
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

// DetectModel returns a descriptive model name for the system
func DetectModel() string {
	if runtime.GOOS == "darwin" {
		// Try to get Mac model
		cmd := exec.Command("sysctl", "-n", "hw.model")
		out, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
		return "Apple Mac"
	}

	if runtime.GOOS == "linux" {
		// Try to get product name from DMI
		data, err := os.ReadFile("/sys/devices/virtual/dmi/id/product_name")
		if err == nil {
			name := strings.TrimSpace(string(data))
			if name != "" && name != "System Product Name" && name != "To Be Filled By O.E.M." {
				return name
			}
		}
		return "Linux Server"
	}

	return "Server"
}

// DetectStorage returns total disk storage in GB
func DetectStorage() string {
	if runtime.GOOS == "darwin" {
		// macOS: use df with different flags
		cmd := exec.Command("df", "-k", "/")
		out, err := cmd.Output()
		if err != nil {
			return ""
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) >= 2 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 2 {
				sizeKB, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					sizeGB := sizeKB / (1024 * 1024)
					return strconv.FormatInt(sizeGB, 10) + " GB"
				}
			}
		}
		return ""
	}

	if runtime.GOOS != "linux" {
		return ""
	}

	// Get root filesystem size using statfs
	cmd := exec.Command("df", "-B1", "--output=size", "/")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) >= 2 {
		sizeStr := strings.TrimSpace(lines[1])
		size, err := strconv.ParseInt(sizeStr, 10, 64)
		if err == nil {
			sizeGB := size / (1024 * 1024 * 1024)
			return strconv.FormatInt(sizeGB, 10) + " GB"
		}
	}
	return ""
}

// DetectCPUModel detects the CPU model name
func DetectCPUModel() string {
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("sysctl", "-n", "machdep.cpu.brand_string")
		out, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
		return strconv.Itoa(runtime.NumCPU()) + " cores"
	}

	if runtime.GOOS != "linux" {
		return strconv.Itoa(runtime.NumCPU()) + " cores"
	}

	file, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return strconv.Itoa(runtime.NumCPU()) + " cores"
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return strconv.Itoa(runtime.NumCPU()) + " cores"
}

// DetectNetworkAddresses returns local network IP addresses (excluding loopback)
func DetectNetworkAddresses() []string {
	var addresses []string

	ifaces, err := net.Interfaces()
	if err != nil {
		log.WithFields(log.Fields{"Error": err}).Warn("Failed to get network interfaces")
		return addresses
	}

	for _, iface := range ifaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// Skip loopback and IPv6 link-local addresses
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}

			// Only include IPv4 addresses
			if ip.To4() != nil {
				addresses = append(addresses, ip.String())
			}
		}
	}

	return addresses
}

// PopulateExecutorCapabilities detects hardware information and populates the executor's capabilities
func PopulateExecutorCapabilities(executor *core.Executor) {
	// Set location from environment
	location := os.Getenv("COLONIES_EXECUTOR_LOCATION")
	if location == "" {
		location = "default"
	}
	executor.Location = core.Location{
		Description: location,
	}

	// Detect GPUs
	gpus := DetectGPUs()

	// Determine GPU name and memory from first GPU (if any)
	gpuName := ""
	gpuMemory := ""
	if len(gpus) > 0 {
		gpuName = gpus[0].Name
		if gpus[0].Memory > 0 {
			gpuMemory = strconv.FormatInt(gpus[0].Memory, 10) + " MB"
		}
	}

	// Detect memory
	memoryMB := DetectMemoryMB()
	memoryStr := ""
	if memoryMB > 0 {
		memoryStr = strconv.FormatInt(memoryMB, 10) + " MB"
	}

	// Set hardware capabilities
	executor.Capabilities = core.Capabilities{
		Hardware: []core.Hardware{{
			Model:        DetectModel(),
			Nodes:        1,
			CPU:          DetectCPUModel(),
			Memory:       memoryStr,
			Storage:      DetectStorage(),
			Platform:     runtime.GOOS,
			Architecture: runtime.GOARCH,
			Network:      DetectNetworkAddresses(),
			GPU: core.GPU{
				Name:      gpuName,
				Memory:    gpuMemory,
				Count:     len(gpus),
				NodeCount: len(gpus), // For single-node deployment, GPUs per node = total GPUs
			},
		}},
		Software: []core.Software{{
			Name: "docker-reconciler",
			Type: "reconciler",
		}},
	}
}
