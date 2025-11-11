package monitoring

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"telegram-archive-bot/utils"
)

// SystemResourceMonitor tracks detailed system resource usage
type SystemResourceMonitor struct {
	logger           *utils.Logger
	lastCPUTimes     map[string]uint64
	lastCPUCheck     time.Time
	processStartTime time.Time
	monitoringActive bool
}

// CPUStats represents CPU utilization statistics
type CPUStats struct {
	UserPercent   float64 `json:"user_percent"`
	SystemPercent float64 `json:"system_percent"`
	IdlePercent   float64 `json:"idle_percent"`
	TotalPercent  float64 `json:"total_percent"`
}

// MemoryStats represents detailed memory statistics
type MemoryStats struct {
	AllocMB        float64 `json:"alloc_mb"`
	SysMB          float64 `json:"sys_mb"`
	HeapAllocMB    float64 `json:"heap_alloc_mb"`
	HeapSysMB      float64 `json:"heap_sys_mb"`
	StackInUseMB   float64 `json:"stack_inuse_mb"`
	StackSysMB     float64 `json:"stack_sys_mb"`
	MSpanInUseMB   float64 `json:"mspan_inuse_mb"`
	MCacheInUseMB  float64 `json:"mcache_inuse_mb"`
	GCPercent      float64 `json:"gc_percent"`
	NumGC          uint32  `json:"num_gc"`
	PauseTotalNs   uint64  `json:"pause_total_ns"`
	LastGC         time.Time `json:"last_gc"`
}

