package main

import (
	_ "embed"
	"net/http"
)

//go:embed web/patient.html
var patientPage []byte

//go:embed web/patient_login.html
var patientLoginPage []byte

//go:embed web/doctor.html
var doctorPage []byte

//go:embed web/doctor_login.html
var doctorLoginPage []byte

//go:embed web/admin.html
var adminPage []byte

//go:embed web/admin_login.html
var adminLoginPage []byte

func writeHTMLPage(w http.ResponseWriter, page []byte) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	_, _ = w.Write(page)
}
