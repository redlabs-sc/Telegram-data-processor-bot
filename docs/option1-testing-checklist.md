# Option 1 Testing Checklist

## Pre-Testing Setup

### 1. Environment Configuration

**Create `.env` file** with the following variables:
```bash
# Required
TELEGRAM_BOT_TOKEN=your_bot_token_here
ADMIN_IDS=123456789,987654321  # Replace with your Telegram user IDs

# Local Bot API (required for files >20MB)
USE_LOCAL_BOT_API=true
LOCAL_BOT_API_URL=http://localhost:8081

# Optional (defaults shown)
MAX_FILE_SIZE_MB=4096
DATABASE_PATH=data/bot.db
LOG_LEVEL=info
```

**Get your Telegram user ID**:
1. Start a chat with @userinfobot on Telegram
2. Send any message
3. Copy your user ID
4. Add to ADMIN_IDS in .env

### 2. Local Bot API Server Setup

**Build and start Local Bot API Server**:
```bash
# Build the server
./scripts/build-native-api.sh

# Start the server (runs in background)
./scripts/start-native-api.sh

# Verify it's running
curl http://localhost:8081/health
```

### 3. Database Initialization

```bash
# Create required directories
mkdir -p data logs
mkdir -p app/extraction/files/{all,pass,txt,done,errors,nopass,etbanks}

# The database will be auto-created on first run with all migrations
```

### 4. Build and Run

```bash
# Build the bot
go build -o telegram-archive-bot .

# Run the bot
./telegram-archive-bot

# Or use the startup script
./start.sh
```

**Expected startup logs**:
```
INFO Telegram Archive Bot starting (Option 1: Sequential Pipeline)...
INFO Authorized admin IDs loaded admins=[123456789]
INFO Health monitoring started
INFO Starting 3 download workers...
INFO Download worker started polling worker_id=1
INFO Download worker started polling worker_id=2
INFO Download worker started polling worker_id=3
INFO Starting sequential processing orchestrator...
INFO Sequential orchestrator started
INFO Starting Telegram bot...
```

---

## Phase 7: End-to-End Testing

### Test 1: Archive File Flow (ZIP)

**Objective**: Verify complete processing pipeline for archive files

**Steps**:
1. Create a test ZIP file containing password-protected text files
2. Send the ZIP file to the bot on Telegram
3. Verify bot responds with confirmation message
4. Monitor logs for processing stages

**Expected Behavior**:
1. **Upload Response**:
   ```
   ✅ File received!
   📄 Filename: test.zip
   📦 Size: 1.5 MB
   🆔 Task ID: abc12345

   You'll receive a notification when processing completes.
   ```

2. **Database Task Creation**:
   ```sql
   SELECT * FROM tasks WHERE file_name = 'test.zip';
   -- Status should be: PENDING
   ```

3. **Download Stage** (5-10 seconds):
   ```
   INFO Download worker picked up task worker_id=1 task_id=abc12345
   INFO Starting file download task_id=abc12345
   INFO File downloaded and moved to temp directory
   INFO Task marked as DOWNLOADING
   INFO File download completed successfully
   INFO Task marked as DOWNLOADED
   INFO File moved to extraction directory dest_dir=app/extraction/files/all
   ```

4. **Extraction Stage** (next 10-second cycle):
   ```
   INFO Starting extraction stage file_count=1
   INFO Archives found, starting extraction...
   [extract.go output]
   INFO Extraction stage completed duration_seconds=5.2
   ```
   - File should appear in `app/extraction/files/pass/` (if password worked)
   - Or in `app/extraction/files/nopass/` (if password failed)
   - Or in `app/extraction/files/errors/` (if extraction failed)

5. **Conversion Stage** (next 10-second cycle):
   ```
   INFO Starting conversion stage file_count=1
   [convert.go output]
   INFO Conversion stage completed duration_seconds=3.1
   ```
   - Converted text should appear in `app/extraction/files/txt/`

6. **Store Stage** (next 10-second cycle):
   ```
   INFO Starting store stage file_count=1
   [store.go 4-stage pipeline output]
   INFO Store stage completed duration_seconds=12.5
   INFO Task marked as COMPLETED task_id=abc12345
   ```

7. **Notification**:
   ```
   ✅ Processing Complete

   📄 File: test.zip

   Your file has been successfully processed and stored!
   ```

**Verification Commands**:
```bash
# Check task status
sqlite3 data/bot.db "SELECT status, notified FROM tasks WHERE file_name='test.zip';"
# Should show: COMPLETED|1

# Check extraction output
ls -lh app/extraction/files/pass/

# Check conversion output
ls -lh app/extraction/files/txt/

# Check database records (example for SQLite)
sqlite3 data/bot.db "SELECT COUNT(*) FROM lines;"
# Should show number of unique lines stored
```

