package utils

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// EnhancedSignatureValidator provides advanced file signature verification
type EnhancedSignatureValidator struct {
	logger              *Logger
	allowedSignatures   map[string][]FileSignatureRule
	malwareSignatures   []MalwareSignature
	polyglotPatterns    []PolyglotPattern
	suspiciousPatterns  []SuspiciousPattern
}

// FileSignatureRule represents a comprehensive file signature rule
type FileSignatureRule struct {
	Name        string
	Magic       []byte
	Offset      int
	Extension   string
	MimeType    string
	Description string
	MinSize     int64
	MaxSize     int64
	Required    bool // If true, file MUST match this signature
	Alternative bool // If true, this is an alternative signature for the same type
}

// MalwareSignature represents known malware signatures
type MalwareSignature struct {
	Name        string
	Pattern     []byte
	Offset      int
	Description string
	ThreatLevel ThreatLevel
}

// PolyglotPattern detects files that can be interpreted as multiple types
type PolyglotPattern struct {
	Name         string
	Signatures   [][]byte
	Description  string
	RiskLevel    ThreatLevel
}

// SuspiciousPattern represents potentially dangerous file characteristics
type SuspiciousPattern struct {
	Name        string
	Pattern     []byte
	Offset      int
	Description string
	Action      string
}

// SignatureValidationResult contains detailed signature analysis results
type SignatureValidationResult struct {
	FileType            string
	ConfidenceLevel     float64
	MatchedSignatures   []string
	DetectedMalware     []string
	PolyglotRisks       []string
	SuspiciousFeatures  []string
	AntiSpoofingChecks  []string
	IsGenuineFileType   bool
	SecurityWarnings    []string
	ThreatAssessment    ThreatLevel
}

// NewEnhancedSignatureValidator creates a new enhanced signature validator
func NewEnhancedSignatureValidator(logger *Logger) *EnhancedSignatureValidator {
	esv := &EnhancedSignatureValidator{
		logger: logger,
	}
	
	esv.initializeAllowedSignatures()
	esv.initializeMalwareSignatures()
	esv.initializePolyglotPatterns()
	esv.initializeSuspiciousPatterns()
	
	return esv
}

// initializeAllowedSignatures sets up comprehensive file signature rules
func (esv *EnhancedSignatureValidator) initializeAllowedSignatures() {
	esv.allowedSignatures = map[string][]FileSignatureRule{
		"zip": {
			{
				Name:        "ZIP Local File Header",
				Magic:       []byte{0x50, 0x4B, 0x03, 0x04},
				Offset:      0,
				Extension:   "zip",
				MimeType:    "application/zip",
				Description: "Standard ZIP archive",
				MinSize:     22, // Minimum ZIP file size
				MaxSize:     0,  // No max limit
				Required:    true,
			},
			{
				Name:        "ZIP Empty Archive",
				Magic:       []byte{0x50, 0x4B, 0x05, 0x06},
				Offset:      0,
				Extension:   "zip",
				MimeType:    "application/zip",
				Description: "Empty ZIP archive",
				MinSize:     22,
				MaxSize:     0,
				Required:    false,
				Alternative: true,
			},
			{
				Name:        "ZIP Spanned Archive",
				Magic:       []byte{0x50, 0x4B, 0x07, 0x08},
				Offset:      0,
				Extension:   "zip",
				MimeType:    "application/zip",
				Description: "ZIP spanned archive",
				MinSize:     22,
				MaxSize:     0,
				Required:    false,
				Alternative: true,
			},
		},
		"rar": {
			{
				Name:        "RAR v4.x Archive",
				Magic:       []byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x00},
				Offset:      0,
				Extension:   "rar",
				MimeType:    "application/vnd.rar",
				Description: "RAR version 4.x archive",
				MinSize:     20,
				MaxSize:     0,
				Required:    true,
			},
			{
				Name:        "RAR v5.x Archive",
				Magic:       []byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x01, 0x00},
				Offset:      0,
				Extension:   "rar",
				MimeType:    "application/vnd.rar",
				Description: "RAR version 5.x archive",
				MinSize:     20,
				MaxSize:     0,
				Required:    false,
				Alternative: true,
			},
		},
		"txt": {
			{
				Name:        "Text File (UTF-8 BOM)",
				Magic:       []byte{0xEF, 0xBB, 0xBF},
				Offset:      0,
				Extension:   "txt",
				MimeType:    "text/plain",
				Description: "UTF-8 text file with BOM",
				MinSize:     3,
				MaxSize:     0,
				Required:    false,
				Alternative: true,
			},
			{
				Name:        "Text File (UTF-16 LE BOM)",
				Magic:       []byte{0xFF, 0xFE},
				Offset:      0,
				Extension:   "txt",
				MimeType:    "text/plain",
				Description: "UTF-16 Little Endian text file with BOM",
				MinSize:     2,
				MaxSize:     0,
				Required:    false,
				Alternative: true,
			},
			{
				Name:        "Text File (UTF-16 BE BOM)",
				Magic:       []byte{0xFE, 0xFF},
				Offset:      0,
				Extension:   "txt",
				MimeType:    "text/plain",
				Description: "UTF-16 Big Endian text file with BOM",
				MinSize:     2,
				MaxSize:     0,
				Required:    false,
				Alternative: true,
			},
		},
	}
}

