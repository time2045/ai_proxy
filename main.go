package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// --- 结构体定义 ---

type Config struct {
	Server      ServerConfig `mapstructure:"server"`
	Upstreams   []Upstream   `mapstructure:"upstreams"`
	ProjectAuth []ProjectKey `mapstructure:"project_auth"`
}

type ServerConfig struct {
	Port                string            `mapstructure:"port"`
	MaxRetries          int               `mapstructure:"max_retries"`
	CoolDownMinutes     int               `mapstructure:"cool_down_minutes"`
	NotificationWebhook string            `mapstructure:"notification_webhook"`
	AutoModels          map[string]string `mapstructure:"auto_models"`
}

type Upstream struct {
	Name    string   `mapstructure:"name"`
	BaseURL string   `mapstructure:"base_url"`
	Models  []string `mapstructure:"models"`
	Keys    []string `mapstructure:"keys"`
}

// [新增] 通知限流记录
type NotificationRecord struct {
	Upstream   string
	ModelName  string
	StatusCode int
	LastSent   time.Time
}

type ProjectKey struct {
	ProjectName   string   `mapstructure:"project_name"`
	APIKey        string   `mapstructure:"api_key"`
	AllowedModels []string `mapstructure:"allowed_models"`
}

// --- 全局状态管理 ---

var (
	globalConfig   *Config
	configLock     sync.RWMutex
	blacklist      sync.Map // API Key 黑名单
	upstreamStates sync.Map // 轮询计数器
	httpClient     *http.Client
	notifyRecords  sync.Map // 通知记录，防止重复通知
)

// --- 初始化与配置 ---

