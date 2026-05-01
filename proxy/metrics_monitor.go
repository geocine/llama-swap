package proxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mostlygeek/llama-swap/event"
	"github.com/tidwall/gjson"
	_ "modernc.org/sqlite"
)

// TokenMetrics represents parsed token statistics from llama-server logs
type TokenMetrics struct {
	ID              int       `json:"id"`
	Timestamp       time.Time `json:"timestamp"`
	Model           string    `json:"model"`
	CachedTokens    int       `json:"cache_tokens"`
	InputTokens     int       `json:"input_tokens"`
	OutputTokens    int       `json:"output_tokens"`
	PromptPerSecond float64   `json:"prompt_per_second"`
	TokensPerSecond float64   `json:"tokens_per_second"`
	DurationMs      int       `json:"duration_ms"`
	HasCapture      bool      `json:"has_capture"`
}

type ReqRespCapture struct {
	ID          int               `json:"id"`
	ReqPath     string            `json:"req_path"`
	ReqHeaders  map[string]string `json:"req_headers"`
	ReqBody     []byte            `json:"req_body"`
	RespHeaders map[string]string `json:"resp_headers"`
	RespBody    []byte            `json:"resp_body"`
}

// Size returns the approximate memory usage of this capture in bytes
func (c *ReqRespCapture) Size() int {
	size := len(c.ReqPath) + len(c.ReqBody) + len(c.RespBody)
	for k, v := range c.ReqHeaders {
		size += len(k) + len(v)
	}
	for k, v := range c.RespHeaders {
		size += len(k) + len(v)
	}
	return size
}

// TokenMetricsEvent represents a token metrics event
type TokenMetricsEvent struct {
	Metrics TokenMetrics
}

func (e TokenMetricsEvent) Type() uint32 {
	return TokenMetricsEventID // defined in events.go
}

type storedCapture struct {
	capture  ReqRespCapture
	size     int
	created  int64
	decrypts bool
}

// metricsMonitor parses llama-server output for token statistics
type metricsMonitor struct {
	mu         sync.RWMutex
	metrics    []TokenMetrics
	maxMetrics int
	nextID     int
	logger     *LogMonitor

	// capture fields
	enableCaptures bool
	captureDB      *sql.DB
	captureDBPath  string
	captureCipher  cipher.AEAD
}

// newMetricsMonitor creates a new metricsMonitor. captureBufferMB is the
// legacy capture buffer setting; 0 disables captures. When captures are enabled,
// bodies are persisted in SQLite instead of being held in memory.
func newMetricsMonitor(logger *LogMonitor, maxMetrics int, captureBufferMB int, captureDBPath ...string) *metricsMonitor {
	dbPath := ":memory:"
	if len(captureDBPath) > 0 && strings.TrimSpace(captureDBPath[0]) != "" {
		dbPath = captureDBPath[0]
	}
	captureSecret := ""
	if len(captureDBPath) > 1 {
		captureSecret = captureDBPath[1]
	}

	mp := &metricsMonitor{
		logger:         logger,
		maxMetrics:     maxMetrics,
		enableCaptures: captureBufferMB > 0,
		captureDBPath:  dbPath,
	}

	if mp.enableCaptures {
		db, err := openCaptureDB(dbPath)
		if err != nil {
			logger.Warnf("failed to initialize capture sqlite database %q: %v; captures disabled", dbPath, err)
			mp.enableCaptures = false
		} else {
			mp.captureDB = db
		}

		if captureSecret != "" {
			captureCipher, err := newCaptureCipher(captureSecret)
			if err != nil {
				logger.Warnf("failed to initialize capture encryption: %v; captures disabled", err)
				mp.enableCaptures = false
				if mp.captureDB != nil {
					mp.captureDB.Close()
					mp.captureDB = nil
				}
			} else {
				mp.captureCipher = captureCipher
			}
		}
	}

	return mp
}

// addMetrics adds a new metric to the collection and publishes an event.
// Returns the assigned metric ID.
func (mp *metricsMonitor) addMetrics(metric TokenMetrics) int {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	metric.ID = mp.nextID
	mp.nextID++
	mp.metrics = append(mp.metrics, metric)
	if len(mp.metrics) > mp.maxMetrics {
		mp.metrics = mp.metrics[len(mp.metrics)-mp.maxMetrics:]
	}
	event.Emit(TokenMetricsEvent{Metrics: metric})
	return metric.ID
}

