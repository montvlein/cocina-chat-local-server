package messaging

import (
	"fmt"
	"sort"
	"strings"
)

const dmChannelPrefix = "dm::"

// BuildDMChannelID returns a deterministic DM channel id for two users.
func BuildDMChannelID(userA, userB string) string {
	ids := []string{userA, userB}
	sort.Strings(ids)
	return fmt.Sprintf("%s%s::%s", dmChannelPrefix, ids[0], ids[1])
}

// OtherUserInDMChannel extracts the other participant from a DM channel id.
func OtherUserInDMChannel(channelID, currentUserID string) (string, bool) {
	if strings.HasPrefix(channelID, dmChannelPrefix) {
		rest := strings.TrimPrefix(channelID, dmChannelPrefix)
		parts := strings.Split(rest, "::")
		if len(parts) != 2 {
			return "", false
		}
		if parts[0] == currentUserID {
			return parts[1], true
		}
		if parts[1] == currentUserID {
			return parts[0], true
		}
		return "", false
	}

	// Legacy format: dm_{id1}_{id2} where ids may contain underscores.
	if strings.HasPrefix(channelID, "dm_") {
		rest := strings.TrimPrefix(channelID, "dm_")
		if strings.HasPrefix(rest, currentUserID+"_") {
			other := strings.TrimPrefix(rest, currentUserID+"_")
			if other != "" && other != currentUserID {
				return other, true
			}
		}
		if strings.HasSuffix(rest, "_"+currentUserID) {
			other := strings.TrimSuffix(rest, "_"+currentUserID)
			if other != "" && other != currentUserID {
				return other, true
			}
		}
	}

	return "", false
}

func IsDMChannel(channelID string) bool {
	return strings.HasPrefix(channelID, dmChannelPrefix) || strings.HasPrefix(channelID, "dm_")
}
