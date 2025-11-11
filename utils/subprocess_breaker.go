package utils

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// SubprocessCircuitBreaker provides circuit breaker functionality specifically for external subprocess calls
type SubprocessCircuitBreaker struct {
	registry       *CircuitBreakerRegistry
	logger         *Logger
	processConfigs map[string]*CircuitBreakerConfig
}

// NewSubprocessCircuitBreaker creates a new subprocess circuit breaker
func NewSubprocessCircuitBreaker(logger *Logger) *SubprocessCircuitBreaker {
	return &SubprocessCircuitBreaker{
		registry: NewCircuitBreakerRegistry(logger),
		logger:   logger,
		processConfigs: map[string]*CircuitBreakerConfig{
			"extract": {
				FailureThreshold:  2,  // Extract processes are critical
				FailureWindow:     3 * time.Minute,
				RecoveryTimeout:   2 * time.Minute,
				HalfOpenMaxCalls:  1,  // Single test call
				MinimumCalls:      1,
				SuccessThreshold:  1.0, // 100% success required for extract
			},
			"convert": {
				FailureThreshold:  3,  // Convert is more tolerant
				FailureWindow:     5 * time.Minute,
				RecoveryTimeout:   90 * time.Second,
				HalfOpenMaxCalls:  2,
				MinimumCalls:      2,
				SuccessThreshold:  0.5, // 50% success rate acceptable
			},
			"default": ProcessCircuitBreakerConfig(),
		},
	}
}

// ExecuteCommand runs a command with circuit breaker protection
func (scb *SubprocessCircuitBreaker) ExecuteCommand(ctx context.Context, processName string, cmd *exec.Cmd, description string) error {
	config := scb.getConfigForProcess(processName)
	breaker := scb.registry.GetOrCreate(processName, config)
	
	return breaker.Execute(ctx, func() error {
		return scb.runCommandWithTimeout(ctx, cmd)
	}, description)
}

// ExecuteWithRetry combines circuit breaker protection with retry logic
func (scb *SubprocessCircuitBreaker) ExecuteWithRetry(ctx context.Context, processName string, cmdFunc func() *exec.Cmd, description string, retryService *EnhancedRetryService) error {
	config := scb.getConfigForProcess(processName)
	breaker := scb.registry.GetOrCreate(processName, config)
	
	// Check circuit breaker state before attempting retries
	if breaker.GetState() == StateOpen {
		return fmt.Errorf("circuit breaker for %s is open, skipping retry attempts: %w", processName, ErrCircuitBreakerOpen)
	}
	
	operationContext := map[string]interface{}{
		"process_name":      processName,
		"circuit_breaker":   breaker.GetState(),
		"description":       description,
	}
	
	return retryService.ExecuteWithCategoryOptimization(ctx, func() error {
		cmd := cmdFunc()
		return scb.ExecuteCommand(ctx, processName, cmd, description)
	}, fmt.Sprintf("subprocess_%s_%s", processName, description), operationContext)
}

// runCommandWithTimeout executes a command with proper timeout handling
func (scb *SubprocessCircuitBreaker) runCommandWithTimeout(ctx context.Context, cmd *exec.Cmd) error {
	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}
	
	// Create a channel for the command result
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	
	// Wait for completion or timeout
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("command failed: %w", err)
		}
		return nil
	case <-ctx.Done():
		// Kill the process if context is cancelled
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return fmt.Errorf("command timed out: %w", ctx.Err())
	}
}

// getConfigForProcess returns the appropriate configuration for a given process
func (scb *SubprocessCircuitBreaker) getConfigForProcess(processName string) *CircuitBreakerConfig {
	if config, exists := scb.processConfigs[processName]; exists {
		return config
	}
	return scb.processConfigs["default"]
}

// GetBreaker returns the circuit breaker for a specific process
func (scb *SubprocessCircuitBreaker) GetBreaker(processName string) (*CircuitBreaker, bool) {
	return scb.registry.Get(processName)
}

// GetAllBreakers returns all process circuit breakers
func (scb *SubprocessCircuitBreaker) GetAllBreakers() map[string]*CircuitBreaker {
	return scb.registry.GetAll()
}

// GetMetrics returns metrics for all subprocess circuit breakers
func (scb *SubprocessCircuitBreaker) GetMetrics() map[string]map[string]interface{} {
	return scb.registry.GetAllMetrics()
}

// ResetBreaker manually resets a specific circuit breaker
func (scb *SubprocessCircuitBreaker) ResetBreaker(processName string) error {
	breaker, exists := scb.registry.Get(processName)
	if !exists {
		return fmt.Errorf("circuit breaker for process %s not found", processName)
	}
	
	breaker.Reset()
	
	scb.logger.WithField("process", processName).
		Info("Reset subprocess circuit breaker")
	
	return nil
}

