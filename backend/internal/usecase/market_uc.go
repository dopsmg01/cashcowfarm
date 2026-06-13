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

type MarketUsecase struct {
	db *gorm.DB
}

func NewMarketUsecase(db *gorm.DB) *MarketUsecase {
	return &MarketUsecase{db: db}
}

// BuyItem memproses pembelian item P2P menggunakan Pessimistic Locking (FOR UPDATE).
func (uc *MarketUsecase) BuyItem(ctx context.Context, buyerID uuid.UUID, listingID uuid.UUID) error {
	// 1. Redlock Pembeli agar tidak spam klik "Beli"
	lockKey := "market_buy:" + buyerID.String()
	token, acquired := customRedis.AcquireLock(ctx, lockKey, 5*time.Second)
	if !acquired {
		return errors.New("Transaksi pembelian sedang diproses...")
	}
	defer customRedis.ReleaseLock(ctx, lockKey, token)

	// 2. Eksekusi Transaksi Database (ACID)
	// BUG FIX: Redlock Poisoning Mitigation.
	// Batasi umur transaksi DB hingga 4 detik (lebih pendek dari 5 detik Redlock TTL)
	ctxDB, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	return uc.db.WithContext(ctxDB).Transaction(func(tx *gorm.DB) error {
		var listing domain.MarketListing

		// ROW-LEVEL LOCKING (FOR UPDATE)
		// Mengunci baris ini di tabel market_listings supaya pembeli lain yang mencoba
		// mengeksekusi listing yang sama di milidetik yang sama akan tertahan/menunggu.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", listingID).
			First(&listing).Error; err != nil {
			return errors.New("Listing tidak ditemukan")
		}

		if listing.Status != "OPEN" {
			return errors.New("Item sudah terjual atau ditarik oleh penjual")
		}

		if listing.SellerID == buyerID {
			return errors.New("Tidak dapat membeli barang sendiri")
		}

		// Kunci User Pembeli dan Penjual secara leksikografis untuk MENCEGAH DEADLOCK.
		// Jika User A beli dari B, dan B beli dari A bersamaan, tanpa pengurutan ini Postgres akan Deadlock.
		buyerIDStr := buyerID.String()
		sellerIDStr := listing.SellerID.String()

		var firstLockID, secondLockID string
		var isBuyerFirst bool

		if buyerIDStr < sellerIDStr {
			firstLockID = buyerIDStr
			secondLockID = sellerIDStr
			isBuyerFirst = true
		} else {
			firstLockID = sellerIDStr
			secondLockID = buyerIDStr
			isBuyerFirst = false
		}

		// Kunci dengan urutan absolut (Lexicographical)
		var firstUser, secondUser domain.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", firstLockID).First(&firstUser).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", secondLockID).First(&secondUser).Error; err != nil {
			return err
		}

		// Petakan kembali ke identitas Pembeli / Penjual
		var buyer, seller *domain.User
		if isBuyerFirst {
			buyer = &firstUser
			seller = &secondUser
		} else {
			buyer = &secondUser
			seller = &firstUser
		}

		// Operasi Pengurangan USDT Menggunakan math/big (shopspring/decimal) agar PRESISI MUTLAK
		if buyer.USDTBalance.LessThan(listing.PriceUSDT) {
			return errors.New("Saldo USDT tidak mencukupi")
		}
		buyer.USDTBalance = buyer.USDTBalance.Sub(listing.PriceUSDT)
		if err := tx.Save(buyer).Error; err != nil {
			return err
		}

		seller.USDTBalance = seller.USDTBalance.Add(listing.PriceUSDT)
		if err := tx.Save(seller).Error; err != nil {
			return err
		}

		// Transfer Item (Misal GRASS) ke Inventory Pembeli
		var buyerInv domain.Inventory
		// BUG FIX: Tambahkan Pessimistic Lock (FOR UPDATE) pada tabel Inventory Pembeli.
		// Memastikan jika AdWebhook menambah Rumput/Susu ke pembeli di detik yang sama persis, data tidak akan saling tindih!
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", buyerID).First(&buyerInv).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// Jika pembeli belum punya tabel inventory, kita buatkan on-the-fly
				buyerInv = domain.Inventory{
					UserID: buyerID,
					Grass:  0,
					Milk:   0,
				}
				if err := tx.Create(&buyerInv).Error; err != nil {
					return errors.New("Gagal membuat inventory baru untuk pembeli")
				}
			} else {
				return errors.New("Inventory pembeli corrupt")
			}
		}

		// BUG FIX: Handle SEMUA tipe item, bukan hanya GRASS
		// Sebelumnya MILK silently dropped — pembeli bayar USDT tapi item tidak masuk!
		switch listing.ItemType {
		case "GRASS":
			buyerInv.Grass += listing.Quantity
		case "MILK":
			buyerInv.Milk += listing.Quantity
		default:
			return errors.New("Tipe item tidak dikenali")
		}
		if err := tx.Save(&buyerInv).Error; err != nil {
			return err
		}

		// Tutup Listing
		listing.Status = "SOLD"
		if err := tx.Save(&listing).Error; err != nil {
			return err
		}

		// BUG FIX: Audit Trail untuk transaksi Marketplace (sebelumnya tidak ada)
		listingIDStr := listingID.String()
		txLog := domain.TxLog{
			UserID:      buyerID,
			Type:        "MARKET_BUY",
			Amount:      listing.PriceUSDT,
			Currency:    "USDT",
			Status:      domain.TxSuccess,
			ReferenceID: &listingIDStr,
		}
		if err := tx.Create(&txLog).Error; err != nil {
			return err
		}

		return nil
	})
}

