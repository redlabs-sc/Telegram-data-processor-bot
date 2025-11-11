package utils

import (
	"sync"
	"time"
)

// RateLimitConfig defines basic configuration for monitoring
type RateLimitConfig struct {
	// Cleanup interval for old entries
	CleanupInterval time.Duration
}

// DefaultRateLimitConfig returns default configuration
func DefaultRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		CleanupInterval: 5 * time.Minute,
	}
}

// UserRateState tracks usage statistics for monitoring only
type UserRateState struct {
	UserID           int64
	Username         string
	LastCommandTime  time.Time
	LastFileTime     time.Time
	CommandCount     int // Commands in current window
	FileCount        int // Files in current window
	WindowStart      time.Time
}

// RateLimiter tracks usage statistics (no actual limiting)
type RateLimiter struct {
	config      *RateLimitConfig
	logger      *Logger
	userStates  map[int64]*UserRateState
	mutex       sync.RWMutex
	stopCleanup chan struct{}
}

// NewRateLimiter creates a new usage tracker (no actual limiting)
func NewRateLimiter(config *RateLimitConfig, logger *Logger) *RateLimiter {
	if config == nil {
		config = DefaultRateLimitConfig()
	}

	rl := &RateLimiter{
		config:      config,
		logger:      logger,
		userStates:  make(map[int64]*UserRateState),
		stopCleanup: make(chan struct{}),
	}

	// Start background cleanup routine
	go rl.startCleanupRoutine()

	logger.Info("Usage tracker initialized (no rate limiting)")

	return rl
}

// AllowCommand always allows commands for authorized users (single user system)
func (rl *RateLimiter) AllowCommand(userID int64, username, command string) (bool, error) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	state := rl.getUserState(userID, username)
	now := time.Now()

	// Always allow command - no blocking, no rate limiting
	state.LastCommandTime = now
	state.CommandCount++

	rl.logger.WithField("user_id", userID).
		WithField("username", username).
		WithField("command", command).
		WithField("commands_in_window", state.CommandCount).
		Debug("Command allowed")

	return true, nil
}

// AllowFileUpload always accepts file uploads for authorized users
// All files are queued for processing regardless of frequency
func (rl *RateLimiter) AllowFileUpload(userID int64, username, fileName string, fileSize int64) (bool, error) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	state := rl.getUserState(userID, username)
	now := time.Now()

	// Always accept the file - no blocking, no rate limiting
	state.LastFileTime = now
	state.FileCount++

	rl.logger.WithField("user_id", userID).
		WithField("username", username).
		WithField("file_name", fileName).
		WithField("file_size", fileSize).
		WithField("files_in_window", state.FileCount).
		Info("File upload accepted - will be queued for processing")

	return true, nil
}

// getUserState gets or creates user state
func (rl *RateLimiter) getUserState(userID int64, username string) *UserRateState {
	state, exists := rl.userStates[userID]
	if !exists {
		now := time.Now()
		state = &UserRateState{
			UserID:          userID,
			Username:        username,
			LastCommandTime: now,
			LastFileTime:    now,
			WindowStart:     now,
		}
		rl.userStates[userID] = state

		rl.logger.WithField("user_id", userID).
			WithField("username", username).
			Debug("Created new usage tracking state for user")
	}
	return state
}

// resetCommandWindow resets command counting window if needed
func (rl *RateLimiter) resetCommandWindow(state *UserRateState, now time.Time) {
	// Reset window if a minute has passed
	if now.Sub(state.WindowStart) >= time.Minute {
		state.CommandCount = 0
		state.WindowStart = now
	}
}

// resetFileWindow resets file counting window if needed  
func (rl *RateLimiter) resetFileWindow(state *UserRateState, now time.Time) {
	// Reset window if an hour has passed
	if now.Sub(state.WindowStart) >= time.Hour {
		state.FileCount = 0
		state.WindowStart = now
	}
}


// GetUserStats returns rate limiting statistics for a user
func (rl *RateLimiter) GetUserStats(userID int64) *UserRateState {
	rl.mutex.RLock()
	defer rl.mutex.RUnlock()

	state, exists := rl.userStates[userID]
	if !exists {
		return nil
	}

	// Return a copy to avoid race conditions
	stateCopy := *state
	return &stateCopy
}

// GetAllStats returns rate limiting statistics for all users
func (rl *RateLimiter) GetAllStats() map[int64]*UserRateState {
	rl.mutex.RLock()
	defer rl.mutex.RUnlock()

	stats := make(map[int64]*UserRateState)
	for userID, state := range rl.userStates {
		stateCopy := *state
		stats[userID] = &stateCopy
	}

	return stats
}

// ResetUserLimits resets usage statistics for a specific user
func (rl *RateLimiter) ResetUserLimits(userID int64) {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	state, exists := rl.userStates[userID]
	if !exists {
		return
	}

	now := time.Now()
	state.CommandCount = 0
	state.FileCount = 0
	state.WindowStart = now

	rl.logger.WithField("user_id", userID).
		WithField("username", state.Username).
		Info("Usage statistics reset for user")
}

// IsUserBlocked always returns false (no blocking in single-user system)
func (rl *RateLimiter) IsUserBlocked(userID int64) (bool, time.Time) {
	return false, time.Time{}
}

// startCleanupRoutine runs background cleanup of old user states
func (rl *RateLimiter) startCleanupRoutine() {
	ticker := time.NewTicker(rl.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.performCleanup()
		case <-rl.stopCleanup:
			rl.logger.Info("Stopping rate limiter cleanup routine")
			return
		}
	}
}

// performCleanup removes old user states that haven't been active
func (rl *RateLimiter) performCleanup() {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()
	cleanupThreshold := 24 * time.Hour // Remove states older than 24 hours
	removedCount := 0

	for userID, state := range rl.userStates {
		// Remove if user hasn't been active for more than cleanup threshold
		if now.Sub(state.LastCommandTime) > cleanupThreshold && 
		   now.Sub(state.LastFileTime) > cleanupThreshold {
			delete(rl.userStates, userID)
			removedCount++
		}
	}

	if removedCount > 0 {
		rl.logger.WithField("removed_count", removedCount).
			WithField("active_users", len(rl.userStates)).
			Debug("Usage tracker cleanup completed")
	}
}

// Shutdown gracefully shuts down the rate limiter
func (rl *RateLimiter) Shutdown() {
	rl.logger.Info("Shutting down rate limiter")
	close(rl.stopCleanup)
}

// RateLimitStats provides overall usage statistics
type RateLimitStats struct {
	ActiveUsers     int
	ConfigLimits    *RateLimitConfig
}

// GetStats returns overall usage statistics
func (rl *RateLimiter) GetStats() *RateLimitStats {
	rl.mutex.RLock()
	defer rl.mutex.RUnlock()

	stats := &RateLimitStats{
		ActiveUsers:  len(rl.userStates),
		ConfigLimits: rl.config,
	}

	return stats
}