// DiskStats represents disk usage statistics
type DiskStats struct {
	TotalBytes     uint64  `json:"total_bytes"`
	FreeBytes      uint64  `json:"free_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	UsedPercent    float64 `json:"used_percent"`
	AvailableBytes uint64  `json:"available_bytes"`
	InodeTotal     uint64  `json:"inode_total"`
	InodeFree      uint64  `json:"inode_free"`
	InodeUsed      uint64  `json:"inode_used"`
}

// ProcessStats represents process-specific statistics
type ProcessStats struct {
	PID              int       `json:"pid"`
	Goroutines       int       `json:"goroutines"`
	Threads          int       `json:"threads"`
	FDs              int       `json:"file_descriptors"`
	VMemMB           float64   `json:"virtual_memory_mb"`
	RSSMemMB         float64   `json:"rss_memory_mb"`
	CPUPercent       float64   `json:"cpu_percent"`
	StartTime        time.Time `json:"start_time"`
	Uptime           time.Duration `json:"uptime"`
}

// SystemResourceSnapshot represents a complete system resource snapshot
type SystemResourceSnapshot struct {
	Timestamp  time.Time    `json:"timestamp"`
	CPU        CPUStats     `json:"cpu"`
	Memory     MemoryStats  `json:"memory"`
	Disk       map[string]DiskStats `json:"disk"`
	Process    ProcessStats `json:"process"`
	LoadAvg    []float64    `json:"load_average"`
}

// NewSystemResourceMonitor creates a new system resource monitor
func NewSystemResourceMonitor(logger *utils.Logger) *SystemResourceMonitor {
	return &SystemResourceMonitor{
		logger:           logger,
		lastCPUTimes:     make(map[string]uint64),
		processStartTime: time.Now(),
		monitoringActive: true,
	}
}

// GetSystemSnapshot captures a complete system resource snapshot
func (srm *SystemResourceMonitor) GetSystemSnapshot() (*SystemResourceSnapshot, error) {
	snapshot := &SystemResourceSnapshot{
		Timestamp: time.Now(),
		Disk:      make(map[string]DiskStats),
	}

	// Get CPU stats
	cpuStats, err := srm.getCPUStats()
	if err != nil {
		srm.logger.WithError(err).Warn("Failed to get CPU stats")
		// Continue with other metrics even if CPU fails
	} else {
		snapshot.CPU = *cpuStats
	}

	// Get memory stats
	memStats := srm.getMemoryStats()
	snapshot.Memory = *memStats

	// Get disk stats for important paths
	importantPaths := []string{
		".", // Current directory (project root)
		"temp",
		"data",
		"logs",
		"app/extraction",
	}

	for _, path := range importantPaths {
		if diskStats, err := srm.getDiskStats(path); err == nil {
			cleanPath := strings.ReplaceAll(path, "/", "_")
			if cleanPath == "." {
				cleanPath = "root"
			}
			snapshot.Disk[cleanPath] = *diskStats
		} else {
			srm.logger.WithError(err).WithField("path", path).Debug("Failed to get disk stats")
		}
	}

	// Get process stats
	processStats, err := srm.getProcessStats()
	if err != nil {
		srm.logger.WithError(err).Warn("Failed to get process stats")
	} else {
		snapshot.Process = *processStats
	}

	// Get load average (Linux only)
	if loadAvg, err := srm.getLoadAverage(); err == nil {
		snapshot.LoadAvg = loadAvg
	}

	return snapshot, nil
}

// getCPUStats gets CPU utilization statistics (Linux specific)
func (srm *SystemResourceMonitor) getCPUStats() (*CPUStats, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return nil, fmt.Errorf("failed to open /proc/stat: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return nil, fmt.Errorf("failed to read CPU line from /proc/stat")
	}

	line := scanner.Text()
	fields := strings.Fields(line)
	if len(fields) < 8 || fields[0] != "cpu" {
		return nil, fmt.Errorf("invalid CPU line format")
	}

	// Parse CPU times
	var times [7]uint64
	for i := 0; i < 7; i++ {
		if val, err := strconv.ParseUint(fields[i+1], 10, 64); err == nil {
			times[i] = val
		}
	}

	user := times[0]
	nice := times[1]
	system := times[2]
	idle := times[3]
	iowait := times[4]
	irq := times[5]
	softirq := times[6]

	total := user + nice + system + idle + iowait + irq + softirq
	totalActive := total - idle - iowait

	stats := &CPUStats{}

	// Calculate percentages if we have previous measurements
	now := time.Now()
	if !srm.lastCPUCheck.IsZero() && total > srm.lastCPUTimes["total"] {
		totalDelta := total - srm.lastCPUTimes["total"]
		userDelta := user - srm.lastCPUTimes["user"]
		systemDelta := system - srm.lastCPUTimes["system"]
		idleDelta := idle - srm.lastCPUTimes["idle"]
		activeDelta := totalActive - srm.lastCPUTimes["active"]

		if totalDelta > 0 {
			stats.UserPercent = float64(userDelta) / float64(totalDelta) * 100
			stats.SystemPercent = float64(systemDelta) / float64(totalDelta) * 100
			stats.IdlePercent = float64(idleDelta) / float64(totalDelta) * 100
			stats.TotalPercent = float64(activeDelta) / float64(totalDelta) * 100
		}
	}

	// Store current values for next calculation
	srm.lastCPUTimes["total"] = total
	srm.lastCPUTimes["user"] = user
	srm.lastCPUTimes["system"] = system
	srm.lastCPUTimes["idle"] = idle
	srm.lastCPUTimes["active"] = totalActive
	srm.lastCPUCheck = now

	return stats, nil
}

// getMemoryStats gets detailed memory statistics
func (srm *SystemResourceMonitor) getMemoryStats() *MemoryStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	stats := &MemoryStats{
		AllocMB:       float64(m.Alloc) / 1024 / 1024,
		SysMB:         float64(m.Sys) / 1024 / 1024,
		HeapAllocMB:   float64(m.HeapAlloc) / 1024 / 1024,
		HeapSysMB:     float64(m.HeapSys) / 1024 / 1024,
		StackInUseMB:  float64(m.StackInuse) / 1024 / 1024,
		StackSysMB:    float64(m.StackSys) / 1024 / 1024,
		MSpanInUseMB:  float64(m.MSpanInuse) / 1024 / 1024,
		MCacheInUseMB: float64(m.MCacheInuse) / 1024 / 1024,
		NumGC:         m.NumGC,
		PauseTotalNs:  m.PauseTotalNs,
	}

	// Calculate GC percentage
	if m.Sys > 0 {
		stats.GCPercent = float64(m.HeapSys-m.HeapIdle) / float64(m.Sys) * 100
	}

	// Get last GC time
	if m.NumGC > 0 {
		stats.LastGC = time.Unix(0, int64(m.LastGC))
	}

	return stats
}

// getDiskStats gets disk usage statistics for a given path
func (srm *SystemResourceMonitor) getDiskStats(path string) (*DiskStats, error) {
	var stat syscall.Statfs_t
	
	// Get absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Check if path exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("path does not exist: %s", absPath)
	}

	err = syscall.Statfs(absPath, &stat)
	if err != nil {
		return nil, fmt.Errorf("failed to get filesystem stats: %w", err)
	}

	totalBytes := stat.Blocks * uint64(stat.Bsize)
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	usedBytes := totalBytes - (stat.Bfree * uint64(stat.Bsize))
	
	stats := &DiskStats{
		TotalBytes:     totalBytes,
		FreeBytes:      freeBytes,
		UsedBytes:      usedBytes,
		AvailableBytes: stat.Bavail * uint64(stat.Bsize),
		InodeTotal:     stat.Files,
		InodeFree:      stat.Ffree,
		InodeUsed:      stat.Files - stat.Ffree,
	}

	if totalBytes > 0 {
		stats.UsedPercent = float64(usedBytes) / float64(totalBytes) * 100
	}

	return stats, nil
}

// getProcessStats gets process-specific statistics
func (srm *SystemResourceMonitor) getProcessStats() (*ProcessStats, error) {
	pid := os.Getpid()
	
	stats := &ProcessStats{
		PID:        pid,
		Goroutines: runtime.NumGoroutine(),
		StartTime:  srm.processStartTime,
		Uptime:     time.Since(srm.processStartTime),
	}

	// Get process memory info from /proc/self/status
	if memInfo, err := srm.getProcessMemoryInfo(); err == nil {
		stats.VMemMB = memInfo["VmSize"]
		stats.RSSMemMB = memInfo["VmRSS"]
	}

	// Get file descriptor count
	if fdCount, err := srm.getFileDescriptorCount(); err == nil {
		stats.FDs = fdCount
	}

	// Get thread count from /proc/self/status
	if threadCount, err := srm.getThreadCount(); err == nil {
		stats.Threads = threadCount
	}

	return stats, nil
}

// getProcessMemoryInfo reads memory info from /proc/self/status
func (srm *SystemResourceMonitor) getProcessMemoryInfo() (map[string]float64, error) {
	file, err := os.Open("/proc/self/status")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	memInfo := make(map[string]float64)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "VmSize:") || strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				key := strings.TrimSuffix(fields[0], ":")
				if value, err := strconv.ParseFloat(fields[1], 64); err == nil {
					// Convert from KB to MB
					memInfo[key] = value / 1024
				}
			}
		}
	}

	return memInfo, scanner.Err()
}

// getFileDescriptorCount counts open file descriptors
func (srm *SystemResourceMonitor) getFileDescriptorCount() (int, error) {
	fdDir := "/proc/self/fd"
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

// getThreadCount gets thread count from /proc/self/status
func (srm *SystemResourceMonitor) getThreadCount() (int, error) {
	file, err := os.Open("/proc/self/status")
	if err != nil {
		return 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Threads:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return strconv.Atoi(fields[1])
			}
		}
	}

	return 0, fmt.Errorf("thread count not found")
}

// getLoadAverage gets system load average (Linux only)
func (srm *SystemResourceMonitor) getLoadAverage() ([]float64, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil, err
	}

	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return nil, fmt.Errorf("invalid loadavg format")
	}

	loadAvg := make([]float64, 3)
	for i := 0; i < 3; i++ {
		if val, err := strconv.ParseFloat(fields[i], 64); err == nil {
			loadAvg[i] = val
		}
	}

	return loadAvg, nil
}

// FormatBytes formats bytes into human-readable format
func FormatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// GetResourceSummary returns a formatted summary of system resources
func (srm *SystemResourceMonitor) GetResourceSummary() (string, error) {
	snapshot, err := srm.GetSystemSnapshot()
	if err != nil {
		return "", fmt.Errorf("failed to get system snapshot: %w", err)
	}

	summary := fmt.Sprintf(`📊 **System Resources**

💾 **Memory Usage:**
• Allocated: %.1f MB
• System: %.1f MB  
• Heap: %.1f MB
• Stack: %.1f MB
• GC: %d collections

⚡ **CPU Usage:**
• Total: %.1f%%
• User: %.1f%%
• System: %.1f%%
• Idle: %.1f%%`,
		snapshot.Memory.AllocMB,
		snapshot.Memory.SysMB,
		snapshot.Memory.HeapAllocMB,
		snapshot.Memory.StackInUseMB,
		snapshot.Memory.NumGC,
		snapshot.CPU.TotalPercent,
		snapshot.CPU.UserPercent,
		snapshot.CPU.SystemPercent,
		snapshot.CPU.IdlePercent)

	// Add disk usage
	summary += "\n\n💿 **Disk Usage:**"
	for path, disk := range snapshot.Disk {
		summary += fmt.Sprintf("\n• %s: %s / %s (%.1f%%)",
			path,
			FormatBytes(disk.UsedBytes),
			FormatBytes(disk.TotalBytes),
			disk.UsedPercent)
	}

	// Add process info
	summary += fmt.Sprintf(`

🔧 **Process Info:**
• PID: %d
• Goroutines: %d
• Threads: %d
• File Descriptors: %d
• Uptime: %s`,
		snapshot.Process.PID,
		snapshot.Process.Goroutines,
		snapshot.Process.Threads,
		snapshot.Process.FDs,
		snapshot.Process.Uptime.Round(time.Second).String())

	// Add load average if available
	if len(snapshot.LoadAvg) == 3 {
		summary += fmt.Sprintf("\n• Load Avg: %.2f, %.2f, %.2f",
			snapshot.LoadAvg[0], snapshot.LoadAvg[1], snapshot.LoadAvg[2])
	}

	return summary, nil
}