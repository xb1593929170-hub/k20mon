package model

import "time"

type ServiceCatalog struct {
	ID          string    `gorm:"primaryKey;size:64" json:"id"`
	Name        string    `gorm:"size:120;not null" json:"name"`
	Description string    `gorm:"size:240" json:"description"`
	Category    string    `gorm:"size:60;not null" json:"category"`
	OwnerType   string    `gorm:"size:16;not null;index" json:"owner_type"`
	URL         string    `gorm:"size:2048;not null" json:"url"`
	Enabled     bool      `gorm:"not null;default:true;index" json:"enabled"`
	SortOrder   int       `gorm:"not null;default:0;index" json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (ServiceCatalog) TableName() string {
	return "k20mon_service_catalog"
}

// DefaultServiceCatalog seeds an empty catalog on first start. It only runs
// when the table has no rows, and the root agent's own catalog wins when it
// already has entries. Replace the placeholder URLs with your own services.
func DefaultServiceCatalog() []ServiceCatalog {
	rows := []ServiceCatalog{
		{ID: "k20mon", Name: "K20Mon", Description: "设备监控与操作中心", Category: "监控", OwnerType: "mine", URL: "http://127.0.0.1:8899", Enabled: true},
		{ID: "1panel", Name: "1Panel", Description: "服务器管理面板", Category: "管理", OwnerType: "mine", URL: "https://example.com/1panel", Enabled: false},
		{ID: "it-tools", Name: "IT Tools", Description: "常用开发与运维工具", Category: "工具", OwnerType: "mine", URL: "https://example.com/it-tools", Enabled: false},
	}
	for index := range rows {
		rows[index].SortOrder = index
	}
	return rows
}
