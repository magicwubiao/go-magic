package bot

import (
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{"researcher", "research-bot", "bot_2", "A1", "x"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("expected %q to be valid, got: %v", name, err)
		}
	}

	invalid := []string{"", "-lead", "has space", "has/slash", "default", "list", "new", strings.Repeat("a", 65)}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Errorf("expected %q to be rejected", name)
		}
	}
}

func TestMentionTag(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"researcher", "researcher"},
		{"Research_Bot", "research-bot"},
		{"My-Bot-2", "my-bot-2"},
	}
	for _, tt := range tests {
		c := &Config{Name: tt.name}
		if got := c.MentionTag(); got != tt.want {
			t.Errorf("MentionTag(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestParseBotMention(t *testing.T) {
	resolve := func(tag string) bool {
		return tag == "researcher" || tag == "writer"
	}

	// Slash form always matches.
	name, text, ok := ParseBotMention("/bot researcher find papers", resolve)
	if !ok || name != "researcher" || text != "find papers" {
		t.Errorf("slash form failed: (%q, %q, %v)", name, text, ok)
	}

	// Slash form with unknown bot still matches (caller reports error).
	name, _, ok = ParseBotMention("/bot ghost hello", resolve)
	if !ok || name != "ghost" {
		t.Errorf("slash form with unknown bot should match with name: (%q, %v)", name, ok)
	}

	// Mention form matches only known tags.
	name, text, ok = ParseBotMention("@writer draft the intro please", resolve)
	if !ok || name != "writer" || text != "draft the intro please" {
		t.Errorf("mention form failed: (%q, %q, %v)", name, text, ok)
	}

	// Unknown mention does not hijack.
	if _, _, ok := ParseBotMention("@unknownuser hi there", resolve); ok {
		t.Error("unknown mention should not match")
	}

	// @user is reserved for humans.
	if _, _, ok := ParseBotMention("@user look at this", resolve); ok {
		t.Error("@user must not route to a bot")
	}

	// Plain message untouched.
	if _, _, ok := ParseBotMention("hello world", resolve); ok {
		t.Error("plain message should not match")
	}

	// /bots alias works.
	if _, _, ok := ParseBotMention("/bots writer hi", resolve); !ok {
		t.Error("/bots alias should work")
	}
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	cfg := &Config{
		Name:         "test_bot",
		Title:        "Test Bot",
		Description:  "round trip",
		SystemPrompt: "Be terse.",
		Model:        "gpt-4o-mini",
		CreatedAt:    12345,
	}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Load("test_bot")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Title != "Test Bot" || got.Model != "gpt-4o-mini" || got.SystemPrompt != "Be terse." {
		t.Errorf("round trip mismatch: %+v", got)
	}

	list, err := store.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %d bots, err=%v", len(list), err)
	}

	if err := store.Delete("test_bot"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, _ = store.List()
	if len(list) != 0 {
		t.Errorf("expected empty list after delete, got %d", len(list))
	}
}

func TestRoutinesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	// Empty by default.
	routines, err := store.LoadRoutines("b")
	if err != nil || len(routines) != 0 {
		t.Fatalf("expected empty routines, got %d, err=%v", len(routines), err)
	}

	routines = []*RoutineConfig{
		{ID: NewRoutineID("b"), Name: "digest", Schedule: "0 9 * * *", Prompt: "summarize", Enabled: true},
	}
	if err := store.SaveRoutines("b", routines); err != nil {
		t.Fatalf("SaveRoutines: %v", err)
	}
	got, err := store.LoadRoutines("b")
	if err != nil || len(got) != 1 || got[0].Name != "digest" {
		t.Errorf("routines round trip failed: %+v, err=%v", got, err)
	}
}

func TestEffectiveSystemPrompt(t *testing.T) {
	c := &Config{Name: "scout", Title: "Scout", Description: "Finds things"}
	prompt := c.EffectiveSystemPrompt()
	if !strings.Contains(prompt, "Scout") || !strings.Contains(prompt, "Finds things") {
		t.Errorf("prompt missing role info: %q", prompt)
	}

	// With persona instructions.
	c.SystemPrompt = "Always answer in bullets."
	prompt = c.EffectiveSystemPrompt()
	if !strings.Contains(prompt, "bullets") {
		t.Errorf("prompt missing persona: %q", prompt)
	}
}
