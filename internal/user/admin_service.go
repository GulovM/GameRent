package user

import (
	"context"
	"errors"

	shared_authorization "rent_game_accs/internal/shared/authorization"
	"rent_game_accs/internal/shared/database"
)

var (
	ErrAdminAuthorization       = errors.New("current administrator authorization is required")
	ErrAdminSelfUpdateForbidden = errors.New("administrators cannot change their own administrative user state")
)

type AdminUpdateInput struct {
	TrustScore *int
	IsBlocked  *bool
	Role       *string
}

type AdminService struct {
	repo AdminRepository
	tx   database.TxManager
}

func NewAdminService(repo AdminRepository, tx database.TxManager) *AdminService {
	return &AdminService{repo: repo, tx: tx}
}

func (s *AdminService) ListUsers(ctx context.Context) ([]*User, error) {
	return s.repo.ListUsers(ctx, 100000, 0)
}
func (s *AdminService) ListAuditLogs(ctx context.Context) ([]AuditLog, error) {
	return s.repo.ListAuditLogs(ctx, 100)
}

func (s *AdminService) UpdateUser(ctx context.Context, actorUserID, targetUserID int64, input AdminUpdateInput) error {
	return s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := shared_authorization.RequireCurrentAdminForMutation(txCtx, database.GetTxOrPool(txCtx, nil), actorUserID); err != nil {
			if errors.Is(err, shared_authorization.ErrCurrentAdminRequired) {
				return ErrAdminAuthorization
			}
			return err
		}
		if actorUserID == targetUserID {
			return ErrAdminSelfUpdateForbidden
		}
		current, err := s.repo.GetUserForUpdate(txCtx, targetUserID)
		if err != nil {
			return err
		}
		if err := s.repo.UpdateAdminState(txCtx, targetUserID, input.TrustScore, input.IsBlocked, input.Role); err != nil {
			return err
		}
		newRole, newBlocked := string(current.Role), current.IsBlocked
		if input.Role != nil {
			newRole = *input.Role
		}
		if input.IsBlocked != nil {
			newBlocked = *input.IsBlocked
		}
		if newRole != string(current.Role) || newBlocked != current.IsBlocked {
			if err := s.repo.RevokeActiveRefreshTokens(txCtx, targetUserID); err != nil {
				return err
			}
			return s.repo.RecordAdminUserStateChange(txCtx, actorUserID, targetUserID, string(current.Role), current.IsBlocked, newRole, newBlocked)
		}
		return nil
	})
}
