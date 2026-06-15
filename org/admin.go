package org

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/cocina/server-mvp/types"
)

const settingAdminBootstrap = "admin_bootstrap_complete"

func (s *Service) AdminOrgID() (string, error) {
	if linked, _ := s.getLinkedIdentityOrgID(); linked != "" {
		return linked, nil
	}
	return DefaultOrgID, nil
}

func (s *Service) HasAnyOrgAdmin() bool {
	var count int
	_ = s.db.QueryRow(`
		SELECT COUNT(*) FROM org_members WHERE role IN (?, ?)`,
		types.RoleOwner, types.RoleAdmin,
	).Scan(&count)
	return count > 0
}

func (s *Service) BootstrapFirstAdmin(userID string) error {
	if s.HasAnyOrgAdmin() {
		return fmt.Errorf("admin already exists")
	}

	orgID, err := s.AdminOrgID()
	if err != nil {
		return err
	}

	if err := s.setOrgMemberRole(userID, orgID, types.RoleOwner); err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT INTO server_settings (key, value) VALUES (?, 'true')
		ON CONFLICT(key) DO UPDATE SET value = 'true'`,
		settingAdminBootstrap,
	)
	return err
}

func (s *Service) ListOrgUsers(orgID string) ([]types.AdminUser, error) {
	rows, err := s.db.Query(`
		SELECT u.id, u.email, u.username, COALESCE(u.display_name, ''), COALESCE(m.role, 'member'),
		       COALESCE(u.presence_status, 'available'), u.created_at
		FROM users u
		LEFT JOIN org_members m ON m.user_id = u.id AND m.org_id = ?
		ORDER BY u.created_at ASC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []types.AdminUser
	for rows.Next() {
		var u types.AdminUser
		var createdAt string
		if err := rows.Scan(
			&u.ID, &u.Email, &u.Username, &u.DisplayName, &u.Role,
			&u.PresenceStatus, &createdAt,
		); err != nil {
			return nil, err
		}
		if u.Role == "" {
			u.Role = types.RoleMember
		}
		u.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		users = append(users, u)
	}
	return users, nil
}

func (s *Service) SetOrgMemberRole(actorID, targetUserID, orgID, role string) error {
	if !s.UserIsOrgAdmin(actorID) {
		return fmt.Errorf("forbidden")
	}
	if targetUserID == actorID && role != types.RoleOwner && role != types.RoleAdmin {
		return fmt.Errorf("cannot demote yourself")
	}
	switch role {
	case types.RoleOwner, types.RoleAdmin, types.RoleMember:
	default:
		return fmt.Errorf("invalid role")
	}
	return s.setOrgMemberRole(targetUserID, orgID, role)
}

func (s *Service) setOrgMemberRole(userID, orgID, role string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var exists int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM org_members WHERE user_id = ? AND org_id = ?`, userID, orgID,
	).Scan(&exists); err != nil {
		return err
	}

	if exists == 0 {
		return s.addUserToOrg(userID, orgID, role)
	}

	_, err := s.db.Exec(
		`UPDATE org_members SET role = ? WHERE user_id = ? AND org_id = ?`, role, userID, orgID,
	)
	if err != nil {
		return err
	}

	var wsID string
	if err := s.db.QueryRow(
		`SELECT id FROM workspaces WHERE org_id = ? AND is_default = 1 LIMIT 1`, orgID,
	).Scan(&wsID); err != nil {
		return err
	}

	_, err = s.db.Exec(
		`UPDATE workspace_members SET role = ? WHERE user_id = ? AND workspace_id = ?`,
		role, userID, wsID,
	)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT OR IGNORE INTO channel_members (channel_id, user_id, joined_at)
		SELECT id, ?, ? FROM channels WHERE workspace_id = ? AND type = 'public'`,
		userID, now, wsID,
	)
	return err
}

func (s *Service) CountUsers() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (s *Service) GetOrgByID(orgID string) (*types.Organization, error) {
	var org types.Organization
	var createdAt string
	var serverURL string
	err := s.db.QueryRow(`
		SELECT id, name, slug, COALESCE(server_url, ''), deployment_mode, created_at
		FROM organizations WHERE id = ?`, orgID,
	).Scan(&org.ID, &org.Name, &org.Slug, &serverURL, &org.DeploymentMode, &createdAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("organization not found")
	}
	if err != nil {
		return nil, err
	}
	if serverURL == "" {
		serverURL = s.serverURL
	}
	org.ServerURL = serverURL
	org.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &org, nil
}
