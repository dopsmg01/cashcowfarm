package usecase

import (
	"context"
	"testing"

	"cashcowvalley/backend/internal/domain"

	"github.com/google/uuid"
)

func TestBindReferrer_Success(t *testing.T) {
	db := setupTestDB(t)
	uc := NewUserUsecase(db)

	user := seedUser(t, db, "0xchild", domain.RoleF2P)
	referrer := seedUser(t, db, "0xparent", domain.RoleF2P)

	err := uc.BindReferrer(context.Background(), user.ID, referrer.WalletAddress)
	if err != nil {
		t.Fatalf("BindReferrer failed: %v", err)
	}

	var updated domain.User
	db.Where("id = ?", user.ID).First(&updated)
	if updated.ReferrerID == nil || *updated.ReferrerID != referrer.ID {
		t.Errorf("referrer not set correctly")
	}
}

func TestBindReferrer_AlreadySet(t *testing.T) {
	db := setupTestDB(t)
	uc := NewUserUsecase(db)

	referrer1 := seedUser(t, db, "0xref1", domain.RoleF2P)
	referrer2 := seedUser(t, db, "0xref2", domain.RoleF2P)
	user := seedUser(t, db, "0xchild2", domain.RoleF2P)

	// Set initial referrer
	db.Model(&user).Update("referrer_id", referrer1.ID)

	err := uc.BindReferrer(context.Background(), user.ID, referrer2.WalletAddress)
	if err == nil {
		t.Fatal("expected error when referrer already set")
	}
}

func TestBindReferrer_SelfRefer(t *testing.T) {
	db := setupTestDB(t)
	uc := NewUserUsecase(db)

	user := seedUser(t, db, "0xselfreferral", domain.RoleF2P)

	err := uc.BindReferrer(context.Background(), user.ID, user.WalletAddress)
	if err == nil {
		t.Fatal("expected error on self-referral")
	}
}

func TestBindReferrer_CyclicalPrevention(t *testing.T) {
	db := setupTestDB(t)
	uc := NewUserUsecase(db)

	userA := seedUser(t, db, "0xcyclea", domain.RoleF2P)
	userB := seedUser(t, db, "0xcycleb", domain.RoleF2P)

	// B refers A
	db.Model(&userB).Update("referrer_id", userA.ID)

	// Now A tries to refer B → should fail (cycle)
	err := uc.BindReferrer(context.Background(), userA.ID, userB.WalletAddress)
	if err == nil {
		t.Fatal("expected error for cyclical referral")
	}
}

func TestBindReferrer_UnknownWallet(t *testing.T) {
	db := setupTestDB(t)
	uc := NewUserUsecase(db)

	user := seedUser(t, db, "0xorphan", domain.RoleF2P)

	err := uc.BindReferrer(context.Background(), user.ID, "0xnonexistent")
	if err == nil {
		t.Fatal("expected error for unknown referrer wallet")
	}
}

func TestGetUplineReferrerWithCow_DirectHasCow(t *testing.T) {
	db := setupTestDB(t)
	uc := NewUserUsecase(db)

	referrer := seedUser(t, db, "0xupline", domain.RoleF2P)
	seedCow(t, db, referrer.ID, 100)

	user := seedUser(t, db, "0xdownline", domain.RoleF2P)
	db.Model(&user).Update("referrer_id", referrer.ID)

	found, err := uc.GetUplineReferrerWithCow(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found == nil {
		t.Fatal("expected to find referrer with cow")
	}
	if found.ID != referrer.ID {
		t.Errorf("found referrer %v, want %v", found.ID, referrer.ID)
	}
}

func TestGetUplineReferrerWithCow_RollUp(t *testing.T) {
	db := setupTestDB(t)
	uc := NewUserUsecase(db)

	// grandparent has cow, parent does not
	grandparent := seedUser(t, db, "0xgrandparent", domain.RoleF2P)
	seedCow(t, db, grandparent.ID, 100)

	parent := seedUser(t, db, "0xparent2", domain.RoleF2P)
	db.Model(&parent).Update("referrer_id", grandparent.ID)

	child := seedUser(t, db, "0xchild3", domain.RoleF2P)
	db.Model(&child).Update("referrer_id", parent.ID)

	found, err := uc.GetUplineReferrerWithCow(context.Background(), child.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found == nil {
		t.Fatal("expected roll-up to find grandparent")
	}
	if found.ID != grandparent.ID {
		t.Errorf("found %v, want grandparent %v", found.ID, grandparent.ID)
	}
}

func TestGetUplineReferrerWithCow_NoneFound(t *testing.T) {
	db := setupTestDB(t)
	uc := NewUserUsecase(db)

	user := seedUser(t, db, "0xlonely", domain.RoleF2P)

	found, err := uc.GetUplineReferrerWithCow(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != nil {
		t.Error("expected nil when no referrer exists")
	}
}

func TestGetReferralStats(t *testing.T) {
	db := setupTestDB(t)
	uc := NewUserUsecase(db)

	referrer := seedUser(t, db, "0xstatsref", domain.RoleF2P)
	seedCow(t, db, referrer.ID, 80)

	// Create two referrals
	r1 := seedUser(t, db, "0xinvite1", domain.RoleF2P)
	r2 := seedUser(t, db, "0xinvite2", domain.RoleF2P)
	db.Model(&r1).Update("referrer_id", referrer.ID)
	db.Model(&r2).Update("referrer_id", referrer.ID)

	stats, err := uc.GetReferralStats(context.Background(), referrer.ID)
	if err != nil {
		t.Fatalf("GetReferralStats error: %v", err)
	}
	if stats.TotalDirectInvites != 2 {
		t.Errorf("TotalDirectInvites = %d, want 2", stats.TotalDirectInvites)
	}
	if !stats.IsEligibleForBonus {
		t.Error("expected eligible for bonus (has cow)")
	}
	if stats.ReferralLink == "" {
		t.Error("expected non-empty referral link")
	}
}

func TestGetReferralStats_NotFound(t *testing.T) {
	db := setupTestDB(t)
	uc := NewUserUsecase(db)

	_, err := uc.GetReferralStats(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error for unknown user")
	}
}
