package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mostlygeek/llama-swap/event"
	"github.com/mostlygeek/llama-swap/proxy/config"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	PROFILE_SPLIT_CHAR = ":"
)

type proxyCtxKey string

type InflightCounter struct {
	mu    sync.Mutex
	total int
}

func newInflightCounter() *InflightCounter {
	return &InflightCounter{}
}

func (ic *InflightCounter) Current() int {
	ic.mu.Lock()
	total := ic.total
	ic.mu.Unlock()
	return total
}

func (ic *InflightCounter) Increment() int {
	ic.mu.Lock()
	ic.total++
	total := ic.total
	ic.mu.Unlock()
	return total
}

func (ic *InflightCounter) Decrement() int {
	ic.mu.Lock()
	if ic.total > 0 {
		ic.total--
	}
	total := ic.total
	ic.mu.Unlock()
	return total
}

type ProxyManager struct {
	sync.Mutex

	config    config.Config
	ginEngine *gin.Engine

	// logging
	proxyLogger    *LogMonitor
	upstreamLogger *LogMonitor
	muxLogger      *LogMonitor

	metricsMonitor       *metricsMonitor
	sessionModelSettings *sessionModelSettingsStore

	processGroups map[string]*ProcessGroup

	inFlightCounter *InflightCounter

	// shutdown signaling
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc

	// version info
	buildDate string
	commit    string
	version   string

	// peer proxy see: #296, #433
	peerProxy *PeerProxy
}

func New(proxyConfig config.Config) *ProxyManager {
	// set up loggers

	var muxLogger, upstreamLogger, proxyLogger *LogMonitor
	switch proxyConfig.LogToStdout {
	case config.LogToStdoutNone:
		muxLogger = NewLogMonitorWriter(io.Discard)
		upstreamLogger = NewLogMonitorWriter(io.Discard)
		proxyLogger = NewLogMonitorWriter(io.Discard)
	case config.LogToStdoutBoth:
		muxLogger = NewLogMonitorWriter(os.Stdout)
		upstreamLogger = NewLogMonitorWriter(muxLogger)
		proxyLogger = NewLogMonitorWriter(muxLogger)
	case config.LogToStdoutUpstream:
		muxLogger = NewLogMonitorWriter(os.Stdout)
		upstreamLogger = NewLogMonitorWriter(muxLogger)
		proxyLogger = NewLogMonitorWriter(io.Discard)
	default:
		// same as config.LogToStdoutProxy
		// helpful because some old tests create a config.Config directly and it
		// may not have LogToStdout set explicitly
		muxLogger = NewLogMonitorWriter(os.Stdout)
		upstreamLogger = NewLogMonitorWriter(io.Discard)
		proxyLogger = NewLogMonitorWriter(muxLogger)
	}

	if proxyConfig.LogRequests {
		proxyLogger.Warn("LogRequests configuration is deprecated. Use logLevel instead.")
	}

	switch strings.ToLower(strings.TrimSpace(proxyConfig.LogLevel)) {
	case "debug":
		proxyLogger.SetLogLevel(LevelDebug)
		upstreamLogger.SetLogLevel(LevelDebug)
	case "info":
		proxyLogger.SetLogLevel(LevelInfo)
		upstreamLogger.SetLogLevel(LevelInfo)
	case "warn":
		proxyLogger.SetLogLevel(LevelWarn)
		upstreamLogger.SetLogLevel(LevelWarn)
	case "error":
		proxyLogger.SetLogLevel(LevelError)
		upstreamLogger.SetLogLevel(LevelError)
	default:
		proxyLogger.SetLogLevel(LevelInfo)
		upstreamLogger.SetLogLevel(LevelInfo)
	}

	// see: https://go.dev/src/time/format.go
	timeFormats := map[string]string{
		"ansic":       time.ANSIC,
		"unixdate":    time.UnixDate,
		"rubydate":    time.RubyDate,
		"rfc822":      time.RFC822,
		"rfc822z":     time.RFC822Z,
		"rfc850":      time.RFC850,
		"rfc1123":     time.RFC1123,
		"rfc1123z":    time.RFC1123Z,
		"rfc3339":     time.RFC3339,
		"rfc3339nano": time.RFC3339Nano,
		"kitchen":     time.Kitchen,
		"stamp":       time.Stamp,
		"stampmilli":  time.StampMilli,
		"stampmicro":  time.StampMicro,
		"stampnano":   time.StampNano,
	}

	if timeFormat, ok := timeFormats[strings.ToLower(strings.TrimSpace(proxyConfig.LogTimeFormat))]; ok {
		proxyLogger.SetLogTimeFormat(timeFormat)
		upstreamLogger.SetLogTimeFormat(timeFormat)
	}

	shutdownCtx, shutdownCancel := context.WithCancel(context.Background())

	var maxMetrics int
	if proxyConfig.MetricsMaxInMemory <= 0 {
		maxMetrics = 1000 // Default fallback
	} else {
		maxMetrics = proxyConfig.MetricsMaxInMemory
	}
	captureKey := ""
	if len(proxyConfig.RequiredAPIKeys) > 0 {
		captureKey = proxyConfig.RequiredAPIKeys[0]
	}

	peerProxy, err := NewPeerProxy(proxyConfig.Peers, proxyLogger)
	if err != nil {
		proxyLogger.Errorf("Disabling Peering. Failed to create proxy peers: %v", err)
		peerProxy = nil
	}

	sessionModelSettings, err := newSessionModelSettingsStore(proxyConfig.CaptureDBPath)
	if err != nil {
		proxyLogger.Warnf("failed to initialize session model settings sqlite database %q: %v; model config editing disabled", proxyConfig.CaptureDBPath, err)
	}

	// Merge any models that were created at runtime via the Duplicate UI
	// before the process groups are built so they participate in normal
	// per-group lifecycle (and the YAML-defined entries always take
	// precedence on id collisions).
	mergeUserAddedModels(&proxyConfig, sessionModelSettings, proxyLogger)

	pm := &ProxyManager{
		config:    proxyConfig,
		ginEngine: gin.New(),

		proxyLogger:    proxyLogger,
		muxLogger:      muxLogger,
		upstreamLogger: upstreamLogger,

		metricsMonitor:       newMetricsMonitor(proxyLogger, maxMetrics, proxyConfig.CaptureBuffer, proxyConfig.CaptureDBPath, captureKey),
		sessionModelSettings: sessionModelSettings,

		processGroups: make(map[string]*ProcessGroup),

		inFlightCounter: newInflightCounter(),

		shutdownCtx:    shutdownCtx,
		shutdownCancel: shutdownCancel,

		buildDate: "unknown",
		commit:    "abcd1234",
		version:   "0",

		peerProxy: peerProxy,
	}

	// create the process groups
	for groupID := range proxyConfig.Groups {
		processGroup := NewProcessGroup(groupID, proxyConfig, proxyLogger, upstreamLogger, pm.effectiveModelConfig)
		pm.processGroups[groupID] = processGroup
	}

	pm.setupGinEngine()

	// run any startup hooks
	if len(proxyConfig.Hooks.OnStartup.Preload) > 0 {
		// do it in the background, don't block startup -- not sure if good idea yet
		go func() {
			discardWriter := &DiscardWriter{}
			for _, preloadModelName := range proxyConfig.Hooks.OnStartup.Preload {
				modelID, ok := proxyConfig.RealModelName(preloadModelName)

				if !ok {
					proxyLogger.Warnf("Preload model %s not found in config", preloadModelName)
					continue
				}

				proxyLogger.Infof("Preloading model: %s", modelID)
				processGroup, err := pm.swapProcessGroup(modelID)

				if err != nil {
					event.Emit(ModelPreloadedEvent{
						ModelName: modelID,
						Success:   false,
					})
					proxyLogger.Errorf("Failed to preload model %s: %v", modelID, err)
					continue
				} else {
					req, _ := http.NewRequest("GET", "/", nil)
					processGroup.ProxyRequest(modelID, discardWriter, req)
					event.Emit(ModelPreloadedEvent{
						ModelName: modelID,
						Success:   true,
					})
				}
			}
		}()
	}

	return pm
}

