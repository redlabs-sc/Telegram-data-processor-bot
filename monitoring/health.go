package monitoring

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"telegram-archive-bot/storage"
	"telegram-archive-bot/utils"
)

// HealthStatus represents the health status of a component
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "HEALTHY"
	HealthStatusDegraded  HealthStatus = "DEGRADED"
	HealthStatusUnhealthy HealthStatus = "UNHEALTHY"
)

// ComponentHealth represents the health of a single component
type ComponentHealth struct {
	Name           string       `json:"name"`
	Status         HealthStatus `json:"status"`
	Message        string       `json:"message,omitempty"`
	LastChecked    time.Time    `json:"last_checked"`
	ResponseTimeMs int64        `json:"response_time_ms"`
}

// HealthCheck represents the overall system health
type HealthCheck struct {
	Status     HealthStatus      `json:"status"`
	Timestamp  time.Time         `json:"timestamp"`
	Uptime     time.Duration     `json:"uptime"`
	Components []ComponentHealth `json:"components"`
	SystemInfo SystemInfo        `json:"system_info"`
}

// SystemInfo contains system resource information
type SystemInfo struct {
	CPUUsage      float64 `json:"cpu_usage_percent"`
	MemoryUsage   float64 `json:"memory_usage_mb"`
	MemoryPercent float64 `json:"memory_usage_percent"`
	DiskUsage     int64   `json:"disk_usage_bytes"`
	Goroutines    int     `json:"goroutines"`
	StartTime     time.Time `json:"start_time"`
}

// DiagnosticResult represents the result of a diagnostic check
type DiagnosticResult struct {
	Name        string                 `json:"name"`
	Status      HealthStatus           `json:"status"`
	Message     string                 `json:"message"`
	Timestamp   time.Time              `json:"timestamp"`
	Duration    time.Duration          `json:"duration"`
	Details     map[string]interface{} `json:"details,omitempty"`
}

// DiagnosticSuite represents a collection of diagnostic results
type DiagnosticSuite struct {
	Timestamp   time.Time           `json:"timestamp"`
	Duration    time.Duration       `json:"duration"`
	Results     []DiagnosticResult  `json:"results"`
	OverallStatus HealthStatus      `json:"overall_status"`
}

// HealthMonitor manages application health checks and uptime tracking
type HealthMonitor struct {
	startTime          time.Time
	logger             *utils.Logger
	taskStore          *storage.TaskStore
	metrics            *PerformanceMetrics
	systemMonitor      *SystemResourceMonitor
	alertManager       *AlertManager
	components         map[string]HealthChecker
	lastCheck          *HealthCheck
	lastSystemSnapshot *SystemResourceSnapshot
	lastDiagnostics    *DiagnosticSuite
	checkMutex         sync.RWMutex
	checkInterval      time.Duration
	ctx                context.Context
	cancel             context.CancelFunc
}

// HealthChecker interface for individual component health checks
type HealthChecker interface {
	Name() string
	Check(ctx context.Context) ComponentHealth
}

// NewHealthMonitor creates a new health monitor
func NewHealthMonitor(logger *utils.Logger, taskStore *storage.TaskStore) *HealthMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	
	hm := &HealthMonitor{
		startTime:     time.Now(),
		logger:        logger,
		taskStore:     taskStore,
		metrics:       NewPerformanceMetrics(logger),
		systemMonitor: NewSystemResourceMonitor(logger),
		alertManager:  NewAlertManager(logger),
		components:    make(map[string]HealthChecker),
		checkInterval: 30 * time.Second, // Check every 30 seconds
		ctx:           ctx,
		cancel:        cancel,
	}

	// Register built-in health checkers
	hm.RegisterChecker(&DatabaseHealthChecker{taskStore: taskStore})
	hm.RegisterChecker(&FileSystemHealthChecker{})
	hm.RegisterChecker(&MemoryHealthChecker{})
	hm.RegisterChecker(&ExternalDependencyHealthChecker{})

	return hm
}

// RegisterChecker adds a health checker for a component
func (hm *HealthMonitor) RegisterChecker(checker HealthChecker) {
	hm.components[checker.Name()] = checker
	hm.logger.WithField("component", checker.Name()).Info("Health checker registered")
}

