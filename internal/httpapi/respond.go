package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"habitat/internal/domain"
)

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorResponse{Error: code, Message: message})
}

// mapDomainError преобразует доменные ошибки в HTTP-статусы и коды.
func mapDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		writeError(w, http.StatusBadRequest, "validation_error", err.Error())
	case errors.Is(err, domain.ErrEmailTaken):
		writeError(w, http.StatusConflict, "email_taken", "Этот email уже зарегистрирован")
	case errors.Is(err, domain.ErrLastAdmin):
		writeError(w, http.StatusConflict, "last_admin", "Нельзя удалить или понизить последнего активного администратора")
	case errors.Is(err, domain.ErrInvalidCredentials),
		errors.Is(err, domain.ErrInvalidRefresh),
		errors.Is(err, domain.ErrRefreshReused),
		errors.Is(err, domain.ErrRefreshExpired):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Неверные учетные данные или токен")
	case errors.Is(err, domain.ErrUserBlocked):
		writeError(w, http.StatusForbidden, "user_blocked", "Аккаунт заблокирован или не активирован")
	case errors.Is(err, domain.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "Недостаточно прав для выполнения этого действия")
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Ресурс не найден")
	default:
		// В продакшене здесь должен быть логгер (slog)
		writeError(w, http.StatusInternalServerError, "internal_error", "Внутренняя ошибка сервера")
	}
}
