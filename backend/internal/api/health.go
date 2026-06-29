// SPDX-License-Identifier: AGPL-3.0-only
package api

import "net/http"

func (r *Router) health(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "zxy-panel-api", "version": "0.7.5.9.1-qr-flow-compatibility-fix-agent-xray"})
}
