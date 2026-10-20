package model

// ControlResponse 是 root 代理回包的扁平结构，各动作按需填其中几个字段。
// 注意：计数类字段刻意不加 omitempty —— 否则在线服务数为 0 时字段会消失，
// 前端拿到 undefined 而不是 0。
type ControlResponse struct {
	OK           bool             `json:"ok"`
	Message      string           `json:"message,omitempty"`
	Error        string           `json:"error,omitempty"`
	Detail       string           `json:"detail,omitempty"`
	Data         map[string]any   `json:"data,omitempty"`
	Config       map[string]any   `json:"config,omitempty"`
	Tunnels      map[string]any   `json:"tunnels,omitempty"`
	Wifi         map[string]any   `json:"wifi,omitempty"`
	Cloudflare   map[string]any   `json:"cloudflare,omitempty"`
	RateSync     map[string]any   `json:"rate_sync,omitempty"`
	Interfaces   []map[string]any `json:"interfaces,omitempty"`
	Networks     []map[string]any `json:"networks,omitempty"`
	Services     []map[string]any `json:"services,omitempty"`
	Interface    string           `json:"interface,omitempty"`
	Mode         string           `json:"mode,omitempty"`
	Total        int              `json:"total"`
	Online       int              `json:"online"`
	Offline      int              `json:"offline"`
	CheckedAt    float64          `json:"checked_at"`
	Service      map[string]any   `json:"service,omitempty"`
	Capacity     *int64           `json:"capacity,omitempty"`
	CurrentMaxUA *int64           `json:"current_max_ua,omitempty"`
}

type ChargeGuardRequest struct {
	Enabled         *bool `json:"enabled"`
	LowThreshold    *int  `json:"low_threshold"`
	ResumeThreshold *int  `json:"resume_threshold"`
	LowCurrentUA    *int  `json:"low_current_ua"`
	NormalCurrentUA *int  `json:"normal_current_ua"`
}

type WifiConnectRequest struct {
	Interface string `json:"interface"`
	SSID      string `json:"ssid"`
	Password  string `json:"password"`
}

type WifiDisconnectRequest struct {
	Interface string `json:"interface"`
}

type FrpApplyRequest struct {
	Tunnels []map[string]any `json:"tunnels"`
}

type ServiceOwnerUpdateRequest struct {
	ID        string `json:"id"`
	OwnerType string `json:"ownerType"`
}

type ServiceConfigRequest struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Category        string `json:"category"`
	OwnerType       string `json:"ownerType"`
	OwnerTypeLegacy string `json:"owner_type"`
	URL             string `json:"url"`
	Enabled         *bool  `json:"enabled"`
}

type ServiceOrderRequest struct {
	IDs []string `json:"ids"`
}