func (pm *ProxyManager) setupGinEngine() {

	pm.ginEngine.Use(func(c *gin.Context) {

		// don't log the Wake on Lan proxy health check
		if c.Request.URL.Path == "/wol-health" {
			c.Next()
			return
		}

		// Start timer
		start := time.Now()

		// capture these because /upstream/:model rewrites them in c.Next()
		clientIP := c.ClientIP()
		method := c.Request.Method
		path := c.Request.URL.Path

		// Process request
		c.Next()

		// Stop timer
		duration := time.Since(start)

		statusCode := c.Writer.Status()
		bodySize := c.Writer.Size()

		pm.proxyLogger.Infof("Request %s \"%s %s %s\" %d %d \"%s\" %v",
			clientIP,
			method,
			path,
			c.Request.Proto,
			statusCode,
			bodySize,
			c.Request.UserAgent(),
			duration,
		)
	})

	// see: issue: #81, #77 and #42 for CORS issues
	// respond with permissive OPTIONS for any endpoint
	pm.ginEngine.Use(func(c *gin.Context) {
		if c.Request.Method == "OPTIONS" {
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")

			// allow whatever the client requested by default
			if headers := c.Request.Header.Get("Access-Control-Request-Headers"); headers != "" {
				sanitized := SanitizeAccessControlRequestHeaderValues(headers)
				c.Header("Access-Control-Allow-Headers", sanitized)
			} else {
				c.Header(
					"Access-Control-Allow-Headers",
					"Content-Type, Authorization, Accept, X-Requested-With",
				)
			}
			c.Header("Access-Control-Max-Age", "86400")
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// Set up routes using the Gin engine
	// Protected routes use pm.apiKeyAuth() middleware
	pm.ginEngine.POST("/v1/chat/completions", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyInferenceHandler)
	pm.ginEngine.POST("/v1/responses", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyInferenceHandler)
	// Support legacy /v1/completions api, see issue #12
	pm.ginEngine.POST("/v1/completions", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyInferenceHandler)
	// Support anthropic /v1/messages (added https://github.com/ggml-org/llama.cpp/pull/17570)
	pm.ginEngine.POST("/v1/messages", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyInferenceHandler)
	// Support anthropic count_tokens API (Also added in the above PR)
	pm.ginEngine.POST("/v1/messages/count_tokens", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyInferenceHandler)

	// Support embeddings and reranking
	pm.ginEngine.POST("/v1/embeddings", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyInferenceHandler)

	// llama-server's /reranking endpoint + aliases
	pm.ginEngine.POST("/reranking", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyInferenceHandler)
	pm.ginEngine.POST("/rerank", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyInferenceHandler)
	pm.ginEngine.POST("/v1/rerank", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyInferenceHandler)
	pm.ginEngine.POST("/v1/reranking", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyInferenceHandler)

	// llama-server's /infill endpoint for code infilling
	pm.ginEngine.POST("/infill", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyInferenceHandler)

	// llama-server's /completion endpoint
	pm.ginEngine.POST("/completion", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyInferenceHandler)

	// Support audio/speech endpoint
	pm.ginEngine.POST("/v1/audio/speech", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyInferenceHandler)
	pm.ginEngine.POST("/v1/audio/voices", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyInferenceHandler)
	pm.ginEngine.GET("/v1/audio/voices", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyGETModelHandler)
	pm.ginEngine.POST("/v1/audio/transcriptions", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyOAIPostFormHandler)
	pm.ginEngine.POST("/v1/images/generations", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyInferenceHandler)
	pm.ginEngine.POST("/v1/images/edits", pm.apiKeyAuth(true), pm.trackInflight(), pm.proxyOAIPostFormHandler)

	pm.ginEngine.GET("/v1/models", pm.apiKeyAuth(true), pm.listModelsHandler)

	// in proxymanager_loghandlers.go
	pm.ginEngine.GET("/logs", pm.apiKeyAuth(false), pm.sendLogsHandlers)
	pm.ginEngine.GET("/logs/stream", pm.apiKeyAuth(false), pm.streamLogsHandler)
	pm.ginEngine.GET("/logs/stream/*logMonitorID", pm.apiKeyAuth(false), pm.streamLogsHandler)

	/**
	 * User Interface Endpoints
	 */
	pm.ginEngine.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/ui")
	})

	pm.ginEngine.GET("/upstream", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/ui/models")
	})
	pm.ginEngine.Any("/upstream/*upstreamPath", pm.apiKeyAuth(false), pm.trackInflight(), pm.proxyToUpstream)
	pm.ginEngine.GET("/unload", pm.apiKeyAuth(false), pm.unloadAllModelsHandler)
	pm.ginEngine.GET("/running", pm.apiKeyAuth(false), pm.listRunningProcessesHandler)
	pm.ginEngine.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	// see cmd/wol-proxy/wol-proxy.go, not logged
	pm.ginEngine.GET("/wol-health", func(c *gin.Context) {
		c.String(http.StatusOK, "OK")
	})

	pm.ginEngine.GET("/favicon.ico", func(c *gin.Context) {
		if data, err := reactStaticFS.ReadFile("ui_dist/favicon.ico"); err == nil {
			c.Data(http.StatusOK, "image/x-icon", data)
		} else {
			c.String(http.StatusInternalServerError, err.Error())
		}
	})

	reactFS, err := GetReactFS()
	if err != nil {
		pm.proxyLogger.Errorf("Failed to load React filesystem: %v", err)
	} else {
		// Serve files with compression support under /ui/*
		// This handler checks for pre-compressed .br and .gz files
		pm.ginEngine.GET("/ui/*filepath", func(c *gin.Context) {
			filepath := strings.TrimPrefix(c.Param("filepath"), "/")
			// Default to index.html for directory-like paths
			if filepath == "" {
				filepath = "index.html"
			}

			ServeCompressedFile(reactFS, c.Writer, c.Request, filepath)
		})

		// Serve SPA for UI under /ui/* - fallback to index.html for client-side routing
		pm.ginEngine.NoRoute(func(c *gin.Context) {
			if !strings.HasPrefix(c.Request.URL.Path, "/ui") {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}

			// Check if this looks like a file request (has extension)
			path := c.Request.URL.Path
			if strings.Contains(path, ".") && !strings.HasSuffix(path, "/") {
				// This was likely a file request that wasn't found
				c.AbortWithStatus(http.StatusNotFound)
				return
			}

			// Serve index.html for SPA routing
			ServeCompressedFile(reactFS, c.Writer, c.Request, "index.html")
		})
	}

	// see: proxymanager_api.go
	// add API handler functions
	addApiHandlers(pm)

	// Disable console color for testing
	gin.DisableConsoleColor()
}

