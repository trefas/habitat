package httpapi

import (
	"context"
	"net/http"
	"strings"

	"habitat/internal/auth"
)

type contextKey string

const claimsKey contextKey = "claims"

// RequireAuth проверяет наличие и валидность Bearer-токена.
func RequireAuth(tm auth.TokenManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			raw, ok := strings.CutPrefix(authHeader, "Bearer ")
			if !ok || raw == "" {
				writeError(w, http.StatusUnauthorized, "invalid_token", "Требуется Bearer-токен")
				return
			}

			claims, err := tm.ParseAccess(raw)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid_token", "Токен недействителен или истёк")
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole проверяет, что роль пользователя соответствует одной из разрешенных.
func RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(claimsKey).(*auth.Claims)
			if !ok {
				writeError(w, http.StatusInternalServerError, "internal_error", "Claims не найдены в контексте")
				return
			}

			for _, role := range allowedRoles {
				if claims.Role == role {
					next.ServeHTTP(w, r)
					return
				}
			}

			writeError(w, http.StatusForbidden, "forbidden", "Недостаточно прав для выполнения этого действия")
		})
	}
}

// GetClaims извлекает claims из контекста (удобно для хендлеров).
func GetClaims(ctx context.Context) (*auth.Claims, bool) {
	claims, ok := ctx.Value(claimsKey).(*auth.Claims)
	return claims, ok
}
