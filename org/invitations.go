package org

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/cocina/server-mvp/auth"
	"github.com/cocina/server-mvp/ids"
	"github.com/cocina/server-mvp/types"
)

type CreateInvitationInput struct {
	Role          string
	ExpiresInDays int
	MaxUses       int
}

func (s *Service) CreateInvitation(orgID, userID, publicBaseURL string, in CreateInvitationInput) (*types.CreateInvitationResponse, error) {
	if !s.UserIsOrgAdmin(userID) {
		return nil, fmt.Errorf("forbidden")
	}

	role := in.Role
	if role == "" {
		role = types.RoleMember
	}
	switch role {
	case types.RoleMember, types.RoleAdmin:
	default:
		return nil, fmt.Errorf("invalid role")
	}

	org, err := s.GetOrgByID(orgID)
	if err != nil {
		return nil, err
	}

	token, prefix, err := auth.GenerateInviteToken()
	if err != nil {
		return nil, err
	}
	hash := auth.HashToken(token)

	expiresInDays := in.ExpiresInDays
	if expiresInDays <= 0 {
		expiresInDays = 7
	}
	expiresAt := time.Now().UTC().Add(time.Duration(expiresInDays) * 24 * time.Hour)

	invID := ids.NewUUID()
	now := time.Now().UTC().Format(time.RFC3339)
	expiresStr := expiresAt.Format(time.RFC3339)

	_, err = s.db.Exec(`
		INSERT INTO org_invitations (id, org_id, token_hash, token_prefix, role, created_by, expires_at, max_uses, use_count, revoked, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, 0, ?)`,
		invID, orgID, hash, prefix, role, userID, expiresStr, in.MaxUses, now,
	)
	if err != nil {
		return nil, err
	}

	base := strings.TrimRight(publicBaseURL, "/")
	if base == "" {
		base = strings.TrimSuffix(s.serverURL, "/api/v1")
	}
	inviteURL := base + "/admin?invite=" + token

	inv := types.Invitation{
		ID:          invID,
		OrgID:       orgID,
		OrgName:     org.Name,
		Role:        role,
		TokenPrefix: prefix,
		InviteURL:   inviteURL,
		ExpiresAt:   &expiresAt,
		MaxUses:     in.MaxUses,
	}
	inv.CreatedAt, _ = time.Parse(time.RFC3339, now)

	return &types.CreateInvitationResponse{
		Invitation: inv,
		Token:      token,
		InviteURL:  inviteURL,
	}, nil
}

