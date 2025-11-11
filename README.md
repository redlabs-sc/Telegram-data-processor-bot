# Telegram Data Processor - Archive and Text Processing Bot

A high-performance Telegram bot written in Go for automated processing of archive and text files. Built with crash recovery, security auditing, graceful degradation, and comprehensive health monitoring.

## 📋 Quick Navigation

- [Features](#features)
- [System Architecture](#system-architecture)
- [Setup & Configuration](#setup--configuration)
- [Project Structure](#project-structure)
- [Core Components](#core-components)
- [Running the Bot](#running-the-bot)
- [Task Management](#task-management)
- [Monitoring & Health](#monitoring--health)
- [Development](#development)

## ✨ Features

### Core Processing
- **Multi-format Support**: ZIP, RAR, and TXT files
- **Large File Support**: Up to 4GB using Local Bot API Server integration
- **3-Stage Pipeline**: Download → Extraction → Conversion
- **Admin-only Access**: Secured with configurable admin IDs
- **Task Persistence**: SQLite database with complete audit trail

### Reliability & Recovery
- **Crash Recovery**: Automatic restoration of incomplete tasks on restart
- **Graceful Degradation**: Maintains functionality with disabled components
- **Circuit Breaker Pattern**: Prevents cascading failures
- **Retry Mechanism**: Exponential backoff with configurable retry limits
- **Dead Letter Queue**: Failed tasks stored for manual review
- **File Deduplication**: Hash-based duplicate prevention using SHA256

### Monitoring & Health
- **Health Monitoring System**: Real-time component and dependency tracking
- **System Metrics**: CPU, memory, disk, and goroutine monitoring
- **Alerting System**: Multiple alert levels (Info, Warning, Critical)
- **Security Audit Logging**: All admin actions logged with timestamps
- **Worker Pool Visibility**: Live status of download (3), extraction (1), and conversion workers

### Security
- **Security Validation**: Input sanitization and request validation
- **Audit Trail**: Complete tracking of user actions and system events
- **Enhanced Signature Validation**: Request integrity verification
- **Temporary File Management**: Secure cleanup with encryption
- **Admin-only Commands**: Authorization checks on all operations

## 🏗️ System Architecture

### Processing Pipeline

```
┌─────────────────────────────────────────┐
│           Telegram User Upload          │
└────────────────┬────────────────────────┘
                 │
                 ▼
         ┌──────────────┐
         │  Task Queue  │
         └──────┬───────┘
                │
    ┌───────────┼───────────┐
    │           │           │
    ▼           ▼           ▼
┌───────┐  ┌────────┐  ┌──────────┐
│Down-  │  │Extract │  │Convert   │
│load   │  │(1 seq) │  │(Parallel)│
│(3x)   │  │        │  │          │
└───┬───┘  └───┬────┘  └────┬─────┘
    │          │            │
    └──────────┼────────────┘
               ▼
        ┌─────────────┐
        │   Storage   │
        │ (SQLite DB) │
        └─────────────┘
```

### Worker Configuration
- **Download Workers**: 3 concurrent (respects Telegram limits)
- **Extraction Workers**: 1 sequential (single-threaded for stability)
- **Conversion Workers**: 2 concurrent
- **Worker Timeout**: 30 minutes per task
- **Queue Buffer**: 100 tasks per pool

### Error Handling Flow
```
Task Error
    ↓
[Retry Service] → Exponential backoff
    ↓
[Circuit Breaker] → Detects repeated failures
    ↓
[Dead Letter Queue] → Failed task storage
    ↓
[Admin Alert] → Notification via Telegram
```

## 🚀 Setup & Configuration

### Prerequisites
- Go 1.23.0 or later
- SQLite3
- Telegram Bot Token (from @BotFather)
- Local Bot API Server (optional, for large files)

### Environment Variables

Create `.env` file with required configuration:

```bash
# REQUIRED
TELEGRAM_BOT_TOKEN=your_token_here
ADMIN_IDS=123456789,987654321

# OPTIONAL (with defaults)
MAX_FILE_SIZE_MB=4096                          # Default: 4GB
DATABASE_PATH=data/bot.db                      # Default: data/bot.db
LOG_LEVEL=info                                 # Default: info
LOG_FILE_PATH=logs/bot.log                     # Default: logs/bot.log
USE_LOCAL_BOT_API=true                         # Default: true
LOCAL_BOT_API_URL=http://localhost:8081        # Default: localhost:8081
```

### Build & Run

```bash
# Install dependencies
go mod download
go mod tidy

# Build
go build -o telegram-bot main.go

# Run
./telegram-bot

# Or use provided scripts
./start.sh      # Start the bot
./run.sh        # Alternative startup
```

## 📁 Project Structure

```
telegram_data_processor/
│
├── main.go                          # Entry point, bot initialization
│
├── bot/                             # Telegram bot layer
│   ├── telegram.go                  # Bot API client & lifecycle
│   ├── handlers.go                  # Command handlers
│   ├── auth.go                      # Admin authorization
│   ├── notifications.go             # User messaging
│   └── ratelimit.go                 # Telegram API rate limiting
│
├── pipeline/                        # Task orchestration
│   ├── pipeline.go                  # Pipeline lifecycle (Start/Stop)
│   ├── coordinator.go               # Task coordination & routing
│   ├── queue.go                     # Task queue implementation
│   ├── priority_queue.go            # Priority-based queueing
│   └── worker_pool.go               # Generic worker pool
│
├── workers/                         # Processing workers
│   ├── download.go                  # File download from Telegram
│   │   ├── SHA256 hashing for dedup
│   │   ├── Retry logic (3 attempts)
│   │   ├── Local Bot API path detection
│   │   └── Auto-move to extraction dirs
│   │
│   ├── extraction.go                # Archive extraction
│   │   ├── Circuit breaker
│   │   ├── Single-threaded enforcement
│   │   └── Graceful degradation
│   │
│   ├── conversion.go                # File conversion
│   │   ├── Subprocess execution
│   │   ├── Timeout management (30min)
│   │   └── Dependency monitoring
│   │
│   └── interfaces.go                # Worker interfaces & Job definition
│
├── storage/                         # Data persistence
│   ├── database.go                  # SQLite setup & migrations
│   │   └── WAL mode, connection pooling
│   │
│   ├── taskstore.go                 # Task CRUD operations
│   │   ├── Create, Read, Update, Delete
│   │   ├── Query by status
│   │   └── Transaction support
│   │
│   ├── recovery.go                  # Crash recovery
│   │   ├── Incomplete task detection
│   │   ├── Orphaned file cleanup
│   │   └── Task resumption
│   │
│   ├── audit.go                     # General audit logging
│   ├── security_audit.go            # Security-specific audit
│   ├── deadletter.go                # Failed task storage
│   ├── deadletter_manager.go        # DLQ operations
│   └── backup.go                    # Database backup utilities
│
├── models/                          # Data structures
│   └── task.go                      # Task model with statuses
│       ├── PENDING → DOWNLOADED → COMPLETED/FAILED
│       ├── Error tracking (message, category, severity)
│       └── Retry counting
│
├── monitoring/                      # System health & metrics
│   ├── health.go                    # Health check system
│   │   ├── Component status tracking
│   │   ├── System resource monitoring
│   │   └── Uptime calculation
│   │
│   ├── metrics.go                   # Performance metrics
│   ├── system.go                    # CPU, memory, disk stats
│   └── alerting.go                  # Alert generation & delivery
│
├── utils/                           # Utility modules
│   ├── config.go                    # Configuration loading (.env)
│   ├── logging.go                   # Structured logging (logrus)
│   ├── errors.go                    # Error categorization
│   ├── files.go                     # File operations
│   │
│   ├── bot_api.go                   # Telegram API client wrapper
│   ├── bot_api_path.go              # Dynamic Local Bot API paths
│   │
│   ├── circuit_breaker.go           # Circuit breaker implementation
│   ├── subprocess_breaker.go        # Subprocess circuit breaker
│   ├── retry.go                     # Retry service with backoff
│   │
│   ├── security_validation.go       # Input validation & sanitization
│   ├── enhanced_signature_validator.go # Request integrity checks
│   │
│   ├── graceful_degradation.go      # Dependency monitoring & fallbacks
│   ├── rate_limiter.go              # Request rate limiting
│   ├── secure_temp_manager.go       # Temporary file management
│   └── logging.go                   # Structured logging setup
│
├── app/extraction/                  # File extraction system
│   ├── store.go                     # Extraction storage operations
│   ├── extract/
│   │   └── extract.go               # Archive extraction executable
│   ├── convert/
│   │   └── convert.go               # File conversion executable
│   └── files/
│       ├── all/                     # Archive input directory
│       ├── txt/                     # Text file directory
│       ├── done/                    # Processed files
│       ├── errors/                  # Failed extractions
│       ├── nopass/                  # Password-protected files
│       └── pass/                    # Successfully processed
│
├── data/                            # Application data
│   └── bot.db                       # SQLite database
│
├── logs/                            # Log files
│   └── bot.log                      # Application logs
│
├── cmd/                             # CLI utilities
│   └── backup/
│       └── main.go                  # Backup utility
│
└── scripts/                         # Setup & maintenance scripts
    ├── setup.sh                     # Initial setup
    ├── start-native-api.sh          # Local Bot API startup
    ├── build-production.sh          # Production build
    ├── clear-database.sh            # Database cleanup
    └── ...more utilities
```

## 🔧 Core Components

### Main Function (main.go)

**Startup Sequence:**
1. Load configuration from `.env`
2. Initialize logger with rotation
3. Open SQLite database (WAL mode)
4. Create task store
5. Initialize download worker and recovery service
6. Perform crash recovery and orphan cleanup
7. Create Telegram bot
8. Set up pipeline coordinator
9. Start health monitor with alert callbacks
10. Launch bot, coordinator, and auto-move monitoring
11. Setup graceful shutdown handlers

**Key Features:**
- Alert notifications sent to all admins
- Auto-move task runs every 15 seconds
- Health monitor tracks uptime and metrics
- Context-based cancellation for clean shutdown

### Config (utils/config.go)

**Loaded Values:**
- `TELEGRAM_BOT_TOKEN` (required)
- `ADMIN_IDS` (required, comma-separated)
- `MAX_FILE_SIZE_MB` (default: 4096)
- `DATABASE_PATH` (default: data/bot.db)
- `LOG_LEVEL` (default: info)
- `LOG_FILE_PATH` (default: logs/bot.log)
- `USE_LOCAL_BOT_API` (default: true)
- `LOCAL_BOT_API_URL` (default: http://localhost:8081)

**Methods:**
- `IsAdmin(userID)` - Authorization check
- `MaxFileSizeBytes()` - Size validation

### Task Model (models/task.go)

**Status Lifecycle:**
```
PENDING → DOWNLOADED → COMPLETED
                    ↓
                   FAILED
```

**Fields:**
- ID, UserID, ChatID, FileName, FileSize
- FileType, FileHash (SHA256), TelegramFileID
- LocalAPIPath, Status, ErrorMessage, ErrorCategory, ErrorSeverity
- RetryCount, CreatedAt, UpdatedAt, CompletedAt

### Pipeline (pipeline/pipeline.go)

**Worker Pools:**
- Download: 3 workers
- Extraction: 1 worker (sequential)
- Conversion: 2 workers
- Queue buffer: 100 per pool

**Operations:**
- `Start()` - Starts all worker pools
- `Stop()` - Graceful shutdown
- `SubmitTask()` - Enqueues task
- `Coordinator()` - Routes tasks between pools

### Download Worker (workers/download.go)

**Features:**
- Retry logic with 3 attempts
- SHA256 file hashing for deduplication
- Local Bot API path detection
- Security validation before download
- Automatic file move to extraction directories
- Timeout: 10 minutes per file

**Methods:**
- `Process(ctx, job)` - Downloads and hashes file
- `MoveDownloadedFilesToExtraction()` - Auto-move
- `GetBotAPIPathManager()` - Path access
- `Shutdown()` - Cleanup temp files

### Extraction Worker (workers/extraction.go)

**Features:**
- Single-threaded execution (enforced by mutex)
- Circuit breaker for subprocess protection
- Graceful degradation support
- Dependency monitoring (extract executable, directories)
- Timeout: 30 minutes per extraction

**Methods:**
- `Process(ctx, job)` - Extracts archives
- `StartMonitoring(ctx)` - Dependency tracking
- `GetDependencyHealth()` - Health status

### Conversion Worker (workers/conversion.go)

**Features:**
- Subprocess execution via Go runtime
- Timeout: 30 minutes per conversion
- Output file naming with task ID
- Error categorization
- Dependency monitoring

**Methods:**
- `Process(ctx, job)` - Converts files
- `StartMonitoring(ctx)` - Dependency tracking
- `GetDependencyHealth()` - Health status

### Storage Layer

#### Database (storage/database.go)
- SQLite3 with WAL mode
- Connection pooling (25 max open, 25 idle)
- Auto-migration system
- Query timeout: 5000ms

#### Task Store (storage/taskstore.go)
- Task CRUD operations
- Status queries
- Completion tracking
- Error logging

#### Recovery Service (storage/recovery.go)
- Incomplete task detection
- Orphaned file cleanup
- Automatic task resumption
- Recovery logging

### Monitoring (monitoring/health.go)

**Health Status Levels:**
- `HEALTHY` - All systems nominal
- `DEGRADED` - Some components offline
- `UNHEALTHY` - Critical failures

**Components Tracked:**
- Telegram Bot API connectivity
- Database connection
- File system access
- Worker pool status
- Dependency availability

**Metrics:**
- CPU usage percentage
- Memory usage (MB and %)
- Disk usage (bytes)
- Goroutine count
- System uptime
- Component response times

## 💾 Task Management

### Database Schema

**Tasks Table:**
```sql
id (PRIMARY KEY)
user_id, chat_id
file_name, file_size, file_type
file_hash (SHA256), telegram_file_id
local_api_path
status, error_message, error_category, error_severity
retry_count
created_at, updated_at, completed_at
```

**Audit Table:**
```sql
id (PRIMARY KEY)
user_id, action, resource
timestamp, details
```

### Task Lifecycle

1. **PENDING**: Task created, waiting for download
   - Queued in download pool
   - No file yet

2. **DOWNLOADED**: File received and verified
   - Hash computed for deduplication
   - Queued in extraction pool
   - File moved to extraction directory

3. **COMPLETED**: Processing finished
   - Status updated with completion time
   - Admin notified via Telegram
   - Task archived

4. **FAILED**: Processing failed after retries
   - Moved to dead letter queue
   - Error message, category, severity logged
   - Admin notified with error details

## 🏥 Monitoring & Health

### Health Check System

**Manual Trigger:**
- Telegram: `/health` command
- Returns component status and system metrics

**Automatic Monitoring:**
- Real-time health updates
- Alert generation on thresholds
- Admin notifications for critical issues

**Alert Types:**
- `HIGH_MEMORY` - Memory usage critical
- `HIGH_CPU` - CPU usage critical
- `DISK_SPACE` - Low disk space
- `QUEUE_BACKUP` - Task queue saturated
- `PROCESS_FAILURE` - Worker process failed
- `SYSTEM_FAILURE` - System-level issue
- `COMPONENT_DOWN` - Component unavailable
- `HIGH_LOAD_AVG` - System load critical

**Alert Levels:**
- `INFO` - Informational (ℹ️)
- `WARNING` - Needs attention (⚠️)
- `CRITICAL` - Immediate action needed (🚨)

## 🔒 Security

### Authorization
- All commands restricted to admin IDs from `.env`
- Per-command authorization checks
- Admin action audit logging

### Data Protection
- File hash verification (SHA256)
- Secure temporary file cleanup
- Encrypted temporary storage
- Request signature validation
- Input sanitization on all endpoints

### Audit Logging
- All user actions logged with timestamps
- Security events tracked separately
- Compliance-ready audit trail
- Persistent database storage

## 🛠️ Development

### Build
```bash
go build -o telegram-bot main.go
```

### Run Tests
```bash
go test ./...
go test -v ./...
go test -cover ./...
```

### Code Style
- Follow Go conventions
- Use `fmt.Errorf` for error wrapping
- Implement proper context handling
- Use structured logging with logrus
- Add unit tests for new features

### Dependencies (from go.mod)
- `github.com/go-telegram-bot-api/telegram-bot-api/v5` - Telegram API
- `github.com/sirupsen/logrus` - Structured logging
- `github.com/mattn/go-sqlite3` - SQLite driver
- `github.com/joho/godotenv` - .env file loading
- `github.com/cheggaaa/pb/v3` - Progress bars
- `github.com/go-sql-driver/mysql` - MySQL support
- And many utility libraries

## 🚨 Troubleshooting

### Bot Not Starting
1. Check `.env` file exists and has required variables
2. Verify Telegram bot token is valid
3. Check logs: `tail -f logs/bot.log`
4. Ensure database directory is writable

### Tasks Stuck in PENDING
1. Check download worker is running
2. Verify file size < MAX_FILE_SIZE_MB
3. Check Local Bot API Server is running (if enabled)
4. Review error messages in database

### High Memory Usage
1. Check task queue size
2. Reduce `QueueBufferSize` in pipeline config
3. Monitor extraction of large files
4. Health monitor alerts on high memory

### Extraction Fails
1. Verify `unzip` and `unrar` are installed
2. Check `app/extraction/files/all` directory permissions
3. Ensure `extract` executable is present
4. Review extraction worker logs

### Database Errors
1. Verify `data/bot.db` exists and is readable
2. Check disk space availability
3. Restart bot to trigger recovery
4. Check WAL files: `data/bot.db-wal`, `data/bot.db-shm`

## 📊 Performance Tuning

### Worker Pool Configuration
Edit `pipeline/pipeline.go` to adjust:
```go
DownloadWorkers:    3,  // For network bandwidth
ExtractionWorkers:  1,  // Keep at 1 for stability
ConversionWorkers:  2,  // For CPU capability
WorkerTimeout:      30 * time.Minute,
QueueBufferSize:    100,
```

### Resource Limits
- Monitor `logs/bot.log` for resource usage
- Run `/health` command for real-time metrics
- Adjust `MAX_FILE_SIZE_MB` based on storage
- Monitor goroutine count in health reports

## 🔄 Graceful Shutdown

The bot handles shutdown signals (`SIGINT`, `SIGTERM`) by:
1. Stopping the pipeline coordinator
2. Flushing pending tasks
3. Shutting down worker pools
4. Cleaning up temporary files
5. Closing database connection
6. Exiting cleanly

All incomplete tasks are saved to database and resumed on next startup.