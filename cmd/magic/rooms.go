package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/bot"
)

var roomsCmd = &cobra.Command{
	Use:   "rooms",
	Short: "Manage Bot Mode group chat rooms (2-6 bots)",
	Long: `Group chat rooms let multiple bots collaborate on one shared conversation.

Each room has 2-6 member bots. When the human sends a message, members take
turns responding (up to 3 rounds), addressing each other with @mention tags.
A bot can stop the round by starting its reply with @user (escalation).

Quick start:
  magic rooms create <name> --members researcher,coder
  magic rooms send <room-id> "Review the plan" [--target researcher]
  magic rooms edit <room-id> --members researcher,coder,writer --rounds 2
  magic rooms list`,
}

func init() {
	rootCmd.AddCommand(roomsCmd)

	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a group chat room",
		Args:  cobra.ExactArgs(1),
		RunE:  runRoomsCreate,
	}
	createCmd.Flags().StringVar(&roomsFlagMembers, "members", "", "Comma-separated bot names (2-6)")
	createCmd.Flags().StringVar(&roomsFlagTopic, "topic", "", "Room topic")

	sendCmd := &cobra.Command{
		Use:   "send <room-id> <message>",
		Short: "Send a message to a room and wait for the round",
		Args:  cobra.MinimumNArgs(2),
		RunE:  runRoomsSend,
	}
	sendCmd.Flags().StringVar(&roomsFlagTarget, "target", "", "Bot mention tag to address first")

	editCmd := &cobra.Command{
		Use:   "edit <room-id>",
		Short: "Edit a room's name, topic, members or limits",
		Args:  cobra.ExactArgs(1),
		RunE:  runRoomsEdit,
	}
	editCmd.Flags().StringVar(&roomsFlagName, "name", "", "New room name")
	editCmd.Flags().StringVar(&roomsFlagTopic, "topic", "", "New room topic")
	editCmd.Flags().StringVar(&roomsFlagMembers, "members", "", "Comma-separated bot names (2-6); replaces the member list")
	editCmd.Flags().IntVar(&roomsFlagRounds, "rounds", 0, "Max speaking rounds per message (default 3)")
	editCmd.Flags().IntVar(&roomsFlagMaxMessages, "max-messages", 0, "Recent messages per bot context (default 10)")

	roomsCmd.AddCommand(createCmd)
	roomsCmd.AddCommand(sendCmd)
	roomsCmd.AddCommand(editCmd)
	roomsCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all group chat rooms",
		RunE:  runRoomsList,
	})
	roomsCmd.AddCommand(&cobra.Command{
		Use:   "show <room-id>",
		Short: "Show a room's config",
		Args:  cobra.ExactArgs(1),
		RunE:  runRoomsShow,
	})
	roomsCmd.AddCommand(&cobra.Command{
		Use:   "messages <room-id>",
		Short: "Show a room's recent messages",
		Args:  cobra.ExactArgs(1),
		RunE:  runRoomsMessages,
	})
	roomsCmd.AddCommand(&cobra.Command{
		Use:     "remove <room-id>",
		Aliases: []string{"delete", "rm"},
		Short:   "Delete a room",
		Args:    cobra.ExactArgs(1),
		RunE:    runRoomsRemove,
	})
}

var (
	roomsFlagMembers     string
	roomsFlagTopic       string
	roomsFlagTarget      string
	roomsFlagName        string
	roomsFlagRounds      int
	roomsFlagMaxMessages int
)

func newRoomsManager() (*bot.Manager, error) {
	cfg, err := loadConfigForBots()
	if err != nil {
		return nil, err
	}
	return bot.NewManager(cfg)
}

func runRoomsCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	var members []string
	if roomsFlagMembers != "" {
		for _, m := range strings.Split(roomsFlagMembers, ",") {
			if t := strings.TrimSpace(m); t != "" {
				members = append(members, t)
			}
		}
	}
	if len(members) < bot.MinRoomMembers {
		return fmt.Errorf("--members requires at least %d bots (e.g. --members researcher,coder)", bot.MinRoomMembers)
	}

	mgr, err := newRoomsManager()
	if err != nil {
		return err
	}
	if mgr == nil {
		return fmt.Errorf("bot manager unavailable")
	}
	if err := mgr.Start(context.Background()); err != nil {
		return err
	}
	defer mgr.Stop()

	room := &bot.RoomConfig{Name: name, Members: members, Topic: roomsFlagTopic}
	if err := mgr.CreateRoom(room); err != nil {
		return err
	}
	fmt.Printf("✅ Room %q created (%s)\n", room.Name, room.ID)
	fmt.Printf("   Members: @%s\n", strings.Join(room.Members, ", @"))
	fmt.Printf("   Send:    magic rooms send %s \"your message\"\n", room.ID)
	return nil
}

func runRoomsList(cmd *cobra.Command, args []string) error {
	mgr, err := newRoomsManager()
	if err != nil {
		return err
	}
	if mgr == nil {
		return fmt.Errorf("bot manager unavailable")
	}
	rooms, err := mgr.ListRooms()
	if err != nil {
		return err
	}
	if len(rooms) == 0 {
		fmt.Println("No group chat rooms yet.")
		fmt.Println("Create one: magic rooms create <name> --members bot1,bot2")
		return nil
	}
	fmt.Printf("%-12s %-24s %-12s %s\n", "ID", "NAME", "MEMBERS", "TOPIC")
	for _, r := range rooms {
		fmt.Printf("%-12s %-24s %-12d %s\n", r.ID, truncateStr(r.Name, 23), len(r.Members), r.Topic)
	}
	return nil
}

