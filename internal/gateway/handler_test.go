package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "procureflow-ai/gen/procureflow/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const createBody = `{"client_request_id":"demo-key","employee_id":"emp-101","item_name":"Laptop","quantity":2,"unit_price_cents":80000}`
const decisionBody = `{"manager_id":"manager-demo","decision":"APPROVED","comment":"Within budget"}`
const vueOrigin = "http://localhost:5173"

type fakePurchases struct {
	create func(context.Context, *pb.CreatePurchaseRequest) (*pb.Purchase, error)
	get    func(context.Context, *pb.GetPurchaseRequest) (*pb.Purchase, error)
	list   func(context.Context, *pb.ListPurchasesRequest) (*pb.ListPurchasesResponse, error)
}

func (f fakePurchases) CreatePurchase(ctx context.Context, req *pb.CreatePurchaseRequest, _ ...grpc.CallOption) (*pb.Purchase, error) {
	return f.create(ctx, req)
}
func (f fakePurchases) GetPurchase(ctx context.Context, req *pb.GetPurchaseRequest, _ ...grpc.CallOption) (*pb.Purchase, error) {
	return f.get(ctx, req)
}
func (f fakePurchases) ListPurchases(ctx context.Context, req *pb.ListPurchasesRequest, _ ...grpc.CallOption) (*pb.ListPurchasesResponse, error) {
	return f.list(ctx, req)
}

type fakeApprovals func(context.Context, *pb.DecideApprovalRequest) (*pb.Approval, error)

func (f fakeApprovals) DecideApproval(ctx context.Context, req *pb.DecideApprovalRequest, _ ...grpc.CallOption) (*pb.Approval, error) {
	return f(ctx, req)
}

func testHandler(p fakePurchases, a fakeApprovals) *Handler {
	return NewHandler(p, a, time.Second, 65536, vueOrigin, slog.New(slog.NewJSONHandler(io.Discard, nil)))
}

