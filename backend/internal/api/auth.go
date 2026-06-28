// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"net/http"
	"time"

	"zxy-panel/backend/internal/security"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (r *Router) login(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body loginRequest
	if err := readJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	admin, ok := r.store.FindAdminByUsername(body.Username)
	if !ok || !admin.Enabled || !security.VerifyPassword(body.Password, admin.PasswordHash) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	token, err := security.SignJWT(r.jwtSecret, security.Claims{Sub: admin.ID, Username: admin.Username, Role: admin.Role, Exp: time.Now().Add(24 * time.Hour).Unix()})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "token error"})
		return
	}
	r.store.UpdateAdminLogin(admin.ID, clientIP(req))
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user": map[string]string{"username": admin.Username, "role": admin.Role}})
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (r *Router) changePassword(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	claims := currentClaims(req)
	var body changePasswordRequest
	if err := readJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if len(body.NewPassword) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new password must be at least 8 characters"})
		return
	}
	if err := r.store.ChangeAdminPassword(claims.Sub, body.OldPassword, body.NewPassword); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	r.store.Mu.Lock()
	r.store.AddLog(claims.Username, "admin.change_password", clientIP(req), claims.Username)
	_ = r.store.SaveLocked()
	r.store.Mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
