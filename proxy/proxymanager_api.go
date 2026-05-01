package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mostlygeek/llama-swap/event"
)

type Model struct {
	Id             string     `json:"id"`
	Name           string     `json:"name"`
	Description    string     `json:"description"`
	State          string     `json:"state"`
	StateChangedAt *time.Time `json:"stateChangedAt,omitempty"`
	Unlisted       bool       `json:"unlisted"`
	PeerID         string     `json:"peerID"`
	Aliases        []string   `json:"aliases,omitempty"`
	ContextSize    int        `json:"contextSize,omitempty"`
}

func addApiHandlers(pm *ProxyManager) {
	pm.ginEngine.GET("/api/auth/session", pm.apiAuthSession)
	pm.ginEngine.POST("/api/auth/login", pm.apiAuthLogin)
	pm.ginEngine.POST("/api/auth/logout", pm.apiAuthLogout)

	// Add API endpoints for React to consume
	// Protected with API key authentication
	apiGroup := pm.ginEngine.Group("/api", pm.apiKeyAuth(false))
	{
		apiGroup.GET("/models", pm.apiGetModels)
		apiGroup.GET("/models/", pm.apiGetModels)
		apiGroup.POST("/models/unload", pm.apiUnloadAllModels)
		apiGroup.POST("/models/unload/*model", pm.apiUnloadSingleModelHandler)
		apiGroup.GET("/config/models", pm.apiGetModelConfigs)
		apiGroup.PUT("/config/models/:model/settings", pm.apiPutModelConfigSettings)
		apiGroup.DELETE("/config/models/:model/settings", pm.apiDeleteModelConfigSettings)
		apiGroup.GET("/config/export", pm.apiExportModelConfigSettings)
		apiGroup.POST("/config/import", pm.apiImportModelConfigSettings)
		apiGroup.GET("/events", pm.apiSendEvents)
		apiGroup.GET("/metrics", pm.apiGetMetrics)
		apiGroup.DELETE("/metrics", pm.apiClearActivity)
		apiGroup.GET("/metrics/export", pm.apiExportActivityDB)
		apiGroup.GET("/version", pm.apiGetVersion)
		apiGroup.GET("/info", pm.apiGetInfo)
		apiGroup.GET("/captures/:id", pm.apiGetCapture)
	}
}

// ServerInfo describes connection details a UI client needs to call the
// OpenAI-compatible API from external tools. Only returned to authenticated
// callers (route is registered under apiGroup), so emitting the first
// configured API key is no different from what the user already supplied at
// login or holds in their config file.
type ServerInfo struct {
	AuthRequired bool   `json:"authRequired"`
	APIKey       string `json:"apiKey,omitempty"`
}

func (pm *ProxyManager) apiGetInfo(c *gin.Context) {
	info := ServerInfo{
		AuthRequired: pm.authRequired(),
	}
	if info.AuthRequired && len(pm.config.RequiredAPIKeys) > 0 {
		info.APIKey = pm.config.RequiredAPIKeys[0]
	}
	c.JSON(http.StatusOK, info)
}

func (pm *ProxyManager) apiUnloadAllModels(c *gin.Context) {
	pm.StopProcesses(StopImmediately)
	c.JSON(http.StatusOK, gin.H{"msg": "ok"})
}

func (pm *ProxyManager) getModelStatus() []Model {
	// Extract keys and sort them
	models := []Model{}

	modelIDs := make([]string, 0, len(pm.config.Models))
	for modelID := range pm.config.Models {
		modelIDs = append(modelIDs, modelID)
	}
	sort.Strings(modelIDs)

	// Iterate over sorted keys
	for _, modelID := range modelIDs {
		modelConfig := pm.config.Models[modelID]
		contextSize := configuredContextSize(modelConfig.SanitizedCommand)

		// Get process state
		processGroup := pm.findGroupByModelName(modelID)
		state := "unknown"
		if processGroup != nil {
			process := processGroup.processes[modelID]
			if process != nil {
				stateChangedAt := process.CurrentStateChangedAt()
				var stateStr string
				switch process.CurrentState() {
				case StateReady:
					stateStr = "ready"
				case StateStarting:
					stateStr = "starting"
				case StateStopping:
					stateStr = "stopping"
				case StateShutdown:
					stateStr = "shutdown"
				case StateStopped:
					stateStr = "stopped"
				default:
					stateStr = "unknown"
				}
				state = stateStr
				models = append(models, Model{
					Id:             modelID,
					Name:           modelConfig.Name,
					Description:    modelConfig.Description,
					State:          state,
					StateChangedAt: &stateChangedAt,
					Unlisted:       modelConfig.Unlisted,
					Aliases:        modelConfig.Aliases,
					ContextSize:    contextSize,
				})
				continue
			}
		}
		models = append(models, Model{
			Id:          modelID,
			Name:        modelConfig.Name,
			Description: modelConfig.Description,
			State:       state,
			Unlisted:    modelConfig.Unlisted,
			Aliases:     modelConfig.Aliases,
			ContextSize: contextSize,
		})
	}

	// Iterate over the peer models
	if pm.peerProxy != nil {
		for peerID, peer := range pm.peerProxy.ListPeers() {
			for _, modelID := range peer.Models {
				models = append(models, Model{
					Id:     modelID,
					PeerID: peerID,
				})
			}
		}
	}

	return models
}

