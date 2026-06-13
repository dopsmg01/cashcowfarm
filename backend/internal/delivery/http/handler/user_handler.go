package handler

import (
	"net/http"

	"cashcowvalley/backend/internal/usecase"
	"cashcowvalley/backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type UserHandler struct {
	userUC *usecase.UserUsecase
}

func NewUserHandler(userUC *usecase.UserUsecase) *UserHandler {
	return &UserHandler{userUC: userUC}
}

// BindReferrerHandler handles the request to link a user to an upline referrer.
func (h *UserHandler) BindReferrerHandler(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.SendError(c, http.StatusUnauthorized, "User ID tidak valid", nil)
		return
	}

	var req struct {
		ReferrerWallet string `json:"referrer_wallet" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	// 1. Usecase Logic: Bind Referrer preventing cycles
	if err := h.userUC.BindReferrer(c.Request.Context(), userID, req.ReferrerWallet); err != nil {
		utils.SendError(c, http.StatusConflict, err.Error(), nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Referrer successfully bound!", nil, nil)
}

// GetReferralStatsHandler returns the user's referral link, eligibility (Cow ownership), and total invites.
func (h *UserHandler) GetReferralStatsHandler(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.SendError(c, http.StatusUnauthorized, "User ID tidak valid", nil)
		return
	}

	stats, err := h.userUC.GetReferralStats(c.Request.Context(), userID)
	if err != nil {
		utils.SendError(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Referral stats berhasil diambil", stats, nil)
}
