package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "procureflow-ai/gen/assistant/v1"
)

type fakeAssistant func(context.Context, *pb.AnswerPolicyRequest) (*pb.AnswerPolicyResponse, error)

func (f fakeAssistant) AnswerPolicy(c context.Context, r *pb.AnswerPolicyRequest, _ ...grpc.CallOption) (*pb.AnswerPolicyResponse, error) {
	return f(c, r)
}

func TestPolicyAssistantDisabledAndValidated(t *testing.T) {
	h := testHandler(fakePurchases{}, nil)
	w := request(h, "POST", "/api/assistant/ask", `{"question":"What is the policy?"}`)
	var answer struct {
		Configured bool               `json:"configured"`
		Answer     string             `json:"answer"`
		Sources    []policySourceJSON `json:"sources"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &answer); err != nil || w.Code != 200 || answer.Configured || answer.Sources == nil || !strings.Contains(answer.Answer, "Not configured") {
		t.Fatalf("disabled response=%s error=%v", w.Body, err)
	}
	for _, body := range []string{`{}`, `{"question":null}`, `{"question":" "}`, `{"question":1}`, `{"question":"policy","purchase_id":"x"}`, `{"question":"` + strings.Repeat("x", 1001) + `"}`} {
		w = request(h, "POST", "/api/assistant/ask", body)
		if w.Code != 400 {
			t.Fatalf("invalid question accepted: %d", w.Code)
		}
	}
}
func TestPolicyAssistantOnlyCallsAnswerRPC(t *testing.T) {
	h := testHandler(fakePurchases{}, nil) // Purchase/approval calls would panic.
	h.assistantTimeout = time.Second
	h.assistant = fakeAssistant(func(ctx context.Context, in *pb.AnswerPolicyRequest) (*pb.AnswerPolicyResponse, error) {
		checkDeadline(t, ctx)
		if in.Question != "What is the policy?" {
			t.Fatal("question changed")
		}
		return &pb.AnswerPolicyResponse{Configured: true, Answer: "Fictional policy answer.", Sources: []*pb.PolicySource{{Document: "fictional.md", Section: "Rule@L1", Quote: "Fictional policy."}}}, nil
	})
	w := request(h, "POST", "/api/assistant/ask", `{"question":"What is the policy?"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"chunk_index":0`) || !strings.Contains(w.Body.String(), "fictional.md") {
		t.Fatalf("policy response=%s", w.Body)
	}
	h.assistant = fakeAssistant(func(context.Context, *pb.AnswerPolicyRequest) (*pb.AnswerPolicyResponse, error) {
		return nil, status.Error(codes.Unavailable, "private provider detail")
	})
	e := wantError(t, request(h, "POST", "/api/assistant/ask", `{"question":"policy"}`), http.StatusServiceUnavailable, "UNAVAILABLE")
	if !e.Retryable || strings.Contains(e.Message, "private provider") || !strings.Contains(e.Message, "procurement remains available") {
		t.Fatal("assistant failure was not controlled")
	}
	h.assistant = fakeAssistant(func(context.Context, *pb.AnswerPolicyRequest) (*pb.AnswerPolicyResponse, error) {
		return nil, context.DeadlineExceeded
	})
	e = wantError(t, request(h, "POST", "/api/assistant/ask", `{"question":"policy"}`), http.StatusGatewayTimeout, "DEADLINE_EXCEEDED")
	if !e.Retryable || strings.Contains(e.Message, "write") || strings.Contains(e.Message, "create") {
		t.Fatal("read-only timeout was described as a purchase write")
	}
}