func request(h http.Handler, method, route, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, route, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func wantError(t *testing.T, w *httptest.ResponseRecorder, code int, name string) errorBody {
	t.Helper()
	if w.Code != code {
		t.Fatalf("HTTP=%d want=%d body=%s", w.Code, code, w.Body.String())
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatal("error is not JSON")
	}
	var result struct {
		Error errorBody `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Error.Code != name || result.Error.Message == "" {
		t.Fatalf("error=%+v", result.Error)
	}
	return result.Error
}

func checkDeadline(t *testing.T, ctx context.Context) {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > time.Second {
		t.Fatal("RPC deadline not bounded")
	}
}

func TestRoutesAndJSONRepresentations(t *testing.T) {
	purchase := &pb.Purchase{Id: "purchase-demo", ClientRequestId: "demo-key", EmployeeId: "emp-101", ItemName: "Laptop", Quantity: 2, UnitPriceCents: 80000, TotalCents: 160000, Status: pb.Status_STATUS_PENDING}
	createCalls, getCalls, listCalls, decisionCalls := 0, 0, 0, 0
	p := fakePurchases{
		create: func(ctx context.Context, req *pb.CreatePurchaseRequest) (*pb.Purchase, error) {
			checkDeadline(t, ctx)
			createCalls++
			if req.ClientRequestId != "demo-key" || req.EmployeeId != "emp-101" || req.ItemName != "Laptop" || req.Quantity != 2 || req.UnitPriceCents != 80000 {
				t.Fatalf("RPC input=%v", req)
			}
			return purchase, nil
		},
		get: func(ctx context.Context, req *pb.GetPurchaseRequest) (*pb.Purchase, error) {
			checkDeadline(t, ctx)
			getCalls++
			if req.PurchaseId != purchase.Id {
				t.Fatal("wrong ID")
			}
			return purchase, nil
		},
		list: func(ctx context.Context, _ *pb.ListPurchasesRequest) (*pb.ListPurchasesResponse, error) {
			checkDeadline(t, ctx)
			listCalls++
			return &pb.ListPurchasesResponse{Purchases: []*pb.Purchase{purchase}}, nil
		},
	}
	a := fakeApprovals(func(ctx context.Context, req *pb.DecideApprovalRequest) (*pb.Approval, error) {
		checkDeadline(t, ctx)
		decisionCalls++
		if req.PurchaseId != purchase.Id || req.Status != pb.Status_STATUS_APPROVED {
			t.Fatalf("decision RPC=%v", req)
		}
		return &pb.Approval{PurchaseId: purchase.Id, Status: req.Status}, nil
	})
	h := testHandler(p, a)
	w := request(h, "GET", "/healthz", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("health=%s", w.Body.String())
	}
	w = request(h, "POST", "/api/purchases", createBody)
	if w.Code != 201 || w.Header().Get("Location") != "/api/purchases/purchase-demo" {
		t.Fatalf("create=%d %s", w.Code, w.Body.String())
	}
	var created purchaseJSON
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.Status != "PENDING" || created.TotalCents != 160000 || created.Quantity != 2 {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	w = request(h, "GET", "/api/purchases/purchase-demo", "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = request(h, "GET", "/api/purchases", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"purchases":[{`) {
		t.Fatal(w.Body.String())
	}
	w = request(h, "POST", "/api/purchases/purchase-demo/decision", decisionBody)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"demo_only":true`) || !strings.Contains(w.Body.String(), `"metadata_persisted":false`) || !strings.Contains(w.Body.String(), `"status":"APPROVED"`) {
		t.Fatalf("decision=%d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "Within budget") {
		t.Fatal("comment should not be echoed as persisted metadata")
	}
	if createCalls != 1 || getCalls != 1 || listCalls != 1 || decisionCalls != 1 {
		t.Fatal("wrong RPC call counts")
	}
}

func TestEmptyListIsArray(t *testing.T) {
	h := testHandler(fakePurchases{list: func(context.Context, *pb.ListPurchasesRequest) (*pb.ListPurchasesResponse, error) {
		return &pb.ListPurchasesResponse{}, nil
	}}, nil)
	w := request(h, "GET", "/api/purchases", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"purchases":[]}` {
		t.Fatalf("list=%s", w.Body.String())
	}
}

func TestInvalidCreateNeverCallsGRPC(t *testing.T) {
	h := testHandler(fakePurchases{create: func(context.Context, *pb.CreatePurchaseRequest) (*pb.Purchase, error) {
		t.Fatal("invalid request reached RPC")
		return nil, nil
	}}, nil)
	for _, tc := range []struct{ name, body, code string }{
		{"empty", "", "INVALID_JSON"}, {"malformed", "{", "INVALID_JSON"}, {"null", "null", "INVALID_JSON"}, {"array", "[]", "INVALID_JSON"},
		{"unknown", strings.TrimSuffix(createBody, "}") + `,"unexpected":1}`, "INVALID_JSON"},
		{"trailing value", createBody + ` {}`, "INVALID_JSON"}, {"trailing junk", createBody + ` nope`, "INVALID_JSON"},
		{"type", strings.Replace(createBody, `"quantity":2`, `"quantity":"2"`, 1), "INVALID_JSON"},
		{"fraction", strings.Replace(createBody, `"quantity":2`, `"quantity":1.5`, 1), "INVALID_JSON"},
		{"out of range", strings.Replace(createBody, `"quantity":2`, `"quantity":9223372036854775808`, 1), "INVALID_JSON"},
		{"missing quantity", strings.Replace(createBody, `"quantity":2,`, "", 1), "INVALID_ARGUMENT"},
		{"null quantity", strings.Replace(createBody, `"quantity":2`, `"quantity":null`, 1), "INVALID_ARGUMENT"},
		{"missing price", strings.Replace(createBody, `,"unit_price_cents":80000`, "", 1), "INVALID_ARGUMENT"},
		{"null price", strings.Replace(createBody, `"unit_price_cents":80000`, `"unit_price_cents":null`, 1), "INVALID_ARGUMENT"},
		{"zero quantity", strings.Replace(createBody, `"quantity":2`, `"quantity":0`, 1), "INVALID_ARGUMENT"},
		{"negative quantity", strings.Replace(createBody, `"quantity":2`, `"quantity":-1`, 1), "INVALID_ARGUMENT"},
		{"negative price", strings.Replace(createBody, `"unit_price_cents":80000`, `"unit_price_cents":-1`, 1), "INVALID_ARGUMENT"},
		{"overflow", strings.Replace(createBody, `"unit_price_cents":80000`, `"unit_price_cents":9223372036854775807`, 1), "INVALID_ARGUMENT"},
		{"blank employee", strings.Replace(createBody, `"emp-101"`, `" "`, 1), "INVALID_ARGUMENT"},
		{"invalid key", strings.Replace(createBody, `"demo-key"`, `"bad key!"`, 1), "INVALID_ARGUMENT"},
		{"blank name", strings.Replace(createBody, `"Laptop"`, `" \t "`, 1), "INVALID_ARGUMENT"},
		{"long name", strings.Replace(createBody, `"Laptop"`, `"`+strings.Repeat("a", 201)+`"`, 1), "INVALID_ARGUMENT"},
		{"NUL name", strings.Replace(createBody, `"Laptop"`, `"\u0000"`, 1), "INVALID_ARGUMENT"},
		{"bad utf8", strings.Replace(createBody, "Laptop", string([]byte{0xff}), 1), "INVALID_JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/purchases", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			wantError(t, w, 400, tc.code)
		})
	}
}

