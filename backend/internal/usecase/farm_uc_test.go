package usecase

import (
	"context"
	"testing"
	"time"

	"cashcowvalley/backend/internal/domain"

	"github.com/google/uuid"
)

func TestFeedCow_Success(t *testing.T) {
	db := setupTestDB(t)
	uc := NewFarmUsecase(db)

	user := seedUser(t, db, "0xfarmer", domain.RoleF2P)
	// Give user some grass
	db.Model(&domain.Inventory{}).Where("user_id = ?", user.ID).Update("grass", 5)

	cow := seedCow(t, db, user.ID, 50)

	err := uc.FeedCow(context.Background(), user.ID, cow.ID)
	if err != nil {
		t.Fatalf("FeedCow failed: %v", err)
	}

	// Verify grass was deducted
	var inv domain.Inventory
	db.Where("user_id = ?", user.ID).First(&inv)
	if inv.Grass != 4 {
		t.Errorf("grass = %d, want 4", inv.Grass)
	}

	// Verify happiness increased
	var updated domain.Cow
	db.Where("id = ?", cow.ID).First(&updated)
	if updated.Happiness != 70 { // 50 + 20
		t.Errorf("happiness = %d, want 70", updated.Happiness)
	}
}

func TestFeedCow_HappinessCappedAt100(t *testing.T) {
	db := setupTestDB(t)
	uc := NewFarmUsecase(db)

	user := seedUser(t, db, "0xfarmer2", domain.RoleF2P)
	db.Model(&domain.Inventory{}).Where("user_id = ?", user.ID).Update("grass", 5)
	cow := seedCow(t, db, user.ID, 90)

	err := uc.FeedCow(context.Background(), user.ID, cow.ID)
	if err != nil {
		t.Fatalf("FeedCow failed: %v", err)
	}

	var updated domain.Cow
	db.Where("id = ?", cow.ID).First(&updated)
	if updated.Happiness != 100 {
		t.Errorf("happiness = %d, want 100 (capped)", updated.Happiness)
	}
}

func TestFeedCow_AlreadyMaxHappy(t *testing.T) {
	db := setupTestDB(t)
	uc := NewFarmUsecase(db)

	user := seedUser(t, db, "0xfarmer3", domain.RoleF2P)
	db.Model(&domain.Inventory{}).Where("user_id = ?", user.ID).Update("grass", 5)
	cow := seedCow(t, db, user.ID, 100)

	err := uc.FeedCow(context.Background(), user.ID, cow.ID)
	if err == nil {
		t.Fatal("expected error when cow is already at 100% happiness")
	}
}

func TestFeedCow_NoGrass(t *testing.T) {
	db := setupTestDB(t)
	uc := NewFarmUsecase(db)

	user := seedUser(t, db, "0xfarmer4", domain.RoleF2P)
	cow := seedCow(t, db, user.ID, 50)

	err := uc.FeedCow(context.Background(), user.ID, cow.ID)
	if err == nil {
		t.Fatal("expected error when user has no grass")
	}
}

func TestFeedCow_NotOwned(t *testing.T) {
	db := setupTestDB(t)
	uc := NewFarmUsecase(db)

	user := seedUser(t, db, "0xfarmer5", domain.RoleF2P)
	other := seedUser(t, db, "0xother", domain.RoleF2P)
	db.Model(&domain.Inventory{}).Where("user_id = ?", user.ID).Update("grass", 5)
	cow := seedCow(t, db, other.ID, 50)

	err := uc.FeedCow(context.Background(), user.ID, cow.ID)
	if err == nil {
		t.Fatal("expected error when cow not owned by user")
	}
}

func TestFeedCow_AuditTrail(t *testing.T) {
	db := setupTestDB(t)
	uc := NewFarmUsecase(db)

	user := seedUser(t, db, "0xfarmeraudit", domain.RoleF2P)
	db.Model(&domain.Inventory{}).Where("user_id = ?", user.ID).Update("grass", 5)
	cow := seedCow(t, db, user.ID, 50)

	_ = uc.FeedCow(context.Background(), user.ID, cow.ID)

	var logs []domain.TxLog
	db.Where("user_id = ? AND type = ?", user.ID, "FEED_COW").Find(&logs)
	if len(logs) != 1 {
		t.Errorf("expected 1 FEED_COW tx log, got %d", len(logs))
	}
}