// initializeMalwareSignatures sets up known malware signatures
func (esv *EnhancedSignatureValidator) initializeMalwareSignatures() {
	esv.malwareSignatures = []MalwareSignature{
		{
			Name:        "PE Executable Header",
			Pattern:     []byte{0x4D, 0x5A}, // "MZ"
			Offset:      0,
			Description: "Windows PE executable embedded in file",
			ThreatLevel: ThreatLevelCritical,
		},
		{
			Name:        "ELF Executable Header",
			Pattern:     []byte{0x7F, 0x45, 0x4C, 0x46}, // "\x7FELF"
			Offset:      0,
			Description: "Linux ELF executable embedded in file",
			ThreatLevel: ThreatLevelCritical,
		},
		{
			Name:        "Mach-O Executable (32-bit)",
			Pattern:     []byte{0xFE, 0xED, 0xFA, 0xCE},
			Offset:      0,
			Description: "macOS Mach-O executable embedded in file",
			ThreatLevel: ThreatLevelCritical,
		},
		{
			Name:        "Mach-O Executable (64-bit)",
			Pattern:     []byte{0xFE, 0xED, 0xFA, 0xCF},
			Offset:      0,
			Description: "macOS Mach-O 64-bit executable embedded in file",
			ThreatLevel: ThreatLevelCritical,
		},
		{
			Name:        "Java Class File",
			Pattern:     []byte{0xCA, 0xFE, 0xBA, 0xBE},
			Offset:      0,
			Description: "Java class file (potential malware)",
			ThreatLevel: ThreatLevelHigh,
		},
		{
			Name:        "PDF with JavaScript",
			Pattern:     []byte("/JavaScript"),
			Offset:      -1, // Can appear anywhere
			Description: "PDF with potentially malicious JavaScript",
			ThreatLevel: ThreatLevelHigh,
		},
		{
			Name:        "HTML Script Tag",
			Pattern:     []byte("<script"),
			Offset:      -1,
			Description: "HTML with script tags in archive",
			ThreatLevel: ThreatLevelMedium,
		},
		{
			Name:        "VBS Script",
			Pattern:     []byte("WScript.Shell"),
			Offset:      -1,
			Description: "Visual Basic Script with shell access",
			ThreatLevel: ThreatLevelHigh,
		},
		{
			Name:        "PowerShell Command",
			Pattern:     []byte("powershell"),
			Offset:      -1,
			Description: "PowerShell command execution",
			ThreatLevel: ThreatLevelHigh,
		},
	}
}

