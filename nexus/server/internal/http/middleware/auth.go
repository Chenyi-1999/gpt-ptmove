package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pt-nexus/server/internal/service"
)

// JWTGuard 对 /api 路由进行身份校验，并保留必要的白名单放行逻辑。
func JWTGuard(authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/api") {
			c.Next()
			return
		}
		if path == "/api/auth/login" || path == "/api/auth/status" || strings.HasPrefix(path, "/api/v1/") || c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}
		// 内部令牌只允许明确的后端协作端点，不能用于配置、密钥或自动化管理。
		internalPath := path == "/api/migrate/get_db_seed_info" || strings.HasPrefix(path, "/api/cross-seed-data/batch-cross-seed-core") || strings.HasPrefix(path, "/api/cross-seed-data/batch-cross-seed-internal")
		if internalPath && isLoopbackIP(c.ClientIP()) && validateInternalToken(strings.TrimSpace(c.GetHeader("X-Internal-API-Key"))) {
			c.Set(ContextKeyUserID, "内部调用")
			c.Next()
			return
		}

		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.JSON(401, gin.H{"success": false, "message": "未授权"})
			c.Abort()
			return
		}

		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		claims, err := authService.ValidateJWT(token)
		if err != nil {
			if errors.Is(err, jwt.ErrTokenExpired) {
				c.JSON(401, gin.H{"success": false, "message": "登录已过期"})
				c.Abort()
				return
			}
			c.JSON(401, gin.H{"success": false, "message": "无效的令牌"})
			c.Abort()
			return
		}

		subject, _ := claims["sub"].(string)
		if !authService.IsUserStillValid(subject) {
			c.JSON(401, gin.H{"success": false, "message": "用户已失效，请重新登录"})
			c.Abort()
			return
		}

		if strings.TrimSpace(subject) == "" {
			subject = "未知用户"
		}
		c.Set(ContextKeyUserID, subject)
		c.Next()
	}
}

// isLoopbackIP 判断请求来源是否为本机地址。
func isLoopbackIP(ip string) bool {
	return ip == "127.0.0.1" || ip == "::1" || ip == "localhost"
}

// validateInternalToken 校验内部接口调用令牌，允许小时级时间偏差。
func validateInternalToken(token string) bool {
	secret := os.Getenv("INTERNAL_SECRET")
	if len(secret) < 32 {
		return false
	}
	currentHour := time.Now().Unix() / 3600
	for _, offset := range []int64{-2, -1, 0, 1, 2} {
		hour := currentHour + offset
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte("pt-nexus-internal-" + strconv.FormatInt(hour, 10)))
		digest := hex.EncodeToString(mac.Sum(nil))
		if len(digest) >= 16 {
			digest = digest[:16]
		}
		if hmac.Equal([]byte(digest), []byte(token)) {
			return true
		}
	}
	return false
}
