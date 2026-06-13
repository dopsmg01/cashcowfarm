package middleware

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"cashcowvalley/backend/pkg/utils"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// RequireAuth validates the JWT token provided by the Web3 Nonce signing process
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			utils.SendError(c, http.StatusUnauthorized, "Akses Ditolak: Token tidak ditemukan", nil)
			c.Abort()
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			utils.SendError(c, http.StatusUnauthorized, "Akses Ditolak: Format token salah", nil)
			c.Abort()
			return
		}

		tokenString := parts[1]
		jwtSecret := os.Getenv("JWT_SECRET")
		if jwtSecret == "" {
			utils.SendError(c, http.StatusInternalServerError, "Server misconfiguration", nil)
			c.Abort()
			return
		}

		token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return []byte(jwtSecret), nil
		})

		if err != nil || !token.Valid {
			utils.SendError(c, http.StatusForbidden, "Sesi berakhir atau token tidak valid", nil)
			c.Abort()
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			utils.SendError(c, http.StatusForbidden, "Klaim token tidak valid", nil)
			c.Abort()
			return
		}

		// BUG FIX (DL2): Konversi claims ke string secara eksplisit
		// JWT claims bisa berupa float64/interface{} tergantung library.
		// c.GetString() hanya bekerja jika tipe persis string.
		c.Set("user_id", fmt.Sprintf("%v", claims["user_id"]))
		c.Set("wallet_address", fmt.Sprintf("%v", claims["wallet_address"]))
		c.Set("role", fmt.Sprintf("%v", claims["role"]))

		c.Next()
	}
}

// RequireRole enforces RBAC
func RequireRole(allowedRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, exists := c.Get("role")
		if !exists {
			utils.SendError(c, http.StatusForbidden, "Akses Terlarang: Role tidak ditemukan", nil)
			c.Abort()
			return
		}

		// BUG FIX (DL1): Safe type assertion — sebelumnya crash PANIC jika role bukan string
		roleStr, ok := role.(string)
		if !ok {
			utils.SendError(c, http.StatusForbidden, "Akses Terlarang: Format role tidak valid", nil)
			c.Abort()
			return
		}
		for _, allowed := range allowedRoles {
			if roleStr == allowed {
				c.Next()
				return
			}
		}

		utils.SendError(c, http.StatusForbidden, "Akses Terlarang: Hak akses tidak mencukupi", nil)
		c.Abort()
	}
}
