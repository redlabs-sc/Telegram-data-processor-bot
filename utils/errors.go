package utils

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorCategory represents different categories of errors for handling strategies
type ErrorCategory string

const (
	// Network-related errors
	ErrorCategoryNetwork ErrorCategory = "network"
	
	// File system and I/O errors
	ErrorCategoryFileSystem ErrorCategory = "filesystem"
	
	// Database and storage errors
	ErrorCategoryDatabase ErrorCategory = "database"
	
	// External process errors (extract.go, convert.go)
	ErrorCategoryExternalProcess ErrorCategory = "external_process"
	
	// Telegram API errors
	ErrorCategoryTelegramAPI ErrorCategory = "telegram_api"
	
	// Validation and user input errors
	ErrorCategoryValidation ErrorCategory = "validation"
	
	// Authentication and authorization errors
	ErrorCategoryAuth ErrorCategory = "auth"
	
	// System resource errors (memory, disk, CPU)
	ErrorCategorySystemResource ErrorCategory = "system_resource"
	
	// Configuration and environment errors
	ErrorCategoryConfiguration ErrorCategory = "configuration"
	
	// Task processing and pipeline errors
	ErrorCategoryTaskProcessing ErrorCategory = "task_processing"
	
	// Critical system errors
	ErrorCategoryCritical ErrorCategory = "critical"
	
	// Unknown/unclassified errors
	ErrorCategoryUnknown ErrorCategory = "unknown"
)

// ErrorSeverity indicates how severe an error is
type ErrorSeverity string

const (
	SeverityLow      ErrorSeverity = "low"      // Informational, doesn't affect operation
	SeverityMedium   ErrorSeverity = "medium"   // Affects operation but recoverable
	SeverityHigh     ErrorSeverity = "high"     // Serious issue requiring attention
	SeverityCritical ErrorSeverity = "critical" // System-threatening issue
)

// RetryStrategy defines how errors should be retried
type RetryStrategy string

const (
	RetryNever      RetryStrategy = "never"       // Don't retry these errors
	RetryImmediate  RetryStrategy = "immediate"   // Retry immediately with backoff
	RetryDelayed    RetryStrategy = "delayed"     // Retry after significant delay
	RetryManual     RetryStrategy = "manual"      // Requires manual intervention
)

// CategorizedError represents an error with metadata for handling
type CategorizedError struct {
	Original   error         `json:"original"`
	Category   ErrorCategory `json:"category"`
	Severity   ErrorSeverity `json:"severity"`
	Retry      RetryStrategy `json:"retry_strategy"`
	Message    string        `json:"message"`
	Context    map[string]interface{} `json:"context,omitempty"`
	Recoverable bool         `json:"recoverable"`
}

func (ce *CategorizedError) Error() string {
	return fmt.Sprintf("[%s:%s] %s", ce.Category, ce.Severity, ce.Message)
}

func (ce *CategorizedError) Unwrap() error {
	return ce.Original
}

// ErrorClassifier categorizes errors based on their content and type
type ErrorClassifier struct {
	patterns map[ErrorCategory][]string
}