func createCaptureSchema(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS captures (
			id INTEGER PRIMARY KEY,
			req_path BLOB NOT NULL,
			req_headers BLOB NOT NULL,
			req_body BLOB,
			resp_headers BLOB NOT NULL,
			resp_body BLOB,
			size INTEGER NOT NULL,
			encrypted INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL
		)
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS captures_created_at_idx ON captures(created_at)`); err != nil {
		return err
	}
	if _, err := db.Exec(`ALTER TABLE captures ADD COLUMN encrypted INTEGER NOT NULL DEFAULT 0`); err != nil {
		errText := strings.ToLower(err.Error())
		if !strings.Contains(errText, "duplicate column") {
			return err
		}
	}
	return nil
}

func openCaptureDB(dbPath string) (*sql.DB, error) {
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}
	if dbPath != ":memory:" {
		if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err := createCaptureSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func newCaptureCipher(secret string) (cipher.AEAD, error) {
	key := sha256.Sum256([]byte("llama-swap capture db\x00" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func encryptCaptureField(aead cipher.AEAD, value []byte) ([]byte, error) {
	if aead == nil {
		return value, nil
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, value, nil), nil
}

func decryptCaptureField(aead cipher.AEAD, value []byte) ([]byte, error) {
	if aead == nil {
		return value, nil
	}
	if len(value) < aead.NonceSize() {
		return nil, fmt.Errorf("encrypted capture field is too short")
	}
	nonce := value[:aead.NonceSize()]
	ciphertext := value[aead.NonceSize():]
	return aead.Open(nil, nonce, ciphertext, nil)
}

// addCapture adds a new capture to the buffer with size-based eviction.
// Captures are skipped if enableCaptures is false.
func (mp *metricsMonitor) addCapture(capture ReqRespCapture) {
	if !mp.enableCaptures || mp.captureDB == nil {
		return
	}

	reqHeaders, err := json.Marshal(capture.ReqHeaders)
	if err != nil {
		mp.logger.Warnf("failed to marshal capture request headers for metric %d: %v", capture.ID, err)
		mp.setMetricCaptureAvailable(capture.ID, false)
		return
	}
	respHeaders, err := json.Marshal(capture.RespHeaders)
	if err != nil {
		mp.logger.Warnf("failed to marshal capture response headers for metric %d: %v", capture.ID, err)
		mp.setMetricCaptureAvailable(capture.ID, false)
		return
	}

	reqPath := []byte(capture.ReqPath)
	reqBody := capture.ReqBody
	respBody := capture.RespBody
	encrypted := 0
	if mp.captureCipher != nil {
		encrypted = 1
	}
	fields := []*[]byte{&reqPath, &reqHeaders, &reqBody, &respHeaders, &respBody}
	for _, field := range fields {
		encryptedField, err := encryptCaptureField(mp.captureCipher, *field)
		if err != nil {
			mp.logger.Warnf("failed to encrypt capture %d: %v", capture.ID, err)
			mp.setMetricCaptureAvailable(capture.ID, false)
			return
		}
		*field = encryptedField
	}

	_, err = mp.captureDB.Exec(`
		INSERT OR REPLACE INTO captures
			(id, req_path, req_headers, req_body, resp_headers, resp_body, size, encrypted, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		capture.ID,
		reqPath,
		reqHeaders,
		reqBody,
		respHeaders,
		respBody,
		capture.Size(),
		encrypted,
		time.Now().UnixNano(),
	)
	if err != nil {
		mp.logger.Warnf("failed to store capture %d in sqlite: %v", capture.ID, err)
		mp.setMetricCaptureAvailable(capture.ID, false)
		return
	}
	mp.setMetricCaptureAvailable(capture.ID, true)
}

func (mp *metricsMonitor) setMetricCaptureAvailable(id int, available bool) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.setMetricCaptureAvailableLocked(id, available)
}

func (mp *metricsMonitor) setMetricCaptureAvailableLocked(id int, available bool) {
	for i := range mp.metrics {
		if mp.metrics[i].ID == id {
			mp.metrics[i].HasCapture = available
			return
		}
	}
}

