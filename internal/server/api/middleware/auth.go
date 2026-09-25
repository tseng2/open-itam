package middleware

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// DefaultJWTSecret 未配置时的内置回落值：仅保证存量部署升级后开箱可跑
//（向后兼容），生产必须经 server.json jwt_secret 或环境变量覆盖
const DefaultJWTSecret = "itagent-super-secret-key-change-in-prod"

// JWTSecret JWT 签名/校验密钥：包级 var，main 启动时读配置经 SetJWTSecret
// 注入（GenerateToken/ParseToken/AuthMiddleware 签名零波及，登录自动生效）。
// secret 变更后存量 token 全失效（401 → 前端跳登录）属预期行为
var JWTSecret = []byte(DefaultJWTSecret)

// SetJWTSecret 启动时注入签名密钥；空串忽略（保持现值）
func SetJWTSecret(secret string) {
	if secret == "" {
		return
	}
	JWTSecret = []byte(secret)
}

type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// GenerateToken generates a JWT token for a user
func GenerateToken(userID int64, username, role string) (string, error) {
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "itagent",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(JWTSecret)
}

// ParseToken parses and validates a token string, returning claims if valid
func ParseToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return JWTSecret, nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid or expired token")
	}
	return claims, nil
}

// AuthMiddleware validates the JWT token and injects claims into the context
func AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "missing authorization header"})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid authorization format"})
			c.Abort()
			return
		}

		tokenString := parts[1]
		claims := &Claims{}

		token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return JWTSecret, nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "invalid or expired token"})
			c.Abort()
			return
		}

		// Inject user info into context
		c.Set("userID", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)

		c.Next()
	}
}

// RoleMiddleware checks if the authenticated user has the required role
func RoleMiddleware(allowedRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "unauthorized"})
			c.Abort()
			return
		}

		roleStr, ok := userRole.(string)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "invalid role type in context"})
			c.Abort()
			return
		}

		// Super Admin always has access to everything unless specifically restricted (which is rare)
		// But let's just check if the user's role is in the allowed list, OR they are super_admin.
		// Usually super_admin passes all checks.
		if roleStr == "super_admin" {
			c.Next()
			return
		}

		allowed := false
		for _, r := range allowedRoles {
			if roleStr == r {
				allowed = true
				break
			}
		}

		if !allowed {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "forbidden: insufficient permissions"})
			c.Abort()
			return
		}

		c.Next()
	}
}
