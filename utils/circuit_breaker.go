package utils

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// CircuitBreakerState represents the current state of a circuit breaker
type CircuitBreakerState string

const (
	StateClosed   CircuitBreakerState = "closed"   // Normal operation
	StateOpen     CircuitBreakerState = "open"     // Circuit is open, rejecting calls
	StateHalfOpen CircuitBreakerState = "half_open" // Testing if service has recovered
)

// CircuitBreakerConfig holds configuration for a circuit breaker
type CircuitBreakerConfig struct {
	// Maximum number of failures before opening the circuit
	FailureThreshold int
	
	// Time window for counting failures
	FailureWindow time.Duration
	
	// How long to wait before attempting to close the circuit
	RecoveryTimeout time.Duration
	
	// Maximum number of test calls in half-open state
	HalfOpenMaxCalls int
	
	// Minimum number of calls required before considering failure rate
	MinimumCalls int
	
	// Success threshold percentage to close circuit (0.0-1.0)
	SuccessThreshold float64
}

// DefaultCircuitBreakerConfig returns a sensible default configuration
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		FailureThreshold:  5,
		FailureWindow:     1 * time.Minute,
		RecoveryTimeout:   30 * time.Second,
		HalfOpenMaxCalls:  3,
		MinimumCalls:      3,
		SuccessThreshold:  0.6, // 60% success rate required
	}
}

// ProcessCircuitBreakerConfig returns configuration optimized for external processes
func ProcessCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		FailureThreshold:  3,  // Processes can be fragile
		FailureWindow:     2 * time.Minute,
		RecoveryTimeout:   1 * time.Minute,
		HalfOpenMaxCalls:  2,  // Conservative testing
		MinimumCalls:      2,
		SuccessThreshold:  0.5, // 50% success rate for processes
	}
}

// CallResult represents the result of a circuit breaker protected call
type CallResult struct {
	Success   bool
	Duration  time.Duration
	Error     error
	Timestamp time.Time
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	name        string
	config      *CircuitBreakerConfig
	state       CircuitBreakerState
	failures    []time.Time
	successes   []time.Time
	lastFailure time.Time
	lastAttempt time.Time
	halfOpenCalls int
	mutex       sync.RWMutex
	logger      *Logger
	
	// Metrics
	totalCalls    int64
	totalFailures int64
	totalSuccesses int64
	stateChanges  map[CircuitBreakerState]int64
}

// NewCircuitBreaker creates a new circuit breaker with the given configuration
func NewCircuitBreaker(name string, config *CircuitBreakerConfig, logger *Logger) *CircuitBreaker {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}
	
	return &CircuitBreaker{
		name:         name,
		config:       config,
		state:        StateClosed,
		failures:     make([]time.Time, 0),
		successes:    make([]time.Time, 0),
		logger:       logger,
		stateChanges: make(map[CircuitBreakerState]int64),
	}
}

// Execute runs the given function with circuit breaker protection
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error, description string) error {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	
	cb.totalCalls++
	
	// Check if we should allow the call
	if !cb.allowCall() {
		cb.logger.WithField("circuit_breaker", cb.name).
			WithField("state", cb.state).
			WithField("description", description).
			Debug("Circuit breaker rejected call")
		return fmt.Errorf("circuit breaker %s is %s: %w", cb.name, cb.state, ErrCircuitBreakerOpen)
	}
	
	start := time.Now()
	err := fn()
	duration := time.Since(start)
	
	result := &CallResult{
		Success:   err == nil,
		Duration:  duration,
		Error:     err,
		Timestamp: start,
	}
	
	cb.recordResult(result, description)
	
	return err
}

// allowCall determines if a call should be allowed based on current state
func (cb *CircuitBreaker) allowCall() bool {
	now := time.Now()
	
	switch cb.state {
	case StateClosed:
		return true
		
	case StateOpen:
		// Check if recovery timeout has passed
		if now.Sub(cb.lastFailure) >= cb.config.RecoveryTimeout {
			cb.transitionTo(StateHalfOpen)
			cb.halfOpenCalls = 0
			return true
		}
		return false
		
	case StateHalfOpen:
		// Allow limited calls to test recovery
		if cb.halfOpenCalls < cb.config.HalfOpenMaxCalls {
			cb.halfOpenCalls++
			return true
		}
		return false
		
	default:
		return false
	}
}