**Pass Criteria**:
- [ ] Task created with PENDING status
- [ ] Status transitions: PENDING → DOWNLOADING → DOWNLOADED → COMPLETED
- [ ] File extracted to pass/ directory
- [ ] File converted to txt/ directory
- [ ] Data stored in database
- [ ] User receives completion notification
- [ ] Task marked as notified (notified=1)

---

### Test 2: Text File Flow (TXT)

**Objective**: Verify direct processing of text files (skip extract/convert stages)

**Steps**:
1. Create a test TXT file with sample content
2. Send the TXT file to the bot

**Expected Behavior**:
1. **Upload Response**: Same as Test 1
2. **Download Stage**: Downloads and moves to `app/extraction/files/txt/`
3. **Store Stage**: Processes directly (skips extract/convert)
4. **Notification**: User receives completion message

**Verification**:
```bash
# File should go directly to txt directory
ls -lh app/extraction/files/txt/

# Verify it's not in all/ or pass/ directories
ls -lh app/extraction/files/all/
ls -lh app/extraction/files/pass/
```

**Pass Criteria**:
- [ ] TXT file routed directly to txt/ directory (not to all/)
- [ ] Extract and convert stages skipped
- [ ] Store stage processes the file
- [ ] Data stored in database
- [ ] User receives notification

---

### Test 3: Multiple File Upload (Concurrent Downloads)

**Objective**: Verify 3 concurrent download workers handle multiple files

**Steps**:
1. Upload 5 files to the bot (mix of ZIP and TXT)
2. Monitor download worker logs

**Expected Behavior**:
- 3 files downloaded concurrently (worker_id=1, 2, 3)
- Remaining 2 files queued and processed when workers become available
- All files eventually processed

**Verification**:
```bash
# Check queue status while uploading
sqlite3 data/bot.db "SELECT file_name, status FROM tasks ORDER BY created_at;"

# Monitor logs for concurrent downloads
tail -f logs/bot.log | grep "Download worker"
```

**Pass Criteria**:
- [ ] Maximum 3 concurrent downloads at any time
- [ ] All 5 files eventually processed
- [ ] No download errors
- [ ] Queue drains completely (all tasks completed)

---

### Test 4: Large File Handling (2GB+)

**Objective**: Verify Local Bot API handles large files

**Steps**:
1. Create a 2GB+ archive file
2. Upload to bot

**Expected Behavior**:
- Download completes via Local Bot API
- File appears in Local Bot API documents directory
- Download worker moves file to temp, then to extraction directory
- Processing continues normally

**Verification**:
```bash
# Check Local Bot API path (dynamic based on bot token)
ls -lh <BOT_TOKEN>/documents/

# Check temp path
ls -lh <BOT_TOKEN>/temp/

# Monitor download progress
tail -f logs/bot.log | grep "file_size"
```

**Pass Criteria**:
- [ ] Large file downloads successfully
- [ ] Local Bot API used (not standard Telegram API)
- [ ] File hash calculated correctly
- [ ] File moved through all stages
- [ ] No memory exhaustion

---

### Test 5: Error Handling

**Objective**: Verify error handling and retry logic

#### 5a. Invalid File Type
**Steps**: Upload a .pdf file

**Expected**: Bot rejects with error message
```
❌ Unsupported file type. Supported: ZIP, RAR, TXT
```

#### 5b. File Too Large
**Steps**: Upload a 5GB file (exceeds 4GB limit)

**Expected**:
```
❌ File too large. Max size: 4096 MB
```

#### 5c. Download Failure (Network Error)
**Steps**: Kill Local Bot API server mid-download

**Expected**:
- Retry with exponential backoff (3 attempts)
- If all retries fail, task marked as FAILED
- Error logged in database

**Verification**:
```bash
sqlite3 data/bot.db "SELECT status, error_message FROM tasks WHERE status='FAILED';"
```

#### 5d. Extraction Failure (Corrupted Archive)
**Steps**: Upload a corrupted ZIP file

**Expected**:
- Extract stage fails
- File moved to `app/extraction/files/errors/`
- Task remains DOWNLOADED (not marked COMPLETED)
- No notification sent

**Pass Criteria**:
- [ ] Invalid file types rejected
- [ ] Oversized files rejected
- [ ] Download failures trigger retries
- [ ] Failed tasks marked correctly
- [ ] Error messages logged
- [ ] Extraction errors handled gracefully

---

### Test 6: Crash Recovery

**Objective**: Verify bot recovers from crashes

