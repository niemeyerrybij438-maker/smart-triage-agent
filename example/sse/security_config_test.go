package main

import "testing"

func TestValidateRuntimeConfigurationDevelopment(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("BaseUrl", "https://example.test/v1")
	t.Setenv("APIKey", "test-key")
	t.Setenv("MYSQL_DSN", "user:password@tcp(localhost:3306)/aggo")
	if err := validateRuntimeConfiguration(); err != nil {
		t.Fatalf("development configuration failed: %v", err)
	}
}

func TestValidateRuntimeConfigurationProduction(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("BaseUrl", "https://example.test/v1")
	t.Setenv("APIKey", "test-key")
	t.Setenv("MYSQL_DSN", "user:password@tcp(localhost:3306)/aggo")
	t.Setenv("DOCTOR_USERNAME", "clinician")
	t.Setenv("DOCTOR_PASSWORD", "Doctor-Strong-2026")
	t.Setenv("ADMIN_USERNAME", "platform_manager")
	t.Setenv("ADMIN_PASSWORD", "Admin-Strong-2026")
	t.Setenv("BAIDU_MAP_AK", "test-map-key")
	t.Setenv("SMS_PROVIDER_URL", "https://sms.example.test/send")
	if err := validateRuntimeConfiguration(); err != nil {
		t.Fatalf("production configuration failed: %v", err)
	}
}

func TestValidateRuntimeConfigurationRejectsDemoPassword(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("BaseUrl", "https://example.test/v1")
	t.Setenv("APIKey", "test-key")
	t.Setenv("MYSQL_DSN", "user:password@tcp(localhost:3306)/aggo")
	t.Setenv("DOCTOR_USERNAME", "doctor")
	t.Setenv("DOCTOR_PASSWORD", "doctor123")
	t.Setenv("ADMIN_USERNAME", "platform_manager")
	t.Setenv("ADMIN_PASSWORD", "Admin-Strong-2026")
	t.Setenv("BAIDU_MAP_AK", "test-map-key")
	t.Setenv("SMS_PROVIDER_URL", "https://sms.example.test/send")
	if err := validateRuntimeConfiguration(); err == nil {
		t.Fatal("production configuration must reject demo passwords")
	}
}

func TestValidateRuntimeConfigurationRequiresDatabase(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("BaseUrl", "https://example.test/v1")
	t.Setenv("APIKey", "test-key")
	t.Setenv("MYSQL_DSN", "")
	if err := validateRuntimeConfiguration(); err == nil {
		t.Fatal("configuration must require MYSQL_DSN")
	}
}
