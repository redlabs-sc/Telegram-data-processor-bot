package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"telegram-archive-bot/storage"
	"telegram-archive-bot/utils"
)

var (
	action         = flag.String("action", "", "Action to perform: backup, restore, list, cleanup, stats")
	configFile     = flag.String("config", ".env", "Path to config file")
	backupFile     = flag.String("file", "", "Backup file path (for restore)")
	backupDir      = flag.String("dir", "backups", "Backup directory")
	retentionDays  = flag.Int("retention", 30, "Backup retention in days")
	compress       = flag.Bool("compress", true, "Compress backups")
	verify         = flag.Bool("verify", true, "Verify backup/restore operations")
	createBackup   = flag.Bool("backup-current", true, "Create backup of current DB before restore")
	force          = flag.Bool("force", false, "Force operation without confirmation")
)

func main() {
	flag.Parse()

	if *action == "" {
		printUsage()
		os.Exit(1)
	}

	// Load configuration
	config, err := utils.LoadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Initialize database
	db, err := storage.NewDatabase(config.DatabasePath)
	if err != nil {
		fmt.Printf("Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	// Initialize backup service
	backupService, err := storage.NewBackupService(db, storage.BackupOptions{
		BackupDir:       *backupDir,
		RetentionPeriod: time.Duration(*retentionDays) * 24 * time.Hour,
		Compress:        *compress,
		VerifyBackup:    *verify,
	})
	if err != nil {
		fmt.Printf("Error initializing backup service: %v\n", err)
		os.Exit(1)
	}

	// Execute requested action
	switch *action {
	case "backup":
		executeBackup(backupService)
	case "restore":
		executeRestore(backupService)
	case "list":
		listBackups(backupService)
	case "cleanup":
		cleanupBackups(backupService)
	case "stats":
		showStats(backupService)
	default:
		fmt.Printf("Unknown action: %s\n", *action)
		printUsage()
		os.Exit(1)
	}
}

func executeBackup(bs *storage.BackupService) {
	fmt.Println("Creating database backup...")
	
	opts := storage.BackupOptions{
		BackupDir:    *backupDir,
		Compress:     *compress,
		VerifyBackup: *verify,
	}
	
	backupPath, err := bs.CreateBackup(opts)
	if err != nil {
		fmt.Printf("Error creating backup: %v\n", err)
		os.Exit(1)
	}
	
	// Get file info
	info, err := os.Stat(backupPath)
	if err != nil {
		fmt.Printf("Error getting backup file info: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("✅ Backup created successfully!\n")
	fmt.Printf("   File: %s\n", backupPath)
	fmt.Printf("   Size: %s\n", formatBytes(info.Size()))
	fmt.Printf("   Compressed: %t\n", *compress)
	fmt.Printf("   Verified: %t\n", *verify)
}

func executeRestore(bs *storage.BackupService) {
	if *backupFile == "" {
		fmt.Println("Error: backup file must be specified with -file flag")
		os.Exit(1)
	}
	
	// Check if backup file exists
	if _, err := os.Stat(*backupFile); os.IsNotExist(err) {
		fmt.Printf("Error: backup file does not exist: %s\n", *backupFile)
		os.Exit(1)
	}
	
	// Confirm restore operation
	if !*force {
		fmt.Printf("⚠️  This will restore the database from: %s\n", *backupFile)
		if *createBackup {
			fmt.Println("   A backup of the current database will be created first.")
		}
		fmt.Print("Are you sure you want to continue? (y/N): ")
		
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			fmt.Println("Restore cancelled.")
			return
		}
	}
	
	fmt.Println("Restoring database from backup...")
	
	opts := storage.RestoreOptions{
		BackupFile:    *backupFile,
		VerifyRestore: *verify,
		CreateBackup:  *createBackup,
	}
	
	err := bs.RestoreFromBackup(opts)
	if err != nil {
		fmt.Printf("Error restoring backup: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("✅ Database restored successfully from: %s\n", *backupFile)
	if *verify {
		fmt.Println("   Restore verified: integrity check passed")
	}
}

func listBackups(bs *storage.BackupService) {
	backups, err := bs.ListBackups()
	if err != nil {
		fmt.Printf("Error listing backups: %v\n", err)
		os.Exit(1)
	}
	
	if len(backups) == 0 {
		fmt.Printf("No backups found in directory: %s\n", *backupDir)
		return
	}
	
	fmt.Printf("Found %d backup(s) in %s:\n\n", len(backups), *backupDir)
	fmt.Printf("%-30s %-12s %-20s %s\n", "NAME", "SIZE", "CREATED", "COMPRESSED")
	fmt.Printf("%s\n", strings.Repeat("-", 80))
	
	for _, backup := range backups {
		compressed := "No"
		if backup.Compressed {
			compressed = "Yes"
		}
		
		fmt.Printf("%-30s %-12s %-20s %s\n",
			backup.Name,
			formatBytes(backup.Size),
			backup.Created.Format("2006-01-02 15:04:05"),
			compressed,
		)
	}
}

func cleanupBackups(bs *storage.BackupService) {
	if !*force {
		fmt.Printf("⚠️  This will remove backup files older than %d days.\n", *retentionDays)
		fmt.Print("Are you sure you want to continue? (y/N): ")
		
		var response string
		fmt.Scanln(&response)
		if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
			fmt.Println("Cleanup cancelled.")
			return
		}
	}
	
	fmt.Println("Cleaning up old backups...")
	
	err := bs.CleanupOldBackups()
	if err != nil {
		fmt.Printf("Error during cleanup: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Println("✅ Backup cleanup completed successfully!")
}

func showStats(bs *storage.BackupService) {
	stats, err := bs.GetBackupStats()
	if err != nil {
		fmt.Printf("Error getting backup stats: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Println("📊 Backup Statistics")
	fmt.Println(strings.Repeat("=", 40))
	fmt.Printf("Backup Directory:     %s\n", stats.BackupDir)
	fmt.Printf("Total Backups:        %d\n", stats.TotalBackups)
	fmt.Printf("Total Size:           %s\n", formatBytes(stats.TotalSize))
	fmt.Printf("Retention Period:     %v\n", stats.Retention)
	
	if stats.OldestBackup != nil {
		fmt.Printf("Oldest Backup:        %s\n", stats.OldestBackup.Format("2006-01-02 15:04:05"))
	}
	
	if stats.NewestBackup != nil {
		fmt.Printf("Newest Backup:        %s\n", stats.NewestBackup.Format("2006-01-02 15:04:05"))
	}
	
	if stats.TotalBackups > 0 {
		avgSize := stats.TotalSize / int64(stats.TotalBackups)
		fmt.Printf("Average Backup Size:  %s\n", formatBytes(avgSize))
	}
	
	// Check if cleanup is needed
	cutoffTime := time.Now().Add(-stats.Retention)
	if stats.OldestBackup != nil && stats.OldestBackup.Before(cutoffTime) {
		fmt.Printf("\n⚠️  Some backups are older than retention period and can be cleaned up.\n")
		fmt.Printf("   Run with -action=cleanup to remove old backups.\n")
	}
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func printUsage() {
	fmt.Println("Telegram Archive Bot - Database Backup Tool")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Printf("  %s -action=<action> [options]\n", os.Args[0])
	fmt.Println()
	fmt.Println("Actions:")
	fmt.Println("  backup    Create a new database backup")
	fmt.Println("  restore   Restore database from backup file")
	fmt.Println("  list      List available backup files")
	fmt.Println("  cleanup   Remove old backup files")
	fmt.Println("  stats     Show backup statistics")
	fmt.Println()
	fmt.Println("Options:")
	flag.PrintDefaults()
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  # Create a compressed backup")
	fmt.Printf("  %s -action=backup -compress=true\n", os.Args[0])
	fmt.Println()
	fmt.Println("  # List all backups")
	fmt.Printf("  %s -action=list\n", os.Args[0])
	fmt.Println()
	fmt.Println("  # Restore from specific backup")
	fmt.Printf("  %s -action=restore -file=backups/bot_backup_20240125_120000.sql.gz\n", os.Args[0])
	fmt.Println()
	fmt.Println("  # Cleanup old backups")
	fmt.Printf("  %s -action=cleanup -retention=7\n", os.Args[0])
	fmt.Println()
	fmt.Println("  # Show backup statistics")
	fmt.Printf("  %s -action=stats\n", os.Args[0])
}