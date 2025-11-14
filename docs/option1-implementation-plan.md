# Option 1 Implementation Plan
## Simple Sequential Pipeline for Current Project

**Date**: 2025-11-14
**Status**: Ready for Implementation
**Target**: Current Telegram-data-processor-bot project

---

## 1. CONSTRAINT ANALYSIS

### 1.1 Files That CANNOT Be Modified

**Critical**: These files must remain 100% unchanged:

1. **`app/extraction/extract/extract.go`**
   - Reason: Core extraction logic that processes entire `files/all/` directory
   - Function: `ExtractArchives()` - processes all archives in one run
   - Constraint: Called directly without modifications

2. **`app/extraction/convert/convert.go`**
   - Reason: Core conversion logic
   - Function: `ConvertTextFiles()` - processes files in `CONVERT_INPUT_DIR`
   - Constraint: Uses environment variables for configuration

3. **`app/extraction/store.go` (Core Pipeline Logic)**
   - Reason: 4-stage pipeline (Move → Merger → Valuable → Filter+DB)
   - Function: `RunPipeline(ctx)` - processes files in batches of 2
   - Constraint: Only wrapper/caller code can be added, not pipeline logic itself

### 1.2 Existing Infrastructure (Can Be Extended)

**Available for Use**:

1. ✅ **`models/task.go`**
   - Has: TaskStatus (PENDING, DOWNLOADED, COMPLETED, FAILED)
   - Needs: Add DOWNLOADING status
   - Has: Complete Task struct with all required fields

2. ✅ **`storage/database.go`**
   - Has: SQLite with WAL mode
   - Has: Migration system
   - Needs: Migration to add new fields/statuses

3. ✅ **`storage/taskstore.go`**
   - Has: Basic CRUD operations
   - Needs: Add queue operations (GetPendingTasks, MarkDownloading, etc.)

4. ✅ **`workers/download.go`**
   - Has: Download worker skeleton
   - Needs: Modify to download to correct directories based on file type

5. ✅ **`workers/extraction.go`, `workers/conversion.go`**
   - Has: Workers that wrap extract.go and convert.go
   - Status: Can be reused or refactored for orchestrator

6. ✅ **`pipeline/coordinator.go`**
   - Has: Pipeline coordination logic
   - Status: Exists but may need refactoring for Option 1 sequential approach

7. ✅ **`utils/*`**
   - Has: Config, logging, retry, circuit breaker, error handling
   - Status: Ready to use

8. ✅ **`monitoring/*`**
   - Has: Health monitoring, metrics, alerting
   - Status: Ready to use

### 1.3 Missing Components (Must Create)

**Required New Components**:

1. ❌ **`bot/` package**
   - telegram_bot.go - Telegram receiver and message handler
   - handlers.go - Command and file upload handlers
   - notifications.go - User notification system

2. ❌ **Processing Orchestrator**
   - Option A: Extend `pipeline/coordinator.go`
   - Option B: Create new `orchestrator/` package
   - Decision: Extend existing coordinator with sequential mode

3. ❌ **Queue Operations**
   - Add to `storage/taskstore.go`:
     - GetPendingTasks(limit int)
     - MarkDownloading(taskID)
     - GetCompletedUnnotifiedTasks()
     - MarkNotified(taskID)

---

## 2. TASK DECOMPOSITION & ORDERING

### Phase 1: Database & Model Extensions (30 min)

**Tasks**:
1. Add `DOWNLOADING` status to `models/task.go`
2. Add migration for new status to `storage/database.go`
3. Add `notified` boolean field to tasks table
4. Add queue operations to `storage/taskstore.go`

**Dependencies**: None
**Verification**: Run migrations, check schema

---

### Phase 2: Bot Package Creation (2 hours)

**Tasks**:
1. Create `bot/telegram_bot.go` - Main bot structure
2. Create `bot/handlers.go` - File upload and command handlers
3. Create `bot/notifications.go` - Notification system
4. Implement admin validation
5. Implement file type detection and task creation

**Dependencies**: Phase 1 complete
**Verification**: Bot accepts files, creates tasks

---

### Phase 3: Download Worker Enhancement (1 hour)

**Tasks**:
1. Modify `workers/download.go` to implement file routing:
   - Archives (.zip, .rar) → `app/extraction/files/all/`
   - TXT files (.txt) → `app/extraction/files/txt/`