// Start begins periodic health checks
func (hm *HealthMonitor) Start() {
	hm.logger.Info("Starting health monitor")
	
	// Perform initial health check
	hm.performHealthCheck()
	
	// Start periodic checks
	ticker := time.NewTicker(hm.checkInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-hm.ctx.Done():
				hm.logger.Info("Health monitor stopped")
				return
			case <-ticker.C:
				hm.performHealthCheck()
			}
		}
	}()
}

// Stop stops the health monitor
func (hm *HealthMonitor) Stop() {
	hm.logger.Info("Stopping health monitor")
	hm.cancel()
}

// GetUptime returns the application uptime
func (hm *HealthMonitor) GetUptime() time.Duration {
	return time.Since(hm.startTime)
}

// GetStartTime returns when the application started
func (hm *HealthMonitor) GetStartTime() time.Time {
	return hm.startTime
}

// GetMetrics returns the performance metrics collector
func (hm *HealthMonitor) GetMetrics() *PerformanceMetrics {
	return hm.metrics
}

// GetSystemMonitor returns the system resource monitor
func (hm *HealthMonitor) GetSystemMonitor() *SystemResourceMonitor {
	return hm.systemMonitor
}

// GetLastSystemSnapshot returns the most recent system resource snapshot
func (hm *HealthMonitor) GetLastSystemSnapshot() *SystemResourceSnapshot {
	hm.checkMutex.RLock()
	defer hm.checkMutex.RUnlock()
	return hm.lastSystemSnapshot
}

// GetAlertManager returns the alert manager
func (hm *HealthMonitor) GetAlertManager() *AlertManager {
	return hm.alertManager
}

// GetLastHealthCheck returns the most recent health check result
func (hm *HealthMonitor) GetLastHealthCheck() *HealthCheck {
	hm.checkMutex.RLock()
	defer hm.checkMutex.RUnlock()
	return hm.lastCheck
}

// performHealthCheck runs all registered health checkers
func (hm *HealthMonitor) performHealthCheck() {
	start := time.Now()
	
	healthCheck := &HealthCheck{
		Timestamp:  start,
		Uptime:     hm.GetUptime(),
		Components: make([]ComponentHealth, 0, len(hm.components)),
		SystemInfo: hm.getSystemInfo(),
	}

	overallStatus := HealthStatusHealthy
	
	// Check all registered components
	for _, checker := range hm.components {
		checkStart := time.Now()
		componentHealth := checker.Check(hm.ctx)
		componentHealth.ResponseTimeMs = time.Since(checkStart).Milliseconds()
		componentHealth.LastChecked = time.Now()
		
		healthCheck.Components = append(healthCheck.Components, componentHealth)
		
		// Determine overall status (worst case wins)
		if componentHealth.Status == HealthStatusUnhealthy {
			overallStatus = HealthStatusUnhealthy
		} else if componentHealth.Status == HealthStatusDegraded && overallStatus == HealthStatusHealthy {
			overallStatus = HealthStatusDegraded
		}
	}
	
	healthCheck.Status = overallStatus
	
	// Store the result
	hm.checkMutex.Lock()
	hm.lastCheck = healthCheck
	hm.checkMutex.Unlock()
	
	// Log the results
	duration := time.Since(start)
	hm.logger.WithField("status", string(overallStatus)).
		WithField("components", len(healthCheck.Components)).
		WithField("duration_ms", duration.Milliseconds()).
		Debug("Health check completed")
		
	// Update queue metrics if taskStore is available
	if hm.taskStore != nil {
		hm.updateQueueMetrics()
	}
	
	// Capture system resource snapshot
	if systemSnapshot, err := hm.systemMonitor.GetSystemSnapshot(); err == nil {
		hm.checkMutex.Lock()
		hm.lastSystemSnapshot = systemSnapshot
		hm.checkMutex.Unlock()
	} else {
		hm.logger.WithError(err).Debug("Failed to capture system snapshot")
	}
	
	// Check alerts based on current system state
	hm.alertManager.CheckAlerts(hm.lastSystemSnapshot, hm.metrics)
	
	// Run periodic self-diagnostics (every 10 health checks = ~5 minutes)
	shouldRunDiagnostics := false
	if hm.lastDiagnostics == nil {
		// Run initial diagnostics
		shouldRunDiagnostics = true
	} else if time.Since(hm.lastDiagnostics.Timestamp) > 5*time.Minute {
		// Run diagnostics every 5 minutes
		shouldRunDiagnostics = true
	}
	
	if shouldRunDiagnostics {
		go hm.RunSelfDiagnostics()
	}
	
	// Log any unhealthy components
	for _, component := range healthCheck.Components {
		if component.Status != HealthStatusHealthy {
			hm.logger.WithField("component", component.Name).
				WithField("status", string(component.Status)).
				WithField("message", component.Message).
				Warn("Component health issue detected")
		}
	}
}

