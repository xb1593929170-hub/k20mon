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

// DefaultServiceCatalog 只在首次启动且表为空时写入。
// 这里刻意只放占位示例，不含任何真实服务；请在界面上改成自己的。
// 如果 root 代理的 /etc/k20mon/services.json 已有条目，会用那份数据覆盖这里的种子。
func DefaultServiceCatalog() []ServiceCatalog {
	rows := []ServiceCatalog{
		{ID: "k20mon", Name: "K20Mon", Description: "设备监控与操作中心", Category: "监控", OwnerType: "mine", URL: "http://127.0.0.1:8899", Enabled: true},
		{ID: "example-panel", Name: "管理面板", Description: "替换成你自己的服务地址", Category: "管理", OwnerType: "mine", URL: "https://example.com/panel", Enabled: false},
		{ID: "example-tools", Name: "在线工具", Description: "替换成你自己的服务地址", Category: "工具", OwnerType: "mine", URL: "https://example.com/tools", Enabled: false},
	}
	for index := range rows {
		rows[index].SortOrder = index
	}
	return rows
}