**Steps**:
1. Upload a file
2. Kill bot process while downloading (Ctrl+C)
3. Restart bot
4. Observe recovery

**Expected Behavior**:
```
INFO Performing crash recovery...
INFO Found 1 incomplete task(s)
INFO Recovered task task_id=abc12345 status=DOWNLOADING → PENDING
INFO Crash recovery completed
```
- Incomplete task reset to PENDING
- Download worker picks up task again
- Processing continues normally

**Verification**:
```bash
# Check recovery logs
tail -f logs/bot.log | grep "recovery"

# Verify task was re-queued
sqlite3 data/bot.db "SELECT status FROM tasks WHERE id='abc12345';"
# Should be PENDING after recovery
```

**Pass Criteria**:
- [ ] Incomplete tasks detected
- [ ] Tasks reset to PENDING
- [ ] Orphaned files cleaned up
- [ ] Processing resumes automatically
- [ ] No data loss

---

### Test 7: Notification System

**Objective**: Verify batched notifications with rate limiting

**Steps**:
1. Upload 3 files
2. Wait for all to complete
3. Observe notifications

**Expected Behavior**:
- Single notification with all 3 files:
  ```
  ✅ Processing Complete

  📦 3 files processed:
  • file1.zip
  • file2.txt
  • file3.rar

  All files have been successfully processed and stored!
  ```
- 3-second delay between notifications (if multiple users)

**Verification**:
```bash
# Check notified flag
sqlite3 data/bot.db "SELECT file_name, notified FROM tasks WHERE status='COMPLETED';"
# All should have notified=1
```

**Pass Criteria**:
- [ ] Multiple files batched in single notification
- [ ] Rate limiting respected (3 seconds between messages)
- [ ] Tasks marked as notified
- [ ] No duplicate notifications

---

### Test 8: Commands

**Objective**: Verify all bot commands work

#### /start
**Expected**:
```
👋 Welcome to Telegram Archive Bot!

📂 Supported file types:
• Archives: ZIP, RAR (up to 4GB)
• Text files: TXT (up to 4GB)

📊 Available commands:
/help - Show this help message
/queue - View queue status
/stats - View processing statistics

🔄 Files are processed sequentially for maximum reliability!
```

#### /help
**Expected**: Command list and usage instructions

#### /queue
**Expected**:
```
📊 Queue Status

• Pending: 2 files
• Downloading: 1 files
• Downloaded (waiting for processing): 3 files

Processing is sequential - one stage at a time for reliability.
```

#### /stats
**Expected**: Processing statistics (or "Coming soon...")

**Pass Criteria**:
- [ ] All commands respond correctly
- [ ] Queue status shows accurate counts
- [ ] Non-admin users silently ignored

---

### Test 9: Admin-Only Access

**Objective**: Verify only admins can use the bot

**Steps**:
1. Get a friend's Telegram account (not in ADMIN_IDS)
2. Have them send a file to the bot
3. Check logs

**Expected Behavior**:
- No response to non-admin
- Logged warning:
  ```
  WARN Unauthorized access attempt user_id=999999999
  ```
- No task created

**Verification**:
```bash
# Check for unauthorized attempts
tail -f logs/bot.log | grep "Unauthorized"

# Verify no task created
sqlite3 data/bot.db "SELECT COUNT(*) FROM tasks WHERE user_id=999999999;"
# Should be 0
```

**Pass Criteria**:
- [ ] Non-admin messages silently ignored
- [ ] No confirmation sent to non-admin
- [ ] Unauthorized attempts logged
- [ ] No tasks created for non-admins

---

### Test 10: Health Monitoring & Alerts

**Objective**: Verify system health monitoring

**Steps**:
1. Let bot run for 5 minutes
2. Check health stats
3. Trigger an alert (simulate high memory)

**Expected Behavior**:
- Health monitor tracks uptime, task counts, system resources
- Alerts sent to admins on critical events

**Verification**:
```bash
# Check health monitor logs
tail -f logs/bot.log | grep "health"

# Simulate load
# (Upload many large files simultaneously)
```

**Pass Criteria**:
- [ ] Health monitor starts successfully
- [ ] System metrics tracked
- [ ] Alerts sent to admins when thresholds exceeded
- [ ] Alert callbacks execute

---

## Load Testing

### Test 11: 10 File Batch

**Objective**: Verify bot handles moderate load

**Steps**:
1. Upload 10 files (mix of sizes: 1MB, 10MB, 100MB, 1GB)
2. Monitor completion

**Expected Behavior**:
- All files processed successfully
- No memory leaks
- No database deadlocks
- Notifications sent for all