// GetListings mengembalikan semua listing OPEN di marketplace (Read-Only).
func (uc *MarketUsecase) GetListings(ctx context.Context) ([]domain.MarketListing, error) {
	var listings []domain.MarketListing
	if err := uc.db.WithContext(ctx).Where("status = ?", "OPEN").
		Order("created_at DESC").Find(&listings).Error; err != nil {
		return nil, errors.New("Gagal mengambil data marketplace")
	}
	return listings, nil
}

// SellItem membuat listing baru di marketplace.
func (uc *MarketUsecase) SellItem(ctx context.Context, sellerID uuid.UUID, itemType string, quantity int, priceUSDT decimal.Decimal) error {
	// Validasi input
	if itemType != "GRASS" && itemType != "MILK" {
		return errors.New("Tipe item tidak valid, hanya GRASS atau MILK")
	}
	if quantity <= 0 {
		return errors.New("Jumlah item harus lebih dari 0")
	}
	minPrice := decimal.NewFromFloat(0.01)
	maxPrice := decimal.NewFromInt(10000)
	if priceUSDT.LessThan(minPrice) || priceUSDT.GreaterThan(maxPrice) {
		return errors.New("Harga harus antara 0.01 dan 10000 USDT")
	}

	// Redlock agar seller tidak spam listing
	lockKey := "market_sell:" + sellerID.String()
	token, acquired := customRedis.AcquireLock(ctx, lockKey, 5*time.Second)
	if !acquired {
		return errors.New("Proses listing sedang berjalan...")
	}
	defer customRedis.ReleaseLock(ctx, lockKey, token)

	ctxDB, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	return uc.db.WithContext(ctxDB).Transaction(func(tx *gorm.DB) error {
		// Kurangi inventory seller
		var inv domain.Inventory
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ?", sellerID).First(&inv).Error; err != nil {
			return errors.New("Inventory tidak ditemukan")
		}

		switch itemType {
		case "GRASS":
			if inv.Grass < quantity {
				return errors.New("Rumput tidak cukup untuk dijual")
			}
			inv.Grass -= quantity
		case "MILK":
			if inv.Milk < quantity {
				return errors.New("Susu tidak cukup untuk dijual")
			}
			inv.Milk -= quantity
		}

		if err := tx.Save(&inv).Error; err != nil {
			return err
		}

		// Buat listing
		listing := domain.MarketListing{
			SellerID:  sellerID,
			ItemType:  itemType,
			Quantity:  quantity,
			PriceUSDT: priceUSDT,
			Status:    "OPEN",
		}
		if err := tx.Create(&listing).Error; err != nil {
			return errors.New("Gagal membuat listing")
		}

		// Audit trail
		listingIDStr := listing.ID.String()
		txLog := domain.TxLog{
			UserID:      sellerID,
			Type:        "MARKET_SELL",
			Amount:      priceUSDT,
			Currency:    "USDT",
			Status:      domain.TxSuccess,
			ReferenceID: &listingIDStr,
		}
		if err := tx.Create(&txLog).Error; err != nil {
			return err
		}

		return nil
	})
}

