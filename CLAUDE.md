# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

A robust Telegram bot written in Go for automated processing of archive (ZIP/RAR) and text files with support for files up to 4GB. Features crash recovery, graceful degradation, health monitoring, and a 3-stage concurrent processing pipeline.

**Critical Design Principle**: This bot processes potentially large files (1-4GB) in a resource-constrained environment. All code changes must respect memory limits (20% RAM) and CPU constraints (50% cores).

## Build & Development Commands

### Build
```bash
go build -o telegram-bot main.go
```

### Run
```bash
./telegram-bot
# Or use provided scripts:
./start.sh
./run.sh
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests with coverage
go test -cover ./...

# Test specific package
go test ./workers
go test ./pipeline
```

### Dependency Management
```bash
# Download dependencies
go mod download

# Tidy up dependencies
go mod tidy

# Verify dependencies
go mod verify
```

### Local Bot API Server (for large files >20MB)
```bash
# Build Local Bot API Server binary
./scripts/build-native-api.sh

# Start Local Bot API Server
./scripts/start-native-api.sh
```

## Architecture Overview

### Three-Stage Processing Pipeline

The bot uses a **concurrent, multi-stage pipeline** with worker pools:

```
Telegram Upload → Download Queue → Extract Queue → Convert Queue → Completed
                  (3 workers)      (1 worker)     (2 workers)
```

**Critical Constraints**:
- **Download**: 3 concurrent workers (respects Telegram API rate limits)
- **Extraction**: **EXACTLY 1 worker** (enforced by mutex, never change this)
- **Conversion**: 2 concurrent workers (CPU-bound)
- Worker timeout: 30 minutes per task

**Why 1 extraction worker?** The extraction process (`app/extraction/extract/extract.go`) is single-threaded by design for stability. Running multiple extraction workers concurrently causes race conditions and file corruption. This is enforced with a mutex in `workers/extraction.go`.

### Component Architecture

**main.go**: Entry point with initialization sequence
1. Load config → 2. Init logger → 3. Open DB → 4. Create task store
5. Init download worker → 6. Perform crash recovery → 7. Create Telegram bot
8. Setup pipeline coordinator → 9. Start health monitor → 10. Launch workers
11. Setup graceful shutdown

**Pipeline Coordinator** (`pipeline/coordinator.go`):
- Central orchestrator that manages all worker pools
- Routes tasks between download → extraction → conversion stages
- Provides stats and monitoring interfaces
- Handles manual trigger commands (`/extract`, `/convert`)

**Workers** (`workers/`):
- `download.go`: Downloads files from Telegram, computes SHA256 hash, handles Local Bot API path detection
- `extraction.go`: Extracts ZIP/RAR archives using subprocess with circuit breaker protection
- `conversion.go`: Converts text files using subprocess execution
- All workers implement the `Worker` interface with `Process(ctx, job) error`

**Task Store** (`storage/taskstore.go`):
- SQLite database with WAL mode for concurrent access
- Stores task lifecycle: PENDING → DOWNLOADED → COMPLETED/FAILED
- Tracks errors with category and severity
- Audit trail for all operations

**Graceful Degradation** (`utils/graceful_degradation.go`):
- Monitors critical dependencies (extract executable, convert executable, directories)
- Implements fallback strategies when dependencies fail:
  - `FallbackQueue`: Queue operations for later when dependency recovers
  - `FallbackSkip`: Skip operation with notification
  - `FallbackAlternate`: Use alternative method
  - `FallbackManual`: Require manual intervention
- Auto-recovery when dependencies come back online

### File Flow with Local Bot API Server

**Dynamic Path Detection**: The bot automatically detects Local Bot API Server paths based on the bot token (no hardcoded paths). This is handled by `utils/bot_api_path.go`.

**File Movement Pipeline**:
1. Telegram upload → Local Bot API downloads to `{BOT_TOKEN}/documents/`
2. Download worker moves to `{BOT_TOKEN}/temp/{task_id}_{filename}`
3. Auto-move system (runs every 15s) routes files:
   - TXT files → `app/extraction/files/txt/`
   - ZIP/RAR → `app/extraction/files/all/`
