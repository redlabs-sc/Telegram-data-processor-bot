# Product Requirements Document (PRD): Robust Telegram Archive and Text Processing Bot

## 1. Introduction/Overview

This document outlines the requirements for a robust Telegram bot, written in Go, designed to fully automate the ingestion, extraction, conversion, and management of archive and text files sent via Telegram. The bot is intended for a small group of administrators and aims to eliminate manual file handling, ensure reliability, and provide clear, actionable feedback to users.

## 2. Goals

- Automate the processing of ZIP, RAR, and TXT files sent to the bot by admins.
- Support large files (1GB-4GB) using Local Bot API Server integration.
- Ensure all processing is persistent, resilient, and recoverable after crashes.
- Restrict all bot interactions and commands to authorized admins only.
- Provide real-time, clear, and rate-limited feedback to admins via Telegram.
- Prevent duplicate processing of the same file (deduplication).
- Maintain persistent logs and task history for monitoring and troubleshooting.

## 3. User Stories

1. As an admin, I want to upload a ZIP file and have it automatically processed so I don't have to do it manually.
2. As an admin, I want to upload large files (1GB-4GB) without size restrictions preventing processing.
3. As an admin, I want to see the status of all in-progress and completed tasks so I can monitor the system.
4. As an admin, I want to receive a notification if my file fails to process so I can take corrective action.
5. As an admin, I want the bot to automatically handle all files (0GB-4GB) using Native Local Bot API Server transparently.

## 4. Functional Requirements

1. The system must accept ZIP, RAR, and TXT files sent by authorized admins via Telegram.
2. The system must support all files (up to 4GB) using Native Local Bot API Server:
   - **Native Local Bot API Server**: For all files 0GB-4GB (native binary)
   - **Standard Bot API**: Legacy compatibility only (20MB limit)
   - **All Downloads**: Use Local Bot API Server for all file downloads
   - **Transparent Operation**: Users get consistent 4GB file support
3. The bot must immediately log each incoming file as a task in a persistent SQLite TaskStore with a unique ID and initial status `PENDING`.
4. The bot must use goroutines and channels to implement a concurrent, multi-stage processing pipeline:
    - DownloadWorker: Downloads files and updates status to `DOWNLOADED`.
    - ExtractionWorker: Extracts archives or routes TXT files; updates status accordingly.
    - ConversionWorker: Converts extracted files as needed; updates status to `COMPLETED` or `FAILED`.
5. The bot must integrate `extract` and `convert` functionality through direct function calls:

      How to implement:
      - Extraction is triggered by `/extract` Telegram command.
      - Conversion is triggered by `/convert` Telegram command.
      - Bot calls `extract.ExtractArchives()` and `convert.ConvertTextFiles()` functions directly from workers.
      - Concurrency is controlled via a semaphore/worker pool to prevent resource contention.
      - The code of `extract.go` and `convert.go` remains unchanged; orchestration is handled in the bot.
      - Users are notified if their request is queued or delayed due to concurrency limits.
6. The bot must recover from crashes by resuming incomplete tasks on startup.
7. The bot must rate-limit all status and progress updates to Telegram to avoid API limits, primarily by editing a single status message per task.
8. The bot must restrict all commands and file processing to admins listed in the `.env` file.
9. The bot must provide the following admin commands:
    - `/stats`: Show bot uptime, processed/failed file counts, disk usage.
    - `/extract` and `/convert`: trigger extraction and conversion workflows, convert functionality uses environment variables (CONVERT_INPUT_DIR and CONVERT_OUTPUT_FILE) for the processing pipeline.
    - `/stop`: Force stop all operations and clear queues.
    - `/cleanup`: Delete temporary/error files to free up disk space. deletes all files in 'extraction/files/errors', 'extraction/files/nopass'.
    - `/exit`: Safe shutdown ensuring all active tasks are saved.
    - `/commands`: List all commands and perform a self-diagnostic check for dependencies.
    - `/health`: Display system health status including dependency monitoring and graceful degradation state.