// getSystemInfo collects current system resource information
func (hm *HealthMonitor) getSystemInfo() SystemInfo {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	return SystemInfo{
		MemoryUsage:   float64(m.Alloc) / 1024 / 1024, // Convert to MB
		MemoryPercent: float64(m.Alloc) / float64(m.Sys) * 100,
		Goroutines:    runtime.NumGoroutine(),
		StartTime:     hm.startTime,
	}
}

// updateQueueMetrics updates queue metrics from task store
func (hm *HealthMonitor) updateQueueMetrics() {
	stats, err := hm.taskStore.GetStats()
	if err != nil {
		hm.logger.WithError(err).Error("Failed to get task stats for queue metrics")
		return
	}
	
	pending := stats["PENDING"]
	downloaded := stats["DOWNLOADED"]
	completed := stats["COMPLETED"]
	failed := stats["FAILED"]
	
	// Update performance metrics
	hm.metrics.UpdateQueueMetrics(pending, downloaded, 0, completed, failed)
}

// DatabaseHealthChecker checks database connectivity and performance
type DatabaseHealthChecker struct {
	taskStore *storage.TaskStore
}

func (d *DatabaseHealthChecker) Name() string {
	return "database"
}

func (d *DatabaseHealthChecker) Check(ctx context.Context) ComponentHealth {
	if d.taskStore == nil {
		return ComponentHealth{
			Name:    d.Name(),
			Status:  HealthStatusUnhealthy,
			Message: "TaskStore not initialized",
		}
	}
	
	// Test database connectivity by getting stats
	_, err := d.taskStore.GetStats()
	if err != nil {
		return ComponentHealth{
			Name:    d.Name(),
			Status:  HealthStatusUnhealthy,
			Message: fmt.Sprintf("Database query failed: %v", err),
		}
	}
	
	return ComponentHealth{
		Name:    d.Name(),
		Status:  HealthStatusHealthy,
		Message: "Database responding normally",
	}
}

// FileSystemHealthChecker checks file system access and disk space
type FileSystemHealthChecker struct{}

func (f *FileSystemHealthChecker) Name() string {
	return "filesystem"
}

func (f *FileSystemHealthChecker) Check(ctx context.Context) ComponentHealth {
	// Check critical directories
	criticalDirs := []string{
		"temp",
		"data",
		"logs",
		"app/extraction/files/all",
		"app/extraction/files/pass",
		"app/extraction/files/txt",
		"app/extraction/files/done",
		"app/extraction/files/errors",
		"app/extraction/files/nopass",
		"app/extraction/files/etbanks",
	}
	
	for _, dir := range criticalDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return ComponentHealth{
				Name:    f.Name(),
				Status:  HealthStatusDegraded,
				Message: fmt.Sprintf("Directory missing: %s", dir),
			}
		}
	}
	
	// Test write access to temp directory
	testFile := filepath.Join("temp", fmt.Sprintf("health_check_%d", time.Now().UnixNano()))
	if err := os.WriteFile(testFile, []byte("health check"), 0644); err != nil {
		return ComponentHealth{
			Name:    f.Name(),
			Status:  HealthStatusUnhealthy,
			Message: fmt.Sprintf("Cannot write to temp directory: %v", err),
		}
	}
	os.Remove(testFile) // Clean up
	
	return ComponentHealth{
		Name:    f.Name(),
		Status:  HealthStatusHealthy,
		Message: "All directories accessible",
	}
}

// MemoryHealthChecker monitors memory usage
type MemoryHealthChecker struct{}

func (m *MemoryHealthChecker) Name() string {
	return "memory"
}

func (m *MemoryHealthChecker) Check(ctx context.Context) ComponentHealth {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	
	memoryMB := float64(mem.Alloc) / 1024 / 1024
	
	// Set thresholds (configurable in production)
	const (
		degradedThresholdMB  = 500  // 500MB
		unhealthyThresholdMB = 1000 // 1GB
	)
	
	if memoryMB > unhealthyThresholdMB {
		return ComponentHealth{
			Name:    m.Name(),
			Status:  HealthStatusUnhealthy,
			Message: fmt.Sprintf("High memory usage: %.2fMB", memoryMB),
		}
	} else if memoryMB > degradedThresholdMB {
		return ComponentHealth{
			Name:    m.Name(),
			Status:  HealthStatusDegraded,
			Message: fmt.Sprintf("Elevated memory usage: %.2fMB", memoryMB),
		}
	}
	
	return ComponentHealth{
		Name:    m.Name(),
		Status:  HealthStatusHealthy,
		Message: fmt.Sprintf("Memory usage normal: %.2fMB", memoryMB),
	}
}