func runRoomsShow(cmd *cobra.Command, args []string) error {
	mgr, err := newRoomsManager()
	if err != nil {
		return err
	}
	if mgr == nil {
		return fmt.Errorf("bot manager unavailable")
	}
	room, err := mgr.GetRoom(args[0])
	if err != nil {
		return err
	}
	fmt.Printf("ID:      %s\n", room.ID)
	fmt.Printf("Name:    %s\n", room.Name)
	fmt.Printf("Topic:   %s\n", room.Topic)
	fmt.Printf("Members: @%s\n", strings.Join(room.Members, ", @"))
	fmt.Printf("Rounds:  %d\n", room.Rounds())
	fmt.Printf("Context: last %d messages\n", room.MessagesCap())
	return nil
}

func runRoomsMessages(cmd *cobra.Command, args []string) error {
	mgr, err := newRoomsManager()
	if err != nil {
		return err
	}
	if mgr == nil {
		return fmt.Errorf("bot manager unavailable")
	}
	msgs, err := mgr.RoomMessages(args[0])
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		fmt.Println("No messages yet.")
		return nil
	}
	for _, m := range msgs {
		fmt.Printf("[%s] @%s: %s\n", time.Unix(m.Timestamp, 0).Format("15:04:05"), m.From, m.Content)
	}
	return nil
}

// runRoomsEdit applies partial updates to a room. Only flags the caller
// actually passed are applied, so `--topic ""` genuinely clears the topic while
// omitting a flag leaves the stored value alone.
func runRoomsEdit(cmd *cobra.Command, args []string) error {
	if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("topic") &&
		!cmd.Flags().Changed("members") && !cmd.Flags().Changed("rounds") &&
		!cmd.Flags().Changed("max-messages") {
		return fmt.Errorf("nothing to change: pass at least one of --name/--topic/--members/--rounds/--max-messages")
	}

	mgr, err := newRoomsManager()
	if err != nil {
		return err
	}
	if mgr == nil {
		return fmt.Errorf("bot manager unavailable")
	}
	if err := mgr.Start(context.Background()); err != nil {
		return err
	}
	defer mgr.Stop()

	var members []string
	if cmd.Flags().Changed("members") {
		for _, m := range strings.Split(roomsFlagMembers, ",") {
			t := strings.TrimSpace(m)
			if t == "" {
				continue
			}
			if mgr.FindByTag(t) == nil {
				return fmt.Errorf("unknown bot %q; create it first with `magic bots create`", t)
			}
			members = append(members, t)
		}
		if len(members) < bot.MinRoomMembers {
			return fmt.Errorf("--members requires at least %d bots", bot.MinRoomMembers)
		}
	}

	room, err := mgr.UpdateRoom(args[0], func(r *bot.RoomConfig) {
		if cmd.Flags().Changed("name") && strings.TrimSpace(roomsFlagName) != "" {
			r.Name = strings.TrimSpace(roomsFlagName)
		}
		if cmd.Flags().Changed("topic") {
			r.Topic = roomsFlagTopic
		}
		if cmd.Flags().Changed("members") {
			r.Members = members
		}
		if cmd.Flags().Changed("rounds") {
			r.MaxRounds = roomsFlagRounds
		}
		if cmd.Flags().Changed("max-messages") {
			r.MaxMessages = roomsFlagMaxMessages
		}
	})
	if err != nil {
		return err
	}
	fmt.Printf("✅ Room %q updated (%s)\n", room.Name, room.ID)
	fmt.Printf("   Members: @%s\n", strings.Join(room.Members, ", @"))
	fmt.Printf("   Topic:   %s\n", room.Topic)
	fmt.Printf("   Rounds:  %d, context: last %d messages\n", room.Rounds(), room.MessagesCap())
	return nil
}

func runRoomsSend(cmd *cobra.Command, args []string) error {
	roomID := args[0]
	message := strings.Join(args[1:], " ")

	mgr, err := newRoomsManager()
	if err != nil {
		return err
	}
	if mgr == nil {
		return fmt.Errorf("bot manager unavailable")
	}
	if err := mgr.Start(context.Background()); err != nil {
		return err
	}
	defer mgr.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	res, err := mgr.SendToRoom(ctx, roomID, message, roomsFlagTarget)
	if err != nil {
		return err
	}
	// Print only messages added in this round: they carry the room's shared
	// log, so print the tail (most recent entries) with speaker labels.
	for _, m := range res.Messages {
		fmt.Printf("[%s] @%s: %s\n", time.Unix(m.Timestamp, 0).Format("15:04:05"), m.From, m.Content)
	}
	if res.NeedsUser {
		fmt.Println("\n⚠️  A bot escalated to @user — the round stopped early.")
	}
	return nil
}

func runRoomsRemove(cmd *cobra.Command, args []string) error {
	mgr, err := newRoomsManager()
	if err != nil {
		return err
	}
	if mgr == nil {
		return fmt.Errorf("bot manager unavailable")
	}
	if err := mgr.Start(context.Background()); err != nil {
		return err
	}
	defer mgr.Stop()
	if err := mgr.DeleteRoom(args[0]); err != nil {
		return err
	}
	fmt.Printf("🗑️  Room %q removed\n", args[0])
	return nil
}
