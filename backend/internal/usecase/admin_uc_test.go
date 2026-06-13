package usecase

import (
	"context"
	"testing"

	"cashcowvalley/backend/internal/domain"

	"github.com/shopspring/decimal"
)

func TestTransferItem_Gold(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAdminUsecase(db)
	admin := seedUser(t, db, "0xadmin", domain.RoleAdmin)
	target := seedUser(t, db, "0xtarget", domain.RoleF2P)

	err := uc.TransferItem(context.Background(), admin.ID, target.WalletAddress, "GOLD", decimal.NewFromInt(500))
	if err != nil {
		t.Fatalf("TransferItem GOLD failed: %v", err)
	}

	var updated domain.User
	db.Where("id = ?", target.ID).First(&updated)
	if !updated.GoldBalance.Equal(decimal.NewFromInt(500)) {
		t.Errorf("gold balance = %s, want 500", updated.GoldBalance.String())
	}
}

func TestTransferItem_USDT(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAdminUsecase(db)
	admin := seedUser(t, db, "0xadmin2", domain.RoleAdmin)
	target := seedUser(t, db, "0xtarget2", domain.RoleF2P)

	err := uc.TransferItem(context.Background(), admin.ID, target.WalletAddress, "USDT", decimal.NewFromFloat(100.50))
	if err != nil {
		t.Fatalf("TransferItem USDT failed: %v", err)
	}

	var updated domain.User
	db.Where("id = ?", target.ID).First(&updated)
	if !updated.USDTBalance.Equal(decimal.NewFromFloat(100.50)) {
		t.Errorf("usdt balance = %s, want 100.50", updated.USDTBalance.String())
	}
}

func TestTransferItem_Grass(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAdminUsecase(db)
	admin := seedUser(t, db, "0xadmin3", domain.RoleAdmin)
	target := seedUser(t, db, "0xtarget3", domain.RoleF2P)

	err := uc.TransferItem(context.Background(), admin.ID, target.WalletAddress, "GRASS", decimal.NewFromInt(25))
	if err != nil {
		t.Fatalf("TransferItem GRASS failed: %v", err)
	}

	var inv domain.Inventory
	db.Where("user_id = ?", target.ID).First(&inv)
	if inv.Grass != 25 {
		t.Errorf("grass = %d, want 25", inv.Grass)
	}
}

func TestTransferItem_Milk(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAdminUsecase(db)
	admin := seedUser(t, db, "0xadmin4", domain.RoleAdmin)
	target := seedUser(t, db, "0xtarget4", domain.RoleF2P)

	err := uc.TransferItem(context.Background(), admin.ID, target.WalletAddress, "MILK", decimal.NewFromInt(10))
	if err != nil {
		t.Fatalf("TransferItem MILK failed: %v", err)
	}

	var inv domain.Inventory
	db.Where("user_id = ?", target.ID).First(&inv)
	if inv.Milk != 10 {
		t.Errorf("milk = %d, want 10", inv.Milk)
	}
}

func TestTransferItem_CowToken(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAdminUsecase(db)
	admin := seedUser(t, db, "0xadmin5", domain.RoleAdmin)
	target := seedUser(t, db, "0xtarget5", domain.RoleF2P)

	err := uc.TransferItem(context.Background(), admin.ID, target.WalletAddress, "COW_TOKEN", decimal.NewFromInt(100))
	if err != nil {
		t.Fatalf("TransferItem COW_TOKEN failed: %v", err)
	}

	var updated domain.User
	db.Where("id = ?", target.ID).First(&updated)
	if !updated.Points.Equal(decimal.NewFromInt(100)) {
		t.Errorf("points = %s, want 100", updated.Points.String())
	}
}

func TestTransferItem_Land(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAdminUsecase(db)
	admin := seedUser(t, db, "0xadmin6", domain.RoleAdmin)
	target := seedUser(t, db, "0xtarget6", domain.RoleF2P)

	err := uc.TransferItem(context.Background(), admin.ID, target.WalletAddress, "LAND", decimal.NewFromInt(3))
	if err != nil {
		t.Fatalf("TransferItem LAND failed: %v", err)
	}

	var inv domain.Inventory
	db.Where("user_id = ?", target.ID).First(&inv)
	// 1 (starter) + 3 = 4
	if inv.LandSlots != 4 {
		t.Errorf("land_slots = %d, want 4", inv.LandSlots)
	}
}

func TestTransferItem_InvalidType(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAdminUsecase(db)
	admin := seedUser(t, db, "0xadmin7", domain.RoleAdmin)
	target := seedUser(t, db, "0xtarget7", domain.RoleF2P)

	err := uc.TransferItem(context.Background(), admin.ID, target.WalletAddress, "DIAMONDS", decimal.NewFromInt(1))
	if err == nil {
		t.Fatal("expected error for invalid item type")
	}
}

func TestTransferItem_ZeroAmount(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAdminUsecase(db)
	admin := seedUser(t, db, "0xadmin8", domain.RoleAdmin)
	target := seedUser(t, db, "0xtarget8", domain.RoleF2P)

	err := uc.TransferItem(context.Background(), admin.ID, target.WalletAddress, "GOLD", decimal.Zero)
	if err == nil {
		t.Fatal("expected error for zero amount")
	}
}

func TestTransferItem_UnknownWallet(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAdminUsecase(db)
	admin := seedUser(t, db, "0xadmin9", domain.RoleAdmin)

	err := uc.TransferItem(context.Background(), admin.ID, "0xghost", "GOLD", decimal.NewFromInt(100))
	if err == nil {
		t.Fatal("expected error for unknown target wallet")
	}
}

func TestTransferItem_AuditLog(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAdminUsecase(db)
	admin := seedUser(t, db, "0xadminlog", domain.RoleAdmin)
	target := seedUser(t, db, "0xtargetlog", domain.RoleF2P)

	_ = uc.TransferItem(context.Background(), admin.ID, target.WalletAddress, "GOLD", decimal.NewFromInt(42))

	var logs []domain.TxLog
	db.Where("user_id = ?", target.ID).Find(&logs)
	if len(logs) != 1 {
		t.Fatalf("expected 1 tx log, got %d", len(logs))
	}
	if logs[0].Type != "ADMIN_TRANSFER_GOLD" {
		t.Errorf("log type = %s, want ADMIN_TRANSFER_GOLD", logs[0].Type)
	}
}

func TestListUsers(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAdminUsecase(db)

	seedUser(t, db, "0xlist1", domain.RoleF2P)
	seedUser(t, db, "0xlist2", domain.RoleSultan)

	items, err := uc.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 users, got %d", len(items))
	}
}

func TestGetPlatformStats(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAdminUsecase(db)

	u1 := seedUser(t, db, "0xstats1", domain.RoleF2P)
	u2 := seedUser(t, db, "0xstats2", domain.RoleF2P)
	db.Model(&u1).Update("gold_balance", decimal.NewFromInt(100))
	db.Model(&u2).Update("gold_balance", decimal.NewFromInt(200))
	seedCow(t, db, u1.ID, 80)

	stats, err := uc.GetPlatformStats(context.Background())
	if err != nil {
		t.Fatalf("GetPlatformStats failed: %v", err)
	}
	if stats.TotalUsers != 2 {
		t.Errorf("TotalUsers = %d, want 2", stats.TotalUsers)
	}
	if stats.TotalCows != 1 {
		t.Errorf("TotalCows = %d, want 1", stats.TotalCows)
	}
}
