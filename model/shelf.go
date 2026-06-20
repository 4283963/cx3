package model

import "time"

type ShelfStatus int

const (
	ShelfStatusNormal   ShelfStatus = 0
	ShelfStatusLocked   ShelfStatus = 1
	ShelfStatusOffline  ShelfStatus = 2
)

type Shelf struct {
	ShelfID     string      `json:"shelf_id" gorm:"column:shelf_id;primaryKey;type:varchar(64);not null;comment:货架唯一ID"`
	StoreID     string      `json:"store_id" gorm:"column:store_id;type:varchar(64);not null;index:idx_store_id;comment:门店ID"`
	Name        string      `json:"name" gorm:"column:name;type:varchar(128);not null;comment:货架名称"`
	Status      ShelfStatus `json:"status" gorm:"column:status;type:tinyint;not null;default:0;comment:状态:0正常1锁定2离线"`
	Zone        string      `json:"zone" gorm:"column:zone;type:varchar(64);comment:区域位置"`
	CreatedAt   time.Time   `json:"created_at" gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt   time.Time   `json:"updated_at" gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP"`
}

func (Shelf) TableName() string {
	return "shelf"
}

type ShelfLockRecord struct {
	ID          int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement;type:bigint"`
	ShelfID     string    `json:"shelf_id" gorm:"column:shelf_id;type:varchar(64);not null;index:idx_shelf_id;comment:货架ID"`
	OperatorID  string    `json:"operator_id" gorm:"column:operator_id;type:varchar(64);not null;comment:操作员ID"`
	Operator    string    `json:"operator" gorm:"column:operator;type:varchar(64);not null;comment:操作员姓名"`
	LockType    int       `json:"lock_type" gorm:"column:lock_type;type:tinyint;not null;default:1;comment:锁定类型:1重力异常2运营锁定3系统锁定"`
	Reason      string    `json:"reason" gorm:"column:reason;type:varchar(512);not null;comment:锁定原因"`
	LockUntil   int64     `json:"lock_until" gorm:"column:lock_until;type:bigint;not null;comment:锁定截止时间戳(秒)"`
	IsUnlock    int       `json:"is_unlock" gorm:"column:is_unlock;type:tinyint;not null;default:0;comment:是否已解锁:0否1是"`
	UnlockBy    string    `json:"unlock_by" gorm:"column:unlock_by;type:varchar(64);comment:解锁人"`
	UnlockAt    *int64    `json:"unlock_at" gorm:"column:unlock_at;type:bigint;comment:解锁时间戳"`
	UnlockReason string   `json:"unlock_reason" gorm:"column:unlock_reason;type:varchar(512);comment:解锁原因"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP"`
}

func (ShelfLockRecord) TableName() string {
	return "shelf_lock_record"
}

type Product struct {
	ProductID   string    `json:"product_id" gorm:"column:product_id;primaryKey;type:varchar(64);not null;comment:商品ID"`
	SKU         string    `json:"sku" gorm:"column:sku;type:varchar(64);not null;uniqueIndex:uk_sku;comment:SKU编码"`
	Barcode     string    `json:"barcode" gorm:"column:barcode;type:varchar(64);not null;index:idx_barcode;comment:条码"`
	Name        string    `json:"name" gorm:"column:name;type:varchar(256);not null;comment:商品名称"`
	CategoryID  string    `json:"category_id" gorm:"column:category_id;type:varchar(64);comment:分类ID"`
	Price       int64     `json:"price" gorm:"column:price;type:bigint;not null;comment:单价(分)"`
	Cost        int64     `json:"cost" gorm:"column:cost;type:bigint;comment:成本(分)"`
	ImageURL    string    `json:"image_url" gorm:"column:image_url;type:varchar(512);comment:图片地址"`
	IsActive    int       `json:"is_active" gorm:"column:is_active;type:tinyint;not null;default:1;comment:是否上架:0否1是"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP"`
}

func (Product) TableName() string {
	return "product"
}

type ShelfProduct struct {
	ID          int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement;type:bigint"`
	ShelfID     string    `json:"shelf_id" gorm:"column:shelf_id;type:varchar(64);not null;uniqueIndex:uk_shelf_slot;comment:货架ID"`
	SlotNo      int       `json:"slot_no" gorm:"column:slot_no;type:int;not null;uniqueIndex:uk_shelf_slot;comment:货道号"`
	ProductID   string    `json:"product_id" gorm:"column:product_id;type:varchar(64);not null;index:idx_product_id;comment:商品ID"`
	MaxCapacity int       `json:"max_capacity" gorm:"column:max_capacity;type:int;not null;default:0;comment:最大容量"`
	SortOrder   int       `json:"sort_order" gorm:"column:sort_order;type:int;not null;default:0;comment:排序"`
	CreatedAt   time.Time `json:"created_at" gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP"`
}

