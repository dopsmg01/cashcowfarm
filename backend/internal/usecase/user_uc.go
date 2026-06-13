package usecase

import (
	"context"
	"errors"
	"fmt"

	"cashcowvalley/backend/internal/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type UserUsecase struct {
	db *gorm.DB
}

func NewUserUsecase(db *gorm.DB) *UserUsecase {
	return &UserUsecase{db: db}
}

// BindReferrer links a user to a referrer via their wallet address.
func (uc *UserUsecase) BindReferrer(ctx context.Context, userID uuid.UUID, referrerWallet string) error {
	return uc.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user domain.User
		if err := tx.Where("id = ?", userID).First(&user).Error; err != nil {
			return errors.New("User not found")
		}

		if user.ReferrerID != nil {
			return errors.New("Referrer has already been set")
		}

		var referrer domain.User
		if err := tx.Where("wallet_address = ?", referrerWallet).First(&referrer).Error; err != nil {
			return errors.New("Referrer wallet not found")
		}

		if referrer.ID == user.ID {
			return errors.New("Cannot refer yourself")
		}

		// Prevent simple cyclical referral (A -> B -> A)
		if referrer.ReferrerID != nil && *referrer.ReferrerID == user.ID {
			return errors.New("Cyclical referrals are not allowed")
		}

		user.ReferrerID = &referrer.ID
		return tx.Save(&user).Error
	})
}

// GetUplineReferrerWithCow is the core "Roll-Up" mechanic.
// It traverses up the referral tree. If the direct referrer has NO cows,
// it rolls up to their referrer, up to a maximum depth (e.g., 10) to prevent infinite loops.
func (uc *UserUsecase) GetUplineReferrerWithCow(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	const maxDepth = 10
	currentUserID := userID

	for i := 0; i < maxDepth; i++ {
		var user domain.User
		// Load User and their direct Referrer
		if err := uc.db.WithContext(ctx).Preload("Referrer").Where("id = ?", currentUserID).First(&user).Error; err != nil {
			return nil, err
		}

		if user.ReferrerID == nil || user.Referrer == nil {
			// No further upline exists. Roll-up stops here.
			return nil, nil // Return nil, nil meaning no eligible referrer found
		}

		referrer := user.Referrer

		// Check if this referrer owns at least one cow
		var cowCount int64
		if err := uc.db.WithContext(ctx).Model(&domain.Cow{}).Where("owner_id = ?", referrer.ID).Count(&cowCount).Error; err != nil {
			return nil, err
		}

		if cowCount > 0 {
			// Found an eligible referrer!
			return referrer, nil
		}

		// If no cow, move the pointer up the tree and try again
		currentUserID = referrer.ID
	}

	// Reached max depth without finding an eligible referrer
	return nil, nil
}

type ReferralStats struct {
	TotalDirectInvites int64  `json:"total_direct_invites"`
	IsEligibleForBonus bool   `json:"is_eligible_for_bonus"`
	ReferralLink       string `json:"referral_link"`
}

func (uc *UserUsecase) GetReferralStats(ctx context.Context, userID uuid.UUID) (*ReferralStats, error) {
	var user domain.User
	if err := uc.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, errors.New("User not found")
	}

	var invites int64
	if err := uc.db.WithContext(ctx).Model(&domain.User{}).Where("referrer_id = ?", userID).Count(&invites).Error; err != nil {
		return nil, fmt.Errorf("failed to count referrals: %w", err)
	}

	var cowCount int64
	if err := uc.db.WithContext(ctx).Model(&domain.Cow{}).Where("owner_id = ?", userID).Count(&cowCount).Error; err != nil {
		return nil, fmt.Errorf("failed to count cows: %w", err)
	}

	return &ReferralStats{
		TotalDirectInvites: invites,
		IsEligibleForBonus: cowCount > 0,
		ReferralLink:       fmt.Sprintf("https://cashcowvalley.com?ref=%s", user.WalletAddress),
	}, nil
}