// initializePolyglotPatterns sets up polyglot file detection
func (esv *EnhancedSignatureValidator) initializePolyglotPatterns() {
	esv.polyglotPatterns = []PolyglotPattern{
		{
			Name: "ZIP-PDF Polyglot",
			Signatures: [][]byte{
				{0x50, 0x4B, 0x03, 0x04}, // ZIP signature
				{0x25, 0x50, 0x44, 0x46}, // "%PDF"
			},
			Description: "File that can be interpreted as both ZIP and PDF",
			RiskLevel:   ThreatLevelHigh,
		},
		{
			Name: "ZIP-HTML Polyglot",
			Signatures: [][]byte{
				{0x50, 0x4B, 0x03, 0x04}, // ZIP signature
				[]byte("<html"),
			},
			Description: "File that can be interpreted as both ZIP and HTML",
			RiskLevel:   ThreatLevelMedium,
		},
		{
			Name: "RAR-EXE Polyglot",
			Signatures: [][]byte{
				{0x52, 0x61, 0x72, 0x21}, // RAR signature
				{0x4D, 0x5A},             // EXE signature
			},
			Description: "File that can be interpreted as both RAR and executable",
			RiskLevel:   ThreatLevelCritical,
		},
	}
}

// initializeSuspiciousPatterns sets up suspicious file characteristic detection
func (esv *EnhancedSignatureValidator) initializeSuspiciousPatterns() {
	esv.suspiciousPatterns = []SuspiciousPattern{
		{
			Name:        "Double Extension Pattern",
			Pattern:     []byte(".txt.exe"),
			Offset:      -1,
			Description: "File name with double extension (social engineering)",
			Action:      "quarantine",
		},
		{
			Name:        "Hidden Extension Pattern",
			Pattern:     []byte(".scr"),
			Offset:      -1,
			Description: "Screen saver file extension (often malware)",
			Action:      "reject",
		},
		{
			Name:        "Macro Signature",
			Pattern:     []byte("macroEnabled"),
			Offset:      -1,
			Description: "Document contains macros (potential threat)",
			Action:      "monitor",
		},
		{
			Name:        "Zip Bomb Indicator",
			Pattern:     []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, // Long sequence of zeros
			Offset:      -1,
			Description: "Potential zip bomb (highly compressed data)",
			Action:      "inspect",
		},
	}
}

// ValidateFileSignature performs comprehensive file signature validation
func (esv *EnhancedSignatureValidator) ValidateFileSignature(filePath, declaredType string) (*SignatureValidationResult, error) {
	result := &SignatureValidationResult{
		ConfidenceLevel:    0.0,
		MatchedSignatures:  make([]string, 0),
		DetectedMalware:    make([]string, 0),
		PolyglotRisks:      make([]string, 0),
		SuspiciousFeatures: make([]string, 0),
		AntiSpoofingChecks: make([]string, 0),
		SecurityWarnings:   make([]string, 0),
		ThreatAssessment:   ThreatLevelSafe,
	}
	
	// Read file header (first 8KB should be enough for most signatures)
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file for signature validation: %w", err)
	}
	defer file.Close()
	
	headerSize := int64(8192) // 8KB header
	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}
	
	if stat.Size() < headerSize {
		headerSize = stat.Size()
	}
	
	header := make([]byte, headerSize)
	n, err := file.Read(header)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read file header: %w", err)
	}
	header = header[:n]
	
	// Step 1: Validate against allowed signatures
	esv.validateAllowedSignatures(header, declaredType, result)
	
	// Step 2: Check for malware signatures
	esv.detectMalwareSignatures(header, result)
	
	// Step 3: Detect polyglot files
	esv.detectPolyglotPatterns(header, result)
	
	// Step 4: Check for suspicious patterns
	esv.detectSuspiciousPatterns(header, filePath, result)
	
	// Step 5: Perform anti-spoofing checks
	esv.performAntiSpoofingChecks(header, declaredType, stat.Size(), result)
	
	// Step 6: Calculate overall threat assessment
	esv.calculateThreatAssessment(result)
	
	esv.logger.WithField("file_path", filePath).
		WithField("declared_type", declaredType).
		WithField("confidence", result.ConfidenceLevel).
		WithField("threat_level", result.ThreatAssessment).
		WithField("genuine", result.IsGenuineFileType).
		Info("Enhanced signature validation completed")
	
	return result, nil
}

