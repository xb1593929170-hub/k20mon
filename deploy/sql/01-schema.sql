-- ---------------------------------------------------------------------------
-- K20Mon 数据库结构
--
-- 什么时候需要跑这个文件：
--   * 想先手工建库建表（比如 DBA 要先审 DDL）；
--   * 不想让后端账号有 DDL 权限（AutoMigrate 需要 CREATE/ALTER）。
--
-- 什么时候不需要：
--   * 后端启动时会用 GORM AutoMigrate 自动建 k20mon_* 两张表。
--     只跑「建库 + 建 sys_users」也能跑起来。
--
-- 表结构取自 GORM 在 MySQL 8.4 上实际生成的结果，逐字段核对过。
-- 用法：
--   mysql -h <host> -P <port> -u root -p < 01-schema.sql
-- ---------------------------------------------------------------------------

CREATE DATABASE IF NOT EXISTS `k20mon`
  DEFAULT CHARACTER SET utf8mb4
  DEFAULT COLLATE utf8mb4_general_ci;

USE `k20mon`;

-- ---------------------------------------------------------------------------
-- 账号表
--
-- 沿用 gin-vue-admin 的 sys_users 结构，后端只「读」它来登录：
--   server/main.go 的 userRecord 映射了 id/uuid/username/password/
--   nick_name/header_img/authority_id/phone/email/enable 这几个字段，
--   password 用 bcrypt 校验，enable != 1 视为禁用。
--
-- 如果你是从已有的 GVA 部署迁移，这张表已经存在且有数据，跳过本段。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `sys_users` (
  `id`           bigint unsigned NOT NULL AUTO_INCREMENT,
  `uuid`         varchar(191)    NOT NULL DEFAULT '',
  `username`     varchar(191)    NOT NULL,
  `password`     varchar(255)    NOT NULL COMMENT 'bcrypt hash',
  `nick_name`    varchar(191)    NOT NULL DEFAULT '',
  `header_img`   varchar(255)    NOT NULL DEFAULT '',
  `authority_id` bigint unsigned NOT NULL DEFAULT 888,
  `phone`        varchar(32)     NOT NULL DEFAULT '',
  `email`        varchar(191)    NOT NULL DEFAULT '',
  `enable`       int             NOT NULL DEFAULT 1 COMMENT '1=启用 0=禁用',
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_sys_users_username` (`username`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='登录账号（复用 GVA 结构）';

-- ---------------------------------------------------------------------------
-- 指标采样表
--
-- payload 存完整快照 JSON，其余列是从 payload 里冗余出来的图表/统计字段。
-- 冗余的原因：按时间范围画图和算电池容量时要扫几十万行，直接对 JSON 做
-- JSON_EXTRACT 太慢，所以把常用字段提成真列并建索引。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `k20mon_metric_samples` (
  `id`                          bigint unsigned NOT NULL AUTO_INCREMENT,
  `sampled_at`                  datetime(3)     NOT NULL COMMENT '采样时刻(UTC)',
  `payload`                     longtext        NOT NULL COMMENT '完整 Metrics JSON',
  `battery_capacity`            bigint          DEFAULT NULL COMMENT '电量 %',
  `battery_power_w`             double          DEFAULT NULL COMMENT '电池功率 W',
  `battery_consumption_w`       double          DEFAULT NULL COMMENT '整机功耗 W',
  `battery_temperature_c`       double          DEFAULT NULL COMMENT '电池温度 ℃',
  `battery_current_ma`          double          DEFAULT NULL COMMENT '电池电流 mA',
  `battery_voltage_v`           double          DEFAULT NULL COMMENT '电池电压 V',
  `battery_status`              varchar(24)     DEFAULT NULL COMMENT 'Charging/Discharging/...',
  `battery_design_capacity_uah` bigint          DEFAULT NULL COMMENT '设计容量 µAh',
  `cpu_usage_percent`           double          DEFAULT NULL COMMENT 'CPU 使用率 %',
  `hottest_temperature_c`       double          DEFAULT NULL COMMENT '最高温 ℃',
  `memory_used_percent`         double          DEFAULT NULL COMMENT '内存占用 %',
  `network_rxbps`               double          DEFAULT NULL COMMENT '下行 B/s',
  `network_txbps`               double          DEFAULT NULL COMMENT '上行 B/s',
  PRIMARY KEY (`id`),
  KEY `idx_k20mon_metric_samples_sampled_at` (`sampled_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='指标历史采样';

-- ---------------------------------------------------------------------------
-- 服务宫格目录
--
-- id 是业务主键（字符串），sort_order 控制拖拽顺序，owner_type 只有
-- mine / other 两个值（表示「我的服务」和「别人的地址」）。
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `k20mon_service_catalog` (
  `id`          varchar(64)  NOT NULL,
  `name`        varchar(120) NOT NULL,
  `description` varchar(240) DEFAULT NULL,
  `category`    varchar(60)  NOT NULL,
  `owner_type`  varchar(16)  NOT NULL COMMENT 'mine / other',
  `url`         varchar(2048) NOT NULL COMMENT 'http(s)://',
  `enabled`     tinyint(1)   NOT NULL DEFAULT 1,
  `created_at`  datetime(3)  DEFAULT NULL,
  `updated_at`  datetime(3)  DEFAULT NULL,
  `sort_order`  bigint       NOT NULL DEFAULT 0,
  PRIMARY KEY (`id`),
  KEY `idx_k20mon_service_catalog_owner_type` (`owner_type`),
  KEY `idx_k20mon_service_catalog_enabled` (`enabled`),
  KEY `idx_k20mon_service_catalog_sort_order` (`sort_order`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='服务宫格目录';

-- 服务目录的种子数据不放这里：后端首次启动且表为空时会自动写入
-- model.DefaultServiceCatalog()，或者从 root 代理的 /etc/k20mon/services.json 导入。
-- 想自定义初始数据，改那个 JSON 文件即可（见 deploy/services.json.example）。
