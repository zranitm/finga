// Package handler is the account service's transport layer: the
// AccountService gRPC server (api/proto/internal/account/v1). Each method
// decodes the request, calls exactly one service method, and encodes the
// result; errors leave through Errors. WatchSessions streams the session
// ends the module reports through its hooks to every subscribed gateway.
package handler

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/kernel"
	"finga/pkg/auth"
	"finga/pkg/grpcx"
	"finga/pkg/hub"
	"finga/services/account/internal/account"
)

// Service is what the handler needs from the account module.
type Service interface {
	Register(ctx context.Context, in account.RegisterInput) (account.Account, error)
	Login(ctx context.Context, in account.LoginInput) (string, error)
	Validate(ctx context.Context, token string) (auth.Principal, error)
	Rotate(ctx context.Context, sessionID uuid.UUID) (string, error)
	Check(ctx context.Context, sessionID uuid.UUID) error
	Revoke(ctx context.Context, sessionID uuid.UUID) error
	RevokeAll(ctx context.Context, accountID uuid.UUID) error
	Settings(ctx context.Context, id kernel.AccountID) (account.Settings, error)
	UpdateSettings(ctx context.Context, id kernel.AccountID, s account.Settings) error
}

// watchQueue is how many session ends may wait for a slow gateway before
// its stream is ended; its minute check catches what it then misses.
const watchQueue = 1024

// Server serves AccountService over the module.
type Server struct {
	accountv1.UnimplementedAccountServiceServer
	svc      Service
	bootID   string
	watchers *hub.Hub[*accountv1.WatchSessionsResponse]
}

// New builds the server. bootID is random per process start, so a gateway
// can tell a restart from a broken stream.
func New(svc Service, bootID string) *Server {
	return &Server{svc: svc, bootID: bootID, watchers: hub.New[*accountv1.WatchSessionsResponse](watchQueue)}
}

// Register creates an account.
func (s *Server) Register(ctx context.Context, req *accountv1.RegisterRequest) (*accountv1.RegisterResponse, error) {
	a, err := s.svc.Register(ctx, account.RegisterInput{Username: req.GetUsername(), Password: req.GetPassword()})
	if err != nil {
		return nil, err
	}
	return &accountv1.RegisterResponse{Account: &accountv1.Account{
		Id: a.ID.String(), Username: a.Username, Settings: settingsOf(a.Settings), CreatedAt: timestamppb.New(a.CreatedAt),
	}}, nil
}

// Login issues a session.
func (s *Server) Login(ctx context.Context, req *accountv1.LoginRequest) (*accountv1.LoginResponse, error) {
	token, err := s.svc.Login(ctx, account.LoginInput{Username: req.GetUsername(), Password: req.GetPassword(), IP: req.GetIp()})
	if err != nil {
		return nil, err
	}
	return &accountv1.LoginResponse{Token: token}, nil
}

// Validate resolves a token to its session.
func (s *Server) Validate(ctx context.Context, req *accountv1.ValidateRequest) (*accountv1.ValidateResponse, error) {
	p, err := s.svc.Validate(ctx, req.GetToken())
	if err != nil {
		return nil, err
	}
	return &accountv1.ValidateResponse{AccountId: p.AccountID.String(), SessionId: p.SessionID.String()}, nil
}

// Rotate issues a session's next token.
func (s *Server) Rotate(ctx context.Context, req *accountv1.RotateRequest) (*accountv1.RotateResponse, error) {
	id, err := parse(req.GetSessionId())
	if err != nil {
		return nil, err
	}
	token, err := s.svc.Rotate(ctx, id)
	if err != nil {
		return nil, err
	}
	return &accountv1.RotateResponse{Token: token}, nil
}

// Check reports whether a session is still active.
func (s *Server) Check(ctx context.Context, req *accountv1.CheckRequest) (*accountv1.CheckResponse, error) {
	id, err := parse(req.GetSessionId())
	if err != nil {
		return nil, err
	}
	return &accountv1.CheckResponse{}, s.svc.Check(ctx, id)
}