func (pm *ProxyManager) trackInflight() gin.HandlerFunc {
	return func(c *gin.Context) {
		event.Emit(InFlightRequestsEvent{Total: pm.inFlightCounter.Increment()})
		defer event.Emit(InFlightRequestsEvent{Total: pm.inFlightCounter.Decrement()})
		c.Next()
	}
}

// ServeHTTP implements http.Handler interface
func (pm *ProxyManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	pm.ginEngine.ServeHTTP(w, r)
}

// StopProcesses acquires a lock and stops all running upstream processes.
// This is the public method safe for concurrent calls.
// Unlike Shutdown, this method only stops the processes but doesn't perform
// a complete shutdown, allowing for process replacement without full termination.
func (pm *ProxyManager) StopProcesses(strategy StopStrategy) {
	pm.Lock()
	defer pm.Unlock()

	// stop Processes in parallel
	var wg sync.WaitGroup
	for _, processGroup := range pm.processGroups {
		wg.Add(1)
		go func(processGroup *ProcessGroup) {
			defer wg.Done()
			processGroup.StopProcesses(strategy)
		}(processGroup)
	}

	wg.Wait()
}

// Shutdown stops all processes managed by this ProxyManager
func (pm *ProxyManager) Shutdown() {
	pm.Lock()
	defer pm.Unlock()

	pm.proxyLogger.Debug("Shutdown() called in proxy manager")

	var wg sync.WaitGroup
	// Send shutdown signal to all process in groups
	for _, processGroup := range pm.processGroups {
		wg.Add(1)
		go func(processGroup *ProcessGroup) {
			defer wg.Done()
			processGroup.Shutdown()
		}(processGroup)
	}
	wg.Wait()
	if pm.metricsMonitor != nil {
		if err := pm.metricsMonitor.close(); err != nil {
			pm.proxyLogger.Warnf("error closing capture sqlite database: %v", err)
		}
	}
	if pm.sessionModelSettings != nil {
		if err := pm.sessionModelSettings.close(); err != nil {
			pm.proxyLogger.Warnf("error closing session model settings sqlite database: %v", err)
		}
	}
	pm.shutdownCancel()
}

func (pm *ProxyManager) swapProcessGroup(realModelName string) (*ProcessGroup, error) {
	processGroup := pm.findGroupByModelName(realModelName)
	if processGroup == nil {
		return nil, fmt.Errorf("could not find process group for model %s", realModelName)
	}

	if processGroup.exclusive {
		pm.proxyLogger.Debugf("Exclusive mode for group %s, stopping other process groups", processGroup.id)
		for groupId, otherGroup := range pm.processGroups {
			if groupId != processGroup.id && !otherGroup.persistent {
				otherGroup.StopProcesses(StopWaitForInflightRequest)
			}
		}
	}

	return processGroup, nil
}

