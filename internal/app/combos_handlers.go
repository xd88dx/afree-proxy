package app

// combos 面板端点（gateway 批：combos 数据/改写域出包 internal/gateway，
// 面板响应封装留装配根）。

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

func handleCombosList(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"combos": listCombos()}})
}

// POST /admin/api/combos  body: { id, platform, target }
func handleComboCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		ID         string `json:"id"`
		Platform   string `json:"platform"`
		Target     string `json:"target"`
		UseProxies bool   `json:"useProxies"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	c, err := addCombo(req.ID, req.Platform, req.Target, req.UseProxies)
	if err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	log.Printf("combo created: %s -> %s/%s", c.ID, c.Platform, c.Target)
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: "combo created", Data: c})
}

// POST /admin/api/combos/delete  body: { id }
func handleComboDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteAPI(w, http.StatusMethodNotAllowed, apiResponse{Error: "method not allowed"})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: err.Error()})
		return
	}
	defer r.Body.Close()
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		WriteAPI(w, http.StatusBadRequest, apiResponse{Error: "invalid JSON"})
		return
	}
	if !deleteCombo(req.ID) {
		WriteAPI(w, http.StatusNotFound, apiResponse{Error: "combo not found: " + req.ID})
		return
	}
	log.Printf("combo deleted: %s", req.ID)
	WriteAPI(w, http.StatusOK, apiResponse{Success: true, Message: "combo deleted"})
}
