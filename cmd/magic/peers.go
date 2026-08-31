package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/peer"
)

var peersFlagToken string // optional shared relay secret for the peer being added

var peersCmd = &cobra.Command{
	Use:   "peer",
	Short: "Manage cross-machine Bot Mode peers (relay DMs)",
	Long: `Peers are remote go-magic instances that can DM this machine's bots
(and vice versa) over the relay endpoint.

Each machine keeps a stable instance id (<magicHome>/instance_id) and a table
of known peers (<magicHome>/peers.json). A DM blocks until the remote bot
finishes its turn, so a slow bot means a slow command.

Quick start (machine A -> machine B):
  # on B:  magic server (or magic gateway)  # relay endpoint listens on :8642
  # on A:  magic peer add b http://192.168.1.20:8642
  # on A:  magic peer dm b researcher "What's the status of the report?"

Security: if the remote has bot_mode.relay_token set, add it here with --token
so outgoing DMs are authenticated.`,
}

func init() {
	rootCmd.AddCommand(peersCmd)

	addCmd := &cobra.Command{
		Use:   "add <name> <base-url>",
		Short: "Register a remote go-magic instance",
		Args:  cobra.ExactArgs(2),
		RunE:  runPeersAdd,
	}
	addCmd.Flags().StringVar(&peersFlagToken, "token", "", "Shared relay secret required by the remote (bot_mode.relay_token)")
	peersCmd.AddCommand(addCmd)

	peersCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List registered peers",
		RunE:  runPeersList,
	})
	peersCmd.AddCommand(&cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a registered peer",
		Args:    cobra.ExactArgs(1),
		RunE:    runPeersRemove,
	})
	peersCmd.AddCommand(&cobra.Command{
		Use:   "dm <peer> <bot> <message>",
		Short: "DM a bot on a remote machine and wait for its reply",
		Args:  cobra.MinimumNArgs(3),
		RunE:  runPeersDM,
	})
}

func newPeerStore() (*peer.Store, error) {
	return peer.NewStore(peer.DefaultMagicHome())
}

func runPeersAdd(cmd *cobra.Command, args []string) error {
	name, baseURL := args[0], args[1]
	store, err := newPeerStore()
	if err != nil {
		return err
	}
	p := &peer.Peer{Name: name, BaseURL: baseURL, Token: peersFlagToken}
	if err := store.Add(p); err != nil {
		return err
	}
	note := ""
	if p.Token != "" {
		note = " (token-protected)"
	}
	fmt.Printf("🤝 Peer %q registered -> %s%s\n", p.Name, p.BaseURL, note)
	fmt.Println("   Now DM its bots: magic peer dm " + p.Name + " <bot> \"message\"")
	return nil
}

func runPeersList(cmd *cobra.Command, args []string) error {
	store, err := newPeerStore()
	if err != nil {
		return err
	}
	list := store.List()
	if len(list) == 0 {
		fmt.Println("No peers registered. Add one with: magic peer add <name> <base-url>")
		return nil
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	fmt.Printf("%-20s %-40s %s\n", "NAME", "BASE URL", "CREATED")
	for _, p := range list {
		created := time.Unix(p.CreatedAt, 0).Format("2006-01-02 15:04")
		auth := "open"
		if p.Token != "" {
			auth = "token"
		}
		fmt.Printf("%-20s %-40s %s (%s)\n", p.Name, p.BaseURL, created, auth)
	}
	return nil
}

func runPeersRemove(cmd *cobra.Command, args []string) error {
	name := args[0]
	store, err := newPeerStore()
	if err != nil {
		return err
	}
	if err := store.Remove(name); err != nil {
		return err
	}
	fmt.Printf("🗑  Peer %q removed\n", name)
	return nil
}

func runPeersDM(cmd *cobra.Command, args []string) error {
	peerName, botName := args[0], args[1]
	message := strings.Join(args[2:], " ")

	store, err := newPeerStore()
	if err != nil {
		return err
	}
	p, ok := store.Get(peerName)
	if !ok {
		return fmt.Errorf("peer %q not found (add it first: magic peer add %s <base-url>)", peerName, peerName)
	}

	instanceID, err := peer.InstanceID(peer.DefaultMagicHome())
	if err != nil {
		return fmt.Errorf("resolve local instance id: %w", err)
	}

	fmt.Printf("📡 Relaying to %s (%s) -> bot %q ...\n", p.Name, p.BaseURL, botName)
	reply, err := peer.NewClient().SendDM(context.Background(), p, instanceID, "cli", botName, message)
	if err != nil {
		return err
	}
	fmt.Println("")
	fmt.Println(reply)
	return nil
}