func TestJSONBodyLimitsAndMediaTypes(t *testing.T) {
	h := testHandler(fakePurchases{}, nil)
	h.maxBody = int64(len(createBody))
	w := request(h, "POST", "/api/purchases", createBody+" ")
	wantError(t, w, 413, "BODY_TOO_LARGE")
	for _, contentType := range []string{"", "text/plain", "application/json; invalid"} {
		r := httptest.NewRequest("POST", "/api/purchases", strings.NewReader(createBody))
		r.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		wantError(t, w, 415, "UNSUPPORTED_MEDIA_TYPE")
	}
	r := httptest.NewRequest("POST", "/api/purchases", strings.NewReader(createBody))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Content-Encoding", "gzip")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	wantError(t, w, 415, "UNSUPPORTED_MEDIA_TYPE")
	// A zero price and the largest safe int64 quantity are accepted as integers.
	h = testHandler(fakePurchases{create: func(_ context.Context, in *pb.CreatePurchaseRequest) (*pb.Purchase, error) {
		if in.UnitPriceCents != 0 || in.Quantity != math.MaxInt64 {
			t.Fatal("numeric input changed")
		}
		return &pb.Purchase{Id: "free-item"}, nil
	}}, nil)
	w = request(h, "POST", "/api/purchases", `{"client_request_id":"free","employee_id":"emp","item_name":"Gift","quantity":9223372036854775807,"unit_price_cents":0}`)
	if w.Code != 201 {
		t.Fatal(w.Body.String())
	}
}

func TestInvalidDecisionsNeverCallGRPC(t *testing.T) {
	h := testHandler(fakePurchases{}, fakeApprovals(func(context.Context, *pb.DecideApprovalRequest) (*pb.Approval, error) {
		t.Fatal("invalid decision reached RPC")
		return nil, nil
	}))
	for _, tc := range []struct{ name, body, code string }{
		{"missing manager", `{"decision":"APPROVED"}`, "INVALID_ARGUMENT"},
		{"bad manager", strings.Replace(decisionBody, `"manager-demo"`, `" "`, 1), "INVALID_ARGUMENT"},
		{"pending", strings.Replace(decisionBody, "APPROVED", "PENDING", 1), "INVALID_ARGUMENT"},
		{"lowercase", strings.Replace(decisionBody, "APPROVED", "approved", 1), "INVALID_ARGUMENT"},
		{"unknown field", strings.TrimSuffix(decisionBody, "}") + `,"chatbot_approved":true}`, "INVALID_JSON"},
		{"long comment", strings.Replace(decisionBody, "Within budget", strings.Repeat("a", 1001), 1), "INVALID_ARGUMENT"},
		{"invalid comment type", strings.Replace(decisionBody, `"Within budget"`, `42`, 1), "INVALID_JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantError(t, request(h, "POST", "/api/purchases/purchase-demo/decision", tc.body), 400, tc.code)
		})
	}
	wantError(t, request(h, "GET", "/api/purchases/bad%20id", ""), 400, "INVALID_ARGUMENT")
}