func (pm *ProxyManager) listModelsHandler(c *gin.Context) {
	data := make([]gin.H, 0, len(pm.config.Models))
	codexModelCatalogRequested := isCodexModelCatalogRequest(c.Request)
	var codexModels []gin.H
	if codexModelCatalogRequested {
		codexModels = make([]gin.H, 0, len(pm.config.Models))
	}
	createdTime := time.Now().Unix()

	newRecord := func(modelId string, modelConfig config.ModelConfig) gin.H {
		record := gin.H{
			"id":       modelId,
			"object":   "model",
			"created":  createdTime,
			"owned_by": "llama-swap",
		}

		if name := strings.TrimSpace(modelConfig.Name); name != "" {
			record["name"] = name
		}
		if desc := strings.TrimSpace(modelConfig.Description); desc != "" {
			record["description"] = desc
		}

		// Add metadata if present
		if len(modelConfig.Metadata) > 0 {
			record["meta"] = gin.H{
				"llamaswap": modelConfig.Metadata,
			}
		}
		return record
	}
	newCodexRecord := func(modelId string, modelConfig config.ModelConfig, priority int) gin.H {
		displayName := strings.TrimSpace(modelConfig.Name)
		if displayName == "" {
			displayName = modelId
		}
		description := strings.TrimSpace(modelConfig.Description)
		if description == "" {
			description = "llama-swap model"
		}
		contextWindow := codexModelContextWindow(modelConfig)

		return gin.H{
			"slug":                         modelId,
			"display_name":                 displayName,
			"description":                  description,
			"default_reasoning_level":      nil,
			"supported_reasoning_levels":   []gin.H{},
			"shell_type":                   "shell_command",
			"visibility":                   "list",
			"supported_in_api":             true,
			"priority":                     priority,
			"additional_speed_tiers":       []string{},
			"availability_nux":             nil,
			"upgrade":                      nil,
			"base_instructions":            defaultCodexBaseInstructions,
			"model_messages":               nil,
			"supports_reasoning_summaries": false,
			"default_reasoning_summary":    "auto",
			"support_verbosity":            false,
			"default_verbosity":            nil,
			"apply_patch_tool_type":        "function",
			"web_search_tool_type":         "text",
			"truncation_policy": gin.H{
				"mode":  "bytes",
				"limit": 10000,
			},
			"supports_parallel_tool_calls":     true,
			"supports_image_detail_original":   false,
			"context_window":                   contextWindow,
			"max_context_window":               contextWindow,
			"auto_compact_token_limit":         nil,
			"effective_context_window_percent": 95,
			"experimental_supported_tools":     []string{},
			"input_modalities":                 []string{"text", "image"},
			"supports_search_tool":             false,
		}
	}

	priority := 0
	for id, modelConfig := range pm.config.Models {
		if modelConfig.Unlisted {
			continue
		}

		data = append(data, newRecord(id, modelConfig))
		if codexModelCatalogRequested {
			codexModels = append(codexModels, newCodexRecord(id, modelConfig, priority))
			priority++
		}

		// Include aliases
		if pm.config.IncludeAliasesInList {
			for _, alias := range modelConfig.Aliases {
				if alias := strings.TrimSpace(alias); alias != "" {
					data = append(data, newRecord(alias, modelConfig))
					if codexModelCatalogRequested {
						codexModels = append(codexModels, newCodexRecord(alias, modelConfig, priority))
						priority++
					}
				}
			}
		}
	}

	if pm.peerProxy != nil {
		for peerID, peer := range pm.peerProxy.ListPeers() {
			// add peer models
			for _, modelID := range peer.Models {
				// Skip unlisted models if not showing them
				record := newRecord(modelID, config.ModelConfig{
					Name: fmt.Sprintf("%s: %s", peerID, modelID),
					Metadata: map[string]any{
						"peerID": peerID,
					},
				})

				data = append(data, record)
				if codexModelCatalogRequested {
					codexModels = append(codexModels, newCodexRecord(modelID, config.ModelConfig{
						Name:        fmt.Sprintf("%s: %s", peerID, modelID),
						Description: "llama-swap peer model",
					}, priority))
					priority++
				}
			}
		}
	}

	// Sort by the "id" key
	sort.Slice(data, func(i, j int) bool {
		si, _ := data[i]["id"].(string)
		sj, _ := data[j]["id"].(string)
		return si < sj
	})
	if codexModelCatalogRequested {
		sort.Slice(codexModels, func(i, j int) bool {
			si, _ := codexModels[i]["slug"].(string)
			sj, _ := codexModels[j]["slug"].(string)
			return si < sj
		})
	}

	// Set CORS headers if origin exists
	if origin := c.GetHeader("Origin"); origin != "" {
		c.Header("Access-Control-Allow-Origin", origin)
	}

	response := gin.H{
		"object": "list",
		"data":   data,
	}
	if codexModelCatalogRequested {
		response["models"] = codexModels
	}

	// Use gin's JSON method which handles content-type and encoding
	c.JSON(http.StatusOK, response)
}

const defaultCodexBaseInstructions = "You are Codex, a coding agent. Help the user complete software engineering tasks clearly and safely."

func codexModelContextWindow(modelConfig config.ModelConfig) int64 {
	if contextWindow, ok := modelConfig.Metadata["context_window"]; ok {
		if parsed, ok := parseInt64Like(contextWindow); ok && parsed > 0 {
			return parsed
		}
	}
	if contextWindow, ok := modelConfig.Metadata["context"]; ok {
		if parsed, ok := parseInt64Like(contextWindow); ok && parsed > 0 {
			return parsed
		}
	}

	args, err := modelConfig.SanitizedCommand()
	if err == nil {
		for i, arg := range args {
			if (arg == "-c" || arg == "--ctx-size") && i+1 < len(args) {
				if parsed, err := strconv.ParseInt(args[i+1], 10, 64); err == nil && parsed > 0 {
					return parsed
				}
			}
			if strings.HasPrefix(arg, "--ctx-size=") {
				if parsed, err := strconv.ParseInt(strings.TrimPrefix(arg, "--ctx-size="), 10, 64); err == nil && parsed > 0 {
					return parsed
				}
			}
		}
	}

	return 272000
}

