# Option 1: Simple Sequential Pipeline Design

## Overview

This is the simplest and most reliable architecture for the Telegram bot. It uses a single-threaded sequential pipeline that processes files one stage at a time, ensuring zero race conditions and complete preservation of the existing extract.go, convert.go, and store.go code.

## Architecture Diagram

```
┌────────────────────────────────────────────────────────┐
│            TELEGRAM (Multiple Admins)                  │
│         Continuous file uploads 24/7                   │
└────────────────────┬───────────────────────────────────┘
                     │
                     ▼
┌────────────────────────────────────────────────────────┐
│               MASTER BOT (One Instance)                │
│                                                        │
│  ┌──────────────────────────────────────────────┐    │
│  │  TELEGRAM RECEIVER                           │    │
│  │  - No restrictions on file acceptance        │    │
│  │  - Accepts ALL files immediately             │    │
│  │  - Unlimited queue capacity                  │    │
│  │  - Responds instantly to user                │    │
│  └────────────┬─────────────────────────────────┘    │
│               │                                       │
│  ┌────────────▼─────────────────────────────────┐    │
│  │  DOWNLOAD QUEUE (PostgreSQL-backed)          │    │
│  │  - Persistent queue (survives restarts)      │    │
│  │  - 10,000+ files capacity                    │    │
│  │  - Priority: First-In-First-Out              │    │
│  └────────────┬─────────────────────────────────┘    │
│               │                                       │
│  ┌────────────▼─────────────────────────────────┐    │
│  │  DOWNLOAD WORKERS (Exactly 3 concurrent)     │    │
│  │  - Worker 1: Downloads file 1                │    │
│  │  - Worker 2: Downloads file 2                │    │
│  │  - Worker 3: Downloads file 3                │    │
│  │  - Respects Telegram rate limits             │    │
│  │  - Uses Telegram Premium (4GB support)       │    │
│  └────────────┬─────────────────────────────────┘    │
│               │                                       │
│               ▼                                       │
│         File Type Router                              │
│               │                                       │
│    ┌──────────┴──────────┐                           │
│    ▼                     ▼                            │
│  Archives              TXT Files                      │
│  (.zip/.rar)           (.txt)                         │
│    │                     │                            │
│    ▼                     │                            │
│ files/all/              │                            │
│    │                     │                            │
│    │                     ▼                            │
│    │                  files/txt/                      │
│    │                     │                            │
└────┼─────────────────────┼────────────────────────────┘
     │                     │
     └─────────┬───────────┘
               ▼
┌────────────────────────────────────────────────────────┐
│      PROCESSING ORCHESTRATOR (Single Thread)           │
│                                                        │
│  Infinite Loop (runs 24/7):                           │
│                                                        │
│  ┌──────────────────────────────────────────┐        │
│  │ 1. CHECK: files/all/ has archives?       │        │
│  │    YES → Run extract.go                  │        │
│  │          Wait for completion             │        │
│  │    NO  → Skip to step 2                  │        │
│  └──────────────────────────────────────────┘        │
│                 ↓                                     │
│  ┌──────────────────────────────────────────┐        │
│  │ 2. CHECK: files/pass/ has txt files?     │        │
│  │    YES → Run convert.go                  │        │
│  │          Wait for completion             │        │
│  │    NO  → Skip to step 3                  │        │
│  └──────────────────────────────────────────┘        │
│                 ↓                                     │
│  ┌──────────────────────────────────────────┐        │
│  │ 3. CHECK: files/txt/ has txt files?      │        │
│  │    YES → Run store.go pipeline           │        │
│  │          Wait for completion             │        │
│  │    NO  → Skip to step 4                  │        │
│  └──────────────────────────────────────────┘        │
│                 ↓                                     │
│  ┌──────────────────────────────────────────┐        │
│  │ 4. Notify users of completed files       │        │
│  └──────────────────────────────────────────┘        │
│                 ↓                                     │
│  ┌──────────────────────────────────────────┐        │
│  │ 5. Sleep 10 seconds                      │        │
│  └──────────────────────────────────────────┘        │
│                 ↓                                     │
│  └──────────── REPEAT LOOP ─────────────────┘        │
│                                                        │
└────────────────────────────────────────────────────────┘
```