func TestGetFarmStatus_Empty(t *testing.T) {
	db := setupTestDB(t)
	uc := NewFarmUsecase(db)

	user := seedUser(t, db, "0xemptyfarm", domain.RoleF2P)

	status, err := uc.GetFarmStatus(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetFarmStatus error: %v", err)
	}
	if len(status.Cows) != 0 {
		t.Errorf("expected 0 cows, got %d", len(status.Cows))
	}
	if status.Inventory == nil {
		t.Fatal("expected non-nil inventory")
	}
}

func TestGetFarmStatus_WithCows(t *testing.T) {
	db := setupTestDB(t)
	uc := NewFarmUsecase(db)

	user := seedUser(t, db, "0xwithcows", domain.RoleF2P)
	seedCow(t, db, user.ID, 80)
	seedCow(t, db, user.ID, 60)
	db.Model(&domain.Inventory{}).Where("user_id = ?", user.ID).Updates(map[string]interface{}{"grass": 10, "milk": 5})

	status, err := uc.GetFarmStatus(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("GetFarmStatus error: %v", err)
	}
	if len(status.Cows) != 2 {
		t.Errorf("expected 2 cows, got %d", len(status.Cows))
	}
	if status.Inventory.Grass != 10 {
		t.Errorf("grass = %d, want 10", status.Inventory.Grass)
	}
}

func TestGetFarmStatus_UnknownUser(t *testing.T) {
	db := setupTestDB(t)
	uc := NewFarmUsecase(db)

	_, err := uc.GetFarmStatus(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error for unknown user")
	}
}

func TestHarvestFarm_Success(t *testing.T) {
	db := setupTestDB(t)
	uc := NewFarmUsecase(db)

	user := seedUser(t, db, "0xharvester", domain.RoleF2P)
	// Mark user as having recently watched an ad
	now := time.Now()
	db.Model(&domain.User{}).Where("id = ?", user.ID).Update("last_ad_watched_at", now)

	// Cow with last harvest 2 hours ago, level 1 → expect 2 milk
	cow := seedCow(t, db, user.ID, 80)
	_ = cow

	milk, err := uc.HarvestFarm(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("HarvestFarm failed: %v", err)
	}
	if milk < 1 {
		t.Errorf("expected at least 1 milk, got %d", milk)
	}

	// Verify milk was added to inventory
	var inv domain.Inventory
	db.Where("user_id = ?", user.ID).First(&inv)
	if inv.Milk != milk {
		t.Errorf("inventory milk = %d, want %d", inv.Milk, milk)
	}
}

func TestHarvestFarm_NoCows(t *testing.T) {
	db := setupTestDB(t)
	uc := NewFarmUsecase(db)

	user := seedUser(t, db, "0xnocows", domain.RoleF2P)

	_, err := uc.HarvestFarm(context.Background(), user.ID)
	if err == nil {
		t.Fatal("expected error when user has no cows")
	}
}

func TestHarvestFarm_NoAdWatched_ZeroYield(t *testing.T) {
	db := setupTestDB(t)
	uc := NewFarmUsecase(db)

	user := seedUser(t, db, "0xlazyfarmer", domain.RoleF2P)
	// No ad watched → standard cows yield 0
	seedCow(t, db, user.ID, 80)

	_, err := uc.HarvestFarm(context.Background(), user.ID)
	if err == nil {
		t.Fatal("expected error (zero yield) when ad not watched in 24h")
	}
}

func TestHarvestFarm_HappinessPenalty(t *testing.T) {
	db := setupTestDB(t)
	uc := NewFarmUsecase(db)

	user := seedUser(t, db, "0xsadfarmer", domain.RoleF2P)
	now := time.Now()
	db.Model(&domain.User{}).Where("id = ?", user.ID).Update("last_ad_watched_at", now)

	// Low-happiness cow (yield halved)
	seedCow(t, db, user.ID, 30)

	milk, err := uc.HarvestFarm(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("HarvestFarm failed: %v", err)
	}
	// With 2 hours elapsed, level 1, happiness < 50 → yield = 2/2 = 1
	if milk != 1 {
		t.Errorf("expected 1 milk (halved), got %d", milk)
	}
}