4. Extraction worker processes from `app/extraction/files/all/`
5. Results go to:
   - Success: `app/extraction/files/pass/`
   - Errors: `app/extraction/files/errors/`
   - Password-protected: `app/extraction/files/nopass/`

**Critical**: Never bypass the auto-move system. All file routing MUST go through `downloadWorker.MoveDownloadedFilesToExtraction()`.

## Key Implementation Patterns

### Task Lifecycle & Status Flow

```go
// Task statuses (models/task.go)
PENDING     // Initial state, queued for download
DOWNLOADED  // File downloaded, ready for extraction
COMPLETED   // Fully processed
FAILED      // Processing failed after retries
```

**Status transitions are critical**:
- Always update task status in database when changing state
- Use `taskStore.UpdateTaskStatus()` with proper error handling
- Failed tasks go to dead letter queue for admin review

### Circuit Breaker Pattern

Used extensively to prevent cascading failures:

```go
// Extraction worker uses circuit breaker for subprocess protection
circuitBreaker := utils.NewCircuitBreaker(
    5,              // maxFailures
    30*time.Second, // timeout
)

// Always wrap risky operations
if !circuitBreaker.AllowRequest() {
    return fmt.Errorf("circuit breaker open")
}
```

### Error Categorization

All errors must be categorized for proper handling:

```go
// utils/errors.go categories
ErrCategoryNetwork     // Temporary, retry
ErrCategoryValidation  // Permanent, don't retry
ErrCategoryResource    // System resource issue
ErrCategoryPermission  // Access denied
ErrCategoryDependency  // External dependency failed
```

### Retry Logic

Implemented with exponential backoff:

```go
// utils/retry.go
retryService := utils.NewRetryService(
    3,              // maxAttempts
    2*time.Second,  // initialDelay
    2.0,            // backoffMultiplier
)
```

**When to retry**:
- Network errors (temporary)
- Resource unavailable (temporary)
- Timeout errors (temporary)

**Never retry**:
- Validation errors (permanent)
- Permission errors (permanent)
- File not found (permanent)

### Graceful Degradation Usage

When a worker depends on external executables:

```go
// Register dependencies in worker initialization
degradationManager := utils.NewGracefulDegradationManager(logger)
degradationManager.RegisterDependency(
    "extract",              // name
    "executable",           // type: executable, file, or directory
    1*time.Minute,          // check interval
    utils.FallbackQueue,    // fallback mode
)

// Start monitoring in background
degradationManager.StartMonitoring(ctx)

// Before critical operations
if !degradationManager.IsAvailable("extract") {
    return degradationManager.HandleUnavailableDependency(
        "extract",
        "extraction",
        parameters,
    )
}
```

## Configuration & Environment

Required environment variables (`.env`):

```bash
# REQUIRED
TELEGRAM_BOT_TOKEN=your_token_here
ADMIN_IDS=123456789,987654321  # Comma-separated admin user IDs

# OPTIONAL (defaults shown)
USE_LOCAL_BOT_API=true
LOCAL_BOT_API_URL=http://localhost:8081
MAX_FILE_SIZE_MB=4096
DATABASE_PATH=data/bot.db
LOG_LEVEL=info
```

**Admin-only access**: All commands and file processing are restricted to users in `ADMIN_IDS`. Always check `config.IsAdmin(userID)` before processing user requests.

## Critical Implementation Notes

### Memory Management for Large Files

**Problem**: Processing 4GB files can exhaust memory if not handled carefully.

**Solutions implemented**:
1. **Streaming**: Never load entire files into memory. Use `io.Copy()` with buffers
2. **Chunked Processing**: The extraction system uses chunked external sorting (see `app/extraction/OPTIMIZATION_GUIDE.md`)
3. **Bloom Filters**: Deduplication uses probabilistic filters instead of hash maps
4. **Resource Monitoring**: `monitoring/system.go` tracks memory usage and triggers GC

When writing new file processing code:
```go
// GOOD: Stream processing
src, _ := os.Open(srcPath)
defer src.Close()
dst, _ := os.Create(dstPath)
defer dst.Close()
io.Copy(dst, src)

// BAD: Loading entire file
data, _ := os.ReadFile(srcPath) // Can use 4GB RAM!
os.WriteFile(dstPath, data, 0644)
```

### Database Transactions