func parseInt64Like(value any) (int64, bool) {
	switch v := value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case int32:
		return int64(v), true
	case float64:
		return int64(v), true
	case float32:
		return int64(v), true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func isCodexModelCatalogRequest(r *http.Request) bool {
	return r.URL.Query().Has("client_version") || isCodexRequest(r)
}

func isCodexRequest(r *http.Request) bool {
	for name := range r.Header {
		if strings.HasPrefix(strings.ToLower(name), "x-codex-") {
			return true
		}
	}

	userAgent := strings.ToLower(r.UserAgent())
	return strings.Contains(userAgent, "codex-tui/") ||
		strings.Contains(userAgent, "codex-cli/") ||
		strings.Contains(userAgent, "codex-exec/") ||
		strings.Contains(userAgent, "codex/")
}

// findModelInPath searches for a valid model name in a path with slashes.
// It iteratively builds up path segments until it finds a matching model.
// Returns: (searchModelName, realModelName, remainingPath, found)
// Example: "/author/model/endpoint" with model "author/model" -> ("author/model", "author/model", "/endpoint", true)
func (pm *ProxyManager) findModelInPath(path string) (searchName string, realName string, remainingPath string, found bool) {
	parts := strings.Split(strings.TrimSpace(path), "/")
	searchModelName := ""

	for i, part := range parts {
		if part == "" {
			continue
		}

		if searchModelName == "" {
			searchModelName = part
		} else {
			searchModelName = searchModelName + "/" + part
		}

		if modelID, ok := pm.config.RealModelName(searchModelName); ok {
			return searchModelName, modelID, "/" + strings.Join(parts[i+1:], "/"), true
		}
	}

	return "", "", "", false
}

func (pm *ProxyManager) proxyToUpstream(c *gin.Context) {
	upstreamPath := c.Param("upstreamPath")

	searchModelName, modelID, remainingPath, modelFound := pm.findModelInPath(upstreamPath)

	if !modelFound {
		pm.sendErrorResponse(c, http.StatusBadRequest, "model id required in path")
		return
	}

	// Redirect /upstream/modelname to /upstream/modelname/ for URL consistency.
	// This ensures relative URLs in upstream responses resolve correctly and
	// provides canonical URL form. Uses 308 for POST/PUT/etc to preserve the
	// HTTP method (301 would downgrade to GET).
	if remainingPath == "/" && !strings.HasSuffix(upstreamPath, "/") {
		newPath := "/upstream/" + searchModelName + "/"
		if c.Request.URL.RawQuery != "" {
			newPath += "?" + c.Request.URL.RawQuery
		}
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
			c.Redirect(http.StatusMovedPermanently, newPath)
		} else {
			c.Redirect(http.StatusPermanentRedirect, newPath)
		}
		return
	}

	processGroup, err := pm.swapProcessGroup(modelID)
	if err != nil {
		pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error swapping process group: %s", err.Error()))
		return
	}

	// rewrite the path
	originalPath := c.Request.URL.Path
	c.Request.URL.Path = remainingPath

	// attempt to record metrics if it is a POST request
	if pm.metricsMonitor != nil && c.Request.Method == "POST" {
		if err := pm.metricsMonitor.wrapHandler(modelID, c.Writer, c.Request, processGroup.ProxyRequest); err != nil {
			pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error proxying metrics wrapped request: %s", err.Error()))
			pm.proxyLogger.Errorf("Error proxying wrapped upstream request for model %s, path=%s", modelID, originalPath)
			return
		}
	} else {
		if err := processGroup.ProxyRequest(modelID, c.Writer, c.Request); err != nil {
			pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error proxying request: %s", err.Error()))
			pm.proxyLogger.Errorf("Error proxying upstream request for model %s, path=%s", modelID, originalPath)
			return
		}
	}
}

func (pm *ProxyManager) proxyInferenceHandler(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		pm.sendErrorResponse(c, http.StatusBadRequest, "could not ready request body")
		return
	}

	requestedModel := gjson.GetBytes(bodyBytes, "model").String()
	if requestedModel == "" {
		pm.sendErrorResponse(c, http.StatusBadRequest, "missing or invalid 'model' key")
		return
	}

	// Look for a matching local model first
	var nextHandler func(modelID string, w http.ResponseWriter, r *http.Request) error

	modelID, found := pm.config.RealModelName(requestedModel)
	if found {
		processGroup, err := pm.swapProcessGroup(modelID)
		if err != nil {
			pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error swapping process group: %s", err.Error()))
			return
		}

		// issue #69 allow custom model names to be sent to upstream
		useModelName := pm.config.Models[modelID].UseModelName
		if useModelName != "" {
			bodyBytes, err = sjson.SetBytes(bodyBytes, "model", useModelName)
			if err != nil {
				pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error rewriting model name in JSON: %s", err.Error()))
				return
			}
		}

		// issue #174 strip parameters from the JSON body
		stripParams, err := pm.config.Models[modelID].Filters.SanitizedStripParams()
		if err != nil { // just log it and continue
			pm.proxyLogger.Errorf("Error sanitizing strip params string: %s, %s", pm.config.Models[modelID].Filters.StripParams, err.Error())
		} else {
			for _, param := range stripParams {
				pm.proxyLogger.Debugf("<%s> stripping param: %s", modelID, param)
				bodyBytes, err = sjson.DeleteBytes(bodyBytes, param)
				if err != nil {
					pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error deleting parameter %s from request", param))
					return
				}
			}
		}

		// issue #453 set/override parameters in the JSON body
		setParams, setParamKeys := pm.config.Models[modelID].Filters.SanitizedSetParams()
		for _, key := range setParamKeys {
			pm.proxyLogger.Debugf("<%s> setting param: %s", modelID, key)
			bodyBytes, err = sjson.SetBytes(bodyBytes, key, setParams[key])
			if err != nil {
				pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error setting parameter %s in request", key))
				return
			}
		}

		// setParamsByID: set params based on the requested model ID (runs after setParams, can override it)
		setParamsByIDParams, setParamsByIDKeys := pm.config.Models[modelID].Filters.SanitizedSetParamsByID(requestedModel)
		for _, key := range setParamsByIDKeys {
			pm.proxyLogger.Debugf("<%s> setting param by id: %s", requestedModel, key)
			bodyBytes, err = sjson.SetBytes(bodyBytes, key, setParamsByIDParams[key])
			if err != nil {
				pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error setting parameter %s in request", key))
				return
			}
		}

		if c.Request.URL.Path == "/v1/responses" && isCodexRequest(c.Request) {
			var changed bool
			bodyBytes, changed, err = normalizeResponsesRequestForLlamaCpp(bodyBytes)
			if err != nil {
				pm.sendErrorResponse(c, http.StatusBadRequest, fmt.Sprintf("error normalizing responses request: %s", err.Error()))
				return
			}
			if changed {
				pm.proxyLogger.Debugf("<%s> normalized Responses API request for llama.cpp compatibility", requestedModel)
			}
		}

		pm.proxyLogger.Debugf("ProxyManager using local Process for model: %s", requestedModel)
		nextHandler = processGroup.ProxyRequest
	} else if pm.peerProxy != nil && pm.peerProxy.HasPeerModel(requestedModel) {
		pm.proxyLogger.Debugf("ProxyManager using ProxyPeer for model: %s", requestedModel)
		modelID = requestedModel

		// issue #453 apply filters for peer requests
		peerFilters := pm.peerProxy.GetPeerFilters(requestedModel)

		// Apply stripParams - remove specified parameters from request
		stripParams := peerFilters.SanitizedStripParams()
		for _, param := range stripParams {
			pm.proxyLogger.Debugf("<%s> stripping param: %s", requestedModel, param)
			bodyBytes, err = sjson.DeleteBytes(bodyBytes, param)
			if err != nil {
				pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error stripping parameter %s from request", param))
				return
			}
		}

		// Apply setParams - set/override specified parameters in request
		setParams, setParamKeys := peerFilters.SanitizedSetParams()
		for _, key := range setParamKeys {
			pm.proxyLogger.Debugf("<%s> setting param: %s", requestedModel, key)
			bodyBytes, err = sjson.SetBytes(bodyBytes, key, setParams[key])
			if err != nil {
				pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error setting parameter %s in request", key))
				return
			}
		}

		nextHandler = pm.peerProxy.ProxyRequest
	}

	if nextHandler == nil {
		pm.sendErrorResponse(c, http.StatusBadRequest, fmt.Sprintf("could not find suitable inference handler for %s", requestedModel))
		return
	}

	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	// dechunk it as we already have all the body bytes see issue #11
	c.Request.Header.Del("transfer-encoding")
	c.Request.Header.Set("content-length", strconv.Itoa(len(bodyBytes)))
	c.Request.ContentLength = int64(len(bodyBytes))

	// issue #366 extract values that downstream handlers may need
	isStreaming := gjson.GetBytes(bodyBytes, "stream").Bool()
	ctx := context.WithValue(c.Request.Context(), proxyCtxKey("streaming"), isStreaming)
	ctx = context.WithValue(ctx, proxyCtxKey("model"), modelID)
	c.Request = c.Request.WithContext(ctx)

	if pm.metricsMonitor != nil && c.Request.Method == "POST" {
		if err := pm.metricsMonitor.wrapHandler(modelID, c.Writer, c.Request, nextHandler); err != nil {
			pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error proxying metrics wrapped request: %s", err.Error()))
			pm.proxyLogger.Errorf("Error Proxying Metrics Wrapped Request model %s", modelID)
			return
		}
	} else {
		if err := nextHandler(modelID, c.Writer, c.Request); err != nil {
			pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error proxying request: %s", err.Error()))
			pm.proxyLogger.Errorf("Error Proxying Request for model %s", modelID)
			return
		}
	}
}

