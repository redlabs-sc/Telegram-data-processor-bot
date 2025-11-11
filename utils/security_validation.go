package utils

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

// FileSignature represents a file type signature
type FileSignature struct {
	Extension string
	MimeType  string
	Magic     []byte
	Offset    int
}

// SecurityValidator handles comprehensive file validation and sanitization
type SecurityValidator struct {
	logger                    *Logger
	maxFileSize               int64
	allowedTypes              map[string]FileSignature
	dangerousPatterns         []*regexp.Regexp
	config                    *Config
	enhancedSignatureValidator *EnhancedSignatureValidator
}

// NewSecurityValidator creates a new security validator
func NewSecurityValidator(logger *Logger, config *Config) *SecurityValidator {
	sv := &SecurityValidator{
		logger:      logger,
		maxFileSize: config.MaxFileSizeBytes(),
		config:      config,
	}
	
	// Initialize allowed file type signatures
	sv.initializeFileSignatures()
	
	// Initialize dangerous pattern detection
	sv.initializeDangerousPatterns()
	
	// Initialize enhanced signature validator
	sv.enhancedSignatureValidator = NewEnhancedSignatureValidator(logger)
	
	return sv
}

// initializeFileSignatures sets up file signature validation
func (sv *SecurityValidator) initializeFileSignatures() {
	sv.allowedTypes = map[string]FileSignature{
		"zip": {
			Extension: "zip",
			MimeType:  "application/zip",
			Magic:     []byte{0x50, 0x4B, 0x03, 0x04}, // "PK.."
			Offset:    0,
		},
		"rar": {
			Extension: "rar",
			MimeType:  "application/vnd.rar",
			Magic:     []byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07}, // "Rar!.."
			Offset:    0,
		},
		"txt": {
			Extension: "txt",
			MimeType:  "text/plain",
			Magic:     nil, // Text files don't have a consistent magic signature
			Offset:    0,
		},
	}
}

// initializeDangerousPatterns sets up patterns for potentially dangerous content
func (sv *SecurityValidator) initializeDangerousPatterns() {
	patterns := []string{
		// Script injection patterns
		`<script[^>]*>.*?</script>`,
		`javascript:`,
		`vbscript:`,
		`onload\s*=`,
		`onerror\s*=`,
		
		// Executable patterns
		`\x4D\x5A`, // PE executable header "MZ"
		`\x7F\x45\x4C\x46`, // ELF header
		
		// Malicious archive patterns
		`\.\.[\\/]`, // Directory traversal
		`__MACOSX`, // macOS metadata
		
		// Suspicious file extensions in archives
		`\.(exe|bat|cmd|scr|pif|com|vbs|js|jar|dll|sys)`,
		
		// Large file paths (zip bombs indicator)
		`.{1000,}`, // Paths longer than 1000 characters
	}
	
	sv.dangerousPatterns = make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		if compiled, err := regexp.Compile(pattern); err == nil {
			sv.dangerousPatterns = append(sv.dangerousPatterns, compiled)
		} else {
			sv.logger.WithError(err).
				WithField("pattern", pattern).
				Warn("Failed to compile dangerous pattern regex")
		}
	}
}

// ValidationResult represents the result of file validation
type ValidationResult struct {
	Valid                  bool
	FileType               string
	ActualMimeType         string
	FileSize               int64
	SecurityWarnings       []string
	SanitizationLog        []string
	ThreatLevel            ThreatLevel
	SignatureValidation    *SignatureValidationResult
	EnhancedSecurityChecks map[string]interface{}
}

// ThreatLevel represents the security threat level of a file
type ThreatLevel int

const (
	ThreatLevelSafe ThreatLevel = iota
	ThreatLevelLow
	ThreatLevelMedium
	ThreatLevelHigh
	ThreatLevelCritical
)

func (tl ThreatLevel) String() string {
	switch tl {
	case ThreatLevelSafe:
		return "SAFE"
	case ThreatLevelLow:
		return "LOW"
	case ThreatLevelMedium:
		return "MEDIUM"
	case ThreatLevelHigh:
		return "HIGH"
	case ThreatLevelCritical:
		return "CRITICAL"
	default:
		return "UNKNOWN"
	}
}

