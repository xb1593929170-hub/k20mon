package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/yourname/k20mon/server/model"
)

var Operations = NewOperationService()

var ErrControlUnavailable = errors.New("k20mon root control agent is unavailable")

// allowedControlActions mirrors the dispatch table in deploy/agent/k20mon_agent.py.
// Adding an action here without adding it there returns "不支持的操作".
var allowedControlActions = map[string]struct{}{
	"charge_guard_get":  {},
	"charge_guard_set":  {},
	"wifi_status":       {},
	"wifi_scan":         {},
	"wifi_connect":      {},
	"wifi_disconnect":   {},
	"cloudflare_status": {},
	"rate_sync_status":  {},
	"rate_sync_config":  {},
	"rate_sync_run":     {},
	"services_status":   {},
	"services_sync":     {},
	"frp_config":        {},
	"frp_apply":         {},
	"frp_restart":       {},
}

type OperationService struct {
	client *ControlClient
}

func NewOperationService() *OperationService {
	return &OperationService{client: NewControlClient()}
}

func (s *OperationService) Call(ctx context.Context, action string, data map[string]any) (model.ControlResponse, error) {
	return s.client.Call(ctx, action, data)
}

type ControlClient struct {
	socketPath string
	timeout    time.Duration
}

func NewControlClient() *ControlClient {
	return &ControlClient{timeout: 35 * time.Second}
}

// socket resolves the control socket lazily: Operations is built during package
// init, which happens before main() applies the config file's socket path.
func (c *ControlClient) socket() string {
	if path := os.Getenv("K20M_CONTROL_SOCKET"); path != "" {
		return path
	}
	if c.socketPath != "" {
		return c.socketPath
	}
	return "/run/k20mon-status/control.sock"
}

func (c *ControlClient) Call(ctx context.Context, action string, data map[string]any) (model.ControlResponse, error) {
	if action == "" {
		return model.ControlResponse{}, errors.New("control action is required")
	}
	if _, ok := allowedControlActions[action]; !ok {
		return model.ControlResponse{}, fmt.Errorf("unsupported control action: %s", action)
	}
	if data == nil {
		data = map[string]any{}
	}
	payload, err := json.Marshal(struct {
		Action string         `json:"action"`
		Data   map[string]any `json:"data"`
	}{Action: action, Data: data})
	if err != nil {
		return model.ControlResponse{}, err
	}

	dialer := net.Dialer{Timeout: c.timeout}
	conn, err := dialer.DialContext(ctx, "unix", c.socket())
	if err != nil {
		return model.ControlResponse{}, fmt.Errorf("%w: %v", ErrControlUnavailable, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.timeout))
	if _, err := fmt.Fprintf(conn, "%s\n", payload); err != nil {
		return model.ControlResponse{}, err
	}

	var response model.ControlResponse
	if err := json.NewDecoder(bufio.NewReader(conn)).Decode(&response); err != nil {
		return model.ControlResponse{}, err
	}
	return response, nil
}
