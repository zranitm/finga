// Package handler is the account service's AccountService gRPC server.
package handler

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/account"
)

// Service is the account module's facade, as the handler calls it.
type Service interface {
	Register(ctx context.Context, c account.Credentials) (account.Account, error)
	Login(ctx context.Context, c account.Credentials) (string, error)
	Validate(ctx context.Context, token string) (account.Principal, error)
	Rotate(ctx context.Context, id kernel.SessionID) (string, error)
	Check(ctx context.Context, id kernel.SessionID) error
	Revoke(ctx context.Context, id kernel.SessionID) error
	RevokeAll(ctx context.Context, id kernel.AccountID) error
	Settings(ctx context.Context, id kernel.AccountID) (account.Settings, error)
	UpdateSettings(ctx context.Context, id kernel.AccountID, s account.Settings) error
}

// Server serves AccountService over the module's facade.
type Server struct {
	accountv1.UnimplementedAccountServiceServer
	svc     Service
	streams *Streams
}

// New builds the server over the facade and the streams the module notifies.
func New(svc Service, streams *Streams) *Server {
	return &Server{svc: svc, streams: streams}
}

// Register creates an account.
func (s *Server) Register(ctx context.Context, req *accountv1.RegisterRequest) (*accountv1.RegisterResponse, error) {
	a, err := s.svc.Register(ctx, account.Credentials{Username: req.GetUsername(), Password: req.GetPassword()})
	if err != nil {
		return nil, err
	}
	return &accountv1.RegisterResponse{Account: &accountv1.Account{
		Id: a.ID.String(), Username: a.Username, Settings: settingsOf(a.Settings), CreatedAt: timestamppb.New(a.CreatedAt),
	}}, nil
}

// Login issues a session.
func (s *Server) Login(ctx context.Context, req *accountv1.LoginRequest) (*accountv1.LoginResponse, error) {
	token, err := s.svc.Login(ctx, account.Credentials{Username: req.GetUsername(), Password: req.GetPassword()})
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
	id, err := kernel.ParseSessionID(req.GetSessionId())
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
	id, err := kernel.ParseSessionID(req.GetSessionId())
	if err != nil {
		return nil, err
	}
	if err := s.svc.Check(ctx, id); err != nil {
		return nil, err
	}
	return &accountv1.CheckResponse{}, nil
}

// Revoke ends one session.
func (s *Server) Revoke(ctx context.Context, req *accountv1.RevokeRequest) (*accountv1.RevokeResponse, error) {
	id, err := kernel.ParseSessionID(req.GetSessionId())
	if err != nil {
		return nil, err
	}
	if err := s.svc.Revoke(ctx, id); err != nil {
		return nil, err
	}
	return &accountv1.RevokeResponse{}, nil
}

// RevokeAll ends every session of an account.
func (s *Server) RevokeAll(ctx context.Context, req *accountv1.RevokeAllRequest) (*accountv1.RevokeAllResponse, error) {
	id, err := kernel.ParseAccountID(req.GetAccountId())
	if err != nil {
		return nil, err
	}
	if err := s.svc.RevokeAll(ctx, id); err != nil {
		return nil, err
	}
	return &accountv1.RevokeAllResponse{}, nil
}

// GetSettings reads an account's settings.
func (s *Server) GetSettings(ctx context.Context, req *accountv1.GetSettingsRequest) (*accountv1.GetSettingsResponse, error) {
	id, err := kernel.ParseAccountID(req.GetAccountId())
	if err != nil {
		return nil, err
	}
	st, err := s.svc.Settings(ctx, id)
	if err != nil {
		return nil, err
	}
	return &accountv1.GetSettingsResponse{Settings: settingsOf(st)}, nil
}

// UpdateSettings saves an account's settings.
func (s *Server) UpdateSettings(ctx context.Context, req *accountv1.UpdateSettingsRequest) (*accountv1.UpdateSettingsResponse, error) {
	id, err := kernel.ParseAccountID(req.GetAccountId())
	if err != nil {
		return nil, err
	}
	in := req.GetSettings()
	st := account.Settings{ControlPreset: in.GetControlPreset(), AutoAssignPoints: in.GetAutoAssignPoints(), ChatPinned: in.GetChatPinned()}
	if err := s.svc.UpdateSettings(ctx, id, st); err != nil {
		return nil, err
	}
	return &accountv1.UpdateSettingsResponse{}, nil
}

// WatchSessions streams session ends to one gateway.
func (s *Server) WatchSessions(req *accountv1.WatchSessionsRequest, stream grpc.ServerStreamingServer[accountv1.WatchSessionsResponse]) error {
	if req.GetGatewayId() == "" {
		return status.Error(codes.InvalidArgument, "gateway_id is required")
	}
	first := &accountv1.WatchSessionsResponse{Event: &accountv1.WatchSessionsResponse_Subscribed{Subscribed: &accountv1.Subscribed{BootId: s.streams.bootID}}}
	return grpcx.ServeSubscription(stream, s.streams.watchers.Subscribe(req.GetGatewayId()), first)
}

func settingsOf(s account.Settings) *accountv1.Settings {
	return &accountv1.Settings{ControlPreset: s.ControlPreset, AutoAssignPoints: s.AutoAssignPoints, ChatPinned: s.ChatPinned}
}