// ExternalDependencyHealthChecker checks external dependencies
type ExternalDependencyHealthChecker struct{}

func (e *ExternalDependencyHealthChecker) Name() string {
	return "external_dependencies"
}

func (e *ExternalDependencyHealthChecker) Check(ctx context.Context) ComponentHealth {
	// Check if extract.go and convert.go exist in correct subdirectories
	extractPath := "app/extraction/extract/extract.go"
	convertPath := "app/extraction/convert/convert.go"
	
	if _, err := os.Stat(extractPath); os.IsNotExist(err) {
		return ComponentHealth{
			Name:    e.Name(),
			Status:  HealthStatusUnhealthy,
			Message: "extract.go not found",
		}
	}
	
	if _, err := os.Stat(convertPath); os.IsNotExist(err) {
		return ComponentHealth{
			Name:    e.Name(),
			Status:  HealthStatusUnhealthy,
			Message: "convert.go not found",
		}
	}
	
	// Check if password file exists
	passPath := "app/extraction/pass.txt"
	if _, err := os.Stat(passPath); os.IsNotExist(err) {
		return ComponentHealth{
			Name:    e.Name(),
			Status:  HealthStatusDegraded,
			Message: "pass.txt not found (extraction may fail for password-protected archives)",
		}
	}
	
	return ComponentHealth{
		Name:    e.Name(),
		Status:  HealthStatusHealthy,
		Message: "All external dependencies available",
	}
}

// RunSelfDiagnostics runs comprehensive diagnostic checks
func (hm *HealthMonitor) RunSelfDiagnostics() {
	hm.logger.Info("Starting periodic self-diagnostics")
	
	startTime := time.Now()
	results := make([]DiagnosticResult, 0)
	
	// Diagnostic 1: External executable tests
	results = append(results, hm.diagnosExtractExecutable())
	results = append(results, hm.diagnosConvertExecutable())
	
	// Diagnostic 2: Directory structure validation
	results = append(results, hm.diagnosDirectoryStructure())
	
	// Diagnostic 3: Database integrity check
	results = append(results, hm.diagnosDatabaseIntegrity())
	
	// Diagnostic 4: Password file validation
	results = append(results, hm.diagnosPasswordFile())
	
	// Diagnostic 5: Disk space projections
	results = append(results, hm.diagnosDiskSpaceProjections())
	
	// Diagnostic 6: Network connectivity (Telegram API)
	results = append(results, hm.diagnosTelegramConnectivity())
	
	// Determine overall status
	overallStatus := HealthStatusHealthy
	for _, result := range results {
		if result.Status == HealthStatusUnhealthy {
			overallStatus = HealthStatusUnhealthy
			break
		} else if result.Status == HealthStatusDegraded && overallStatus == HealthStatusHealthy {
			overallStatus = HealthStatusDegraded
		}
	}
	
	suite := &DiagnosticSuite{
		Timestamp:     startTime,
		Duration:      time.Since(startTime),
		Results:       results,
		OverallStatus: overallStatus,
	}
	
	// Store diagnostics
	hm.checkMutex.Lock()
	hm.lastDiagnostics = suite
	hm.checkMutex.Unlock()
	
	// Log results
	hm.logger.WithField("overall_status", string(overallStatus)).
		WithField("duration", suite.Duration).
		WithField("checks_run", len(results)).
		Info("Self-diagnostics completed")
	
	// Alert on any critical issues
	for _, result := range results {
		if result.Status == HealthStatusUnhealthy {
			hm.logger.WithField("diagnostic", result.Name).
				WithField("message", result.Message).
				Error("Critical diagnostic issue detected")
		}
	}
}