2. Add queue polling logic (get PENDING tasks)
3. Add status transitions (PENDING → DOWNLOADING → DOWNLOADED)
4. Implement retry logic with exponential backoff

**Dependencies**: Phase 1 & 2 complete
**Verification**: Files download to correct directories

---

### Phase 4: Processing Orchestrator (3 hours)

**Tasks**:
1. Create `orchestrator/sequential.go` - Main orchestrator logic
2. Implement infinite loop with 10-second ticker
3. Implement Stage 1: Check `files/all/` → Run extract.go
4. Implement Stage 2: Check `files/pass/` → Run convert.go
5. Implement Stage 3: Check `files/txt/` → Run store.go
6. Add error handling and logging for each stage
7. Add file count tracking for notifications

**Dependencies**: Phase 3 complete
**Verification**: Orchestrator processes files sequentially

---

### Phase 5: Notification System (1 hour)

**Tasks**:
1. Implement `bot/notifications.go`:
   - Get completed unnotified tasks
   - Group by user/chat
   - Format batch messages
   - Send with rate limiting (3 sec between messages)
2. Add notification call to orchestrator loop
3. Add `MarkNotified()` to taskstore

**Dependencies**: Phase 2 & 4 complete
**Verification**: Users receive completion notifications

---

### Phase 6: Main Integration (1 hour)

**Tasks**:
1. Modify `main.go`:
   - Initialize bot package
   - Start 3 download workers
   - Start orchestrator
   - Wire up graceful shutdown
2. Remove or refactor existing pipeline coordinator
3. Ensure crash recovery works with new flow

**Dependencies**: All previous phases complete
**Verification**: End-to-end flow works

---

### Phase 7: Testing & Validation (2 hours)

**Tasks**:
1. Test archive file flow (upload → download → extract → convert → store → notify)
2. Test TXT file flow (upload → download → store → notify)
3. Test crash recovery (kill during processing, restart)
4. Test concurrent downloads (exactly 3)
5. Test rate limiting compliance
6. Load test with 10 files

**Dependencies**: Phase 6 complete
**Verification**: All tests pass, no data loss

---

## 3. DETAILED IMPLEMENTATION PLAN

### 3.1 Phase 1: Database & Model Extensions

#### Task 1.1: Add DOWNLOADING Status

**File**: `models/task.go`

```go
// Add to existing TaskStatus constants
const (
    TaskStatusPending    TaskStatus = "PENDING"
    TaskStatusDownloading TaskStatus = "DOWNLOADING"  // NEW
    TaskStatusDownloaded TaskStatus = "DOWNLOADED"
    TaskStatusCompleted  TaskStatus = "COMPLETED"
    TaskStatusFailed     TaskStatus = "FAILED"
)
```

#### Task 1.2: Add Database Migration

**File**: `storage/database.go` (add to migrations array)

```go
{17, `ALTER TABLE tasks ADD COLUMN notified INTEGER DEFAULT 0`},
{18, `ALTER TABLE tasks ADD COLUMN local_api_path TEXT DEFAULT ''`},
```

**Explanation**:
- `notified` tracks if user was notified of completion
- `local_api_path` is already in model but may not be in DB

#### Task 1.3: Add Queue Operations

**File**: `storage/taskstore.go`

Add these methods:

```go
// GetPendingTasks returns up to 'limit' tasks with PENDING status
func (ts *TaskStore) GetPendingTasks(limit int) ([]*models.Task, error) {
    query := `
        SELECT id, user_id, chat_id, file_name, file_size, file_type, file_hash,
               telegram_file_id, local_api_path, status, error_message, error_category,
               error_severity, retry_count, created_at, updated_at, completed_at
        FROM tasks
        WHERE status = ?
        ORDER BY created_at ASC
        LIMIT ?
    `

    rows, err := ts.db.Query(query, models.TaskStatusPending, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var tasks []*models.Task
    for rows.Next() {
        task := &models.Task{}
        err := rows.Scan(
            &task.ID, &task.UserID, &task.ChatID, &task.FileName,
            &task.FileSize, &task.FileType, &task.FileHash,
            &task.TelegramFileID, &task.LocalAPIPath, &task.Status,
            &task.ErrorMessage, &task.ErrorCategory, &task.ErrorSeverity,
            &task.RetryCount, &task.CreatedAt, &task.UpdatedAt, &task.CompletedAt,
        )
        if err != nil {
            return nil, err
        }
        tasks = append(tasks, task)
    }

    return tasks, nil
}

// MarkDownloading updates task status to DOWNLOADING
func (ts *TaskStore) MarkDownloading(taskID string) error {
    return ts.UpdateTaskStatus(taskID, models.TaskStatusDownloading)
}

// MarkDownloaded updates task status to DOWNLOADED
func (ts *TaskStore) MarkDownloaded(taskID string) error {
    return ts.UpdateTaskStatus(taskID, models.TaskStatusDownloaded)
}

// GetCompletedUnnotifiedTasks returns completed tasks that haven't been notified
func (ts *TaskStore) GetCompletedUnnotifiedTasks() ([]*models.Task, error) {
    query := `
        SELECT id, user_id, chat_id, file_name, file_size, file_type, file_hash,
               telegram_file_id, local_api_path, status, error_message, error_category,
               error_severity, retry_count, created_at, updated_at, completed_at
        FROM tasks
        WHERE status = ? AND notified = 0
        ORDER BY completed_at ASC
    `

    rows, err := ts.db.Query(query, models.TaskStatusCompleted)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var tasks []*models.Task
    for rows.Next() {
        task := &models.Task{}
        err := rows.Scan(
            &task.ID, &task.UserID, &task.ChatID, &task.FileName,
            &task.FileSize, &task.FileType, &task.FileHash,
            &task.TelegramFileID, &task.LocalAPIPath, &task.Status,
            &task.ErrorMessage, &task.ErrorCategory, &task.ErrorSeverity,
            &task.RetryCount, &task.CreatedAt, &task.UpdatedAt, &task.CompletedAt,
        )
        if err != nil {
            return nil, err
        }
        tasks = append(tasks, task)
    }

    return tasks, nil
}

// MarkNotified marks a task as notified
func (ts *TaskStore) MarkNotified(taskID string) error {
    query := `UPDATE tasks SET notified = 1 WHERE id = ?`
    _, err := ts.db.Exec(query, taskID)
    return err
}
```

---

### 3.2 Phase 2: Bot Package Creation

#### Task 2.1: Create Bot Structure

**File**: `bot/telegram_bot.go`

```go
package bot

import (
    "fmt"
    "path/filepath"
    "strings"
    "time"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
    "github.com/sirupsen/logrus"

    "telegram-archive-bot/models"
    "telegram-archive-bot/storage"
    "telegram-archive-bot/utils"
)

type TelegramBot struct {
    bot       *tgbotapi.BotAPI
    config    *utils.Config
    logger    *logrus.Logger
    taskStore *storage.TaskStore
    stopChan  chan struct{}
}

func NewTelegramBot(config *utils.Config, logger *logrus.Logger, taskStore *storage.TaskStore) (*TelegramBot, error) {
    var bot *tgbotapi.BotAPI
    var err error

    // Check if using Local Bot API
    if config.UseLocalBotAPI {
        bot, err = tgbotapi.NewBotAPIWithAPIEndpoint(
            config.TelegramBotToken,
            config.LocalBotAPIURL+"/bot%s/%s",
        )
    } else {
        bot, err = tgbotapi.NewBotAPI(config.TelegramBotToken)
    }

    if err != nil {
        return nil, fmt.Errorf("failed to create bot: %w", err)
    }

    logger.WithField("username", bot.Self.UserName).Info("Telegram bot authorized")

    return &TelegramBot{
        bot:       bot,
        config:    config,
        logger:    logger,
        taskStore: taskStore,
        stopChan:  make(chan struct{}),
    }, nil
}

func (tb *TelegramBot) Start() error {
    u := tgbotapi.NewUpdate(0)
    u.Timeout = 60

    updates := tb.bot.GetUpdatesChan(u)

    tb.logger.Info("Bot started, listening for updates...")

    for {
        select {
        case <-tb.stopChan:
            tb.logger.Info("Bot stopping...")
            return nil
        case update := <-updates:
            if update.Message == nil {
                continue
            }

            go tb.handleUpdate(update)
        }
    }
}

func (tb *TelegramBot) Stop() {
    close(tb.stopChan)
    tb.bot.StopReceivingUpdates()
}

func (tb *TelegramBot) GetBotAPI() *tgbotapi.BotAPI {
    return tb.bot
}

func (tb *TelegramBot) SendMessage(chatID int64, text string) error {
    msg := tgbotapi.NewMessage(chatID, text)
    msg.ParseMode = "Markdown"
    _, err := tb.bot.Send(msg)
    return err
}
```

