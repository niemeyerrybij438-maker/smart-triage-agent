package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type config struct {
	baseURL        string
	patientPhone   string
	doctorUsername string
	doctorPassword string
	adminUsername  string
	adminPassword  string
	requests       int
	concurrency    int
	maxP95         time.Duration
	jsonPath       string
}

type target struct {
	name   string
	path   string
	client *http.Client
}

type sample struct {
	duration time.Duration
	status   int
	err      error
}

type benchmarkResult struct {
	Name          string  `json:"name"`
	Path          string  `json:"path"`
	Requests      int     `json:"requests"`
	Concurrency   int     `json:"concurrency"`
	Successes     int     `json:"successes"`
	Failures      int     `json:"failures"`
	ErrorRate     float64 `json:"errorRate"`
	AverageMS     float64 `json:"averageMs"`
	P50MS         float64 `json:"p50Ms"`
	P95MS         float64 `json:"p95Ms"`
	MaxMS         float64 `json:"maxMs"`
	ThresholdPass bool    `json:"thresholdPass"`
}

type report struct {
	GeneratedAt string            `json:"generatedAt"`
	BaseURL     string            `json:"baseUrl"`
	Requests    int               `json:"requestsPerEndpoint"`
	Concurrency int               `json:"concurrency"`
	MaxP95MS    float64           `json:"maxP95Ms"`
	Results     []benchmarkResult `json:"results"`
}

func main() {
	cfg := parseFlags()
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "performance check failed:", err)
		os.Exit(1)
	}
}

func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.baseURL, "base-url", "http://localhost:8080", "service base URL")
	flag.StringVar(&cfg.patientPhone, "patient-phone", "19552075183", "existing patient phone")
	flag.StringVar(&cfg.doctorUsername, "doctor-username", envOr("DOCTOR_USERNAME", "doctor"), "doctor username")
	flag.StringVar(&cfg.doctorPassword, "doctor-password", envOr("DOCTOR_PASSWORD", "doctor123"), "doctor password")
	flag.StringVar(&cfg.adminUsername, "admin-username", envOr("ADMIN_USERNAME", "medical_admin"), "admin username")
	flag.StringVar(&cfg.adminPassword, "admin-password", envOr("ADMIN_PASSWORD", "MedTriage@2026!88"), "admin password")
	flag.IntVar(&cfg.requests, "requests", 30, "requests per endpoint")
	flag.IntVar(&cfg.concurrency, "concurrency", 5, "parallel requests per endpoint")
	maxP95MS := flag.Int("max-p95-ms", 2000, "maximum allowed P95 latency in milliseconds")
	flag.StringVar(&cfg.jsonPath, "json", "performance-report.json", "JSON report path; empty disables output")
	flag.Parse()
	cfg.baseURL = strings.TrimRight(cfg.baseURL, "/")
	cfg.maxP95 = time.Duration(*maxP95MS) * time.Millisecond
	return cfg
}

func run(cfg config) error {
	if cfg.requests <= 0 || cfg.concurrency <= 0 {
		return errors.New("requests and concurrency must be positive")
	}
	publicClient := newClient()
	patientClient := newClient()
	doctorClient := newClient()
	adminClient := newClient()
	if err := loginPatient(patientClient, cfg); err != nil {
		return fmt.Errorf("patient login: %w", err)
	}
	if err := loginStaff(doctorClient, cfg.baseURL+"/api/doctor/login", cfg.doctorUsername, cfg.doctorPassword); err != nil {
		return fmt.Errorf("doctor login: %w", err)
	}
	if err := loginStaff(adminClient, cfg.baseURL+"/api/admin/login", cfg.adminUsername, cfg.adminPassword); err != nil {
		return fmt.Errorf("admin login: %w", err)
	}

	targets := []target{
		{name: "Public patient page", path: "/patient/login", client: publicClient},
		{name: "Patient profile", path: "/api/patient/me", client: patientClient},
		{name: "Patient triage records", path: "/api/triage-records", client: patientClient},
		{name: "Patient follow-ups", path: "/api/follow-ups", client: patientClient},
		{name: "Doctor triage records", path: "/api/triage-records", client: doctorClient},
		{name: "Doctor follow-ups", path: "/api/follow-ups", client: doctorClient},
		{name: "Admin dashboard", path: "/api/admin/dashboard", client: adminClient},
		{name: "Admin triage records", path: "/api/admin/records", client: adminClient},
		{name: "Admin follow-ups", path: "/api/admin/follow-ups", client: adminClient},
		{name: "Admin agent traces", path: "/api/admin/agent-traces?limit=200&compact=1", client: adminClient},
	}

	results := make([]benchmarkResult, 0, len(targets))
	failed := false
	fmt.Printf("Performance baseline: %d requests/endpoint, concurrency=%d, max P95=%s\n\n", cfg.requests, cfg.concurrency, cfg.maxP95)
	fmt.Printf("%-25s %7s %7s %8s %8s %8s %8s\n", "Endpoint", "Avg", "P50", "P95", "Max", "Errors", "Result")
	for _, item := range targets {
		result := benchmark(cfg, item)
		results = append(results, result)
		status := "PASS"
		if !result.ThresholdPass {
			status = "FAIL"
			failed = true
		}
		fmt.Printf("%-25s %6.1fms %6.1fms %7.1fms %7.1fms %7.2f%% %8s\n", result.Name, result.AverageMS, result.P50MS, result.P95MS, result.MaxMS, result.ErrorRate*100, status)
	}

	data := report{GeneratedAt: time.Now().Format(time.RFC3339), BaseURL: cfg.baseURL, Requests: cfg.requests, Concurrency: cfg.concurrency, MaxP95MS: milliseconds(cfg.maxP95), Results: results}
	if cfg.jsonPath != "" {
		encoded, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(cfg.jsonPath, encoded, 0o644); err != nil {
			return err
		}
		fmt.Println("\nJSON report:", cfg.jsonPath)
	}
	if failed {
		return errors.New("one or more endpoints exceeded the latency or error threshold")
	}
	return nil
}

