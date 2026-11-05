// Package gateway translates procurement HTTP into gRPC and owns local identity routes.
// Only its identity module accesses auth.*; procurement tables remain service-owned.
package gateway

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	pb "procureflow-ai/gen/procureflow/v1"
	"procureflow-ai/internal/auth"
	"procureflow-ai/internal/domain"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// Narrow client interfaces keep tests focused on the RPCs exposed over HTTP.
type PurchaseClient interface {
	CreatePurchase(context.Context, *pb.CreatePurchaseRequest, ...grpc.CallOption) (*pb.Purchase, error)
	GetPurchase(context.Context, *pb.GetPurchaseRequest, ...grpc.CallOption) (*pb.Purchase, error)
	ListPurchases(context.Context, *pb.ListPurchasesRequest, ...grpc.CallOption) (*pb.ListPurchasesResponse, error)
}

type ApprovalClient interface {
	DecideApproval(context.Context, *pb.DecideApprovalRequest, ...grpc.CallOption) (*pb.Approval, error)
}

type Handler struct {
	accounts         *auth.Sessions
	purchases        PurchaseClient
	approvals        ApprovalClient
	assistant        AssistantClient
	assistantTimeout time.Duration
	timeout          time.Duration
	maxBody          int64
	origin           string
	logger           *slog.Logger
	mux              *http.ServeMux
}

func NewHandler(purchases PurchaseClient, approvals ApprovalClient, timeout time.Duration, maxBody int64, origin string, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{purchases: purchases, approvals: approvals, timeout: timeout, maxBody: maxBody, origin: origin, logger: logger, mux: http.NewServeMux()}
	h.route("/healthz", map[string]http.HandlerFunc{http.MethodGet: h.health})
	h.route("/api/auth/signup", map[string]http.HandlerFunc{http.MethodPost: h.signup})
	h.route("/api/admin/users", map[string]http.HandlerFunc{http.MethodGet: h.listUsers})
	h.route("/api/admin/users/{id}/role", map[string]http.HandlerFunc{http.MethodPost: h.changeRole})
	h.route("/api/auth/login", map[string]http.HandlerFunc{http.MethodPost: h.login})
	h.route("/api/auth/me", map[string]http.HandlerFunc{http.MethodGet: h.me})
	h.route("/api/auth/logout", map[string]http.HandlerFunc{http.MethodPost: h.logout})
	h.route("/api/purchases", map[string]http.HandlerFunc{http.MethodGet: h.list, http.MethodPost: h.create})
	h.route("/api/purchases/{id}", map[string]http.HandlerFunc{http.MethodGet: h.get})
	h.route("/api/purchases/{id}/decision", map[string]http.HandlerFunc{http.MethodPost: h.decide})
	h.route("/api/assistant/ask", map[string]http.HandlerFunc{http.MethodPost: h.askPolicy})
	h.mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, 404, "NOT_FOUND", "route not found", false)
	})
	return h
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *responseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	out := &responseWriter{ResponseWriter: w}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Add("Vary", "Origin")
	defer func() {
		if recovered := recover(); recovered != nil {
			h.logger.ErrorContext(r.Context(), "HTTP handler panic", "method", r.Method)
			if out.status == 0 {
				writeError(out, 500, "INTERNAL", "internal gateway error", false)
			}
		}
		h.logger.InfoContext(r.Context(), "HTTP request completed", "method", r.Method, "route", r.Pattern, "status", out.status, "duration", time.Since(start))
	}()
	if origins := r.Header.Values("Origin"); len(origins) > 1 || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != h.origin) {
		writeError(out, 403, "ORIGIN_DENIED", "browser origin is not allowed", false)
		return
	}
	if r.Header.Get("Origin") != "" {
		w.Header().Set("Access-Control-Allow-Origin", h.origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Expose-Headers", "Location")
	}
	// Reject noncanonical paths instead of ServeMux's HTML redirect response.
	if path.Clean(r.URL.Path) != r.URL.Path {
		writeError(out, 404, "NOT_FOUND", "route not found", false)
		return
	}
	if r.Method != http.MethodOptions && strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/auth/login" && r.URL.Path != "/api/auth/signup" {
		var ok bool
		r, ok = h.authenticateRequest(out, r)
		if !ok {
			return
		}
	}
	h.mux.ServeHTTP(out, r)
}

