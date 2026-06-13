package usecase

import (
	"context"
	"errors"
	"time"

	"cashcowvalley/backend/internal/domain"
	customRedis "cashcowvalley/backend/pkg/redis"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FarmUsecase struct {
	db *gorm.DB
}

func NewFarmUsecase(db *gorm.DB) *FarmUsecase {
	return &FarmUsecase{db: db}
}

// FeedCow handles feeding a cow, deducting inventory, and increasing happiness.
// Protected by Redis Redlock to prevent Race Conditions (Double-Spend).
func (uc *FarmUsecase) FeedCow(ctx context.Context, userID uuid.UUID, cowID uuid.UUID) error {
	// 1. Acquire Distributed Lock (Redlock)
	// Hanya mengizinkan 1 proses 'Feed' per User pada satu waktu melintasi semua server
	lockKey := "feed_cow:" + userID.String()
	token, acquired := customRedis.AcquireLock(ctx, lockKey, 5*time.Second)
	if !acquired {
		return errors.New("Sistem sedang memproses transaksi Anda sebelumnya, harap tunggu")
	}
	defer customRedis.ReleaseLock(ctx, lockKey, token)

	// 2. ACID Database Transaction
	// [PATCH] Using WithContext(ctx) to prevent Zombie Transactions.
	// Jika user memutus koneksi (TCP Drop), query DB akan dibatalkan (Context Canceled).
	// Ini mencegah Redlock terlepas lebih awal sementara Tx DB masih berjalan di background.
	// BUG FIX: Mencegah Slowloris Redlock Poisoning dengan membatasi I/O Postgres maksimal 4 Detik (Satu detik sisa Redis TTL)
	ctxDB, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	return uc.db.WithContext(ctxDB).Transaction(func(tx *gorm.DB) error {
		var inventory domain.Inventory
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ?", userID).First(&inventory).Error; err != nil {
			return errors.New("Inventory tidak ditemukan")
		}

		if inventory.Grass < 1 {
			return errors.New("Rumput tidak cukup, mohon beli di Marketplace")
		}

		var cow domain.Cow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND owner_id = ?", cowID, userID).First(&cow).Error; err != nil {
			return errors.New("Sapi tidak ditemukan atau bukan milik Anda")
		}

		if cow.Happiness >= 100 {
			return errors.New("Sapi sudah sangat bahagia (100%)")
		}

		// Update Data
		inventory.Grass -= 1
		if err := tx.Save(&inventory).Error; err != nil {
			return err
		}

		cow.Happiness += 20
		if cow.Happiness > 100 {
			cow.Happiness = 100
		}
		now := time.Now()
		cow.LastFedAt = &now
		if err := tx.Save(&cow).Error; err != nil {
			return err
		}

		// Audit Trail / TxLog
		txLog := domain.TxLog{
			UserID:   userID,
			Type:     "FEED_COW",
			Amount:   decimal.NewFromInt(1),
			Currency: "GRASS",
			Status:   domain.TxSuccess,
		}
		if err := tx.Create(&txLog).Error; err != nil {
			return err
		}

		return nil // Commit Transaction
	})
}

// GetFarmStatus menampilkan status peternakan user (Read-Only, tanpa lock).
type FarmStatusResult struct {
	Cows         []domain.Cow       `json:"cows"`
	Inventory    *domain.Inventory  `json:"inventory"`
	GoldBalance  decimal.Decimal    `json:"gold_balance"`
	Points       decimal.Decimal    `json:"points"` // On-chain COW tokens
	USDTBalance  decimal.Decimal    `json:"usdt_balance"`
	DailyAdCount int                `json:"daily_ad_count"`
	Web2Stakes   []domain.Web2Stake `json:"web2_stakes"`
}

func (uc *FarmUsecase) GetFarmStatus(ctx context.Context, userID uuid.UUID) (*FarmStatusResult, error) {
	var cows []domain.Cow
	if err := uc.db.WithContext(ctx).Where("owner_id = ?", userID).
		Order("created_at ASC").Find(&cows).Error; err != nil {
		return nil, errors.New("Gagal mengambil data sapi")
	}

	var inventory domain.Inventory
	if err := uc.db.WithContext(ctx).Where("user_id = ?", userID).
		First(&inventory).Error; err != nil {
		// Jika belum ada inventory, kembalikan default kosong
		inventory = domain.Inventory{UserID: userID, Grass: 0, Milk: 0}
	}

	var user domain.User
	if err := uc.db.WithContext(ctx).Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, errors.New("User tidak ditemukan")
	}

	// Fetch stakes
	var stakes []domain.Web2Stake
	if err := uc.db.WithContext(ctx).Where("user_id = ?", userID).Find(&stakes).Error; err != nil {
		return nil, errors.New("Gagal mengambil data staking")
	}

	return &FarmStatusResult{
		Cows:         cows,
		Inventory:    &inventory,
		GoldBalance:  user.GoldBalance,
		Points:       user.Points,
		USDTBalance:  user.USDTBalance,
		DailyAdCount: user.DailyAdCount,
		Web2Stakes:   stakes,
	}, nil
}

