package handler

import (
	"net/http"
	"strconv"

	"networkdisk/internal/logging"
	"networkdisk/internal/service"
)

type SearchHandler struct {
	svc *service.SearchService
}

func NewSearchHandler(svc *service.SearchService) *SearchHandler {
	return &SearchHandler{svc: svc}
}

func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	query := r.URL.Query().Get("q")
	files, err := h.svc.Search(userID, query)
	if err != nil {
		logging.Error(r.Context(), "search", "search failed", "error", err)
		writeError(w, http.StatusInternalServerError, "搜索失败")
		return
	}
	entries := newFileEntries(files)
	writeJSON(w, http.StatusOK, map[string]interface{}{"files": entries})
}

func (h *SearchHandler) Suggest(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	query := r.URL.Query().Get("q")
	names, err := h.svc.Suggest(userID, query)
	if err != nil {
		logging.Error(r.Context(), "search", "search suggest failed", "error", err)
		writeError(w, http.StatusInternalServerError, "搜索建议失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"suggestions": names})
}

func (h *FileHandler) Recent(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	files, err := h.svc.RecentFiles(userID, limit)
	if err != nil {
		logging.Error(r.Context(), "search", "recent files failed", "error", err)
		writeError(w, http.StatusInternalServerError, "获取最近文件失败")
		return
	}
	entries := newFileEntries(files)
	writeJSON(w, http.StatusOK, map[string]interface{}{"files": entries})
}

func (h *FileHandler) Dashboard(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r)
	if userID == 0 {
		writeError(w, http.StatusUnauthorized, "未授权")
		return
	}
	recentFiles, err := h.svc.RecentFiles(userID, 10)
	if err != nil {
		logging.Error(r.Context(), "search", "dashboard recent files failed", "error", err)
		writeError(w, http.StatusInternalServerError, "获取最近文件失败")
		return
	}
	active, recycle, used, err := h.svc.StorageStats(userID)
	if err != nil {
		logging.Error(r.Context(), "search", "dashboard storage stats failed", "error", err)
		writeError(w, http.StatusInternalServerError, "获取存储统计失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"recent_files":    newFileEntries(recentFiles),
		"active_storage":  active,
		"recycle_storage": recycle,
		"used_storage":    used,
	})
}
