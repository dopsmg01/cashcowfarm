package handler

import (
	"net/http"
	"regexp"
	"strings"

	"cashcowvalley/backend/internal/usecase"

	"github.com/gin-gonic/gin"
)

var ethAddressRegex = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)

type AuthHandler struct {
	authUC *usecase.AuthUsecase
}

func NewAuthHandler(authUC *usecase.AuthUsecase) *AuthHandler {
	return &AuthHandler{authUC: authUC}
}

// GetNonceHandler returns a nonce for the given wallet address.
// GET /auth/nonce/:wallet
func (h *AuthHandler) GetNonceHandler(c *gin.Context) {
	wallet := strings.TrimSpace(c.Param("wallet"))
	if wallet == "" || !ethAddressRegex.MatchString(wallet) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Valid Ethereum wallet address is required (0x + 40 hex chars)"})
		return
	}

	nonce, exists, err := h.authUC.GetOrCreateNonce(c.Request.Context(), wallet)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate nonce"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"nonce":  nonce,
		"exists": exists,
	})
}

// LoginWithSignatureHandler processes Web3 Wallet logins with signature verification.
// POST /auth/login
func (h *AuthHandler) LoginWithSignatureHandler(c *gin.Context) {
	var req struct {
		WalletAddress  string `json:"wallet_address" binding:"required"`
		Signature      string `json:"signature" binding:"required"`
		Message        string `json:"message" binding:"required"`
		ReferrerWallet string `json:"referrer_wallet"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "Wallet address, signature, and message are required",
		})
		return
	}

	if !ethAddressRegex.MatchString(req.WalletAddress) {
		c.JSON(http.StatusBadRequest, gin.H{
			"status":  "error",
			"message": "Invalid Ethereum wallet address format",
		})
		return
	}

	token, err := h.authUC.LoginWithSignature(c.Request.Context(), req.WalletAddress, req.Signature, req.Message, req.ReferrerWallet)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"token":  token,
	})
}

// LoginOrRegisterHandler is the legacy login (no signature).
// Kept for backward compatibility and dev testing.
func (h *AuthHandler) LoginOrRegisterHandler(c *gin.Context) {
	var req struct {
		WalletAddress  string `json:"wallet_address" binding:"required"`
		ReferrerWallet string `json:"referrer_wallet"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Wallet address is required"})
		return
	}

	if !ethAddressRegex.MatchString(req.WalletAddress) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid Ethereum wallet address format"})
		return
	}

	token, err := h.authUC.LoginOrRegister(c.Request.Context(), req.WalletAddress, req.ReferrerWallet)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"token":  token,
	})
}