func NewErrorClassifier() *ErrorClassifier {
	return &ErrorClassifier{
		patterns: map[ErrorCategory][]string{
			ErrorCategoryNetwork: {
				"network error", "connection refused", "connection reset", "timeout",
				"no route to host", "host unreachable", "dns resolution failed",
				"dial tcp", "i/o timeout", "connection timed out",
			},
			ErrorCategoryFileSystem: {
				"permission denied", "no such file or directory", "file exists",
				"directory not empty", "disk full", "no space left on device",
				"device or resource busy", "I/O error", "read-only file system",
				"file too large", "invalid cross-device link",
			},
			ErrorCategoryDatabase: {
				"database locked", "constraint failed", "database is locked",
				"no such table", "sql:", "database/sql", "UNIQUE constraint failed",
				"foreign key constraint", "syntax error",
			},
			ErrorCategoryExternalProcess: {
				"exit status", "exec:", "fork/exec", "executable file not found",
				"permission denied", "no such file or directory", "broken pipe",
				"signal:", "process already finished",
			},
			ErrorCategoryTelegramAPI: {
				"telegram", "bot api", "flood control", "rate limit", "bad request",
				"unauthorized", "forbidden", "too many requests", "internal server error",
				"bad gateway", "service unavailable", "gateway timeout",
			},
			ErrorCategoryValidation: {
				"invalid", "malformed", "parse error", "validation failed",
				"unsupported format", "invalid format", "bad input",
				"missing required", "out of range",
			},
			ErrorCategoryAuth: {
				"unauthorized", "forbidden", "access denied", "authentication failed",
				"invalid token", "token expired", "insufficient privileges",
			},
			ErrorCategorySystemResource: {
				"out of memory", "memory allocation failed", "resource temporarily unavailable",
				"too many open files", "process limit reached", "system overload",
				"cpu throttling", "disk quota exceeded",
			},
			ErrorCategoryConfiguration: {
				"config", "environment variable", "missing configuration",
				"invalid configuration", "configuration error", "env var",
			},
			ErrorCategoryTaskProcessing: {
				"task", "pipeline", "worker", "queue", "processing failed",
				"invalid task state", "task timeout", "worker timeout",
			},
			ErrorCategoryCritical: {
				"panic", "fatal", "critical", "emergency", "system failure",
				"corruption", "data loss", "security breach",
			},
		},
	}
}

// Categorize classifies an error and returns a CategorizedError
func (ec *ErrorClassifier) Categorize(err error) *CategorizedError {
	if err == nil {
		return nil
	}

	errorText := strings.ToLower(err.Error())
	category := ErrorCategoryUnknown
	
	// Find matching category
	for cat, patterns := range ec.patterns {
		for _, pattern := range patterns {
			if strings.Contains(errorText, strings.ToLower(pattern)) {
				category = cat
				break
			}
		}
		if category != ErrorCategoryUnknown {
			break
		}
	}

	// Determine severity and retry strategy based on category
	severity, retryStrategy, recoverable := ec.getHandlingStrategy(category, errorText)

	return &CategorizedError{
		Original:    err,
		Category:    category,
		Severity:    severity,
		Retry:       retryStrategy,
		Message:     err.Error(),
		Context:     make(map[string]interface{}),
		Recoverable: recoverable,
	}
}

func (ec *ErrorClassifier) getHandlingStrategy(category ErrorCategory, errorText string) (ErrorSeverity, RetryStrategy, bool) {
	switch category {
	case ErrorCategoryNetwork:
		if strings.Contains(errorText, "timeout") || strings.Contains(errorText, "connection refused") {
			return SeverityMedium, RetryImmediate, true
		}
		return SeverityHigh, RetryDelayed, true

	case ErrorCategoryFileSystem:
		if strings.Contains(errorText, "permission denied") {
			return SeverityHigh, RetryNever, false
		}
		if strings.Contains(errorText, "no space left") || strings.Contains(errorText, "disk full") {
			return SeverityCritical, RetryManual, false
		}
		if strings.Contains(errorText, "device or resource busy") {
			return SeverityMedium, RetryDelayed, true
		}
		return SeverityMedium, RetryImmediate, true

	case ErrorCategoryDatabase:
		if strings.Contains(errorText, "locked") {
			return SeverityMedium, RetryImmediate, true
		}
		if strings.Contains(errorText, "constraint") {
			return SeverityLow, RetryNever, false
		}
		return SeverityHigh, RetryImmediate, true

	case ErrorCategoryExternalProcess:
		if strings.Contains(errorText, "exit status 1") {
			return SeverityMedium, RetryImmediate, true
		}
		if strings.Contains(errorText, "executable file not found") {
			return SeverityCritical, RetryNever, false
		}
		return SeverityHigh, RetryDelayed, true

	case ErrorCategoryTelegramAPI:
		if strings.Contains(errorText, "flood control") || strings.Contains(errorText, "rate limit") {
			return SeverityLow, RetryDelayed, true
		}
		if strings.Contains(errorText, "unauthorized") || strings.Contains(errorText, "forbidden") {
			return SeverityHigh, RetryNever, false
		}
		return SeverityMedium, RetryImmediate, true

	case ErrorCategoryValidation:
		return SeverityLow, RetryNever, false

	case ErrorCategoryAuth:
		return SeverityHigh, RetryNever, false

	case ErrorCategorySystemResource:
		if strings.Contains(errorText, "out of memory") {
			return SeverityCritical, RetryManual, false
		}
		return SeverityHigh, RetryDelayed, true

	case ErrorCategoryConfiguration:
		return SeverityHigh, RetryNever, false

	case ErrorCategoryTaskProcessing:
		if strings.Contains(errorText, "timeout") {
			return SeverityMedium, RetryImmediate, true
		}
		return SeverityMedium, RetryDelayed, true

	case ErrorCategoryCritical:
		return SeverityCritical, RetryNever, false

	default:
		return SeverityMedium, RetryImmediate, true
	}
}

