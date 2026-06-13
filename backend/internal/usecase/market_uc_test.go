package usecase

import (
	"context"
	"testing"

	"cashcowvalley/backend/internal/domain"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// --- SellItem tests ---

func TestSellItem_Success(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	seller := seedUser(t, db, "0xseller", domain.RoleF2P)
	db.Model(&domain.Inventory{}).Where("user_id = ?", seller.ID).Update("grass", 20)

	err := uc.SellItem(context.Background(), seller.ID, "GRASS", 5, decimal.NewFromFloat(1.50))
	if err != nil {
		t.Fatalf("SellItem failed: %v", err)
	}

	// Grass deducted
	var inv domain.Inventory
	db.Where("user_id = ?", seller.ID).First(&inv)
	if inv.Grass != 15 {
		t.Errorf("grass = %d, want 15", inv.Grass)
	}

	// Listing created
	var listing domain.MarketListing
	db.Where("seller_id = ?", seller.ID).First(&listing)
	if listing.Status != "OPEN" {
		t.Errorf("listing status = %s, want OPEN", listing.Status)
	}
	if listing.Quantity != 5 {
		t.Errorf("listing quantity = %d, want 5", listing.Quantity)
	}
}

func TestSellItem_Milk(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	seller := seedUser(t, db, "0xmilkseller", domain.RoleF2P)
	db.Model(&domain.Inventory{}).Where("user_id = ?", seller.ID).Update("milk", 10)

	err := uc.SellItem(context.Background(), seller.ID, "MILK", 3, decimal.NewFromFloat(2.00))
	if err != nil {
		t.Fatalf("SellItem MILK failed: %v", err)
	}

	var inv domain.Inventory
	db.Where("user_id = ?", seller.ID).First(&inv)
	if inv.Milk != 7 {
		t.Errorf("milk = %d, want 7", inv.Milk)
	}
}

func TestSellItem_InsufficientGrass(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	seller := seedUser(t, db, "0xpoor", domain.RoleF2P)

	err := uc.SellItem(context.Background(), seller.ID, "GRASS", 5, decimal.NewFromFloat(1.00))
	if err == nil {
		t.Fatal("expected error for insufficient grass")
	}
}

func TestSellItem_InvalidItemType(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	seller := seedUser(t, db, "0xinvalid", domain.RoleF2P)

	err := uc.SellItem(context.Background(), seller.ID, "DIAMOND", 1, decimal.NewFromFloat(1.00))
	if err == nil {
		t.Fatal("expected error for invalid item type")
	}
}

func TestSellItem_ZeroQuantity(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	seller := seedUser(t, db, "0xzero", domain.RoleF2P)

	err := uc.SellItem(context.Background(), seller.ID, "GRASS", 0, decimal.NewFromFloat(1.00))
	if err == nil {
		t.Fatal("expected error for zero quantity")
	}
}

func TestSellItem_PriceTooLow(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	seller := seedUser(t, db, "0xcheap", domain.RoleF2P)

	err := uc.SellItem(context.Background(), seller.ID, "GRASS", 1, decimal.NewFromFloat(0.001))
	if err == nil {
		t.Fatal("expected error for price too low")
	}
}

func TestSellItem_PriceTooHigh(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	seller := seedUser(t, db, "0xexpensive", domain.RoleF2P)

	err := uc.SellItem(context.Background(), seller.ID, "GRASS", 1, decimal.NewFromInt(50000))
	if err == nil {
		t.Fatal("expected error for price too high")
	}
}

// --- BuyItem tests ---

func TestBuyItem_Success(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	seller := seedUser(t, db, "0xbuyseller", domain.RoleF2P)
	buyer := seedUser(t, db, "0xbuyer", domain.RoleF2P)

	// Give seller inventory, buyer USDT
	db.Model(&domain.Inventory{}).Where("user_id = ?", seller.ID).Update("grass", 10)
	db.Model(&domain.User{}).Where("id = ?", buyer.ID).Update("usdt_balance", decimal.NewFromInt(100))

	// Create listing
	listing := domain.MarketListing{
		SellerID:  seller.ID,
		ItemType:  "GRASS",
		Quantity:  5,
		PriceUSDT: decimal.NewFromFloat(10.00),
		Status:    "OPEN",
	}
	db.Create(&listing)

	err := uc.BuyItem(context.Background(), buyer.ID, listing.ID)
	if err != nil {
		t.Fatalf("BuyItem failed: %v", err)
	}

	// Buyer should have grass
	var buyerInv domain.Inventory
	db.Where("user_id = ?", buyer.ID).First(&buyerInv)
	if buyerInv.Grass != 5 {
		t.Errorf("buyer grass = %d, want 5", buyerInv.Grass)
	}

	// Buyer USDT should be deducted
	var buyerUser domain.User
	db.Where("id = ?", buyer.ID).First(&buyerUser)
	if !buyerUser.USDTBalance.Equal(decimal.NewFromInt(90)) {
		t.Errorf("buyer USDT = %s, want 90", buyerUser.USDTBalance.String())
	}

	// Seller USDT should increase
	var sellerUser domain.User
	db.Where("id = ?", seller.ID).First(&sellerUser)
	if !sellerUser.USDTBalance.Equal(decimal.NewFromInt(10)) {
		t.Errorf("seller USDT = %s, want 10", sellerUser.USDTBalance.String())
	}

	// Listing should be SOLD
	var updatedListing domain.MarketListing
	db.Where("id = ?", listing.ID).First(&updatedListing)
	if updatedListing.Status != "SOLD" {
		t.Errorf("listing status = %s, want SOLD", updatedListing.Status)
	}
}

func TestBuyItem_MilkTransfer(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	seller := seedUser(t, db, "0xmilkseller2", domain.RoleF2P)
	buyer := seedUser(t, db, "0xmilkbuyer", domain.RoleF2P)
	db.Model(&domain.User{}).Where("id = ?", buyer.ID).Update("usdt_balance", decimal.NewFromInt(50))

	listing := domain.MarketListing{
		SellerID:  seller.ID,
		ItemType:  "MILK",
		Quantity:  3,
		PriceUSDT: decimal.NewFromFloat(5.00),
		Status:    "OPEN",
	}
	db.Create(&listing)

	err := uc.BuyItem(context.Background(), buyer.ID, listing.ID)
	if err != nil {
		t.Fatalf("BuyItem MILK failed: %v", err)
	}

	var buyerInv domain.Inventory
	db.Where("user_id = ?", buyer.ID).First(&buyerInv)
	if buyerInv.Milk != 3 {
		t.Errorf("buyer milk = %d, want 3", buyerInv.Milk)
	}
}

func TestBuyItem_InsufficientUSDT(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	seller := seedUser(t, db, "0xrichseller", domain.RoleF2P)
	buyer := seedUser(t, db, "0xbrokebuyer", domain.RoleF2P)

	listing := domain.MarketListing{
		SellerID:  seller.ID,
		ItemType:  "GRASS",
		Quantity:  1,
		PriceUSDT: decimal.NewFromInt(1000),
		Status:    "OPEN",
	}
	db.Create(&listing)

	err := uc.BuyItem(context.Background(), buyer.ID, listing.ID)
	if err == nil {
		t.Fatal("expected error for insufficient USDT")
	}
}

func TestBuyItem_CannotBuyOwn(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	user := seedUser(t, db, "0xselfbuyer", domain.RoleF2P)
	db.Model(&domain.User{}).Where("id = ?", user.ID).Update("usdt_balance", decimal.NewFromInt(100))

	listing := domain.MarketListing{
		SellerID:  user.ID,
		ItemType:  "GRASS",
		Quantity:  1,
		PriceUSDT: decimal.NewFromInt(1),
		Status:    "OPEN",
	}
	db.Create(&listing)

	err := uc.BuyItem(context.Background(), user.ID, listing.ID)
	if err == nil {
		t.Fatal("expected error when buying own listing")
	}
}

func TestBuyItem_AlreadySold(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	seller := seedUser(t, db, "0xsoldseller", domain.RoleF2P)
	buyer := seedUser(t, db, "0xlatebuyer", domain.RoleF2P)
	db.Model(&domain.User{}).Where("id = ?", buyer.ID).Update("usdt_balance", decimal.NewFromInt(100))

	listing := domain.MarketListing{
		SellerID:  seller.ID,
		ItemType:  "GRASS",
		Quantity:  1,
		PriceUSDT: decimal.NewFromInt(1),
		Status:    "SOLD",
	}
	db.Create(&listing)

	err := uc.BuyItem(context.Background(), buyer.ID, listing.ID)
	if err == nil {
		t.Fatal("expected error when listing is already SOLD")
	}
}

func TestBuyItem_ListingNotFound(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	buyer := seedUser(t, db, "0xghostbuyer", domain.RoleF2P)

	err := uc.BuyItem(context.Background(), buyer.ID, uuid.New())
	if err == nil {
		t.Fatal("expected error when listing not found")
	}
}

// --- GetListings tests ---

func TestGetListings(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	seller := seedUser(t, db, "0xlistingseller", domain.RoleF2P)

	db.Create(&domain.MarketListing{SellerID: seller.ID, ItemType: "GRASS", Quantity: 1, PriceUSDT: decimal.NewFromInt(1), Status: "OPEN"})
	db.Create(&domain.MarketListing{SellerID: seller.ID, ItemType: "MILK", Quantity: 2, PriceUSDT: decimal.NewFromInt(2), Status: "OPEN"})
	db.Create(&domain.MarketListing{SellerID: seller.ID, ItemType: "GRASS", Quantity: 3, PriceUSDT: decimal.NewFromInt(3), Status: "SOLD"})

	listings, err := uc.GetListings(context.Background())
	if err != nil {
		t.Fatalf("GetListings failed: %v", err)
	}
	if len(listings) != 2 {
		t.Errorf("expected 2 OPEN listings, got %d", len(listings))
	}
}

// --- SellMilkForGold tests ---

func TestSellMilkForGold_Success(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	user := seedUser(t, db, "0xmilkgold", domain.RoleF2P)
	db.Model(&domain.Inventory{}).Where("user_id = ?", user.ID).Update("milk", 10)

	err := uc.SellMilkForGold(context.Background(), user.ID, 4)
	if err != nil {
		t.Fatalf("SellMilkForGold failed: %v", err)
	}

	var inv domain.Inventory
	db.Where("user_id = ?", user.ID).First(&inv)
	if inv.Milk != 6 {
		t.Errorf("milk = %d, want 6", inv.Milk)
	}

	var u domain.User
	db.Where("id = ?", user.ID).First(&u)
	expected := decimal.NewFromInt(20) // 4 * 5
	if !u.GoldBalance.Equal(expected) {
		t.Errorf("gold = %s, want %s", u.GoldBalance.String(), expected.String())
	}
}

func TestSellMilkForGold_InsufficientMilk(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	user := seedUser(t, db, "0xnomilk", domain.RoleF2P)

	err := uc.SellMilkForGold(context.Background(), user.ID, 5)
	if err == nil {
		t.Fatal("expected error for insufficient milk")
	}
}

func TestSellMilkForGold_ZeroQuantity(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	user := seedUser(t, db, "0xzeromilk", domain.RoleF2P)

	err := uc.SellMilkForGold(context.Background(), user.ID, 0)
	if err == nil {
		t.Fatal("expected error for zero quantity")
	}
}

// --- BuyInAppItemWithGold tests ---

func TestBuyInAppItemWithGold_Grass(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	user := seedUser(t, db, "0xgoldbuyer", domain.RoleF2P)
	db.Model(&domain.User{}).Where("id = ?", user.ID).Update("gold_balance", decimal.NewFromInt(500))

	err := uc.BuyInAppItemWithGold(context.Background(), user.ID, "GRASS", 3)
	if err != nil {
		t.Fatalf("BuyInAppItemWithGold GRASS failed: %v", err)
	}

	var u domain.User
	db.Where("id = ?", user.ID).First(&u)
	expected := decimal.NewFromInt(500 - 30) // 3*10
	if !u.GoldBalance.Equal(expected) {
		t.Errorf("gold = %s, want %s", u.GoldBalance.String(), expected.String())
	}

	var inv domain.Inventory
	db.Where("user_id = ?", user.ID).First(&inv)
	if inv.Grass != 3 {
		t.Errorf("grass = %d, want 3", inv.Grass)
	}
}

func TestBuyInAppItemWithGold_Cow(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	user := seedUser(t, db, "0xcowbuyer", domain.RoleF2P)
	db.Model(&domain.User{}).Where("id = ?", user.ID).Update("gold_balance", decimal.NewFromInt(5000))

	err := uc.BuyInAppItemWithGold(context.Background(), user.ID, "COW", 2)
	if err != nil {
		t.Fatalf("BuyInAppItemWithGold COW failed: %v", err)
	}

	var cows []domain.Cow
	db.Where("owner_id = ?", user.ID).Find(&cows)
	if len(cows) != 2 {
		t.Errorf("cow count = %d, want 2", len(cows))
	}
}

func TestBuyInAppItemWithGold_InsufficientGold(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	user := seedUser(t, db, "0xpooruyer", domain.RoleF2P)

	err := uc.BuyInAppItemWithGold(context.Background(), user.ID, "COW", 1)
	if err == nil {
		t.Fatal("expected error for insufficient gold")
	}
}

func TestBuyInAppItemWithGold_InvalidItem(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	user := seedUser(t, db, "0xinvaliditem", domain.RoleF2P)
	db.Model(&domain.User{}).Where("id = ?", user.ID).Update("gold_balance", decimal.NewFromInt(10000))

	err := uc.BuyInAppItemWithGold(context.Background(), user.ID, "UNICORN", 1)
	if err == nil {
		t.Fatal("expected error for invalid item type")
	}
}

// --- SwapGoldToTokens tests ---

func TestSwapGoldToTokens_COW(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	user := seedUser(t, db, "0xswapper", domain.RoleF2P)
	db.Model(&domain.User{}).Where("id = ?", user.ID).Update("gold_balance", decimal.NewFromInt(1000))

	err := uc.SwapGoldToTokens(context.Background(), user.ID, decimal.NewFromInt(500), "COW")
	if err != nil {
		t.Fatalf("SwapGoldToTokens COW failed: %v", err)
	}

	var u domain.User
	db.Where("id = ?", user.ID).First(&u)
	if !u.GoldBalance.Equal(decimal.NewFromInt(500)) {
		t.Errorf("remaining gold = %s, want 500", u.GoldBalance.String())
	}
	expected := decimal.NewFromInt(5) // 500/100
	if !u.Points.Equal(expected) {
		t.Errorf("COW points = %s, want %s", u.Points.String(), expected.String())
	}
}

func TestSwapGoldToTokens_USDT(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	user := seedUser(t, db, "0xswapper2", domain.RoleF2P)
	db.Model(&domain.User{}).Where("id = ?", user.ID).Update("gold_balance", decimal.NewFromInt(50000))

	err := uc.SwapGoldToTokens(context.Background(), user.ID, decimal.NewFromInt(30000), "USDT")
	if err != nil {
		t.Fatalf("SwapGoldToTokens USDT failed: %v", err)
	}

	var u domain.User
	db.Where("id = ?", user.ID).First(&u)
	if !u.GoldBalance.Equal(decimal.NewFromInt(20000)) {
		t.Errorf("remaining gold = %s, want 20000", u.GoldBalance.String())
	}
	expected := decimal.NewFromInt(3) // 30000/10000
	if !u.USDTBalance.Equal(expected) {
		t.Errorf("USDT = %s, want %s", u.USDTBalance.String(), expected.String())
	}
}

func TestSwapGoldToTokens_InsufficientGold(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	user := seedUser(t, db, "0xswapbroke", domain.RoleF2P)

	err := uc.SwapGoldToTokens(context.Background(), user.ID, decimal.NewFromInt(100), "COW")
	if err == nil {
		t.Fatal("expected error for insufficient gold")
	}
}

func TestSwapGoldToTokens_InvalidTarget(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	user := seedUser(t, db, "0xswapinvalid", domain.RoleF2P)
	db.Model(&domain.User{}).Where("id = ?", user.ID).Update("gold_balance", decimal.NewFromInt(100))

	err := uc.SwapGoldToTokens(context.Background(), user.ID, decimal.NewFromInt(100), "BTC")
	if err == nil {
		t.Fatal("expected error for invalid target currency")
	}
}

func TestSwapGoldToTokens_ZeroAmount(t *testing.T) {
	db := setupTestDB(t)
	uc := NewMarketUsecase(db)

	user := seedUser(t, db, "0xswapzero", domain.RoleF2P)

	err := uc.SwapGoldToTokens(context.Background(), user.ID, decimal.Zero, "COW")
	if err == nil {
		t.Fatal("expected error for zero amount")
	}
}