func (mp *metricsMonitor) scanStoredCapture(row interface {
	Scan(dest ...any) error
}, id int) (*storedCapture, error) {
	var capture ReqRespCapture
	var reqPath []byte
	var reqHeaders []byte
	var respHeaders []byte
	var encrypted int
	var size int
	var created int64
	err := row.Scan(
		&capture.ID,
		&reqPath,
		&reqHeaders,
		&capture.ReqBody,
		&respHeaders,
		&capture.RespBody,
		&size,
		&encrypted,
		&created,
	)
	if err != nil {
		return nil, err
	}
	logID := id
	if logID == 0 {
		logID = capture.ID
	}
	if encrypted == 1 {
		if mp.captureCipher == nil {
			return nil, fmt.Errorf("capture %d is encrypted but no capture key is configured", logID)
		}
		fields := []*[]byte{&reqPath, &reqHeaders, &capture.ReqBody, &respHeaders, &capture.RespBody}
		for _, field := range fields {
			decryptedField, err := decryptCaptureField(mp.captureCipher, *field)
			if err != nil {
				return nil, fmt.Errorf("failed to decrypt capture %d: %w", logID, err)
			}
			*field = decryptedField
		}
	}

	capture.ReqPath = string(reqPath)
	if err := json.Unmarshal(reqHeaders, &capture.ReqHeaders); err != nil {
		return nil, fmt.Errorf("failed to unmarshal capture request headers for metric %d: %w", logID, err)
	}
	if err := json.Unmarshal(respHeaders, &capture.RespHeaders); err != nil {
		return nil, fmt.Errorf("failed to unmarshal capture response headers for metric %d: %w", logID, err)
	}
	return &storedCapture{
		capture:  capture,
		size:     size,
		created:  created,
		decrypts: encrypted == 1,
	}, nil
}

func (mp *metricsMonitor) getStoredCaptureByID(id int) (*storedCapture, error) {
	if !mp.enableCaptures || mp.captureDB == nil {
		return nil, sql.ErrNoRows
	}

	row := mp.captureDB.QueryRow(`
		SELECT id, req_path, req_headers, req_body, resp_headers, resp_body, size, encrypted, created_at
		FROM captures
		WHERE id = ?
	`, id)
	return mp.scanStoredCapture(row, id)
}

// getCaptureByID returns a capture by its ID, or nil if not found.
func (mp *metricsMonitor) getCaptureByID(id int) *ReqRespCapture {
	stored, err := mp.getStoredCaptureByID(id)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		mp.logger.Warnf("failed to load capture %d from sqlite: %v", id, err)
		return nil
	}
	return &stored.capture
}

func (mp *metricsMonitor) captureExists(id int) bool {
	if !mp.enableCaptures || mp.captureDB == nil {
		return false
	}

	var exists int
	err := mp.captureDB.QueryRow(`SELECT 1 FROM captures WHERE id = ? LIMIT 1`, id).Scan(&exists)
	return err == nil
}

// getMetrics returns a copy of the current metrics
func (mp *metricsMonitor) getMetrics() []TokenMetrics {
	mp.mu.RLock()
	result := make([]TokenMetrics, len(mp.metrics))
	copy(result, mp.metrics)
	mp.mu.RUnlock()

	for i := range result {
		result[i].HasCapture = result[i].HasCapture && mp.captureExists(result[i].ID)
	}
	return result
}

// getMetricsJSON returns metrics as JSON
func (mp *metricsMonitor) getMetricsJSON() ([]byte, error) {
	return json.Marshal(mp.getMetrics())
}

func (mp *metricsMonitor) clearActivity() error {
	if mp.captureDB != nil {
		if _, err := mp.captureDB.Exec(`DELETE FROM captures`); err != nil {
			return err
		}
		if _, err := mp.captureDB.Exec(`VACUUM`); err != nil {
			return err
		}
	}

	mp.mu.Lock()
	mp.metrics = nil
	mp.nextID = 0
	mp.mu.Unlock()
	event.Emit(ActivityClearedEvent{})
	return nil
}