// ErrorHandler provides centralized error handling with logging and recovery
type ErrorHandler struct {
	classifier *ErrorClassifier
	logger     *Logger
	metrics    map[ErrorCategory]int
}

func NewErrorHandler(logger *Logger) *ErrorHandler {
	return &ErrorHandler{
		classifier: NewErrorClassifier(),
		logger:     logger,
		metrics:    make(map[ErrorCategory]int),
	}
}

// Handle processes an error and returns handling instructions
func (eh *ErrorHandler) Handle(err error, context map[string]interface{}) *CategorizedError {
	if err == nil {
		return nil
	}

	categorized := eh.classifier.Categorize(err)
	if context != nil {
		for k, v := range context {
			categorized.Context[k] = v
		}
	}

	// Update metrics
	eh.metrics[categorized.Category]++

	// Log based on severity
	logFields := map[string]interface{}{
		"error_category":     categorized.Category,
		"error_severity":     categorized.Severity,
		"retry_strategy":     categorized.Retry,
		"recoverable":        categorized.Recoverable,
		"original_error":     categorized.Original.Error(),
	}

	// Add context fields
	for k, v := range categorized.Context {
		logFields[k] = v
	}

	switch categorized.Severity {
	case SeverityLow:
		eh.logger.WithFields(logFields).Debug("Low severity error handled")
	case SeverityMedium:
		eh.logger.WithFields(logFields).Warn("Medium severity error handled")
	case SeverityHigh:
		eh.logger.WithFields(logFields).Error("High severity error handled")
	case SeverityCritical:
		eh.logger.WithFields(logFields).Fatal("Critical error handled")
	}

	return categorized
}

// GetMetrics returns error counts by category
func (eh *ErrorHandler) GetMetrics() map[ErrorCategory]int {
	metrics := make(map[ErrorCategory]int)
	for k, v := range eh.metrics {
		metrics[k] = v
	}
	return metrics
}

// ResetMetrics clears error metrics
func (eh *ErrorHandler) ResetMetrics() {
	eh.metrics = make(map[ErrorCategory]int)
}

// Predefined error types for common scenarios
var (
	ErrTaskNotFound       = errors.New("task not found")
	ErrTaskAlreadyExists  = errors.New("task already exists")
	ErrInvalidTaskStatus  = errors.New("invalid task status")
	ErrFileNotFound       = errors.New("file not found")
	ErrFileAlreadyExists  = errors.New("file already exists")
	ErrInvalidFileType    = errors.New("invalid file type")
	ErrFileSizeExceeded   = errors.New("file size exceeded")
	ErrUnauthorizedAccess = errors.New("unauthorized access")
	ErrRateLimitExceeded  = errors.New("rate limit exceeded")
	ErrSystemOverload     = errors.New("system overload")
	ErrConfigurationError = errors.New("configuration error")
	ErrExternalProcessFailed = errors.New("external process failed")
)

// Convenience functions for creating specific error types
func NewTaskError(taskID string, err error) error {
	return fmt.Errorf("task %s: %w", taskID, err)
}

func NewFileError(filename string, err error) error {
	return fmt.Errorf("file %s: %w", filename, err)
}

func NewProcessError(processName string, exitCode int, err error) error {
	return fmt.Errorf("process %s exited with code %d: %w", processName, exitCode, err)
}

func NewValidationError(field string, value interface{}) error {
	return fmt.Errorf("validation failed for field %s with value %v", field, value)
}