## Component Details

### 1. Telegram Receiver

**Purpose**: Accept all incoming files without restrictions

**Behavior**:
- Listens to Telegram updates
- Receives files from authorized admins
- **NEVER rejects files** - always accepts immediately
- Creates task record in PostgreSQL
- Adds task to download queue
- Responds to user: "✅ File received! Task ID: TASK_XXX"

**Implementation**:
```go
func (b *MasterBot) handleDocument(update tgbotapi.Update) {
    // Check admin authorization
    if !config.IsAdmin(update.Message.From.ID) {
        return // Silently ignore non-admin messages
    }

    // Create task
    task := &models.Task{
        ID:             generateTaskID(),
        UserID:         update.Message.From.ID,
        ChatID:         update.Message.Chat.ID,
        FileName:       update.Message.Document.FileName,
        FileSize:       update.Message.Document.FileSize,
        FileType:       detectFileType(update.Message.Document.FileName),
        TelegramFileID: update.Message.Document.FileID,
        Status:         models.TaskStatusPending,
        CreatedAt:      time.Now(),
    }

    // Save to database
    err := taskStore.CreateTask(task)

    // Respond immediately
    msg := fmt.Sprintf("✅ File received! Task ID: %s\nQueued for processing.", task.ID)
    bot.Send(tgbotapi.NewMessage(update.Message.Chat.ID, msg))
}
```

**Telegram Rate Limit Compliance**:
- Batches notifications (1 message per 5 files)
- Max 20 messages/minute per chat
- Uses exponential backoff on flood errors

---

### 2. Download Queue (PostgreSQL-backed)

**Purpose**: Persistent queue that survives bot restarts

**Schema**:
```sql
-- tasks table (already exists)
id, user_id, chat_id, file_name, file_size, file_type,
telegram_file_id, status, created_at, updated_at

-- status values:
-- 'PENDING' = waiting for download
-- 'DOWNLOADING' = currently downloading
-- 'DOWNLOADED' = download complete, file ready for processing
-- 'COMPLETED' = fully processed
-- 'FAILED' = processing failed
```

**Queue Operations**:
```go
// Add to queue
func (ts *TaskStore) EnqueueDownload(task *Task) error {
    task.Status = TaskStatusPending
    return ts.CreateTask(task)
}

// Get next tasks for download (LIMIT 3)
func (ts *TaskStore) GetPendingTasks(limit int) ([]*Task, error) {
    query := `
        SELECT * FROM tasks
        WHERE status = 'PENDING'
        ORDER BY created_at ASC
        LIMIT ?
    `
    return ts.QueryTasks(query, limit)
}

// Mark as downloading
func (ts *TaskStore) MarkDownloading(taskID string) error {
    return ts.UpdateTaskStatus(taskID, TaskStatusDownloading)
}

// Mark as downloaded
func (ts *TaskStore) MarkDownloaded(taskID string, localPath string) error {
    // Update status and save local path
    return ts.UpdateTask(taskID, TaskStatusDownloaded, localPath)
}
```

---

### 3. Download Workers (3 Concurrent)

**Purpose**: Download files from Telegram respecting rate limits

**Configuration**:
```go
const (
    MaxConcurrentDownloads = 3  // NEVER increase (Telegram limit)
    DownloadTimeout       = 10 * time.Minute
    MaxRetries            = 3
)
```

**Worker Logic**:
```go
func (dw *DownloadWorker) Run(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        default:
            // Get next task
            tasks, err := dw.taskStore.GetPendingTasks(1)
            if err != nil || len(tasks) == 0 {
                time.Sleep(5 * time.Second)
                continue
            }

            task := tasks[0]

            // Mark as downloading
            dw.taskStore.MarkDownloading(task.ID)

            // Download file
            err = dw.downloadFile(task)

            if err != nil {
                // Retry logic
                task.RetryCount++
                if task.RetryCount >= MaxRetries {
                    dw.taskStore.UpdateTaskStatus(task.ID, TaskStatusFailed)
                } else {
                    // Exponential backoff: 2s, 4s, 8s
                    time.Sleep(time.Duration(1<<task.RetryCount) * time.Second)
                    dw.taskStore.UpdateTaskStatus(task.ID, TaskStatusPending)
                }
                continue
            }

            // Download successful
            dw.taskStore.MarkDownloaded(task.ID, localPath)
        }
    }
}
```

