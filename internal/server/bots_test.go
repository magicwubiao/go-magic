package server

import (
	"testing"

	"github.com/magicwubiao/go-magic/internal/bot"
)

// TestMaskEnv: botToResponse must never hand a bot's plaintext credentials to
// the dashboard, but the dashboard still needs to know which keys exist.
func TestMaskEnv(t *testing.T) {
	stored := map[string]string{"OPENAI_API_KEY": "sk-live-secret", "REGION": "cn-north"}
	masked := maskEnv(stored)

	if len(masked) != len(stored) {
		t.Fatalf("maskEnv dropped keys: got %d, want %d", len(masked), len(stored))
	}
	for k, v := range masked {
		if v != maskedEnvValue {
			t.Errorf("maskEnv[%q] = %q, want %q", k, v, maskedEnvValue)
		}
	}
	// The input map must not be modified in place: it is the live bot config.
	if stored["OPENAI_API_KEY"] != "sk-live-secret" {
		t.Error("maskEnv mutated the source config")
	}

	if got := maskEnv(nil); got != nil {
		t.Errorf("maskEnv(nil) = %v, want nil", got)
	}
}

// TestMergeMaskedEnv: a value that came back as the mask means "unchanged", so
// saving the dashboard form must not overwrite the real secret with "***".
func TestMergeMaskedEnv(t *testing.T) {
	stored := map[string]string{"API_KEY": "sk-live-secret", "REGION": "cn-north"}

	got := mergeMaskedEnv(stored, map[string]string{
		"API_KEY": maskedEnvValue, // untouched in the UI
		"REGION":  "us-east",      // edited
		"NEW_VAR": "value",        // added
	})
	if got["API_KEY"] != "sk-live-secret" {
		t.Errorf("untouched key lost its secret: %q", got["API_KEY"])
	}
	if got["REGION"] != "us-east" {
		t.Errorf("edited key not applied: %q", got["REGION"])
	}
	if got["NEW_VAR"] != "value" {
		t.Errorf("new key not applied: %q", got["NEW_VAR"])
	}

	// Deleting every line in the UI clears the env block.
	if got := mergeMaskedEnv(stored, map[string]string{}); got != nil {
		t.Errorf("empty incoming env should clear the block, got %v", got)
	}
	if got := mergeMaskedEnv(stored, nil); got != nil {
		t.Errorf("nil incoming env should clear the block, got %v", got)
	}

	// A mask for a key that does not exist yet cannot be resolved: the sentinel
	// is kept rather than inventing a value.
	got = mergeMaskedEnv(nil, map[string]string{"API_KEY": maskedEnvValue})
	if got["API_KEY"] != maskedEnvValue {
		t.Errorf("unresolvable mask = %q, want %q", got["API_KEY"], maskedEnvValue)
	}
}

// TestBotToResponseHidesEnv locks in the API contract: the serialized bot
// exposes env keys but not their values.
func TestBotToResponseHidesEnv(t *testing.T) {
	cfg := &bot.Config{
		Name: "researcher",
		Env:  map[string]string{"ANTHROPIC_API_KEY": "sk-ant-super-secret"},
	}
	resp := botToResponse(cfg, nil)

	env, ok := resp["env"].(map[string]string)
	if !ok {
		t.Fatalf("env field has unexpected type: %T", resp["env"])
	}
	if _, exists := env["ANTHROPIC_API_KEY"]; !exists {
		t.Fatal("env key missing from response; the dashboard cannot show which vars exist")
	}
	for k, v := range env {
		if v == "sk-ant-super-secret" {
			t.Fatalf("env[%q] leaked the plaintext secret", k)
		}
	}
}