// Revoke ends one session.
func (s *Server) Revoke(ctx context.Context, req *accountv1.RevokeRequest) (*accountv1.RevokeResponse, error) {
	id, err := parse(req.GetSessionId())
	if err != nil {
		return nil, err
	}
	return &accountv1.RevokeResponse{}, s.svc.Revoke(ctx, id)
}

// RevokeAll ends every session of an account.
func (s *Server) RevokeAll(ctx context.Context, req *accountv1.RevokeAllRequest) (*accountv1.RevokeAllResponse, error) {
	id, err := parse(req.GetAccountId())
	if err != nil {
		return nil, err
	}
	return &accountv1.RevokeAllResponse{}, s.svc.RevokeAll(ctx, id)
}

// GetSettings reads an account's settings.
func (s *Server) GetSettings(ctx context.Context, req *accountv1.GetSettingsRequest) (*accountv1.GetSettingsResponse, error) {
	id, err := parse(req.GetAccountId())
	if err != nil {
		return nil, err
	}
	st, err := s.svc.Settings(ctx, kernel.AccountID(id))
	if err != nil {
		return nil, err
	}
	return &accountv1.GetSettingsResponse{Settings: settingsOf(st)}, nil
}

// UpdateSettings saves an account's settings.
func (s *Server) UpdateSettings(ctx context.Context, req *accountv1.UpdateSettingsRequest) (*accountv1.UpdateSettingsResponse, error) {
	id, err := parse(req.GetAccountId())
	if err != nil {
		return nil, err
	}
	st := account.Settings{ControlPreset: account.Preset(req.GetSettings().GetControlPreset())}
	return &accountv1.UpdateSettingsResponse{}, s.svc.UpdateSettings(ctx, kernel.AccountID(id), st)
}

// WatchSessions streams session ends to one gateway.
func (s *Server) WatchSessions(req *accountv1.WatchSessionsRequest, stream grpc.ServerStreamingServer[accountv1.WatchSessionsResponse]) error {
	if req.GetGatewayId() == "" {
		return status.Error(codes.InvalidArgument, "gateway_id is required")
	}
	first := &accountv1.WatchSessionsResponse{Event: &accountv1.WatchSessionsResponse_Subscribed{Subscribed: &accountv1.Subscribed{BootId: s.bootID}}}
	return grpcx.ServeSubscription(stream, s.watchers.Subscribe(req.GetGatewayId()), first)
}

// SessionRevoked is the module's OnSessionRevoked hook: every gateway hears
// that the session ended.
func (s *Server) SessionRevoked(_ context.Context, id uuid.UUID, reason string) {
	s.watchers.Broadcast(&accountv1.WatchSessionsResponse{Event: &accountv1.WatchSessionsResponse_SessionRevoked{
		SessionRevoked: &accountv1.SessionRevoked{SessionId: id.String(), Reason: revokeReason(reason)},
	}})
}

// AccountRevoked is the module's OnAccountRevoked hook: every gateway hears
// that the account's sessions ended.
func (s *Server) AccountRevoked(_ context.Context, id uuid.UUID, reason string) {
	s.watchers.Broadcast(&accountv1.WatchSessionsResponse{Event: &accountv1.WatchSessionsResponse_AccountRevoked{
		AccountRevoked: &accountv1.AccountRevoked{AccountId: id.String(), Reason: revokeReason(reason)},
	}})
}

func revokeReason(reason string) accountv1.RevokeReason {
	switch reason {
	case account.ReasonTokenReuse:
		return accountv1.RevokeReason_REVOKE_REASON_TOKEN_REUSE
	case account.ReasonLogout:
		return accountv1.RevokeReason_REVOKE_REASON_LOGOUT
	case account.ReasonNewLogin:
		return accountv1.RevokeReason_REVOKE_REASON_NEW_LOGIN
	}
	return accountv1.RevokeReason_REVOKE_REASON_UNSPECIFIED
}

func settingsOf(s account.Settings) *accountv1.Settings {
	return &accountv1.Settings{ControlPreset: string(s.ControlPreset)}
}

// parse reads an ID the gateway sent; a malformed one is a caller's bug.
func parse(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, status.Error(codes.InvalidArgument, "malformed ID")
	}
	return id, nil
}