// validateAllowedSignatures checks if file matches allowed signature patterns
func (esv *EnhancedSignatureValidator) validateAllowedSignatures(header []byte, declaredType string, result *SignatureValidationResult) {
	rules, exists := esv.allowedSignatures[declaredType]
	if !exists {
		result.SecurityWarnings = append(result.SecurityWarnings, 
			fmt.Sprintf("No signature rules defined for file type: %s", declaredType))
		return
	}
	
	matchedRules := 0
	totalRules := 0
	
	for _, rule := range rules {
		totalRules++
		
		if esv.matchesSignature(header, rule) {
			result.MatchedSignatures = append(result.MatchedSignatures, rule.Name)
			matchedRules++
			
			if rule.Required {
				result.IsGenuineFileType = true
			}
		} else if rule.Required {
			result.SecurityWarnings = append(result.SecurityWarnings,
				fmt.Sprintf("Required signature not found: %s", rule.Name))
		}
	}
	
	// Calculate confidence based on matched signatures
	if totalRules > 0 {
		result.ConfidenceLevel = float64(matchedRules) / float64(totalRules)
	}
	
	result.FileType = declaredType
}

// matchesSignature checks if header matches a specific signature rule
func (esv *EnhancedSignatureValidator) matchesSignature(header []byte, rule FileSignatureRule) bool {
	if len(header) < rule.Offset+len(rule.Magic) {
		return false
	}
	
	if rule.Magic == nil {
		return true // No specific magic bytes required (e.g., text files)
	}
	
	startPos := rule.Offset
	return bytes.Equal(header[startPos:startPos+len(rule.Magic)], rule.Magic)
}

// detectMalwareSignatures scans for known malware patterns
func (esv *EnhancedSignatureValidator) detectMalwareSignatures(header []byte, result *SignatureValidationResult) {
	for _, signature := range esv.malwareSignatures {
		if esv.findPattern(header, signature.Pattern, signature.Offset) {
			result.DetectedMalware = append(result.DetectedMalware, signature.Name)
			result.SecurityWarnings = append(result.SecurityWarnings,
				fmt.Sprintf("Malware signature detected: %s - %s", signature.Name, signature.Description))
			
			// Escalate threat level
			if signature.ThreatLevel > result.ThreatAssessment {
				result.ThreatAssessment = signature.ThreatLevel
			}
		}
	}
}

// detectPolyglotPatterns checks for files that can be interpreted as multiple types
func (esv *EnhancedSignatureValidator) detectPolyglotPatterns(header []byte, result *SignatureValidationResult) {
	for _, pattern := range esv.polyglotPatterns {
		matchCount := 0
		for _, signature := range pattern.Signatures {
			if esv.findPattern(header, signature, 0) {
				matchCount++
			}
		}
		
		if matchCount >= 2 {
			result.PolyglotRisks = append(result.PolyglotRisks, pattern.Name)
			result.SecurityWarnings = append(result.SecurityWarnings,
				fmt.Sprintf("Polyglot file detected: %s - %s", pattern.Name, pattern.Description))
			
			// Escalate threat level
			if pattern.RiskLevel > result.ThreatAssessment {
				result.ThreatAssessment = pattern.RiskLevel
			}
		}
	}
}

