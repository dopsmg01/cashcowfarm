package handler

import (
	"encoding/json"
	"net/http"

	"cashcowvalley/backend/internal/usecase"
	"cashcowvalley/backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type GameHandler struct {
	farmUC      *usecase.FarmUsecase
	marketUC    *usecase.MarketUsecase
	adWebhookUC *usecase.AdWebhookUsecase
	userUC      *usecase.UserUsecase
}

func NewGameHandler(farmUC *usecase.FarmUsecase, marketUC *usecase.MarketUsecase, adWebhookUC *usecase.AdWebhookUsecase, userUC *usecase.UserUsecase) *GameHandler {
	return &GameHandler{
		farmUC:      farmUC,
		marketUC:    marketUC,
		adWebhookUC: adWebhookUC,
		userUC:      userUC,
	}
}

// FeedCowHandler - POST /api/v1/farm/feed
func (h *GameHandler) FeedCowHandler(c *gin.Context) {
	// Extract user_id from JWT context (set by auth middleware)
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.SendError(c, http.StatusUnauthorized, "User ID tidak valid", nil)
		return
	}

	var req struct {
		CowID string `json:"cow_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.SendError(c, http.StatusBadRequest, "Format payload salah", nil)
		return
	}

	cowID, err := uuid.Parse(req.CowID)
	if err != nil {
		utils.SendError(c, http.StatusBadRequest, "Cow ID tidak valid", nil)
		return
	}

	// Execute Business Logic
	if err := h.farmUC.FeedCow(c.Request.Context(), userID, cowID); err != nil {
		utils.SendError(c, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Sapi berhasil diberi makan!", nil, nil)
}

// HarvestFarmHandler - POST /api/v1/farm/harvest
// Enforces Web2 Care Mechanics: Fails or yields 0 if ad-watching quota is not met.
func (h *GameHandler) HarvestFarmHandler(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.SendError(c, http.StatusUnauthorized, "User ID tidak valid", nil)
		return
	}

	milkGained, err := h.farmUC.HarvestFarm(c.Request.Context(), userID)
	if err != nil {
		utils.SendError(c, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Susu berhasil dipanen!", gin.H{"milk_harvested": milkGained}, nil)
}

// BuyItemHandler - POST /api/v1/market/buy
func (h *GameHandler) BuyItemHandler(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.SendError(c, http.StatusUnauthorized, "User ID tidak valid", nil)
		return
	}

	var req struct {
		ListingID string `json:"listing_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.SendError(c, http.StatusBadRequest, "Format payload salah", nil)
		return
	}

	listingID, err := uuid.Parse(req.ListingID)
	if err != nil {
		utils.SendError(c, http.StatusBadRequest, "Listing ID tidak valid", nil)
		return
	}

	if err := h.marketUC.BuyItem(c.Request.Context(), userID, listingID); err != nil {
		utils.SendError(c, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Pembelian berhasil!", nil, nil)
}

// AdWebhookHandler - POST /api/v1/webhooks/ad-reward
// Secured via HMAC SHA256 Signature validation on the RAW Body
func (h *GameHandler) AdWebhookHandler(c *gin.Context) {
	signature := c.GetHeader("X-Ad-Signature")
	if signature == "" {
		utils.SendError(c, http.StatusUnauthorized, "Signature hilang", nil)
		return
	}

	// BUG FIX: Baca RAW BODY untuk validasi HMAC
	// Jika kita bind ke Struct lalu diubah jadi string lagi, spasinya bisa berubah dan Signature PASTI GAGAL (Invalid).
	rawBody, err := c.GetRawData()
	if err != nil {
		utils.SendError(c, http.StatusBadRequest, "Gagal membaca body", nil)
		return
	}

	// Ekstrak EventID (TransID) untuk mencegah Replay Attack
	var req struct {
		UserID  string `json:"user_id"`
		Reward  string `json:"reward"`
		EventID string `json:"event_id"` // Transaction ID unik dari AdNetwork
	}

	if err := json.Unmarshal(rawBody, &req); err != nil {
		utils.SendError(c, http.StatusBadRequest, "Format JSON tidak valid", nil)
		return
	}

	// Teruskan RAW Payload string untuk HMAC, beserta EventID untuk Idempotency
	if err := h.adWebhookUC.ProcessAdReward(c.Request.Context(), string(rawBody), signature, req.UserID, req.EventID); err != nil {
		// Return 200 even on fail to stop Ad Network from retrying indefinitely on bad sigs
		// BUG FIX: Jangan bocorkan pesan error internal ke luar (Info Leak Prevention)
		utils.SendError(c, http.StatusOK, "Webhook gagal diproses", nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Webhook Diterima", nil, nil)
}

// GetFarmStatusHandler - GET /api/v1/farm/status
func (h *GameHandler) GetFarmStatusHandler(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.SendError(c, http.StatusUnauthorized, "User ID tidak valid", nil)
		return
	}

	result, err := h.farmUC.GetFarmStatus(c.Request.Context(), userID)
	if err != nil {
		utils.SendError(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Data farm berhasil diambil", result, nil)
}

// GetMarketListingsHandler - GET /api/v1/market/listings
func (h *GameHandler) GetMarketListingsHandler(c *gin.Context) {
	listings, err := h.marketUC.GetListings(c.Request.Context())
	if err != nil {
		utils.SendError(c, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Listings berhasil diambil", listings, nil)
}

// SellItemHandler - POST /api/v1/market/sell
func (h *GameHandler) SellItemHandler(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.SendError(c, http.StatusUnauthorized, "User ID tidak valid", nil)
		return
	}

	var req struct {
		ItemType string `json:"item_type" binding:"required"`
		Quantity int    `json:"quantity" binding:"required,min=1"`
		Price    string `json:"price" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.SendError(c, http.StatusBadRequest, "Format payload salah", nil)
		return
	}

	priceUSDT, err := decimal.NewFromString(req.Price)
	if err != nil {
		utils.SendError(c, http.StatusBadRequest, "Format harga tidak valid", nil)
		return
	}

	if err := h.marketUC.SellItem(c.Request.Context(), userID, req.ItemType, req.Quantity, priceUSDT); err != nil {
		utils.SendError(c, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Item berhasil didaftarkan di marketplace!", nil, nil)
}

// BuyPlatformItemHandler handles purchasing items directly from the system/platform.
func (h *GameHandler) BuyPlatformItemHandler(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	buyerID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.SendError(c, http.StatusUnauthorized, "User ID tidak valid", nil)
		return
	}

	var req struct {
		ItemType string          `json:"item_type" binding:"required"`
		Quantity int             `json:"quantity" binding:"required"`
		Price    decimal.Decimal `json:"price" binding:"required"`
		Currency string          `json:"currency" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Format request tidak valid"})
		return
	}

	if err := h.marketUC.BuyFromPlatform(c.Request.Context(), buyerID, req.ItemType, req.Quantity, req.Price, req.Currency, h.userUC); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Berhasil melakukan pembelian dari platform",
	})
}

// SellMilkForGoldHandler - POST /api/v1/market/sell-milk-gold
func (h *GameHandler) SellMilkForGoldHandler(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.SendError(c, http.StatusUnauthorized, "User ID tidak valid", nil)
		return
	}

	var req struct {
		Quantity int `json:"quantity" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.SendError(c, http.StatusBadRequest, "Format payload salah", nil)
		return
	}

	if err := h.marketUC.SellMilkForGold(c.Request.Context(), userID, req.Quantity); err != nil {
		utils.SendError(c, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Susu berhasil ditukar dengan Gold!", nil, nil)
}

// BuyInAppItemHandler - POST /api/v1/market/buy-inapp-gold
func (h *GameHandler) BuyInAppItemHandler(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.SendError(c, http.StatusUnauthorized, "User ID tidak valid", nil)
		return
	}

	var req struct {
		ItemType string `json:"item_type" binding:"required"`
		Quantity int    `json:"quantity" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.SendError(c, http.StatusBadRequest, "Format payload salah", nil)
		return
	}

	if err := h.marketUC.BuyInAppItemWithGold(c.Request.Context(), userID, req.ItemType, req.Quantity); err != nil {
		utils.SendError(c, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Item berhasil dibeli dengan Gold!", nil, nil)
}

// SwapGoldHandler - POST /api/v1/market/swap-gold
func (h *GameHandler) SwapGoldHandler(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.SendError(c, http.StatusUnauthorized, "User ID tidak valid", nil)
		return
	}

	var req struct {
		Amount decimal.Decimal `json:"amount" binding:"required"`
		Target string          `json:"target" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.SendError(c, http.StatusBadRequest, "Format payload salah", nil)
		return
	}

	if err := h.marketUC.SwapGoldToTokens(c.Request.Context(), userID, req.Amount, req.Target); err != nil {
		utils.SendError(c, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Gold berhasil ditukar!", nil, nil)
}

// StakeInAppHandler - POST /api/v1/market/stake-inapp
func (h *GameHandler) StakeInAppHandler(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.SendError(c, http.StatusUnauthorized, "User ID tidak valid", nil)
		return
	}

	var req struct {
		AssetType string          `json:"asset_type" binding:"required"`
		Amount    decimal.Decimal `json:"amount" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.SendError(c, http.StatusBadRequest, "Format payload salah", nil)
		return
	}

	if err := h.marketUC.StakeInApp(c.Request.Context(), userID, req.AssetType, req.Amount); err != nil {
		utils.SendError(c, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Berhasil staking!", nil, nil)
}

// ClaimInAppHandler - POST /api/v1/market/claim-inapp
func (h *GameHandler) ClaimInAppHandler(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.SendError(c, http.StatusUnauthorized, "User ID tidak valid", nil)
		return
	}

	if err := h.marketUC.ClaimInAppRewards(c.Request.Context(), userID); err != nil {
		utils.SendError(c, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Reward berhasil diklaim!", nil, nil)
}

// DepositHandler - POST /api/v1/market/deposit
func (h *GameHandler) DepositHandler(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.SendError(c, http.StatusUnauthorized, "User ID tidak valid", nil)
		return
	}

	var req struct {
		Asset  string          `json:"asset" binding:"required"`
		Amount decimal.Decimal `json:"amount" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.SendError(c, http.StatusBadRequest, "Format payload salah", nil)
		return
	}

	if err := h.marketUC.Deposit(c.Request.Context(), userID, req.Asset, req.Amount); err != nil {
		utils.SendError(c, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Deposit sukses!", nil, nil)
}

// WithdrawHandler - POST /api/v1/market/withdraw
func (h *GameHandler) WithdrawHandler(c *gin.Context) {
	userIDStr := c.GetString("user_id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		utils.SendError(c, http.StatusUnauthorized, "User ID tidak valid", nil)
		return
	}

	var req struct {
		Asset  string          `json:"asset" binding:"required"`
		Amount decimal.Decimal `json:"amount" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.SendError(c, http.StatusBadRequest, "Format payload salah", nil)
		return
	}

	if err := h.marketUC.Withdraw(c.Request.Context(), userID, req.Asset, req.Amount); err != nil {
		utils.SendError(c, http.StatusUnprocessableEntity, err.Error(), nil)
		return
	}

	utils.SendSuccess(c, http.StatusOK, "Withdraw sukses!", nil, nil)
}