// recordResult processes the result of a call and updates circuit breaker state
func (cb *CircuitBreaker) recordResult(result *CallResult, description string) {
	now := time.Now()
	cb.lastAttempt = now
	
	if result.Success {
		cb.totalSuccesses++
		cb.successes = append(cb.successes, result.Timestamp)
		cb.cleanOldEntries(&cb.successes, now)
		
		cb.logger.WithField("circuit_breaker", cb.name).
			WithField("state", cb.state).
			WithField("duration", result.Duration).
			WithField("description", description).
			Debug("Circuit breaker call succeeded")
		
		cb.handleSuccess()
	} else {
		cb.totalFailures++
		cb.lastFailure = result.Timestamp
		cb.failures = append(cb.failures, result.Timestamp)
		cb.cleanOldEntries(&cb.failures, now)
		
		cb.logger.WithField("circuit_breaker", cb.name).
			WithField("state", cb.state).
			WithField("error", result.Error).
			WithField("duration", result.Duration).
			WithField("description", description).
			Warn("Circuit breaker call failed")
		
		cb.handleFailure()
	}
}

// handleSuccess processes a successful call result
func (cb *CircuitBreaker) handleSuccess() {
	switch cb.state {
	case StateHalfOpen:
		// Check if we have enough successful calls to close the circuit
		if len(cb.successes) >= cb.config.HalfOpenMaxCalls {
			successRate := float64(len(cb.successes)) / float64(cb.halfOpenCalls)
			if successRate >= cb.config.SuccessThreshold {
				cb.transitionTo(StateClosed)
			}
		}
	}
}

// handleFailure processes a failed call result
func (cb *CircuitBreaker) handleFailure() {
	switch cb.state {
	case StateClosed:
		// Check if we should open the circuit
		if cb.shouldOpenCircuit() {
			cb.transitionTo(StateOpen)
		}
		
	case StateHalfOpen:
		// Any failure in half-open state should open the circuit
		cb.transitionTo(StateOpen)
	}
}

// shouldOpenCircuit determines if the circuit should be opened based on failure rate
func (cb *CircuitBreaker) shouldOpenCircuit() bool {
	recentFailures := len(cb.failures)
	recentSuccesses := len(cb.successes)
	totalRecent := recentFailures + recentSuccesses
	
	// Need minimum calls before considering failure rate
	if totalRecent < cb.config.MinimumCalls {
		return false
	}
	
	// Check if failure threshold is exceeded
	if recentFailures >= cb.config.FailureThreshold {
		return true
	}
	
	// Check failure rate
	failureRate := float64(recentFailures) / float64(totalRecent)
	return failureRate > (1.0 - cb.config.SuccessThreshold)
}

// transitionTo changes the circuit breaker state
func (cb *CircuitBreaker) transitionTo(newState CircuitBreakerState) {
	if cb.state == newState {
		return
	}
	
	oldState := cb.state
	cb.state = newState
	cb.stateChanges[newState]++
	
	// Reset counters on state transitions
	switch newState {
	case StateClosed:
		cb.failures = cb.failures[:0]
		cb.successes = cb.successes[:0]
		cb.halfOpenCalls = 0
		
	case StateOpen:
		cb.halfOpenCalls = 0
		
	case StateHalfOpen:
		cb.halfOpenCalls = 0
	}
	
	cb.logger.WithField("circuit_breaker", cb.name).
		WithField("old_state", oldState).
		WithField("new_state", newState).
		WithField("total_calls", cb.totalCalls).
		WithField("total_failures", cb.totalFailures).
		Info("Circuit breaker state transition")
}