func (ShelfProduct) TableName() string {
	return "shelf_product"
}

type Transaction struct {
	ID              int64     `json:"id" gorm:"column:id;primaryKey;autoIncrement;type:bigint"`
	TransactionID   string    `json:"transaction_id" gorm:"column:transaction_id;type:varchar(64);not null;uniqueIndex:uk_transaction_id;comment:交易流水号"`
	OrderID         string    `json:"order_id" gorm:"column:order_id;type:varchar(64);index:idx_order_id;comment:订单ID"`
	UserID          string    `json:"user_id" gorm:"column:user_id;type:varchar(64);index:idx_user_id;comment:用户ID"`
	ShelfID         string    `json:"shelf_id" gorm:"column:shelf_id;type:varchar(64);not null;index:idx_shelf_id;comment:货架ID"`
	StoreID         string    `json:"store_id" gorm:"column:store_id;type:varchar(64);index:idx_store_id;comment:门店ID"`
	ProductID       string    `json:"product_id" gorm:"column:product_id;type:varchar(64);not null;index:idx_product_id;comment:商品ID"`
	SKU             string    `json:"sku" gorm:"column:sku;type:varchar(64);comment:SKU编码"`
	ProductName     string    `json:"product_name" gorm:"column:product_name;type:varchar(256);comment:商品名称快照"`
	SlotNo          int       `json:"slot_no" gorm:"column:slot_no;type:int;comment:货道号"`
	Quantity        int       `json:"quantity" gorm:"column:quantity;type:int;not null;comment:数量"`
	UnitPrice       int64     `json:"unit_price" gorm:"column:unit_price;type:bigint;not null;comment:成交单价(分)"`
	OriginalPrice   int64     `json:"original_price" gorm:"column:original_price;type:bigint;comment:商品原价(分)"`
	TotalAmount     int64     `json:"total_amount" gorm:"column:total_amount;type:bigint;not null;comment:总金额(分)"`
	PromoID         string    `json:"promo_id" gorm:"column:promo_id;type:varchar(64);index:idx_promo_id;comment:促销活动ID"`
	PromoName       string    `json:"promo_name" gorm:"column:promo_name;type:varchar(128);comment:促销活动名称"`
	TxType          int       `json:"tx_type" gorm:"column:tx_type;type:tinyint;not null;default:1;comment:交易类型:1取货2补货3盘点调整4退货"`
	TxStatus        int       `json:"tx_status" gorm:"column:tx_status;type:tinyint;not null;default:1;comment:状态:1待确认2已确认3已取消4异常"`
	IdempotentKey   string    `json:"idempotent_key" gorm:"column:idempotent_key;type:varchar(128);uniqueIndex:uk_idempotent_key;comment:幂等键"`
	RequestID       string    `json:"request_id" gorm:"column:request_id;type:varchar(64);comment:请求ID"`
	ClientIP        string    `json:"client_ip" gorm:"column:client_ip;type:varchar(64);comment:客户端IP"`
	Remark          string    `json:"remark" gorm:"column:remark;type:varchar(512);comment:备注"`
	StockBefore     int       `json:"stock_before" gorm:"column:stock_before;type:int;comment:扣减前库存"`
	StockAfter      int       `json:"stock_after" gorm:"column:stock_after;type:int;comment:扣减后库存"`
	ConfirmAt       *int64    `json:"confirm_at" gorm:"column:confirm_at;type:bigint;comment:确认时间戳"`
	CreatedAt       time.Time `json:"created_at" gorm:"column:created_at;type:datetime;not null;default:CURRENT_TIMESTAMP;index:idx_created_at"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"column:updated_at;type:datetime;not null;default:CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP"`
}

func (Transaction) TableName() string {
	return "transaction"
}

type ShelfPromotion struct {
	PromoID    string `json:"promo_id"`
	PromoName  string `json:"promo_name"`
	ShelfID    string `json:"shelf_id"`
	SlotNo     int    `json:"slot_no"`
	ProductID  string `json:"product_id"`
	PromoPrice int64  `json:"promo_price"`
	StartAt    int64  `json:"start_at"`
	EndAt      int64  `json:"end_at"`
	CreatedBy  string `json:"created_by"`
	CreatedAt  int64  `json:"created_at"`
}
