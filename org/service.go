package org

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/cocina/server-mvp/identityclient"
	"github.com/cocina/server-mvp/types"
)

// Service handles organization, workspace, and channel operations.
type Service struct {
	db             *sql.DB
	serverURL      string
	identityURL    string
	identityAPIKey string
}

func NewService(db *sql.DB, serverURL, identityURL, identityAPIKey string) *Service {
	return &Service{
		db:             db,
		serverURL:      strings.TrimRight(serverURL, "/"),
		identityURL:    strings.TrimRight(identityURL, "/"),
		identityAPIKey: identityAPIKey,
	}
}

func (s *Service) ListOrgsForUser(userID string) ([]types.OrgMembership, error) {
	rows, err := s.db.Query(`
		SELECT o.id, o.name, o.slug, o.deployment_mode, COALESCE(o.server_url, ''), o.created_at, m.role, m.joined_at
		FROM organizations o
		JOIN org_members m ON m.org_id = o.id
		WHERE m.user_id = ?
		ORDER BY o.name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []types.OrgMembership
	for rows.Next() {
		var item types.OrgMembership
		var createdAt, joinedAt string
		var serverURL string
		if err := rows.Scan(
			&item.Org.ID, &item.Org.Name, &item.Org.Slug, &item.Org.DeploymentMode,
			&serverURL, &createdAt, &item.Role, &joinedAt,
		); err != nil {
			return nil, err
		}
		if serverURL == "" {
			serverURL = s.serverURL
		}
		item.Org.ServerURL = serverURL
		item.Org.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		item.JoinedAt, _ = time.Parse(time.RFC3339, joinedAt)
		result = append(result, item)
	}
	return result, nil
}

func (s *Service) IsOrgMember(userID, orgID string) bool {
	var count int
	_ = s.db.QueryRow(
		`SELECT COUNT(*) FROM org_members WHERE user_id = ? AND org_id = ?`,
		userID, orgID,
	).Scan(&count)
	return count > 0
}

func (s *Service) ListWorkspaces(userID, orgID string) ([]types.Workspace, error) {
	if !s.IsOrgMember(userID, orgID) {
		return nil, fmt.Errorf("not an organization member")
	}

	rows, err := s.db.Query(`
		SELECT w.id, w.org_id, w.name, w.slug, COALESCE(w.description, ''), w.is_default, w.created_at
		FROM workspaces w
		WHERE w.org_id = ?
		ORDER BY w.is_default DESC, w.name ASC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var workspaces []types.Workspace
	for rows.Next() {
		var ws types.Workspace
		var createdAt string
		var isDefault int
		if err := rows.Scan(
			&ws.ID, &ws.OrgID, &ws.Name, &ws.Slug, &ws.Description, &isDefault, &createdAt,
		); err != nil {
			return nil, err
		}
		ws.IsDefault = isDefault == 1
		ws.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		workspaces = append(workspaces, ws)
	}
	return workspaces, nil
}

func (s *Service) IsWorkspaceMember(userID, workspaceID string) bool {
	var count int
	_ = s.db.QueryRow(`
		SELECT COUNT(*) FROM workspace_members
		WHERE user_id = ? AND workspace_id = ?`, userID, workspaceID).Scan(&count)
	return count > 0
}

func (s *Service) ListChannels(userID, workspaceID string) ([]types.Channel, error) {
	if !s.IsWorkspaceMember(userID, workspaceID) {
		return nil, fmt.Errorf("not a workspace member")
	}

	rows, err := s.db.Query(`
		SELECT c.id, c.workspace_id, c.name, c.type, COALESCE(c.description, ''),
		       COALESCE(c.created_by, ''), c.created_at,
		       COALESCE(dp.other_user_id, ''), COALESCE(u.username, '')
		FROM channels c
		LEFT JOIN (
			SELECT channel_id,
			       CASE WHEN user_a = ? THEN user_b ELSE user_a END AS other_user_id
			FROM dm_participants
			WHERE user_a = ? OR user_b = ?
		) dp ON dp.channel_id = c.id
		LEFT JOIN users u ON u.id = dp.other_user_id
		WHERE c.workspace_id = ?
		  AND (
		    c.type IN ('public', 'private')
		    OR (c.type = 'dm' AND dp.other_user_id IS NOT NULL)
		  )
		ORDER BY c.type ASC, c.name ASC`, userID, userID, userID, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []types.Channel
	for rows.Next() {
		var ch types.Channel
		var createdAt string
		if err := rows.Scan(
			&ch.ID, &ch.WorkspaceID, &ch.Name, &ch.Type, &ch.Description,
			&ch.CreatedBy, &createdAt, &ch.ParticipantID, &ch.ParticipantName,
		); err != nil {
			return nil, err
		}
		ch.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		channels = append(channels, ch)
	}
	return channels, nil
}

func (s *Service) UserCanAccessChannel(userID, channelID string) (bool, error) {
	var chType, workspaceID string
	err := s.db.QueryRow(
		`SELECT type, workspace_id FROM channels WHERE id = ?`, channelID,
	).Scan(&chType, &workspaceID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !s.IsWorkspaceMember(userID, workspaceID) {
		return false, nil
	}
	if chType == types.ChannelTypeDM {
		var count int
		_ = s.db.QueryRow(`
			SELECT COUNT(*) FROM dm_participants
			WHERE channel_id = ? AND (user_a = ? OR user_b = ?)`,
			channelID, userID, userID).Scan(&count)
		return count > 0, nil
	}
	return true, nil
}

func (s *Service) GetOrCreateDM(userID, workspaceID string, otherUserIDs []string) (*types.Channel, error) {
	if len(otherUserIDs) != 1 {
		return nil, fmt.Errorf("only one-on-one DMs are supported in MVP")
	}
	otherID := otherUserIDs[0]
	if otherID == userID {
		return nil, fmt.Errorf("cannot create DM with yourself")
	}
	if !s.IsWorkspaceMember(userID, workspaceID) || !s.IsWorkspaceMember(otherID, workspaceID) {
		return nil, fmt.Errorf("both users must be workspace members")
	}

	userA, userB := sortPair(userID, otherID)

	var existingID string
	err := s.db.QueryRow(`
		SELECT channel_id FROM dm_participants
		WHERE workspace_id = ? AND user_a = ? AND user_b = ?`,
		workspaceID, userA, userB).Scan(&existingID)
	if err == nil {
		return s.getChannelByID(existingID, userID)
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	otherName, _ := s.username(otherID)
	channelID, err := GenerateChannelID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	name := otherName
	if name == "" {
		name = "dm"
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO channels (id, workspace_id, name, type, description, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		channelID, workspaceID, name, types.ChannelTypeDM, "Mensaje directo", userID, now)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(`
		INSERT INTO dm_participants (channel_id, workspace_id, user_a, user_b)
		VALUES (?, ?, ?, ?)`,
		channelID, workspaceID, userA, userB)
	if err != nil {
		return nil, err
	}

	for _, uid := range []string{userID, otherID} {
		_, err = tx.Exec(`
			INSERT OR IGNORE INTO channel_members (channel_id, user_id, joined_at)
			VALUES (?, ?, ?)`, channelID, uid, now)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return s.getChannelByID(channelID, userID)
}

// GetChannelByID returns a channel by id with DM participant info when applicable.
func (s *Service) GetChannelByID(channelID, currentUserID string) (*types.Channel, error) {
	return s.getChannelByID(channelID, currentUserID)
}

func (s *Service) getChannelByID(channelID, currentUserID string) (*types.Channel, error) {
	var ch types.Channel
	var createdAt string
	var otherID, otherName string
	err := s.db.QueryRow(`
		SELECT c.id, c.workspace_id, c.name, c.type, COALESCE(c.description, ''),
		       COALESCE(c.created_by, ''), c.created_at,
		       CASE WHEN dp.user_a = ? THEN dp.user_b ELSE dp.user_a END,
		       COALESCE(u.username, '')
		FROM channels c
		LEFT JOIN dm_participants dp ON dp.channel_id = c.id
		LEFT JOIN users u ON u.id = CASE WHEN dp.user_a = ? THEN dp.user_b ELSE dp.user_a END
		WHERE c.id = ?`, currentUserID, currentUserID, channelID).Scan(
		&ch.ID, &ch.WorkspaceID, &ch.Name, &ch.Type, &ch.Description,
		&ch.CreatedBy, &createdAt, &otherID, &otherName,
	)
	if err != nil {
		return nil, err
	}
	ch.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	ch.ParticipantID = otherID
	ch.ParticipantName = otherName
	return &ch, nil
}

func (s *Service) EnsureDefaultOrgForUser(userID string) error {
	if linkedOrgID, _ := s.getLinkedIdentityOrgID(); linkedOrgID != "" {
		return s.EnsureLinkedOrgMember(userID)
	}

	var count int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM org_members WHERE user_id = ?`, userID,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return s.addUserToOrg(userID, DefaultOrgID, types.RoleMember)
}

const (
	settingIdentityOrgID = "identity_org_id"
)

func (s *Service) UpdateNetworkURLs(serverAPIURL, identityURL, deploymentMode string) error {
	serverAPIURL = strings.TrimRight(serverAPIURL, "/")
	s.serverURL = serverAPIURL
	if identityURL != "" {
		s.identityURL = strings.TrimRight(identityURL, "/")
	}

	linkedOrgID, _ := s.getLinkedIdentityOrgID()
	if linkedOrgID != "" {
		_, err := s.db.Exec(`
			UPDATE organizations SET server_url = ?, deployment_mode = ? WHERE id = ?`,
			serverAPIURL, deploymentMode, linkedOrgID,
		)
		return err
	}

	_, err := s.db.Exec(`
		UPDATE organizations SET server_url = ?, deployment_mode = ?
		WHERE server_url IS NULL OR server_url = '' OR server_url LIKE 'http://localhost%'`,
		serverAPIURL, deploymentMode,
	)
	return err
}

func (s *Service) UserIsOrgAdmin(userID string) bool {
	linkedOrgID, err := s.getLinkedIdentityOrgID()
	if err != nil || linkedOrgID == "" {
		var count int
		_ = s.db.QueryRow(`
			SELECT COUNT(*) FROM org_members
			WHERE user_id = ? AND role IN ('owner', 'admin')`, userID,
		).Scan(&count)
		return count > 0
	}

	var role string
	err = s.db.QueryRow(`
		SELECT role FROM org_members WHERE user_id = ? AND org_id = ?`,
		userID, linkedOrgID,
	).Scan(&role)
	if err != nil {
		return false
	}
	return role == types.RoleOwner || role == types.RoleAdmin
}

func (s *Service) RegisterServerWithIdentity(serverAPIURL string) (*types.Organization, error) {
	if s.identityURL == "" || s.identityAPIKey == "" {
		return nil, fmt.Errorf("identity not configured (set COCINA_IDENTITY_URL and COCINA_IDENTITY_API_KEY)")
	}
	client := identityclient.New(s.identityURL, serverAPIURL, s.identityAPIKey)
	org, err := client.RegisterServer()
	if err != nil {
		return nil, err
	}
	if err := s.EnsureIdentityOrg(org); err != nil {
		return nil, err
	}
	return org, nil
}

func (s *Service) SetIdentityURL(url string) {
	s.identityURL = strings.TrimRight(url, "/")
}

func (s *Service) ServerURL() string  { return s.serverURL }
func (s *Service) IdentityURL() string { return s.identityURL }

func (s *Service) getLinkedIdentityOrgID() (string, error) {
	var value string
	err := s.db.QueryRow(
		`SELECT value FROM server_settings WHERE key = ?`, settingIdentityOrgID,
	).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// EnsureIdentityOrg upserts the organization linked to cocina-identity.
func (s *Service) EnsureIdentityOrg(org *types.Organization) error {
	if org == nil || org.ID == "" {
		return fmt.Errorf("invalid identity org")
	}

	now := time.Now().UTC().Format(time.RFC3339)
	serverURL := org.ServerURL
	if serverURL == "" {
		serverURL = s.serverURL
	}
	deploymentMode := org.DeploymentMode
	if deploymentMode == "" {
		deploymentMode = types.DeploymentLocal
	}

	_, err := s.db.Exec(`
		INSERT INTO organizations (id, name, slug, server_url, deployment_mode, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			slug = excluded.slug,
			server_url = excluded.server_url,
			deployment_mode = excluded.deployment_mode`,
		org.ID, org.Name, org.Slug, serverURL, deploymentMode, now,
	)
	if err != nil {
		return err
	}

	wsID := "ws_" + org.ID
	chID := "ch_" + org.ID + "_general"

	_, err = s.db.Exec(`
		INSERT OR IGNORE INTO workspaces (id, org_id, name, slug, description, is_default, created_at)
		VALUES (?, ?, 'General', 'general', 'Workspace principal', 1, ?)`,
		wsID, org.ID, now,
	)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT OR IGNORE INTO channels (id, workspace_id, name, type, description, created_at)
		VALUES (?, ?, 'general', 'public', 'Canal principal de la organización', ?)`,
		chID, wsID, now,
	)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT INTO server_settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		settingIdentityOrgID, org.ID,
	)
	return err
}

// EnsureLinkedOrgMember adds the user to the identity-linked org when configured.
func (s *Service) EnsureLinkedOrgMember(userID string) error {
	orgID, err := s.getLinkedIdentityOrgID()
	if err != nil || orgID == "" {
		return err
	}
	return s.addUserToOrg(userID, orgID, types.RoleMember)
}

// TryLinkIdentityOrg registers this server with Identity if not linked yet.
func (s *Service) TryLinkIdentityOrg() error {
	if s.identityURL == "" || s.identityAPIKey == "" {
		return nil
	}
	if linked, _ := s.getLinkedIdentityOrgID(); linked != "" {
		return nil
	}
	client := identityclient.New(s.identityURL, s.serverURL, s.identityAPIKey)
	org, err := client.RegisterServer()
	if err != nil {
		return err
	}
	return s.EnsureIdentityOrg(org)
}

// SyncIdentityMemberships syncs org memberships from Identity for the authenticated user.
func (s *Service) SyncIdentityMemberships(localUserID, bearerToken string) error {
	if s.identityURL == "" {
		return s.EnsureDefaultOrgForUser(localUserID)
	}

	_ = s.TryLinkIdentityOrg()

	client := identityclient.New(s.identityURL, s.serverURL, s.identityAPIKey)
	memberships, err := client.GetUserOrgs(bearerToken)
	if err != nil {
		return s.EnsureLinkedOrgMember(localUserID)
	}

	thisServer := identityclient.NormalizeServerURL(s.serverURL)
	matched := 0
	for _, m := range memberships {
		orgURL := m.Org.ServerURL
		if orgURL == "" {
			orgURL = s.serverURL
		}
		if identityclient.NormalizeServerURL(orgURL) != thisServer {
			continue
		}
		org := m.Org
		if org.ServerURL == "" {
			org.ServerURL = s.serverURL
		}
		if err := s.EnsureIdentityOrg(&org); err != nil {
			return err
		}
		role := m.Role
		if role == "" {
			role = types.RoleMember
		}
		if err := s.addUserToOrg(localUserID, org.ID, role); err != nil {
			return err
		}
		matched++
	}

	if matched == 0 {
		return nil
	}
	return nil
}

func (s *Service) addUserToOrg(userID, orgID, role string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO org_members (user_id, org_id, role, joined_at)
		VALUES (?, ?, ?, ?)`, userID, orgID, role, now)
	if err != nil {
		return err
	}

	var wsID string
	err = s.db.QueryRow(
		`SELECT id FROM workspaces WHERE org_id = ? AND is_default = 1 LIMIT 1`, orgID,
	).Scan(&wsID)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT OR IGNORE INTO workspace_members (user_id, workspace_id, role, joined_at)
		VALUES (?, ?, ?, ?)`, userID, wsID, role, now)
	if err != nil {
		return err
	}

	_, err = s.db.Exec(`
		INSERT OR IGNORE INTO channel_members (channel_id, user_id, joined_at)
		SELECT id, ?, ? FROM channels
		WHERE workspace_id = ? AND type = 'public'`,
		userID, now, wsID)
	return err
}

// HelloState contains bootstrap data for WebSocket hello event.
type HelloState struct {
	Workspaces []types.Workspace
	Channels   []types.Channel
}

// BuildHelloForUser returns workspaces and channels accessible to the user on this server.
func (s *Service) BuildHelloForUser(userID string) (*HelloState, error) {
	rows, err := s.db.Query(`
		SELECT w.id, w.org_id, w.name, w.slug, COALESCE(w.description, ''), w.is_default, w.created_at
		FROM workspaces w
		JOIN workspace_members wm ON wm.workspace_id = w.id
		WHERE wm.user_id = ?
		ORDER BY w.is_default DESC, w.name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	state := &HelloState{
		Workspaces: []types.Workspace{},
		Channels:   []types.Channel{},
	}

	for rows.Next() {
		var ws types.Workspace
		var createdAt string
		var isDefault int
		if err := rows.Scan(
			&ws.ID, &ws.OrgID, &ws.Name, &ws.Slug, &ws.Description, &isDefault, &createdAt,
		); err != nil {
			return nil, err
		}
		ws.IsDefault = isDefault == 1
		ws.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		state.Workspaces = append(state.Workspaces, ws)

		channels, err := s.ListChannels(userID, ws.ID)
		if err != nil {
			continue
		}
		state.Channels = append(state.Channels, channels...)
	}

	return state, nil
}

func (s *Service) username(userID string) (string, error) {
	var name string
	err := s.db.QueryRow(`SELECT username FROM users WHERE id = ?`, userID).Scan(&name)
	return name, err
}

func sortPair(a, b string) (string, string) {
	if strings.Compare(a, b) < 0 {
		return a, b
	}
	return b, a
}