func benchmark(cfg config, item target) benchmarkResult {
	jobs := make(chan struct{})
	samples := make(chan sample, cfg.requests)
	var workers sync.WaitGroup
	workerCount := min(cfg.concurrency, cfg.requests)
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range jobs {
				started := time.Now()
				response, err := item.client.Get(cfg.baseURL + item.path)
				duration := time.Since(started)
				status := 0
				if response != nil {
					status = response.StatusCode
					_, _ = io.Copy(io.Discard, response.Body)
					_ = response.Body.Close()
				}
				if err == nil && (status < 200 || status >= 300) {
					err = fmt.Errorf("HTTP %d", status)
				}
				samples <- sample{duration: duration, status: status, err: err}
			}
		}()
	}
	go func() {
		for range cfg.requests {
			jobs <- struct{}{}
		}
		close(jobs)
		workers.Wait()
		close(samples)
	}()

	durations := make([]time.Duration, 0, cfg.requests)
	failures := 0
	var total time.Duration
	for value := range samples {
		durations = append(durations, value.duration)
		total += value.duration
		if value.err != nil {
			failures++
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	average := time.Duration(0)
	if len(durations) > 0 {
		average = total / time.Duration(len(durations))
	}
	p50 := percentile(durations, 0.50)
	p95 := percentile(durations, 0.95)
	maximum := percentile(durations, 1)
	errorRate := float64(failures) / float64(cfg.requests)
	return benchmarkResult{
		Name:          item.name,
		Path:          item.path,
		Requests:      cfg.requests,
		Concurrency:   cfg.concurrency,
		Successes:     cfg.requests - failures,
		Failures:      failures,
		ErrorRate:     errorRate,
		AverageMS:     milliseconds(average),
		P50MS:         milliseconds(p50),
		P95MS:         milliseconds(p95),
		MaxMS:         milliseconds(maximum),
		ThresholdPass: failures == 0 && p95 <= cfg.maxP95,
	}
}

func percentile(values []time.Duration, ratio float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	if ratio <= 0 {
		return values[0]
	}
	if ratio >= 1 {
		return values[len(values)-1]
	}
	index := int(float64(len(values))*ratio + 0.999999)
	if index < 1 {
		index = 1
	}
	return values[index-1]
}

func milliseconds(value time.Duration) float64 {
	return float64(value.Microseconds()) / 1000
}

func newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Timeout: 20 * time.Second, Jar: jar, Transport: &http.Transport{MaxIdleConns: 50, MaxIdleConnsPerHost: 20, IdleConnTimeout: 30 * time.Second}}
}

func loginPatient(client *http.Client, cfg config) error {
	var sms struct {
		DebugCode string `json:"debugCode"`
	}
	if err := postJSON(client, cfg.baseURL+"/api/patient/sms", map[string]string{"phone": cfg.patientPhone}, &sms); err != nil {
		return err
	}
	if strings.TrimSpace(sms.DebugCode) == "" {
		return errors.New("local SMS response did not include debugCode")
	}
	return postJSON(client, cfg.baseURL+"/api/patient/login", map[string]string{"phone": cfg.patientPhone, "code": sms.DebugCode}, nil)
}

func loginStaff(client *http.Client, endpoint, username, password string) error {
	return postJSON(client, endpoint, map[string]string{"username": username, "password": password}, nil)
}

func postJSON(client *http.Client, endpoint string, payload any, output any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	response, err := client.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		text, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(text)))
	}
	if output == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	return json.NewDecoder(response.Body).Decode(output)
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