// BuyFromPlatform handles direct purchases from the system (e.g., Minting Cows, Buying Premium Grass).
// It implements the 70/20/10 distribution and the 1-Tier Roll-Up Referral mechanic.
// SellMilkForGold allows users to sell their milk for Gold currency.
func (uc *MarketUsecase) SellMilkForGold(ctx context.Context, userID uuid.UUID, quantity int) error {
	if quantity <= 0 {
		return errors.New("Quantity must be greater than 0")
	}

	ctxDB, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	return uc.db.WithContext(ctxDB).Transaction(func(tx *gorm.DB) error {
		var inv domain.Inventory
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&inv).Error; err != nil {
			return errors.New("Inventory not found")
		}

		if inv.Milk < quantity {
			return errors.New("Not enough milk to sell")
		}

		inv.Milk -= quantity
		if err := tx.Save(&inv).Error; err != nil {
			return err
		}

		var user domain.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return errors.New("User not found")
		}

		// 1 Milk = 5 Gold (Example rate)
		goldReward := decimal.NewFromInt(int64(quantity * 5))
		user.GoldBalance = user.GoldBalance.Add(goldReward)

		if err := tx.Save(&user).Error; err != nil {
			return err
		}

		tx.Create(&domain.TxLog{
			UserID:   userID,
			Type:     "SELL_MILK_GOLD",
			Amount:   goldReward,
			Currency: "GOLD",
			Status:   domain.TxSuccess,
		})

		return nil
	})
}

// BuyInAppItemWithGold allows users to spend Gold on farm essentials.
func (uc *MarketUsecase) BuyInAppItemWithGold(ctx context.Context, userID uuid.UUID, itemType string, quantity int) error {
	priceMap := map[string]int64{
		"GRASS":    10,   // 10 Gold
		"BABY_COW": 500,  // 500 Gold
		"COW":      2000, // 2000 Gold
		"LAND":     1000, // 1000 Gold
		"VITAMIN":  50,   // 50 Gold (Care boost)
	}

	unitPrice, ok := priceMap[itemType]
	if !ok {
		return errors.New("Invalid item type")
	}

	totalPrice := decimal.NewFromInt(unitPrice * int64(quantity))

	ctxDB, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	return uc.db.WithContext(ctxDB).Transaction(func(tx *gorm.DB) error {
		var user domain.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return errors.New("User not found")
		}

		if user.GoldBalance.LessThan(totalPrice) {
			return errors.New("Insufficient Gold balance")
		}

		user.GoldBalance = user.GoldBalance.Sub(totalPrice)

		// Handle Vitamin care boost immediately if applicable
		if itemType == "VITAMIN" {
			now := time.Now()
			user.LastAdWatchedAt = &now
		}

		if err := tx.Save(&user).Error; err != nil {
			return err
		}

		// Deliver items
		switch itemType {
		case "GRASS":
			var inv domain.Inventory
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&inv).Error; err == nil {
				inv.Grass += quantity
				tx.Save(&inv)
			}
		case "LAND":
			var inv domain.Inventory
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&inv).Error; err == nil {
				inv.LandSlots += quantity
				tx.Save(&inv)
			}
		case "BABY_COW", "COW":
			cowType := domain.TypeStandard // In-app are standard cows
			for i := 0; i < quantity; i++ {
				cow := domain.Cow{
					OwnerID:          userID,
					Type:             cowType,
					Level:            1,
					Happiness:        100,
					ExpectedLifespan: time.Now().AddDate(0, 3, 0),
				}
				tx.Create(&cow)
			}
		}

		tx.Create(&domain.TxLog{
			UserID:   userID,
			Type:     "BUY_ITEM_GOLD",
			Amount:   totalPrice,
			Currency: "GOLD",
			Status:   domain.TxSuccess,
		})

		return nil
	})
}