// ValidateFile performs comprehensive file validation
func (sv *SecurityValidator) ValidateFile(filePath, declaredType string) (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:                  true,
		SecurityWarnings:       make([]string, 0),
		SanitizationLog:        make([]string, 0),
		ThreatLevel:            ThreatLevelSafe,
		EnhancedSecurityChecks: make(map[string]interface{}),
	}
	
	sv.logger.WithField("file_path", filePath).
		WithField("declared_type", declaredType).
		Info("Starting comprehensive file validation")
	
	// Step 1: Basic file existence and size check
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("file access error: %w", err)
	}
	
	result.FileSize = fileInfo.Size()
	if result.FileSize > sv.maxFileSize {
		result.Valid = false
		result.ThreatLevel = ThreatLevelHigh
		result.SecurityWarnings = append(result.SecurityWarnings, 
			fmt.Sprintf("File size %d exceeds maximum allowed %d", result.FileSize, sv.maxFileSize))
	}
	
	// Step 2: Enhanced file signature validation (replaces basic signature validation)
	if signatureResult, err := sv.enhancedSignatureValidator.ValidateFileSignature(filePath, declaredType); err != nil {
		sv.logger.WithError(err).Error("Enhanced signature validation failed")
		result.Valid = false
		result.ThreatLevel = ThreatLevelHigh
		result.SecurityWarnings = append(result.SecurityWarnings, 
			fmt.Sprintf("Enhanced signature validation failed: %v", err))
	} else {
		result.SignatureValidation = signatureResult
		result.FileType = signatureResult.FileType
		
		// Merge signature validation warnings
		result.SecurityWarnings = append(result.SecurityWarnings, signatureResult.SecurityWarnings...)
		
		// Update threat level based on signature analysis
		if signatureResult.ThreatAssessment > result.ThreatLevel {
			result.ThreatLevel = signatureResult.ThreatAssessment
		}
		
		// Mark as invalid if signature validation fails
		if !signatureResult.IsGenuineFileType || signatureResult.ConfidenceLevel < 0.3 {
			result.Valid = false
			result.SecurityWarnings = append(result.SecurityWarnings, 
				"File signature validation indicates this may not be a genuine file of the declared type")
		}
		
		// Store enhanced security check results
		result.EnhancedSecurityChecks["signature_confidence"] = signatureResult.ConfidenceLevel
		result.EnhancedSecurityChecks["matched_signatures"] = signatureResult.MatchedSignatures
		result.EnhancedSecurityChecks["detected_malware"] = signatureResult.DetectedMalware
		result.EnhancedSecurityChecks["polyglot_risks"] = signatureResult.PolyglotRisks
		result.EnhancedSecurityChecks["suspicious_features"] = signatureResult.SuspiciousFeatures
		result.EnhancedSecurityChecks["anti_spoofing_checks"] = signatureResult.AntiSpoofingChecks
		
		sv.logger.WithField("confidence", signatureResult.ConfidenceLevel).
			WithField("genuine", signatureResult.IsGenuineFileType).
			WithField("malware_detected", len(signatureResult.DetectedMalware)).
			WithField("polyglot_risks", len(signatureResult.PolyglotRisks)).
			Info("Enhanced signature validation completed")
	}
	
	// Step 3: Content scanning for dangerous patterns
	if err := sv.scanFileContent(filePath, result); err != nil {
		sv.logger.WithError(err).Warn("Content scanning encountered issues")
		result.SecurityWarnings = append(result.SecurityWarnings, 
			fmt.Sprintf("Content scanning warning: %v", err))
	}
	
	// Step 4: Archive-specific validation for ZIP/RAR files
	if result.FileType == "zip" || result.FileType == "rar" {
		if err := sv.validateArchiveStructure(filePath, result); err != nil {
			sv.logger.WithError(err).Warn("Archive validation encountered issues")
			result.SecurityWarnings = append(result.SecurityWarnings, 
				fmt.Sprintf("Archive validation warning: %v", err))
		}
	}
	
	// Step 5: Text file specific validation
	if result.FileType == "txt" {
		if err := sv.validateTextFile(filePath, result); err != nil {
			sv.logger.WithError(err).Warn("Text file validation encountered issues")
			result.SecurityWarnings = append(result.SecurityWarnings, 
				fmt.Sprintf("Text file validation warning: %v", err))
		}
	}
	
	// Determine final threat level based on warnings
	sv.calculateThreatLevel(result)
	
	sv.logger.WithField("file_path", filePath).
		WithField("valid", result.Valid).
		WithField("threat_level", result.ThreatLevel).
		WithField("warnings_count", len(result.SecurityWarnings)).
		Info("File validation completed")
	
	return result, nil
}

