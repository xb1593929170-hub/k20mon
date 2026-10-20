package service

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/yourname/k20mon/server/model"
)

var Catalog = NewServiceCatalogService()

// DB holds the shared connection pool. It may stay nil for a while: the server
// boots even when MySQL is unreachable and attaches the pool once it connects,
// so the dashboard keeps working through a database outage.
var db atomic.Pointer[gorm.DB]

var ErrStoreUnavailable = errors.New("k20mon database is unavailable")

func SetDB(value *gorm.DB) {
	if value == nil {
		db.Store(nil)
		return
	}
	db.Store(value)
}

func conn() *gorm.DB { return db.Load() }

// Conn exposes the pool to the HTTP layer; it returns nil until the database
// is reachable, so callers must handle the unavailable case.
func Conn() *gorm.DB { return db.Load() }

func storeAvailable() bool { return conn() != nil }

var serviceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type ServiceCatalogService struct{}

func NewServiceCatalogService() *ServiceCatalogService {
	return &ServiceCatalogService{}
}

func (s *ServiceCatalogService) List(ctx context.Context) ([]model.ServiceCatalog, error) {
	conn := conn()
	if conn == nil {
		return nil, ErrStoreUnavailable
	}
	var rows []model.ServiceCatalog
	err := conn.WithContext(ctx).Order("sort_order asc, created_at asc, id asc").Find(&rows).Error
	return rows, err
}

func (s *ServiceCatalogService) Create(ctx context.Context, row model.ServiceCatalog) (model.ServiceCatalog, error) {
	if err := validateServiceCatalog(row); err != nil {
		return model.ServiceCatalog{}, err
	}
	conn := conn()
	if conn == nil {
		return model.ServiceCatalog{}, ErrStoreUnavailable
	}
	var maxSortOrder int
	if err := conn.WithContext(ctx).Model(&model.ServiceCatalog{}).
		Select("COALESCE(MAX(sort_order), -1)").Scan(&maxSortOrder).Error; err != nil {
		return model.ServiceCatalog{}, err
	}
	row.SortOrder = maxSortOrder + 1
	if err := conn.WithContext(ctx).Create(&row).Error; err != nil {
		return model.ServiceCatalog{}, err
	}
	s.syncAgent(ctx)
	return row, nil
}

func (s *ServiceCatalogService) Update(ctx context.Context, row model.ServiceCatalog) (model.ServiceCatalog, error) {
	if err := validateServiceCatalog(row); err != nil {
		return model.ServiceCatalog{}, err
	}
	conn := conn()
	if conn == nil {
		return model.ServiceCatalog{}, ErrStoreUnavailable
	}
	var current model.ServiceCatalog
	if err := conn.WithContext(ctx).First(&current, "id = ?", row.ID).Error; err != nil {
		return model.ServiceCatalog{}, err
	}
	row.CreatedAt = current.CreatedAt
	row.SortOrder = current.SortOrder
	if err := conn.WithContext(ctx).Save(&row).Error; err != nil {
		return model.ServiceCatalog{}, err
	}
	s.syncAgent(ctx)
	return row, nil
}

func (s *ServiceCatalogService) Reorder(ctx context.Context, ids []string) error {
	conn := conn()
	if conn == nil {
		return ErrStoreUnavailable
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) != id || !serviceIDPattern.MatchString(id) {
			return errors.New("服务排序数据无效")
		}
		if _, exists := seen[id]; exists {
			return errors.New("服务排序中存在重复 ID")
		}
		seen[id] = struct{}{}
	}

	err := conn.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var currentIDs []string
		if err := tx.Model(&model.ServiceCatalog{}).Order("sort_order asc, created_at asc, id asc").
			Pluck("id", &currentIDs).Error; err != nil {
			return err
		}
		if len(currentIDs) != len(ids) {
			return errors.New("服务列表已变化，请刷新后重试排序")
		}
		currentSet := make(map[string]struct{}, len(currentIDs))
		for _, id := range currentIDs {
			currentSet[id] = struct{}{}
		}
		for index, id := range ids {
			if _, exists := currentSet[id]; !exists {
				return errors.New("服务排序数据不完整")
			}
			if err := tx.Model(&model.ServiceCatalog{}).Where("id = ?", id).
				Update("sort_order", index).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.syncAgent(ctx)
	return nil
}

