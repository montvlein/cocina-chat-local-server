package org

import (
	"crypto/rand"
	"fmt"
)

const (
	DefaultOrgID       = "org_cocina_default"
	DefaultWorkspaceID = "ws_cocina_default"
	DefaultChannelID   = "ch_general"
)

func generateID(prefix string) (string, error) {
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%x", prefix, bytes), nil
}

func GenerateOrgID() (string, error)    { return generateID("org_") }
func GenerateWorkspaceID() (string, error) { return generateID("ws_") }
func GenerateChannelID() (string, error)    { return generateID("ch_") }
