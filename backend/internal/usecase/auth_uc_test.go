package usecase

import (
	"context"
	"strings"
	"testing"

	"cashcowvalley/backend/internal/domain"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func TestGetOrCreateNonce_ExistingUser(t *testing.T) {
	db := setupTestDB(t)
	user := seedUser(t, db, "0xabc123", domain.RoleF2P)
	uc := NewAuthUsecase(db)

	nonce, exists, err := uc.GetOrCreateNonce(context.Background(), user.WalletAddress)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Error("expected exists=true for known wallet")
	}
	if nonce == "" {
		t.Error("expected non-empty nonce")
	}
	// Nonce should have been rotated
	var updated domain.User
	db.Where("id = ?", user.ID).First(&updated)
	if updated.Nonce != nonce {
		t.Errorf("nonce not persisted: got %q, want %q", updated.Nonce, nonce)
	}
}

func TestGetOrCreateNonce_NewWallet(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAuthUsecase(db)

	nonce, exists, err := uc.GetOrCreateNonce(context.Background(), "0xnewwallet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Error("expected exists=false for unknown wallet")
	}
	if nonce == "" {
		t.Error("expected a fresh nonce for unknown wallet")
	}
}

func TestLoginOrRegister_NewUser(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAuthUsecase(db)

	tokenStr, err := uc.LoginOrRegister(context.Background(), "0xNewUser001", "")
	if err != nil {
		t.Fatalf("LoginOrRegister failed: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("expected a JWT token")
	}

	// Verify the JWT payload
	claims := parseTestJWT(t, tokenStr)
	if claims["wallet_address"] != "0xnewuser001" {
		t.Errorf("wallet_address = %v, want 0xnewuser001", claims["wallet_address"])
	}
	if claims["role"] != string(domain.RoleF2P) {
		t.Errorf("role = %v, want F2P", claims["role"])
	}

	// Inventory should have been created
	var inv domain.Inventory
	uid, _ := uuid.Parse(claims["user_id"].(string))
	if err := db.Where("user_id = ?", uid).First(&inv).Error; err != nil {
		t.Errorf("inventory not created for new user: %v", err)
	}
	if inv.LandSlots != 1 || !inv.HasBarn {
		t.Errorf("starter pack wrong: landSlots=%d hasBarn=%v", inv.LandSlots, inv.HasBarn)
	}
}

func TestLoginOrRegister_ExistingUser(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAuthUsecase(db)
	user := seedUser(t, db, "0xexisting", domain.RoleF2P)

	tokenStr, err := uc.LoginOrRegister(context.Background(), user.WalletAddress, "")
	if err != nil {
		t.Fatalf("LoginOrRegister failed: %v", err)
	}

	claims := parseTestJWT(t, tokenStr)
	if claims["user_id"] != user.ID.String() {
		t.Errorf("user_id = %v, want %s", claims["user_id"], user.ID.String())
	}
}

func TestLoginOrRegister_WithReferrer(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAuthUsecase(db)

	referrer := seedUser(t, db, "0xreferrer", domain.RoleF2P)

	tokenStr, err := uc.LoginOrRegister(context.Background(), "0xreferred", referrer.WalletAddress)
	if err != nil {
		t.Fatalf("LoginOrRegister with referrer failed: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("expected JWT token")
	}

	// Verify the referrer was set
	var newUser domain.User
	db.Where("wallet_address = ?", "0xreferred").First(&newUser)
	if newUser.ReferrerID == nil {
		t.Fatal("expected referrer to be set")
	}
	if *newUser.ReferrerID != referrer.ID {
		t.Errorf("referrer ID = %v, want %v", *newUser.ReferrerID, referrer.ID)
	}
}

func TestLoginOrRegister_CannotSelfRefer(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAuthUsecase(db)

	// Wallet tries to refer itself
	tokenStr, err := uc.LoginOrRegister(context.Background(), "0xselfref", "0xselfref")
	if err != nil {
		t.Fatalf("LoginOrRegister should succeed even with self-referral: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("expected token")
	}

	var user domain.User
	db.Where("wallet_address = ?", "0xselfref").First(&user)
	// Self-referral should be ignored; referrer should be nil or dev wallet
	if user.ReferrerID != nil {
		var ref domain.User
		db.Where("id = ?", *user.ReferrerID).First(&ref)
		if ref.WalletAddress == "0xselfref" {
			t.Error("user should not be their own referrer")
		}
	}
}

func TestSeedDevWallet(t *testing.T) {
	db := setupTestDB(t)
	uc := NewAuthUsecase(db)

	uc.SeedDevWallet(context.Background())

	var user domain.User
	err := db.Where("wallet_address = ?", strings.ToLower(DevWalletAddress)).First(&user).Error
	if err != nil {
		t.Fatalf("SeedDevWallet did not create user: %v", err)
	}
	if user.Role != domain.RoleAdmin {
		t.Errorf("role = %v, want ADMIN", user.Role)
	}

	// Calling again should not fail
	uc.SeedDevWallet(context.Background())
}

func TestGenerateJWT(t *testing.T) {
	user := domain.User{
		ID:            uuid.New(),
		WalletAddress: "0xtestaddr",
		Role:          domain.RoleSultan,
	}

	tokenStr, err := generateJWT(user)
	if err != nil {
		t.Fatalf("generateJWT error: %v", err)
	}

	claims := parseTestJWT(t, tokenStr)
	if claims["user_id"] != user.ID.String() {
		t.Errorf("user_id mismatch")
	}
	if claims["role"] != string(domain.RoleSultan) {
		t.Errorf("role = %v, want SULTAN", claims["role"])
	}
}

// parseTestJWT is a helper that parses a JWT without full validation (for test assertions).
func parseTestJWT(t *testing.T, tokenStr string) jwt.MapClaims {
	t.Helper()
	token, _, err := jwt.NewParser().ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("failed to parse JWT: %v", err)
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("failed to cast claims")
	}
	return claims
}