// SwapGoldToTokens handles the conversion of Gold to $COW or $USDT.
func (uc *MarketUsecase) SwapGoldToTokens(ctx context.Context, userID uuid.UUID, goldAmount decimal.Decimal, target string) error {
	if goldAmount.LessThanOrEqual(decimal.Zero) {
		return errors.New("Amount must be positive")
	}

	ctxDB, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	return uc.db.WithContext(ctxDB).Transaction(func(tx *gorm.DB) error {
		var user domain.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return errors.New("User not found")
		}

		if user.GoldBalance.LessThan(goldAmount) {
			return errors.New("Insufficient Gold balance")
		}

		var rewardAmount decimal.Decimal
		if target == "COW" {
			// 100 Gold = 1 COW
			rewardAmount = goldAmount.Div(decimal.NewFromInt(100))
			user.Points = user.Points.Add(rewardAmount)
		} else if target == "USDT" {
			// 10,000 Gold = 1 USDT
			rewardAmount = goldAmount.Div(decimal.NewFromInt(10000))
			user.USDTBalance = user.USDTBalance.Add(rewardAmount)
		} else {
			return errors.New("Invalid target currency")
		}

		user.GoldBalance = user.GoldBalance.Sub(goldAmount)
		if err := tx.Save(&user).Error; err != nil {
			return err
		}

		tx.Create(&domain.TxLog{
			UserID:   userID,
			Type:     "GOLD_SWAP",
			Amount:   goldAmount,
			Currency: "GOLD",
			Status:   domain.TxSuccess,
		})

		return nil
	})
}

