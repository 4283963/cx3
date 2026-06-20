CREATE DATABASE IF NOT EXISTS `cx3_shelf` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

USE `cx3_shelf`;

DROP TABLE IF EXISTS `shelf`;
CREATE TABLE `shelf` (
    `shelf_id`    VARCHAR(64)   NOT NULL COMMENT '货架唯一ID',
    `store_id`    VARCHAR(64)   NOT NULL COMMENT '门店ID',
    `name`        VARCHAR(128)  NOT NULL COMMENT '货架名称',
    `status`      TINYINT       NOT NULL DEFAULT 0 COMMENT '状态:0正常1锁定2离线',
    `zone`        VARCHAR(64)   DEFAULT NULL COMMENT '区域位置',
    `created_at`  DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`  DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`shelf_id`),
    KEY `idx_store_id` (`store_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='货架信息表';

DROP TABLE IF EXISTS `shelf_lock_record`;
CREATE TABLE `shelf_lock_record` (
    `id`            BIGINT        NOT NULL AUTO_INCREMENT,
    `shelf_id`      VARCHAR(64)   NOT NULL COMMENT '货架ID',
    `operator_id`   VARCHAR(64)   NOT NULL COMMENT '操作员ID',
    `operator`      VARCHAR(64)   NOT NULL COMMENT '操作员姓名',
    `lock_type`     TINYINT       NOT NULL DEFAULT 1 COMMENT '锁定类型:1重力异常2运营锁定3系统锁定',
    `reason`        VARCHAR(512)  NOT NULL COMMENT '锁定原因',
    `lock_until`    BIGINT        NOT NULL COMMENT '锁定截止时间戳(秒)',
    `is_unlock`     TINYINT       NOT NULL DEFAULT 0 COMMENT '是否已解锁:0否1是',
    `unlock_by`     VARCHAR(64)   DEFAULT NULL COMMENT '解锁人',
    `unlock_at`     BIGINT        DEFAULT NULL COMMENT '解锁时间戳',
    `unlock_reason` VARCHAR(512)  DEFAULT NULL COMMENT '解锁原因',
    `created_at`    DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_shelf_id` (`shelf_id`),
    KEY `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='货架锁定记录表';

DROP TABLE IF EXISTS `product`;
CREATE TABLE `product` (
    `product_id`  VARCHAR(64)   NOT NULL COMMENT '商品ID',
    `sku`         VARCHAR(64)   NOT NULL COMMENT 'SKU编码',
    `barcode`     VARCHAR(64)   NOT NULL COMMENT '条码',
    `name`        VARCHAR(256)  NOT NULL COMMENT '商品名称',
    `category_id` VARCHAR(64)   DEFAULT NULL COMMENT '分类ID',
    `price`       BIGINT        NOT NULL COMMENT '单价(分)',
    `cost`        BIGINT        DEFAULT NULL COMMENT '成本(分)',
    `image_url`   VARCHAR(512)  DEFAULT NULL COMMENT '图片地址',
    `is_active`   TINYINT       NOT NULL DEFAULT 1 COMMENT '是否上架:0否1是',
    `created_at`  DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`  DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`product_id`),
    UNIQUE KEY `uk_sku` (`sku`),
    KEY `idx_barcode` (`barcode`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='商品信息表';

DROP TABLE IF EXISTS `shelf_product`;
CREATE TABLE `shelf_product` (
    `id`           BIGINT        NOT NULL AUTO_INCREMENT,
    `shelf_id`     VARCHAR(64)   NOT NULL COMMENT '货架ID',
    `slot_no`      INT           NOT NULL COMMENT '货道号',
    `product_id`   VARCHAR(64)   NOT NULL COMMENT '商品ID',
    `max_capacity` INT           NOT NULL DEFAULT 0 COMMENT '最大容量',
    `sort_order`   INT           NOT NULL DEFAULT 0 COMMENT '排序',
    `created_at`   DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`   DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_shelf_slot` (`shelf_id`, `slot_no`),
    KEY `idx_product_id` (`product_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='货架商品关联表';

DROP TABLE IF EXISTS `transaction`;
CREATE TABLE `transaction` (
    `id`              BIGINT        NOT NULL AUTO_INCREMENT,
    `transaction_id`  VARCHAR(64)   NOT NULL COMMENT '交易流水号',
    `order_id`        VARCHAR(64)   DEFAULT NULL COMMENT '订单ID',
    `user_id`         VARCHAR(64)   DEFAULT NULL COMMENT '用户ID',
    `shelf_id`        VARCHAR(64)   NOT NULL COMMENT '货架ID',
    `store_id`        VARCHAR(64)   DEFAULT NULL COMMENT '门店ID',
    `product_id`      VARCHAR(64)   NOT NULL COMMENT '商品ID',
    `sku`             VARCHAR(64)   DEFAULT NULL COMMENT 'SKU编码',
    `product_name`    VARCHAR(256)  DEFAULT NULL COMMENT '商品名称快照',
    `slot_no`         INT           DEFAULT NULL COMMENT '货道号',
    `quantity`        INT           NOT NULL COMMENT '数量',
    `unit_price`      BIGINT        NOT NULL COMMENT '单价(分)',
    `total_amount`    BIGINT        NOT NULL COMMENT '总金额(分)',
    `tx_type`         TINYINT       NOT NULL DEFAULT 1 COMMENT '交易类型:1取货2补货3盘点调整4退货',
    `tx_status`       TINYINT       NOT NULL DEFAULT 1 COMMENT '状态:1待确认2已确认3已取消4异常',
    `idempotent_key`  VARCHAR(128)  DEFAULT NULL COMMENT '幂等键',
    `request_id`      VARCHAR(64)   DEFAULT NULL COMMENT '请求ID',
    `client_ip`       VARCHAR(64)   DEFAULT NULL COMMENT '客户端IP',
    `remark`          VARCHAR(512)  DEFAULT NULL COMMENT '备注',
    `stock_before`    INT           DEFAULT NULL COMMENT '扣减前库存',
    `stock_after`     INT           DEFAULT NULL COMMENT '扣减后库存',
    `confirm_at`      BIGINT        DEFAULT NULL COMMENT '确认时间戳',
    `created_at`      DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`      DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_transaction_id` (`transaction_id`),
    UNIQUE KEY `uk_idempotent_key` (`idempotent_key`),
    KEY `idx_shelf_id` (`shelf_id`),
    KEY `idx_user_id` (`user_id`),
    KEY `idx_order_id` (`order_id`),
    KEY `idx_product_id` (`product_id`),
    KEY `idx_store_id` (`store_id`),
    KEY `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci COMMENT='交易记录表';

INSERT INTO `shelf` (`shelf_id`, `store_id`, `name`, `status`, `zone`) VALUES
('SH001', 'ST001', 'A区一号货架', 0, 'A-01'),
('SH002', 'ST001', 'A区二号货架', 0, 'A-02'),
('SH003', 'ST001', 'B区一号货架', 0, 'B-01');

INSERT INTO `product` (`product_id`, `sku`, `barcode`, `name`, `category_id`, `price`, `cost`, `is_active`) VALUES
('P001', 'SKU0001', '6901234567890', '农夫山泉550ml', 'CAT001', 200, 100, 1),
('P002', 'SKU0002', '6901234567891', '康师傅冰红茶500ml', 'CAT001', 300, 150, 1),
('P003', 'SKU0003', '6901234567892', '可口可乐330ml', 'CAT001', 250, 120, 1),
('P004', 'SKU0004', '6901234567893', '乐事薯片原味75g', 'CAT002', 650, 320, 1),
('P005', 'SKU0005', '6901234567894', '士力架巧克力51g', 'CAT003', 500, 250, 1);

INSERT INTO `shelf_product` (`shelf_id`, `slot_no`, `product_id`, `max_capacity`, `sort_order`) VALUES
('SH001', 1, 'P001', 30, 1),
('SH001', 2, 'P002', 30, 2),
('SH001', 3, 'P003', 30, 3),
('SH001', 4, 'P004', 20, 4),
('SH001', 5, 'P005', 25, 5),
('SH002', 1, 'P001', 30, 1),
('SH002', 2, 'P003', 30, 2),
('SH003', 1, 'P002', 30, 1);