func TestRoutingAndCORS(t *testing.T) {
	h := testHandler(fakePurchases{}, nil)
	for _, origin := range []string{"http://localhost:5174", "http://127.0.0.1:5173", "https://evil.example", "null", vueOrigin + ".evil.example"} {
		t.Run(origin, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/healthz", nil)
			r.Header.Set("Origin", origin)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			wantError(t, w, 403, "ORIGIN_DENIED")
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("denied origin reflected")
			}
		})
	}
	r := httptest.NewRequest("GET", "/healthz", nil)
	r.Header.Set("Origin", vueOrigin)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") != vueOrigin || w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("CORS origin not restricted")
	}
	r.Header.Add("Origin", vueOrigin)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	wantError(t, w, 403, "ORIGIN_DENIED")
	for _, tc := range []struct {
		origin, method, headers string
		status                  int
	}{
		{vueOrigin, "POST", "Content-Type", 204}, {vueOrigin, "POST", "content-type", 204},
		{vueOrigin, "DELETE", "Content-Type", 403}, {vueOrigin, "POST", "Authorization", 403}, {"", "POST", "", 403},
	} {
		r := httptest.NewRequest("OPTIONS", "/api/purchases", nil)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		r.Header.Set("Access-Control-Request-Method", tc.method)
		r.Header.Set("Access-Control-Request-Headers", tc.headers)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("preflight=%d %s", w.Code, w.Body.String())
		}
		if w.Code == 204 && (w.Body.Len() != 0 || w.Header().Get("Access-Control-Allow-Headers") != "Content-Type") {
			t.Fatal("invalid preflight response")
		}
	}
	w = request(h, "DELETE", "/api/purchases", "")
	wantError(t, w, 405, "METHOD_NOT_ALLOWED")
	if w.Header().Get("Allow") != "GET, POST" {
		t.Fatal("missing allowed methods")
	}
	for _, route := range []string{"/unknown", "/api/purchases/one/chatbot", "/api/purchases/", "/api//purchases"} {
		wantError(t, request(h, "GET", route, ""), 404, "NOT_FOUND")
	}
}

func TestGRPCErrorMappingAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		code  codes.Code
		http  int
		name  string
		retry bool
	}{
		{codes.InvalidArgument, 400, "INVALID_ARGUMENT", false}, {codes.OutOfRange, 400, "OUT_OF_RANGE", false},
		{codes.NotFound, 404, "NOT_FOUND", false}, {codes.AlreadyExists, 409, "ALREADY_EXISTS", false},
		{codes.FailedPrecondition, 409, "FAILED_PRECONDITION", false}, {codes.Aborted, 409, "ABORTED", false},
		{codes.Unauthenticated, 401, "UNAUTHENTICATED", false}, {codes.PermissionDenied, 403, "PERMISSION_DENIED", false},
		{codes.ResourceExhausted, 429, "RESOURCE_EXHAUSTED", true}, {codes.Unavailable, 503, "UNAVAILABLE", true},
		{codes.DeadlineExceeded, 504, "DEADLINE_EXCEEDED", true}, {codes.Canceled, 408, "CANCELED", true},
		{codes.Internal, 502, "INTERNAL", false}, {codes.Unknown, 502, "BAD_GATEWAY", false}, {codes.DataLoss, 502, "DATA_LOSS", false},
	} {
		t.Run(tc.code.String(), func(t *testing.T) {
			failure := status.Error(tc.code, "postgres://secret-password/internal-database-table")
			h := testHandler(fakePurchases{get: func(context.Context, *pb.GetPurchaseRequest) (*pb.Purchase, error) { return nil, failure }}, nil)
			w := request(h, "GET", "/api/purchases/purchase-demo", "")
			e := wantError(t, w, tc.http, tc.name)
			if e.Retryable != tc.retry || strings.Contains(w.Body.String(), "secret-password") {
				t.Fatal("unsafe or incorrect error response")
			}
			if tc.retry && w.Header().Get("Retry-After") != "1" {
				t.Fatal("missing retry guidance")
			}
		})
	}
	// Both mutations use the same controlled failure mapping.
	failure := status.Error(codes.Unavailable, "internal detail")
	h := testHandler(fakePurchases{create: func(context.Context, *pb.CreatePurchaseRequest) (*pb.Purchase, error) { return nil, failure }}, fakeApprovals(func(context.Context, *pb.DecideApprovalRequest) (*pb.Approval, error) { return nil, failure }))
	wantError(t, request(h, "POST", "/api/purchases", createBody), 503, "UNAVAILABLE")
	wantError(t, request(h, "POST", "/api/purchases/purchase-demo/decision", decisionBody), 503, "UNAVAILABLE")
}