**Download Process**:
```go
func (dw *DownloadWorker) downloadFile(task *Task) error {
    // Get file URL from Telegram
    fileConfig := tgbotapi.FileConfig{FileID: task.TelegramFileID}
    file, err := dw.bot.GetFile(fileConfig)
    if err != nil {
        return fmt.Errorf("get file failed: %w", err)
    }

    // Download with timeout
    ctx, cancel := context.WithTimeout(context.Background(), DownloadTimeout)
    defer cancel()

    // Determine target directory based on file type
    var targetDir string
    switch task.FileType {
    case "archive":
        targetDir = "app/extraction/files/all"
    case "txt":
        targetDir = "app/extraction/files/txt"
    }

    targetPath := filepath.Join(targetDir, task.FileName)

    // Download file
    err = dw.downloadToFile(ctx, file.Link(dw.bot.Token), targetPath)
    if err != nil {
        return fmt.Errorf("download failed: %w", err)
    }

    // Compute file hash for deduplication
    hash, err := computeSHA256(targetPath)
    if err != nil {
        return fmt.Errorf("hash computation failed: %w", err)
    }

    task.FileHash = hash
    dw.taskStore.UpdateTaskHash(task.ID, hash)

    return nil
}
```

**Telegram Premium Support**:
- Supports files up to 4GB (with Premium account)
- No chunking needed (2GB-4GB files download directly)

---

### 4. Processing Orchestrator (Single Thread)

**Purpose**: Sequential execution of extract → convert → store pipeline

**Core Loop**:
```go
func (po *ProcessingOrchestrator) Run(ctx context.Context) error {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-ticker.C:
            // Stage 1: Extract archives
            if hasFilesInDirectory("app/extraction/files/all") {
                po.logger.Info("Archives found, starting extraction...")

                // Run extract.go (blocks until complete)
                err := po.runExtraction()
                if err != nil {
                    po.logger.Errorf("Extraction failed: %v", err)
                    // Move failed files to errors directory
                    po.handleExtractionFailure(err)
                }
            }

            // Stage 2: Convert extracted files
            if hasFilesInDirectory("app/extraction/files/pass") {
                po.logger.Info("Extracted files found, starting conversion...")

                // Run convert.go (blocks until complete)
                err := po.runConversion()
                if err != nil {
                    po.logger.Errorf("Conversion failed: %v", err)
                    po.handleConversionFailure(err)
                }
            }

            // Stage 3: Store converted credentials
            if hasFilesInDirectory("app/extraction/files/txt") {
                po.logger.Info("TXT files found, starting store pipeline...")

                // Run store.go (blocks until complete)
                err := po.runStore()
                if err != nil {
                    po.logger.Errorf("Store failed: %v", err)
                    po.handleStoreFailure(err)
                }
            }

            // Stage 4: Notify users
            po.notifyCompletedTasks()
        }
    }
}
```

**Stage Execution**:

```go
// Stage 1: Extraction
func (po *ProcessingOrchestrator) runExtraction() error {
    // Call extract.go's main function directly
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
    defer cancel()

    // Run extraction (BLOCKS until all files in files/all/ are processed)
    extract.ExtractArchives()

    // Update tasks that were extracted
    po.updateExtractedTasks()

    return nil
}

// Stage 2: Conversion
func (po *ProcessingOrchestrator) runConversion() error {
    // Set environment variables for convert.go
    os.Setenv("CONVERT_INPUT_DIR", "app/extraction/files/pass")
    os.Setenv("CONVERT_OUTPUT_FILE", "app/extraction/files/txt/converted.txt")

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
    defer cancel()

    // Run conversion (BLOCKS until all files in files/pass/ are processed)
    err := convert.ConvertTextFiles()

    // Update tasks that were converted
    po.updateConvertedTasks()

    return err
}

// Stage 3: Store
func (po *ProcessingOrchestrator) runStore() error {
    // Create store service
    storeService := extraction.NewStoreService(po.logger)

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
    defer cancel()

    // Run store pipeline (BLOCKS until all files in files/txt/ are processed)
    err := storeService.RunPipeline(ctx)

    // Update tasks that were stored
    po.updateStoredTasks()

    return err
}
```