func (h *Handler) route(pattern string, methods map[string]http.HandlerFunc) {
	allowed := make([]string, 0, len(methods))
	for method := range methods {
		allowed = append(allowed, method)
	}
	slices.Sort(allowed)
	h.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Add("Vary", "Access-Control-Request-Method")
			w.Header().Add("Vary", "Access-Control-Request-Headers")
			if r.Header.Get("Origin") == "" || !slices.Contains(allowed, r.Header.Get("Access-Control-Request-Method")) {
				writeError(w, 403, "PREFLIGHT_DENIED", "origin and an allowed preflight method are required", false)
				return
			}
			for _, header := range strings.Split(r.Header.Get("Access-Control-Request-Headers"), ",") {
				if header = strings.TrimSpace(header); header != "" && !strings.EqualFold(header, "Content-Type") && !strings.EqualFold(header, "X-CSRF-Token") {
					writeError(w, 403, "PREFLIGHT_DENIED", "only Content-Type and X-CSRF-Token request headers are allowed", false)
					return
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(allowed, ", "))
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		fn, ok := methods[r.Method]
		if !ok {
			w.Header().Set("Allow", strings.Join(allowed, ", "))
			writeError(w, 405, "METHOD_NOT_ALLOWED", "method not allowed for this route", false)
			return
		}
		fn(w, r)
	})
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}{"ok", "api-gateway"})
}

type purchaseJSON struct {
	ID              string `json:"id"`
	ClientRequestID string `json:"client_request_id"`
	EmployeeID      string `json:"employee_id"`
	ItemName        string `json:"item_name"`
	Quantity        int64  `json:"quantity"`
	UnitPriceCents  int64  `json:"unit_price_cents"`
	TotalCents      int64  `json:"total_cents"`
	Status          string `json:"status"`
}

func statusName(s pb.Status) string {
	switch s {
	case pb.Status_STATUS_PENDING:
		return "PENDING"
	case pb.Status_STATUS_APPROVED:
		return "APPROVED"
	case pb.Status_STATUS_REJECTED:
		return "REJECTED"
	default:
		return "UNSPECIFIED"
	}
}

func purchaseResponse(p *pb.Purchase) purchaseJSON {
	return purchaseJSON{p.Id, p.ClientRequestId, p.EmployeeId, p.ItemName, p.Quantity, p.UnitPriceCents, p.TotalCents, statusName(p.Status)}
}

func (h *Handler) backendFailure(w http.ResponseWriter, r *http.Request, err error) {
	// Error text from a peer can contain internal data, so log/map only its code.
	h.logger.WarnContext(r.Context(), "backend RPC failed", "route", r.Pattern, "grpc_code", status.Code(err).String())
	upstreamError(w, err)
}

func badBackend(w http.ResponseWriter) {
	writeError(w, 502, "BAD_GATEWAY", "backend returned an invalid response", false)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	result, err := h.purchases.ListPurchases(ctx, &pb.ListPurchasesRequest{})
	if err != nil {
		h.backendFailure(w, r, err)
		return
	}
	if result == nil {
		badBackend(w)
		return
	}
	items := make([]purchaseJSON, 0, len(result.Purchases))
	for _, p := range result.Purchases {
		if p == nil {
			badBackend(w)
			return
		}
		items = append(items, purchaseResponse(p))
	}
	writeJSON(w, 200, struct {
		Purchases []purchaseJSON `json:"purchases"`
	}{items})
}

func validID(w http.ResponseWriter, field, value string) bool {
	if err := domain.ValidateID(field, value); err != nil {
		writeError(w, 400, "INVALID_ARGUMENT", err.Error(), false)
		return false
	}
	return true
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(w, "purchase_id", id) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	p, err := h.purchases.GetPurchase(ctx, &pb.GetPurchaseRequest{PurchaseId: id})
	if err != nil {
		h.backendFailure(w, r, err)
		return
	}
	if p == nil {
		badBackend(w)
		return
	}
	writeJSON(w, 200, purchaseResponse(p))
}

