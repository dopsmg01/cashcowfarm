package usecase

import (
	"testing"
	"time"

	"cashcowvalley/backend/internal/domain"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// setupTestDB creates an in-memory SQLite database with all tables migrated.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := db.AutoMigrate(
		&domain.User{},
		&domain.Inventory{},
		&domain.Cow{},
		&domain.TxLog{},
		&domain.MarketListing{},
		&domain.Web2Stake{},
	); err != nil {
		t.Fatalf("failed to migrate test database: %v", err)
	}
	return db
}

// seedUser inserts a user with default balances and an inventory row.
func seedUser(t *testing.T, db *gorm.DB, wallet string, role domain.Role) domain.User {
	t.Helper()
	user := domain.User{
		WalletAddress: wallet,
		Role:          role,
		Nonce:         uuid.NewString(),
		GoldBalance:   decimal.NewFromInt(0),
		USDTBalance:   decimal.NewFromInt(0),
		Points:        decimal.NewFromInt(0),
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("seedUser: %v", err)
	}
	inv := domain.Inventory{
		UserID:    user.ID,
		Grass:     0,
		Milk:      0,
		LandSlots: 1,
		HasBarn:   true,
	}
	if err := db.Create(&inv).Error; err != nil {
		t.Fatalf("seedUser inventory: %v", err)
	}
	return user
}

// seedCow inserts a standard cow owned by the given user.
func seedCow(t *testing.T, db *gorm.DB, ownerID uuid.UUID, happiness int) domain.Cow {
	t.Helper()
	past := time.Now().Add(-2 * time.Hour)
	cow := domain.Cow{
		OwnerID:          ownerID,
		Type:             domain.TypeStandard,
		Level:            1,
		Happiness:        happiness,
		ExpectedLifespan: time.Now().AddDate(0, 3, 0),
		LastHarvestedAt:  &past,
	}
	if err := db.Create(&cow).Error; err != nil {
		t.Fatalf("seedCow: %v", err)
	}
	return cow
}
