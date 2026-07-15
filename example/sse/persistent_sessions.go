package main

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"strings"
	"time"
)

const (
	persistentSessionPatient = "patient"
	persistentSessionDoctor  = "doctor"
	persistentSessionAdmin   = "admin"
)

type PersistentSession struct {
	TokenHash   string    `gorm:"size:64;primaryKey" json:"-"`
	Role        string    `gorm:"size:24;not null;index:idx_persistent_session_subject,priority:1;index" json:"role"`
	Subject     string    `gorm:"size:128;not null;index:idx_persistent_session_subject,priority:2" json:"subject"`
	DisplayName string    `gorm:"size:128" json:"displayName"`
	ExpiresAt   time.Time `gorm:"not null;index" json:"expiresAt"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func persistentSessionTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func validPersistentSessionToken(token, role string) bool {
	token = strings.TrimSpace(token)
	switch role {
	case persistentSessionPatient:
		return len(token) == 26
	case persistentSessionDoctor, persistentSessionAdmin:
		if len(token) != 64 {
			return false
		}
		_, err := hex.DecodeString(token)
		return err == nil
	default:
		return false
	}
}

func savePersistentSession(token, role, subject, displayName string, expiresAt time.Time) {
	if globalDB == nil || !validPersistentSessionToken(token, role) {
		return
	}
	record := PersistentSession{
		TokenHash:   persistentSessionTokenHash(token),
		Role:        role,
		Subject:     strings.TrimSpace(subject),
		DisplayName: strings.TrimSpace(displayName),
		ExpiresAt:   expiresAt,
	}
	if err := globalDB.Save(&record).Error; err != nil {
		log.Printf("persistent session save failed for %s: %v", role, err)
	}
}

func loadPersistentSession(token, role string) (PersistentSession, bool) {
	if globalDB == nil || !validPersistentSessionToken(token, role) {
		return PersistentSession{}, false
	}
	var record PersistentSession
	err := globalDB.Where("token_hash = ? AND role = ?", persistentSessionTokenHash(token), role).First(&record).Error
	if err != nil {
		return PersistentSession{}, false
	}
	if time.Now().After(record.ExpiresAt) {
		_ = globalDB.Delete(&record).Error
		return PersistentSession{}, false
	}
	return record, true
}

func deletePersistentSession(token, role string) {
	if globalDB == nil || !validPersistentSessionToken(token, role) {
		return
	}
	_ = globalDB.Where("token_hash = ? AND role = ?", persistentSessionTokenHash(token), role).Delete(&PersistentSession{}).Error
}

func deletePersistentSessionsForSubject(role, subject string) {
	if globalDB == nil || strings.TrimSpace(subject) == "" {
		return
	}
	_ = globalDB.Where("role = ? AND subject = ?", role, strings.TrimSpace(subject)).Delete(&PersistentSession{}).Error
}

func deleteOtherPersistentSessionsForSubject(role, subject, keepToken string) {
	if globalDB == nil || strings.TrimSpace(subject) == "" {
		return
	}
	query := globalDB.Where("role = ? AND subject = ?", role, strings.TrimSpace(subject))
	if validPersistentSessionToken(keepToken, role) {
		query = query.Where("token_hash <> ?", persistentSessionTokenHash(keepToken))
	}
	_ = query.Delete(&PersistentSession{}).Error
}

func cleanupExpiredPersistentSessions() {
	if globalDB == nil {
		return
	}
	if err := globalDB.Where("expires_at <= ?", time.Now()).Delete(&PersistentSession{}).Error; err != nil {
		log.Printf("persistent session cleanup failed: %v", err)
	}
}

func startPersistentSessionCleanupLoop() {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			cleanupExpiredPersistentSessions()
		}
	}()
}