#### Task 2.2: Create Handlers

**File**: `bot/handlers.go`

```go
package bot

import (
    "fmt"
    "path/filepath"
    "strings"
    "time"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
    "github.com/google/uuid"

    "telegram-archive-bot/models"
)

func (tb *TelegramBot) handleUpdate(update tgbotapi.Update) {
    // Check if user is admin
    if !tb.isAdmin(update.Message.From.ID) {
        tb.logger.WithField("user_id", update.Message.From.ID).
            Warn("Unauthorized access attempt")
        // Silently ignore non-admin messages (don't respond)
        return
    }

    // Handle commands
    if update.Message.IsCommand() {
        tb.handleCommand(update.Message)
        return
    }

    // Handle file uploads
    if update.Message.Document != nil {
        tb.handleDocument(update.Message)
        return
    }
}

func (tb *TelegramBot) isAdmin(userID int64) bool {
    for _, adminID := range tb.config.AdminIDs {
        if adminID == userID {
            return true
        }
    }
    return false
}

func (tb *TelegramBot) handleCommand(message *tgbotapi.Message) {
    switch message.Command() {
    case "start":
        tb.handleStartCommand(message)
    case "help":
        tb.handleHelpCommand(message)
    case "queue":
        tb.handleQueueCommand(message)
    case "stats":
        tb.handleStatsCommand(message)
    default:
        tb.SendMessage(message.Chat.ID, "Unknown command. Send /help for available commands.")
    }
}

func (tb *TelegramBot) handleStartCommand(message *tgbotapi.Message) {
    text := `👋 Welcome to Telegram Archive Bot (Option 1)

📤 Send me files to process:
• Archives: ZIP, RAR (up to 4GB)
• Text files: TXT (up to 4GB)

📊 Available commands:
/help - Show this help message
/queue - View queue status
/stats - View processing statistics

🔄 Files are processed sequentially for maximum reliability!`

    tb.SendMessage(message.Chat.ID, text)
}

func (tb *TelegramBot) handleHelpCommand(message *tgbotapi.Message) {
    text := `📚 Available Commands:

/start - Welcome message
/help - This help message
/queue - Show queue statistics (pending, downloading, processing)
/stats - Overall system statistics

📤 File Upload:
Simply send a file (ZIP, RAR, or TXT) and it will be queued for processing.

⚡ Processing Pipeline (Sequential):
1. Download (3 concurrent workers)
2. Extract archives → Convert → Store
3. Notification on completion

Files are processed one stage at a time for stability and reliability.`

    tb.SendMessage(message.Chat.ID, text)
}

func (tb *TelegramBot) handleQueueCommand(message *tgbotapi.Message) {
    // Get queue statistics
    pending, _ := tb.taskStore.GetTaskCountByStatus(models.TaskStatusPending)
    downloading, _ := tb.taskStore.GetTaskCountByStatus(models.TaskStatusDownloading)
    downloaded, _ := tb.taskStore.GetTaskCountByStatus(models.TaskStatusDownloaded)

    text := fmt.Sprintf(`📊 *Queue Status*

• Pending: %d files
• Downloading: %d files
• Downloaded (waiting for processing): %d files