10. The bot must deduplicate files to prevent reprocessing.
11. The bot must support a configurable maximum file size (default 4GB, configurable via MAX_FILE_SIZE_MB in .env) and reject larger files with a clear notification.
12. The bot must log all actions and errors to both Telegram (when relevant) and a persistent log file (`bot.log`).
13. Processed files and logs must be retained indefinitely unless removed by `/cleanup`.
14. The bot must provide Markdown-formatted notifications with status emojis (✅, ❌, 📥, ⏳, etc.) and use inline buttons for interactive commands (e.g., refresh, pause).
15. If a file fails to download or process, the bot must retry up to a limit, notify the admin, and mark the task as FAILED in the TaskStore.
16. The bot must handle simultaneous uploads and large queues using task queuing and worker concurrency.
17. The bot must implement graceful degradation to maintain partial functionality when dependencies fail:
    - Monitor critical dependencies (extract/convert functions, file directories, executables)
    - Implement fallback strategies: queue operations, skip with notification, use alternate methods, or require manual intervention
    - Automatically recover queued operations when dependencies are restored
    - Provide system health reporting with dependency status information
    - Log all degradation events and recovery actions for troubleshooting

## 4.1 Download and File Management Pipeline

### **Dynamic Local Bot API Integration**
- The bot automatically detects the Local Bot API Server directory based on the bot token
- Path detection is dynamic and adapts when bot tokens change (no hardcoded paths)
- The system validates Local Bot API directory structure (`documents/` and `temp/` folders)
- All file operations use the detected Local Bot API Server paths

### **Integrated Auto-Move System**
- **No External Dependencies**: File movement is handled entirely within the bot (no external shell scripts)
- **Automatic File Routing**: Files are automatically moved based on type:
  - TXT files → `app/extraction/files/txt/`
  - ZIP/RAR files → `app/extraction/files/all/`
- **Built-in Monitoring**: Auto-move runs every 15 seconds to ensure files don't get stuck
- **Original Filename Preservation**: Files maintain their original names throughout the pipeline

### **Local Bot API Server File Flow**
1. **Download**: Files are downloaded by Local Bot API Server to `{BOT_TOKEN}/documents/`
2. **Processing**: Download worker moves files to `{BOT_TOKEN}/temp/` with task ID prefixes
3. **Auto-Move**: Monitoring system automatically moves files to appropriate extraction directories
4. **Extraction**: Files are processed from `app/extraction/files/` directories

### **Parallel Processing Architecture**
- **3 Parallel Downloads**: Maximum of 3 concurrent file downloads (respects Telegram limits)
- **Concurrent Operations**: The system simultaneously handles:
  - Receiving new forwarded files and adding them to queue
  - Downloading files in parallel (up to 3)
  - Running extraction and conversion commands
  - Auto-moving completed downloads to extraction directories
- **Non-blocking Pipeline**: Download, extraction, and conversion operations don't block each other

### **Crash Recovery and Persistence**
- **Enhanced Recovery**: System checks both Local Bot API temp and documents folders on startup
- **Task Continuity**: Interrupted downloads and file moves are properly recovered
- **Local Bot API Awareness**: Recovery system understands Local Bot API directory structure
- **Automatic Cleanup**: Orphaned files in Local Bot API directories are cleaned up periodically

## 5. Non-Goals (Out of Scope)

- No support for public groups or non-admin users.
- No processing of file types other than ZIP, RAR, TXT.
- No integration with cloud storage services (e.g., Google Drive).
- No web dashboard or UI; all interactions occur via Telegram.

## 6. Design Considerations

- Telegram notifications should use Markdown formatting and include status emojis for clarity.
- Use inline buttons for interactive commands and edit messages instead of sending multiple updates.
- All sensitive data (API keys, admin IDs, etc.) must be loaded from a `.env` file.
- The bot is designed to run on a Linux server (VPS recommended).

