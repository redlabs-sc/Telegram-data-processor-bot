package monitoring

import (
	"sync"
	"time"

	"telegram-archive-bot/models"
	"telegram-archive-bot/utils"
)

// MetricType represents different types of metrics
type MetricType string

const (
	MetricTypeCounter   MetricType = "counter"
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeHistogram MetricType = "histogram"
	MetricTypeTiming    MetricType = "timing"
)

// PerformanceMetrics collects and aggregates application performance data
type PerformanceMetrics struct {
	logger   *utils.Logger
	metrics  map[string]*Metric
	timings  map[string]*TimingMetric
	counters map[string]*CounterMetric
	gauges   map[string]*GaugeMetric
	mutex    sync.RWMutex
	
	// Performance tracking
	downloadMetrics    *ProcessingMetrics
	extractionMetrics  *ProcessingMetrics
	conversionMetrics  *ProcessingMetrics
	
	// Queue metrics
	queueMetrics *QueueMetrics
	
	// System metrics
	systemMetrics *SystemMetrics
	
	startTime time.Time
}

// Metric represents a generic metric
type Metric struct {
	Name        string      `json:"name"`
	Type        MetricType  `json:"type"`
	Value       interface{} `json:"value"`
	Description string      `json:"description"`
	LastUpdated time.Time   `json:"last_updated"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// TimingMetric tracks timing statistics
type TimingMetric struct {
	Name        string        `json:"name"`
	Count       int64         `json:"count"`
	TotalTime   time.Duration `json:"total_time"`
	MinTime     time.Duration `json:"min_time"`
	MaxTime     time.Duration `json:"max_time"`
	AvgTime     time.Duration `json:"avg_time"`
	LastUpdated time.Time     `json:"last_updated"`
}

// CounterMetric tracks counting statistics
type CounterMetric struct {
	Name        string    `json:"name"`
	Value       int64     `json:"value"`
	Rate        float64   `json:"rate_per_minute"`
	LastUpdated time.Time `json:"last_updated"`
	LastReset   time.Time `json:"last_reset"`
}

// GaugeMetric tracks current values
type GaugeMetric struct {
	Name        string    `json:"name"`
	Value       float64   `json:"value"`
	LastUpdated time.Time `json:"last_updated"`
}

// ProcessingMetrics tracks metrics for a processing stage
type ProcessingMetrics struct {
	Stage           string        `json:"stage"`
	TotalProcessed  int64         `json:"total_processed"`
	TotalFailed     int64         `json:"total_failed"`
	SuccessRate     float64       `json:"success_rate"`
	AvgProcessTime  time.Duration `json:"avg_process_time"`
	MinProcessTime  time.Duration `json:"min_process_time"`
	MaxProcessTime  time.Duration `json:"max_process_time"`
	Throughput      float64       `json:"throughput_per_hour"`
	ActiveJobs      int           `json:"active_jobs"`
	LastUpdated     time.Time     `json:"last_updated"`
}

// QueueMetrics tracks queue performance
type QueueMetrics struct {
	PendingTasks    int     `json:"pending_tasks"`
	DownloadedTasks int     `json:"downloaded_tasks"`
	ProcessingTasks int     `json:"processing_tasks"`
	CompletedTasks  int     `json:"completed_tasks"`
	FailedTasks     int     `json:"failed_tasks"`
	TotalTasks      int     `json:"total_tasks"`
	QueueDepth      int     `json:"queue_depth"`
	AvgWaitTime     float64 `json:"avg_wait_time_minutes"`
	LastUpdated     time.Time `json:"last_updated"`
}

// SystemMetrics tracks system-level performance
type SystemMetrics struct {
	CPUUsage        float64   `json:"cpu_usage_percent"`
	MemoryUsage     float64   `json:"memory_usage_mb"`
	MemoryPercent   float64   `json:"memory_percent"`
	GoroutineCount  int       `json:"goroutine_count"`
	FileDescriptors int       `json:"file_descriptors"`
	DiskUsageBytes  int64     `json:"disk_usage_bytes"`
	NetworkIO       NetworkIO `json:"network_io"`
	LastUpdated     time.Time `json:"last_updated"`
}

// NetworkIO tracks network statistics
type NetworkIO struct {
	BytesReceived int64 `json:"bytes_received"`
	BytesSent     int64 `json:"bytes_sent"`
	PacketsReceived int64 `json:"packets_received"`
	PacketsSent   int64 `json:"packets_sent"`
}

// TimingContext tracks timing for operations
type TimingContext struct {
	startTime time.Time
	metric    *TimingMetric
	mutex     *sync.RWMutex
}

// NewPerformanceMetrics creates a new performance metrics collector
func NewPerformanceMetrics(logger *utils.Logger) *PerformanceMetrics {
	pm := &PerformanceMetrics{
		logger:    logger,
		metrics:   make(map[string]*Metric),
		timings:   make(map[string]*TimingMetric),
		counters:  make(map[string]*CounterMetric),
		gauges:    make(map[string]*GaugeMetric),
		startTime: time.Now(),
	}
	
	// Initialize processing metrics
	pm.downloadMetrics = &ProcessingMetrics{
		Stage:          "download",
		MinProcessTime: time.Hour, // Initialize to high value
		LastUpdated:    time.Now(),
	}
	pm.extractionMetrics = &ProcessingMetrics{
		Stage:          "extraction",
		MinProcessTime: time.Hour,
		LastUpdated:    time.Now(),
	}
	pm.conversionMetrics = &ProcessingMetrics{
		Stage:          "conversion",
		MinProcessTime: time.Hour,
		LastUpdated:    time.Now(),
	}
	
	// Initialize queue metrics
	pm.queueMetrics = &QueueMetrics{
		LastUpdated: time.Now(),
	}
	
	// Initialize system metrics
	pm.systemMetrics = &SystemMetrics{
		LastUpdated: time.Now(),
	}
	
	// Initialize basic counters and gauges
	pm.initializeMetrics()
	
	logger.Info("Performance metrics collector initialized")
	return pm
}

// initializeMetrics sets up basic metrics
func (pm *PerformanceMetrics) initializeMetrics() {
	// Counters
	pm.counters["files_received"] = &CounterMetric{
		Name:        "files_received",
		LastReset:   time.Now(),
		LastUpdated: time.Now(),
	}
	pm.counters["downloads_completed"] = &CounterMetric{
		Name:        "downloads_completed",
		LastReset:   time.Now(),
		LastUpdated: time.Now(),
	}
	pm.counters["downloads_failed"] = &CounterMetric{
		Name:        "downloads_failed",
		LastReset:   time.Now(),
		LastUpdated: time.Now(),
	}
	pm.counters["extractions_completed"] = &CounterMetric{
		Name:        "extractions_completed",
		LastReset:   time.Now(),
		LastUpdated: time.Now(),
	}
	pm.counters["extractions_failed"] = &CounterMetric{
		Name:        "extractions_failed",
		LastReset:   time.Now(),
		LastUpdated: time.Now(),
	}
	pm.counters["conversions_completed"] = &CounterMetric{
		Name:        "conversions_completed",
		LastReset:   time.Now(),
		LastUpdated: time.Now(),
	}
	pm.counters["conversions_failed"] = &CounterMetric{
		Name:        "conversions_failed",
		LastReset:   time.Now(),
		LastUpdated: time.Now(),
	}
	
	// Gauges
	pm.gauges["active_downloads"] = &GaugeMetric{
		Name:        "active_downloads",
		LastUpdated: time.Now(),
	}
	pm.gauges["active_extractions"] = &GaugeMetric{
		Name:        "active_extractions",
		LastUpdated: time.Now(),
	}
	pm.gauges["active_conversions"] = &GaugeMetric{
		Name:        "active_conversions",
		LastUpdated: time.Now(),
	}
	
	// Timing metrics
	pm.timings["download_duration"] = &TimingMetric{
		Name:        "download_duration",
		MinTime:     time.Hour, // Initialize to high value
		LastUpdated: time.Now(),
	}
	pm.timings["extraction_duration"] = &TimingMetric{
		Name:        "extraction_duration",
		MinTime:     time.Hour,
		LastUpdated: time.Now(),
	}
	pm.timings["conversion_duration"] = &TimingMetric{
		Name:        "conversion_duration",
		MinTime:     time.Hour,
		LastUpdated: time.Now(),
	}
	pm.timings["total_processing_duration"] = &TimingMetric{
		Name:        "total_processing_duration",
		MinTime:     time.Hour,
		LastUpdated: time.Now(),
	}
}

// IncrementCounter increments a counter metric
func (pm *PerformanceMetrics) IncrementCounter(name string) {
	pm.IncrementCounterBy(name, 1)
}

// IncrementCounterBy increments a counter by a specific amount
func (pm *PerformanceMetrics) IncrementCounterBy(name string, value int64) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	counter, exists := pm.counters[name]
	if !exists {
		counter = &CounterMetric{
			Name:      name,
			LastReset: time.Now(),
		}
		pm.counters[name] = counter
	}
	
	counter.Value += value
	counter.LastUpdated = time.Now()
	
	// Calculate rate per minute
	duration := time.Since(counter.LastReset).Minutes()
	if duration > 0 {
		counter.Rate = float64(counter.Value) / duration
	}
}

// SetGauge sets a gauge metric value
func (pm *PerformanceMetrics) SetGauge(name string, value float64) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	gauge, exists := pm.gauges[name]
	if !exists {
		gauge = &GaugeMetric{Name: name}
		pm.gauges[name] = gauge
	}
	
	gauge.Value = value
	gauge.LastUpdated = time.Now()
}

// StartTiming begins timing an operation
func (pm *PerformanceMetrics) StartTiming(name string) *TimingContext {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	timing, exists := pm.timings[name]
	if !exists {
		timing = &TimingMetric{
			Name:    name,
			MinTime: time.Hour, // Initialize to high value
		}
		pm.timings[name] = timing
	}
	
	return &TimingContext{
		startTime: time.Now(),
		metric:    timing,
		mutex:     &pm.mutex,
	}
}

// EndTiming completes timing an operation
func (ctx *TimingContext) EndTiming() time.Duration {
	duration := time.Since(ctx.startTime)
	
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	
	ctx.metric.Count++
	ctx.metric.TotalTime += duration
	ctx.metric.LastUpdated = time.Now()
	
	// Update min/max
	if duration < ctx.metric.MinTime || ctx.metric.Count == 1 {
		ctx.metric.MinTime = duration
	}
	if duration > ctx.metric.MaxTime {
		ctx.metric.MaxTime = duration
	}
	
	// Calculate average
	ctx.metric.AvgTime = ctx.metric.TotalTime / time.Duration(ctx.metric.Count)
	
	return duration
}

// RecordDownloadMetrics records metrics for download operations
func (pm *PerformanceMetrics) RecordDownloadMetrics(task *models.Task, duration time.Duration, success bool) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	pm.downloadMetrics.LastUpdated = time.Now()
	
	if success {
		pm.downloadMetrics.TotalProcessed++
		pm.IncrementCounter("downloads_completed")
	} else {
		pm.downloadMetrics.TotalFailed++
		pm.IncrementCounter("downloads_failed")
	}
	
	// Update timing statistics
	if duration < pm.downloadMetrics.MinProcessTime || pm.downloadMetrics.TotalProcessed == 1 {
		pm.downloadMetrics.MinProcessTime = duration
	}
	if duration > pm.downloadMetrics.MaxProcessTime {
		pm.downloadMetrics.MaxProcessTime = duration
	}
	
	// Calculate averages
	total := pm.downloadMetrics.TotalProcessed + pm.downloadMetrics.TotalFailed
	if total > 0 {
		pm.downloadMetrics.SuccessRate = float64(pm.downloadMetrics.TotalProcessed) / float64(total) * 100
		
		// Calculate throughput (items per hour)
		hours := time.Since(pm.startTime).Hours()
		if hours > 0 {
			pm.downloadMetrics.Throughput = float64(pm.downloadMetrics.TotalProcessed) / hours
		}
	}
	
	pm.logger.WithField("task_id", task.ID).
		WithField("duration", duration).
		WithField("success", success).
		WithField("stage", "download").
		Debug("Recorded download metrics")
}

// RecordExtractionMetrics records metrics for extraction operations
func (pm *PerformanceMetrics) RecordExtractionMetrics(task *models.Task, duration time.Duration, success bool, filesExtracted int) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	pm.extractionMetrics.LastUpdated = time.Now()
	
	if success {
		pm.extractionMetrics.TotalProcessed++
		pm.IncrementCounter("extractions_completed")
	} else {
		pm.extractionMetrics.TotalFailed++
		pm.IncrementCounter("extractions_failed")
	}
	
	// Update timing statistics
	if duration < pm.extractionMetrics.MinProcessTime || pm.extractionMetrics.TotalProcessed == 1 {
		pm.extractionMetrics.MinProcessTime = duration
	}
	if duration > pm.extractionMetrics.MaxProcessTime {
		pm.extractionMetrics.MaxProcessTime = duration
	}
	
	// Calculate averages and throughput
	total := pm.extractionMetrics.TotalProcessed + pm.extractionMetrics.TotalFailed
	if total > 0 {
		pm.extractionMetrics.SuccessRate = float64(pm.extractionMetrics.TotalProcessed) / float64(total) * 100
		
		hours := time.Since(pm.startTime).Hours()
		if hours > 0 {
			pm.extractionMetrics.Throughput = float64(pm.extractionMetrics.TotalProcessed) / hours
		}
	}
	
	pm.logger.WithField("task_id", task.ID).
		WithField("duration", duration).
		WithField("success", success).
		WithField("files_extracted", filesExtracted).
		WithField("stage", "extraction").
		Debug("Recorded extraction metrics")
}

// RecordConversionMetrics records metrics for conversion operations  
func (pm *PerformanceMetrics) RecordConversionMetrics(task *models.Task, duration time.Duration, success bool, credentialsFound int) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	pm.conversionMetrics.LastUpdated = time.Now()
	
	if success {
		pm.conversionMetrics.TotalProcessed++
		pm.IncrementCounter("conversions_completed")
	} else {
		pm.conversionMetrics.TotalFailed++
		pm.IncrementCounter("conversions_failed")
	}
	
	// Update timing statistics
	if duration < pm.conversionMetrics.MinProcessTime || pm.conversionMetrics.TotalProcessed == 1 {
		pm.conversionMetrics.MinProcessTime = duration
	}
	if duration > pm.conversionMetrics.MaxProcessTime {
		pm.conversionMetrics.MaxProcessTime = duration
	}
	
	// Calculate averages and throughput
	total := pm.conversionMetrics.TotalProcessed + pm.conversionMetrics.TotalFailed
	if total > 0 {
		pm.conversionMetrics.SuccessRate = float64(pm.conversionMetrics.TotalProcessed) / float64(total) * 100
		
		hours := time.Since(pm.startTime).Hours()
		if hours > 0 {
			pm.conversionMetrics.Throughput = float64(pm.conversionMetrics.TotalProcessed) / hours
		}
	}
	
	pm.logger.WithField("task_id", task.ID).
		WithField("duration", duration).
		WithField("success", success).
		WithField("credentials_found", credentialsFound).
		WithField("stage", "conversion").
		Debug("Recorded conversion metrics")
}

// UpdateQueueMetrics updates queue-related metrics
func (pm *PerformanceMetrics) UpdateQueueMetrics(pending, downloaded, processing, completed, failed int) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	pm.queueMetrics.PendingTasks = pending
	pm.queueMetrics.DownloadedTasks = downloaded
	pm.queueMetrics.ProcessingTasks = processing
	pm.queueMetrics.CompletedTasks = completed
	pm.queueMetrics.FailedTasks = failed
	pm.queueMetrics.TotalTasks = pending + downloaded + processing + completed + failed
	pm.queueMetrics.QueueDepth = pending + downloaded + processing
	pm.queueMetrics.LastUpdated = time.Now()
}

// SetActiveJobs sets the number of active jobs for a processing stage
func (pm *PerformanceMetrics) SetActiveJobs(stage string, count int) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	switch stage {
	case "download":
		pm.downloadMetrics.ActiveJobs = count
		pm.SetGauge("active_downloads", float64(count))
	case "extraction":
		pm.extractionMetrics.ActiveJobs = count
		pm.SetGauge("active_extractions", float64(count))
	case "conversion":
		pm.conversionMetrics.ActiveJobs = count
		pm.SetGauge("active_conversions", float64(count))
	}
}

// GetProcessingMetrics returns processing metrics for all stages
func (pm *PerformanceMetrics) GetProcessingMetrics() map[string]*ProcessingMetrics {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	
	return map[string]*ProcessingMetrics{
		"download":   pm.downloadMetrics,
		"extraction": pm.extractionMetrics,
		"conversion": pm.conversionMetrics,
	}
}

// GetQueueMetrics returns current queue metrics
func (pm *PerformanceMetrics) GetQueueMetrics() *QueueMetrics {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	
	return pm.queueMetrics
}

// GetCounters returns all counter metrics
func (pm *PerformanceMetrics) GetCounters() map[string]*CounterMetric {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	
	// Return a copy to avoid race conditions
	counters := make(map[string]*CounterMetric)
	for k, v := range pm.counters {
		counters[k] = &CounterMetric{
			Name:        v.Name,
			Value:       v.Value,
			Rate:        v.Rate,
			LastUpdated: v.LastUpdated,
			LastReset:   v.LastReset,
		}
	}
	return counters
}

// GetTimings returns all timing metrics
func (pm *PerformanceMetrics) GetTimings() map[string]*TimingMetric {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	
	// Return a copy to avoid race conditions
	timings := make(map[string]*TimingMetric)
	for k, v := range pm.timings {
		timings[k] = &TimingMetric{
			Name:        v.Name,
			Count:       v.Count,
			TotalTime:   v.TotalTime,
			MinTime:     v.MinTime,
			MaxTime:     v.MaxTime,
			AvgTime:     v.AvgTime,
			LastUpdated: v.LastUpdated,
		}
	}
	return timings
}

// GetGauges returns all gauge metrics
func (pm *PerformanceMetrics) GetGauges() map[string]*GaugeMetric {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	
	// Return a copy to avoid race conditions
	gauges := make(map[string]*GaugeMetric)
	for k, v := range pm.gauges {
		gauges[k] = &GaugeMetric{
			Name:        v.Name,
			Value:       v.Value,
			LastUpdated: v.LastUpdated,
		}
	}
	return gauges
}

// ResetCounters resets all counter metrics
func (pm *PerformanceMetrics) ResetCounters() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	
	resetTime := time.Now()
	for _, counter := range pm.counters {
		counter.Value = 0
		counter.Rate = 0
		counter.LastReset = resetTime
		counter.LastUpdated = resetTime
	}
	
	pm.logger.Info("Performance counters reset")
}