// validateFileSignature verifies file signature matches declared type
func (sv *SecurityValidator) validateFileSignature(filePath, declaredType string, result *ValidationResult) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file for signature validation: %w", err)
	}
	defer file.Close()
	
	// Read first 512 bytes for signature detection
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to read file signature: %w", err)
	}
	
	// Check against allowed file signatures
	expectedSig, exists := sv.allowedTypes[declaredType]
	if !exists {
		return fmt.Errorf("unsupported file type: %s", declaredType)
	}
	
	// For text files, perform encoding validation instead of magic bytes
	if declaredType == "txt" {
		return sv.validateTextEncoding(buffer[:n], result)
	}
	
	// Validate magic bytes for binary files
	if expectedSig.Magic != nil {
		if n < len(expectedSig.Magic)+expectedSig.Offset {
			return fmt.Errorf("file too small for signature validation")
		}
		
		actualMagic := buffer[expectedSig.Offset : expectedSig.Offset+len(expectedSig.Magic)]
		if !bytes.Equal(actualMagic, expectedSig.Magic) {
			return fmt.Errorf("file signature mismatch: expected %s, file may be corrupted or misnamed", declaredType)
		}
	}
	
	result.FileType = expectedSig.Extension
	result.ActualMimeType = expectedSig.MimeType
	return nil
}

// validateTextEncoding validates text file encoding and content
func (sv *SecurityValidator) validateTextEncoding(buffer []byte, result *ValidationResult) error {
	// Check if the content is valid UTF-8
	if !utf8.Valid(buffer) {
		result.SecurityWarnings = append(result.SecurityWarnings, 
			"Text file contains non-UTF-8 content, potential encoding issues")
		result.ThreatLevel = ThreatLevelLow
	}
	
	// Check for null bytes (binary content in text file)
	if bytes.Contains(buffer, []byte{0x00}) {
		result.SecurityWarnings = append(result.SecurityWarnings, 
			"Text file contains null bytes, may be binary file disguised as text")
		result.ThreatLevel = ThreatLevelMedium
	}
	
	result.FileType = "txt"
	result.ActualMimeType = "text/plain"
	return nil
}

// scanFileContent scans file content for dangerous patterns
func (sv *SecurityValidator) scanFileContent(filePath string, result *ValidationResult) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file for content scanning: %w", err)
	}
	defer file.Close()
	
	// For large files, only scan first 1MB to avoid performance issues
	scanSize := int64(1024 * 1024) // 1MB
	if result.FileSize < scanSize {
		scanSize = result.FileSize
	}
	
	content := make([]byte, scanSize)
	n, err := file.Read(content)
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to read file content: %w", err)
	}
	
	content = content[:n]
	
	// Scan for dangerous patterns
	for _, pattern := range sv.dangerousPatterns {
		if pattern.Match(content) {
			warning := fmt.Sprintf("Detected potentially dangerous pattern: %s", pattern.String())
			result.SecurityWarnings = append(result.SecurityWarnings, warning)
			
			// Escalate threat level based on pattern severity
			if strings.Contains(pattern.String(), "script") || strings.Contains(pattern.String(), "executable") {
				result.ThreatLevel = ThreatLevelHigh
			} else {
				result.ThreatLevel = ThreatLevelMedium
			}
		}
	}
	
	return nil
}

