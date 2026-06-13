package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func generateTestJWT(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "super_secret_dev_key_123!"
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("generateTestJWT: %v", err)
	}
	return s
}

func TestRequireAuth_ValidToken(t *testing.T) {
	tokenStr := generateTestJWT(t, jwt.MapClaims{
		"user_id":        "abc-123",
		"wallet_address": "0xtest",
		"role":           "F2P",
		"exp":            time.Now().Add(time.Hour).Unix(),
	})

	r := gin.New()
	r.Use(RequireAuth())
	r.GET("/test", func(c *gin.Context) {
		uid, _ := c.Get("user_id")
		role, _ := c.Get("role")
		c.JSON(200, gin.H{"user_id": uid, "role": role})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestRequireAuth_MissingToken(t *testing.T) {
	r := gin.New()
	r.Use(RequireAuth())
	r.GET("/test", func(c *gin.Context) { c.Status(200) })

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRequireAuth_InvalidFormat(t *testing.T) {
	r := gin.New()
	r.Use(RequireAuth())
	r.GET("/test", func(c *gin.Context) { c.Status(200) })

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Token abc123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestRequireAuth_ExpiredToken(t *testing.T) {
	tokenStr := generateTestJWT(t, jwt.MapClaims{
		"user_id":        "abc-123",
		"wallet_address": "0xtest",
		"role":           "F2P",
		"exp":            time.Now().Add(-time.Hour).Unix(),
	})

	r := gin.New()
	r.Use(RequireAuth())
	r.GET("/test", func(c *gin.Context) { c.Status(200) })

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestRequireAuth_WrongSecret(t *testing.T) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": "abc-123",
		"role":    "F2P",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	tokenStr, _ := token.SignedString([]byte("wrong_secret_key"))

	r := gin.New()
	r.Use(RequireAuth())
	r.GET("/test", func(c *gin.Context) { c.Status(200) })

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

// --- RequireRole tests ---

func TestRequireRole_Allowed(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("role", "ADMIN"); c.Next() })
	r.Use(RequireRole("ADMIN", "SULTAN"))
	r.GET("/admin", func(c *gin.Context) { c.Status(200) })

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestRequireRole_Forbidden(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("role", "F2P"); c.Next() })
	r.Use(RequireRole("ADMIN"))
	r.GET("/admin", func(c *gin.Context) { c.Status(200) })

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestRequireRole_MissingRole(t *testing.T) {
	r := gin.New()
	r.Use(RequireRole("ADMIN"))
	r.GET("/admin", func(c *gin.Context) { c.Status(200) })

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestRequireRole_NonStringRole(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("role", 42); c.Next() })
	r.Use(RequireRole("ADMIN"))
	r.GET("/admin", func(c *gin.Context) { c.Status(200) })

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403 for non-string role", w.Code)
	}
}
