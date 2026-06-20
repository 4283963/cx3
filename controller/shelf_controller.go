package controller

import (
	"cx3/model"
	"cx3/service"
	"cx3/utils"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"go.uber.org/zap"
)

type ShelfController struct {
	shelfService *service.ShelfService
}

func NewShelfController(shelfService *service.ShelfService) *ShelfController {
	return &ShelfController{
		shelfService: shelfService,
	}
}

func (ctl *ShelfController) Pickup(c *gin.Context) {
	var req model.PickupRequest
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		utils.SugarLogger.Warnw("pickup invalid request",
			zap.String("trace_id", utils.GetTraceID(c)),
			zap.Error(err),
		)
		utils.Fail(c, http.StatusBadRequest, utils.CodeBadRequest, "参数错误: "+err.Error())
		return
	}

	ctx := utils.ContextWithTraceID(c.Request.Context(), utils.GetTraceID(c))
	clientIP := utils.GetClientIP(c)

	result := ctl.shelfService.Pickup(ctx, &req, clientIP)

	if result.ErrCode == utils.CodeSuccess {
		utils.Success(c, result.Response)
		return
	}

	switch result.ErrCode {
	case utils.CodeStockNotEnough:
		utils.Fail(c, http.StatusOK, utils.CodeStockNotEnough, result.Err.Error())
	case utils.CodeShelfLocked:
		utils.Fail(c, http.StatusOK, utils.CodeShelfLocked, result.Err.Error())
	case utils.CodeShelfOffline:
		utils.Fail(c, http.StatusOK, utils.CodeShelfOffline, result.Err.Error())
	case utils.CodeProductNotFound:
		utils.Fail(c, http.StatusOK, utils.CodeProductNotFound, result.Err.Error())
	case utils.CodeSlotMismatch:
		utils.Fail(c, http.StatusOK, utils.CodeSlotMismatch, result.Err.Error())
	case utils.CodeServiceUnavailable:
		utils.Fail(c, http.StatusServiceUnavailable, utils.CodeServiceUnavailable, result.Err.Error())
	default:
		utils.Fail(c, http.StatusInternalServerError, utils.CodeInternalError, result.Err.Error())
	}
}

func (ctl *ShelfController) Lock(c *gin.Context) {
	var req model.ShelfLockRequest
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		utils.SugarLogger.Warnw("lock shelf invalid request",
			zap.String("trace_id", utils.GetTraceID(c)),
			zap.Error(err),
		)
		utils.Fail(c, http.StatusBadRequest, utils.CodeBadRequest, "参数错误: "+err.Error())
		return
	}

	ctx := utils.ContextWithTraceID(c.Request.Context(), utils.GetTraceID(c))

	result := ctl.shelfService.LockShelf(ctx, &req)

	if result.ErrCode == utils.CodeSuccess {
		utils.Success(c, result.Response)
		return
	}

	switch result.ErrCode {
	case utils.CodeNotFound:
		utils.Fail(c, http.StatusOK, utils.CodeNotFound, result.Err.Error())
	case utils.CodeShelfOffline:
		utils.Fail(c, http.StatusOK, utils.CodeShelfOffline, result.Err.Error())
	case utils.CodeServiceUnavailable:
		utils.Fail(c, http.StatusServiceUnavailable, utils.CodeServiceUnavailable, result.Err.Error())
	default:
		utils.Fail(c, http.StatusInternalServerError, utils.CodeInternalError, result.Err.Error())
	}
}

func (ctl *ShelfController) Unlock(c *gin.Context) {
	var req model.ShelfUnlockRequest
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		utils.SugarLogger.Warnw("unlock shelf invalid request",
			zap.String("trace_id", utils.GetTraceID(c)),
			zap.Error(err),
		)
		utils.Fail(c, http.StatusBadRequest, utils.CodeBadRequest, "参数错误: "+err.Error())
		return
	}

	ctx := utils.ContextWithTraceID(c.Request.Context(), utils.GetTraceID(c))

	result := ctl.shelfService.UnlockShelf(ctx, &req)

	if result.ErrCode == utils.CodeSuccess {
		utils.Success(c, result.Response)
		return
	}

	switch result.ErrCode {
	case utils.CodeNotFound:
		utils.Fail(c, http.StatusOK, utils.CodeNotFound, result.Err.Error())
	case utils.CodeServiceUnavailable:
		utils.Fail(c, http.StatusServiceUnavailable, utils.CodeServiceUnavailable, result.Err.Error())
	default:
		utils.Fail(c, http.StatusInternalServerError, utils.CodeInternalError, result.Err.Error())
	}
}

func (ctl *ShelfController) GetStatus(c *gin.Context) {
	shelfID := c.Param("shelf_id")
	if shelfID == "" {
		utils.Fail(c, http.StatusBadRequest, utils.CodeBadRequest, "shelf_id is required")
		return
	}

	ctx := utils.ContextWithTraceID(c.Request.Context(), utils.GetTraceID(c))

	info, err := ctl.shelfService.GetShelfStatus(ctx, shelfID)
	if err != nil {
		utils.SugarLogger.Errorw("get shelf status failed",
			zap.String("trace_id", utils.GetTraceID(c)),
			zap.String("shelf_id", shelfID),
			zap.Error(err),
		)
		utils.Fail(c, http.StatusInternalServerError, utils.CodeInternalError, err.Error())
		return
	}

	utils.Success(c, info)
}

