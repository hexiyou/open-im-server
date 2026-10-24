-- =====================================================================
-- V1__init_xunlink.sql · 讯联IM 平台层与租户层初始化建表
-- 对应数据库字典：docs/dev/数据库字典.md T-01 ~ T-10
-- 约定：utf8mb4 / InnoDB；平台层表 tenant_id=0；租户层表强制 tenant_id
-- 执行时机：运行环境就绪后（M0-D01 决议暂缓），由部署脚本执行
-- 变更必须与数据库字典同提交（开发规范 §4）
-- =====================================================================

-- ---------------------------------------------------------------------
-- T-01 tenant 租户
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tenant` (
  `id`               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `tenant_code`      VARCHAR(32)     NOT NULL COMMENT '租户标识/子域名，全局唯一，如 xingyao',
  `name`             VARCHAR(64)     NOT NULL COMMENT '商户名称',
  `industry`         VARCHAR(32)     DEFAULT NULL COMMENT '行业',
  `plan_id`          BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '当前套餐 T-02.id',
  `seats_purchased`  INT             NOT NULL DEFAULT 0 COMMENT '已购客服席位数',
  `status`           TINYINT         NOT NULL DEFAULT 0 COMMENT '0=待审核 1=正常 2=停用',
  `expire_at`        DATETIME(3)     DEFAULT NULL COMMENT 'SaaS 服务期到期时间（私有化形态不使用）',
  `guest_widget_on`  TINYINT         NOT NULL DEFAULT 1 COMMENT '访客组件开关 1=开',
  `config`           JSON            DEFAULT NULL COMMENT '商户配置（结构见字典 §5）',
  `created_at`       DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`       DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at`       DATETIME(3)     DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_tenant_code` (`tenant_code`),
  KEY `idx_status_plan` (`status`, `plan_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='T-01 租户';

-- ---------------------------------------------------------------------
-- T-02 plan 套餐
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `plan` (
  `id`              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `name`            VARCHAR(64)     NOT NULL COMMENT '套餐名（基础版/标准版/旗舰版/私有化授权）',
  `max_members`     INT             NOT NULL DEFAULT 0 COMMENT '成员数上限 0=不限',
  `max_seats`       INT             NOT NULL DEFAULT 0 COMMENT '席位上限',
  `max_storage_gb`  INT             NOT NULL DEFAULT 0 COMMENT '存储上限 GB',
  `max_monthly_msg` BIGINT          NOT NULL DEFAULT 0 COMMENT '月消息量上限',
  `features`        JSON            NOT NULL DEFAULT ('{}') COMMENT '功能开关 {"guest_chat":true,"voice":true}',
  `price_month`     DECIMAL(10,2)   DEFAULT NULL COMMENT '月价（一期仅展示）',
  `on_shelf`        TINYINT         NOT NULL DEFAULT 1 COMMENT '1=上架',
  `created_at`      DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`      DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at`      DATETIME(3)     DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `idx_on_shelf` (`on_shelf`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='T-02 套餐';

-- ---------------------------------------------------------------------
-- T-03 platform_admin 总后台账号（独立账号体系，独立 JWT issuer）
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `platform_admin` (
  `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `username`      VARCHAR(64)     NOT NULL COMMENT '登录名，全局唯一',
  `password_hash` VARCHAR(128)    NOT NULL COMMENT 'bcrypt',
  `role`          VARCHAR(16)     NOT NULL COMMENT 'super/ops/readonly（docs/03 §4.1）',
  `totp_on`       TINYINT         NOT NULL DEFAULT 0 COMMENT '双因子开关',
  `status`        TINYINT         NOT NULL DEFAULT 1 COMMENT '1=正常 0=禁用',
  `last_login_at` DATETIME(3)     DEFAULT NULL,
  `created_at`    DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`    DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_username` (`username`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='T-03 平台管理员';

-- ---------------------------------------------------------------------
-- T-04 license 激活码/私有化授权（一机一码）
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `license` (
  `id`              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `license_no`      VARCHAR(32)     NOT NULL COMMENT '授权编号 L202610240001',
  `merchant_name`   VARCHAR(64)     NOT NULL COMMENT '商户名',
  `tenant_code`     VARCHAR(32)     DEFAULT NULL COMMENT '关联租户编码（私有化实例自建租户后回填）',
  `machine_fp`      CHAR(64)        NOT NULL COMMENT '机器指纹 sha256（签发即绑定）',
  `apply_code`      TEXT            DEFAULT NULL COMMENT '商户提交的申请码原文',
  `activation_code` TEXT            DEFAULT NULL COMMENT '签发的激活码（加密存储）',
  `not_before`      BIGINT          NOT NULL COMMENT '生效时间戳（秒）',
  `not_after`       BIGINT          NOT NULL COMMENT '到期时间戳（秒）',
  `grace_days`      INT             NOT NULL DEFAULT 7 COMMENT '离线宽限天数',
  `seats`           INT             NOT NULL DEFAULT 0 COMMENT '席位上限',
  `features`        JSON            NOT NULL DEFAULT ('[]') COMMENT '功能开关数组',
  `mode`            VARCHAR(8)      NOT NULL DEFAULT 'online' COMMENT 'online=心跳 offline=离线',
  `status`          VARCHAR(16)     NOT NULL DEFAULT 'unactivated' COMMENT 'unactivated/activated/grace/degraded/revoked',
  `activated_at`    BIGINT          DEFAULT NULL COMMENT '首次激活时间戳（秒）',
  `last_heartbeat`  BIGINT          DEFAULT NULL COMMENT '最近心跳时间戳（秒）',
  `instance_info`   JSON            DEFAULT NULL COMMENT '心跳回传 版本/主机名/用量',
  `created_at`      DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`      DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_license_no` (`license_no`),
  KEY `idx_machine_fp` (`machine_fp`),
  KEY `idx_status` (`status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='T-04 激活码/私有化授权';

-- ---------------------------------------------------------------------
-- T-05 audit_log 操作审计（只写不改不删，无 updated_at/deleted_at）
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `audit_log` (
  `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `tenant_id`   BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '租户域（平台操作=0）',
  `actor_type`  VARCHAR(16)     NOT NULL COMMENT 'platform/tenant/system',
  `actor_id`    BIGINT UNSIGNED NOT NULL COMMENT '操作人ID',
  `actor_name`  VARCHAR(64)     NOT NULL COMMENT '操作人名（冗余）',
  `action`      VARCHAR(64)     NOT NULL COMMENT '动作码 如 tenant.create/license.revoke',
  `target`      VARCHAR(128)    DEFAULT NULL COMMENT '操作对象描述',
  `detail`      JSON            DEFAULT NULL COMMENT '前后快照摘要',
  `ip`          VARCHAR(45)     DEFAULT NULL COMMENT '操作IP（IPv6兼容）',
  `created_at`  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  KEY `idx_tenant_time` (`tenant_id`, `created_at`),
  KEY `idx_actor` (`actor_id`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='T-05 操作审计';

-- ---------------------------------------------------------------------
-- T-06 tenant_admin 商户后台账号（独立于平台账号体系）
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tenant_admin` (
  `id`            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `tenant_id`     BIGINT UNSIGNED NOT NULL COMMENT '所属租户',
  `username`      VARCHAR(64)     NOT NULL COMMENT '登录名，租户内唯一',
  `password_hash` VARCHAR(128)    NOT NULL COMMENT 'bcrypt',
  `role`          VARCHAR(16)     NOT NULL DEFAULT 'agent' COMMENT 'tenant_admin/class_leader/agent（docs/03 §4.2）',
  `status`        TINYINT         NOT NULL DEFAULT 1 COMMENT '1=正常 0=禁用',
  `created_at`    DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`    DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at`    DATETIME(3)     DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_tenant_username` (`tenant_id`, `username`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='T-06 商户后台账号';

-- ---------------------------------------------------------------------
-- T-07 staff 客服/成员（映射 OpenIM User，im_user_id=t{tid}_u{uid}）
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `staff` (
  `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `tenant_id`   BIGINT UNSIGNED NOT NULL COMMENT '所属租户',
  `im_user_id`  VARCHAR(64)     NOT NULL COMMENT 'OpenIM userID，全局唯一',
  `name`        VARCHAR(64)     NOT NULL COMMENT '姓名',
  `mobile`      VARCHAR(20)     DEFAULT NULL COMMENT '手机号',
  `role`        VARCHAR(16)     NOT NULL DEFAULT 'agent' COMMENT 'admin/class_leader/agent',
  `seat_on`     TINYINT         NOT NULL DEFAULT 1 COMMENT '是否占用坐席席位',
  `status`      TINYINT         NOT NULL DEFAULT 1 COMMENT '1=在职 0=禁用',
  `created_at`  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  `deleted_at`  DATETIME(3)     DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_im_user_id` (`im_user_id`),
  KEY `idx_tenant_status` (`tenant_id`, `status`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='T-07 客服/成员';

-- ---------------------------------------------------------------------
-- T-08 guest 匿名访客（im_user_id=t{tid}_g{rand}）
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `guest` (
  `id`              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `tenant_id`       BIGINT UNSIGNED NOT NULL COMMENT '所属租户',
  `im_user_id`      VARCHAR(64)     NOT NULL COMMENT 'OpenIM userID，全局唯一',
  `history_key`     CHAR(32)        NOT NULL COMMENT '续聊凭证（访客端 localStorage 持有）',
  `fingerprint`     VARCHAR(128)    DEFAULT NULL COMMENT 'IP+UA 哈希（风控/黑名单）',
  `source_url`      VARCHAR(255)    DEFAULT NULL COMMENT '首次咨询来源页面',
  `status`          TINYINT         NOT NULL DEFAULT 1 COMMENT '1=活跃 2=归档 3=拉黑',
  `last_active_at`  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '归档清理依据（默认30天）',
  `created_at`      DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`      DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_im_user_id` (`im_user_id`),
  KEY `idx_tenant_active` (`tenant_id`, `last_active_at`),
  KEY `idx_history_key` (`history_key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='T-08 匿名访客';

-- ---------------------------------------------------------------------
-- T-09 agent_session 客服接待记录（挂接 OpenIM 会话）
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `agent_session` (
  `id`                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `tenant_id`         BIGINT UNSIGNED NOT NULL COMMENT '所属租户',
  `guest_im_user_id`  VARCHAR(64)     NOT NULL COMMENT '访客',
  `agent_im_user_id`  VARCHAR(64)     DEFAULT NULL COMMENT '接待坐席（排队中为 NULL）',
  `conversation_id`   VARCHAR(64)     NOT NULL COMMENT 'OpenIM 会话ID',
  `source_url`        VARCHAR(255)    DEFAULT NULL COMMENT '来源页面',
  `status`            VARCHAR(16)     NOT NULL DEFAULT 'queueing' COMMENT 'queueing/active/transferred/closed',
  `claimed_at`        DATETIME(3)     DEFAULT NULL COMMENT '坐席认领时间',
  `closed_at`         DATETIME(3)     DEFAULT NULL COMMENT '结束时间',
  `rating`            TINYINT         DEFAULT NULL COMMENT '满意度 1-5',
  `created_at`        DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`        DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`id`),
  KEY `idx_tenant_status` (`tenant_id`, `status`, `claimed_at`),
  KEY `idx_conv` (`conversation_id`),
  KEY `idx_agent_time` (`agent_im_user_id`, `claimed_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='T-09 客服接待记录';

-- ---------------------------------------------------------------------
-- T-10 tenant_quota_usage 配额用量日汇总
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS `tenant_quota_usage` (
  `tenant_id`      BIGINT UNSIGNED NOT NULL COMMENT '租户',
  `date`           DATE            NOT NULL COMMENT '统计日',
  `members`        INT             NOT NULL DEFAULT 0 COMMENT '当日成员数快照',
  `online_agents`  INT             NOT NULL DEFAULT 0 COMMENT '峰值在线坐席',
  `messages`       BIGINT          NOT NULL DEFAULT 0 COMMENT '当日消息量',
  `storage_bytes`  BIGINT          NOT NULL DEFAULT 0 COMMENT '存储字节',
  `created_at`     DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at`     DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (`tenant_id`, `date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='T-10 配额用量日汇总';

-- =====================================================================
-- T-90/T-91 OpenIM 原生表加列（原生表由 OpenIM 自身在启动时自动建表，
-- 本段需在 OpenIM 首次启动建表之后执行；执行顺序在部署脚本中保证。
-- 注意：MySQL 8.0 不支持 ADD COLUMN IF NOT EXISTS，版本化迁移只执行一次，
-- 重复执行会报 Duplicate column——由迁移记录表保证不重复执行。）
-- =====================================================================

-- T-90 users 增加 tenant_id（users 表实际列名以 OpenIM 建表为准，联调时校验）
ALTER TABLE `users`
  ADD COLUMN `tenant_id` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'XunLink: 所属租户（平台注册=0）';
ALTER TABLE `users`
  ADD INDEX `idx_tenant_user` (`tenant_id`, `user_id`);

-- T-91 friends 增加 tenant_id（好友备注名使用原生 remark 字段，不加列）
ALTER TABLE `friends`
  ADD COLUMN `tenant_id` BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'XunLink: 所属租户';
ALTER TABLE `friends`
  ADD INDEX `idx_tenant_owner` (`tenant_id`, `owner_user_id`);
