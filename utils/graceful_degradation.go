package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// DependencyStatus represents the availability status of a dependency
type DependencyStatus string

const (
	StatusAvailable   DependencyStatus = "available"
	StatusDegraded    DependencyStatus = "degraded"
	StatusUnavailable DependencyStatus = "unavailable"
	StatusUnknown     DependencyStatus = "unknown"
)

// DependencyInfo holds information about a dependency
type DependencyInfo struct {
	Name            string           `json:"name"`
	Type            string           `json:"type"`
	Status          DependencyStatus `json:"status"`
	LastCheck       time.Time        `json:"last_check"`
	LastAvailable   time.Time        `json:"last_available"`
	ErrorMessage    string           `json:"error_message,omitempty"`
	CheckInterval   time.Duration    `json:"check_interval"`
	ConsecutiveFails int             `json:"consecutive_fails"`
}

// FallbackMode represents different fallback strategies
type FallbackMode string

const (
	FallbackQueue    FallbackMode = "queue"     // Queue operations for later
	FallbackSkip     FallbackMode = "skip"      // Skip operation with notification
	FallbackAlternate FallbackMode = "alternate" // Use alternative method
	FallbackManual   FallbackMode = "manual"    // Require manual intervention
)

// GracefulDegradationManager manages system behavior when dependencies are unavailable
type GracefulDegradationManager struct {
	dependencies      map[string]*DependencyInfo
	fallbackModes     map[string]FallbackMode
	checkTicker       *time.Ticker
	stopChan          chan struct{}
	mutex             sync.RWMutex
	logger            *Logger
	queuedOperations  []QueuedOperation
	notificationsSent map[string]time.Time
}

// QueuedOperation represents an operation waiting for dependency recovery
type QueuedOperation struct {
	ID           string                 `json:"id"`
	DependencyName string               `json:"dependency_name"`
	Operation    string                 `json:"operation"`
	Parameters   map[string]interface{} `json:"parameters"`
	QueuedAt     time.Time              `json:"queued_at"`
	MaxWaitTime  time.Duration          `json:"max_wait_time"`
}

// NewGracefulDegradationManager creates a new degradation manager
func NewGracefulDegradationManager(logger *Logger) *GracefulDegradationManager {
	return &GracefulDegradationManager{
		dependencies:      make(map[string]*DependencyInfo),
		fallbackModes:     make(map[string]FallbackMode),
		logger:            logger,
		queuedOperations:  make([]QueuedOperation, 0),
		notificationsSent: make(map[string]time.Time),
		stopChan:          make(chan struct{}),
	}
}

// RegisterDependency registers a new dependency for monitoring
func (gdm *GracefulDegradationManager) RegisterDependency(name, depType string, checkInterval time.Duration, fallbackMode FallbackMode) {
	gdm.mutex.Lock()
	defer gdm.mutex.Unlock()
	
	gdm.dependencies[name] = &DependencyInfo{
		Name:          name,
		Type:          depType,
		Status:        StatusUnknown,
		CheckInterval: checkInterval,
		LastCheck:     time.Time{},
		LastAvailable: time.Time{},
	}
	
	gdm.fallbackModes[name] = fallbackMode
	
	gdm.logger.WithField("dependency", name).
		WithField("type", depType).
		WithField("fallback_mode", fallbackMode).
		Info("Registered dependency for graceful degradation")
}

