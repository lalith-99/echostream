package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

const testSecret = "test-secret-key-for-unit-tests"

func TestGenerateAndParse(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()
	email := "alice@test.com"

	token, err := GenerateToken(userID, tenantID, email, testSecret, 24*time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := ParseToken(token, testSecret)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}

	if claims.UserID != userID {
		t.Errorf("UserID = %v, want %v", claims.UserID, userID)
	}
	if claims.TenantID != tenantID {
		t.Errorf("TenantID = %v, want %v", claims.TenantID, tenantID)
	}
	if claims.Email != email {
		t.Errorf("Email = %q, want %q", claims.Email, email)
	}
	if claims.Issuer != "echostream" {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, "echostream")
	}
}

func TestParseToken_WrongSecret(t *testing.T) {
	token, _ := GenerateToken(uuid.New(), uuid.New(), "a@b.com", testSecret, time.Hour)
	_, err := ParseToken(token, "wrong-secret")
	if err == nil {
		t.Fatal("expected error with wrong secret, got nil")
	}
}

func TestParseToken_Expired(t *testing.T) {
	token, _ := GenerateToken(uuid.New(), uuid.New(), "a@b.com", testSecret, -time.Hour)
	_, err := ParseToken(token, testSecret)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestParseToken_Garbage(t *testing.T) {
	_, err := ParseToken("not.a.jwt", testSecret)
	if err == nil {
		t.Fatal("expected error for garbage token, got nil")
	}
}

func TestParseToken_Empty(t *testing.T) {
	_, err := ParseToken("", testSecret)
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
}
