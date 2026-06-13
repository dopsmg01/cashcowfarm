package utils

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestVerifyEthSignature_Valid(t *testing.T) {
	// Generate a fresh key pair
	privKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	address := crypto.PubkeyToAddress(privKey.PublicKey).Hex()

	message := "Sign this nonce: abc-123-xyz"

	// personal_sign hash (must match the format in VerifyEthSignature)
	prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
	hash := crypto.Keccak256Hash([]byte(prefixed))

	sig, err := crypto.Sign(hash.Bytes(), privKey)
	if err != nil {
		t.Fatalf("failed to sign: %v", err)
	}
	// Adjust V to Ethereum convention (27/28)
	sig[64] += 27
	sigHex := hexutil.Encode(sig)

	err = VerifyEthSignature(message, sigHex, address)
	if err != nil {
		t.Errorf("valid signature rejected: %v", err)
	}
}

func TestVerifyEthSignature_WrongAddress(t *testing.T) {
	privKey, _ := crypto.GenerateKey()
	message := "test message"

	prefixed := fmt.Sprintf("\x19Ethereum Signed Message:\n%d%s", len(message), message)
	hash := crypto.Keccak256Hash([]byte(prefixed))
	sig, _ := crypto.Sign(hash.Bytes(), privKey)
	sig[64] += 27
	sigHex := hexutil.Encode(sig)

	err := VerifyEthSignature(message, sigHex, "0x0000000000000000000000000000000000000001")
	if err == nil {
		t.Error("expected error for wrong address")
	}
}

func TestVerifyEthSignature_InvalidHex(t *testing.T) {
	err := VerifyEthSignature("msg", "not-hex", "0xaddr")
	if err == nil {
		t.Error("expected error for invalid hex")
	}
}

func TestVerifyEthSignature_WrongLength(t *testing.T) {
	err := VerifyEthSignature("msg", "0xabcdef", "0xaddr")
	if err == nil {
		t.Error("expected error for wrong signature length")
	}
}