var contextSizeFlags = map[string]bool{
	"-c":             true,
	"--ctx-size":     true,
	"--ctx_size":     true,
	"--context-size": true,
	"--n-ctx":        true,
	"--n_ctx":        true,
}

func configuredContextSize(sanitizedCommand func() ([]string, error)) int {
	args, err := sanitizedCommand()
	if err != nil {
		return 0
	}

	for i, arg := range args {
		if contextSizeFlags[arg] {
			if i+1 >= len(args) {
				return 0
			}
			return parsePositiveInt(args[i+1])
		}

		flag, value, found := strings.Cut(arg, "=")
		if found && contextSizeFlags[flag] {
			return parsePositiveInt(value)
		}
	}

	return 0
}

func parsePositiveInt(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func (pm *ProxyManager) apiGetModels(c *gin.Context) {
	c.JSON(http.StatusOK, pm.getModelStatus())
}

func (pm *ProxyManager) apiGetModelConfigs(c *gin.Context) {
	configs, err := pm.listEditableModelConfigs()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get model config settings"})
		return
	}
	c.JSON(http.StatusOK, configs)
}

func (pm *ProxyManager) apiPutModelConfigSettings(c *gin.Context) {
	if pm.sessionModelSettings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "session model settings are unavailable"})
		return
	}

	modelID := c.Param("model")
	var settings SessionModelSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model settings"})
		return
	}

	modelConfig, err := pm.saveSessionModelSettings(modelID, settings)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, modelConfig)
}

func (pm *ProxyManager) apiDeleteModelConfigSettings(c *gin.Context) {
	if pm.sessionModelSettings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "session model settings are unavailable"})
		return
	}

	modelID := c.Param("model")
	modelConfig, err := pm.resetSessionModelSettings(modelID)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, modelConfig)
}

func (pm *ProxyManager) apiExportModelConfigSettings(c *gin.Context) {
	data, err := pm.exportSessionConfigYAML()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to export model config settings"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="llama-swap-session-config.yaml"`)
	c.Data(http.StatusOK, "application/x-yaml; charset=utf-8", data)
}

