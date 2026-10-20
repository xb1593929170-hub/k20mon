// Command k20mon-server is the K20Mon backend: it samples the device through
// procfs/sysfs, stores history in MySQL, and forwards privileged operations to
// the root control agent over a Unix socket.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
	mysqlDriver "gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/yourname/k20mon/server/model"
	metricsRequest "github.com/yourname/k20mon/server/model/request"
	"github.com/yourname/k20mon/server/service"
)

type config struct {
	Addr          string
	MySQLDSN      string
	JWTKey        string
	JWTIssuer     string
	JWTExpires    time.Duration
	ControlSocket string
	Interval      time.Duration
}

type fileConfig struct {
	MySQL struct {
		Path     string `yaml:"path"`
		Port     string `yaml:"port"`
		Config   string `yaml:"config"`
		DBName   string `yaml:"db-name"`
		Username string `yaml:"username"`
		Password string `yaml:"password"`
	} `yaml:"mysql"`
	JWT struct {
		SigningKey  string `yaml:"signing-key"`
		ExpiresTime string `yaml:"expires-time"`
		Issuer      string `yaml:"issuer"`
	} `yaml:"jwt"`
	System struct {
		Addr int `yaml:"addr"`
	} `yaml:"system"`
}

// userRecord maps the subset of the GVA sys_users table this app needs, so it
// can reuse an existing deployment's accounts without its schema.
type userRecord struct {
	ID          uint   `json:"ID" gorm:"primaryKey"`
	UUID        string `json:"uuid"`
	Username    string `json:"userName" gorm:"column:username"`
	Password    string `json:"-"`
	NickName    string `json:"nickName" gorm:"column:nick_name"`
	HeaderImg   string `json:"headerImg" gorm:"column:header_img"`
	AuthorityId uint   `json:"authorityId" gorm:"column:authority_id"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Enable      int    `json:"enable"`
}

func (userRecord) TableName() string { return "sys_users" }

type claims struct {
	UserID      uint   `json:"userId"`
	UUID        string `json:"uuid"`
	Username    string `json:"username"`
	NickName    string `json:"nickName"`
	AuthorityId uint   `json:"authorityId"`
	jwt.RegisteredClaims
}

type application struct {
	cfg config
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		panic(err)
	}

	// The server must boot even when MySQL is down: the dashboard still has
	// live procfs/sysfs metrics, and the control agent does not need the DB.
	// A background loop attaches the pool as soon as the database answers.
	connectDatabase(cfg.MySQLDSN)

	if cfg.ControlSocket != "" {
		_ = os.Setenv("K20M_CONTROL_SOCKET", cfg.ControlSocket)
	}

	ctx := context.Background()
	service.Catalog.EnsureSeed(ctx)
	service.Metrics.StartSampler(ctx, cfg.Interval)

	app := &application{cfg: cfg}
	fmt.Printf("K20Mon backend listening on %s\n", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, app.router()); err != nil {
		panic(err)
	}
}

// connectDatabase keeps retrying until the pool is usable. Migrations run once
// per successful attach, and the sampler starts persisting right away.
func connectDatabase(dsn string) {
	go func() {
		for attempt := 1; ; attempt++ {
			db, err := gorm.Open(mysqlDriver.Open(dsn), &gorm.Config{})
			if err == nil {
				if sqlDB, dbErr := db.DB(); dbErr == nil {
					sqlDB.SetMaxIdleConns(5)
					sqlDB.SetMaxOpenConns(20)
					sqlDB.SetConnMaxLifetime(30 * time.Minute)
					if pingErr := sqlDB.Ping(); pingErr != nil {
						err = pingErr
					} else if migrateErr := db.AutoMigrate(&model.MetricSample{}, &model.ServiceCatalog{}); migrateErr != nil {
						err = migrateErr
					}
				} else {
					err = dbErr
				}
			}
			if err == nil {
				service.SetDB(db)
				service.Catalog.EnsureSeed(context.Background())
				fmt.Println("K20Mon database connected")
				return
			}
			fmt.Printf("K20Mon database unavailable (attempt %d): %v\n", attempt, err)
			time.Sleep(10 * time.Second)
		}
	}()
}

func loadConfig() (config, error) {
	result := config{
		Addr:          env("K20MON_ADDR", ":8888"),
		JWTKey:        env("K20MON_JWT_KEY", ""),
		JWTIssuer:     env("K20MON_JWT_ISSUER", ""),
		JWTExpires:    7 * 24 * time.Hour,
		ControlSocket: env("K20M_CONTROL_SOCKET", "/run/k20mon-status/control.sock"),
		Interval:      5 * time.Second,
	}
	if raw := os.Getenv("K20M_INTERVAL"); raw != "" {
		if seconds, err := strconv.ParseFloat(raw, 64); err == nil && seconds > 0 {
			result.Interval = time.Duration(seconds * float64(time.Second))
		}
	}
	if raw := os.Getenv("K20MON_JWT_EXPIRES"); raw != "" {
		if value, err := time.ParseDuration(raw); err == nil {
			result.JWTExpires = value
		}
	}

	var file fileConfig
	if data, err := os.ReadFile(env("K20MON_CONFIG_PATH", "/etc/k20mon/config.yaml")); err == nil {
		_ = yaml.Unmarshal(data, &file)
	}
	if result.JWTKey == "" {
		result.JWTKey = file.JWT.SigningKey
	}
	if result.JWTIssuer == "" {
		result.JWTIssuer = file.JWT.Issuer
	}
	if result.Addr == ":8888" && file.System.Addr > 0 {
		result.Addr = ":" + strconv.Itoa(file.System.Addr)
	}
	if result.JWTKey == "" {
		return result, errors.New("K20MON_JWT_KEY or jwt.signing-key is required")
	}
	if result.JWTIssuer == "" {
		result.JWTIssuer = "k20mon"
	}

	if raw := os.Getenv("K20MON_MYSQL_DSN"); raw != "" {
		result.MySQLDSN = raw
		return result, nil
	}
	port := file.MySQL.Port
	if port == "" {
		port = "3306"
	}
	params := file.MySQL.Config
	if params == "" {
		params = "charset=utf8mb4&parseTime=True&loc=Local"
	}
	if file.MySQL.Username == "" || file.MySQL.DBName == "" {
		return result, errors.New("K20MON_MYSQL_DSN or mysql config is required")
	}
	result.MySQLDSN = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?%s",
		file.MySQL.Username, file.MySQL.Password, file.MySQL.Path, port, file.MySQL.DBName, params)
	return result, nil
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func (a *application) router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": "k20mon"})
	})
	r.POST("/base/login", a.login)

	private := r.Group("/")
	private.Use(a.auth())
	private.GET("/user/getUserInfo", a.getUserInfo)
	private.GET("/k20mon/metrics/current", a.getCurrent)
	private.GET("/k20mon/metrics/history", a.getHistory)
	private.GET("/k20mon/metrics/summary", a.getSummary)
	private.GET("/k20mon/metrics/stream", a.stream)

	ops := private.Group("/k20mon/operations")
	ops.GET("/charge-guard", a.callAction("charge_guard_get", nil))
	ops.PUT("/charge-guard", a.callAction("charge_guard_set", func(c *gin.Context) (map[string]any, error) {
		var input model.ChargeGuardRequest
		if err := c.ShouldBindJSON(&input); err != nil {
			return nil, err
		}
		data := map[string]any{}
		if input.Enabled != nil {
			data["enabled"] = *input.Enabled
		}
		if input.LowThreshold != nil {
			data["low_threshold"] = *input.LowThreshold
		}
		if input.ResumeThreshold != nil {
			data["resume_threshold"] = *input.ResumeThreshold
		}
		if input.LowCurrentUA != nil {
			data["low_current_ua"] = *input.LowCurrentUA
		}
		if input.NormalCurrentUA != nil {
			data["normal_current_ua"] = *input.NormalCurrentUA
		}
		return data, nil
	}))
	ops.GET("/wifi/status", a.callAction("wifi_status", nil))
	ops.POST("/wifi/scan", a.callAction("wifi_scan", func(c *gin.Context) (map[string]any, error) {
		return map[string]any{"interface": c.Query("interface")}, nil
	}))
	ops.POST("/wifi/connect", a.callAction("wifi_connect", bind[model.WifiConnectRequest](
		func(input model.WifiConnectRequest) map[string]any {
			return map[string]any{"interface": input.Interface, "ssid": input.SSID, "password": input.Password}
		},
	)))
	ops.POST("/wifi/disconnect", a.callAction("wifi_disconnect", bind[model.WifiDisconnectRequest](
		func(input model.WifiDisconnectRequest) map[string]any {
			return map[string]any{"interface": input.Interface}
		},
	)))
	ops.GET("/cloudflare/status", a.callAction("cloudflare_status", nil))
	ops.GET("/rate-sync", a.callAction("rate_sync_status", nil))
	ops.PUT("/rate-sync/config", a.callAction("rate_sync_config",
		bind[map[string]any](func(input map[string]any) map[string]any { return input })))
	ops.POST("/rate-sync/run", a.callAction("rate_sync_run", nil))
	ops.GET("/frp/config", a.callAction("frp_config", nil))
	ops.PUT("/frp/config", a.callAction("frp_apply", bind[model.FrpApplyRequest](
		func(input model.FrpApplyRequest) map[string]any {
			return map[string]any{"tunnels": input.Tunnels}
		},
	)))
	ops.POST("/frp/restart", a.callAction("frp_restart", nil))

	ops.GET("/services/status", a.servicesStatus)
	ops.PUT("/services/owner", a.updateServiceOwner)
	ops.POST("/services", a.createService)
	ops.PUT("/services", a.updateService)
	ops.POST("/services/order", a.reorderServices)
	ops.DELETE("/services/:id", a.deleteService)

	return r
}

// bind turns a typed request body into the loose map the control protocol uses.
func bind[T any](transform func(T) map[string]any) func(*gin.Context) (map[string]any, error) {
	return func(c *gin.Context) (map[string]any, error) {
		var input T
		if err := c.ShouldBindJSON(&input); err != nil {
			return nil, err
		}
		return transform(input), nil
	}
}

func (a *application) login(c *gin.Context) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Username == "" || input.Password == "" {
		fail(c, "用户名和密码不能为空")
		return
	}
	db := service.Conn()
	if db == nil {
		fail(c, "数据库暂不可用，请稍后重试")
		return
	}
	var user userRecord
	err := db.Where("username = ?", input.Username).First(&user).Error
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.Password)) != nil || user.Enable != 1 {
		fail(c, "用户名不存在或者密码错误")
		return
	}
	token, err := a.sign(user)
	if err != nil {
		fail(c, "获取 token 失败")
		return
	}
	ok(c, gin.H{
		"user":      user,
		"token":     token,
		"expiresAt": time.Now().Add(a.cfg.JWTExpires).UnixMilli(),
	}, "登录成功")
}

func (a *application) sign(user userRecord) (string, error) {
	now := time.Now()
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		UserID:      user.ID,
		UUID:        user.UUID,
		Username:    user.Username,
		NickName:    user.NickName,
		AuthorityId: user.AuthorityId,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    a.cfg.JWTIssuer,
			Subject:   strconv.FormatUint(uint64(user.ID), 10),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Second)),
			ExpiresAt: jwt.NewNumericDate(now.Add(a.cfg.JWTExpires)),
		},
	}).SignedString([]byte(a.cfg.JWTKey))
}

func (a *application) auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader("x-token")
		if raw == "" {
			unauthorized(c, "未登录或非法访问，请登录")
			return
		}
		token, err := jwt.ParseWithClaims(raw, &claims{}, func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, errors.New("invalid signing method")
			}
			return []byte(a.cfg.JWTKey), nil
		})
		if err != nil || !token.Valid {
			unauthorized(c, "登录已过期，请重新登录")
			return
		}
		value, ok := token.Claims.(*claims)
		if !ok {
			unauthorized(c, "登录状态无效")
			return
		}
		c.Set("claims", value)
		c.Next()
	}
}

func (a *application) getUserInfo(c *gin.Context) {
	claim, _ := c.Get("claims")
	db := service.Conn()
	if db == nil {
		fail(c, "数据库暂不可用，请稍后重试")
		return
	}
	var user userRecord
	if err := db.First(&user, "id = ?", claim.(*claims).UserID).Error; err != nil {
		unauthorized(c, "用户不存在")
		return
	}
	ok(c, gin.H{"userInfo": user}, "获取成功")
}

func (a *application) getCurrent(c *gin.Context) {
	snapshot, err := service.Metrics.Current(c.Request.Context())
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, snapshot, "成功")
}

func (a *application) getHistory(c *gin.Context) {
	var query metricsRequest.MetricsHistoryRequest
	if err := c.ShouldBindQuery(&query); err != nil {
		fail(c, err.Error())
		return
	}
	history, err := service.Metrics.History(c.Request.Context(), query)
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, history, "成功")
}

func (a *application) getSummary(c *gin.Context) {
	var query metricsRequest.MetricsHistoryRequest
	if err := c.ShouldBindQuery(&query); err != nil {
		fail(c, err.Error())
		return
	}
	summary, err := service.Metrics.Summary(c.Request.Context(), query)
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, summary, "成功")
}

// stream pushes snapshots over Server-Sent Events for the live dashboard.
func (a *application) stream(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	updates, unsubscribe := service.Metrics.Subscribe(c.Request.Context())
	defer unsubscribe()
	if snapshot, err := service.Metrics.Current(c.Request.Context()); err == nil {
		writeSSE(c, snapshot)
	}
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case snapshot, open := <-updates:
			if !open || writeSSE(c, snapshot) != nil {
				return
			}
		}
	}
}

func writeSSE(c *gin.Context, snapshot model.Metrics) error {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, "event: metrics\ndata: %s\n\n", payload); err != nil {
		return err
	}
	if flusher, ok := c.Writer.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func (a *application) callAction(action string, body func(*gin.Context) (map[string]any, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		var data map[string]any
		if body != nil {
			var err error
			if data, err = body(c); err != nil {
				fail(c, err.Error())
				return
			}
		}
		result, err := service.Operations.Call(c.Request.Context(), action, data)
		if err != nil {
			fail(c, err.Error())
			return
		}
		if !result.OK {
			message := result.Error
			if message == "" {
				message = "操作失败"
			}
			fail(c, message)
			return
		}
		ok(c, result, "成功")
	}
}

func (a *application) servicesStatus(c *gin.Context) {
	rows, err := service.Catalog.List(c.Request.Context())
	if err != nil {
		fail(c, err.Error())
		return
	}
	result, err := service.Operations.Call(c.Request.Context(), "services_status",
		map[string]any{"services": serviceRows(rows)})
	if err != nil {
		fail(c, err.Error())
		return
	}
	if !result.OK {
		fail(c, result.Error)
		return
	}
	ok(c, result, "成功")
}

func serviceRows(rows []model.ServiceCatalog) []map[string]any {
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result = append(result, map[string]any{
			"id": row.ID, "name": row.Name, "description": row.Description,
			"category": row.Category, "owner_type": row.OwnerType,
			"sort_order": row.SortOrder, "url": row.URL, "enabled": row.Enabled,
		})
	}
	return result
}

func (a *application) updateServiceOwner(c *gin.Context) {
	var input model.ServiceOwnerUpdateRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, err.Error())
		return
	}
	row, err := service.Catalog.UpdateOwner(c.Request.Context(), input.ID,
		strings.ToLower(strings.TrimSpace(input.OwnerType)))
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{"ok": true, "message": "服务归属已更新", "service": row}, "成功")
}

func (a *application) createService(c *gin.Context) {
	var input model.ServiceConfigRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, err.Error())
		return
	}
	row, err := service.Catalog.Create(c.Request.Context(), toCatalog(input))
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{"ok": true, "message": "服务已添加", "service": row}, "成功")
}

func (a *application) updateService(c *gin.Context) {
	var input model.ServiceConfigRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, err.Error())
		return
	}
	row, err := service.Catalog.Update(c.Request.Context(), toCatalog(input))
	if err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{"ok": true, "message": "服务配置已更新", "service": row}, "成功")
}

func toCatalog(input model.ServiceConfigRequest) model.ServiceCatalog {
	owner := input.OwnerType
	if owner == "" {
		owner = input.OwnerTypeLegacy
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	return model.ServiceCatalog{
		ID:          strings.TrimSpace(input.ID),
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		Category:    strings.TrimSpace(input.Category),
		OwnerType:   strings.ToLower(strings.TrimSpace(owner)),
		URL:         strings.TrimSpace(input.URL),
		Enabled:     enabled,
	}
}

func (a *application) reorderServices(c *gin.Context) {
	var input model.ServiceOrderRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		fail(c, err.Error())
		return
	}
	if err := service.Catalog.Reorder(c.Request.Context(), input.IDs); err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{"ok": true, "message": "服务顺序已保存"}, "成功")
}

func (a *application) deleteService(c *gin.Context) {
	if err := service.Catalog.Delete(c.Request.Context(), strings.TrimSpace(c.Param("id"))); err != nil {
		fail(c, err.Error())
		return
	}
	ok(c, gin.H{"ok": true, "message": "服务已删除", "id": c.Param("id")}, "成功")
}

func ok(c *gin.Context, data any, message string) {
	writeJSON(c, http.StatusOK, gin.H{"code": 0, "data": data, "msg": message})
}

func fail(c *gin.Context, message string) {
	writeJSON(c, http.StatusOK, gin.H{"code": 7, "data": gin.H{}, "msg": message})
}

func unauthorized(c *gin.Context, message string) {
	writeJSON(c, http.StatusUnauthorized, gin.H{"code": 7, "data": nil, "msg": message})
	c.Abort()
}

func writeJSON(c *gin.Context, status int, value any) {
	payload, err := json.Marshal(value)
	if err != nil {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	c.Data(status, "application/json; charset=utf-8", payload)
}
