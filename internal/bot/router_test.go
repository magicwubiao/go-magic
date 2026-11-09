package bot

import "testing"

// TestRouteMessageScoresByTitleDescription: a message that clearly belongs to
// one bot's remit is routed to it; an unrelated one is left alone (returning
// "no owner" is a valid answer, not a failure).
func TestRouteMessageScoresByTitleDescription(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")
	if _, err := mgr.UpdateBot("alice", func(c *Config) {
		c.Title = "Research Analyst"
		c.Description = "academic papers literature review research"
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.UpdateBot("bob", func(c *Config) {
		c.Title = "Writing Coach"
		c.Description = "draft manuscript editing style"
	}); err != nil {
		t.Fatal(err)
	}

	decision, ok := mgr.RouteMessage("please review these academic papers for me")
	if !ok {
		t.Fatal("expected the research bot to claim a literature-review request")
	}
	if decision.Bot != "alice" {
		t.Fatalf("routed to %q, want alice (reason %q, score %.1f)", decision.Bot, decision.Reason, decision.Score)
	}
	if decision.Score < MinAutoRouteScore {
		t.Fatalf("score %.1f is below the routing threshold", decision.Score)
	}

	// One keyword hit is not enough: the general agent keeps the message.
	if _, ok := mgr.RouteMessage("academic"); ok {
		t.Fatal("a single weak keyword should not win the message")
	}
	if _, ok := mgr.RouteMessage("what's the weather like"); ok {
		t.Fatal("unrelated message should not be auto-routed")
	}
}

// TestRouteMessageRespectsRoomMembers: auto-routing for a room must never pick
// a bot that is not in it, no matter how well its description matches.
func TestRouteMessageRespectsRoomMembers(t *testing.T) {
	mgr := newTestManager(t, "alice", "bob")
	if _, err := mgr.UpdateBot("alice", func(c *Config) {
		c.Title = "Research Analyst"
		c.Description = "academic papers literature review research"
	}); err != nil {
		t.Fatal(err)
	}

	// A room that only contains bob: alice matches the text but is not a member.
	room := &RoomConfig{Name: "only-bob", Members: []string{"bob"}}
	if _, ok := mgr.RouteRoomOpeningSpeaker(room, "review these academic papers"); ok {
		t.Fatal("routed to a bot that is not a room member")
	}

	room.Members = []string{"alice", "bob"}
	if decision, ok := mgr.RouteRoomOpeningSpeaker(room, "review these academic papers"); !ok || decision.Bot != "alice" {
		t.Fatalf("room routing = %+v, %v; want alice", decision, ok)
	}
}

// TestRouteMessageIsDeterministic: equal scores must fall back to the name,
// not to Go's randomized map iteration order.
func TestRouteMessageIsDeterministic(t *testing.T) {
	mgr := newTestManager(t, "bob", "alice")
	for _, name := range []string{"alice", "bob"} {
		if _, err := mgr.UpdateBot(name, func(c *Config) {
			c.Title = "General Helper"
			c.Description = "budget planning spreadsheet report"
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		decision, ok := mgr.RouteMessage("help me with the budget planning spreadsheet report")
		if !ok {
			t.Fatal("expected a route decision")
		}
		if decision.Bot != "alice" {
			t.Fatalf("iteration %d routed to %q; ties must resolve to the first name", i, decision.Bot)
		}
	}
}

// TestRouteKeywordsSkipsStopwords makes sure the heuristic does not treat
// filler words as signal (otherwise every bot scores on every message).
func TestRouteKeywordsSkipsStopwords(t *testing.T) {
	kws := routeKeywords(&Config{
		Title:       "General Assistant",
		Description: "helps with tasks and work for you",
	})
	for _, bad := range []string{"the", "and", "for", "with", "you", "help", "task", "work"} {
		for _, kw := range kws {
			if kw == bad {
				t.Fatalf("stopword %q used as a routing keyword (%v)", bad, kws)
			}
		}
	}
}