func normalizeResponsesRequestForLlamaCpp(bodyBytes []byte) ([]byte, bool, error) {
	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		return nil, false, err
	}

	changed := false
	if normalizeResponsesToolsForLlamaCpp(body) {
		changed = true
	}
	if hoistResponsesInstructionMessagesForLlamaCpp(body) {
		changed = true
	}

	if !changed {
		return bodyBytes, false, nil
	}

	normalizedBody, err := json.Marshal(body)
	if err != nil {
		return nil, false, err
	}
	return normalizedBody, true, nil
}

func normalizeResponsesToolsForLlamaCpp(body map[string]any) bool {
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) == 0 {
		return false
	}

	normalizedTools := make([]any, 0, len(tools))
	changed := false
	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			normalizedTools = append(normalizedTools, rawTool)
			continue
		}

		switch toolType(tool) {
		case "function":
			normalizedTools = append(normalizedTools, tool)
		case "custom":
			normalizedTools = append(normalizedTools, customResponsesToolToFunction(tool))
			changed = true
		case "apply_patch":
			normalizedTools = append(normalizedTools, customResponsesToolToFunction(tool))
			changed = true
		case "local_shell", "shell":
			normalizedTools = append(normalizedTools, shellResponsesToolToFunction(tool))
			changed = true
		case "namespace":
			nestedTools, flattened := flattenResponsesNamespaceTools(tool)
			normalizedTools = append(normalizedTools, nestedTools...)
			changed = changed || flattened
		default:
			// llama.cpp currently rejects non-function Responses tools. Drop
			// built-ins that Codex cannot execute from a normal function call
			// instead of exposing a tool that would fail after selection.
			changed = true
		}
	}

	if !changed {
		return false
	}

	body["tools"] = normalizedTools
	normalizeResponsesToolChoice(body)
	return true
}

func hoistResponsesInstructionMessagesForLlamaCpp(body map[string]any) bool {
	input, ok := body["input"].([]any)
	if !ok || len(input) == 0 {
		return false
	}

	keptInput := make([]any, 0, len(input))
	instructionSections := make([]string, 0)
	changed := false
	for _, rawItem := range input {
		item, ok := rawItem.(map[string]any)
		if !ok || toolType(item) != "message" {
			keptInput = append(keptInput, rawItem)
			continue
		}

		role, _ := item["role"].(string)
		role = strings.ToLower(strings.TrimSpace(role))
		if role != "system" && role != "developer" {
			keptInput = append(keptInput, rawItem)
			continue
		}

		if text := responsesMessageText(item); text != "" {
			instructionSections = append(instructionSections, text)
		}
		changed = true
	}

	if !changed {
		return false
	}

	body["input"] = keptInput
	if len(instructionSections) > 0 {
		existingInstructions, _ := body["instructions"].(string)
		body["instructions"] = joinResponsesInstructions(existingInstructions, instructionSections)
	}
	return true
}

