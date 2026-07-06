package handler

import (
	"encoding/json"
	"net/http"

	"github.com/yeying-community/warehouse/internal/application/service"
	"github.com/yeying-community/warehouse/internal/domain/addressbook"
	"github.com/yeying-community/warehouse/internal/interface/http/middleware"
	"go.uber.org/zap"
)

type AddressBookHandler struct {
	service *service.AddressBookService
	logger  *zap.Logger
}

func NewAddressBookHandler(service *service.AddressBookService, logger *zap.Logger) *AddressBookHandler {
	return &AddressBookHandler{
		service: service,
		logger:  logger,
	}
}

func (h *AddressBookHandler) HandleGroupList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	groups, err := h.service.ListGroups(r.Context(), u)
	if err != nil {
		h.logger.Error("failed to list groups", zap.Error(err))
		http.Error(w, "Failed to list groups", http.StatusInternalServerError)
		return
	}
	type item struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		CanManage bool   `json:"canManage"`
		CreatedAt string `json:"createdAt"`
	}
	resp := struct {
		Items []item `json:"items"`
	}{Items: make([]item, 0, len(groups))}
	for _, g := range groups {
		resp.Items = append(resp.Items, item{
			ID:        g.ID,
			Name:      g.Name,
			CanManage: g.UserID == u.ID,
			CreatedAt: g.CreatedAt.Format(timeLayout),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *AddressBookHandler) HandleGroupCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	group, err := h.service.CreateGroup(r.Context(), u, req.Name)
	if err != nil {
		if err == addressbook.ErrDuplicateGroupName {
			http.Error(w, "Group name already exists", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":        group.ID,
		"name":      group.Name,
		"canManage": true,
		"createdAt": group.CreatedAt.Format(timeLayout),
	})
}

func (h *AddressBookHandler) HandleGroupUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if err := h.service.RenameGroup(r.Context(), u, req.ID, req.Name); err != nil {
		if err == addressbook.ErrGroupNotFound {
			http.Error(w, "Group not found", http.StatusNotFound)
			return
		}
		if err == addressbook.ErrDuplicateGroupName {
			http.Error(w, "Group name already exists", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *AddressBookHandler) HandleGroupDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if err := h.service.DeleteGroup(r.Context(), u, req.ID); err != nil {
		if err == addressbook.ErrGroupNotFound {
			http.Error(w, "Group not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *AddressBookHandler) HandleMemberList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	members, err := h.service.ListMembers(r.Context(), u)
	if err != nil {
		h.logger.Error("failed to list group members", zap.Error(err))
		http.Error(w, "Failed to list group members", http.StatusInternalServerError)
		return
	}
	type item struct {
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		WalletAddress string   `json:"walletAddress"`
		GroupID       string   `json:"groupId"`
		Tags          []string `json:"tags"`
		Status        string   `json:"status"`
		CanManage     bool     `json:"canManage"`
		CreatedAt     string   `json:"createdAt"`
	}
	resp := struct {
		Items []item `json:"items"`
	}{Items: make([]item, 0, len(members))}
	for _, m := range members {
		resp.Items = append(resp.Items, item{
			ID:            m.ID,
			Name:          m.Name,
			WalletAddress: m.WalletAddress,
			GroupID:       m.GroupID,
			Tags:          m.Tags,
			Status:        m.Status,
			CanManage:     m.UserID == u.ID,
			CreatedAt:     m.CreatedAt.Format(timeLayout),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *AddressBookHandler) HandleMemberCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		Name          string   `json:"name"`
		WalletAddress string   `json:"walletAddress"`
		GroupID       string   `json:"groupId"`
		Tags          []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	member, err := h.service.CreateMember(r.Context(), u, req.Name, req.WalletAddress, req.GroupID, req.Tags)
	if err != nil {
		if err == addressbook.ErrDuplicateMember {
			http.Error(w, "Member already exists in group", http.StatusConflict)
			return
		}
		if err == addressbook.ErrGroupNotFound {
			http.Error(w, "Group not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":            member.ID,
		"name":          member.Name,
		"walletAddress": member.WalletAddress,
		"groupId":       member.GroupID,
		"tags":          member.Tags,
		"status":        member.Status,
		"canManage":     member.UserID == u.ID,
		"createdAt":     member.CreatedAt.Format(timeLayout),
	})
}

func (h *AddressBookHandler) HandleMemberUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		ID            string    `json:"id"`
		Name          string    `json:"name"`
		WalletAddress string    `json:"walletAddress"`
		GroupID       string    `json:"groupId"`
		Tags          *[]string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	member, err := h.service.UpdateMember(r.Context(), u, req.ID, req.Name, req.WalletAddress, req.GroupID, req.Tags)
	if err != nil {
		if err == addressbook.ErrMemberNotFound {
			http.Error(w, "Member not found", http.StatusNotFound)
			return
		}
		if err == addressbook.ErrDuplicateMember {
			http.Error(w, "Member already exists in group", http.StatusConflict)
			return
		}
		if err == addressbook.ErrGroupNotFound {
			http.Error(w, "Group not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":            member.ID,
		"name":          member.Name,
		"walletAddress": member.WalletAddress,
		"groupId":       member.GroupID,
		"tags":          member.Tags,
		"status":        member.Status,
		"canManage":     member.UserID == u.ID,
	})
}

func (h *AddressBookHandler) HandleMemberApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if err := h.service.ApproveMember(r.Context(), u, req.ID); err != nil {
		if err == addressbook.ErrMemberNotFound {
			http.Error(w, "Member not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *AddressBookHandler) HandleMemberReject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if err := h.service.RejectMember(r.Context(), u, req.ID); err != nil {
		if err == addressbook.ErrMemberNotFound {
			http.Error(w, "Member not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *AddressBookHandler) HandleMemberDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if err := h.service.DeleteMember(r.Context(), u, req.ID); err != nil {
		if err == addressbook.ErrMemberNotFound {
			http.Error(w, "Member not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}
