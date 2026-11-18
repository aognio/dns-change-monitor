// Package persistence stores the domain state in a SQLite database through
// GORM. It implements the repository ports declared by the domain packages.
package persistence

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// StringList stores a list of strings in a single text column.
type StringList []string

// TargetModel is the stored form of a monitored DNS name.
type TargetModel struct {
	ID         string    `gorm:"primaryKey;size:64"`
	Name       string    `gorm:"index:idx_target_identity,unique;size:255"`
	RecordType string    `gorm:"index:idx_target_identity,unique;size:16"`
	Resolver   string    `gorm:"index:idx_target_identity,unique;size:255"`
	Enabled    bool      `gorm:"not null;default:true"`
	Source     string    `gorm:"size:16"`
	CreatedAt  time.Time `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"not null"`
}

// TableName implements the GORM naming convention.
func (TargetModel) TableName() string { return "dns_targets" }

// SnapshotModel is the stored form of a resolution result.
type SnapshotModel struct {
	ID         string     `gorm:"primaryKey;size:64"`
	TargetID   string     `gorm:"index;size:64"`
	Values     StringList `gorm:"type:text"`
	TTLSeconds int64      `gorm:"not null;default:0"`
	ObservedAt time.Time  `gorm:"index;not null"`
}

// TableName implements the GORM naming convention.
func (SnapshotModel) TableName() string { return "dns_snapshots" }

// ChangeModel is the stored form of a detected difference.
type ChangeModel struct {
	ID         string     `gorm:"primaryKey;size:64"`
	TargetID   string     `gorm:"index;size:64"`
	TargetName string     `gorm:"size:255"`
	RecordType string     `gorm:"size:16"`
	Added      StringList `gorm:"type:text"`
	Removed    StringList `gorm:"type:text"`
	DetectedAt time.Time  `gorm:"index;not null"`
	NotifiedAt *time.Time `gorm:"index"`
}

// TableName implements the GORM naming convention.
func (ChangeModel) TableName() string { return "dns_changes" }

// SubscriberModel is the stored form of a notifyable Telegram account.
type SubscriberModel struct {
	ID             string     `gorm:"primaryKey;size:64"`
	Username       string     `gorm:"uniqueIndex;size:128"`
	TelegramUserID int64      `gorm:"index"`
	ChatID         int64      `gorm:"not null;default:0"`
	Status         string     `gorm:"size:16"`
	CreatedAt      time.Time  `gorm:"not null"`
	UpdatedAt      time.Time  `gorm:"not null"`
	PairedAt       *time.Time `gorm:"index"`
}

// TableName implements the GORM naming convention.
func (SubscriberModel) TableName() string { return "subscribers" }

// PairingTokenModel is the stored form of a pairing token.
type PairingTokenModel struct {
	ID         string     `gorm:"primaryKey;size:64"`
	CreatedAt  time.Time  `gorm:"not null"`
	ExpiresAt  time.Time  `gorm:"index;not null"`
	ConsumedAt *time.Time `gorm:"index"`
	ConsumedBy string     `gorm:"size:64"`
	CreatedBy  string     `gorm:"size:64"`
}

// TableName implements the GORM naming convention.
func (PairingTokenModel) TableName() string { return "pairing_tokens" }

// AllModels lists every model managed by the automatic migration.
func AllModels() []any {
	return []any{
		&TargetModel{},
		&SnapshotModel{},
		&ChangeModel{},
		&SubscriberModel{},
		&PairingTokenModel{},
	}
}

// Open opens the SQLite database, creating the file and the schema when
// needed.
func Open(path string, verbose bool) (*gorm.DB, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create database directory %s: %w", dir, err)
		}
	}

	level := logger.Silent
	if verbose {
		level = logger.Info
	}

	db, err := gorm.Open(sqlite.Open(path+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"), &gorm.Config{
		Logger: logger.Default.LogMode(level),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	})
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}

	if err := db.AutoMigrate(AllModels()...); err != nil {
		return nil, fmt.Errorf("migrate database %s: %w", path, err)
	}
	return db, nil
}