// diagnosExtractExecutable tests the extract.go executable
func (hm *HealthMonitor) diagnosExtractExecutable() DiagnosticResult {
	start := time.Now()
	result := DiagnosticResult{
		Name:      "extract_executable",
		Timestamp: start,
		Details:   make(map[string]interface{}),
	}
	
	extractPath := "app/extraction/extract/extract.go"
	
	// Check if file exists
	if _, err := os.Stat(extractPath); os.IsNotExist(err) {
		result.Status = HealthStatusUnhealthy
		result.Message = "extract.go not found"
		result.Duration = time.Since(start)
		return result
	}
	
	// Test basic compilation (go build check)
	// Note: In production, we might skip this to avoid overhead
	result.Details["path"] = extractPath
	result.Details["size_bytes"] = func() int64 {
		if info, err := os.Stat(extractPath); err == nil {
			return info.Size()
		}
		return 0
	}()
	
	result.Status = HealthStatusHealthy
	result.Message = "extract.go is available and accessible"
	result.Duration = time.Since(start)
	
	return result
}

// diagnosConvertExecutable tests the convert.go executable
func (hm *HealthMonitor) diagnosConvertExecutable() DiagnosticResult {
	start := time.Now()
	result := DiagnosticResult{
		Name:      "convert_executable",
		Timestamp: start,
		Details:   make(map[string]interface{}),
	}
	
	convertPath := "app/extraction/convert/convert.go"
	
	// Check if file exists
	if _, err := os.Stat(convertPath); os.IsNotExist(err) {
		result.Status = HealthStatusUnhealthy
		result.Message = "convert.go not found"
		result.Duration = time.Since(start)
		return result
	}
	
	result.Details["path"] = convertPath
	result.Details["size_bytes"] = func() int64 {
		if info, err := os.Stat(convertPath); err == nil {
			return info.Size()
		}
		return 0
	}()
	
	result.Status = HealthStatusHealthy
	result.Message = "convert.go is available and accessible"
	result.Duration = time.Since(start)
	
	return result
}

// diagnosDirectoryStructure validates critical directory structure
func (hm *HealthMonitor) diagnosDirectoryStructure() DiagnosticResult {
	start := time.Now()
	result := DiagnosticResult{
		Name:      "directory_structure",
		Timestamp: start,
		Details:   make(map[string]interface{}),
	}
	
	requiredDirs := []string{
		"app/extraction/files/all",
		"app/extraction/files/pass",
		"app/extraction/files/txt",
		"app/extraction/files/done",
		"app/extraction/files/errors",
		"app/extraction/files/nopass",
		"app/extraction/files/etbanks",
		"temp",
		"data",
		"logs",
	}
	
	missingDirs := make([]string, 0)
	existingDirs := make([]string, 0)
	
	for _, dir := range requiredDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			missingDirs = append(missingDirs, dir)
		} else {
			existingDirs = append(existingDirs, dir)
		}
	}
	
	result.Details["required_directories"] = len(requiredDirs)
	result.Details["existing_directories"] = len(existingDirs)
	result.Details["missing_directories"] = missingDirs
	
	if len(missingDirs) > 0 {
		if len(missingDirs) > len(requiredDirs)/2 {
			result.Status = HealthStatusUnhealthy
			result.Message = fmt.Sprintf("Critical directory structure missing: %d/%d directories not found", len(missingDirs), len(requiredDirs))
		} else {
			result.Status = HealthStatusDegraded
			result.Message = fmt.Sprintf("Some directories missing: %v", missingDirs)
		}
	} else {
		result.Status = HealthStatusHealthy
		result.Message = "All required directories exist and are accessible"
	}
	
	result.Duration = time.Since(start)
	return result
}

// diagnosDatabaseIntegrity checks database health and integrity
func (hm *HealthMonitor) diagnosDatabaseIntegrity() DiagnosticResult {
	start := time.Now()
	result := DiagnosticResult{
		Name:      "database_integrity",
		Timestamp: start,
		Details:   make(map[string]interface{}),
	}
	
	if hm.taskStore == nil {
		result.Status = HealthStatusUnhealthy
		result.Message = "TaskStore not initialized"
		result.Duration = time.Since(start)
		return result
	}
	
	// Test basic database operations
	stats, err := hm.taskStore.GetStats()
	if err != nil {
		result.Status = HealthStatusUnhealthy
		result.Message = fmt.Sprintf("Database query failed: %v", err)
		result.Duration = time.Since(start)
		return result
	}
	
	totalTasks := 0
	for _, count := range stats {
		totalTasks += count
	}
	
	result.Details["total_tasks"] = totalTasks
	result.Details["task_breakdown"] = stats
	result.Status = HealthStatusHealthy
	result.Message = fmt.Sprintf("Database operational with %d total tasks", totalTasks)
	result.Duration = time.Since(start)
	
	return result
}