func (s *ServiceCatalogService) UpdateOwner(ctx context.Context, id, ownerType string) (model.ServiceCatalog, error) {
	if ownerType != "mine" && ownerType != "other" {
		return model.ServiceCatalog{}, errors.New("服务归属必须是 mine 或 other")
	}
	conn := conn()
	if conn == nil {
		return model.ServiceCatalog{}, ErrStoreUnavailable
	}
	var row model.ServiceCatalog
	if err := conn.WithContext(ctx).First(&row, "id = ?", id).Error; err != nil {
		return model.ServiceCatalog{}, err
	}
	row.OwnerType = ownerType
	if err := conn.WithContext(ctx).Save(&row).Error; err != nil {
		return model.ServiceCatalog{}, err
	}
	s.syncAgent(ctx)
	return row, nil
}

func (s *ServiceCatalogService) Delete(ctx context.Context, id string) error {
	conn := conn()
	if conn == nil {
		return ErrStoreUnavailable
	}
	result := conn.WithContext(ctx).Delete(&model.ServiceCatalog{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	s.syncAgent(ctx)
	return nil
}

// EnsureSeed fills an empty catalog from the root agent (or the built-in
// defaults) so a fresh database still shows something useful.
func (s *ServiceCatalogService) EnsureSeed(ctx context.Context) {
	conn := conn()
	if conn == nil {
		return
	}
	var count int64
	if err := conn.WithContext(ctx).Model(&model.ServiceCatalog{}).Count(&count).Error; err != nil || count > 0 {
		return
	}

	seed := model.DefaultServiceCatalog()
	seedCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if result, err := Operations.Call(seedCtx, "services_status", nil); err == nil && result.OK && len(result.Services) > 0 {
		seed = make([]model.ServiceCatalog, 0, len(result.Services))
		for index, item := range result.Services {
			row, ok := serviceFromMap(item)
			if ok {
				row.SortOrder = index
				seed = append(seed, row)
			}
		}
	}
	if len(seed) > 0 {
		_ = conn.WithContext(ctx).Create(&seed).Error
	}
}

// syncAgent mirrors the MySQL catalog into the root agent's JSON cache. The
// database stays authoritative; the cache exists so the agent can answer
// services_status without a database round trip.
func (s *ServiceCatalogService) syncAgent(ctx context.Context) {
	rows, err := s.List(ctx)
	if err != nil {
		return
	}
	services := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		services = append(services, map[string]any{
			"id": row.ID, "name": row.Name, "description": row.Description,
			"category": row.Category, "owner_type": row.OwnerType, "sort_order": row.SortOrder,
			"url": row.URL, "enabled": row.Enabled,
		})
	}
	syncCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, _ = Operations.Call(syncCtx, "services_sync", map[string]any{"services": services})
}

func serviceFromMap(item map[string]any) (model.ServiceCatalog, bool) {
	id, _ := item["id"].(string)
	name, _ := item["name"].(string)
	description, _ := item["description"].(string)
	category, _ := item["category"].(string)
	ownerType, _ := item["owner_type"].(string)
	serviceURL, _ := item["url"].(string)
	enabled, _ := item["enabled"].(bool)
	sortOrder, _ := item["sort_order"].(float64)
	row := model.ServiceCatalog{
		ID: id, Name: name, Description: description, Category: category,
		OwnerType: ownerType, URL: serviceURL, Enabled: enabled, SortOrder: int(sortOrder),
	}
	return row, validateServiceCatalog(row) == nil
}

func validateServiceCatalog(row model.ServiceCatalog) error {
	if !serviceIDPattern.MatchString(strings.TrimSpace(row.ID)) {
		return errors.New("服务 ID 只能包含字母、数字、点、下划线和短横线")
	}
	if strings.TrimSpace(row.Name) == "" || len([]rune(row.Name)) > 120 {
		return errors.New("服务名称不能为空且不能超过 120 个字符")
	}
	if len([]rune(row.Description)) > 240 || len([]rune(row.Category)) > 60 {
		return errors.New("服务描述或分类过长")
	}
	if row.OwnerType != "mine" && row.OwnerType != "other" {
		return errors.New("服务归属必须是 mine 或 other")
	}
	parsed, err := url.ParseRequestURI(strings.TrimSpace(row.URL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("服务地址必须是 http 或 https URL")
	}
	return nil
}