SQLite is used in WAL mode for concurrent reads, but writes must be serialized:

```go
// Always use transactions for multi-step operations
tx, err := db.Begin()
if err != nil {
    return err
}
defer tx.Rollback() // Safe to call even after commit

// Do work...
task, err := taskStore.CreateTask(tx, task)
if err != nil {
    return err // Rollback happens automatically
}

// Commit only if all operations succeed
return tx.Commit()
```

### Context Handling

All long-running operations must respect context cancellation:

```go
// Workers receive context from coordinator
func (w *Worker) Process(ctx context.Context, job Job) error {
    // Check context before expensive operations
    select {
    case <-ctx.Done():
        return ctx.Err()
    default:
        // Continue processing
    }

    // Use context in subprocess execution
    cmd := exec.CommandContext(ctx, "extract", args...)
    return cmd.Run()
}
```

**When to create new context**:
- Long-running background tasks: `context.WithTimeout()`
- Subprocess execution: `exec.CommandContext()`
- Network operations: `http.NewRequestWithContext()`

### Subprocess Execution

Extraction and conversion run as Go subprocesses (not external binaries):

```go
// Execute Go source file as subprocess
ctx, cancel := context.WithTimeout(parentCtx, 30*time.Minute)
defer cancel()

cmd := exec.CommandContext(ctx, "go", "run", "app/extraction/extract/extract.go")
output, err := cmd.CombinedOutput()
```

**Circuit breaker protection**: Always wrap subprocess calls with circuit breaker to prevent cascading failures from misbehaving extraction code.

## Common Development Tasks

### Adding a New Worker

1. Implement `workers.Worker` interface in `workers/`
2. Add worker pool to `pipeline/pipeline.go`
3. Update coordinator to route tasks to new worker
4. Register dependencies with graceful degradation manager
5. Add stats and monitoring
6. Update health checks

### Adding a New Telegram Command

1. Add command constant to bot handler
2. Implement handler function with admin check
3. Update `/commands` list
4. Add audit logging for the action
5. Document in README and PRD

### Modifying Task Schema

1. Update `models/task.go` struct
2. Add migration in `storage/database.go`
3. Update all `taskStore` methods that read/write affected fields
4. Test with existing database to ensure backward compatibility

### Debugging Pipeline Issues

1. Check logs: `tail -f logs/bot.log`
2. Database inspection: `sqlite3 data/bot.db "SELECT * FROM tasks WHERE status='FAILED'"`
3. Health check: Send `/health` command to bot
4. Graceful degradation: Check dependency status in health report
5. Worker stats: Send `/stats` command for queue sizes and worker utilization

## Testing Guidelines

### Unit Tests
- Test each worker independently with mock dependencies
- Use table-driven tests for multiple scenarios
- Mock external dependencies (filesystem, database, Telegram API)

### Integration Tests
- Test full pipeline with real SQLite database (in-memory mode)
- Verify task state transitions
- Test crash recovery by killing and restarting bot

### Load Testing
- Test with multiple large files (1-4GB) simultaneously
- Monitor memory usage stays under 20% of system RAM
- Verify CPU usage respects 50% limit
- Check that extraction worker never processes >1 file concurrently

## Troubleshooting

### Bot fails to start
- Check `.env` file exists and has required variables
- Verify bot token is valid
- Ensure `data/` and `logs/` directories exist
- Check Local Bot API Server is running if `USE_LOCAL_BOT_API=true`

### Tasks stuck in PENDING
- Check download worker is running: `/stats` command
- Verify file size < `MAX_FILE_SIZE_MB`
- Check Local Bot API Server connectivity
- Review logs for network errors

### Extraction fails repeatedly
- Circuit breaker may be open (wait 30 seconds for reset)
- Check `app/extraction/extract/extract.go` is present
- Verify Go runtime is installed (`go version`)
- Review graceful degradation status: `/health`

### High memory usage
- Check if extraction is processing very large archive
- Verify only 1 extraction worker is active (never increase this)
- Review bloom filter settings in optimization guide
- Trigger GC manually or reduce queue sizes

### Database locked errors
- Ensure WAL mode is enabled (checked in `storage/database.go`)
- Avoid long-running transactions
- Check for orphaned database connections