func (mp *metricsMonitor) exportActivityDB(exportPath string) error {
	db, err := sql.Open("sqlite", exportPath)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if err := createCaptureSchema(db); err != nil {
		return err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS metrics (
			id INTEGER PRIMARY KEY,
			timestamp TEXT NOT NULL,
			model TEXT NOT NULL,
			cache_tokens INTEGER NOT NULL,
			input_tokens INTEGER NOT NULL,
			output_tokens INTEGER NOT NULL,
			prompt_per_second REAL NOT NULL,
			tokens_per_second REAL NOT NULL,
			duration_ms INTEGER NOT NULL,
			has_capture INTEGER NOT NULL
		)
	`); err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			tx.Rollback()
		}
	}()

	for _, metric := range mp.getMetrics() {
		hasCapture := 0
		if metric.HasCapture {
			hasCapture = 1
		}
		if _, err := tx.Exec(`
			INSERT INTO metrics
				(id, timestamp, model, cache_tokens, input_tokens, output_tokens, prompt_per_second, tokens_per_second, duration_ms, has_capture)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			metric.ID,
			metric.Timestamp.Format(time.RFC3339Nano),
			metric.Model,
			metric.CachedTokens,
			metric.InputTokens,
			metric.OutputTokens,
			metric.PromptPerSecond,
			metric.TokensPerSecond,
			metric.DurationMs,
			hasCapture,
		); err != nil {
			return err
		}
	}

	if mp.captureDB != nil {
		rows, err := mp.captureDB.Query(`
			SELECT id, req_path, req_headers, req_body, resp_headers, resp_body, size, encrypted, created_at
			FROM captures
			ORDER BY id
		`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			stored, err := mp.scanStoredCapture(rows, 0)
			if err != nil {
				return err
			}
			reqHeaders, err := json.Marshal(stored.capture.ReqHeaders)
			if err != nil {
				return err
			}
			respHeaders, err := json.Marshal(stored.capture.RespHeaders)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(`
				INSERT OR REPLACE INTO captures
					(id, req_path, req_headers, req_body, resp_headers, resp_body, size, encrypted, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)
			`,
				stored.capture.ID,
				[]byte(stored.capture.ReqPath),
				reqHeaders,
				stored.capture.ReqBody,
				respHeaders,
				stored.capture.RespBody,
				stored.size,
				stored.created,
			); err != nil {
				return err
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (mp *metricsMonitor) close() error {
	if mp.captureDB == nil {
		return nil
	}
	return mp.captureDB.Close()
}

// wrapHandler wraps the proxy handler to extract token metrics
// if wrapHandler returns an error it is safe to assume that no
// data was sent to the client
func (mp *metricsMonitor) wrapHandler(
	modelID string,
	writer gin.ResponseWriter,
	request *http.Request,
	next func(modelID string, w http.ResponseWriter, r *http.Request) error,
) error {
	// Capture request body and headers if captures enabled
	var reqBody []byte
	var reqHeaders map[string]string
	if mp.enableCaptures {
		if request.Body != nil {
			var err error
			reqBody, err = io.ReadAll(request.Body)
			if err != nil {
				return fmt.Errorf("failed to read request body for capture: %w", err)
			}
			request.Body.Close()
			request.Body = io.NopCloser(bytes.NewBuffer(reqBody))
		}
		reqHeaders = make(map[string]string)
		for key, values := range request.Header {
			if len(values) > 0 {
				reqHeaders[key] = values[0]
			}
		}
		redactHeaders(reqHeaders)
	}

	recorder := newBodyCopier(writer)

	// Filter Accept-Encoding to only include encodings we can decompress for metrics
	if ae := request.Header.Get("Accept-Encoding"); ae != "" {
		request.Header.Set("Accept-Encoding", filterAcceptEncoding(ae))
	}

	if err := next(modelID, recorder, request); err != nil {
		return err
	}

	// after this point we have to assume that data was sent to the client
	// and we can only log errors but not send them to clients

	if recorder.Status() != http.StatusOK {
		mp.logger.Warnf("metrics skipped, HTTP status=%d, path=%s", recorder.Status(), request.URL.Path)
		return nil
	}

	// Initialize default metrics - these will always be recorded
	tm := TokenMetrics{
		Timestamp:  time.Now(),
		Model:      modelID,
		DurationMs: int(time.Since(recorder.StartTime()).Milliseconds()),
	}

	body := recorder.body.Bytes()
	if len(body) == 0 {
		mp.logger.Warn("metrics: empty body, recording minimal metrics")
		mp.addMetrics(tm)
		return nil
	}

	// Decompress if needed
	if encoding := recorder.Header().Get("Content-Encoding"); encoding != "" {
		var err error
		body, err = decompressBody(body, encoding)
		if err != nil {
			mp.logger.Warnf("metrics: decompression failed: %v, path=%s, recording minimal metrics", err, request.URL.Path)
			mp.addMetrics(tm)
			return nil
		}
	}
	if strings.Contains(recorder.Header().Get("Content-Type"), "text/event-stream") {
		if parsed, err := processStreamingResponse(modelID, recorder.StartTime(), body); err != nil {
			mp.logger.Warnf("error processing streaming response: %v, path=%s, recording minimal metrics", err, request.URL.Path)
		} else {
			tm = parsed
		}
	} else {
		if gjson.ValidBytes(body) {
			parsed := gjson.ParseBytes(body)
			usage := parsed.Get("usage")
			timings := parsed.Get("timings")

			// extract timings for infill - response is an array, timings are in the last element
			// see #463
			if strings.HasPrefix(request.URL.Path, "/infill") {
				if arr := parsed.Array(); len(arr) > 0 {
					timings = arr[len(arr)-1].Get("timings")
				}
			}

			if usage.Exists() || timings.Exists() {
				if parsedMetrics, err := parseMetrics(modelID, recorder.StartTime(), usage, timings); err != nil {
					mp.logger.Warnf("error parsing metrics: %v, path=%s, recording minimal metrics", err, request.URL.Path)
				} else {
					tm = parsedMetrics
				}
			}
		} else {
			mp.logger.Warnf("metrics: invalid JSON in response body path=%s, recording minimal metrics", request.URL.Path)
		}
	}

	// Build capture if enabled and determine if it will be stored
	var capture *ReqRespCapture
	if mp.enableCaptures {
		respHeaders := make(map[string]string)
		for key, values := range recorder.Header() {
			if len(values) > 0 {
				respHeaders[key] = values[0]
			}
		}
		redactHeaders(respHeaders)
		delete(respHeaders, "Content-Encoding")
		capture = &ReqRespCapture{
			ReqPath:     request.URL.Path,
			ReqHeaders:  reqHeaders,
			ReqBody:     reqBody,
			RespHeaders: respHeaders,
			RespBody:    body,
		}
		tm.HasCapture = mp.enableCaptures && mp.captureDB != nil
	}

	metricID := mp.addMetrics(tm)

	// Store capture if enabled
	if capture != nil {
		capture.ID = metricID
		mp.addCapture(*capture)
	}

	return nil
}

func processStreamingResponse(modelID string, start time.Time, body []byte) (TokenMetrics, error) {
	// Iterate **backwards** through the body looking for the data payload with
	// usage data. This avoids allocating a slice of all lines via bytes.Split.

	// Start from the end of the body and scan backwards for newlines
	pos := len(body)
	for pos > 0 {
		// Find the previous newline (or start of body)
		lineStart := bytes.LastIndexByte(body[:pos], '\n')
		if lineStart == -1 {
			lineStart = 0
		} else {
			lineStart++ // Move past the newline
		}

		line := bytes.TrimSpace(body[lineStart:pos])
		pos = lineStart - 1 // Move position before the newline for next iteration

		if len(line) == 0 {
			continue
		}

		// SSE payload always follows "data:"
		prefix := []byte("data:")
		if !bytes.HasPrefix(line, prefix) {
			continue
		}
		data := bytes.TrimSpace(line[len(prefix):])

		if len(data) == 0 {
			continue
		}

		if bytes.Equal(data, []byte("[DONE]")) {
			// [DONE] line itself contains nothing of interest.
			continue
		}

		if gjson.ValidBytes(data) {
			parsed := gjson.ParseBytes(data)
			usage := parsed.Get("usage")
			timings := parsed.Get("timings")

			if usage.Exists() || timings.Exists() {
				return parseMetrics(modelID, start, usage, timings)
			}
		}
	}

	return TokenMetrics{}, fmt.Errorf("no valid JSON data found in stream")
}

func parseMetrics(modelID string, start time.Time, usage, timings gjson.Result) (TokenMetrics, error) {
	// default values
	cachedTokens := -1 // unknown or missing data
	outputTokens := 0
	inputTokens := 0

	// timings data
	tokensPerSecond := -1.0
	promptPerSecond := -1.0
	durationMs := int(time.Since(start).Milliseconds())

	if usage.Exists() {
		if pt := usage.Get("prompt_tokens"); pt.Exists() {
			// v1/chat/completions
			inputTokens = int(pt.Int())
		} else if it := usage.Get("input_tokens"); it.Exists() {
			// v1/messages
			inputTokens = int(it.Int())
		}

		if ct := usage.Get("completion_tokens"); ct.Exists() {
			// v1/chat/completions
			outputTokens = int(ct.Int())
		} else if ot := usage.Get("output_tokens"); ot.Exists() {
			outputTokens = int(ot.Int())
		}

		if ct := usage.Get("cache_read_input_tokens"); ct.Exists() {
			cachedTokens = int(ct.Int())
		}
	}

	// use llama-server's timing data for tok/sec and duration as it is more accurate
	if timings.Exists() {
		inputTokens = int(timings.Get("prompt_n").Int())
		outputTokens = int(timings.Get("predicted_n").Int())
		promptPerSecond = timings.Get("prompt_per_second").Float()
		tokensPerSecond = timings.Get("predicted_per_second").Float()
		durationMs = int(timings.Get("prompt_ms").Float() + timings.Get("predicted_ms").Float())

		if cachedValue := timings.Get("cache_n"); cachedValue.Exists() {
			cachedTokens = int(cachedValue.Int())
		}
	}

	return TokenMetrics{
		Timestamp:       time.Now(),
		Model:           modelID,
		CachedTokens:    cachedTokens,
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		PromptPerSecond: promptPerSecond,
		TokensPerSecond: tokensPerSecond,
		DurationMs:      durationMs,
	}, nil
}

// decompressBody decompresses the body based on Content-Encoding header
func decompressBody(body []byte, encoding string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return io.ReadAll(reader)
	case "deflate":
		reader := flate.NewReader(bytes.NewReader(body))
		defer reader.Close()
		return io.ReadAll(reader)
	default:
		return body, nil // Return as-is for unknown/no encoding
	}
}

// responseBodyCopier records the response body and writes to the original response writer
// while also capturing it in a buffer for later processing
type responseBodyCopier struct {
	gin.ResponseWriter
	body  *bytes.Buffer
	tee   io.Writer
	start time.Time
}

func newBodyCopier(w gin.ResponseWriter) *responseBodyCopier {
	bodyBuffer := &bytes.Buffer{}
	return &responseBodyCopier{
		ResponseWriter: w,
		body:           bodyBuffer,
		tee:            io.MultiWriter(w, bodyBuffer),
	}
}

func (w *responseBodyCopier) Write(b []byte) (int, error) {
	if w.start.IsZero() {
		w.start = time.Now()
	}

	// Single write operation that writes to both the response and buffer
	return w.tee.Write(b)
}

func (w *responseBodyCopier) WriteHeader(statusCode int) {
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseBodyCopier) Header() http.Header {
	return w.ResponseWriter.Header()
}

func (w *responseBodyCopier) StartTime() time.Time {
	return w.start
}

// sensitiveHeaders lists headers that should be redacted in captures
var sensitiveHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"cookie":              true,
	"set-cookie":          true,
	"x-api-key":           true,
}

// redactHeaders replaces sensitive header values in-place with "[REDACTED]"
func redactHeaders(headers map[string]string) {
	for key := range headers {
		if sensitiveHeaders[strings.ToLower(key)] {
			headers[key] = "[REDACTED]"
		}
	}
}

// filterAcceptEncoding filters the Accept-Encoding header to only include
// encodings we can decompress (gzip, deflate). This respects the client's
// preferences while ensuring we can parse response bodies for metrics.
func filterAcceptEncoding(acceptEncoding string) string {
	if acceptEncoding == "" {
		return ""
	}

	supported := map[string]bool{"gzip": true, "deflate": true}
	var filtered []string

	for _, part := range strings.Split(acceptEncoding, ",") {
		// Parse encoding and optional quality value (e.g., "gzip;q=1.0")
		encoding := strings.TrimSpace(strings.Split(part, ";")[0])
		if supported[strings.ToLower(encoding)] {
			filtered = append(filtered, strings.TrimSpace(part))
		}
	}

	return strings.Join(filtered, ", ")
}
