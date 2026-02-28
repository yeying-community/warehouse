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
		CreatedAt string `json:"createdAt"`
	}
	resp := struct {
		Items []item `json:"items"`
	}{Items: make([]item, 0, len(groups))}
	for _, g := range groups {
		resp.Items = append(resp.Items, item{
			ID:        g.ID,
			Name:      g.Name,
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

func (h *AddressBookHandler) HandleContactList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u, ok := middleware.GetUserFromContext(r.Context())
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	contacts, err := h.service.ListContacts(r.Context(), u)
	if err != nil {
		h.logger.Error("failed to list contacts", zap.Error(err))
		http.Error(w, "Failed to list contacts", http.StatusInternalServerError)
		return
	}
	type item struct {
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		WalletAddress string   `json:"walletAddress"`
		GroupID       string   `json:"groupId,omitempty"`
		Tags          []string `json:"tags"`
		CreatedAt     string   `json:"createdAt"`
	}
	resp := struct {
		Items []item `json:"items"`
	}{Items: make([]item, 0, len(contacts))}
	for _, c := range contacts {
		resp.Items = append(resp.Items, item{
			ID:            c.ID,
			Name:          c.Name,
			WalletAddress: c.WalletAddress,
			GroupID:       c.GroupID,
			Tags:          c.Tags,
			CreatedAt:     c.CreatedAt.Format(timeLayout),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *AddressBookHandler) HandleContactCreate(w http.ResponseWriter, r *http.Request) {
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
	contact, err := h.service.CreateContact(r.Context(), u, req.Name, req.WalletAddress, req.GroupID, req.Tags)
	if err != nil {
		if err == addressbook.ErrDuplicateWallet {
			http.Error(w, "Wallet address already exists", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":            contact.ID,
		"name":          contact.Name,
		"walletAddress": contact.WalletAddress,
		"groupId":       contact.GroupID,
		"tags":          contact.Tags,
		"createdAt":     contact.CreatedAt.Format(timeLayout),
	})
}

func (h *AddressBookHandler) HandleContactUpdate(w http.ResponseWriter, r *http.Request) {
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
	contact, err := h.service.UpdateContact(r.Context(), u, req.ID, req.Name, req.WalletAddress, req.GroupID, req.Tags)
	if err != nil {
		if err == addressbook.ErrContactNotFound {
			http.Error(w, "Contact not found", http.StatusNotFound)
			return
		}
		if err == addressbook.ErrDuplicateWallet {
			http.Error(w, "Wallet address already exists", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":            contact.ID,
		"name":          contact.Name,
		"walletAddress": contact.WalletAddress,
		"groupId":       contact.GroupID,
		"tags":          contact.Tags,
	})
}

func (h *AddressBookHandler) HandleContactDelete(w http.ResponseWriter, r *http.Request) {
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
	if err := h.service.DeleteContact(r.Context(), u, req.ID); err != nil {
		if err == addressbook.ErrContactNotFound {
			http.Error(w, "Contact not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}