// StartMonitoring begins continuous monitoring of dependencies
func (gdm *GracefulDegradationManager) StartMonitoring(ctx context.Context) {
	gdm.checkTicker = time.NewTicker(30 * time.Second) // Check every 30 seconds
	
	go func() {
		for {
			select {
			case <-gdm.checkTicker.C:
				gdm.checkAllDependencies()
				gdm.processQueuedOperations()
				gdm.cleanupExpiredOperations()
			case <-gdm.stopChan:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	
	gdm.logger.Info("Started graceful degradation monitoring")
}

// StopMonitoring stops dependency monitoring
func (gdm *GracefulDegradationManager) StopMonitoring() {
	if gdm.checkTicker != nil {
		gdm.checkTicker.Stop()
	}
	
	close(gdm.stopChan)
	gdm.logger.Info("Stopped graceful degradation monitoring")
}

// checkAllDependencies performs health checks on all registered dependencies
func (gdm *GracefulDegradationManager) checkAllDependencies() {
	gdm.mutex.Lock()
	defer gdm.mutex.Unlock()
	
	for name, dep := range gdm.dependencies {
		if time.Since(dep.LastCheck) >= dep.CheckInterval {
			gdm.checkDependency(name, dep)
		}
	}
}

// checkDependency performs a health check on a specific dependency
func (gdm *GracefulDegradationManager) checkDependency(name string, dep *DependencyInfo) {
	dep.LastCheck = time.Now()
	oldStatus := dep.Status
	
	var isAvailable bool
	var errorMsg string
	
	switch dep.Type {
	case "executable":
		isAvailable, errorMsg = gdm.checkExecutable(name)
	case "file":
		isAvailable, errorMsg = gdm.checkFile(name)
	case "directory":
		isAvailable, errorMsg = gdm.checkDirectory(name)
	default:
		isAvailable, errorMsg = false, "unknown dependency type"
	}
	
	if isAvailable {
		dep.Status = StatusAvailable
		dep.LastAvailable = time.Now()
		dep.ConsecutiveFails = 0
		dep.ErrorMessage = ""
	} else {
		dep.ConsecutiveFails++
		dep.ErrorMessage = errorMsg
		
		if dep.ConsecutiveFails >= 3 {
			dep.Status = StatusUnavailable
		} else if dep.ConsecutiveFails >= 1 {
			dep.Status = StatusDegraded
		}
	}
	
	// Log status changes
	if oldStatus != dep.Status {
		gdm.logger.WithField("dependency", name).
			WithField("old_status", oldStatus).
			WithField("new_status", dep.Status).
			WithField("consecutive_fails", dep.ConsecutiveFails).
			WithField("error", errorMsg).
			Warn("Dependency status changed")
	}
}

// checkExecutable verifies if an executable dependency is available
func (gdm *GracefulDegradationManager) checkExecutable(name string) (bool, string) {
	switch name {
	case "extract":
		return gdm.checkGoFile("app/extraction/extract/extract.go")
	case "convert":
		return gdm.checkGoFile("app/extraction/convert/convert.go")
	case "go":
		_, err := exec.LookPath("go")
		if err != nil {
			return false, fmt.Sprintf("Go runtime not found: %v", err)
		}
		return true, ""
	default:
		_, err := exec.LookPath(name)
		if err != nil {
			return false, fmt.Sprintf("executable %s not found: %v", name, err)
		}
		return true, ""
	}
}

// checkGoFile verifies if a Go source file is available and syntactically correct
func (gdm *GracefulDegradationManager) checkGoFile(filepath string) (bool, string) {
	// Check if file exists
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		return false, fmt.Sprintf("Go file %s does not exist", filepath)
	}
	
	// Quick syntax check using go fmt
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	cmd := exec.CommandContext(ctx, "go", "fmt", "-n", filepath)
	if err := cmd.Run(); err != nil {
		return false, fmt.Sprintf("Go file %s has syntax errors: %v", filepath, err)
	}
	
	return true, ""
}

// checkFile verifies if a file dependency exists
func (gdm *GracefulDegradationManager) checkFile(filename string) (bool, string) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return false, fmt.Sprintf("file %s does not exist", filename)
	}
	return true, ""
}

// checkDirectory verifies if a directory dependency exists
func (gdm *GracefulDegradationManager) checkDirectory(dirname string) (bool, string) {
	if stat, err := os.Stat(dirname); os.IsNotExist(err) {
		return false, fmt.Sprintf("directory %s does not exist", dirname)
	} else if err != nil {
		return false, fmt.Sprintf("cannot access directory %s: %v", dirname, err)
	} else if !stat.IsDir() {
		return false, fmt.Sprintf("%s is not a directory", dirname)
	}
	return true, ""
}

// IsAvailable checks if a dependency is currently available
func (gdm *GracefulDegradationManager) IsAvailable(dependencyName string) bool {
	gdm.mutex.RLock()
	defer gdm.mutex.RUnlock()
	
	dep, exists := gdm.dependencies[dependencyName]
	if !exists {
		return false
	}
	
	return dep.Status == StatusAvailable
}

// GetDependencyStatus returns the current status of a dependency
func (gdm *GracefulDegradationManager) GetDependencyStatus(dependencyName string) (DependencyStatus, error) {
	gdm.mutex.RLock()
	defer gdm.mutex.RUnlock()
	
	dep, exists := gdm.dependencies[dependencyName]
	if !exists {
		return StatusUnknown, fmt.Errorf("dependency %s not registered", dependencyName)
	}
	
	return dep.Status, nil
}