// HarvestFarm handles harvesting milk from all eligible cows.
// It enforces the Web2 "Care Mechanic": Standard cows yield 0 if the user hasn't watched an ad in 24h.
func (uc *FarmUsecase) HarvestFarm(ctx context.Context, userID uuid.UUID) (int, error) {
	lockKey := "harvest_farm:" + userID.String()
	token, acquired := customRedis.AcquireLock(ctx, lockKey, 5*time.Second)
	if !acquired {
		return 0, errors.New("Sistem sedang memproses transaksi Anda sebelumnya, harap tunggu")
	}
	defer customRedis.ReleaseLock(ctx, lockKey, token)

	ctxDB, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	var totalMilkHarvested int

	err := uc.db.WithContext(ctxDB).Transaction(func(tx *gorm.DB) error {
		var user domain.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", userID).First(&user).Error; err != nil {
			return errors.New("User tidak ditemukan")
		}

		var cows []domain.Cow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_id = ?", userID).Find(&cows).Error; err != nil {
			return err
		}

		if len(cows) == 0 {
			return errors.New("Anda belum memiliki sapi untuk dipanen")
		}

		now := time.Now()
		hasWatchedAdRecently := false
		if user.LastAdWatchedAt != nil && now.Sub(*user.LastAdWatchedAt) <= 24*time.Hour {
			hasWatchedAdRecently = true
		}

		for i := range cows {
			cow := &cows[i]

			// Web2 Penalty: Standard F2P Cows require daily "Vitamin" (Ad) care.
			if cow.Type == domain.TypeStandard && !hasWatchedAdRecently {
				// SICK/HUNGRY Penalty: Yield drops to zero
				continue
			}

			// Time-based harvest logic (1 milk per hour base)
			var lastHarvest time.Time
			if cow.LastHarvestedAt != nil {
				lastHarvest = *cow.LastHarvestedAt
			} else {
				lastHarvest = cow.CreatedAt
			}

			hoursElapsed := int(now.Sub(lastHarvest).Hours())
			if hoursElapsed < 1 {
				continue // Harus menunggu minimal 1 jam untuk panen
			}

			// Yield Calculation
			yield := hoursElapsed * cow.Level
			// Happiness Penalty (-50% if happiness is low)
			if cow.Happiness < 50 {
				yield = yield / 2
			}

			totalMilkHarvested += yield
			cow.LastHarvestedAt = &now

			// Decrease happiness after harvesting to simulate work effort
			cow.Happiness -= 2
			if cow.Happiness < 0 {
				cow.Happiness = 0
			}

			if err := tx.Save(cow).Error; err != nil {
				return err
			}
		}

		if totalMilkHarvested > 0 {
			var inventory domain.Inventory
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("user_id = ?", userID).First(&inventory).Error; err != nil {
				// Create if it doesn't exist
				inventory = domain.Inventory{UserID: userID, Milk: 0, Grass: 0}
			}
			inventory.Milk += totalMilkHarvested

			if inventory.ID == uuid.Nil {
				if err := tx.Create(&inventory).Error; err != nil {
					return err
				}
			} else {
				if err := tx.Save(&inventory).Error; err != nil {
					return err
				}
			}

			txLog := domain.TxLog{
				UserID:   userID,
				Type:     "HARVEST_MILK",
				Amount:   decimal.NewFromInt(int64(totalMilkHarvested)),
				Currency: "MILK",
				Status:   domain.TxSuccess,
			}
			if err := tx.Create(&txLog).Error; err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return 0, err
	}

	if totalMilkHarvested == 0 {
		return 0, errors.New("Tidak ada susu yang bisa dipanen (Mungkin belum 1 jam, atau Sapi Kelaparan karena tidak diberi Vitamin Iklan dalam 24 jam terakhir!)")
	}

	return totalMilkHarvested, nil
}
