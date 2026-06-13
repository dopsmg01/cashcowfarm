package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cashcowvalley/backend/internal/domain"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type AdminUsecase struct {
	db *gorm.DB
}

func NewAdminUsecase(db *gorm.DB) *AdminUsecase {
	return &AdminUsecase{db: db}
}

// TransferItem transfers an in-app item from the admin (unlimited) to a target user.
// Admin balance is not deducted — items are minted/created.
func (uc *AdminUsecase) TransferItem(ctx context.Context, adminID uuid.UUID, targetWallet string, itemType string, amount decimal.Decimal) error {
	targetWallet = strings.ToLower(strings.TrimSpace(targetWallet))
	if amount.LessThanOrEqual(decimal.Zero) {
		return errors.New("amount must be greater than 0")
	}

	return uc.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Find target user
		var target domain.User
		if err := tx.Where("wallet_address = ?", targetWallet).Preload("Inventory").First(&target).Error; err != nil {
			return fmt.Errorf("target user not found: %s", targetWallet)
		}

		var updateErr error
		switch strings.ToUpper(itemType) {
		case "GOLD":
			updateErr = tx.Model(&target).Update("gold_balance", gorm.Expr("gold_balance + ?", amount)).Error
		case "USDT":
			updateErr = tx.Model(&target).Update("usdt_balance", gorm.Expr("usdt_balance + ?", amount)).Error
		case "COW_TOKEN":
			updateErr = tx.Model(&target).Update("points", gorm.Expr("points + ?", amount)).Error
		case "GRASS":
			updateErr = tx.Model(&domain.Inventory{}).Where("user_id = ?", target.ID).Update("grass", gorm.Expr("grass + ?", amount.IntPart())).Error
		case "MILK":
			updateErr = tx.Model(&domain.Inventory{}).Where("user_id = ?", target.ID).Update("milk", gorm.Expr("milk + ?", amount.IntPart())).Error
		case "LAND":
			updateErr = tx.Model(&domain.Inventory{}).Where("user_id = ?", target.ID).Update("land_slots", gorm.Expr("land_slots + ?", amount.IntPart())).Error
		default:
			return fmt.Errorf("unknown item type: %s", itemType)
		}
		if updateErr != nil {
			return fmt.Errorf("failed to update %s balance: %w", itemType, updateErr)
		}

		// Log the transaction
		refID := fmt.Sprintf("admin-transfer-%s-%s-%s", adminID.String()[:8], target.ID.String()[:8], uuid.NewString()[:8])
		txLog := domain.TxLog{
			UserID:      target.ID,
			Type:        fmt.Sprintf("ADMIN_TRANSFER_%s", strings.ToUpper(itemType)),
			Amount:      amount,
			Currency:    strings.ToUpper(itemType),
			Status:      domain.TxSuccess,
			ReferenceID: &refID,
		}
		return tx.Create(&txLog).Error
	})
}

type UserListItem struct {
	ID            string `json:"id"`
	WalletAddress string `json:"wallet_address"`
	Role          string `json:"role"`
	GoldBalance   string `json:"gold_balance"`
	USDTBalance   string `json:"usdt_balance"`
	CowToken      string `json:"cow_token"`
	CowCount      int64  `json:"cow_count"`
	CreatedAt     string `json:"created_at"`
}

// ListUsers returns all users with their key balances.
func (uc *AdminUsecase) ListUsers(ctx context.Context) ([]UserListItem, error) {
	var users []domain.User
	if err := uc.db.WithContext(ctx).Order("created_at DESC").Find(&users).Error; err != nil {
		return nil, err
	}

	items := make([]UserListItem, 0, len(users))
	for _, u := range users {
		var cowCount int64
		if err := uc.db.WithContext(ctx).Model(&domain.Cow{}).Where("owner_id = ?", u.ID).Count(&cowCount).Error; err != nil {
			return nil, fmt.Errorf("failed to count cows for user %s: %w", u.ID, err)
		}

		items = append(items, UserListItem{
			ID:            u.ID.String(),
			WalletAddress: u.WalletAddress,
			Role:          string(u.Role),
			GoldBalance:   u.GoldBalance.String(),
			USDTBalance:   u.USDTBalance.String(),
			CowToken:      u.Points.String(),
			CowCount:      cowCount,
			CreatedAt:     u.CreatedAt.Format("2006-01-02 15:04"),
		})
	}

	return items, nil
}

type PlatformStats struct {
	TotalUsers    int64  `json:"total_users"`
	TotalCows     int64  `json:"total_cows"`
	TotalGold     string `json:"total_gold"`
	TotalUSDT     string `json:"total_usdt"`
	TotalCowToken string `json:"total_cow_token"`
}

// GetPlatformStats returns aggregate platform statistics.
func (uc *AdminUsecase) GetPlatformStats(ctx context.Context) (*PlatformStats, error) {
	var stats PlatformStats

	if err := uc.db.WithContext(ctx).Model(&domain.User{}).Count(&stats.TotalUsers).Error; err != nil {
		return nil, fmt.Errorf("failed to count users: %w", err)
	}
	if err := uc.db.WithContext(ctx).Model(&domain.Cow{}).Count(&stats.TotalCows).Error; err != nil {
		return nil, fmt.Errorf("failed to count cows: %w", err)
	}

	var goldSum, usdtSum, cowSum decimal.NullDecimal
	if err := uc.db.WithContext(ctx).Model(&domain.User{}).Select("COALESCE(SUM(gold_balance), 0)").Scan(&goldSum).Error; err != nil {
		return nil, fmt.Errorf("failed to sum gold balances: %w", err)
	}
	if err := uc.db.WithContext(ctx).Model(&domain.User{}).Select("COALESCE(SUM(usdt_balance), 0)").Scan(&usdtSum).Error; err != nil {
		return nil, fmt.Errorf("failed to sum USDT balances: %w", err)
	}
	if err := uc.db.WithContext(ctx).Model(&domain.User{}).Select("COALESCE(SUM(points), 0)").Scan(&cowSum).Error; err != nil {
		return nil, fmt.Errorf("failed to sum COW token balances: %w", err)
	}

	if goldSum.Valid {
		stats.TotalGold = goldSum.Decimal.String()
	}
	if usdtSum.Valid {
		stats.TotalUSDT = usdtSum.Decimal.String()
	}
	if cowSum.Valid {
		stats.TotalCowToken = cowSum.Decimal.String()
	}

	return &stats, nil
}