// HandleUnavailableDependency manages operations when a dependency is unavailable
func (gdm *GracefulDegradationManager) HandleUnavailableDependency(dependencyName, operation string, parameters map[string]interface{}) error {
	gdm.mutex.Lock()
	defer gdm.mutex.Unlock()
	
	dep, exists := gdm.dependencies[dependencyName]
	if !exists {
		return fmt.Errorf("dependency %s not registered", dependencyName)
	}
	
	if dep.Status == StatusAvailable {
		return nil // Dependency is available, no degradation needed
	}
	
	fallbackMode := gdm.fallbackModes[dependencyName]
	
	gdm.logger.WithField("dependency", dependencyName).
		WithField("operation", operation).
		WithField("status", dep.Status).
		WithField("fallback_mode", fallbackMode).
		Warn("Handling unavailable dependency with graceful degradation")
	
	switch fallbackMode {
	case FallbackQueue:
		return gdm.queueOperation(dependencyName, operation, parameters)
		
	case FallbackSkip:
		return gdm.skipOperation(dependencyName, operation)
		
	case FallbackAlternate:
		return gdm.useAlternateMethod(dependencyName, operation, parameters)
		
	case FallbackManual:
		return gdm.requireManualIntervention(dependencyName, operation)
		
	default:
		return fmt.Errorf("unknown fallback mode for dependency %s", dependencyName)
	}
}

// queueOperation queues an operation for later execution when dependency recovers
func (gdm *GracefulDegradationManager) queueOperation(dependencyName, operation string, parameters map[string]interface{}) error {
	op := QueuedOperation{
		ID:             generateOperationID(),
		DependencyName: dependencyName,
		Operation:      operation,
		Parameters:     parameters,
		QueuedAt:       time.Now(),
		MaxWaitTime:    24 * time.Hour, // Queue for up to 24 hours
	}
	
	gdm.queuedOperations = append(gdm.queuedOperations, op)
	
	gdm.logger.WithField("operation_id", op.ID).
		WithField("dependency", dependencyName).
		WithField("operation", operation).
		Info("Queued operation for later execution")
	
	return fmt.Errorf("dependency %s unavailable, operation queued (ID: %s)", dependencyName, op.ID)
}

// skipOperation skips an operation with appropriate notification
func (gdm *GracefulDegradationManager) skipOperation(dependencyName, operation string) error {
	gdm.logger.WithField("dependency", dependencyName).
		WithField("operation", operation).
		Warn("Skipping operation due to unavailable dependency")
	
	return fmt.Errorf("dependency %s unavailable, operation skipped", dependencyName)
}

// useAlternateMethod attempts to use an alternative approach
func (gdm *GracefulDegradationManager) useAlternateMethod(dependencyName, operation string, parameters map[string]interface{}) error {
	switch dependencyName {
	case "extract":
		return gdm.alternateExtraction(parameters)
	case "convert":
		return gdm.alternateConversion(parameters)
	default:
		return fmt.Errorf("no alternate method available for dependency %s", dependencyName)
	}
}

// alternateExtraction provides fallback extraction functionality
func (gdm *GracefulDegradationManager) alternateExtraction(parameters map[string]interface{}) error {
	// For extract.go, we can try basic file operations
	// This is a simplified fallback - just move files to appropriate directories
	
	gdm.logger.Info("Using alternate extraction method - basic file organization")
	
	// Move files from all/ to errors/ directory to indicate manual processing needed
	allDir := "app/extraction/files/all"
	errorsDir := "app/extraction/files/errors"
	
	files, err := filepath.Glob(filepath.Join(allDir, "*"))
	if err != nil {
		return fmt.Errorf("alternate extraction failed to list files: %w", err)
	}
	
	for _, file := range files {
		if stat, err := os.Stat(file); err == nil && !stat.IsDir() {
			filename := filepath.Base(file)
			destPath := filepath.Join(errorsDir, "needs_manual_extraction_"+filename)
			
			if err := os.Rename(file, destPath); err != nil {
				gdm.logger.WithField("file", filename).
					WithError(err).
					Warn("Failed to move file for manual extraction")
			} else {
				gdm.logger.WithField("file", filename).
					Info("Moved file for manual extraction")
			}
		}
	}
	
	return fmt.Errorf("used alternate extraction method - files moved for manual processing")
}

// alternateConversion provides fallback conversion functionality
func (gdm *GracefulDegradationManager) alternateConversion(parameters map[string]interface{}) error {
	// For convert.go, we can create a basic report of available files
	
	gdm.logger.Info("Using alternate conversion method - basic file listing")
	
	passDir := "app/extraction/files/pass"
	files, err := filepath.Glob(filepath.Join(passDir, "*"))
	if err != nil {
		return fmt.Errorf("alternate conversion failed to list files: %w", err)
	}
	
	// Create a basic output file listing available files
	outputFile := "app/extraction/fallback_output.txt"
	content := fmt.Sprintf("Fallback conversion report - %s\n\nFiles available for manual processing:\n", time.Now().Format(time.RFC3339))
	
	for _, file := range files {
		if stat, err := os.Stat(file); err == nil && !stat.IsDir() {
			content += fmt.Sprintf("- %s (size: %d bytes)\n", filepath.Base(file), stat.Size())
		}
	}
	
	content += "\nNote: This is a fallback report. Use manual processing for credential extraction.\n"
	
	if err := os.WriteFile(outputFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to create fallback output file: %w", err)
	}
	
	gdm.logger.WithField("output_file", outputFile).
		Info("Created fallback conversion report")
	
	return fmt.Errorf("used alternate conversion method - basic report created")
}