// detectSuspiciousPatterns looks for potentially dangerous file characteristics
func (esv *EnhancedSignatureValidator) detectSuspiciousPatterns(header []byte, filePath string, result *SignatureValidationResult) {
	// Check header content
	for _, pattern := range esv.suspiciousPatterns {
		if esv.findPattern(header, pattern.Pattern, pattern.Offset) {
			result.SuspiciousFeatures = append(result.SuspiciousFeatures, pattern.Name)
			result.SecurityWarnings = append(result.SecurityWarnings,
				fmt.Sprintf("Suspicious pattern detected: %s - %s", pattern.Name, pattern.Description))
		}
	}
	
	// Check filename patterns
	fileName := strings.ToLower(filePath)
	suspiciousExtensions := []string{".exe", ".scr", ".bat", ".cmd", ".pif", ".com", ".vbs", ".js"}
	for _, ext := range suspiciousExtensions {
		if strings.Contains(fileName, ext) {
			result.SuspiciousFeatures = append(result.SuspiciousFeatures, 
				fmt.Sprintf("Suspicious extension in filename: %s", ext))
		}
	}
}

// performAntiSpoofingChecks validates file authenticity
func (esv *EnhancedSignatureValidator) performAntiSpoofingChecks(header []byte, declaredType string, fileSize int64, result *SignatureValidationResult) {
	// Check 1: File size consistency
	rules, exists := esv.allowedSignatures[declaredType]
	if exists {
		for _, rule := range rules {
			if rule.MinSize > 0 && fileSize < rule.MinSize {
				result.AntiSpoofingChecks = append(result.AntiSpoofingChecks,
					fmt.Sprintf("File too small for %s format (min: %d, actual: %d)", declaredType, rule.MinSize, fileSize))
				result.ThreatAssessment = ThreatLevelMedium
			}
			if rule.MaxSize > 0 && fileSize > rule.MaxSize {
				result.AntiSpoofingChecks = append(result.AntiSpoofingChecks,
					fmt.Sprintf("File too large for %s format (max: %d, actual: %d)", declaredType, rule.MaxSize, fileSize))
			}
		}
	}
	
	// Check 2: Header consistency for archives
	if declaredType == "zip" {
		esv.validateZipHeaderConsistency(header, result)
	} else if declaredType == "rar" {
		esv.validateRarHeaderConsistency(header, result)
	}
	
	// Check 3: Entropy analysis for potential encryption/packing
	entropy := esv.calculateEntropy(header)
	if entropy > 7.5 { // High entropy might indicate encryption or packing
		result.AntiSpoofingChecks = append(result.AntiSpoofingChecks,
			fmt.Sprintf("High entropy detected (%.2f) - possible encryption or packing", entropy))
		result.ThreatAssessment = ThreatLevelMedium
	}
}

// validateZipHeaderConsistency checks ZIP file structure consistency
func (esv *EnhancedSignatureValidator) validateZipHeaderConsistency(header []byte, result *SignatureValidationResult) {
	if len(header) < 30 {
		result.AntiSpoofingChecks = append(result.AntiSpoofingChecks, "ZIP header too short")
		return
	}
	
	// Check version needed to extract (bytes 4-5)
	versionNeeded := int(header[4]) | (int(header[5]) << 8)
	if versionNeeded > 63 { // Unreasonably high version
		result.AntiSpoofingChecks = append(result.AntiSpoofingChecks,
			fmt.Sprintf("Suspicious ZIP version: %d", versionNeeded))
	}
	
	// Check filename length (bytes 26-27)
	filenameLen := int(header[26]) | (int(header[27]) << 8)
	if filenameLen > 1024 { // Unreasonably long filename
		result.AntiSpoofingChecks = append(result.AntiSpoofingChecks,
			fmt.Sprintf("Suspicious filename length in ZIP: %d", filenameLen))
		result.ThreatAssessment = ThreatLevelMedium
	}
}