// diagnosPasswordFile validates the password file for extraction
func (hm *HealthMonitor) diagnosPasswordFile() DiagnosticResult {
	start := time.Now()
	result := DiagnosticResult{
		Name:      "password_file",
		Timestamp: start,
		Details:   make(map[string]interface{}),
	}
	
	passPath := "app/extraction/pass.txt"
	
	if _, err := os.Stat(passPath); os.IsNotExist(err) {
		result.Status = HealthStatusDegraded
		result.Message = "pass.txt not found - extraction may fail for password-protected archives"
		result.Duration = time.Since(start)
		return result
	}
	
	// Check file size and readability
	if info, err := os.Stat(passPath); err == nil {
		result.Details["file_size"] = info.Size()
		result.Details["last_modified"] = info.ModTime()
		
		if info.Size() == 0 {
			result.Status = HealthStatusDegraded
			result.Message = "pass.txt is empty - no passwords available for extraction"
		} else {
			result.Status = HealthStatusHealthy
			result.Message = fmt.Sprintf("pass.txt available with %d bytes", info.Size())
		}
	} else {
		result.Status = HealthStatusDegraded
		result.Message = fmt.Sprintf("Cannot read pass.txt: %v", err)
	}
	
	result.Duration = time.Since(start)
	return result
}

// diagnosDiskSpaceProjections analyzes disk usage trends
func (hm *HealthMonitor) diagnosDiskSpaceProjections() DiagnosticResult {
	start := time.Now()
	result := DiagnosticResult{
		Name:      "disk_space_projections",
		Timestamp: start,
		Details:   make(map[string]interface{}),
	}
	
	// Get current disk usage
	snapshot := hm.GetLastSystemSnapshot()
	if snapshot == nil || len(snapshot.Disk) == 0 {
		result.Status = HealthStatusDegraded
		result.Message = "Cannot assess disk space - no system snapshot available"
		result.Duration = time.Since(start)
		return result
	}
	
	criticalPaths := 0
	warningPaths := 0
	totalPaths := len(snapshot.Disk)
	
	for path, disk := range snapshot.Disk {
		result.Details[fmt.Sprintf("%s_usage_percent", path)] = disk.UsedPercent
		result.Details[fmt.Sprintf("%s_free_mb", path)] = float64(disk.FreeBytes) / 1024 / 1024
		
		if disk.UsedPercent > 90 {
			criticalPaths++
		} else if disk.UsedPercent > 80 {
			warningPaths++
		}
	}
	
	result.Details["total_paths_checked"] = totalPaths
	result.Details["warning_paths"] = warningPaths
	result.Details["critical_paths"] = criticalPaths
	
	if criticalPaths > 0 {
		result.Status = HealthStatusUnhealthy
		result.Message = fmt.Sprintf("Critical disk space: %d paths above 90%% usage", criticalPaths)
	} else if warningPaths > 0 {
		result.Status = HealthStatusDegraded
		result.Message = fmt.Sprintf("Warning disk space: %d paths above 80%% usage", warningPaths)
	} else {
		result.Status = HealthStatusHealthy
		result.Message = "Disk space levels are healthy across all monitored paths"
	}
	
	result.Duration = time.Since(start)
	return result
}

// diagnosTelegramConnectivity tests basic connectivity (placeholder)
func (hm *HealthMonitor) diagnosTelegramConnectivity() DiagnosticResult {
	start := time.Now()
	result := DiagnosticResult{
		Name:      "telegram_connectivity",
		Timestamp: start,
		Details:   make(map[string]interface{}),
	}
	
	// Note: This is a placeholder - in a real implementation, we might:
	// - Test getMe API call
	// - Check webhook status
	// - Verify rate limiting status
	// For now, we'll assume healthy if no major errors are logged
	
	result.Status = HealthStatusHealthy
	result.Message = "Telegram connectivity assumed healthy (detailed check not implemented)"
	result.Details["note"] = "Placeholder diagnostic - implement actual API test"
	result.Duration = time.Since(start)
	
	return result
}

// GetLastDiagnostics returns the most recent diagnostic suite
func (hm *HealthMonitor) GetLastDiagnostics() *DiagnosticSuite {
	hm.checkMutex.RLock()
	defer hm.checkMutex.RUnlock()
	return hm.lastDiagnostics
}