func init() {
	httpClient = &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 20,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

func initConfig() {
	viper.SetConfigFile("config.yaml")
	viper.SetDefault("server.port", "8080")
	viper.SetDefault("server.max_retries", 3)
	viper.SetDefault("server.cool_down_minutes", 5)

	viper.OnConfigChange(func(e fsnotify.Event) {
		log.Println("[CONFIG] 配置文件变更，重载中...")
		reloadConfig()
	})
	viper.WatchConfig()
	reloadConfig()
}

func reloadConfig() {
	var err error
	for i := 0; i < 5; i++ {
		err = viper.ReadInConfig()
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err != nil {
		log.Printf("[CONFIG] 读取失败: %v", err)
		return
	}

	var newCfg Config
	if err := viper.Unmarshal(&newCfg); err != nil {
		log.Printf("[CONFIG] 解析失败: %v", err)
		return
	}

	configLock.Lock()
	globalConfig = &newCfg
	configLock.Unlock()

	log.Println("[CONFIG] 加载成功")
}

// --- 核心工具 ---

func GetAvailableKey(u *Upstream) (string, error) {
	if len(u.Keys) == 0 {
		return "", fmt.Errorf("上游 '%s' 未配置 Key", u.Name)
	}

	val, _ := upstreamStates.LoadOrStore(u.Name, new(uint64))
	counterPtr := val.(*uint64)
	numKeys := len(u.Keys)

	for i := 0; i < numKeys; i++ {
		idx := atomic.AddUint64(counterPtr, 1) % uint64(numKeys)
		key := u.Keys[idx]

		if until, ok := blacklist.Load(key); ok {
			if time.Now().Before(until.(time.Time)) {
				continue
			}
			blacklist.Delete(key)
		}
		return key, nil
	}
	return "", fmt.Errorf("上游 '%s' 所有 Key 均在冷却中", u.Name)
}

// [新增] 简单的密钥打码工具，防止通知泄露
func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

// [新增] 生成通知记录的唯一键
func getNotifyKey(upstream, model string, statusCode int) string {
	return fmt.Sprintf("%s|%s|%d", upstream, model, statusCode)
}

// [新增] 异步发送通知（带去重和重试）
func sendNotification(webhookURL, upstream, model string, statusCode int, content string) {
	if webhookURL == "" {
		return
	}

	key := getNotifyKey(upstream, model, statusCode)
	now := time.Now()

	// 检查是否在冷却期内（5分钟内不重复发送）
	if val, ok := notifyRecords.Load(key); ok {
		if record, ok := val.(NotificationRecord); ok {
			if now.Sub(record.LastSent) < 5*time.Minute {
				log.Printf("[INFO] Notification skipped (within cooldown): %s", key)
				return
			}
		}
	}

	// 更新通知记录
	notifyRecords.Store(key, NotificationRecord{
		Upstream:   upstream,
		ModelName:  model,
		StatusCode: statusCode,
		LastSent:   now,
	})

	// 异步发送，避免阻塞主流程
	go func() {
		for i := 0; i < 2; i++ {
			payload := map[string]string{
				"msg_type": "text",
				"content":  content,
				"text":     content,
			}
			jsonBody, err := json.Marshal(payload)
			if err != nil {
				log.Printf("[WARN] Webhook marshal failed: %v", err)
				return
			}

			req, err := http.NewRequest("POST", webhookURL, bytes.NewBuffer(jsonBody))
			if err != nil {
				log.Printf("[WARN] Webhook create request failed: %v", err)
				return
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := httpClient.Do(req)
			if err != nil {
				log.Printf("[WARN] Webhook send failed (attempt %d): %v", i+1, err)
				time.Sleep(1 * time.Second)
				continue
			}
			defer resp.Body.Close()

			respBody, _ := io.ReadAll(resp.Body)
			if resp.StatusCode >= 400 {
				log.Printf("[WARN] Webhook returned error (attempt %d): Status=%d, Body=%s", i+1, resp.StatusCode, string(respBody))
				time.Sleep(1 * time.Second)
				continue
			}

			log.Printf("[INFO] Webhook sent successfully: %s", key)
			return
		}
		log.Printf("[ERROR] Webhook send exhausted after retries: %s", key)
	}()
}

func copyHeaders(src, dst http.Header) {
	for k, vv := range src {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func isHopByHop(h string) bool {
	switch http.CanonicalHeaderKey(h) {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
		"Te", "Trailers", "Transfer-Encoding", "Upgrade":
		return true
	}
	return false
}

// --- 主逻辑 ---

func main() {
	gin.SetMode(gin.ReleaseMode)
	initConfig()

	r := gin.New()
	r.Use(gin.Recovery())
	_ = r.SetTrustedProxies(nil)

	r.Any("/v1/*path", handleRequest)

	port := globalConfig.Server.Port
	if !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	srv := &http.Server{
		Addr:              port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("[SYSTEM] 服务启动监听 %s", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("[SYSTEM] 启动失败: %v", err)
	}
}

func handleModels(c *gin.Context) {
	configLock.RLock()
	cfg := globalConfig
	authList := cfg.ProjectAuth
	upstreams := cfg.Upstreams
	srvCfg := cfg.Server
	configLock.RUnlock()

	// 1. 鉴权
	authHeader := c.GetHeader("Authorization")
	clientKey := strings.TrimPrefix(authHeader, "Bearer ")
	var allowedModels []string
	var foundProject bool

	for _, p := range authList {
		if p.APIKey == clientKey {
			allowedModels = p.AllowedModels
			foundProject = true
			break
		}
	}

	if !foundProject {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid API Key"})
		return
	}

	// 2. 聚合
	type ModelResponse struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}

	var responseList []ModelResponse
	seen := make(map[string]bool)

	allowAll := false
	for _, m := range allowedModels {
		if m == "*" {
			allowAll = true
			break
		}
	}

	// 物理模型
	for _, u := range upstreams {
		for _, m := range u.Models {
			if m == "*" {
				continue
			}
			if !seen[m] {
				isAllowed := allowAll
				if !isAllowed {
					for _, allowed := range allowedModels {
						if allowed == m {
							isAllowed = true
							break
						}
					}
				}
				if isAllowed {
					responseList = append(responseList, ModelResponse{
						ID:      m,
						Object:  "model",
						Created: time.Now().Unix(),
						OwnedBy: u.Name,
					})
					seen[m] = true
				}
			}
		}
	}

	// 映射模型 (Auto)
	for reqKey, mapVal := range srvCfg.AutoModels {
		parts := strings.SplitN(mapVal, ":", 2)
		var realModel string
		if len(parts) == 2 {
			realModel = parts[1]
		} else {
			realModel = parts[0]
		}

		canUseTarget := allowAll
		if !canUseTarget {
			for _, allowed := range allowedModels {
				if allowed == realModel {
					canUseTarget = true
					break
				}
			}
		}

		if canUseTarget && !seen[reqKey] {
			responseList = append(responseList, ModelResponse{
				ID:      reqKey,
				Object:  "model",
				Created: time.Now().Unix(),
				OwnedBy: "system-auto-map",
			})
			seen[reqKey] = true
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   responseList,
	})
}

func handleRequest(c *gin.Context) {
	path := c.Param("path")
	if c.Request.Method == http.MethodGet && (path == "/models" || path == "/models/") {
		handleModels(c)
		return
	}

	configLock.RLock()
	cfg := globalConfig
	authList := cfg.ProjectAuth
	srvCfg := cfg.Server
	upstreams := cfg.Upstreams
	configLock.RUnlock()

	// 1. 鉴权
	authHeader := c.GetHeader("Authorization")
	clientKey := strings.TrimPrefix(authHeader, "Bearer ")
	var projectName string
	var allowedModels []string

	for _, p := range authList {
		if p.APIKey == clientKey {
			projectName = p.ProjectName
			allowedModels = p.AllowedModels
			break
		}
	}

	if projectName == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized Project Key"})
		return
	}

	// 2. Body
	bodyBytes, err := c.GetRawData()
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "Read body failed"})
		return
	}

	var reqInfo struct {
		Model string `json:"model"`
	}
	if len(bodyBytes) > 0 {
		_ = json.Unmarshal(bodyBytes, &reqInfo)
	}

	// --- Auto Models 映射 ---
	var targetUpstreamName string

	if mapVal, exists := srvCfg.AutoModels[reqInfo.Model]; exists {
		log.Printf("[MAP] Proj:%s | '%s' -> '%s'", projectName, reqInfo.Model, mapVal)
		parts := strings.SplitN(mapVal, ":", 2)
		if len(parts) == 2 {
			targetUpstreamName = parts[0]
			reqInfo.Model = parts[1]
		} else {
			reqInfo.Model = parts[0]
		}

		var jsonBody map[string]interface{}
		if jsonErr := json.Unmarshal(bodyBytes, &jsonBody); jsonErr == nil {
			jsonBody["model"] = reqInfo.Model
			if newBytes, marshalErr := json.Marshal(jsonBody); marshalErr == nil {
				bodyBytes = newBytes
			}
		}
	}

	// 权限检查
	isAllowed := false
	for _, m := range allowedModels {
		if m == "*" || m == reqInfo.Model {
			isAllowed = true
			break
		}
	}
	if !isAllowed {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
			"error": fmt.Sprintf("Model '%s' is not allowed for project '%s'", reqInfo.Model, projectName),
		})
		return
	}

	// 3. 匹配上游
	var target *Upstream

	if targetUpstreamName != "" {
		for i := range upstreams {
			if upstreams[i].Name == targetUpstreamName {
				supports := false
				for _, m := range upstreams[i].Models {
					if m == "*" || m == reqInfo.Model {
						supports = true
						break
					}
				}
				if supports {
					target = &upstreams[i]
				}
				break
			}
		}
		if target == nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("Target upstream '%s' not found or does not support model '%s'", targetUpstreamName, reqInfo.Model),
			})
			return
		}
	} else {
		for i := range upstreams {
			for _, m := range upstreams[i].Models {
				if m == reqInfo.Model {
					target = &upstreams[i]
					break
				}
			}
			if target != nil {
				break
			}
		}
		if target == nil {
			for i := range upstreams {
				for _, m := range upstreams[i].Models {
					if m == "*" {
						target = &upstreams[i]
						break
					}
				}
				if target != nil {
					break
				}
			}
		}
	}

	if target == nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("No upstream found for model: %s", reqInfo.Model),
		})
		return
	}

	forwardWithRetry(c, target, srvCfg, bodyBytes, projectName, reqInfo.Model)
}