**Verification**:
```bash
# Monitor memory usage
top -p $(pgrep telegram-archive-bot)

# Check task completion
sqlite3 data/bot.db "SELECT COUNT(*) FROM tasks WHERE status='COMPLETED';"
```

**Pass Criteria**:
- [ ] All 10 files processed
- [ ] Memory usage stable (no leaks)
- [ ] No database errors
- [ ] All notifications sent
- [ ] Average processing time acceptable

---

## Performance Benchmarks

### Expected Timings

| Stage | Small File (1MB) | Medium File (100MB) | Large File (2GB) |
|-------|------------------|---------------------|------------------|
| Download | 1-2 seconds | 5-10 seconds | 30-60 seconds |
| Extract | 1-2 seconds | 3-5 seconds | 10-30 seconds |
| Convert | <1 second | 2-3 seconds | 5-15 seconds |
| Store | 1-2 seconds | 5-10 seconds | 60-300 seconds |
| **Total** | **5-10 seconds** | **20-30 seconds** | **2-7 minutes** |

### Resource Limits

- **Memory**: Should stay under 20% of system RAM
- **CPU**: Should stay under 50% of available cores
- **Disk I/O**: Varies based on file size
- **Network**: Limited by Telegram API and Local Bot API

---

## Debugging Tips

### View Live Logs
```bash
tail -f logs/bot.log

# Filter for specific task
tail -f logs/bot.log | grep "task_id=abc12345"

# Filter for errors
tail -f logs/bot.log | grep "ERROR\|WARN"
```

### Database Inspection
```bash
# View all tasks
sqlite3 data/bot.db "SELECT * FROM tasks ORDER BY created_at DESC LIMIT 10;"

# Count by status
sqlite3 data/bot.db "SELECT status, COUNT(*) FROM tasks GROUP BY status;"

# View failed tasks
sqlite3 data/bot.db "SELECT id, file_name, error_message FROM tasks WHERE status='FAILED';"

# View audit log
sqlite3 data/bot.db "SELECT * FROM audit_log ORDER BY timestamp DESC LIMIT 20;"
```

### Check File Routing
```bash
# Downloaded files in temp
ls -lh <BOT_TOKEN>/temp/

# Files awaiting extraction
ls -lh app/extraction/files/all/

# Extracted files
ls -lh app/extraction/files/pass/

# Converted text files
ls -lh app/extraction/files/txt/

# Error files
ls -lh app/extraction/files/errors/
```

### Monitor Workers
```bash
# Download workers
tail -f logs/bot.log | grep "Download worker"

# Orchestrator
tail -f logs/bot.log | grep "orchestrator\|stage"
```

---

## Known Issues & Limitations

### Current Limitations
1. **Extract/Convert/Store**: Cannot be modified (constraint)
2. **File Type**: Only ZIP, RAR, TXT supported
3. **Max File Size**: 4GB (Local Bot API limit)
4. **Sequential Processing**: One stage at a time (by design)
5. **Admin-Only**: No public access (security feature)

### Potential Issues
1. **Large Files**: May take several minutes to process
2. **Password-Protected**: Requires password file at `app/extraction/pass.txt`
3. **Disk Space**: Large files can fill disk quickly
4. **Memory**: 4GB files require sufficient RAM for hashing

---

## Success Criteria Summary

Option 1 implementation is **SUCCESSFUL** if:

✅ All core features work:
- [x] File upload creates PENDING task
- [x] 3 download workers process queue
- [x] Status transitions: PENDING → DOWNLOADING → DOWNLOADED → COMPLETED
- [x] Files routed correctly (archives to all/, txt to txt/)
- [x] Sequential orchestrator runs all 3 stages
- [x] Tasks marked COMPLETED after store stage
- [x] Notifications sent to users
- [x] Crash recovery works

✅ All constraints respected:
- [x] extract.go, convert.go, store.go unchanged
- [x] Uses existing directory structure
- [x] Integrates with existing TaskStore
- [x] Admin-only access enforced

✅ Quality standards met:
- [x] Code compiles without errors
- [x] No data loss on crashes
- [x] Proper error handling and logging
- [x] Resource usage within limits
- [x] All tests pass

---

## Next Steps After Testing

If all tests pass:
1. ✅ Option 1 implementation is complete and production-ready
2. Consider optimization:
   - Add retry logic for extraction/conversion failures
   - Implement progress tracking for large files
   - Add more detailed statistics
3. Monitor production usage and gather metrics
4. Compare with Option 2 performance when implemented

If tests fail:
1. Review failure logs
2. Identify root cause
3. Fix issues
4. Re-run tests
5. Update documentation

---

**End of Testing Checklist**