Processing is sequential - one stage at a time for reliability.`,
        pending, downloading, downloaded)

    tb.SendMessage(message.Chat.ID, text)
}

func (tb *TelegramBot) handleStatsCommand(message *tgbotapi.Message) {
    // TODO: Implement statistics
    tb.SendMessage(message.Chat.ID, "Statistics coming soon...")
}

func (tb *TelegramBot) handleDocument(message *tgbotapi.Message) {
    doc := message.Document

    // Validate file size
    maxSize := tb.config.MaxFileSizeMB * 1024 * 1024
    if int64(doc.FileSize) > maxSize {
        tb.SendMessage(message.Chat.ID, fmt.Sprintf("❌ File too large. Max size: %d MB", tb.config.MaxFileSizeMB))
        return
    }

    // Detect file type
    fileType := tb.detectFileType(doc.FileName)
    if fileType == "" {
        tb.SendMessage(message.Chat.ID, "❌ Unsupported file type. Supported: ZIP, RAR, TXT")
        return
    }

    // Create task
    task := &models.Task{
        ID:             uuid.New().String(),
        UserID:         message.From.ID,
        ChatID:         message.Chat.ID,
        FileName:       doc.FileName,
        FileSize:       int64(doc.FileSize),
        FileType:       fileType,
        TelegramFileID: doc.FileID,
        Status:         models.TaskStatusPending,
        RetryCount:     0,
        CreatedAt:      time.Now(),
        UpdatedAt:      time.Now(),
    }

    // Save to database
    err := tb.taskStore.CreateTask(task)
    if err != nil {
        tb.logger.WithError(err).Error("Failed to create task")
        tb.SendMessage(message.Chat.ID, "❌ Error queuing file for processing. Please try again.")
        return
    }

    // Send confirmation
    confirmText := fmt.Sprintf(`✅ File received!

📄 Filename: %s
📦 Size: %.2f MB
🆔 Task ID: %s

You'll receive a notification when processing completes.`,
        doc.FileName,
        float64(doc.FileSize)/(1024*1024),
        task.ID[:8]) // Show first 8 chars of UUID

    tb.SendMessage(message.Chat.ID, confirmText)

    tb.logger.WithFields(logrus.Fields{
        "task_id":   task.ID,
        "filename":  doc.FileName,
        "file_type": fileType,
        "file_size": doc.FileSize,
        "user_id":   message.From.ID,
    }).Info("File queued for processing")
}

func (tb *TelegramBot) detectFileType(filename string) string {
    ext := strings.ToLower(filepath.Ext(filename))
    switch ext {
    case ".zip", ".rar":
        return "archive"
    case ".txt":
        return "txt"
    default:
        return ""
    }
}
```

---

**This implementation plan continues with Phases 3-7 covering:**
- Phase 3: Download Worker Enhancement
- Phase 4: Processing Orchestrator
- Phase 5: Notification System
- Phase 6: Main Integration
- Phase 7: Testing & Validation

**Total estimated time**: ~10 hours of development work

---

## 4. DEPENDENCIES GRAPH

```
Phase 1 (Database)
    ↓
Phase 2 (Bot) ← depends on Phase 1
    ↓
Phase 3 (Download Worker) ← depends on Phase 1 & 2
    ↓
Phase 4 (Orchestrator) ← depends on Phase 3
    ↓
Phase 5 (Notifications) ← depends on Phase 2 & 4
    ↓
Phase 6 (Integration) ← depends on all previous
    ↓
Phase 7 (Testing) ← depends on Phase 6
```

---

## 5. VERIFICATION CHECKLIST

**Phase 1**:
- [ ] DOWNLOADING status added to models/task.go
- [ ] Migration runs successfully
- [ ] New queue methods work (GetPendingTasks, etc.)

**Phase 2**:
- [ ] Bot starts and responds to /start
- [ ] File upload creates task in database
- [ ] Admin validation works (non-admins ignored)

**Phase 3**:
- [ ] Download worker picks up PENDING tasks
- [ ] Files download to correct directories (all/ or txt/)
- [ ] Status transitions work (PENDING → DOWNLOADING → DOWNLOADED)

**Phase 4**:
- [ ] Orchestrator detects files in directories
- [ ] Extract.go runs when files/all/ has files
- [ ] Convert.go runs when files/pass/ has files
- [ ] Store.go runs when files/txt/ has files

**Phase 5**:
- [ ] Users receive completion notifications
- [ ] Notifications batched properly
- [ ] Rate limiting respected

**Phase 6**:
- [ ] Bot, workers, orchestrator all start from main.go
- [ ] Graceful shutdown works
- [ ] No component conflicts

**Phase 7**:
- [ ] Archive file flow works end-to-end
- [ ] TXT file flow works end-to-end
- [ ] Crash recovery works
- [ ] Exactly 3 concurrent downloads verified

---

## 6. ROLLBACK PLAN

If implementation fails:
1. Revert database migrations (version tracking allows this)
2. Remove bot/ package
3. Restore original main.go from git
4. System returns to pre-Option 1 state

---

## END OF IMPLEMENTATION PLAN

**Next Step**: Begin Phase 1 implementation