// validateRarHeaderConsistency checks RAR file structure consistency
func (esv *EnhancedSignatureValidator) validateRarHeaderConsistency(header []byte, result *SignatureValidationResult) {
	if len(header) < 20 {
		result.AntiSpoofingChecks = append(result.AntiSpoofingChecks, "RAR header too short")
		return
	}
	
	// RAR version check (byte 6)
	if len(header) > 6 {
		version := header[6]
		if version > 5 { // Version higher than known RAR versions
			result.AntiSpoofingChecks = append(result.AntiSpoofingChecks,
				fmt.Sprintf("Unknown RAR version: %d", version))
		}
	}
}

// calculateEntropy calculates Shannon entropy of data
func (esv *EnhancedSignatureValidator) calculateEntropy(data []byte) float64 {
	if len(data) == 0 {
		return 0.0
	}
	
	// Count frequency of each byte value
	freq := make(map[byte]int)
	for _, b := range data {
		freq[b]++
	}
	
	// Calculate entropy
	entropy := 0.0
	dataLen := float64(len(data))
	
	for _, count := range freq {
		if count > 0 {
			p := float64(count) / dataLen
			entropy -= p * (float64(count) / dataLen) // Simplified calculation
		}
	}
	
	return entropy * 8.0 // Scale to bits
}

// findPattern searches for a pattern in data at specified offset
func (esv *EnhancedSignatureValidator) findPattern(data, pattern []byte, offset int) bool {
	if len(pattern) == 0 {
		return false
	}
	
	if offset == -1 {
		// Search anywhere in the data
		return bytes.Contains(data, pattern)
	}
	
	if offset < 0 || offset+len(pattern) > len(data) {
		return false
	}
	
	return bytes.Equal(data[offset:offset+len(pattern)], pattern)
}

// calculateThreatAssessment determines overall threat level
func (esv *EnhancedSignatureValidator) calculateThreatAssessment(result *SignatureValidationResult) {
	// Start with current threat level
	maxThreat := result.ThreatAssessment
	
	// Escalate based on findings
	if len(result.DetectedMalware) > 0 {
		maxThreat = ThreatLevelCritical
	} else if len(result.PolyglotRisks) > 0 {
		if maxThreat < ThreatLevelHigh {
			maxThreat = ThreatLevelHigh
		}
	} else if len(result.SuspiciousFeatures) > 2 {
		if maxThreat < ThreatLevelMedium {
			maxThreat = ThreatLevelMedium
		}
	} else if len(result.AntiSpoofingChecks) > 0 {
		if maxThreat < ThreatLevelLow {
			maxThreat = ThreatLevelLow
		}
	} else if !result.IsGenuineFileType && result.ConfidenceLevel < 0.5 {
		if maxThreat < ThreatLevelMedium {
			maxThreat = ThreatLevelMedium
		}
	}
	
	result.ThreatAssessment = maxThreat
}

// GetSignatureInfo returns detailed information about supported file signatures
func (esv *EnhancedSignatureValidator) GetSignatureInfo() map[string]interface{} {
	info := make(map[string]interface{})
	
	// Allowed signatures info
	allowedInfo := make(map[string]interface{})
	for fileType, rules := range esv.allowedSignatures {
		ruleInfo := make([]map[string]interface{}, 0)
		for _, rule := range rules {
			ruleInfo = append(ruleInfo, map[string]interface{}{
				"name":        rule.Name,
				"magic_hex":   hex.EncodeToString(rule.Magic),
				"offset":      rule.Offset,
				"mime_type":   rule.MimeType,
				"description": rule.Description,
				"required":    rule.Required,
				"alternative": rule.Alternative,
			})
		}
		allowedInfo[fileType] = ruleInfo
	}
	info["allowed_signatures"] = allowedInfo
	
	// Malware signatures count
	info["malware_signatures_count"] = len(esv.malwareSignatures)
	
	// Polyglot patterns count
	info["polyglot_patterns_count"] = len(esv.polyglotPatterns)
	
	// Suspicious patterns count
	info["suspicious_patterns_count"] = len(esv.suspiciousPatterns)
	
	return info
}