// ResetAllBreakers resets all subprocess circuit breakers
func (scb *SubprocessCircuitBreaker) ResetAllBreakers() {
	scb.registry.ResetAll()
	
	scb.logger.Info("Reset all subprocess circuit breakers")
}

// ForceOpenBreaker manually opens a specific circuit breaker
func (scb *SubprocessCircuitBreaker) ForceOpenBreaker(processName string) error {
	breaker, exists := scb.registry.Get(processName)
	if !exists {
		return fmt.Errorf("circuit breaker for process %s not found", processName)
	}
	
	breaker.ForceOpen()
	
	scb.logger.WithField("process", processName).
		Warn("Manually opened subprocess circuit breaker")
	
	return nil
}

// GetBreakerStatus returns a human-readable status for all circuit breakers
func (scb *SubprocessCircuitBreaker) GetBreakerStatus() map[string]interface{} {
	metrics := scb.GetMetrics()
	status := make(map[string]interface{})
	
	for processName, processMetrics := range metrics {
		state := processMetrics["state"].(CircuitBreakerState)
		totalCalls := processMetrics["total_calls"].(int64)
		totalFailures := processMetrics["total_failures"].(int64)
		failureRate := processMetrics["failure_rate"].(float64)
		
		var healthStatus string
		switch state {
		case StateClosed:
			if failureRate < 0.1 {
				healthStatus = "healthy"
			} else {
				healthStatus = "degraded"
			}
		case StateHalfOpen:
			healthStatus = "recovering"
		case StateOpen:
			healthStatus = "unhealthy"
		}
		
		status[processName] = map[string]interface{}{
			"state":         string(state),
			"health":        healthStatus,
			"total_calls":   totalCalls,
			"total_failures": totalFailures,
			"failure_rate":  fmt.Sprintf("%.1f%%", failureRate*100),
			"last_failure":  processMetrics["last_failure"],
			"last_attempt":  processMetrics["last_attempt"],
		}
	}
	
	return status
}

// CheckHealth performs a health check on all subprocess circuit breakers
func (scb *SubprocessCircuitBreaker) CheckHealth() (bool, []string) {
	metrics := scb.GetMetrics()
	var issues []string
	healthy := true
	
	for processName, processMetrics := range metrics {
		state := processMetrics["state"].(CircuitBreakerState)
		failureRate := processMetrics["failure_rate"].(float64)
		recentFailures := processMetrics["recent_failures"].(int)
		
		if state == StateOpen {
			issues = append(issues, fmt.Sprintf("Circuit breaker for %s is OPEN", processName))
			healthy = false
		} else if state == StateHalfOpen {
			issues = append(issues, fmt.Sprintf("Circuit breaker for %s is HALF-OPEN (recovering)", processName))
		} else if failureRate > 0.5 {
			issues = append(issues, fmt.Sprintf("High failure rate for %s: %.1f%%", processName, failureRate*100))
		}
		
		if recentFailures >= 3 {
			issues = append(issues, fmt.Sprintf("Multiple recent failures for %s: %d", processName, recentFailures))
		}
	}
	
	return healthy, issues
}

// ProcessCommand represents a command for a specific process
type ProcessCommand struct {
	ProcessName string
	Cmd         *exec.Cmd
	Description string
}

// NewProcessCommand creates a new process command
func NewProcessCommand(processName string, cmd *exec.Cmd, description string) *ProcessCommand {
	return &ProcessCommand{
		ProcessName: processName,
		Cmd:         cmd,
		Description: description,
	}
}

// ExecuteProcessCommand executes a process command with circuit breaker protection
func (scb *SubprocessCircuitBreaker) ExecuteProcessCommand(ctx context.Context, processCmd *ProcessCommand) error {
	return scb.ExecuteCommand(ctx, processCmd.ProcessName, processCmd.Cmd, processCmd.Description)
}

// BulkExecute executes multiple process commands with individual circuit breaker protection
func (scb *SubprocessCircuitBreaker) BulkExecute(ctx context.Context, commands []*ProcessCommand) map[string]error {
	results := make(map[string]error)
	
	for _, cmd := range commands {
		err := scb.ExecuteProcessCommand(ctx, cmd)
		results[fmt.Sprintf("%s_%s", cmd.ProcessName, cmd.Description)] = err
		
		if err != nil {
			scb.logger.WithField("process", cmd.ProcessName).
				WithField("description", cmd.Description).
				WithField("error", err).
				Error("Bulk process command failed")
		}
	}
	
	return results
}