func (uc *MarketUsecase) BuyFromPlatform(ctx context.Context, buyerID uuid.UUID, itemType string, quantity int, price decimal.Decimal, currency string, userUC *UserUsecase) error {
	if currency != "USDT" && currency != "COW" {
		return errors.New("Hanya mendukung pembayaran menggunakan USDT atau COW")
	}

	lockKey := "platform_buy:" + buyerID.String()
	token, acquired := customRedis.AcquireLock(ctx, lockKey, 5*time.Second)
	if !acquired {
		return errors.New("Transaksi pembelian sedang diproses...")
	}
	defer customRedis.ReleaseLock(ctx, lockKey, token)

	ctxDB, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	return uc.db.WithContext(ctxDB).Transaction(func(tx *gorm.DB) error {
		var buyer domain.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", buyerID).First(&buyer).Error; err != nil {
			return errors.New("User tidak ditemukan")
		}

		// 1. Deduct Balance
		if currency == "USDT" {
			if buyer.USDTBalance.LessThan(price) {
				return errors.New("Saldo USDT tidak mencukupi")
			}
			buyer.USDTBalance = buyer.USDTBalance.Sub(price)
		} else {
			if buyer.Points.LessThan(price) {
				return errors.New("Saldo COW tidak mencukupi")
			}
			buyer.Points = buyer.Points.Sub(price)
		}

		if err := tx.Save(&buyer).Error; err != nil {
			return err
		}

		// 2. Deliver Item
		switch itemType {
		case "COW":
			for i := 0; i < quantity; i++ {
				cow := domain.Cow{
					OwnerID:          buyerID,
					Level:            1,
					Happiness:        100,
					ExpectedLifespan: time.Now().AddDate(0, 3, 0), // 3 months
				}
				if err := tx.Create(&cow).Error; err != nil {
					return err
				}
			}
		case "GRASS":
			var inv domain.Inventory
			err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", buyerID).First(&inv).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					inv = domain.Inventory{UserID: buyerID, Grass: quantity, Milk: 0}
					if err := tx.Create(&inv).Error; err != nil {
						return err
					}
				} else {
					return err
				}
			} else {
				inv.Grass += quantity
				if err := tx.Save(&inv).Error; err != nil {
					return err
				}
			}
		default:
			return errors.New("Item platform tidak valid")
		}

		// 3. Economy Split: 70% LP, 20% Referral, 10% Dev/Treasury
		devCut := price.Mul(decimal.NewFromFloat(0.10))
		lpCut := price.Mul(decimal.NewFromFloat(0.70))
		refCut := price.Mul(decimal.NewFromFloat(0.20))

		// Process Roll-Up Referral
		upline, err := userUC.GetUplineReferrerWithCow(ctxDB, buyerID)
		if err != nil {
			return err
		}

		if upline != nil {
			// Found an eligible upline! Give them the 20%
			var uplineUser domain.User
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", upline.ID).First(&uplineUser).Error; err == nil {
				if currency == "USDT" {
					uplineUser.USDTBalance = uplineUser.USDTBalance.Add(refCut)
				} else {
					uplineUser.Points = uplineUser.Points.Add(refCut)
				}
				tx.Save(&uplineUser)

				// Log Referral Bonus
				tx.Create(&domain.TxLog{
					UserID:   uplineUser.ID,
					Type:     "REFERRAL_BONUS",
					Amount:   refCut,
					Currency: currency,
					Status:   domain.TxSuccess,
				})
			}
		} else {
			// No eligible upline found. The 20% "Rolls-up" to the Dev Treasury.
			devCut = devCut.Add(refCut)
		}

		// Log the system distributions
		tx.Create(&domain.TxLog{UserID: buyerID, Type: "PLATFORM_BUY", Amount: price, Currency: currency, Status: domain.TxSuccess})
		tx.Create(&domain.TxLog{UserID: buyerID, Type: "TREASURY_LP_BUYBACK", Amount: lpCut, Currency: currency, Status: domain.TxSuccess})
		tx.Create(&domain.TxLog{UserID: buyerID, Type: "TREASURY_DEV_FEE", Amount: devCut, Currency: currency, Status: domain.TxSuccess})

		return nil
	})
}

// === Web2 Staking Logic (In-App) ===

func (uc *MarketUsecase) StakeInApp(ctx context.Context, userID uuid.UUID, assetType string, amount decimal.Decimal) error {
	if amount.LessThanOrEqual(decimal.Zero) {
		return errors.New("Jumlah tidak valid")
	}

	return uc.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user domain.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}

		// Deduct balance
		if assetType == "GOLD" {
			if user.GoldBalance.LessThan(amount) {
				return errors.New("Gold tidak mencukupi")
			}
			user.GoldBalance = user.GoldBalance.Sub(amount)
		} else if assetType == "MILK" {
			var inv domain.Inventory
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&inv).Error; err != nil {
				return err
			}
			if decimal.NewFromInt(int64(inv.Milk)).LessThan(amount) {
				return errors.New("Susu tidak mencukupi")
			}
			inv.Milk -= int(amount.IntPart())
			tx.Save(&inv)
		} else {
			return errors.New("Tipe aset tidak didukung")
		}

		if err := tx.Save(&user).Error; err != nil {
			return err
		}

		// Create or Update Stake
		var stake domain.Web2Stake
		err := tx.Where("user_id = ? AND asset_type = ?", userID, assetType).First(&stake).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				stake = domain.Web2Stake{
					UserID:        userID,
					AssetType:     assetType,
					Amount:        amount,
					StakedAt:      time.Now(),
					LastClaimedAt: time.Now(),
				}
				return tx.Create(&stake).Error
			}
			return err
		}

		// If exists, claim rewards first then add to principal
		stake.Amount = stake.Amount.Add(amount)
		return tx.Save(&stake).Error
	})
}