**Notification Logic**:
```go
func (po *ProcessingOrchestrator) notifyCompletedTasks() {
    // Get tasks that were completed but not yet notified
    tasks, err := po.taskStore.GetCompletedUnnotifiedTasks()
    if err != nil {
        return
    }

    // Group by user
    userTasks := groupTasksByUser(tasks)

    // Send batched notifications (respect 20 msg/min limit)
    for userID, tasks := range userTasks {
        message := formatCompletionMessage(tasks)
        po.bot.Send(tgbotapi.NewMessage(userID, message))

        // Mark tasks as notified
        for _, task := range tasks {
            po.taskStore.MarkNotified(task.ID)
        }

        // Rate limit: wait 3 seconds between messages
        time.Sleep(3 * time.Second)
    }
}
```

---

## Data Flow

### Archive File Flow

```
1. Admin uploads passwords.zip (500MB)
   ↓
2. Telegram Receiver:
   - Creates task (status: PENDING)
   - Saves to PostgreSQL
   - Responds: "✅ File received! Task ID: TASK_001"
   ↓
3. Download Worker 1:
   - Picks up TASK_001
   - Downloads from Telegram (3 minutes)
   - Saves to: app/extraction/files/all/passwords.zip
   - Updates status: DOWNLOADED
   ↓
4. Processing Orchestrator (next cycle):
   - Detects files in files/all/
   - Runs extract.go:
     * Processes passwords.zip
     * Extracts credential files
     * Saves to: app/extraction/files/pass/password_0_xxx.txt
     * Deletes passwords.zip (or moves to nopass/ if locked)
   - Takes 30 minutes
   ↓
5. Processing Orchestrator (next cycle):
   - Detects files in files/pass/
   - Runs convert.go:
     * Processes password_0_xxx.txt
     * Searches for banking credentials
     * Saves to: app/extraction/files/txt/converted.txt
     * Deletes password_0_xxx.txt
   - Takes 5 minutes
   ↓
6. Processing Orchestrator (next cycle):
   - Detects files in files/txt/
   - Runs store.go:
     * Processes converted.txt
     * Stage 1 (Valuable): Filters valuable data
     * Stage 2 (Filter): Deduplicates
     * Stage 3 (AddToDB): Stores in PostgreSQL
   - Takes 10 minutes
   ↓
7. Notification:
   - Sends to admin: "✅ TASK_001 complete! Found 47 credentials."

Total time: ~48 minutes
```

### TXT File Flow

```
1. Admin uploads credentials.txt (100MB)
   ↓
2. Telegram Receiver:
   - Creates task (status: PENDING)
   - Responds: "✅ File received! Task ID: TASK_002"
   ↓
3. Download Worker 2:
   - Downloads credentials.txt (1 minute)
   - Saves to: app/extraction/files/txt/credentials.txt
   - Updates status: DOWNLOADED
   - SKIPS files/all/ (goes directly to txt/)
   ↓
4. Processing Orchestrator (next cycle):
   - SKIPS extract (no files in files/all/)
   - SKIPS convert (no files in files/pass/)
   - Detects files in files/txt/
   - Runs store.go:
     * Processes credentials.txt
     * 3-stage pipeline
   - Takes 8 minutes
   ↓
5. Notification:
   - Sends to admin: "✅ TASK_002 complete! Found 123 credentials."

Total time: ~9 minutes
```

---

## Timeline Analysis: 100 Files (50 Archives + 50 TXT)

### Minute-by-Minute Breakdown

**Minute 0:00 - Upload Phase**
```
Admin uploads 100 files to bot
- 50 archives (avg 500MB each) = 25GB total
- 50 TXT files (avg 100MB each) = 5GB total

Bot receives all 100 files in ~1 minute:
- Creates 100 task records in PostgreSQL
- All tasks status: PENDING
- Responds 100 times: "✅ File received!"

Download queue: 100 files
```

**Minute 0:01 - Download Begins**
```
3 Download Workers start:
- Worker 1: Downloads TASK_001 (archive, 500MB)
- Worker 2: Downloads TASK_002 (archive, 500MB)
- Worker 3: Downloads TASK_003 (txt, 100MB)

Download queue: 97 files waiting
Active downloads: 3 files
```

