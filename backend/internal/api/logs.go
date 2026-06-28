// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"net/http"
	"sort"
	"strconv"

	"zxy-panel/backend/internal/model"
)

func (r *Router) logs(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	limit := 200
	if v := req.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	r.store.Mu.RLock()
	defer r.store.Mu.RUnlock()
	list := make([]model.OperationLog, 0, len(r.store.Data.OperationLogs))
	for _, l := range r.store.Data.OperationLogs {
		list = append(list, l)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	if len(list) > limit {
		list = list[:limit]
	}
	writeJSON(w, http.StatusOK, list)
}