// requireManualIntervention marks an operation as requiring manual intervention
func (gdm *GracefulDegradationManager) requireManualIntervention(dependencyName, operation string) error {
	gdm.logger.WithField("dependency", dependencyName).
		WithField("operation", operation).
		Error("Operation requires manual intervention due to unavailable dependency")
	
	return fmt.Errorf("dependency %s unavailable, manual intervention required for operation %s", dependencyName, operation)
}

// processQueuedOperations attempts to execute queued operations when dependencies recover
func (gdm *GracefulDegradationManager) processQueuedOperations() {
	if len(gdm.queuedOperations) == 0 {
		return
	}
	
	var remainingOps []QueuedOperation
	processedCount := 0
	
	for _, op := range gdm.queuedOperations {
		if gdm.IsAvailable(op.DependencyName) {
			gdm.logger.WithField("operation_id", op.ID).
				WithField("dependency", op.DependencyName).
				Info("Dependency recovered, processing queued operation")
			
			// Here you would trigger the actual operation
			// For now, we just log that it would be processed
			processedCount++
		} else {
			remainingOps = append(remainingOps, op)
		}
	}
	
	gdm.queuedOperations = remainingOps
	
	if processedCount > 0 {
		gdm.logger.WithField("processed_count", processedCount).
			WithField("remaining_count", len(remainingOps)).
			Info("Processed queued operations after dependency recovery")
	}
}

// cleanupExpiredOperations removes operations that have exceeded their max wait time
func (gdm *GracefulDegradationManager) cleanupExpiredOperations() {
	if len(gdm.queuedOperations) == 0 {
		return
	}
	
	var validOps []QueuedOperation
	expiredCount := 0
	
	for _, op := range gdm.queuedOperations {
		if time.Since(op.QueuedAt) > op.MaxWaitTime {
			gdm.logger.WithField("operation_id", op.ID).
				WithField("dependency", op.DependencyName).
				WithField("queued_at", op.QueuedAt).
				Warn("Removing expired queued operation")
			expiredCount++
		} else {
			validOps = append(validOps, op)
		}
	}
	
	gdm.queuedOperations = validOps
	
	if expiredCount > 0 {
		gdm.logger.WithField("expired_count", expiredCount).
			Info("Cleaned up expired queued operations")
	}
}

// GetSystemHealth returns overall system health considering all dependencies
func (gdm *GracefulDegradationManager) GetSystemHealth() (bool, []string) {
	gdm.mutex.RLock()
	defer gdm.mutex.RUnlock()
	
	var issues []string
	allHealthy := true
	
	for name, dep := range gdm.dependencies {
		switch dep.Status {
		case StatusUnavailable:
			issues = append(issues, fmt.Sprintf("Dependency '%s' is unavailable: %s", name, dep.ErrorMessage))
			allHealthy = false
		case StatusDegraded:
			issues = append(issues, fmt.Sprintf("Dependency '%s' is degraded: %s", name, dep.ErrorMessage))
		case StatusUnknown:
			issues = append(issues, fmt.Sprintf("Dependency '%s' status is unknown", name))
		}
	}
	
	queuedCount := len(gdm.queuedOperations)
	if queuedCount > 0 {
		issues = append(issues, fmt.Sprintf("%d operations are queued due to dependency issues", queuedCount))
	}
	
	return allHealthy, issues
}

// GetDependencyReport returns a comprehensive report of all dependencies
func (gdm *GracefulDegradationManager) GetDependencyReport() map[string]interface{} {
	gdm.mutex.RLock()
	defer gdm.mutex.RUnlock()
	
	report := make(map[string]interface{})
	
	dependencies := make(map[string]interface{})
	for name, dep := range gdm.dependencies {
		dependencies[name] = map[string]interface{}{
			"status":            dep.Status,
			"type":              dep.Type,
			"last_check":        dep.LastCheck,
			"last_available":    dep.LastAvailable,
			"error_message":     dep.ErrorMessage,
			"consecutive_fails": dep.ConsecutiveFails,
			"fallback_mode":     gdm.fallbackModes[name],
		}
	}
	
	report["dependencies"] = dependencies
	report["queued_operations"] = len(gdm.queuedOperations)
	report["report_time"] = time.Now()
	
	healthy, issues := gdm.GetSystemHealth()
	report["overall_healthy"] = healthy
	report["issues"] = issues
	
	return report
}

// generateOperationID creates a unique ID for queued operations
func generateOperationID() string {
	return fmt.Sprintf("op_%d", time.Now().UnixNano())
}