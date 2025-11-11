package utils

import (
	"context"
	"fmt"
	"time"
)

type RetryConfig struct {
	MaxAttempts     int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	BackoffFactor   float64
	RetryableErrors []string
	// New exponential backoff configuration
	UseJitter       bool          // Add randomization to delays
	JitterFactor    float64       // Percentage of jitter (0.0-1.0)
	BackoffType     BackoffType   // Type of backoff algorithm
	TimeoutPerAttempt time.Duration // Individual attempt timeout
}

// BackoffType defines different backoff algorithms
type BackoffType string

const (
	BackoffLinear      BackoffType = "linear"      // delay = attempt * base
	BackoffExponential BackoffType = "exponential" // delay = base * factor^attempt
	BackoffPolynomial  BackoffType = "polynomial"  // delay = base * attempt^2
	BackoffFibonacci   BackoffType = "fibonacci"   // delay follows fibonacci sequence
)

type RetryService struct {
	config *RetryConfig
	logger *Logger
}

func NewRetryService(logger *Logger) *RetryService {
	return &RetryService{
		config: &RetryConfig{
			MaxAttempts:     3,
			InitialDelay:    time.Second,
			MaxDelay:        30 * time.Second,
			BackoffFactor:   2.0,
			UseJitter:       true,
			JitterFactor:    0.1, // 10% jitter
			BackoffType:     BackoffExponential,
			TimeoutPerAttempt: 30 * time.Second,
			RetryableErrors: []string{
				"network error",
				"timeout",
				"connection reset",
				"temporary failure",
				"service unavailable",
				"internal server error",
			},
		},
		logger: logger,
	}
}

func (rs *RetryService) WithConfig(config *RetryConfig) *RetryService {
	rs.config = config
	return rs
}

func (rs *RetryService) Execute(ctx context.Context, operation func() error, description string) error {
	return rs.ExecuteWithCallback(ctx, operation, description, nil)
}

func (rs *RetryService) ExecuteWithCallback(ctx context.Context, operation func() error, description string, onRetry func(attempt int, err error)) error {
	var lastErr error
	
	for attempt := 1; attempt <= rs.config.MaxAttempts; attempt++ {
		rs.logger.WithField("attempt", attempt).
			WithField("max_attempts", rs.config.MaxAttempts).
			WithField("operation", description).
			Debug("Executing operation")

		// Create timeout context for individual attempt if configured
		attemptCtx := ctx
		var cancel context.CancelFunc
		if rs.config.TimeoutPerAttempt > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, rs.config.TimeoutPerAttempt)
		}

		// Execute operation with timeout support
		err := rs.executeWithTimeout(attemptCtx, operation)
		
		if cancel != nil {
			cancel()
		}

		if err == nil {
			if attempt > 1 {
				rs.logger.WithField("attempt", attempt).
					WithField("operation", description).
					Info("Operation succeeded after retry")
			}
			return nil
		}

		lastErr = err
		
		if !rs.isRetryable(err) {
			rs.logger.WithField("error", err.Error()).
				WithField("operation", description).
				Info("Non-retryable error encountered")
			return fmt.Errorf("non-retryable error in %s: %w", description, err)
		}

		if attempt == rs.config.MaxAttempts {
			rs.logger.WithField("attempt", attempt).
				WithField("error", err.Error()).
				WithField("operation", description).
				Error("Maximum retry attempts reached")
			break
		}

		delay := rs.calculateDelayForError(attempt, err)
		
		rs.logger.WithField("attempt", attempt).
			WithField("error", err.Error()).
			WithField("delay", delay).
			WithField("backoff_type", rs.config.BackoffType).
			WithField("operation", description).
			Warn("Operation failed, retrying with exponential backoff")

		if onRetry != nil {
			onRetry(attempt, err)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled: %w", ctx.Err())
		case <-time.After(delay):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("operation %s failed after %d attempts: %w", description, rs.config.MaxAttempts, lastErr)
}

// executeWithTimeout executes an operation with timeout support
func (rs *RetryService) executeWithTimeout(ctx context.Context, operation func() error) error {
	done := make(chan error, 1)
	
	go func() {
		done <- operation()
	}()
	
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("operation timeout: %w", ctx.Err())
	}
}

func (rs *RetryService) isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Check if it's a CategorizedError and use its retry strategy
	if categorizedErr, ok := err.(*CategorizedError); ok {
		return categorizedErr.Retry == RetryImmediate || categorizedErr.Retry == RetryDelayed
	}

	// Fallback to original pattern matching
	errorText := err.Error()
	for _, retryableError := range rs.config.RetryableErrors {
		if contains(errorText, retryableError) {
			return true
		}
	}

	return false
}