type createInput struct {
	ClientRequestID string `json:"client_request_id"`
	EmployeeID      string `json:"employee_id"`
	ItemName        string `json:"item_name"`
	Quantity        *int64 `json:"quantity"`
	UnitPriceCents  *int64 `json:"unit_price_cents"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeJSON[createInput](w, r, h.maxBody)
	if !ok {
		return
	}
	if !validID(w, "client_request_id", in.ClientRequestID) || !validID(w, "employee_id", in.EmployeeID) {
		return
	}
	actor, _ := auth.Identity(r.Context())
	if in.EmployeeID != actor.Subject {
		writeError(w, 403, "FORBIDDEN", "employee_id must match the signed-in account", false)
		return
	}
	if strings.TrimSpace(in.ItemName) == "" || strings.ContainsRune(in.ItemName, '\x00') || utf8.RuneCountInString(in.ItemName) > 200 {
		writeError(w, 400, "INVALID_ARGUMENT", "item_name must contain 1-200 characters and no NUL characters", false)
		return
	}
	if in.Quantity == nil || *in.Quantity <= 0 || in.UnitPriceCents == nil || *in.UnitPriceCents < 0 {
		writeError(w, 400, "INVALID_ARGUMENT", "quantity must be a positive integer and unit_price_cents a nonnegative integer; both are required", false)
		return
	}
	if *in.UnitPriceCents > math.MaxInt64 / *in.Quantity {
		writeError(w, 400, "INVALID_ARGUMENT", "total_cents exceeds int64", false)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	p, err := h.purchases.CreatePurchase(ctx, &pb.CreatePurchaseRequest{ClientRequestId: in.ClientRequestID, EmployeeId: in.EmployeeID, ItemName: in.ItemName, Quantity: *in.Quantity, UnitPriceCents: *in.UnitPriceCents})
	if err != nil {
		h.backendFailure(w, r, err)
		return
	}
	if p == nil {
		badBackend(w)
		return
	}
	w.Header().Set("Location", "/api/purchases/"+p.Id)
	// The RPC does not distinguish a first insert from an identical retry.
	writeJSON(w, http.StatusCreated, purchaseResponse(p))
}

type decisionInput struct {
	// Optional compatibility field; if provided it must match authenticated identity.
	ManagerID string `json:"manager_id"`
	Decision  string `json:"decision"`
	Comment   string `json:"comment"`
}

func (h *Handler) decide(w http.ResponseWriter, r *http.Request) {
	actor, _ := auth.Identity(r.Context())
	if !auth.CanReview(actor.Role) {
		writeError(w, 403, "FORBIDDEN", "manager role required", false)
		return
	}
	id := r.PathValue("id")
	if !validID(w, "purchase_id", id) {
		return
	}
	in, ok := decodeJSON[decisionInput](w, r, h.maxBody)
	if !ok {
		return
	}
	if in.ManagerID != "" && in.ManagerID != actor.Subject {
		writeError(w, 403, "FORBIDDEN", "manager_id must match the signed-in account", false)
		return
	}
	decision := pb.Status_STATUS_UNSPECIFIED
	switch in.Decision {
	case "APPROVED":
		decision = pb.Status_STATUS_APPROVED
	case "REJECTED":
		decision = pb.Status_STATUS_REJECTED
	default:
		writeError(w, 400, "INVALID_ARGUMENT", "decision must be APPROVED or REJECTED", false)
		return
	}
	if utf8.RuneCountInString(in.Comment) > 1000 || strings.ContainsRune(in.Comment, '\x00') {
		writeError(w, 400, "INVALID_ARGUMENT", "comment must be at most 1000 characters and contain no NUL characters", false)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	a, err := h.approvals.DecideApproval(ctx, &pb.DecideApprovalRequest{PurchaseId: id, Status: decision})
	if err != nil {
		h.backendFailure(w, r, err)
		return
	}
	if a == nil {
		badBackend(w)
		return
	}
	// Existing RPCs cannot store manager/comment; make this explicit in the result.
	writeJSON(w, 200, struct {
		PurchaseID        string `json:"purchase_id"`
		Status            string `json:"status"`
		DemoOnly          bool   `json:"demo_only"`
		MetadataPersisted bool   `json:"metadata_persisted"`
	}{a.PurchaseId, statusName(a.Status), false, false})
}
