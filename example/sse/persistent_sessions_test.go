package main

import "testing"

func TestPersistentSessionTokenValidation(t *testing.T) {
	patientToken := "01KXD8H17ETRN8T2AVPMG2JTY3"
	staffToken := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if !validPersistentSessionToken(patientToken, persistentSessionPatient) {
		t.Fatal("valid patient token rejected")
	}
	if !validPersistentSessionToken(staffToken, persistentSessionDoctor) {
		t.Fatal("valid doctor token rejected")
	}
	if !validPersistentSessionToken(staffToken, persistentSessionAdmin) {
		t.Fatal("valid admin token rejected")
	}
	if validPersistentSessionToken("short", persistentSessionPatient) {
		t.Fatal("invalid patient token accepted")
	}
	if validPersistentSessionToken(patientToken, persistentSessionDoctor) {
		t.Fatal("patient token accepted as doctor token")
	}
}

func TestPersistentSessionStoresTokenHashOnly(t *testing.T) {
	token := "01KXD8H17ETRN8T2AVPMG2JTY3"
	hash := persistentSessionTokenHash(token)
	if hash == token {
		t.Fatal("session token was not hashed")
	}
	if len(hash) != 64 {
		t.Fatalf("hash length = %d", len(hash))
	}
	if hash != persistentSessionTokenHash(token) {
		t.Fatal("session token hash is not deterministic")
	}
}
