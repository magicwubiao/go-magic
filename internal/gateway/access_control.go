package gateway

import (
	"regexp"
	"strings"
	"sync"
)

// AccessPolicy defines the access control policy
type AccessPolicy string

const (
	AccessPolicyOpen      AccessPolicy = "open"      // 允许所有人
	AccessPolicyAllowlist AccessPolicy = "allowlist" // 仅白名单
	AccessPolicyDisabled  AccessPolicy = "disabled"  // 禁用
)

// AccessControl manages access control for DMs and groups
type AccessControl struct {
	mu sync.RWMutex

	DMPolicy        AccessPolicy     `json:"dm_policy"`
	GroupPolicy     AccessPolicy     `json:"group_policy"`
	DMAllowlist     []string         `json:"dm_allowlist"`
	GroupAllowlist  []string         `json:"group_allowlist"`
	MentionPatterns []*regexp.Regexp `json:"-"`
}

// DefaultAccessControl creates a default access control (open for all)
func DefaultAccessControl() *AccessControl {
	return &AccessControl{
		DMPolicy:    AccessPolicyOpen,
		GroupPolicy: AccessPolicyOpen,
	}
}

// NewAccessControl creates access control from config
func NewAccessControl(config map[string]interface{}) *AccessControl {
	ac := DefaultAccessControl()

	if v, ok := config["dm_policy"].(string); ok {
		ac.DMPolicy = AccessPolicy(v)
	}
	if v, ok := config["group_policy"].(string); ok {
		ac.GroupPolicy = AccessPolicy(v)
	}
	if v, ok := config["dm_allowlist"].([]string); ok {
		ac.DMAllowlist = v
	}
	if v, ok := config["group_allowlist"].([]string); ok {
		ac.GroupAllowlist = v
	}
	if v, ok := config["mention_patterns"].([]string); ok {
		ac.SetMentionPatterns(v)
	}

	return ac
}

// SetMentionPatterns sets regex patterns for mention detection
func (ac *AccessControl) SetMentionPatterns(patterns []string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.MentionPatterns = make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			ac.MentionPatterns = append(ac.MentionPatterns, re)
		}
	}
}

// CheckDM checks if DM from user is allowed
func (ac *AccessControl) CheckDM(userID string) bool {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	switch ac.DMPolicy {
	case AccessPolicyOpen:
		return true
	case AccessPolicyAllowlist:
		return contains(ac.DMAllowlist, userID)
	case AccessPolicyDisabled:
		return false
	default:
		return false
	}
}

// CheckGroup checks if group message is allowed
func (ac *AccessControl) CheckGroup(groupID string, isMentioned bool) bool {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	switch ac.GroupPolicy {
	case AccessPolicyOpen:
		return isMentioned
	case AccessPolicyAllowlist:
		if !contains(ac.GroupAllowlist, groupID) {
			return false
		}
		return isMentioned
	case AccessPolicyDisabled:
		return false
	default:
		return false
	}
}

// IsMentioned checks if message contains a mention
func (ac *AccessControl) IsMentioned(content string, botID string) bool {
	if botID != "" && strings.Contains(content, "<@"+botID+">") {
		return true
	}
	if botID != "" && strings.Contains(content, "@"+botID) {
		return true
	}

	ac.mu.RLock()
	defer ac.mu.RUnlock()

	for _, re := range ac.MentionPatterns {
		if re.MatchString(content) {
			return true
		}
	}

	return false
}

// AddDMAllowlist adds a user to DM allowlist
func (ac *AccessControl) AddDMAllowlist(userID string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if !contains(ac.DMAllowlist, userID) {
		ac.DMAllowlist = append(ac.DMAllowlist, userID)
	}
}

// AddGroupAllowlist adds a group to group allowlist
func (ac *AccessControl) AddGroupAllowlist(groupID string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if !contains(ac.GroupAllowlist, groupID) {
		ac.GroupAllowlist = append(ac.GroupAllowlist, groupID)
	}
}

// RemoveDMAllowlist removes a user from DM allowlist
func (ac *AccessControl) RemoveDMAllowlist(userID string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.DMAllowlist = removeFromSlice(ac.DMAllowlist, userID)
}

// RemoveGroupAllowlist removes a group from group allowlist
func (ac *AccessControl) RemoveGroupAllowlist(groupID string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	ac.GroupAllowlist = removeFromSlice(ac.GroupAllowlist, groupID)
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func removeFromSlice(slice []string, item string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}
