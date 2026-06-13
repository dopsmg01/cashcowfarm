package usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"cashcowvalley/backend/internal/domain"
	"cashcowvalley/backend/pkg/utils"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// DevWalletAddress is the root admin wallet. Hardcoded for security.
const DevWalletAddress = "0xbB9468c225C35BA3CBe441660EF9dE3a66Eb772A"

type AuthUsecase struct {
	db *gorm.DB
}

func NewAuthUsecase(db *gorm.DB) *AuthUsecase {
	return &AuthUsecase{db: db}
}

// SeedDevWallet ensures the dev/admin wallet is registered as root on startup.
func (uc *AuthUsecase) SeedDevWallet(ctx context.Context) {
	var user domain.User
	err := uc.db.WithContext(ctx).Where("wallet_address = ?", strings.ToLower(DevWalletAddress)).First(&user).Error
	if err == nil {
		// Already exists, ensure ADMIN role
		if user.Role != domain.RoleAdmin {
			if err := uc.db.WithContext(ctx).Model(&user).Update("role", domain.RoleAdmin).Error; err != nil {
				log.Printf("[SEED] Failed to upgrade dev wallet role: %v", err)
				return
			}
			log.Println("[SEED] Dev wallet role upgraded to ADMIN")
		}
		log.Printf("[SEED] Dev wallet already registered: %s", user.ID)
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("[SEED] Error checking dev wallet: %v", err)
		return
	}

	// Create the root admin user
	user = domain.User{
		WalletAddress: strings.ToLower(DevWalletAddress),
		Role:          domain.RoleAdmin,
		Nonce:         uuid.NewString(),
	}
	if err := uc.db.WithContext(ctx).Create(&user).Error; err != nil {
		log.Printf("[SEED] Failed to create dev wallet: %v", err)
		return
	}

	// Create inventory for the admin
	inv := domain.Inventory{
		UserID:    user.ID,
		Grass:     0,
		Milk:      0,
		LandSlots: 1,
		HasBarn:   true,
	}
	if err := uc.db.WithContext(ctx).Create(&inv).Error; err != nil {
		log.Printf("[SEED] Failed to create inventory for dev wallet: %v", err)
		return
	}

	log.Printf("[SEED] Dev wallet created as ROOT ADMIN: %s (ID: %s)", DevWalletAddress, user.ID)
}

// GetOrCreateNonce returns the nonce for a wallet address.
// If the wallet is unknown, it creates a temporary nonce entry.
func (uc *AuthUsecase) GetOrCreateNonce(ctx context.Context, walletAddress string) (string, bool, error) {
	walletAddress = strings.ToLower(walletAddress)
	var user domain.User

	err := uc.db.WithContext(ctx).Where("wallet_address = ?", walletAddress).First(&user).Error
	if err == nil {
		// Existing user — rotate nonce
		newNonce := uuid.NewString()
		if err := uc.db.WithContext(ctx).Model(&user).Update("nonce", newNonce).Error; err != nil {
			return "", false, fmt.Errorf("failed to rotate nonce: %w", err)
		}
		return newNonce, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// New user — return a fresh nonce (stored temporarily)
		newNonce := uuid.NewString()
		return newNonce, false, nil
	}
	return "", false, err
}

// LoginWithSignature verifies wallet ownership via signature, then logs in or registers.
func (uc *AuthUsecase) LoginWithSignature(ctx context.Context, walletAddress string, signature string, message string, referrerWallet string) (string, error) {
	walletAddress = strings.ToLower(walletAddress)
	referrerWallet = strings.ToLower(strings.TrimSpace(referrerWallet))

	// 1. Verify the Ethereum signature
	if err := utils.VerifyEthSignature(message, signature, walletAddress); err != nil {
		return "", fmt.Errorf("signature verification failed: %w", err)
	}

	var user domain.User
	var isNewUser bool

	err := uc.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Try to find existing user
		err := tx.Where("wallet_address = ?", walletAddress).First(&user).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				isNewUser = true
			} else {
				return err
			}
		}

		if isNewUser {
			// Determine referrer: use provided or fallback to dev wallet
			var referrerID *uuid.UUID
			if referrerWallet != "" && referrerWallet != walletAddress {
				var referrer domain.User
				if err := tx.Where("wallet_address = ?", referrerWallet).First(&referrer).Error; err == nil {
					referrerID = &referrer.ID
				}
			}

			// If no valid referrer found, auto-assign to dev wallet
			if referrerID == nil {
				var devUser domain.User
				devAddr := strings.ToLower(DevWalletAddress)
				if err := tx.Where("wallet_address = ?", devAddr).First(&devUser).Error; err == nil {
					referrerID = &devUser.ID
				}
			}

			// Create new user
			user = domain.User{
				WalletAddress: walletAddress,
				ReferrerID:    referrerID,
				Role:          domain.RoleF2P,
				Nonce:         uuid.NewString(),
			}
			if err := tx.Create(&user).Error; err != nil {
				return err
			}

			// Starter Pack
			inv := domain.Inventory{
				UserID:    user.ID,
				Grass:     0,
				Milk:      0,
				LandSlots: 1,
				HasBarn:   true,
			}
			if err := tx.Create(&inv).Error; err != nil {
				return err
			}
		} else {
			// Existing user — rotate nonce for replay protection
			user.Nonce = uuid.NewString()
			if err := tx.Model(&user).Update("nonce", user.Nonce).Error; err != nil {
				return fmt.Errorf("failed to rotate nonce: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	return generateJWT(user)
}

// Legacy: LoginOrRegister for backward compatibility (will be deprecated)
func (uc *AuthUsecase) LoginOrRegister(ctx context.Context, walletAddress string, referrerWallet string) (string, error) {
	// Redirect to the new flow, but without signature (for dev/testing only)
	walletAddress = strings.ToLower(walletAddress)
	referrerWallet = strings.ToLower(strings.TrimSpace(referrerWallet))

	var user domain.User
	var isNewUser bool

	err := uc.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Where("wallet_address = ?", walletAddress).First(&user).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				isNewUser = true
			} else {
				return err
			}
		}

		if isNewUser {
			var referrerID *uuid.UUID
			if referrerWallet != "" && referrerWallet != walletAddress {
				var referrer domain.User
				if err := tx.Where("wallet_address = ?", referrerWallet).First(&referrer).Error; err == nil {
					referrerID = &referrer.ID
				}
			}
			if referrerID == nil {
				var devUser domain.User
				devAddr := strings.ToLower(DevWalletAddress)
				if err := tx.Where("wallet_address = ?", devAddr).First(&devUser).Error; err == nil {
					referrerID = &devUser.ID
				}
			}

			user = domain.User{
				WalletAddress: walletAddress,
				ReferrerID:    referrerID,
				Role:          domain.RoleF2P,
				Nonce:         uuid.NewString(),
			}
			if err := tx.Create(&user).Error; err != nil {
				return err
			}

			inv := domain.Inventory{
				UserID:    user.ID,
				Grass:     0,
				Milk:      0,
				LandSlots: 1,
				HasBarn:   true,
			}
			if err := tx.Create(&inv).Error; err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	return generateJWT(user)
}

func generateJWT(user domain.User) (string, error) {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "super_secret_dev_key_123!" // Fallback for dev
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":        user.ID.String(),
		"wallet_address": user.WalletAddress,
		"role":           string(user.Role),
		"exp":            time.Now().Add(time.Hour * 24).Unix(),
	})

	return token.SignedString([]byte(secret))
}