func (ctl *ShelfController) GetStock(c *gin.Context) {
	shelfID := c.Param("shelf_id")
	slotNoStr := c.Param("slot_no")
	if shelfID == "" || slotNoStr == "" {
		utils.Fail(c, http.StatusBadRequest, utils.CodeBadRequest, "shelf_id and slot_no are required")
		return
	}

	slotNo, err := strconv.Atoi(slotNoStr)
	if err != nil || slotNo <= 0 {
		utils.Fail(c, http.StatusBadRequest, utils.CodeBadRequest, "slot_no must be positive integer")
		return
	}

	ctx := utils.ContextWithTraceID(c.Request.Context(), utils.GetTraceID(c))

	info, err := ctl.shelfService.GetStock(ctx, shelfID, slotNo)
	if err != nil {
		utils.SugarLogger.Errorw("get stock failed",
			zap.String("trace_id", utils.GetTraceID(c)),
			zap.String("shelf_id", shelfID),
			zap.Int("slot_no", slotNo),
			zap.Error(err),
		)
		if err == service.ErrSlotMismatch {
			utils.Fail(c, http.StatusOK, utils.CodeSlotMismatch, err.Error())
			return
		}
		utils.Fail(c, http.StatusInternalServerError, utils.CodeInternalError, err.Error())
		return
	}

	utils.Success(c, info)
}

func (ctl *ShelfController) SelfCheck(c *gin.Context) {
	shelfID := c.Param("shelf_id")
	maxSlotStr := c.DefaultQuery("max_slot", "50")

	maxSlot, err := strconv.Atoi(maxSlotStr)
	if err != nil || maxSlot <= 0 {
		maxSlot = 50
	}

	ctx := utils.ContextWithTraceID(c.Request.Context(), utils.GetTraceID(c))
	result, err := ctl.shelfService.SelfCheck(ctx, shelfID, maxSlot)
	if err != nil {
		utils.Fail(c, http.StatusInternalServerError, utils.CodeInternalError, err.Error())
		return
	}

	utils.Success(c, result)
}

func (ctl *ShelfController) GetAuditLogs(c *gin.Context) {
	shelfID := c.Param("shelf_id")
	limitStr := c.DefaultQuery("limit", "100")

	limit, err := strconv.ParseInt(limitStr, 10, 64)
	if err != nil || limit <= 0 {
		limit = 100
	}

	ctx := utils.ContextWithTraceID(c.Request.Context(), utils.GetTraceID(c))
	logs, err := ctl.shelfService.GetAuditLogs(ctx, shelfID, limit)
	if err != nil {
		utils.Fail(c, http.StatusInternalServerError, utils.CodeInternalError, err.Error())
		return
	}

	utils.Success(c, gin.H{
		"shelf_id":    shelfID,
		"total_count": len(logs),
		"logs":        logs,
	})
}

func (ctl *ShelfController) SetPromo(c *gin.Context) {
	var req model.SetPromoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, utils.CodeBadRequest, err.Error())
		return
	}

	ctx := utils.ContextWithTraceID(c.Request.Context(), utils.GetTraceID(c))
	result := ctl.shelfService.SetPromo(ctx, &req)
	if result.ErrCode != utils.CodeSuccess {
		msg := result.Err.Error()
		if msg == "" {
			msg = "设置促销失败"
		}
		utils.Fail(c, http.StatusOK, result.ErrCode, msg)
		return
	}
	utils.Success(c, result.Response)
}

func (ctl *ShelfController) CancelPromo(c *gin.Context) {
	var req model.CancelPromoRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Fail(c, http.StatusBadRequest, utils.CodeBadRequest, err.Error())
		return
	}

	ctx := utils.ContextWithTraceID(c.Request.Context(), utils.GetTraceID(c))
	result := ctl.shelfService.CancelPromo(ctx, &req)
	if result.ErrCode != utils.CodeSuccess {
		msg := result.Err.Error()
		if msg == "" {
			msg = "取消促销失败"
		}
		utils.Fail(c, http.StatusOK, result.ErrCode, msg)
		return
	}
	utils.Success(c, result.Response)
}

func (ctl *ShelfController) GetPromo(c *gin.Context) {
	shelfID := c.Param("shelf_id")
	slotNoStr := c.Param("slot_no")
	if shelfID == "" || slotNoStr == "" {
		utils.Fail(c, http.StatusBadRequest, utils.CodeBadRequest, "shelf_id and slot_no are required")
		return
	}
	slotNo, err := strconv.Atoi(slotNoStr)
	if err != nil || slotNo <= 0 {
		utils.Fail(c, http.StatusBadRequest, utils.CodeBadRequest, "slot_no must be positive integer")
		return
	}

	ctx := utils.ContextWithTraceID(c.Request.Context(), utils.GetTraceID(c))
	result, err := ctl.shelfService.GetPromo(ctx, shelfID, slotNo)
	if err != nil {
		utils.Fail(c, http.StatusInternalServerError, utils.CodeInternalError, err.Error())
		return
	}
	utils.Success(c, result)
}