## 7. Technical Considerations

- Persistent task management via SQLite database (to be created in `data/` directory).
- External dependencies: `extract` and `convert` functionality is integrated as library functions from `app/extraction/extract` and `app/extraction/convert` packages.
- All logs should be structured and output to both console and `bot.log`.
- The bot must handle Telegram API rate limits gracefully. Specifically, when a `FloodWait` error is received, the bot must:
  - Automatically parse the error to extract the required wait duration.
  - Pause all API requests for that duration.
  - Resume operations automatically after the wait period expires.
  - This ensures compliance with API limits without losing tasks or requiring manual intervention.
- **Graceful Degradation System**: The bot must implement comprehensive dependency monitoring with automatic fallback mechanisms:
  - Continuous health checks for extract/convert functions, file system components, and critical directories
  - Four fallback strategies: queue operations (retry later), skip operations (with notification), alternate methods (simplified processing), manual intervention
  - Automatic recovery and processing of queued operations when dependencies are restored
  - Real-time system health reporting accessible via admin commands

### 7.0.1 Large File Support Architecture

The system implements Native Local Bot API Server for all file downloads (0GB-4GB):

**Native Local Bot API Server Integration:**
- Handles all files 0GB-4GB using native Local Bot API Server binary
- Requires API_ID and API_HASH from https://my.telegram.org/apps
- Automated setup via `scripts/start-native-api.sh`
- All downloads use Local Bot API Server for consistent 4GB support

**Legacy Standard Bot API Integration:**
- Only for compatibility with 20MB limit
- Not recommended for production use
- Provides fallback for systems without Local Bot API Server

**Local Bot API Server for All Downloads:**
- All Files 0GB-4GB: Uses Native Local Bot API Server
- Files >4GB: Provides clear error message with size limit information
- No file size switching - consistent API usage for all downloads
- Enhanced reliability with native binary implementation

**Configuration Requirements:**
```
# Standard Bot API (always required)
TELEGRAM_BOT_TOKEN=your_bot_token

# Local Bot API Server (for large files)
API_ID=your_api_id
API_HASH=your_api_hash
USE_LOCAL_BOT_API=true
LOCAL_BOT_API_URL=http://localhost:8081
LOCAL_BOT_API_ENABLED=true
```

**Dynamic Path Management:**
- Bot automatically detects Local Bot API directory: `{BOT_TOKEN}/`
- No hardcoded paths - adapts to token changes automatically
- Validates directory structure: `{BOT_TOKEN}/documents/` and `{BOT_TOKEN}/temp/`
- Creates missing directories automatically during startup

**Performance Considerations:**
- Large file downloads use streaming to minimize memory usage
- Extended timeouts (30-60 minutes) for large file operations
- Docker resource limits prevent system overload
- Health monitoring ensures API server availability

## 7.1 Folder Structure Analysis for Extract.go and Convert.go

### **Required Folder Structure:**
```
{BOT_TOKEN}/            # Local Bot API Server directory (auto-detected)
├── documents/          # Local Bot API downloads (temporary)
├── temp/              # Processing directory with task ID prefixes
│   └── secure_*/      # Secure temp manager subdirectories
└── td.binlog          # Local Bot API Server state file

app/
├── extraction/        # Self-contained extraction system
│   ├── extract.go     # Archive extraction executable (UNCHANGED CODE)
│   ├── convert.go     # Credential conversion executable (UNCHANGED CODE)
│   ├── pass.txt       # Password list for extract.go
│   └── files/         # All file processing directories
│       ├── all/       # ZIP/RAR files (auto-moved from Local Bot API)
│       ├── txt/       # TXT files (auto-moved from Local Bot API)
│       ├── pass/      # Output from extract.go → Input for convert.go
│       ├── done/      # Special outputs from convert.go
│       │   └── banks.txt  # Context for found search strings
│       ├── errors/    # Quarantined problematic files
│       ├── nopass/    # Password-protected archives that failed
│       └── etbanks/   # Files with search strings but no credentials

utils/
└── bot_api_path.go    # Dynamic Local Bot API path detection
```