// cleanOldEntries removes entries outside the failure window
func (cb *CircuitBreaker) cleanOldEntries(entries *[]time.Time, now time.Time) {
	cutoff := now.Add(-cb.config.FailureWindow)
	
	// Find the first entry within the window
	start := 0
	for i, timestamp := range *entries {
		if timestamp.After(cutoff) {
			start = i
			break
		}
		start = len(*entries) // All entries are old
	}
	
	// Keep only recent entries
	if start > 0 {
		*entries = (*entries)[start:]
	}
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

// GetMetrics returns comprehensive metrics about the circuit breaker
func (cb *CircuitBreaker) GetMetrics() map[string]interface{} {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	
	now := time.Now()
	cb.cleanOldEntries(&cb.failures, now)
	cb.cleanOldEntries(&cb.successes, now)
	
	recentFailures := len(cb.failures)
	recentSuccesses := len(cb.successes)
	totalRecent := recentFailures + recentSuccesses
	
	var failureRate float64
	if totalRecent > 0 {
		failureRate = float64(recentFailures) / float64(totalRecent)
	}
	
	return map[string]interface{}{
		"name":               cb.name,
		"state":              cb.state,
		"total_calls":        cb.totalCalls,
		"total_failures":     cb.totalFailures,
		"total_successes":    cb.totalSuccesses,
		"recent_failures":    recentFailures,
		"recent_successes":   recentSuccesses,
		"failure_rate":       failureRate,
		"last_failure":       cb.lastFailure,
		"last_attempt":       cb.lastAttempt,
		"half_open_calls":    cb.halfOpenCalls,
		"state_changes":      cb.stateChanges,
		"config": map[string]interface{}{
			"failure_threshold":   cb.config.FailureThreshold,
			"failure_window":      cb.config.FailureWindow.String(),
			"recovery_timeout":    cb.config.RecoveryTimeout.String(),
			"half_open_max_calls": cb.config.HalfOpenMaxCalls,
			"minimum_calls":       cb.config.MinimumCalls,
			"success_threshold":   cb.config.SuccessThreshold,
		},
	}
}

// Reset manually resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	
	cb.logger.WithField("circuit_breaker", cb.name).
		WithField("old_state", cb.state).
		Info("Manually resetting circuit breaker")
	
	cb.transitionTo(StateClosed)
}

// ForceOpen manually forces the circuit breaker to open state
func (cb *CircuitBreaker) ForceOpen() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	
	cb.logger.WithField("circuit_breaker", cb.name).
		WithField("old_state", cb.state).
		Warn("Manually forcing circuit breaker open")
	
	cb.transitionTo(StateOpen)
}

// Common circuit breaker errors
var (
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open")
)

// CircuitBreakerRegistry manages multiple circuit breakers
type CircuitBreakerRegistry struct {
	breakers map[string]*CircuitBreaker
	mutex    sync.RWMutex
	logger   *Logger
}

// NewCircuitBreakerRegistry creates a new circuit breaker registry
func NewCircuitBreakerRegistry(logger *Logger) *CircuitBreakerRegistry {
	return &CircuitBreakerRegistry{
		breakers: make(map[string]*CircuitBreaker),
		logger:   logger,
	}
}

// GetOrCreate gets an existing circuit breaker or creates a new one
func (cbr *CircuitBreakerRegistry) GetOrCreate(name string, config *CircuitBreakerConfig) *CircuitBreaker {
	cbr.mutex.Lock()
	defer cbr.mutex.Unlock()
	
	if breaker, exists := cbr.breakers[name]; exists {
		return breaker
	}
	
	breaker := NewCircuitBreaker(name, config, cbr.logger)
	cbr.breakers[name] = breaker
	
	cbr.logger.WithField("circuit_breaker", name).
		Info("Created new circuit breaker")
	
	return breaker
}

// Get retrieves a circuit breaker by name
func (cbr *CircuitBreakerRegistry) Get(name string) (*CircuitBreaker, bool) {
	cbr.mutex.RLock()
	defer cbr.mutex.RUnlock()
	
	breaker, exists := cbr.breakers[name]
	return breaker, exists
}

// GetAll returns all registered circuit breakers
func (cbr *CircuitBreakerRegistry) GetAll() map[string]*CircuitBreaker {
	cbr.mutex.RLock()
	defer cbr.mutex.RUnlock()
	
	result := make(map[string]*CircuitBreaker)
	for name, breaker := range cbr.breakers {
		result[name] = breaker
	}
	return result
}

// GetAllMetrics returns metrics for all circuit breakers
func (cbr *CircuitBreakerRegistry) GetAllMetrics() map[string]map[string]interface{} {
	cbr.mutex.RLock()
	defer cbr.mutex.RUnlock()
	
	result := make(map[string]map[string]interface{})
	for name, breaker := range cbr.breakers {
		result[name] = breaker.GetMetrics()
	}
	return result
}

// ResetAll resets all circuit breakers
func (cbr *CircuitBreakerRegistry) ResetAll() {
	cbr.mutex.RLock()
	defer cbr.mutex.RUnlock()
	
	for _, breaker := range cbr.breakers {
		breaker.Reset()
	}
	
	cbr.logger.Info("Reset all circuit breakers")
}