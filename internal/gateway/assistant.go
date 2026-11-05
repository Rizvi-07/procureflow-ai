package gateway

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "procureflow-ai/gen/assistant/v1"
)

// No purchase clients or database handles belong to this policy-only interface.
type AssistantClient interface {
	AnswerPolicy(context.Context, *pb.AnswerPolicyRequest, ...grpc.CallOption) (*pb.AnswerPolicyResponse, error)
}
type policySourceJSON struct {
	Document   string `json:"document"`
	Section    string `json:"section"`
	ChunkIndex int32  `json:"chunk_index"`
	Quote      string `json:"quote"`
}

func (h *Handler) askPolicy(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeJSON[struct {
		Question string `json:"question"`
	}](w, r, h.maxBody)
	if !ok {
		return
	}
	if strings.TrimSpace(input.Question) == "" || len(input.Question) > 1000 || strings.ContainsRune(input.Question, 0) {
		writeError(w, 400, "INVALID_ARGUMENT", "question must contain 1-1000 UTF-8 bytes without NUL", false)
		return
	}
	if h.assistant == nil {
		writeJSON(w, 200, map[string]any{"configured": false, "answer": "Not configured. The optional policy assistant is disabled.", "sources": []policySourceJSON{}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.assistantTimeout)
	defer cancel()
	answer, err := h.assistant.AnswerPolicy(ctx, &pb.AnswerPolicyRequest{Question: input.Question})
	if err != nil {
		policyUpstreamError(w, err)
		return
	}
	if answer == nil || answer.Answer == "" || len(answer.Answer) > 4096 || !utf8.ValidString(answer.Answer) || len(answer.Sources) > 5 || (!answer.Configured && len(answer.Sources) > 0) {
		writeError(w, 502, "BAD_GATEWAY", "invalid policy assistant response", false)
		return
	}
	sources := make([]policySourceJSON, 0, len(answer.Sources))
	for _, source := range answer.Sources {
		if source == nil || source.Document == "" || source.Section == "" || source.Quote == "" || source.ChunkIndex < 0 {
			writeError(w, 502, "BAD_GATEWAY", "invalid policy citation response", false)
			return
		}
		sources = append(sources, policySourceJSON{source.Document, source.Section, source.ChunkIndex, source.Quote})
	}
	writeJSON(w, 200, map[string]any{"configured": answer.Configured, "answer": answer.Answer, "sources": sources})
}

// Policy requests never write purchases, so their errors must not tell users
// that an order write may have completed or ask for a purchase idempotency key.
func policyUpstreamError(w http.ResponseWriter, err error) {
	code := status.Code(err)
	if errors.Is(err, context.DeadlineExceeded) {
		code = codes.DeadlineExceeded
	}
	if errors.Is(err, context.Canceled) {
		code = codes.Canceled
	}
	httpCode, message, retryable := http.StatusBadGateway, "policy assistant returned an invalid response", false
	switch code {
	case codes.InvalidArgument:
		httpCode, message = http.StatusBadRequest, "policy question was rejected"
	case codes.Unavailable:
		httpCode, message, retryable = http.StatusServiceUnavailable, "policy assistant unavailable; procurement remains available", true
	case codes.DeadlineExceeded:
		httpCode, message, retryable = http.StatusGatewayTimeout, "policy assistant timed out; no policy answer was returned", true
	case codes.Canceled:
		httpCode, message, retryable = http.StatusRequestTimeout, "policy question was canceled", true
	case codes.ResourceExhausted:
		httpCode, message, retryable = http.StatusTooManyRequests, "policy assistant is busy; retry later", true
	}
	if retryable {
		w.Header().Set("Retry-After", "1")
	}
	name, ok := grpcErrorNames[code]
	if !ok {
		name = "BAD_GATEWAY"
	}
	writeError(w, httpCode, name, message, retryable)
}
