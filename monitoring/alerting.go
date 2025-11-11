package monitoring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"telegram-archive-bot/utils"
)

// AlertLevel represents the severity of an alert
type AlertLevel string

const (
	AlertLevelInfo     AlertLevel = "INFO"
	AlertLevelWarning  AlertLevel = "WARNING"
	AlertLevelCritical AlertLevel = "CRITICAL"
)

// AlertType represents the type of alert
type AlertType string

const (
	AlertTypeHealthCheck    AlertType = "HEALTH_CHECK"
	AlertTypeHighMemory     AlertType = "HIGH_MEMORY"
	AlertTypeHighCPU        AlertType = "HIGH_CPU"
	AlertTypeDiskSpace      AlertType = "DISK_SPACE"
	AlertTypeQueueBackup    AlertType = "QUEUE_BACKUP"
	AlertTypeProcessFailure AlertType = "PROCESS_FAILURE"
	AlertTypeSystemFailure  AlertType = "SYSTEM_FAILURE"
	AlertTypeComponentDown  AlertType = "COMPONENT_DOWN"
	AlertTypeHighLoadAvg    AlertType = "HIGH_LOAD_AVERAGE"
)

// Alert represents a system alert
type Alert struct {
	ID          string                 `json:"id"`
	Type        AlertType              `json:"type"`
	Level       AlertLevel             `json:"level"`
	Title       string                 `json:"title"`
	Message     string                 `json:"message"`
	Timestamp   time.Time              `json:"timestamp"`
	Component   string                 `json:"component,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Resolved    bool                   `json:"resolved"`
	ResolvedAt  *time.Time             `json:"resolved_at,omitempty"`
	Count       int                    `json:"count"`
	LastSeen    time.Time              `json:"last_seen"`
}

// AlertRule represents conditions that trigger alerts
type AlertRule struct {
	Name        string                 `json:"name"`
	Type        AlertType              `json:"type"`
	Level       AlertLevel             `json:"level"`
	Condition   func(snapshot *SystemResourceSnapshot, metrics *PerformanceMetrics) bool
	Message     string                 `json:"message"`
	Cooldown    time.Duration          `json:"cooldown"`
	Enabled     bool                   `json:"enabled"`
	LastFired   time.Time              `json:"last_fired"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// AlertManager manages alerts and notifications
type AlertManager struct {
	logger          *utils.Logger
	rules           map[string]*AlertRule
	activeAlerts    map[string]*Alert
	alertHistory    []*Alert
	mutex           sync.RWMutex
	notificationCh  chan *Alert
	ctx             context.Context
	cancel          context.CancelFunc
	alertCallbacks  []AlertCallback
	maxHistorySize  int
}

// AlertCallback is called when an alert is triggered
type AlertCallback func(alert *Alert)

// NewAlertManager creates a new alert manager
func NewAlertManager(logger *utils.Logger) *AlertManager {
	ctx, cancel := context.WithCancel(context.Background())
	
	am := &AlertManager{
		logger:         logger,
		rules:          make(map[string]*AlertRule),
		activeAlerts:   make(map[string]*Alert),
		alertHistory:   make([]*Alert, 0),
		notificationCh: make(chan *Alert, 100),
		ctx:            ctx,
		cancel:         cancel,
		maxHistorySize: 1000,
	}
	
	// Setup default alert rules
	am.setupDefaultRules()
	
	// Start the notification processor
	go am.processNotifications()
	
	logger.Info("Alert manager initialized with default rules")
	return am
}

// setupDefaultRules configures default alerting rules
func (am *AlertManager) setupDefaultRules() {
	// High memory usage alert
	am.AddRule(&AlertRule{
		Name:  "high_memory_usage",
		Type:  AlertTypeHighMemory,
		Level: AlertLevelWarning,
		Condition: func(snapshot *SystemResourceSnapshot, metrics *PerformanceMetrics) bool {
			return snapshot != nil && snapshot.Memory.AllocMB > 500 // 500MB threshold
		},
		Message:  "High memory usage detected: %.1fMB allocated",
		Cooldown: 5 * time.Minute,
		Enabled:  true,
	})
	
	// Critical memory usage alert
	am.AddRule(&AlertRule{
		Name:  "critical_memory_usage",
		Type:  AlertTypeHighMemory,
		Level: AlertLevelCritical,
		Condition: func(snapshot *SystemResourceSnapshot, metrics *PerformanceMetrics) bool {
			return snapshot != nil && snapshot.Memory.AllocMB > 1000 // 1GB threshold
		},
		Message:  "Critical memory usage detected: %.1fMB allocated",
		Cooldown: 2 * time.Minute,
		Enabled:  true,
	})
	
	// High CPU usage alert
	am.AddRule(&AlertRule{
		Name:  "high_cpu_usage",
		Type:  AlertTypeHighCPU,
		Level: AlertLevelWarning,
		Condition: func(snapshot *SystemResourceSnapshot, metrics *PerformanceMetrics) bool {
			return snapshot != nil && snapshot.CPU.TotalPercent > 80 // 80% threshold
		},
		Message:  "High CPU usage detected: %.1f%% utilization",
		Cooldown: 3 * time.Minute,
		Enabled:  true,
	})
	
	// Disk space alert
	am.AddRule(&AlertRule{
		Name:  "low_disk_space",
		Type:  AlertTypeDiskSpace,
		Level: AlertLevelWarning,
		Condition: func(snapshot *SystemResourceSnapshot, metrics *PerformanceMetrics) bool {
			if snapshot == nil || snapshot.Disk == nil {
				return false
			}
			for _, disk := range snapshot.Disk {
				if disk.UsedPercent > 85 { // 85% threshold
					return true
				}
			}
			return false
		},
		Message:  "Low disk space detected on one or more volumes",
		Cooldown: 10 * time.Minute,
		Enabled:  true,
	})
	
	// Queue backup alert
	am.AddRule(&AlertRule{
		Name:  "queue_backup",
		Type:  AlertTypeQueueBackup,
		Level: AlertLevelWarning,
		Condition: func(snapshot *SystemResourceSnapshot, metrics *PerformanceMetrics) bool {
			if metrics == nil {
				return false
			}
			queueMetrics := metrics.GetQueueMetrics()
			return queueMetrics.QueueDepth > 50 // 50 items threshold
		},
		Message:  "Queue backup detected: %d items in queue",
		Cooldown: 5 * time.Minute,
		Enabled:  true,
	})
	
	// High load average alert (Linux)
	am.AddRule(&AlertRule{
		Name:  "high_load_average",
		Type:  AlertTypeHighLoadAvg,
		Level: AlertLevelWarning,
		Condition: func(snapshot *SystemResourceSnapshot, metrics *PerformanceMetrics) bool {
			if snapshot == nil || len(snapshot.LoadAvg) == 0 {
				return false
			}
			// Alert if 5-minute load average > 2.0
			return snapshot.LoadAvg[1] > 2.0
		},
		Message:  "High system load average detected: %.2f (5min)",
		Cooldown: 5 * time.Minute,
		Enabled:  true,
	})
	
	// Too many goroutines alert
	am.AddRule(&AlertRule{
		Name:  "high_goroutine_count",
		Type:  AlertTypeSystemFailure,
		Level: AlertLevelWarning,
		Condition: func(snapshot *SystemResourceSnapshot, metrics *PerformanceMetrics) bool {
			return snapshot != nil && snapshot.Process.Goroutines > 1000 // 1000 goroutines threshold
		},
		Message:  "High goroutine count detected: %d goroutines",
		Cooldown: 5 * time.Minute,
		Enabled:  true,
	})
	
	// Failed tasks accumulation alert
	am.AddRule(&AlertRule{
		Name:  "high_failure_rate",
		Type:  AlertTypeProcessFailure,
		Level: AlertLevelWarning,
		Condition: func(snapshot *SystemResourceSnapshot, metrics *PerformanceMetrics) bool {
			if metrics == nil {
				return false
			}
			queueMetrics := metrics.GetQueueMetrics()
			totalProcessed := queueMetrics.CompletedTasks + queueMetrics.FailedTasks
			if totalProcessed < 10 { // Need at least 10 processed tasks
				return false
			}
			failureRate := float64(queueMetrics.FailedTasks) / float64(totalProcessed) * 100
			return failureRate > 25 // 25% failure rate threshold
		},
		Message:  "High failure rate detected: %.1f%% of tasks are failing",
		Cooldown: 10 * time.Minute,
		Enabled:  true,
	})
}

// AddRule adds a new alert rule
func (am *AlertManager) AddRule(rule *AlertRule) {
	am.mutex.Lock()
	defer am.mutex.Unlock()
	
	if rule.Name == "" {
		am.logger.Error("Cannot add alert rule without name")
		return
	}
	
	am.rules[rule.Name] = rule
	am.logger.WithField("rule_name", rule.Name).
		WithField("rule_type", string(rule.Type)).
		WithField("rule_level", string(rule.Level)).
		Info("Alert rule added")
}

// RemoveRule removes an alert rule
func (am *AlertManager) RemoveRule(name string) {
	am.mutex.Lock()
	defer am.mutex.Unlock()
	
	delete(am.rules, name)
	am.logger.WithField("rule_name", name).Info("Alert rule removed")
}

// AddAlertCallback adds a callback function for alert notifications
func (am *AlertManager) AddAlertCallback(callback AlertCallback) {
	am.mutex.Lock()
	defer am.mutex.Unlock()
	
	am.alertCallbacks = append(am.alertCallbacks, callback)
}

// CheckAlerts evaluates all alert rules against current system state
func (am *AlertManager) CheckAlerts(snapshot *SystemResourceSnapshot, metrics *PerformanceMetrics) {
	am.mutex.RLock()
	rules := make([]*AlertRule, 0, len(am.rules))
	for _, rule := range am.rules {
		if rule.Enabled {
			rules = append(rules, rule)
		}
	}
	am.mutex.RUnlock()
	
	now := time.Now()
	
	for _, rule := range rules {
		// Check cooldown
		if !rule.LastFired.IsZero() && now.Sub(rule.LastFired) < rule.Cooldown {
			continue
		}
		
		// Evaluate condition
		if rule.Condition(snapshot, metrics) {
			am.triggerAlert(rule, snapshot, metrics)
			rule.LastFired = now
		}
	}
	
	// Check for alerts that should be auto-resolved
	am.checkAutoResolve(snapshot, metrics)
}

// triggerAlert creates and processes a new alert
func (am *AlertManager) triggerAlert(rule *AlertRule, snapshot *SystemResourceSnapshot, metrics *PerformanceMetrics) {
	alertID := fmt.Sprintf("%s_%d", rule.Name, time.Now().Unix())
	
	// Prepare alert message with context-specific data
	message := rule.Message
	switch rule.Type {
	case AlertTypeHighMemory:
		if snapshot != nil {
			message = fmt.Sprintf(rule.Message, snapshot.Memory.AllocMB)
		}
	case AlertTypeHighCPU:
		if snapshot != nil {
			message = fmt.Sprintf(rule.Message, snapshot.CPU.TotalPercent)
		}
	case AlertTypeQueueBackup:
		if metrics != nil {
			queueMetrics := metrics.GetQueueMetrics()
			message = fmt.Sprintf(rule.Message, queueMetrics.QueueDepth)
		}
	case AlertTypeHighLoadAvg:
		if snapshot != nil && len(snapshot.LoadAvg) > 1 {
			message = fmt.Sprintf(rule.Message, snapshot.LoadAvg[1])
		}
	case AlertTypeSystemFailure:
		if snapshot != nil {
			message = fmt.Sprintf(rule.Message, snapshot.Process.Goroutines)
		}
	case AlertTypeProcessFailure:
		if metrics != nil {
			queueMetrics := metrics.GetQueueMetrics()
			totalProcessed := queueMetrics.CompletedTasks + queueMetrics.FailedTasks
			failureRate := float64(queueMetrics.FailedTasks) / float64(totalProcessed) * 100
			message = fmt.Sprintf(rule.Message, failureRate)
		}
	}
	
	am.mutex.Lock()
	defer am.mutex.Unlock()
	
	// Check if we already have an active alert of this type
	existingKey := fmt.Sprintf("%s_%s", rule.Type, rule.Name)
	if existingAlert, exists := am.activeAlerts[existingKey]; exists {
		// Update existing alert
		existingAlert.Count++
		existingAlert.LastSeen = time.Now()
		existingAlert.Message = message
	} else {
		// Create new alert
		alert := &Alert{
			ID:        alertID,
			Type:      rule.Type,
			Level:     rule.Level,
			Title:     fmt.Sprintf("%s Alert", string(rule.Type)),
			Message:   message,
			Timestamp: time.Now(),
			Count:     1,
			LastSeen:  time.Now(),
			Metadata:  rule.Metadata,
		}
		
		am.activeAlerts[existingKey] = alert
		am.addToHistory(alert)
		
		// Send notification
		select {
		case am.notificationCh <- alert:
		default:
			am.logger.Warn("Alert notification channel full, dropping alert")
		}
	}
}

// checkAutoResolve checks if any active alerts should be automatically resolved
func (am *AlertManager) checkAutoResolve(snapshot *SystemResourceSnapshot, metrics *PerformanceMetrics) {
	am.mutex.Lock()
	defer am.mutex.Unlock()
	
	now := time.Now()
	
	for key, alert := range am.activeAlerts {
		// Auto-resolve alerts that haven't been seen in the last 10 minutes
		if now.Sub(alert.LastSeen) > 10*time.Minute {
			alert.Resolved = true
			alert.ResolvedAt = &now
			delete(am.activeAlerts, key)
			
			am.logger.WithField("alert_id", alert.ID).
				WithField("alert_type", string(alert.Type)).
				Info("Alert auto-resolved")
		}
	}
}

// processNotifications handles alert notifications
func (am *AlertManager) processNotifications() {
	for {
		select {
		case <-am.ctx.Done():
			return
		case alert := <-am.notificationCh:
			am.handleAlert(alert)
		}
	}
}

// handleAlert processes an alert by calling registered callbacks
func (am *AlertManager) handleAlert(alert *Alert) {
	am.logger.WithField("alert_id", alert.ID).
		WithField("alert_type", string(alert.Type)).
		WithField("alert_level", string(alert.Level)).
		WithField("alert_message", alert.Message).
		Warn("Alert triggered")
	
	am.mutex.RLock()
	callbacks := make([]AlertCallback, len(am.alertCallbacks))
	copy(callbacks, am.alertCallbacks)
	am.mutex.RUnlock()
	
	// Call all registered callbacks
	for _, callback := range callbacks {
		go func(cb AlertCallback) {
			defer func() {
				if r := recover(); r != nil {
					am.logger.WithField("panic", r).Error("Alert callback panicked")
				}
			}()
			cb(alert)
		}(callback)
	}
}

// addToHistory adds an alert to the history
func (am *AlertManager) addToHistory(alert *Alert) {
	// Add to history
	am.alertHistory = append(am.alertHistory, alert)
	
	// Trim history if it gets too large
	if len(am.alertHistory) > am.maxHistorySize {
		// Remove oldest alerts
		am.alertHistory = am.alertHistory[len(am.alertHistory)-am.maxHistorySize:]
	}
}

// GetActiveAlerts returns all currently active alerts
func (am *AlertManager) GetActiveAlerts() []*Alert {
	am.mutex.RLock()
	defer am.mutex.RUnlock()
	
	alerts := make([]*Alert, 0, len(am.activeAlerts))
	for _, alert := range am.activeAlerts {
		alerts = append(alerts, alert)
	}
	return alerts
}

// GetAlertHistory returns recent alert history
func (am *AlertManager) GetAlertHistory(limit int) []*Alert {
	am.mutex.RLock()
	defer am.mutex.RUnlock()
	
	if limit <= 0 || limit > len(am.alertHistory) {
		limit = len(am.alertHistory)
	}
	
	// Return the most recent alerts
	start := len(am.alertHistory) - limit
	if start < 0 {
		start = 0
	}
	
	result := make([]*Alert, limit)
	copy(result, am.alertHistory[start:])
	return result
}

// ResolveAlert manually resolves an active alert
func (am *AlertManager) ResolveAlert(alertID string) bool {
	am.mutex.Lock()
	defer am.mutex.Unlock()
	
	for key, alert := range am.activeAlerts {
		if alert.ID == alertID {
			now := time.Now()
			alert.Resolved = true
			alert.ResolvedAt = &now
			delete(am.activeAlerts, key)
			
			am.logger.WithField("alert_id", alertID).Info("Alert manually resolved")
			return true
		}
	}
	return false
}

// GetAlertStats returns statistics about alerts
func (am *AlertManager) GetAlertStats() map[string]interface{} {
	am.mutex.RLock()
	defer am.mutex.RUnlock()
	
	stats := map[string]interface{}{
		"active_alerts":    len(am.activeAlerts),
		"total_rules":      len(am.rules),
		"enabled_rules":    0,
		"alerts_by_level":  make(map[string]int),
		"alerts_by_type":   make(map[string]int),
		"history_size":     len(am.alertHistory),
	}
	
	// Count enabled rules
	for _, rule := range am.rules {
		if rule.Enabled {
			stats["enabled_rules"] = stats["enabled_rules"].(int) + 1
		}
	}
	
	// Count alerts by level and type
	levelCounts := stats["alerts_by_level"].(map[string]int)
	typeCounts := stats["alerts_by_type"].(map[string]int)
	
	for _, alert := range am.activeAlerts {
		levelCounts[string(alert.Level)]++
		typeCounts[string(alert.Type)]++
	}
	
	return stats
}

// Stop shuts down the alert manager
func (am *AlertManager) Stop() {
	am.logger.Info("Stopping alert manager")
	am.cancel()
	close(am.notificationCh)
}