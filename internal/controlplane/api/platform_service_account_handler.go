package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"mini-cloud/internal/common/operationhistory"
	"mini-cloud/internal/controlplane/serviceaccount"
	"mini-cloud/internal/controlplane/store"
)

type platformServiceAccountHandler struct {
	logger *slog.Logger
	store  *store.Store
}

func newPlatformServiceAccountHandler(logger *slog.Logger, stores *store.Store) platformServiceAccountHandler {
	return platformServiceAccountHandler{
		logger: logger,
		store:  stores,
	}
}

func (h platformServiceAccountHandler) issuePlatformServiceAccount(w http.ResponseWriter, r *http.Request) {
	logger := requestScopedLogger(r, h.logger)

	var input serviceaccount.IssueInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json body"})
		return
	}

	issued, err := h.store.CreatePlatformServiceAccount(r.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, serviceaccount.ErrNameRequired),
			errors.Is(err, serviceaccount.ErrNameInvalid),
			errors.Is(err, serviceaccount.ErrInvalidRole):
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		case errors.Is(err, store.ErrPlatformServiceAccountNameAlreadyUsed):
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("issue control-plane platform service account failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.service_account.issue",
		TargetType: "platform_service_account",
		TargetID:   issued.Account.ID,
		TargetName: issued.Account.Name,
		Details: map[string]any{
			"role":        issued.Account.Role,
			"tokenPrefix": issued.Account.TokenPrefix,
		},
	})

	writeJSON(w, http.StatusCreated, issued)
}

func (h platformServiceAccountHandler) listPlatformServiceAccounts(w http.ResponseWriter, r *http.Request) {
	logger := requestScopedLogger(r, h.logger)

	items, err := h.store.ListPlatformServiceAccounts(r.Context())
	if err != nil {
		logger.Error("list control-plane platform service accounts failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h platformServiceAccountHandler) deletePlatformServiceAccount(w http.ResponseWriter, r *http.Request) {
	accountID := strings.TrimSpace(r.PathValue("accountID"))
	if accountID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "accountID is required"})
		return
	}
	logger := requestScopedLogger(r, h.logger)

	deleted, err := h.store.DeletePlatformServiceAccount(r.Context(), accountID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrPlatformServiceAccountNotFound):
			writeJSON(w, http.StatusNotFound, map[string]any{"error": err.Error()})
			return
		default:
			logger.Error("delete control-plane platform service account failed", "account_id", accountID, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			return
		}
	}

	recordOperationEvent(logger, h.store, r, operationhistory.CreateInput{
		Action:     "control.service_account.delete",
		TargetType: "platform_service_account",
		TargetID:   deleted.ID,
		TargetName: deleted.Name,
		Details: map[string]any{
			"role":        deleted.Role,
			"tokenPrefix": deleted.TokenPrefix,
		},
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": true,
		"account": deleted,
	})
}
