package identityclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cocina/server-mvp/types"
)

type Client struct {
	baseURL   string
	serverURL string
	apiKey    string
	http      *http.Client
}

func New(baseURL, serverURL, apiKey string) *Client {
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		serverURL: strings.TrimRight(serverURL, "/"),
		apiKey:    apiKey,
		http:      &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) RegisterServer() (*types.Organization, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("identity api key is required")
	}

	body, _ := json.Marshal(map[string]string{
		"api_key":    c.apiKey,
		"server_url": c.serverURL,
	})

	resp, err := c.http.Post(c.baseURL+"/api/v1/server/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("identity register failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var envelope struct {
		Data types.Organization `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Data.ID == "" {
		return nil, fmt.Errorf("identity register returned empty org")
	}
	return &envelope.Data, nil
}

func (c *Client) GetUserOrgs(accessToken string) ([]types.OrgMembership, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/v1/users/me/orgs", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("identity orgs failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var envelope struct {
		Data []types.OrgMembership `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	return envelope.Data, nil
}

func NormalizeServerURL(u string) string {
	u = strings.TrimSpace(strings.TrimRight(u, "/"))
	if strings.HasSuffix(u, "/api/v1") {
		u = strings.TrimSuffix(u, "/api/v1")
	}
	return strings.ToLower(u)
}
