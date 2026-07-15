package main

import (
	"fmt"
	"os"
	"strings"
)

func validateRuntimeConfiguration() error {
	for _, key := range []string{"BaseUrl", "APIKey", "MYSQL_DSN"} {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			return fmt.Errorf("%s environment variable must be set", key)
		}
	}
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return nil
	}
	for _, key := range []string{"DOCTOR_USERNAME", "DOCTOR_PASSWORD", "ADMIN_USERNAME", "ADMIN_PASSWORD", "BAIDU_MAP_AK"} {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			return fmt.Errorf("%s environment variable must be set in production", key)
		}
	}
	for _, credential := range []struct {
		name  string
		value string
	}{
		{name: "DOCTOR_PASSWORD", value: os.Getenv("DOCTOR_PASSWORD")},
		{name: "ADMIN_PASSWORD", value: os.Getenv("ADMIN_PASSWORD")},
	} {
		if weakRuntimePassword(credential.value) {
			return fmt.Errorf("%s must contain at least 12 characters and must not use a demo password", credential.name)
		}
	}
	if !productionSMSConfigured() {
		return fmt.Errorf("production SMS provider is not configured")
	}
	return nil
}

func weakRuntimePassword(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 12 {
		return true
	}
	switch value {
	case "doctor123", "MedTriage@2026!88", "replace-with-a-strong-password":
		return true
	default:
		return false
	}
}

func productionSMSConfigured() bool {
	account := strings.TrimSpace(os.Getenv("IHUYI_ACCOUNT"))
	password := strings.TrimSpace(os.Getenv("IHUYI_PASSWORD"))
	if account != "" && password != "" {
		return true
	}
	return strings.TrimSpace(os.Getenv("SMS_PROVIDER_URL")) != ""
}