### **Critical Architecture Requirements:**

**1. File Location Constraints:**
- `extract.go` and `convert.go` MUST reside in `app/extraction/` directory
- `files/` folder MUST be in the same directory as the executables (`app/extraction/files/`)
- `pass.txt` MUST be in the same directory as `extract.go` (`app/extraction/pass.txt`)
- **NO CODE CHANGES** are allowed in `extract.go` or `convert.go`

**2. Bot Integration Requirements:**
- Bot automatically detects Local Bot API directory using `BotAPIPathManager`
- Download worker moves files: `{BOT_TOKEN}/documents/` → `{BOT_TOKEN}/temp/` → `app/extraction/files/`
- Auto-move system routes files by type: TXT → `txt/`, ZIP/RAR → `all/`
- Bot workers call `extract.ExtractArchives()` function directly from extraction worker
- Bot workers call `convert.ConvertTextFiles()` function directly from conversion worker
- Convert functionality uses environment variables: `CONVERT_INPUT_DIR` and `CONVERT_OUTPUT_FILE`

**3. Workflow Process:**
**Extract.go Logic:**
- **Working Directory:** `app/extraction/`
- **Input:** `files/all/` (relative to working directory)
- **Output:** `files/pass/` (relative to working directory)
- **Special:** `files/nopass/` (password-protected archives that failed)
- **Dependencies:** `pass.txt` (relative to working directory)
- **Process:** Extracts ZIP/RAR archives, looks for files matching `.*asswor.*\.txt` pattern

**Convert.go Logic:**
- **Working Directory:** `app/extraction/`
- **Input:** `files/pass/` (via `CONVERT_INPUT_DIR` environment variable)
- **Output:** User-specified file (via `CONVERT_OUTPUT_FILE` environment variable) for extracted credentials
- **Special Outputs:**
  - `files/done/banks.txt` (context for found search strings)
  - `files/errors/` (quarantined problematic files)
  - `files/etbanks/` (files with search strings but no credentials)
- **Usage:** Called via `convert.ConvertTextFiles()` function with environment variables set

### **Integration Points:**
- Local Bot API Server downloads → `{BOT_TOKEN}/documents/`
- Download worker processing → `{BOT_TOKEN}/temp/` (with task ID prefixes)
- Auto-move system → `app/extraction/files/txt/` or `app/extraction/files/all/`
- Extract function: `files/all/` → `files/pass/`
- Convert function: `files/pass/` → user-specified output file
- All extraction operations are relative to `app/extraction/` working directory
- **New Workflow**: Local Bot API download → temp processing → auto-move → extract function → convert function

### **Key Architectural Improvements:**
- **No External Scripts**: File movement is fully integrated into the bot
- **Dynamic Path Detection**: Automatically adapts to bot token changes
- **Enhanced Recovery**: Proper handling of Local Bot API directory structure
- **Parallel Processing**: 3 concurrent downloads with non-blocking pipeline
- **Auto-Move Monitoring**: Built-in 15-second file movement monitoring
- **Graceful Degradation**: Comprehensive dependency monitoring with automatic fallback strategies for system resilience

## 8. Success Metrics

- The bot processes files end-to-end without manual intervention.
- The bot recovers from crashes and resumes incomplete tasks.
- The bot runs for 7+ days without crashing.
- Admins report satisfaction and minimal need for manual intervention.

## 9. Open Questions

- Should the maximum file size be hardcoded or configurable at runtime via a command?
- What is the preferred format for exporting logs or task history (e.g., plain text, CSV)?
- Should the bot support multi-language notifications or only English?
- What resource limits should be set for Local Bot API Server Docker container?
- Should the bot support automatic Local Bot API Server health checks and restart?