func responsesMessageText(message map[string]any) string {
	switch content := message["content"].(type) {
	case string:
		return strings.TrimSpace(content)
	case []any:
		parts := make([]string, 0, len(content))
		for _, rawContentItem := range content {
			contentItem, ok := rawContentItem.(map[string]any)
			if !ok {
				continue
			}
			contentType := toolType(contentItem)
			if contentType != "input_text" && contentType != "output_text" && contentType != "text" {
				continue
			}
			if text, ok := contentItem["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
		}
		return strings.Join(parts, "\n\n")
	default:
		return ""
	}
}

func joinResponsesInstructions(existing string, sections []string) string {
	allSections := make([]string, 0, len(sections)+1)
	if existing = strings.TrimSpace(existing); existing != "" {
		allSections = append(allSections, existing)
	}
	for _, section := range sections {
		if section = strings.TrimSpace(section); section != "" {
			allSections = append(allSections, section)
		}
	}
	return strings.Join(allSections, "\n\n")
}

func toolType(tool map[string]any) string {
	value, _ := tool["type"].(string)
	return value
}

func customResponsesToolToFunction(tool map[string]any) map[string]any {
	name := responseToolName(tool, "custom_tool")
	description := responseToolDescription(tool)
	if description == "" {
		description = "Freeform tool input. Put the entire tool input in the input field."
	} else {
		description += "\n\nThis upstream only accepts function tools. Put the complete freeform tool input in the input field."
	}

	return map[string]any{
		"type":        "function",
		"name":        name,
		"description": description,
		"strict":      false,
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"input": map[string]any{
					"type":        "string",
					"description": "The complete freeform input for this tool.",
				},
			},
			"required":             []any{"input"},
			"additionalProperties": false,
		},
	}
}

func flattenResponsesNamespaceTools(namespace map[string]any) ([]any, bool) {
	nestedTools, ok := namespace["tools"].([]any)
	if !ok || len(nestedTools) == 0 {
		return nil, true
	}

	flattened := make([]any, 0, len(nestedTools))
	for _, rawNestedTool := range nestedTools {
		nestedTool, ok := rawNestedTool.(map[string]any)
		if !ok {
			continue
		}

		switch toolType(nestedTool) {
		case "function":
			flattened = append(flattened, nestedTool)
		case "custom":
			flattened = append(flattened, customResponsesToolToFunction(nestedTool))
		case "apply_patch":
			flattened = append(flattened, customResponsesToolToFunction(nestedTool))
		case "local_shell", "shell":
			flattened = append(flattened, shellResponsesToolToFunction(nestedTool))
		}
	}

	return flattened, true
}

func shellResponsesToolToFunction(tool map[string]any) map[string]any {
	name := responseToolName(tool, toolType(tool))
	if name == "" {
		name = "shell"
	}
	description := responseToolDescription(tool)
	if description == "" {
		description = "Runs a shell command and returns its output."
	}

	return map[string]any{
		"type":        "function",
		"name":        name,
		"description": description,
		"strict":      false,
		"parameters": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "array",
					"description": "The command to execute.",
					"items": map[string]any{
						"type": "string",
					},
				},
				"workdir": map[string]any{
					"type":        "string",
					"description": "The working directory to execute the command in.",
				},
				"timeout_ms": map[string]any{
					"type":        "number",
					"description": "The timeout for the command in milliseconds.",
				},
			},
			"required":             []any{"command"},
			"additionalProperties": false,
		},
	}
}

func responseToolName(tool map[string]any, fallback string) string {
	if name, ok := tool["name"].(string); ok && strings.TrimSpace(name) != "" {
		return name
	}
	return strings.TrimSpace(fallback)
}

func responseToolDescription(tool map[string]any) string {
	description, _ := tool["description"].(string)
	return strings.TrimSpace(description)
}

func normalizeResponsesToolChoice(body map[string]any) {
	toolChoice, ok := body["tool_choice"].(map[string]any)
	if !ok {
		return
	}

	switch toolType(toolChoice) {
	case "custom":
		toolChoice["type"] = "function"
	case "apply_patch", "shell":
		if _, hasName := toolChoice["name"].(string); !hasName {
			toolChoice["name"] = toolType(toolChoice)
		}
		toolChoice["type"] = "function"
	}
}

func (pm *ProxyManager) proxyOAIPostFormHandler(c *gin.Context) {
	// Parse multipart form
	if err := c.Request.ParseMultipartForm(32 << 20); err != nil { // 32MB max memory, larger files go to tmp disk
		pm.sendErrorResponse(c, http.StatusBadRequest, fmt.Sprintf("error parsing multipart form: %s", err.Error()))
		return
	}

	// Get model parameter from the form
	requestedModel := c.Request.FormValue("model")
	if requestedModel == "" {
		pm.sendErrorResponse(c, http.StatusBadRequest, "missing or invalid 'model' parameter in form data")
		return
	}

	// Look for a matching local model first, then check peers
	var nextHandler func(modelID string, w http.ResponseWriter, r *http.Request) error
	var useModelName string

	modelID, found := pm.config.RealModelName(requestedModel)
	if found {
		processGroup, err := pm.swapProcessGroup(modelID)
		if err != nil {
			pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error swapping process group: %s", err.Error()))
			return
		}

		useModelName = pm.config.Models[modelID].UseModelName
		pm.proxyLogger.Debugf("ProxyManager using local Process for model: %s", requestedModel)
		nextHandler = processGroup.ProxyRequest
	} else if pm.peerProxy != nil && pm.peerProxy.HasPeerModel(requestedModel) {
		pm.proxyLogger.Debugf("ProxyManager using ProxyPeer for model: %s", requestedModel)
		modelID = requestedModel
		nextHandler = pm.peerProxy.ProxyRequest
	}

	if nextHandler == nil {
		pm.sendErrorResponse(c, http.StatusBadRequest, fmt.Sprintf("could not find suitable handler for %s", requestedModel))
		return
	}

	// We need to reconstruct the multipart form in any case since the body is consumed
	// Create a new buffer for the reconstructed request
	var requestBuffer bytes.Buffer
	multipartWriter := multipart.NewWriter(&requestBuffer)

	// Copy all form values
	for key, values := range c.Request.MultipartForm.Value {
		for _, value := range values {
			fieldValue := value
			// If this is the model field and we have a profile, use just the model name
			if key == "model" {
				// # issue #69 allow custom model names to be sent to upstream
				if useModelName != "" {
					fieldValue = useModelName
				} else {
					fieldValue = requestedModel
				}
			}
			field, err := multipartWriter.CreateFormField(key)
			if err != nil {
				pm.sendErrorResponse(c, http.StatusInternalServerError, "error recreating form field")
				return
			}
			if _, err = field.Write([]byte(fieldValue)); err != nil {
				pm.sendErrorResponse(c, http.StatusInternalServerError, "error writing form field")
				return
			}
		}
	}

	// Copy all files from the original request
	for key, fileHeaders := range c.Request.MultipartForm.File {
		for _, fileHeader := range fileHeaders {
			formFile, err := multipartWriter.CreateFormFile(key, fileHeader.Filename)
			if err != nil {
				pm.sendErrorResponse(c, http.StatusInternalServerError, "error recreating form file")
				return
			}

			file, err := fileHeader.Open()
			if err != nil {
				pm.sendErrorResponse(c, http.StatusInternalServerError, "error opening uploaded file")
				return
			}

			if _, err = io.Copy(formFile, file); err != nil {
				file.Close()
				pm.sendErrorResponse(c, http.StatusInternalServerError, "error copying file data")
				return
			}
			file.Close()
		}
	}

	// Close the multipart writer to finalize the form
	if err := multipartWriter.Close(); err != nil {
		pm.sendErrorResponse(c, http.StatusInternalServerError, "error finalizing multipart form")
		return
	}

	// Create a new request with the reconstructed form data
	modifiedReq, err := http.NewRequestWithContext(
		c.Request.Context(),
		c.Request.Method,
		c.Request.URL.String(),
		&requestBuffer,
	)
	if err != nil {
		pm.sendErrorResponse(c, http.StatusInternalServerError, "error creating modified request")
		return
	}

	// Copy the headers from the original request
	modifiedReq.Header = c.Request.Header.Clone()
	modifiedReq.Header.Set("Content-Type", multipartWriter.FormDataContentType())

	// set the content length of the body
	modifiedReq.Header.Set("Content-Length", strconv.Itoa(requestBuffer.Len()))
	modifiedReq.ContentLength = int64(requestBuffer.Len())

	// Use the modified request for proxying
	if err := nextHandler(modelID, c.Writer, modifiedReq); err != nil {
		pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error proxying request: %s", err.Error()))
		pm.proxyLogger.Errorf("Error Proxying Request for model %s", modelID)
		return
	}
}