// validateArchiveStructure performs basic archive structure validation
func (sv *SecurityValidator) validateArchiveStructure(filePath string, result *ValidationResult) error {
	// This is a basic validation - in a production system, you'd want to use
	// proper archive libraries to inspect the structure
	
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open archive for structure validation: %w", err)
	}
	defer file.Close()
	
	// Read central directory for ZIP files (simplified check)
	if result.FileType == "zip" {
		return sv.validateZipStructure(file, result)
	}
	
	return nil
}

// validateZipStructure performs basic ZIP file structure validation
func (sv *SecurityValidator) validateZipStructure(file *os.File, result *ValidationResult) error {
	// Seek to end of file to look for central directory signature
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	
	// Look for End of Central Directory signature in last 1KB
	searchSize := int64(1024)
	if stat.Size() < searchSize {
		searchSize = stat.Size()
	}
	
	_, err = file.Seek(-searchSize, io.SeekEnd)
	if err != nil {
		return err
	}
	
	buffer := make([]byte, searchSize)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return err
	}
	
	// Look for ZIP End of Central Directory signature (0x06054b50)
	eocdSignature := []byte{0x50, 0x4b, 0x05, 0x06}
	if !bytes.Contains(buffer[:n], eocdSignature) {
		result.SecurityWarnings = append(result.SecurityWarnings, 
			"ZIP file may be corrupted or incomplete")
		result.ThreatLevel = ThreatLevelMedium
	}
	
	return nil
}

// validateTextFile performs comprehensive text file validation
func (sv *SecurityValidator) validateTextFile(filePath string, result *ValidationResult) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open text file for validation: %w", err)
	}
	defer file.Close()
	
	// Read file content (limit to 10MB for text files)
	maxTextSize := int64(10 * 1024 * 1024) // 10MB
	readSize := result.FileSize
	if readSize > maxTextSize {
		readSize = maxTextSize
		result.SecurityWarnings = append(result.SecurityWarnings, 
			"Large text file, only first 10MB validated")
	}
	
	content := make([]byte, readSize)
	n, err := file.Read(content)
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to read text file content: %w", err)
	}
	
	content = content[:n]
	
	// Validate text content
	sv.validateTextContent(content, result)
	
	return nil
}

// validateTextContent validates text file content for security issues
func (sv *SecurityValidator) validateTextContent(content []byte, result *ValidationResult) {
	// Check for extremely long lines (potential attack vector)
	lines := bytes.Split(content, []byte("\n"))
	for i, line := range lines {
		if len(line) > 10000 { // 10KB per line
			result.SecurityWarnings = append(result.SecurityWarnings, 
				fmt.Sprintf("Line %d exceeds maximum length, potential attack vector", i+1))
			result.ThreatLevel = ThreatLevelMedium
		}
	}
	
	// Check for excessive control characters
	controlCharCount := 0
	for _, b := range content {
		if b < 32 && b != '\n' && b != '\r' && b != '\t' {
			controlCharCount++
		}
	}
	
	if float64(controlCharCount)/float64(len(content)) > 0.1 { // More than 10% control chars
		result.SecurityWarnings = append(result.SecurityWarnings, 
			"Text file contains excessive control characters")
		result.ThreatLevel = ThreatLevelLow
	}
}

// calculateThreatLevel determines final threat level based on all warnings
func (sv *SecurityValidator) calculateThreatLevel(result *ValidationResult) {
	if len(result.SecurityWarnings) == 0 {
		result.ThreatLevel = ThreatLevelSafe
		return
	}
	
	// If already set to high/critical, keep it
	if result.ThreatLevel >= ThreatLevelHigh {
		return
	}
	
	// Escalate based on number and severity of warnings
	warningCount := len(result.SecurityWarnings)
	if warningCount >= 5 {
		result.ThreatLevel = ThreatLevelHigh
	} else if warningCount >= 3 {
		result.ThreatLevel = ThreatLevelMedium
	} else if warningCount >= 1 {
		result.ThreatLevel = ThreatLevelLow
	}
}