func (s *Service) ListInvitations(orgID, userID string) ([]types.Invitation, error) {
	if !s.UserIsOrgAdmin(userID) {
		return nil, fmt.Errorf("forbidden")
	}

	rows, err := s.db.Query(`
		SELECT i.id, i.org_id, o.name, i.role, i.token_prefix, i.expires_at, i.max_uses, i.use_count, i.revoked, i.created_at
		FROM org_invitations i
		JOIN organizations o ON o.id = i.org_id
		WHERE i.org_id = ?
		ORDER BY i.created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []types.Invitation
	for rows.Next() {
		var inv types.Invitation
		var expiresAt, createdAt string
		var revoked int
		if err := rows.Scan(
			&inv.ID, &inv.OrgID, &inv.OrgName, &inv.Role, &inv.TokenPrefix,
			&expiresAt, &inv.MaxUses, &inv.UseCount, &revoked, &createdAt,
		); err != nil {
			return nil, err
		}
		inv.Revoked = revoked == 1
		if expiresAt != "" {
			t, _ := time.Parse(time.RFC3339, expiresAt)
			inv.ExpiresAt = &t
		}
		inv.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		list = append(list, inv)
	}
	return list, nil
}

func (s *Service) RevokeInvitation(orgID, invID, userID string) error {
	if !s.UserIsOrgAdmin(userID) {
		return fmt.Errorf("forbidden")
	}
	res, err := s.db.Exec(
		`UPDATE org_invitations SET revoked = 1 WHERE id = ? AND org_id = ?`, invID, orgID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("invitation not found")
	}
	return nil
}

func (s *Service) PreviewInvitation(token string) (*types.InvitationPreview, error) {
	inv, org, err := s.lookupInvitation(token)
	if err != nil {
		return nil, err
	}

	preview := &types.InvitationPreview{
		OrgID:     org.ID,
		OrgName:   org.Name,
		Role:      inv.Role,
		ExpiresAt: inv.ExpiresAt,
		Valid:     true,
	}
	if reason := s.invalidInviteReason(inv); reason != "" {
		preview.Valid = false
		preview.InvalidReason = reason
	}
	return preview, nil
}

func (s *Service) AcceptInvitation(token, userID string) error {
	inv, _, err := s.lookupInvitation(token)
	if err != nil {
		return err
	}
	if reason := s.invalidInviteReason(inv); reason != "" {
		return fmt.Errorf(reason)
	}

	var existing int
	_ = s.db.QueryRow(
		`SELECT COUNT(*) FROM org_members WHERE user_id = ? AND org_id = ?`, userID, inv.OrgID,
	).Scan(&existing)
	if existing > 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = tx.Exec(
		`INSERT INTO org_members (user_id, org_id, role, joined_at) VALUES (?, ?, ?, ?)`,
		userID, inv.OrgID, inv.Role, now,
	)
	if err != nil {
		return err
	}

	var wsID string
	if err := tx.QueryRow(
		`SELECT id FROM workspaces WHERE org_id = ? AND is_default = 1 LIMIT 1`, inv.OrgID,
	).Scan(&wsID); err != nil {
		return err
	}

	_, err = tx.Exec(
		`INSERT OR IGNORE INTO workspace_members (user_id, workspace_id, role, joined_at) VALUES (?, ?, ?, ?)`,
		userID, wsID, inv.Role, now,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(
		`INSERT OR IGNORE INTO channel_members (channel_id, user_id, joined_at)
		 SELECT id, ?, ? FROM channels WHERE workspace_id = ? AND type = 'public'`,
		userID, now, wsID,
	)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`UPDATE org_invitations SET use_count = use_count + 1 WHERE id = ?`, inv.ID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

type invitationRow struct {
	ID        string
	OrgID     string
	Role      string
	ExpiresAt *time.Time
	MaxUses   int
	UseCount  int
	Revoked   bool
}

func (s *Service) lookupInvitation(token string) (*invitationRow, *types.Organization, error) {
	if token == "" {
		return nil, nil, fmt.Errorf("invalid invitation")
	}
	hash := auth.HashToken(token)

	var inv invitationRow
	var expiresAt string
	var revoked int
	err := s.db.QueryRow(`
		SELECT id, org_id, role, COALESCE(expires_at, ''), max_uses, use_count, revoked
		FROM org_invitations WHERE token_hash = ?`, hash).Scan(
		&inv.ID, &inv.OrgID, &inv.Role, &expiresAt, &inv.MaxUses, &inv.UseCount, &revoked,
	)
	if err == sql.ErrNoRows {
		return nil, nil, fmt.Errorf("invitation not found")
	}
	if err != nil {
		return nil, nil, err
	}
	inv.Revoked = revoked == 1
	if expiresAt != "" {
		t, _ := time.Parse(time.RFC3339, expiresAt)
		inv.ExpiresAt = &t
	}

	org, err := s.GetOrgByID(inv.OrgID)
	if err != nil {
		return nil, nil, err
	}
	return &inv, org, nil
}

func (s *Service) invalidInviteReason(inv *invitationRow) string {
	if inv.Revoked {
		return "invitation revoked"
	}
	if inv.ExpiresAt != nil && time.Now().After(*inv.ExpiresAt) {
		return "invitation expired"
	}
	if inv.MaxUses > 0 && inv.UseCount >= inv.MaxUses {
		return "invitation max uses reached"
	}
	return ""
}