func (rs *RetryService) calculateDelay(attempt int) time.Duration {
	var delay time.Duration
	
	switch rs.config.BackoffType {
	case BackoffLinear:
		delay = time.Duration(int64(rs.config.InitialDelay) * int64(attempt))
	case BackoffExponential:
		delay = time.Duration(float64(rs.config.InitialDelay) * pow(rs.config.BackoffFactor, float64(attempt-1)))
	case BackoffPolynomial:
		delay = time.Duration(float64(rs.config.InitialDelay) * pow(float64(attempt), 2.0))
	case BackoffFibonacci:
		delay = time.Duration(int64(rs.config.InitialDelay) * int64(rs.fibonacci(attempt)))
	default:
		// Default to exponential
		delay = time.Duration(float64(rs.config.InitialDelay) * pow(rs.config.BackoffFactor, float64(attempt-1)))
	}
	
	// Apply maximum delay cap
	if delay > rs.config.MaxDelay {
		delay = rs.config.MaxDelay
	}
	
	// Apply jitter if enabled
	if rs.config.UseJitter && rs.config.JitterFactor > 0 {
		delay = rs.applyJitter(delay)
	}
	
	return delay
}

func (rs *RetryService) calculateDelayForError(attempt int, err error) time.Duration {
	baseDelay := rs.calculateDelay(attempt)
	
	// Adjust delay based on error category
	if categorizedErr, ok := err.(*CategorizedError); ok {
		switch categorizedErr.Retry {
		case RetryImmediate:
			// Use standard backoff
			return baseDelay
		case RetryDelayed:
			// Use longer delays for delayed retry strategy
			return baseDelay * 2
		default:
			return baseDelay
		}
	}
	
	return baseDelay
}

// Helper functions
func contains(text, substr string) bool {
	return len(text) >= len(substr) && (text == substr || 
		(len(text) > len(substr) && 
			(text[:len(substr)] == substr || 
			 text[len(text)-len(substr):] == substr ||
			 findSubstring(text, substr))))
}

