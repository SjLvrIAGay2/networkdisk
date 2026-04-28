package handler

import (
	"net/http"
	"strconv"

	"networkdisk/internal/logging"
	"networkdisk/internal/service"
)

type SystemHandler struct {
	fileSvc *service.FileService
}

func NewSystemHandler(fileSvc *service.FileService) *SystemHandler {
	return &SystemHandler{fileSvc: fileSvc}
}

func (h *SystemHandler) StorageStats(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	active, recycle, used, err := h.fileSvc.StorageStats(userID)
	if err != nil {
		logging.Error(r.Context(), "system", "storage stats failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"active_bytes":  active,
		"recycle_bytes": recycle,
		"used_bytes":    used,
	})
}

func (h *SystemHandler) AuditLogs(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	q := r.URL.Query()
	action := q.Get("action")
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit <= 0 {
		limit = 20
	}
	offset, err := strconv.Atoi(q.Get("offset"))
	if err != nil || offset < 0 {
		offset = 0
	}
	logs, err := h.fileSvc.AuditLogs(userID, action, limit, offset)
	if err != nil {
		logging.Error(r.Context(), "system", "audit logs failed", "error", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
		return
	}
	type entry struct {
		ID         int64  `json:"id"`
		UserID     int64  `json:"user_id"`
		Action     string `json:"action"`
		TargetType string `json:"target_type"`
		TargetID   int64  `json:"target_id"`
		Detail     string `json:"detail"`
		IP         string `json:"ip"`
		CreatedAt  string `json:"created_at"`
	}
	entries := make([]entry, 0, len(logs))
	for _, l := range logs {
		entries = append(entries, entry{
			ID: l.ID, UserID: l.UserID, Action: l.Action,
			TargetType: l.TargetType, TargetID: l.TargetID,
			Detail: l.Detail, IP: l.IP,
			CreatedAt: l.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	if entries == nil {
		entries = []entry{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"logs": entries})
}