**Minute 0:04 - First Downloads Complete**
```
Worker 3: TASK_003 complete (txt file, 100MB)
- Downloaded to: files/txt/file_003.txt
- Status: DOWNLOADED
- Starts TASK_004 (txt, 100MB)

Workers 1 & 2: Still downloading (500MB takes ~3 min each)

Download queue: 96 files waiting
```

**Minute 0:05 - Archive Downloads Complete**
```
Worker 1: TASK_001 complete (archive)
- Downloaded to: files/all/archive_001.zip
- Starts TASK_005 (archive)

Worker 2: TASK_002 complete (archive)
- Downloaded to: files/all/archive_002.zip
- Starts TASK_006 (archive)

files/all/: 2 archives
files/txt/: 1 txt file

Download queue: 95 files waiting
```

**Minute 0:10 - Processing Orchestrator First Check**
```
Orchestrator detects:
- files/all/ has 2 archives
- files/txt/ has 3 txt files (more downloaded meanwhile)

Action: Runs extract.go
- Processes archive_001.zip and archive_002.zip
- Extraction time: 30 minutes per archive
- Total: 60 minutes for 2 archives

Orchestrator BLOCKS (waits for extraction)

Meanwhile:
- Download workers continue downloading
- More files accumulate in files/all/ and files/txt/
```

**Minute 1:10 - Extraction Complete**
```
extract.go finishes:
- Processed 2 archives
- Extracted files now in files/pass/
- archives deleted from files/all/

Orchestrator detects:
- files/all/ has 10 more archives (downloaded during extraction)
- files/pass/ has extracted txt files

Action: Runs extract.go AGAIN
- Processes 10 archives
- Takes another 300 minutes (5 hours!)

This is the BOTTLENECK of Option 1
```

**Hour 2:17 - All Extractions Complete**
```
All 50 archives extracted over multiple cycles:
- Cycle 1: 2 archives (60 min)
- Cycle 2: 10 archives (300 min) - overlapped with downloads
- Final: All archives processed

files/pass/: 50 extracted txt files
files/txt/: 50 original txt files = 100 total

Orchestrator detects files/pass/ has files
Action: Runs convert.go
- Processes all 50 extracted txt files
- Takes 20 minutes
```

**Hour 2:37 - Conversion Complete**
```
convert.go finishes:
- 50 files converted
- Output in files/txt/converted.txt
- files/pass/ now empty

Orchestrator detects files/txt/ has files
Action: Runs store.go
- Processes all 100 txt files (50 original + 50 converted)
- Takes 40 minutes
```

**Hour 3:17 - Store Complete**
```
store.go finishes:
- 100 files processed through 3-stage pipeline
- All credentials stored in database

Orchestrator: Notifies all users
- Sends 100 completion notifications (batched)
- Takes 5 minutes (rate limited to 20 msg/min)
```

**Hour 3:22 - FULLY COMPLETE**
```
All 100 files processed:
✅ Downloaded
✅ Extracted
✅ Converted
✅ Stored
✅ Users notified

Total time: 3 hours 22 minutes
User wait time: 3 hours 22 minutes (same as total)
```

### Timeline Visualization

```
Time     | Downloads | Extract    | Convert | Store  | Notify | User View
---------|-----------|------------|---------|--------|--------|------------------
0:00     | Start     | Idle       | Idle    | Idle   | Idle   | Uploads 100 files
0:01-0:50| 3 workers | Idle       | Idle    | Idle   | Idle   | "Files received"
0:10     | Active    | Starts     | Idle    | Idle   | Idle   | Waiting...
1:10     | Active    | Running    | Idle    | Idle   | Idle   | Waiting...
2:17     | Complete  | Complete   | Starts  | Idle   | Idle   | Waiting...
2:37     | Complete  | Complete   | Done    | Starts | Idle   | Waiting...
3:17     | Complete  | Complete   | Done    | Done   | Starts | Waiting...
3:22     | Complete  | Complete   | Done    | Done   | Done   | ✅ All complete!
```

---

## Performance Characteristics

### Throughput

