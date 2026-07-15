package main

import (
	"errors"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	staffRoleAdmin  = "admin"
	staffRoleDoctor = "doctor"
)

type StaffUser struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	Username     string     `gorm:"size:64;uniqueIndex" json:"username"`
	PasswordHash string     `gorm:"size:255" json:"-"`
	DisplayName  string     `gorm:"size:128" json:"displayName"`
	Role         string     `gorm:"size:24;index" json:"role"`
	Department   string     `gorm:"size:128" json:"department"`
	Enabled      bool       `gorm:"default:true;index" json:"enabled"`
	LastLoginAt  *time.Time `json:"lastLoginAt"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type AuditLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Actor     string    `gorm:"size:128;index" json:"actor"`
	Action    string    `gorm:"size:64;index" json:"action"`
	Target    string    `gorm:"size:255" json:"target"`
	Detail    string    `gorm:"type:text" json:"detail"`
	CreatedAt time.Time `gorm:"index" json:"createdAt"`
}

type PlatformConfig struct {
	ID                   uint      `gorm:"primaryKey" json:"id"`
	ServiceName          string    `gorm:"size:128" json:"serviceName"`
	DefaultLanguage      string    `gorm:"size:32" json:"defaultLanguage"`
	SessionRetentionDays int       `json:"sessionRetentionDays"`
	AlertLevel           string    `gorm:"size:8" json:"alertLevel"`
	UpdatedAt            time.Time `json:"updatedAt"`
}

func hashPassword(password string) (string, error) {
	password = strings.TrimSpace(password)
	if len(password) < 6 {
		return "", errors.New("password must contain at least 6 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

func verifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func seedStaffAccounts() error {
	adminUsername := envOrDefault("ADMIN_USERNAME", "medical_admin")
	adminPassword := envOrDefault("ADMIN_PASSWORD", "MedTriage@2026!88")
	adminDisplayName := envOrDefault("ADMIN_DISPLAY_NAME", "\u533b\u7597\u5e73\u53f0\u7ba1\u7406\u5458")
	if err := syncConfiguredAdmin(adminUsername, adminPassword, adminDisplayName); err != nil {
		return err
	}
	doctorUsername, doctorPassword := doctorCredentials()
	return ensureStaffUser(doctorUsername, doctorPassword, doctorDisplayName(), staffRoleDoctor, "")
}

func syncConfiguredAdmin(username, password, displayName string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	var user StaffUser
	result := globalDB.Where("username = ? AND role = ?", username, staffRoleAdmin).First(&user)
	if result.Error == gorm.ErrRecordNotFound {
		if err := globalDB.Create(&StaffUser{Username: username, PasswordHash: hash, DisplayName: displayName, Role: staffRoleAdmin, Enabled: true}).Error; err != nil {
			return err
		}
	} else if result.Error != nil {
		return result.Error
	} else {
		if err := globalDB.Model(&user).Updates(map[string]any{"password_hash": hash, "display_name": displayName, "enabled": true}).Error; err != nil {
			return err
		}
	}
	return globalDB.Model(&StaffUser{}).Where("role = ? AND username <> ?", staffRoleAdmin, username).Update("enabled", false).Error
}

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func ensureStaffUser(username, password, displayName, role, department string) error {
	var count int64
	if err := globalDB.Model(&StaffUser{}).Where("username = ?", username).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	return globalDB.Create(&StaffUser{Username: username, PasswordHash: hash, DisplayName: displayName, Role: role, Department: department, Enabled: true}).Error
}

func authenticateStaff(username, password, role string) (StaffUser, error) {
	var user StaffUser
	err := globalDB.Where("username = ? AND role = ?", strings.TrimSpace(username), role).First(&user).Error
	if err != nil {
		return StaffUser{}, err
	}
	if !user.Enabled || !verifyPassword(user.PasswordHash, password) {
		return StaffUser{}, gorm.ErrRecordNotFound
	}
	now := time.Now()
	_ = globalDB.Model(&user).Update("last_login_at", now).Error
	user.LastLoginAt = &now
	return user, nil
}

func writeAudit(actor, action, target, detail string) {
	if globalDB == nil {
		return
	}
	_ = globalDB.Create(&AuditLog{Actor: actor, Action: action, Target: target, Detail: detail}).Error
}
