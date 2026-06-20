package model

type PickupRequest struct {
	RequestID      string `json:"request_id" binding:"required,max=64"`
	IdempotentKey  string `json:"idempotent_key" binding:"required,max=128"`
	ShelfID        string `json:"shelf_id" binding:"required,max=64"`
	UserID         string `json:"user_id" binding:"required,max=64"`
	OrderID        string `json:"order_id" binding:"omitempty,max=64"`
	ProductID      string `json:"product_id" binding:"required,max=64"`
	Quantity       int    `json:"quantity" binding:"required,min=1,max=100"`
	SlotNo         int    `json:"slot_no" binding:"required,min=1"`
	DeviceID       string `json:"device_id" binding:"omitempty,max=64"`
}

type PickupResponse struct {
	TransactionID string `json:"transaction_id"`
	ShelfID       string `json:"shelf_id"`
	ProductID     string `json:"product_id"`
	Quantity      int    `json:"quantity"`
	StockBefore   int    `json:"stock_before"`
	StockAfter    int    `json:"stock_after"`
	UnitPrice     int64  `json:"unit_price"`
	TotalAmount   int64  `json:"total_amount"`
	PickupAt      int64  `json:"pickup_at"`
	IsDuplicate   bool   `json:"is_duplicate"`
}

type ShelfLockRequest struct {
	RequestID     string `json:"request_id" binding:"required,max=64"`
	IdempotentKey string `json:"idempotent_key" binding:"required,max=128"`
	ShelfID       string `json:"shelf_id" binding:"required,max=64"`
	OperatorID    string `json:"operator_id" binding:"required,max=64"`
	OperatorName  string `json:"operator_name" binding:"required,max=64"`
	LockType      int    `json:"lock_type" binding:"required,oneof=1 2 3"`
	Reason        string `json:"reason" binding:"required,max=512"`
	LockSeconds   int64  `json:"lock_seconds" binding:"required,min=60,max=86400"`
}

type ShelfLockResponse struct {
	LockID      int64  `json:"lock_id"`
	ShelfID     string `json:"shelf_id"`
	LockType    int    `json:"lock_type"`
	LockUntil   int64  `json:"lock_until"`
	LockedAt    int64  `json:"locked_at"`
	IsDuplicate bool   `json:"is_duplicate"`
}

type ShelfUnlockRequest struct {
	RequestID     string `json:"request_id" binding:"required,max=64"`
	IdempotentKey string `json:"idempotent_key" binding:"required,max=128"`
	ShelfID       string `json:"shelf_id" binding:"required,max=64"`
	OperatorID    string `json:"operator_id" binding:"required,max=64"`
	OperatorName  string `json:"operator_name" binding:"required,max=64"`
	Reason        string `json:"reason" binding:"required,max=512"`
}

type ShelfUnlockResponse struct {
	ShelfID    string `json:"shelf_id"`
	UnlockedAt int64  `json:"unlocked_at"`
}

type ShelfStockInfo struct {
	ShelfID     string `json:"shelf_id"`
	ProductID   string `json:"product_id"`
	SKU         string `json:"sku"`
	ProductName string `json:"product_name"`
	SlotNo      int    `json:"slot_no"`
	Quantity    int    `json:"quantity"`
	MaxCapacity int    `json:"max_capacity"`
	UnitPrice   int64  `json:"unit_price"`
	IsLowStock  bool   `json:"is_low_stock"`
	IsSoldOut   bool   `json:"is_sold_out"`
	ETagVersion int64  `json:"etag_version"`
}

type ShelfStatusInfo struct {
	ShelfID   string      `json:"shelf_id"`
	Status    ShelfStatus `json:"status"`
	IsLocked  bool        `json:"is_locked"`
	LockType  int         `json:"lock_type,omitempty"`
	LockUntil int64       `json:"lock_until,omitempty"`
	Reason    string      `json:"reason,omitempty"`
}

type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	TraceID string      `json:"trace_id,omitempty"`
	Now     int64       `json:"now"`
}