func TestDeadlineCancellationAndMalformedPeer(t *testing.T) {
	h := testHandler(fakePurchases{get: func(ctx context.Context, _ *pb.GetPurchaseRequest) (*pb.Purchase, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}, nil)
	h.timeout = 10 * time.Millisecond
	wantError(t, request(h, "GET", "/api/purchases/purchase-demo", ""), 504, "DEADLINE_EXCEEDED")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/api/purchases/purchase-demo", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	wantError(t, w, 408, "CANCELED")
	h = testHandler(fakePurchases{get: func(context.Context, *pb.GetPurchaseRequest) (*pb.Purchase, error) { return nil, nil }}, nil)
	wantError(t, request(h, "GET", "/api/purchases/purchase-demo", ""), 502, "BAD_GATEWAY")
	h = testHandler(fakePurchases{get: func(context.Context, *pb.GetPurchaseRequest) (*pb.Purchase, error) { panic("private detail") }}, nil)
	wantError(t, request(h, "GET", "/api/purchases/purchase-demo", ""), 500, "INTERNAL")
	h = testHandler(fakePurchases{get: func(context.Context, *pb.GetPurchaseRequest) (*pb.Purchase, error) {
		return nil, errors.New("arbitrary internal failure")
	}}, nil)
	wantError(t, request(h, "GET", "/api/purchases/purchase-demo", ""), 502, "BAD_GATEWAY")
}

func TestDecisionMissingOrConflictingApproval(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      codes.Code
		http      int
		errorName string
	}{
		{"missing approval", codes.NotFound, http.StatusNotFound, "NOT_FOUND"},
		{"conflicting decision", codes.FailedPrecondition, http.StatusConflict, "FAILED_PRECONDITION"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h := testHandler(fakePurchases{}, fakeApprovals(func(_ context.Context, req *pb.DecideApprovalRequest) (*pb.Approval, error) {
				calls++
				if req.PurchaseId != "purchase-demo" || req.Status != pb.Status_STATUS_APPROVED {
					t.Fatalf("unexpected decision input=%v", req)
				}
				return nil, status.Error(tc.code, "private approval detail")
			}))
			w := request(h, "POST", "/api/purchases/purchase-demo/decision", decisionBody)
			e := wantError(t, w, tc.http, tc.errorName)
			if calls != 1 || e.Retryable || strings.Contains(w.Body.String(), "private approval detail") {
				t.Fatalf("unexpected calls=%d or unsafe error=%s", calls, w.Body.String())
			}
		})
	}
}

func TestMutationTimeoutsAreRetryable(t *testing.T) {
	for _, route := range []string{"/api/purchases", "/api/purchases/purchase-demo/decision"} {
		t.Run(route, func(t *testing.T) {
			calls := 0
			waitForDeadline := func(ctx context.Context) error {
				calls++
				checkDeadline(t, ctx)
				<-ctx.Done()
				return ctx.Err()
			}
			h := testHandler(fakePurchases{create: func(ctx context.Context, _ *pb.CreatePurchaseRequest) (*pb.Purchase, error) {
				return nil, waitForDeadline(ctx)
			}}, fakeApprovals(func(ctx context.Context, _ *pb.DecideApprovalRequest) (*pb.Approval, error) {
				return nil, waitForDeadline(ctx)
			}))
			h.timeout = 20 * time.Millisecond
			body := createBody
			if strings.HasSuffix(route, "/decision") {
				body = decisionBody
			}
			w := request(h, "POST", route, body)
			e := wantError(t, w, http.StatusGatewayTimeout, "DEADLINE_EXCEEDED")
			if calls != 1 || !e.Retryable || w.Header().Get("Retry-After") != "1" {
				t.Fatalf("timeout lost retry guidance: calls=%d error=%+v", calls, e)
			}
		})
	}
}