**Single File Processing Time**:
- Small TXT (10MB): 9 minutes
- Medium archive (500MB): 48 minutes
- Large archive (2GB): 90 minutes
- Huge TXT (4GB): 60 minutes

**Batch Processing (100 files)**:
- Mixed (50 archives + 50 txt): 3.3 hours
- All archives (100 files): 6+ hours
- All txt (100 files): 1.5 hours

**Daily Capacity**:
- Archives only: ~200 files/day
- TXT only: ~1000 files/day
- Mixed: ~500 files/day

### Bottlenecks

**Primary Bottleneck**: Extract stage
- Single-threaded processing
- 30-60 minutes per archive
- Sequential processing (can't parallelize)

**Secondary Bottleneck**: Store stage
- Batch processing in store.go
- Can take 60+ minutes for large batches
- But doesn't block user experience (can run async)

### Scalability Limits

**Maximum Concurrent Operations**:
- Downloads: 3 (Telegram limit)
- Extraction: 1 (sequential, can't parallelize)
- Conversion: 1 (sequential, can't parallelize)
- Store: 1 (sequential, can't parallelize)

**Memory Usage**:
- Download workers: 3 × 500MB = 1.5GB
- Extract: 2GB peak (for 4GB archives)
- Convert: 500MB peak
- Store: 1GB peak
- Total: ~5GB peak

**Disk Usage**:
- Downloaded files: Up to 50GB (100 files × 500MB avg)
- Extracted files: Up to 25GB
- Total working: ~75GB needed

---

## Reliability & Fault Tolerance

### Crash Recovery

**Scenario: Bot Crashes Mid-Processing**

1. **During Download**:
   ```
   Task status: DOWNLOADING
   Recovery:
   - On restart, find tasks with status DOWNLOADING
   - Reset status to PENDING
   - Download workers pick up again
   - No data loss
   ```

2. **During Extract**:
   ```
   Files in: files/all/
   Recovery:
   - On restart, orchestrator detects files in files/all/
   - Runs extract.go again
   - Processes all files (including partial)
   - No data loss (files still on disk)
   ```

3. **During Convert**:
   ```
   Files in: files/pass/
   Recovery:
   - On restart, orchestrator detects files in files/pass/
   - Runs convert.go again
   - No data loss
   ```

4. **During Store**:
   ```
   Files in: files/txt/
   Recovery:
   - On restart, orchestrator detects files in files/txt/
   - Runs store.go again
   - Store.go's batch processing resumes from checkpoint
   - No data loss
   ```

### Error Handling

**Download Failures**:
```go
- Network error → Retry up to 3 times with exponential backoff
- Telegram rate limit → Wait specified time, then retry
- File too large → Mark as FAILED, notify admin
- Timeout → Retry with increased timeout
```

**Extract Failures**:
```go
- Corrupted archive → Move to files/errors/, mark FAILED
- Password-protected → Move to files/nopass/, mark NEEDS_PASSWORD
- Extraction timeout → Log error, continue with next file
```

**Convert Failures**:
```go
- Encoding error → Move to files/errors/, log error
- No credentials found → Delete file (not an error)
- File read error → Retry once, then move to errors/
```

**Store Failures**:
```go
- Database connection lost → Retry with backoff
- Disk full → Alert admin, pause processing
- Deduplication error → Log warning, continue
```

### Data Integrity

**Hash-Based Deduplication**:
```go
func checkDuplicate(fileHash string) bool {
    // Check if file hash already exists in database
    exists, err := taskStore.FileHashExists(fileHash)
    if err != nil {
        logger.Errorf("Duplicate check failed: %v", err)
        return false
    }

    if exists {
        logger.Infof("File already processed (hash: %s), skipping", fileHash)
        return true
    }

    return false
}
```

**File Validation**:
```go
// After download, verify file integrity
func validateDownload(filePath string, expectedSize int64) error {
    info, err := os.Stat(filePath)
    if err != nil {
        return fmt.Errorf("file not found: %w", err)
    }

    if info.Size() != expectedSize {
        return fmt.Errorf("size mismatch: expected %d, got %d",
            expectedSize, info.Size())
    }

    // Verify file is readable
    f, err := os.Open(filePath)
    if err != nil {
        return fmt.Errorf("file not readable: %w", err)
    }
    f.Close()

    return nil
}
```

---

## Telegram API Compliance

### Rate Limits

**Message Sending**:
- Limit: 20 messages/minute per chat
- Implementation: Batch notifications (1 msg per 5 files)
- Result: 4 messages/minute max

**File Downloads**:
- Limit: ~30 API calls/second (global)
- Implementation: 3 concurrent downloads
- Usage: ~0.5 calls/second
- Safety margin: 60× under limit

**Flood Protection**:
```go
func handleFloodError(err error) {
    if strings.Contains(err.Error(), "Too Many Requests") {
        // Parse retry_after from error
        retryAfter := parseRetryAfter(err)

        logger.Warnf("Flood limit hit, waiting %d seconds", retryAfter)
        time.Sleep(time.Duration(retryAfter) * time.Second)

        // Retry operation
    }
}
```

### Premium Features Used

**4GB File Support**:
```go
// With Telegram Premium account
const MaxFileSize = 4 * 1024 * 1024 * 1024 // 4GB

func validateFileSize(size int64) error {
    if size > MaxFileSize {
        return fmt.Errorf("file too large: %d bytes (max: %d)",
            size, MaxFileSize)
    }
    return nil
}
```

**Faster Downloads**:
- Premium removes download throttling
- Download speed: Network bandwidth limited only
- 500MB file: ~3 minutes on 100Mbps connection

---

## Code Preservation

### Extract.go - Zero Changes

**Existing Code**:
```go
func ExtractArchives() {
    inputDirectory := "app/extraction/files/all"
    outputDirectory := "app/extraction/files/pass"

    processArchivesInDir(inputDirectory, outputDirectory)
}
```

**How We Call It**:
```go
// Orchestrator calls extract.go directly
func (po *ProcessingOrchestrator) runExtraction() error {
    // No changes to extract.go needed!
    extract.ExtractArchives()
    return nil
}
```

**Result**: extract.go preserved **100% exactly as-is**

### Convert.go - Zero Changes

**Existing Code**:
```go
func ConvertTextFiles() error {
    inputPath := os.Getenv("CONVERT_INPUT_DIR")
    outputFile := os.Getenv("CONVERT_OUTPUT_FILE")

    // Process all files in inputPath
    // ...
}
```

**How We Call It**:
```go
func (po *ProcessingOrchestrator) runConversion() error {
    // Set environment variables
    os.Setenv("CONVERT_INPUT_DIR", "app/extraction/files/pass")
    os.Setenv("CONVERT_OUTPUT_FILE", "app/extraction/files/txt/converted.txt")

    // No changes to convert.go needed!
    return convert.ConvertTextFiles()
}
```

**Result**: convert.go preserved **100% exactly as-is**

### Store.go - Zero Changes

**Existing Code**:
```go
func (s *StoreService) RunPipeline(ctx context.Context) error {
    // Processes all files in InputDir
    // 4-stage pipeline: move → merger → valuable → filter+db
    // ...
}
```

**How We Call It**:
```go
func (po *ProcessingOrchestrator) runStore() error {
    storeService := extraction.NewStoreService(logger)

    // No changes to store.go needed!
    return storeService.RunPipeline(context.Background())
}
```

**Result**: store.go preserved **100% exactly as-is**

---

## Advantages

### Simplicity

✅ **Single Execution Path**
- No complex coordination
- Easy to understand and debug
- Predictable behavior

✅ **Zero Race Conditions**
- Only one instance of each stage runs at a time
- No file access conflicts
- No concurrent write issues

✅ **Code Preservation**
- extract.go: unchanged
- convert.go: unchanged
- store.go: unchanged
- Zero modifications to processing logic

### Reliability

✅ **Guaranteed Correctness**
- Sequential processing ensures no data corruption
- Each stage completes before next begins
- Easy to verify results

✅ **Simple Recovery**
- Files on disk = processing state
- Restart picks up where left off
- No complex state management

✅ **Easy Debugging**
- Single execution flow
- Clear logs
- Deterministic behavior

### Maintainability

✅ **Easy to Modify**
- Change one stage without affecting others
- Add new stages easily
- Clear separation of concerns

✅ **Low Complexity**
- No distributed systems complexity
- No synchronization primitives needed
- Minimal moving parts

---

## Disadvantages

### Performance

❌ **Slow for Large Batches**
- 100 files: 3+ hours
- 1000 files: 30+ hours (1.25 days)
- Not suitable for high-volume scenarios

❌ **Sequential Bottleneck**
- Extract must complete before convert
- Convert must complete before store
- No parallelism

❌ **Extract Stage Dominates**
- 90% of processing time in extract
- Single-threaded by design (can't change)
- Limits overall throughput

### Scalability

❌ **Fixed Throughput**
- ~200 archives/day maximum
- Can't scale horizontally
- Adding more servers doesn't help

❌ **No Load Distribution**
- All processing on one instance
- Can't leverage multiple machines
- Resource utilization limited

---

## When to Use Option 1

### Ideal Scenarios

✅ **Low to Medium Volume**
- <200 files/day
- Occasional batch uploads
- Not time-critical

✅ **Reliability Critical**
- Zero data loss required
- Correctness over speed
- Simple recovery needed

✅ **Development/Testing**
- Easy to set up
- Simple to debug
- Quick to deploy

✅ **Limited Resources**
- Single server
- Low budget
- Minimal infrastructure

### NOT Suitable For

❌ **High Volume**
- >500 files/day
- Continuous uploads
- Time-sensitive processing

❌ **Large Scale**
- 1000+ files in queue
- Multiple admins uploading simultaneously
- Need for horizontal scaling

---

## Implementation Checklist

### Prerequisites

- [ ] PostgreSQL database set up
- [ ] Telegram bot token (Premium account)
- [ ] Server with 8GB RAM, 100GB disk
- [ ] Go 1.23+ installed

### Setup Steps

1. **Database Setup**
   ```bash
   # Create PostgreSQL database
   createdb telegram_bot

   # Run migrations
   go run cmd/migrate/main.go
   ```

2. **Configuration**
   ```bash
   # Create .env file
   cp .env.example .env

   # Edit .env
   TELEGRAM_BOT_TOKEN=your_premium_bot_token
   ADMIN_IDS=123456789,987654321
   DATABASE_URL=postgresql://user:pass@localhost/telegram_bot
   ```

3. **Build**
   ```bash
   go build -o telegram-bot main.go
   ```

4. **Run**
   ```bash
   ./telegram-bot
   ```

### Testing

1. **Upload Test Files**
   - 1 small txt (10MB)
   - 1 medium archive (500MB)
   - Verify processing completes

2. **Crash Recovery Test**
   - Upload files
   - Kill bot mid-processing
   - Restart bot
   - Verify processing resumes

3. **Load Test**
   - Upload 10 files
   - Monitor processing time
   - Verify all complete

---

## Monitoring

### Key Metrics

**Queue Depth**:
```sql
SELECT status, COUNT(*)
FROM tasks
GROUP BY status;
```

**Processing Times**:
```sql
SELECT
    AVG(EXTRACT(EPOCH FROM (completed_at - created_at))) as avg_time_seconds
FROM tasks
WHERE status = 'COMPLETED';
```

**Error Rate**:
```sql
SELECT
    (COUNT(*) FILTER (WHERE status = 'FAILED')::float / COUNT(*)) * 100 as error_rate_percent
FROM tasks;
```

### Health Checks

```go
func (po *ProcessingOrchestrator) HealthCheck() map[string]interface{} {
    return map[string]interface{}{
        "status": "healthy",
        "queue_depth": po.taskStore.CountPending(),
        "active_downloads": 3,
        "extract_running": po.extractRunning,
        "convert_running": po.convertRunning,
        "store_running": po.storeRunning,
        "last_processed": po.lastProcessedTime,
    }
}
```

---

## Conclusion

Option 1 provides a **simple, reliable, and maintainable** architecture that:

- ✅ Accepts unlimited files without restrictions
- ✅ Preserves existing code 100%
- ✅ Guarantees zero data loss
- ✅ Complies with Telegram rate limits
- ✅ Easy to understand and debug

**Trade-off**: Slower processing (3+ hours for 100 files)

**Best for**: Low-medium volume scenarios where reliability > speed

**Not suitable for**: High-volume, time-critical processing requirements
