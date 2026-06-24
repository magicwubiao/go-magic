package gateway

import (
	"context"
	"testing"
)

func TestAccessControl_CheckDM(t *testing.T) {
	ac := DefaultAccessControl()

	if !ac.CheckDM("user1") {
		t.Error("open policy should allow all DMs")
	}

	ac.DMPolicy = AccessPolicyAllowlist
	ac.DMAllowlist = []string{"user1", "user2"}

	if !ac.CheckDM("user1") {
		t.Error("allowlist should include user1")
	}
	if ac.CheckDM("user3") {
		t.Error("allowlist should exclude user3")
	}

	ac.DMPolicy = AccessPolicyDisabled
	if ac.CheckDM("user1") {
		t.Error("disabled policy should block all DMs")
	}
}

func TestAccessControl_CheckGroup(t *testing.T) {
	ac := DefaultAccessControl()

	if !ac.CheckGroup("group1", true) {
		t.Error("open policy should allow mentioned group messages")
	}
	if ac.CheckGroup("group1", false) {
		t.Error("open policy should block non-mentioned group messages")
	}

	ac.GroupPolicy = AccessPolicyAllowlist
	ac.GroupAllowlist = []string{"group1"}

	if !ac.CheckGroup("group1", true) {
		t.Error("allowlist group with mention should pass")
	}
	if ac.CheckGroup("group2", true) {
		t.Error("non-allowlist group should be blocked")
	}
}

func TestBasePlatform_ShouldProcessChannel(t *testing.T) {
	bp := NewBasePlatform("test", nil)

	if !bp.ShouldProcessChannel("chan1") {
		t.Error("empty allowlist should allow all channels")
	}

	bp.SetChannelFilter([]string{"chan1", "chan2"}, nil)
	if !bp.ShouldProcessChannel("chan1") {
		t.Error("allowlisted channel should pass")
	}
	if bp.ShouldProcessChannel("chan3") {
		t.Error("non-allowlisted channel should be blocked")
	}

	bp.SetChannelFilter(nil, []string{"blocked1"})
	if !bp.ShouldProcessChannel("chan1") {
		t.Error("non-blocked channel should pass")
	}
	if bp.ShouldProcessChannel("blocked1") {
		t.Error("blocked channel should not pass")
	}
}

func TestPlatformRegistry_AllPlatforms(t *testing.T) {
	registry := GetRegistry()

	expected := []string{
		"qq", "whatsapp", "whatsapp_business", "telegram",
		"discord", "slack", "feishu", "wechat_ilink",
		"wecom", "dingtalk", "line", "matrix",
	}

	for _, id := range expected {
		info, ok := registry.GetInfo(id)
		if !ok {
			t.Errorf("platform %s not found in registry", id)
			continue
		}
		if info.ID != id {
			t.Errorf("platform %s has wrong ID: %s", id, info.ID)
		}
	}

	all := registry.List()
	if len(all) != len(expected) {
		t.Errorf("expected %d platforms, got %d", len(expected), len(all))
	}
}

func TestBasePlatform_AccessControlInEmit(t *testing.T) {
	bp := NewBasePlatform("test", map[string]interface{}{
		"dm_policy":    string(AccessPolicyAllowlist),
		"dm_allowlist": []string{"allowed_user"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bp.onConnect = func(ctx context.Context) error { return nil }
	bp.Connect(ctx)

	msg := Message{
		ID:        "test1",
		ChannelID: "dm1",
		UserID:    "allowed_user",
		Content:   "hello",
		IsGroup:   false,
	}
	bp.EmitMessage(msg)

	select {
	case received := <-bp.Receive():
		if received.UserID != "allowed_user" {
			t.Error("expected allowed_user message")
		}
	default:
		t.Error("allowed user message should be emitted")
	}

	msg2 := Message{
		ID:        "test2",
		ChannelID: "dm2",
		UserID:    "blocked_user",
		Content:   "hello",
		IsGroup:   false,
	}
	bp.EmitMessage(msg2)

	select {
	case <-bp.Receive():
		t.Error("blocked user message should not be emitted")
	default:
	}
}