func forwardWithRetry(c *gin.Context, upstream *Upstream, srvCfg ServerConfig, body []byte, projectName, modelName string) {
	var lastErr error

	for retry := 0; retry <= srvCfg.MaxRetries; retry++ {
		// 1. Key
		realKey, err := GetAvailableKey(upstream)
		if err != nil {
			lastErr = err
			time.Sleep(100 * time.Millisecond)
			continue
		}

		log.Printf("[ROUTE] Proj:%s | Model:%s | Pool:%s | Try:%d",
			projectName, modelName, upstream.Name, retry+1)

		// 2. URL
		targetURL, err := url.Parse(upstream.BaseURL)
		if err != nil {
			c.JSON(500, gin.H{"error": "Config error: invalid base_url"})
			return
		}

		finalPath := strings.TrimRight(targetURL.Path, "/") + "/v1" + c.Param("path")
		targetURL.Path = finalPath
		targetURL.RawQuery = c.Request.URL.RawQuery

		req, err := http.NewRequest(c.Request.Method, targetURL.String(), bytes.NewBuffer(body))
		if err != nil {
			c.JSON(500, gin.H{"error": "NewRequest failed"})
			return
		}

		copyHeaders(c.Request.Header, req.Header)
		req.Header.Set("Authorization", "Bearer "+realKey)
		req.Header.Set("Host", targetURL.Host)
		req.Header.Set("Content-Length", fmt.Sprintf("%d", len(body)))

		req = req.WithContext(c.Request.Context())

		// 3. Request
		resp, err := httpClient.Do(req)

		shouldRetry := false
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, c.Request.Context().Err()) {
				log.Printf("[INFO] Client disconnected: %v", err)
				return
			}
			log.Printf("[WARN] Network error: %v", err)
			shouldRetry = true
			lastErr = err
		} else {
			// [修改] 错误处理与通知逻辑
			if resp.StatusCode == 401 || resp.StatusCode == 429 || resp.StatusCode >= 500 {
				log.Printf("[WARN] Upstream %d, blocking key...", resp.StatusCode)

				// 加入黑名单
				blacklist.Store(realKey, time.Now().Add(time.Duration(srvCfg.CoolDownMinutes)*time.Minute))

				// [新增] 发送通知
				msg := fmt.Sprintf("⚠️ [AI-Proxy Alert]\nUpstream: %s\nStatus: %d\nKey: %s\nProject: %s\nModel: %s\nTime: %s",
					upstream.Name, resp.StatusCode, maskKey(realKey), projectName, modelName, time.Now().Format("15:04:05"))
				sendNotification(srvCfg.NotificationWebhook, upstream.Name, modelName, resp.StatusCode, msg)

				resp.Body.Close()
				shouldRetry = true
				lastErr = fmt.Errorf("status %d", resp.StatusCode)
			}
		}

		if shouldRetry {
			if retry < srvCfg.MaxRetries {
				time.Sleep(200 * time.Millisecond)
				continue
			}
			break
		}

		// 4. Success
		defer resp.Body.Close()
		copyHeaders(resp.Header, c.Writer.Header())
		c.Writer.Header().Set("X-Accel-Buffering", "no")
		c.Status(resp.StatusCode)

		if _, err := io.Copy(c.Writer, resp.Body); err != nil {
			if !strings.Contains(err.Error(), "context canceled") {
				log.Printf("[ERROR] Copy body failed: %v", err)
			}
		}
		return
	}

	// 所有重试失败，发送一条最终失败通知
	failMsg := fmt.Sprintf("❌ [AI-Proxy Fail]\nAll retries exhausted for Project: %s (Model: %s).\nLast Error: %v", projectName, modelName, lastErr)
	sendNotification(srvCfg.NotificationWebhook, upstream.Name, modelName, 0, failMsg)

	c.JSON(http.StatusServiceUnavailable, gin.H{
		"error": "Service Unavailable",
		"msg":   fmt.Sprintf("Retries exhausted. Last error: %v", lastErr),
	})
}