func (uc *MarketUsecase) ClaimInAppRewards(ctx context.Context, userID uuid.UUID) error {
	return uc.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var stakes []domain.Web2Stake
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).Find(&stakes).Error; err != nil {
			return err
		}

		var user domain.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}

		now := time.Now()
		for i := range stakes {
			stake := &stakes[i]
			hours := now.Sub(stake.LastClaimedAt).Hours()
			if hours < 1 {
				continue
			}

			if stake.AssetType == "GOLD" {
				// 1000 Gold = 0.1 Milk / hour
				reward := stake.Amount.Div(decimal.NewFromInt(10000)).Mul(decimal.NewFromFloat(hours))
				var inv domain.Inventory
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&inv).Error; err == nil {
					inv.Milk += int(reward.IntPart())
					tx.Save(&inv)
				}
			} else if stake.AssetType == "MILK" {
				// 10 Milk = 1 Gold / hour
				reward := stake.Amount.Div(decimal.NewFromInt(10)).Mul(decimal.NewFromFloat(hours))
				user.GoldBalance = user.GoldBalance.Add(reward)
			}

			// Update last claimed time
			tx.Model(stake).Update("last_claimed_at", now)
		}

		return tx.Save(&user).Error
	})
}

// === Financial Core (Simulated) ===

func (uc *MarketUsecase) Deposit(ctx context.Context, userID uuid.UUID, asset string, amount decimal.Decimal) error {
	if asset != "COW" && asset != "USDT" {
		return errors.New("Invalid asset type: only COW or USDT supported")
	}
	if amount.LessThanOrEqual(decimal.Zero) {
		return errors.New("Amount must be positive")
	}

	return uc.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user domain.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}

		if asset == "COW" {
			user.Points = user.Points.Add(amount)
		} else if asset == "USDT" {
			user.USDTBalance = user.USDTBalance.Add(amount)
		}

		tx.Create(&domain.TxLog{
			UserID:   userID,
			Type:     "DEPOSIT",
			Amount:   amount,
			Currency: asset,
			Status:   domain.TxSuccess,
		})

		return tx.Save(&user).Error
	})
}

func (uc *MarketUsecase) Withdraw(ctx context.Context, userID uuid.UUID, asset string, amount decimal.Decimal) error {
	if asset != "COW" && asset != "USDT" {
		return errors.New("Invalid asset type: only COW or USDT supported")
	}
	if amount.LessThanOrEqual(decimal.Zero) {
		return errors.New("Amount must be positive")
	}

	return uc.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user domain.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", userID).First(&user).Error; err != nil {
			return err
		}

		if asset == "COW" {
			if user.Points.LessThan(amount) {
				return errors.New("Saldo COW tidak mencukupi")
			}
			user.Points = user.Points.Sub(amount)
		} else if asset == "USDT" {
			if user.USDTBalance.LessThan(amount) {
				return errors.New("Saldo USDT tidak mencukupi")
			}
			user.USDTBalance = user.USDTBalance.Sub(amount)
		}

		tx.Create(&domain.TxLog{
			UserID:   userID,
			Type:     "WITHDRAW",
			Amount:   amount,
			Currency: asset,
			Status:   domain.TxSuccess,
		})

		return tx.Save(&user).Error
	})
}