func findSubstring(text, substr string) bool {
	for i := 0; i <= len(text)-len(substr); i++ {
		if text[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func pow(base float64, exp float64) float64 {
	result := 1.0
	for i := 0; i < int(exp); i++ {
		result *= base
	}
	return result
}

// applyJitter adds randomization to delay to prevent thundering herd
func (rs *RetryService) applyJitter(delay time.Duration) time.Duration {
	if rs.config.JitterFactor <= 0 || rs.config.JitterFactor > 1 {
		return delay // Invalid jitter factor, return original delay
	}
	
	// Calculate jitter range
	jitterRange := float64(delay) * rs.config.JitterFactor
	
	// Generate random jitter between -jitterRange/2 and +jitterRange/2
	// This creates a more distributed spread around the original delay
	jitter := (randomFloat() - 0.5) * jitterRange
	
	newDelay := time.Duration(float64(delay) + jitter)
	
	// Ensure delay doesn't go negative
	if newDelay < 0 {
		newDelay = delay / 2 // Fallback to half the original delay
	}
	
	return newDelay
}

// fibonacci calculates nth fibonacci number for fibonacci backoff
func (rs *RetryService) fibonacci(n int) int {
	if n <= 1 {
		return 1
	}
	if n == 2 {
		return 1
	}
	
	a, b := 1, 1
	for i := 3; i <= n; i++ {
		a, b = b, a+b
	}
	return b
}

// randomFloat generates a random float between 0 and 1
// Simple linear congruential generator for deterministic testing
func randomFloat() float64 {
	// Using a simple method to avoid importing crypto/rand or math/rand
	// This is sufficient for jitter purposes
	seed := time.Now().UnixNano()
	return float64((seed*1103515245+12345)&0x7fffffff) / float64(0x7fffffff)
}

// FileOperationRetry provides specialized retry logic for file operations
type FileOperationRetry struct {
	retryService *RetryService
	fileManager  *FileManager
}

func NewFileOperationRetry(logger *Logger) *FileOperationRetry {
	return &FileOperationRetry{
		retryService: NewRetryService(logger).WithConfig(&RetryConfig{
			MaxAttempts:       5,
			InitialDelay:      500 * time.Millisecond,
			MaxDelay:          10 * time.Second,
			BackoffFactor:     1.5,
			UseJitter:         true,
			JitterFactor:      0.15, // 15% jitter for file operations
			BackoffType:       BackoffExponential,
			TimeoutPerAttempt: 60 * time.Second, // File operations can take longer
			RetryableErrors: []string{
				"permission denied",
				"resource temporarily unavailable",
				"device or resource busy",
				"no space left on device",
				"disk full",
				"I/O error",
			},
		}),
		fileManager: NewFileManager(logger),
	}
}

func (fr *FileOperationRetry) MoveFileWithRetry(ctx context.Context, src, dst string) error {
	return fr.retryService.Execute(ctx, func() error {
		return fr.fileManager.MoveFile(src, dst)
	}, fmt.Sprintf("move file %s to %s", src, dst))
}

func (fr *FileOperationRetry) CopyFileWithRetry(ctx context.Context, src, dst string) error {
	return fr.retryService.Execute(ctx, func() error {
		return fr.fileManager.CopyFile(src, dst)
	}, fmt.Sprintf("copy file %s to %s", src, dst))
}

func (fr *FileOperationRetry) CalculateHashWithRetry(ctx context.Context, filePath string) (string, error) {
	var hash string
	err := fr.retryService.Execute(ctx, func() error {
		var err error
		hash, err = fr.fileManager.CalculateFileHash(filePath)
		return err
	}, fmt.Sprintf("calculate hash for %s", filePath))
	
	return hash, err
}

// ProcessOperationRetry provides specialized retry logic for external process operations
type ProcessOperationRetry struct {
	retryService *RetryService
}

func NewProcessOperationRetry(logger *Logger) *ProcessOperationRetry {
	return &ProcessOperationRetry{
		retryService: NewRetryService(logger).WithConfig(&RetryConfig{
			MaxAttempts:       3,
			InitialDelay:      2 * time.Second,
			MaxDelay:          30 * time.Second,
			BackoffFactor:     2.0,
			UseJitter:         true,
			JitterFactor:      0.2, // 20% jitter for process operations
			BackoffType:       BackoffExponential,
			TimeoutPerAttempt: 120 * time.Second, // External processes can take long
			RetryableErrors: []string{
				"exit status 1",
				"broken pipe",
				"connection refused",
				"timeout",
				"resource temporarily unavailable",
			},
		}),
	}
}

func (por *ProcessOperationRetry) ExecuteWithRetry(ctx context.Context, operation func() error, description string) error {
	return por.retryService.ExecuteWithCallback(ctx, operation, description, func(attempt int, err error) {
		// Custom callback for process retries - could include cleanup logic
	})
}

// RetryConfigFactory provides pre-configured retry configurations for different scenarios
type RetryConfigFactory struct{}

func NewRetryConfigFactory() *RetryConfigFactory {
	return &RetryConfigFactory{}
}

// GetConfigForCategory returns optimized retry configuration for specific error categories
func (rcf *RetryConfigFactory) GetConfigForCategory(category ErrorCategory) *RetryConfig {
	switch category {
	case ErrorCategoryNetwork:
		return &RetryConfig{
			MaxAttempts:       5,
			InitialDelay:      time.Second,
			MaxDelay:          60 * time.Second,
			BackoffFactor:     2.0,
			UseJitter:         true,
			JitterFactor:      0.25, // Higher jitter for network issues
			BackoffType:       BackoffExponential,
			TimeoutPerAttempt: 30 * time.Second,
			RetryableErrors: []string{
				"network error", "timeout", "connection refused", "connection reset",
				"no route to host", "host unreachable", "dns resolution failed",
			},
		}
		
	case ErrorCategoryFileSystem:
		return &RetryConfig{
			MaxAttempts:       4,
			InitialDelay:      500 * time.Millisecond,
			MaxDelay:          20 * time.Second,
			BackoffFactor:     1.8,
			UseJitter:         true,
			JitterFactor:      0.15,
			BackoffType:       BackoffLinear, // Linear backoff for file system
			TimeoutPerAttempt: 60 * time.Second,
			RetryableErrors: []string{
				"device or resource busy", "I/O error", "resource temporarily unavailable",
			},
		}
		
	case ErrorCategoryDatabase:
		return &RetryConfig{
			MaxAttempts:       6,
			InitialDelay:      100 * time.Millisecond,
			MaxDelay:          5 * time.Second,
			BackoffFactor:     1.5,
			UseJitter:         true,
			JitterFactor:      0.1, // Low jitter for database operations
			BackoffType:       BackoffExponential,
			TimeoutPerAttempt: 10 * time.Second,
			RetryableErrors: []string{
				"database locked", "database is locked", "SQLITE_BUSY",
			},
		}
		
	case ErrorCategoryExternalProcess:
		return &RetryConfig{
			MaxAttempts:       3,
			InitialDelay:      3 * time.Second,
			MaxDelay:          45 * time.Second,
			BackoffFactor:     2.5,
			UseJitter:         true,
			JitterFactor:      0.2,
			BackoffType:       BackoffExponential,
			TimeoutPerAttempt: 180 * time.Second, // Long timeout for processes
			RetryableErrors: []string{
				"exit status 1", "broken pipe", "resource temporarily unavailable",
			},
		}
		
	case ErrorCategoryTelegramAPI:
		return &RetryConfig{
			MaxAttempts:       4,
			InitialDelay:      2 * time.Second,
			MaxDelay:          120 * time.Second, // Long delays for rate limiting
			BackoffFactor:     3.0, // Aggressive backoff for API limits
			UseJitter:         true,
			JitterFactor:      0.3, // High jitter to spread out requests
			BackoffType:       BackoffExponential,
			TimeoutPerAttempt: 30 * time.Second,
			RetryableErrors: []string{
				"flood control", "rate limit", "too many requests", "service unavailable",
			},
		}
		
	case ErrorCategorySystemResource:
		return &RetryConfig{
			MaxAttempts:       2, // Fewer retries for resource issues
			InitialDelay:      5 * time.Second,
			MaxDelay:          30 * time.Second,
			BackoffFactor:     2.0,
			UseJitter:         false, // No jitter for system resources
			BackoffType:       BackoffLinear,
			TimeoutPerAttempt: 60 * time.Second,
			RetryableErrors: []string{
				"resource temporarily unavailable", "too many open files",
			},
		}
		
	default:
		// Default configuration
		return &RetryConfig{
			MaxAttempts:       3,
			InitialDelay:      time.Second,
			MaxDelay:          30 * time.Second,
			BackoffFactor:     2.0,
			UseJitter:         true,
			JitterFactor:      0.1,
			BackoffType:       BackoffExponential,
			TimeoutPerAttempt: 30 * time.Second,
			RetryableErrors: []string{
				"temporary failure", "service unavailable", "internal server error",
			},
		}
	}
}

// GetConfigForBackoffType returns configuration optimized for specific backoff types
func (rcf *RetryConfigFactory) GetConfigForBackoffType(backoffType BackoffType, maxAttempts int, initialDelay time.Duration) *RetryConfig {
	config := &RetryConfig{
		MaxAttempts:       maxAttempts,
		InitialDelay:      initialDelay,
		UseJitter:         true,
		JitterFactor:      0.1,
		BackoffType:       backoffType,
		TimeoutPerAttempt: 30 * time.Second,
	}
	
	switch backoffType {
	case BackoffLinear:
		config.MaxDelay = initialDelay * time.Duration(maxAttempts) * 2
		config.BackoffFactor = 1.0 // Not used in linear
		
	case BackoffExponential:
		config.MaxDelay = initialDelay * 30 // Conservative max
		config.BackoffFactor = 2.0
		
	case BackoffPolynomial:
		config.MaxDelay = initialDelay * time.Duration(maxAttempts*maxAttempts)
		config.BackoffFactor = 1.0 // Not used in polynomial
		
	case BackoffFibonacci:
		// Fibonacci grows quickly, use conservative max
		config.MaxDelay = initialDelay * 20
		config.BackoffFactor = 1.0 // Not used in fibonacci
	}
	
	return config
}

// EnhancedRetryService integrates error categorization with retry logic
type EnhancedRetryService struct {
	retryService    *RetryService
	errorHandler    *ErrorHandler
	configFactory   *RetryConfigFactory
	logger          *Logger
}

func NewEnhancedRetryService(logger *Logger) *EnhancedRetryService {
	return &EnhancedRetryService{
		retryService:  NewRetryService(logger),
		errorHandler:  NewErrorHandler(logger),
		configFactory: NewRetryConfigFactory(),
		logger:        logger,
	}
}

// ExecuteWithCategoryOptimization automatically selects optimal retry configuration based on error category
func (ers *EnhancedRetryService) ExecuteWithCategoryOptimization(ctx context.Context, operation func() error, description string, operationContext map[string]interface{}) error {
	var lastCategorizedErr *CategorizedError
	var currentConfig *RetryConfig
	
	// Start with default configuration
	currentRetryService := ers.retryService
	
	for attempt := 1; attempt <= ers.retryService.config.MaxAttempts; attempt++ {
		ers.logger.WithField("attempt", attempt).
			WithField("operation", description).
			Debug("Executing operation with category optimization")

		// Create timeout context for individual attempt
		attemptCtx := ctx
		var cancel context.CancelFunc
		if currentRetryService.config.TimeoutPerAttempt > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, currentRetryService.config.TimeoutPerAttempt)
		}

		err := currentRetryService.executeWithTimeout(attemptCtx, operation)
		
		if cancel != nil {
			cancel()
		}

		if err == nil {
			if attempt > 1 {
				ers.logger.WithField("attempt", attempt).
					WithField("operation", description).
					Info("Operation succeeded after category-optimized retry")
			}
			return nil
		}

		// Categorize the error
		lastCategorizedErr = ers.errorHandler.Handle(err, operationContext)
		
		// Optimize retry configuration based on error category (only on first error)  
		if attempt == 1 && lastCategorizedErr.Category != ErrorCategoryUnknown {
			categoryConfig := ers.configFactory.GetConfigForCategory(lastCategorizedErr.Category)
			currentRetryService = ers.retryService.WithConfig(categoryConfig)
			currentConfig = categoryConfig
			
			ers.logger.WithField("error_category", lastCategorizedErr.Category).
				WithField("backoff_type", categoryConfig.BackoffType).
				WithField("max_attempts", categoryConfig.MaxAttempts).
				WithField("operation", description).
				Info("Switched to category-optimized retry configuration")
		}
		
		// Check if error is retryable based on category
		if lastCategorizedErr.Retry == RetryNever {
			ers.logger.WithField("error_category", lastCategorizedErr.Category).
				WithField("operation", description).
				Info("Non-retryable error encountered")
			return fmt.Errorf("non-retryable error in %s: %w", description, lastCategorizedErr)
		}

		maxAttempts := currentRetryService.config.MaxAttempts
		if attempt >= maxAttempts {
			ers.logger.WithField("attempt", attempt).
				WithField("error", err.Error()).
				WithField("error_category", lastCategorizedErr.Category).
				WithField("operation", description).
				Error("Maximum category-optimized retry attempts reached")
			break
		}

		// Calculate delay using category-optimized configuration
		delay := ers.calculateCategoryOptimizedDelay(attempt, lastCategorizedErr, currentConfig)
		
		ers.logger.WithField("attempt", attempt).
			WithField("error", err.Error()).
			WithField("error_category", lastCategorizedErr.Category).
			WithField("delay", delay).
			WithField("backoff_type", currentRetryService.config.BackoffType).
			WithField("operation", description).
			Warn("Operation failed, retrying with category-optimized exponential backoff")

		select {
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled: %w", ctx.Err())
		case <-time.After(delay):
			// Continue to next attempt
		}
	}

	maxAttempts := currentRetryService.config.MaxAttempts
	return fmt.Errorf("operation %s failed after %d category-optimized attempts: %w", description, maxAttempts, lastCategorizedErr)
}

func (ers *EnhancedRetryService) calculateCategoryOptimizedDelay(attempt int, categorizedErr *CategorizedError, config *RetryConfig) time.Duration {
	if config == nil {
		// Fallback to standard calculation
		return ers.calculateCategoryDelay(attempt, categorizedErr)
	}
	
	var delay time.Duration
	
	switch config.BackoffType {
	case BackoffLinear:
		delay = time.Duration(int64(config.InitialDelay) * int64(attempt))
	case BackoffExponential:
		delay = time.Duration(float64(config.InitialDelay) * pow(config.BackoffFactor, float64(attempt-1)))
	case BackoffPolynomial:
		delay = time.Duration(float64(config.InitialDelay) * pow(float64(attempt), 2.0))
	case BackoffFibonacci:
		fibValue := ers.fibonacci(attempt)
		delay = time.Duration(int64(config.InitialDelay) * int64(fibValue))
	default:
		delay = time.Duration(float64(config.InitialDelay) * pow(config.BackoffFactor, float64(attempt-1)))
	}
	
	// Apply maximum delay cap
	if delay > config.MaxDelay {
		delay = config.MaxDelay
	}
	
	// Apply jitter if enabled
	if config.UseJitter && config.JitterFactor > 0 {
		delay = ers.applyJitter(delay, config.JitterFactor)
	}
	
	// Additional category-specific adjustments
	switch categorizedErr.Category {
	case ErrorCategoryTelegramAPI:
		if categorizedErr.Retry == RetryDelayed {
			// Extra delay for rate limiting
			delay = delay * 2
		}
	case ErrorCategorySystemResource:
		// System resource issues need extra recovery time
		delay = delay + (5 * time.Second)
	}
	
	return delay
}

func (ers *EnhancedRetryService) applyJitter(delay time.Duration, jitterFactor float64) time.Duration {
	if jitterFactor <= 0 || jitterFactor > 1 {
		return delay
	}
	
	jitterRange := float64(delay) * jitterFactor
	jitter := (randomFloat() - 0.5) * jitterRange
	newDelay := time.Duration(float64(delay) + jitter)
	
	if newDelay < 0 {
		newDelay = delay / 2
	}
	
	return newDelay
}

func (ers *EnhancedRetryService) fibonacci(n int) int {
	if n <= 1 {
		return 1
	}
	if n == 2 {
		return 1
	}
	
	a, b := 1, 1
	for i := 3; i <= n; i++ {
		a, b = b, a+b
	}
	return b
}

func (ers *EnhancedRetryService) ExecuteWithErrorHandling(ctx context.Context, operation func() error, description string, operationContext map[string]interface{}) error {
	var lastCategorizedErr *CategorizedError
	
	for attempt := 1; attempt <= ers.retryService.config.MaxAttempts; attempt++ {
		ers.retryService.logger.WithField("attempt", attempt).
			WithField("max_attempts", ers.retryService.config.MaxAttempts).
			WithField("operation", description).
			Debug("Executing operation with error handling")

		err := operation()
		if err == nil {
			if attempt > 1 {
				ers.retryService.logger.WithField("attempt", attempt).
					WithField("operation", description).
					Info("Operation succeeded after retry")
			}
			return nil
		}

		// Categorize the error
		lastCategorizedErr = ers.errorHandler.Handle(err, operationContext)
		
		// Check if error is retryable based on category
		if lastCategorizedErr.Retry == RetryNever {
			ers.retryService.logger.WithField("error_category", lastCategorizedErr.Category).
				WithField("operation", description).
				Info("Non-retryable error encountered")
			return fmt.Errorf("non-retryable error in %s: %w", description, lastCategorizedErr)
		}

		if attempt == ers.retryService.config.MaxAttempts {
			ers.retryService.logger.WithField("attempt", attempt).
				WithField("error", err.Error()).
				WithField("error_category", lastCategorizedErr.Category).
				WithField("operation", description).
				Error("Maximum retry attempts reached")
			break
		}

		// Calculate delay based on error category
		delay := ers.calculateCategoryDelay(attempt, lastCategorizedErr)
		
		ers.retryService.logger.WithField("attempt", attempt).
			WithField("error", err.Error()).
			WithField("error_category", lastCategorizedErr.Category).
			WithField("delay", delay).
			WithField("operation", description).
			Warn("Operation failed, retrying with categorized error handling")

		select {
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled: %w", ctx.Err())
		case <-time.After(delay):
			// Continue to next attempt
		}
	}

	return fmt.Errorf("operation %s failed after %d attempts: %w", description, ers.retryService.config.MaxAttempts, lastCategorizedErr)
}

func (ers *EnhancedRetryService) calculateCategoryDelay(attempt int, categorizedErr *CategorizedError) time.Duration {
	baseDelay := ers.retryService.calculateDelay(attempt)
	
	switch categorizedErr.Category {
	case ErrorCategoryNetwork:
		// Network errors get progressive backoff
		return baseDelay
	case ErrorCategoryFileSystem:
		if categorizedErr.Retry == RetryDelayed {
			// File system busy errors need longer delays
			return baseDelay * 3
		}
		return baseDelay
	case ErrorCategoryDatabase:
		// Database locks need short delays
		return baseDelay / 2
	case ErrorCategoryExternalProcess:
		// External processes may need time to clean up
		return baseDelay * 2
	case ErrorCategoryTelegramAPI:
		if categorizedErr.Retry == RetryDelayed {
			// Rate limiting requires significant delays
			return baseDelay * 5
		}
		return baseDelay
	case ErrorCategorySystemResource:
		// System resource issues need time to recover
		return baseDelay * 4
	default:
		return baseDelay
	}
}