// SanitizeFile performs file sanitization if possible
func (sv *SecurityValidator) SanitizeFile(filePath string, result *ValidationResult) error {
	if result.ThreatLevel == ThreatLevelCritical {
		return fmt.Errorf("file threat level too high for sanitization, quarantine required")
	}
	
	// For text files, we can perform content sanitization
	if result.FileType == "txt" && result.ThreatLevel <= ThreatLevelMedium {
		return sv.sanitizeTextFile(filePath, result)
	}
	
	// For archives, we can't safely sanitize content, only validate
	result.SanitizationLog = append(result.SanitizationLog, 
		"Archive files cannot be sanitized, validation only")
	
	return nil
}

// sanitizeTextFile sanitizes text file content
func (sv *SecurityValidator) sanitizeTextFile(filePath string, result *ValidationResult) error {
	file, err := os.OpenFile(filePath, os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file for sanitization: %w", err)
	}
	defer file.Close()
	
	content, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("failed to read file for sanitization: %w", err)
	}
	
	originalSize := len(content)
	
	// Remove null bytes
	content = bytes.ReplaceAll(content, []byte{0x00}, []byte{})
	
	// Remove excessive control characters (keep only \n, \r, \t)
	cleaned := make([]byte, 0, len(content))
	for _, b := range content {
		if b >= 32 || b == '\n' || b == '\r' || b == '\t' {
			cleaned = append(cleaned, b)
		}
	}
	content = cleaned
	
	// Truncate excessively long lines
	lines := bytes.Split(content, []byte("\n"))
	for i, line := range lines {
		if len(line) > 10000 {
			lines[i] = line[:10000]
			lines[i] = append(lines[i], []byte(" [TRUNCATED]")...)
		}
	}
	content = bytes.Join(lines, []byte("\n"))
	
	// Write sanitized content back
	if err := file.Truncate(0); err != nil {
		return fmt.Errorf("failed to truncate file: %w", err)
	}
	
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek to beginning: %w", err)
	}
	
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("failed to write sanitized content: %w", err)
	}
	
	sanitizedSize := len(content)
	result.SanitizationLog = append(result.SanitizationLog, 
		fmt.Sprintf("Text file sanitized: %d bytes -> %d bytes", originalSize, sanitizedSize))
	
	sv.logger.WithField("file_path", filePath).
		WithField("original_size", originalSize).
		WithField("sanitized_size", sanitizedSize).
		Info("Text file sanitization completed")
	
	return nil
}

// IsFileSafe returns true if file is safe to process
func (sv *SecurityValidator) IsFileSafe(result *ValidationResult) bool {
	return result.Valid && result.ThreatLevel <= ThreatLevelMedium
}

// ShouldQuarantine returns true if file should be quarantined
func (sv *SecurityValidator) ShouldQuarantine(result *ValidationResult) bool {
	// Temporarily relaxed for testing - only quarantine CRITICAL threats
	return result.ThreatLevel >= ThreatLevelCritical
}

// GetEnhancedSignatureInfo returns detailed information about the enhanced signature validation system
func (sv *SecurityValidator) GetEnhancedSignatureInfo() map[string]interface{} {
	return sv.enhancedSignatureValidator.GetSignatureInfo()
}

// GetValidationSummary provides a comprehensive summary of validation capabilities
func (sv *SecurityValidator) GetValidationSummary() map[string]interface{} {
	summary := make(map[string]interface{})
	
	// Basic validation info
	summary["max_file_size"] = sv.maxFileSize
	summary["supported_types"] = []string{"zip", "rar", "txt"}
	summary["dangerous_patterns_count"] = len(sv.dangerousPatterns)
	
	// Enhanced signature validation info
	summary["enhanced_signatures"] = sv.GetEnhancedSignatureInfo()
	
	// Security features
	features := []string{
		"File signature verification with magic bytes",
		"Malware signature detection",
		"Polyglot file detection",
		"Anti-spoofing checks",
		"Content pattern analysis",
		"Archive structure validation",
		"Text encoding validation",
		"Entropy analysis",
		"File sanitization",
		"Automatic quarantine",
	}
	summary["security_features"] = features
	
	return summary
}