func (pm *ProxyManager) proxyGETModelHandler(c *gin.Context) {
	requestedModel := c.Query("model")
	if requestedModel == "" {
		pm.sendErrorResponse(c, http.StatusBadRequest, "missing required 'model' query parameter")
		return
	}

	var nextHandler func(modelID string, w http.ResponseWriter, r *http.Request) error
	var modelID string

	if realModelID, found := pm.config.RealModelName(requestedModel); found {
		processGroup, err := pm.swapProcessGroup(realModelID)
		if err != nil {
			pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error swapping process group: %s", err.Error()))
			return
		}
		modelID = realModelID
		pm.proxyLogger.Debugf("ProxyManager using local Process for model: %s", requestedModel)
		nextHandler = processGroup.ProxyRequest
	} else if pm.peerProxy != nil && pm.peerProxy.HasPeerModel(requestedModel) {
		modelID = requestedModel
		pm.proxyLogger.Debugf("ProxyManager using ProxyPeer for model: %s", requestedModel)
		nextHandler = pm.peerProxy.ProxyRequest
	}

	if nextHandler == nil {
		pm.sendErrorResponse(c, http.StatusBadRequest, fmt.Sprintf("could not find suitable handler for %s", requestedModel))
		return
	}

	if err := nextHandler(modelID, c.Writer, c.Request); err != nil {
		pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error proxying request: %s", err.Error()))
		pm.proxyLogger.Errorf("Error Proxying GET Request for model %s", modelID)
		return
	}
}

func (pm *ProxyManager) sendErrorResponse(c *gin.Context, statusCode int, message string) {
	acceptHeader := c.GetHeader("Accept")

	if strings.Contains(acceptHeader, "application/json") {
		c.JSON(statusCode, gin.H{"error": message})
	} else {
		c.String(statusCode, message)
	}
}

// apiKeyAuth returns a middleware that validates API keys if configured.
// Returns a pass-through handler if no API keys are configured.
func (pm *ProxyManager) apiKeyAuth(allowBasic bool) gin.HandlerFunc {
	if !pm.authRequired() {
		return func(c *gin.Context) { c.Next() }
	}

	return func(c *gin.Context) {
		if !pm.isValidAPIKey(pm.extractAPIKey(c, allowBasic, true)) {
			pm.clearAuthCookie(c)
			pm.sendErrorResponse(c, http.StatusUnauthorized, "unauthorized: invalid or missing API key")
			c.Abort()
			return
		}

		c.Request.Header.Del("Authorization")
		c.Request.Header.Del("x-api-key")
		pm.stripAuthCookie(c.Request)

		c.Next()
	}
}

func (pm *ProxyManager) unloadAllModelsHandler(c *gin.Context) {
	pm.StopProcesses(StopImmediately)
	c.String(http.StatusOK, "OK")
}

func (pm *ProxyManager) listRunningProcessesHandler(context *gin.Context) {
	context.Header("Content-Type", "application/json")
	runningProcesses := make([]gin.H, 0) // Default to an empty response.

	for _, processGroup := range pm.processGroups {
		for _, process := range processGroup.processes {
			if process.CurrentState() == StateReady {
				modelConfig := process.currentConfig()
				runningProcesses = append(runningProcesses, gin.H{
					"model":       process.ID,
					"state":       process.state,
					"cmd":         modelConfig.Cmd,
					"proxy":       modelConfig.Proxy,
					"ttl":         modelConfig.UnloadAfter,
					"name":        modelConfig.Name,
					"description": modelConfig.Description,
				})
			}
		}
	}

	// Put the results under the `running` key.
	response := gin.H{
		"running": runningProcesses,
	}

	context.JSON(http.StatusOK, response) // Always return 200 OK
}

func (pm *ProxyManager) findGroupByModelName(modelName string) *ProcessGroup {
	for _, group := range pm.processGroups {
		if group.HasMember(modelName) {
			return group
		}
	}
	return nil
}

func (pm *ProxyManager) SetVersion(buildDate string, commit string, version string) {
	pm.Lock()
	defer pm.Unlock()
	pm.buildDate = buildDate
	pm.commit = commit
	pm.version = version
}
