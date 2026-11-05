package assistant

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	pb "procureflow-ai/gen/assistant/v1"
)

var ErrProvider = errors.New("policy provider unavailable")

type Citation struct {
	SourceID int    `json:"source_id"`
	Quote    string `json:"quote"`
}
type Draft struct {
	Supported bool       `json:"supported"`
	Answer    string     `json:"answer"`
	Citations []Citation `json:"citations"`
}
type Provider interface {
	Embed(context.Context, string) ([]float32, error)
	Generate(context.Context, string, []Chunk) (Draft, error)
}

// Read-only interface: answering cannot import, mutate purchases, or decide approvals.
type Retriever interface {
	Search(context.Context, []float32, int, float64) ([]Chunk, error)
}
type Service struct {
	pb.UnimplementedAssistantServiceServer
	Config   Config
	Provider Provider
	Store    Retriever
	slots    chan struct{}
}

func NewService(cfg Config, p Provider, r Retriever) *Service {
	return &Service{Config: cfg, Provider: p, Store: r, slots: make(chan struct{}, 4)}
}

func ValidateQuestion(q string) error {
	if strings.TrimSpace(q) == "" || !utf8.ValidString(q) || len(q) > MaxQuestionBytes || strings.ContainsRune(q, 0) {
		return fmt.Errorf("question must contain 1-%d UTF-8 bytes without NUL", MaxQuestionBytes)
	}
	return nil
}

var actionQuestion = regexp.MustCompile(`(?i)\b(approve|reject|create|submit|cancel|place|update|delete)\b.*\b(purchase|order|request)s?\b|\b(status|track|lookup|look up)\b.*\b(purchase|order)s?\b|\b(purchase|order)s?\b.*\b(status|track)\b|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
var policyQuestion = regexp.MustCompile(`(?i)\b(policy|policies|supplier|suppliers|reimbursement|reimbursements|receipt|receipts|meal|expense|expenses|threshold|approval|manager|procurement)\b`)

func ValidateVector(vector []float32, dimensions int) error {
	if len(vector) != dimensions {
		return fmt.Errorf("embedding dimensions do not match the configured model/index")
	}
	var norm float64
	for _, v := range vector {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return fmt.Errorf("embedding contains a nonfinite value")
		}
		norm += float64(v) * float64(v)
	}
	if norm == 0 {
		return fmt.Errorf("zero embedding cannot be searched with cosine distance")
	}
	return nil
}

func (s *Service) AnswerPolicy(ctx context.Context, in *pb.AnswerPolicyRequest) (*pb.AnswerPolicyResponse, error) {
	if err := ValidateQuestion(in.GetQuestion()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if !s.Config.Configured() {
		return &pb.AnswerPolicyResponse{Answer: NotConfigured}, nil
	}
	question := strings.TrimSpace(in.Question)
	if actionQuestion.MatchString(question) || !policyQuestion.MatchString(question) {
		return &pb.AnswerPolicyResponse{Configured: true, Answer: PolicyOnly}, nil
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return nil, status.Error(codes.ResourceExhausted, "policy assistant is busy; retry later")
	}
	ctx, cancel := context.WithTimeout(ctx, s.Config.RequestTimeout)
	defer cancel()
	vector, err := s.Provider.Embed(ctx, question)
	if err != nil {
		return nil, assistantError(err)
	}
	if err = ValidateVector(vector, s.Config.Dimensions); err != nil {
		return nil, status.Error(codes.Unavailable, "provider embedding is incompatible with the policy index")
	}
	chunks, err := s.Store.Search(ctx, vector, s.Config.TopK, s.Config.MaxDistance)
	if err != nil {
		return nil, assistantError(err)
	}
	if len(chunks) == 0 {
		return &pb.AnswerPolicyResponse{Configured: true, Answer: NoEvidence}, nil
	}
	if len(chunks) > s.Config.TopK {
		return nil, status.Error(codes.Unavailable, "policy retrieval exceeded its configured limit")
	}
	for _, c := range chunks {
		if len(c.Content) > MaxChunkBytes || c.Content == "" || c.Document == "" || c.Section == "" {
			return nil, status.Error(codes.Unavailable, "invalid indexed policy chunk")
		}
	}
	draft, err := s.Provider.Generate(ctx, question, chunks)
	if err != nil {
		return nil, assistantError(err)
	}
	if !draft.Supported {
		return &pb.AnswerPolicyResponse{Configured: true, Answer: NoEvidence}, nil
	}
	if strings.TrimSpace(draft.Answer) == "" || len(draft.Answer) > 4096 || len(draft.Citations) == 0 || len(draft.Citations) > 5 {
		return nil, status.Error(codes.Unavailable, "provider did not return a supported policy answer")
	}
	sources := make([]*pb.PolicySource, 0, len(draft.Citations))
	seen := map[int]bool{}
	for _, cite := range draft.Citations {
		if cite.SourceID < 1 || cite.SourceID > len(chunks) || strings.TrimSpace(cite.Quote) == "" || len(cite.Quote) > MaxChunkBytes {
			return nil, status.Error(codes.Unavailable, "provider returned an invalid policy citation")
		}
		c := chunks[cite.SourceID-1]
		if !strings.Contains(c.Content, cite.Quote) {
			return nil, status.Error(codes.Unavailable, "provider quote does not match retrieved policy evidence")
		}
		if !seen[cite.SourceID] {
			sources = append(sources, &pb.PolicySource{Document: c.Document, Section: c.Section, ChunkIndex: int32(c.Index), Quote: cite.Quote})
			seen[cite.SourceID] = true
		}
	}
	return &pb.AnswerPolicyResponse{Configured: true, Answer: draft.Answer, Sources: sources}, nil
}

func assistantError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return status.Error(codes.DeadlineExceeded, "policy assistant deadline exceeded")
	}
	if errors.Is(err, context.Canceled) {
		return status.Error(codes.Canceled, "policy request canceled")
	}
	return status.Error(codes.Unavailable, "policy retrieval or generation unavailable; no answer was produced")
}