func (pm *ProxyManager) apiImportModelConfigSettings(c *gin.Context) {
	if pm.sessionModelSettings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "session model settings are unavailable"})
		return
	}

	data, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read imported config"})
		return
	}
	result, err := pm.importSessionConfigYAML(data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

type messageType string

const (
	msgTypeModelStatus messageType = "modelStatus"
	msgTypeLogData     messageType = "logData"
	msgTypeMetrics     messageType = "metrics"
	msgTypeInFlight    messageType = "inflight"
	msgTypeActivity    messageType = "activity"
)

type messageEnvelope struct {
	Type messageType `json:"type"`
	Data string      `json:"data"`
}

// sends a stream of different message types that happen on the server
func (pm *ProxyManager) apiSendEvents(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Content-Type-Options", "nosniff")
	// prevent nginx from buffering SSE
	c.Header("X-Accel-Buffering", "no")

	sendBuffer := make(chan messageEnvelope, 25)
	ctx, cancel := context.WithCancel(c.Request.Context())
	sendModels := func() {
		data, err := json.Marshal(pm.getModelStatus())
		if err == nil {
			msg := messageEnvelope{Type: msgTypeModelStatus, Data: string(data)}
			select {
			case sendBuffer <- msg:
			case <-ctx.Done():
				return
			default:
			}

		}
	}

	sendLogData := func(source string, data []byte) {
		data, err := json.Marshal(gin.H{
			"source": source,
			"data":   string(data),
		})
		if err == nil {
			select {
			case sendBuffer <- messageEnvelope{Type: msgTypeLogData, Data: string(data)}:
			case <-ctx.Done():
				return
			default:
			}
		}
	}

	sendMetrics := func(metrics []TokenMetrics) {
		jsonData, err := json.Marshal(metrics)
		if err == nil {
			select {
			case sendBuffer <- messageEnvelope{Type: msgTypeMetrics, Data: string(jsonData)}:
			case <-ctx.Done():
				return
			default:
			}
		}
	}

	sendInFlight := func(total int) {
		jsonData, err := json.Marshal(gin.H{"total": total})
		if err == nil {
			select {
			case sendBuffer <- messageEnvelope{Type: msgTypeInFlight, Data: string(jsonData)}:
			case <-ctx.Done():
				return
			default:
			}
		}
	}

	sendActivityCleared := func() {
		jsonData, err := json.Marshal(gin.H{"cleared": true})
		if err == nil {
			select {
			case sendBuffer <- messageEnvelope{Type: msgTypeActivity, Data: string(jsonData)}:
			case <-ctx.Done():
				return
			default:
			}
		}
	}

	/**
	 * Send updated models list
	 */
	defer event.On(func(e ProcessStateChangeEvent) {
		sendModels()
	})()
	defer event.On(func(e ConfigFileChangedEvent) {
		sendModels()
	})()

	/**
	 * Send Log data
	 */
	defer pm.proxyLogger.OnLogData(func(data []byte) {
		sendLogData("proxy", data)
	})()
	defer pm.upstreamLogger.OnLogData(func(data []byte) {
		sendLogData("upstream", data)
	})()

	/**
	 * Send Metrics data
	 */
	defer event.On(func(e TokenMetricsEvent) {
		sendMetrics([]TokenMetrics{e.Metrics})
	})()
	defer event.On(func(e ActivityClearedEvent) {
		sendActivityCleared()
	})()

	/**
	 * Send in-flight request stats related to token stats "Waiting: N" count.
	 */
	defer event.On(func(e InFlightRequestsEvent) {
		sendInFlight(e.Total)
	})()

	// send initial batch of data
	sendLogData("proxy", pm.proxyLogger.GetHistory())
	sendLogData("upstream", pm.upstreamLogger.GetHistory())
	sendModels()
	sendMetrics(pm.metricsMonitor.getMetrics())
	sendInFlight(pm.inFlightCounter.Current())

	for {
		select {
		case <-c.Request.Context().Done():
			cancel()
			return
		case <-pm.shutdownCtx.Done():
			cancel()
			return
		case msg := <-sendBuffer:
			c.SSEvent("message", msg)
			c.Writer.Flush()
		}
	}
}

func (pm *ProxyManager) apiGetMetrics(c *gin.Context) {
	jsonData, err := pm.metricsMonitor.getMetricsJSON()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get metrics"})
		return
	}
	c.Data(http.StatusOK, "application/json", jsonData)
}

func (pm *ProxyManager) apiClearActivity(c *gin.Context) {
	if err := pm.metricsMonitor.clearActivity(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear activity"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"msg": "ok"})
}

func (pm *ProxyManager) apiExportActivityDB(c *gin.Context) {
	file, err := os.CreateTemp("", "llama-swap-activity-*.sqlite")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create export file"})
		return
	}
	exportPath := file.Name()
	if err := file.Close(); err != nil {
		os.Remove(exportPath)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create export file"})
		return
	}
	defer os.Remove(exportPath)

	if err := pm.metricsMonitor.exportActivityDB(exportPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to export activity database"})
		return
	}

	c.Header("Cache-Control", "no-store")
	c.FileAttachment(exportPath, "llama-swap-activity.sqlite")
}

func (pm *ProxyManager) apiUnloadSingleModelHandler(c *gin.Context) {
	requestedModel := strings.TrimPrefix(c.Param("model"), "/")
	realModelName, found := pm.config.RealModelName(requestedModel)
	if !found {
		pm.sendErrorResponse(c, http.StatusNotFound, "Model not found")
		return
	}

	processGroup := pm.findGroupByModelName(realModelName)
	if processGroup == nil {
		pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("process group not found for model %s", requestedModel))
		return
	}

	if err := processGroup.StopProcess(realModelName, StopImmediately); err != nil {
		pm.sendErrorResponse(c, http.StatusInternalServerError, fmt.Sprintf("error stopping process: %s", err.Error()))
		return
	} else {
		c.String(http.StatusOK, "OK")
	}
}

func (pm *ProxyManager) apiGetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, map[string]string{
		"version":    pm.version,
		"commit":     pm.commit,
		"build_date": pm.buildDate,
	})
}

func (pm *ProxyManager) apiGetCapture(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid capture ID"})
		return
	}

	capture := pm.metricsMonitor.getCaptureByID(id)
	if capture == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "capture not found"})
		return
	}

	c.JSON(http.StatusOK, capture)
}
