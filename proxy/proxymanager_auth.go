package proxy

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	uiAuthCookieName         = "llama_swap_ui_auth"
	uiAuthCookieMaxAgeSecond = 60 * 60 * 24 * 30
)

type authSessionResponse struct {
	AuthRequired  bool `json:"authRequired"`
	Authenticated bool `json:"authenticated"`
}

type authLoginRequest struct {
	Password string `json:"password" form:"password"`
}

func (pm *ProxyManager) authRequired() bool {
	return len(pm.config.RequiredAPIKeys) > 0
}

func (pm *ProxyManager) isValidAPIKey(providedKey string) bool {
	if !pm.authRequired() {
		return true
	}

	for _, key := range pm.config.RequiredAPIKeys {
		if providedKey == key {
			return true
		}
	}

	return false
}

func extractAPIKeyFromAuthorizationHeader(auth string, allowBasic bool) string {
	if auth == "" {
		return ""
	}

	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	if !allowBasic || !strings.HasPrefix(auth, "Basic ") {
		return ""
	}

	encoded := strings.TrimPrefix(auth, "Basic ")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return ""
	}

	return parts[1]
}

func extractAPIKeyFromCookie(c *gin.Context) string {
	cookieValue, err := c.Cookie(uiAuthCookieName)
	if err != nil || cookieValue == "" {
		return ""
	}

	decoded, err := base64.RawURLEncoding.DecodeString(cookieValue)
	if err != nil {
		return ""
	}

	return string(decoded)
}

func (pm *ProxyManager) extractAPIKey(c *gin.Context, allowBasic bool, allowCookie bool) string {
	if key := extractAPIKeyFromAuthorizationHeader(c.GetHeader("Authorization"), allowBasic); key != "" {
		return key
	}

	if xApiKey := c.GetHeader("x-api-key"); xApiKey != "" {
		return xApiKey
	}

	if allowCookie {
		return extractAPIKeyFromCookie(c)
	}

	return ""
}

func (pm *ProxyManager) setAuthCookie(c *gin.Context, apiKey string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(
		uiAuthCookieName,
		base64.RawURLEncoding.EncodeToString([]byte(apiKey)),
		uiAuthCookieMaxAgeSecond,
		"/",
		"",
		requestIsSecure(c.Request),
		true,
	)
}

func (pm *ProxyManager) clearAuthCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(uiAuthCookieName, "", -1, "/", "", requestIsSecure(c.Request), true)
}

func (pm *ProxyManager) stripAuthCookie(request *http.Request) {
	cookies := request.Cookies()
	if len(cookies) == 0 {
		return
	}

	request.Header.Del("Cookie")
	for _, cookie := range cookies {
		if cookie.Name == uiAuthCookieName {
			continue
		}
		request.AddCookie(cookie)
	}
}

func requestIsSecure(request *http.Request) bool {
	if request.TLS != nil {
		return true
	}

	return strings.EqualFold(strings.TrimSpace(request.Header.Get("X-Forwarded-Proto")), "https")
}

func (pm *ProxyManager) apiAuthSession(c *gin.Context) {
	authenticated := pm.isValidAPIKey(extractAPIKeyFromCookie(c))
	if !authenticated {
		pm.clearAuthCookie(c)
	}

	c.JSON(http.StatusOK, authSessionResponse{
		AuthRequired:  pm.authRequired(),
		Authenticated: authenticated,
	})
}

func (pm *ProxyManager) apiAuthLogin(c *gin.Context) {
	if !pm.authRequired() {
		c.JSON(http.StatusOK, authSessionResponse{
			AuthRequired:  false,
			Authenticated: true,
		})
		return
	}

	var request authLoginRequest
	if err := c.ShouldBind(&request); err != nil || request.Password == "" {
		pm.clearAuthCookie(c)
		c.JSON(http.StatusBadRequest, gin.H{"error": "password is required"})
		return
	}

	if !pm.isValidAPIKey(request.Password) {
		pm.clearAuthCookie(c)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid password"})
		return
	}

	pm.setAuthCookie(c, request.Password)
	c.JSON(http.StatusOK, authSessionResponse{
		AuthRequired:  true,
		Authenticated: true,
	})
}

func (pm *ProxyManager) apiAuthLogout(c *gin.Context) {
	pm.clearAuthCookie(c)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
