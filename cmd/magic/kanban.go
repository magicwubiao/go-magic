package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"

	"github.com/magicwubiao/go-magic/internal/kanban"
	"github.com/magicwubiao/go-magic/pkg/config"
)

var (
	kanbanStatus    string
	kanbanAssignee  string
	kanbanTenant    string
	kanbanPriority  int
	kanbanBody      string
	kanbanSummary   string
	kanbanReason    string
	kanbanParentID  string
	kanbanChildID   string
	kanbanWorkspace string
)

var kanbanCmd = &cobra.Command{
	Use:   "kanban",
	Short: "Kanban board management",
	Long:  "Manage kanban tasks for multi-agent collaboration",
}

var kanbanCreateCmd = &cobra.Command{
	Use:   "create <title>",
	Short: "Create a new task",
	Args:  cobra.ExactArgs(1),
	RunE:  runKanbanCreate,
}

var kanbanListCmd = &cobra.Command{
	Use:   "list",
	Short: "List tasks",
	RunE:  runKanbanList,
}

var kanbanShowCmd = &cobra.Command{
	Use:   "show <task_id>",
	Short: "Show task details",
	Args:  cobra.ExactArgs(1),
	RunE:  runKanbanShow,
}

var kanbanStartCmd = &cobra.Command{
	Use:   "start <task_id>",
	Short: "Start a task (triage/todo → ready)",
	Args:  cobra.ExactArgs(1),
	RunE:  runKanbanStart,
}

var kanbanClaimCmd = &cobra.Command{
	Use:   "claim <task_id>",
	Short: "Claim a task (ready → running)",
	Args:  cobra.ExactArgs(1),
	RunE:  runKanbanClaim,
}

var kanbanCompleteCmd = &cobra.Command{
	Use:   "complete <task_id>",
	Short: "Complete a task (running → done)",
	Args:  cobra.ExactArgs(1),
	RunE:  runKanbanComplete,
}

var kanbanBlockCmd = &cobra.Command{
	Use:   "block <task_id>",
	Short: "Block a task (running → blocked)",
	Args:  cobra.ExactArgs(1),
	RunE:  runKanbanBlock,
}

var kanbanUnblockCmd = &cobra.Command{
	Use:   "unblock <task_id>",
	Short: "Unblock a task (blocked → ready)",
	Args:  cobra.ExactArgs(1),
	RunE:  runKanbanUnblock,
}

var kanbanArchiveCmd = &cobra.Command{
	Use:   "archive <task_id>",
	Short: "Archive a task (done → archived)",
	Args:  cobra.ExactArgs(1),
	RunE:  runKanbanArchive,
}

var kanbanCommentCmd = &cobra.Command{
	Use:   "comment <task_id> <text>",
	Short: "Add a comment to a task",
	Args:  cobra.ExactArgs(2),
	RunE:  runKanbanComment,
}

var kanbanLinkCmd = &cobra.Command{
	Use:   "link <parent_id> <child_id>",
	Short: "Add a parent-child dependency",
	Args:  cobra.ExactArgs(2),
	RunE:  runKanbanLink,
}

var kanbanUnlinkCmd = &cobra.Command{
	Use:   "unlink <parent_id> <child_id>",
	Short: "Remove a parent-child dependency",
	Args:  cobra.ExactArgs(2),
	RunE:  runKanbanUnlink,
}

var kanbanBoardCmd = &cobra.Command{
	Use:   "board",
	Short: "Show kanban board view",
	RunE:  runKanbanBoard,
}

var kanbanStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show task statistics",
	RunE:  runKanbanStats,
}

var kanbanTriageCmd = &cobra.Command{
	Use:   "triage <task_id>",
	Short: "LLM-assisted task refinement",
	Args:  cobra.ExactArgs(1),
	RunE:  runKanbanTriage,
}

var kanbanDeleteCmd = &cobra.Command{
	Use:   "delete <task_id>",
	Short: "Delete a task",
	Args:  cobra.ExactArgs(1),
	RunE:  runKanbanDelete,
}

func init() {
	rootCmd.AddCommand(kanbanCmd)

	// Add subcommands
	kanbanCmd.AddCommand(kanbanCreateCmd)
	kanbanCmd.AddCommand(kanbanListCmd)
	kanbanCmd.AddCommand(kanbanShowCmd)
	kanbanCmd.AddCommand(kanbanStartCmd)
	kanbanCmd.AddCommand(kanbanClaimCmd)
	kanbanCmd.AddCommand(kanbanCompleteCmd)
	kanbanCmd.AddCommand(kanbanBlockCmd)
	kanbanCmd.AddCommand(kanbanUnblockCmd)
	kanbanCmd.AddCommand(kanbanArchiveCmd)
	kanbanCmd.AddCommand(kanbanCommentCmd)
	kanbanCmd.AddCommand(kanbanLinkCmd)
	kanbanCmd.AddCommand(kanbanUnlinkCmd)
	kanbanCmd.AddCommand(kanbanBoardCmd)
	kanbanCmd.AddCommand(kanbanStatsCmd)
	kanbanCmd.AddCommand(kanbanTriageCmd)
	kanbanCmd.AddCommand(kanbanDeleteCmd)

	// Flags for create
	kanbanCreateCmd.Flags().StringVarP(&kanbanAssignee, "assignee", "a", "", "Task assignee")
	kanbanCreateCmd.Flags().IntVarP(&kanbanPriority, "priority", "p", 0, "Priority (0=low, 1=medium, 2=high, 3=critical)")
	kanbanCreateCmd.Flags().StringVarP(&kanbanBody, "body", "b", "", "Task description")
	kanbanCreateCmd.Flags().StringVarP(&kanbanParentID, "parent", "", "", "Parent task ID")
	kanbanCreateCmd.Flags().StringVarP(&kanbanTenant, "tenant", "t", "", "Tenant/namespace")
	kanbanCreateCmd.Flags().StringVarP(&kanbanWorkspace, "workspace", "w", "", "Workspace (scratch|dir:/path|worktree)")

	// Flags for list
	kanbanListCmd.Flags().StringVarP(&kanbanStatus, "status", "s", "", "Filter by status")
	kanbanListCmd.Flags().StringVarP(&kanbanAssignee, "assignee", "a", "", "Filter by assignee")
	kanbanListCmd.Flags().StringVarP(&kanbanTenant, "tenant", "t", "", "Filter by tenant")

	// Flags for complete
	kanbanCompleteCmd.Flags().StringVarP(&kanbanSummary, "summary", "s", "", "Completion summary")

	// Flags for block
	kanbanBlockCmd.Flags().StringVarP(&kanbanReason, "reason", "r", "", "Reason for blocking")
}

func getKanbanManager() (*kanban.Manager, error) {
	home := config.GetMagicHome()

	mgr, err := kanban.NewManager(home)
	if err != nil {
		return nil, err
	}
	if err := mgr.Init(); err != nil {
		return nil, err
	}
	return mgr, nil
}

func runKanbanCreate(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	title := args[0]
	opts := []kanban.TaskOption{
		kanban.WithAssignee(kanbanAssignee),
		kanban.WithPriority(kanbanPriority),
	}
	if kanbanBody != "" {
		opts = append(opts, kanban.WithBody(kanbanBody))
	}
	if kanbanTenant != "" {
		opts = append(opts, kanban.WithTenant(kanbanTenant))
	}
	if kanbanWorkspace != "" {
		opts = append(opts, kanban.WithWorkspace(kanbanWorkspace))
	}

	var task *kanban.Task
	if kanbanParentID != "" {
		task, err = mgr.CreateTaskWithParent(title, kanbanBody, kanbanAssignee, kanbanParentID, opts...)
	} else {
		task, err = mgr.CreateTask(title, kanbanBody, kanbanAssignee, opts...)
	}

	if err != nil {
		return fmt.Errorf("failed to create task: %w", err)
	}

	fmt.Printf("✓ Task created: %s\n", task.ID)
	fmt.Printf("  Title: %s\n", task.Title)
	fmt.Printf("  Status: %s\n", task.Status)
	fmt.Printf("  Priority: %d\n", task.Priority)
	if task.Assignee != "" {
		fmt.Printf("  Assignee: %s\n", task.Assignee)
	}

	return nil
}

func runKanbanList(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	filter := kanban.TaskFilter{}
	if kanbanStatus != "" {
		filter.Status = []kanban.TaskStatus{parseKanbanStatus(kanbanStatus)}
	}
	if kanbanAssignee != "" {
		filter.Assignee = kanbanAssignee
	}
	if kanbanTenant != "" {
		filter.Tenant = kanbanTenant
	}

	tasks, err := mgr.ListTasks(filter)
	if err != nil {
		return fmt.Errorf("failed to list tasks: %w", err)
	}

	if len(tasks) == 0 {
		fmt.Println("No tasks found")
		return nil
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "Title", "Status", "Priority", "Assignee"})

	for _, task := range tasks {
		title := task.Title
		if len(title) > 40 {
			title = title[:37] + "..."
		}
		assignee := task.Assignee
		if assignee == "" {
			assignee = "-"
		}
		table.Append([]string{task.ID, title, string(task.Status), strconv.Itoa(task.Priority), assignee})
	}

	table.Render()
	fmt.Printf("\nTotal: %d tasks\n", len(tasks))

	return nil
}

func runKanbanShow(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	taskID := args[0]
	task, err := mgr.GetTask(taskID)
	if err != nil {
		return fmt.Errorf("task not found: %s", taskID)
	}

	fmt.Printf("╔══════════════════════════════════════════════════════════════╗\n")
	fmt.Printf("║ %-64s ║\n", task.ID)
	fmt.Printf("╠══════════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║ Title:    %-56s ║\n", task.Title)
	fmt.Printf("║ Status:   %-56s ║\n", task.Status)
	fmt.Printf("║ Priority: %-56s ║\n", strconv.Itoa(task.Priority))
	fmt.Printf("║ Assignee: %-56s ║\n", task.Assignee)
	fmt.Printf("║ Created:  %-56s ║\n", task.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("╚══════════════════════════════════════════════════════════════╝\n")

	if task.Body != "" {
		fmt.Printf("\n📝 Description:\n%s\n", task.Body)
	}

	// Show parents
	parents, _ := mgr.GetParents(taskID)
	if len(parents) > 0 {
		fmt.Printf("\n👆 Parents (%d):\n", len(parents))
		for _, p := range parents {
			fmt.Printf("  - %s [%s] %s\n", p.ID, p.Status, p.Title)
		}
	}

	// Show children
	children, _ := mgr.GetChildren(taskID)
	if len(children) > 0 {
		fmt.Printf("\n👇 Children (%d/%d done):\n", task.ChildDoneCount, task.ChildCount)
		for _, c := range children {
			fmt.Printf("  - %s [%s] %s\n", c.ID, c.Status, c.Title)
		}
	}

	// Show comments
	comments, _ := mgr.ListComments(taskID)
	if len(comments) > 0 {
		fmt.Printf("\n💬 Comments (%d):\n", len(comments))
		for _, c := range comments {
			fmt.Printf("  [%s] %s: %s\n", c.CreatedAt.Format("01-02 15:04"), c.Author, c.Body)
		}
	}

	return nil
}

func runKanbanStart(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	task, err := mgr.StartTask(args[0])
	if err != nil {
		return fmt.Errorf("failed to start task: %w", err)
	}

	fmt.Printf("✓ Task %s started (status: %s → ready)\n", task.ID, task.Status)
	return nil
}

func runKanbanClaim(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	// Use current user as assignee
	assignee := os.Getenv("USER")
	if assignee == "" {
		assignee = "cli"
	}

	task, err := mgr.ClaimTask(args[0], assignee)
	if err != nil {
		return fmt.Errorf("failed to claim task: %w", err)
	}

	fmt.Printf("✓ Task %s claimed by %s (status: ready → running)\n", task.ID, assignee)
	return nil
}

func runKanbanComplete(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	if kanbanSummary == "" {
		kanbanSummary = "Completed"
	}

	task, err := mgr.CompleteTask(args[0], kanbanSummary)
	if err != nil {
		return fmt.Errorf("failed to complete task: %w", err)
	}

	fmt.Printf("✓ Task %s completed\n", task.ID)
	return nil
}

func runKanbanBlock(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	if kanbanReason == "" {
		kanbanReason = "Blocked by user"
	}

	task, err := mgr.BlockTask(args[0], kanbanReason)
	if err != nil {
		return fmt.Errorf("failed to block task: %w", err)
	}

	fmt.Printf("✓ Task %s blocked: %s\n", task.ID, kanbanReason)
	return nil
}

func runKanbanUnblock(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	task, err := mgr.UnblockTask(args[0])
	if err != nil {
		return fmt.Errorf("failed to unblock task: %w", err)
	}

	fmt.Printf("✓ Task %s unblocked (status: blocked → ready)\n", task.ID)
	return nil
}

func runKanbanArchive(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	task, err := mgr.ArchiveTask(args[0])
	if err != nil {
		return fmt.Errorf("failed to archive task: %w", err)
	}

	fmt.Printf("✓ Task %s archived\n", task.ID)
	return nil
}

func runKanbanComment(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	author := os.Getenv("USER")
	if author == "" {
		author = "cli"
	}

	comment, err := mgr.AddComment(args[0], author, args[1])
	if err != nil {
		return fmt.Errorf("failed to add comment: %w", err)
	}

	fmt.Printf("✓ Comment added to task %s\n", args[0])
	fmt.Printf("  [%s] %s: %s\n", comment.CreatedAt.Format("01-02 15:04"), author, args[1])
	return nil
}

func runKanbanLink(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	if err := mgr.AddLink(args[0], args[1]); err != nil {
		return fmt.Errorf("failed to link tasks: %w", err)
	}

	fmt.Printf("✓ Linked %s → %s\n", args[0], args[1])
	return nil
}

func runKanbanUnlink(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	if err := mgr.RemoveLink(args[0], args[1]); err != nil {
		return fmt.Errorf("failed to unlink tasks: %w", err)
	}

	fmt.Printf("✓ Unlinked %s → %s\n", args[0], args[1])
	return nil
}

func runKanbanBoard(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	board, err := mgr.GetBoard(kanbanTenant)
	if err != nil {
		return fmt.Errorf("failed to get board: %w", err)
	}

	statuses := []kanban.TaskStatus{
		kanban.StatusTriage,
		kanban.StatusTodo,
		kanban.StatusReady,
		kanban.StatusRunning,
		kanban.StatusBlocked,
		kanban.StatusDone,
	}

	statusLabels := map[kanban.TaskStatus]string{
		kanban.StatusTriage:  "🔍 Triage",
		kanban.StatusTodo:    "📋 Todo",
		kanban.StatusReady:   "✅ Ready",
		kanban.StatusRunning: "🔄 Running",
		kanban.StatusBlocked: "🚫 Blocked",
		kanban.StatusDone:    "🎉 Done",
	}

	fmt.Println("┌─────────────────────────────────────────────────────────────────────┐")
	fmt.Println("│                         📊 Kanban Board                              │")
	fmt.Println("└─────────────────────────────────────────────────────────────────────┘")

	for _, status := range statuses {
		tasks := board[status]
		label := statusLabels[status]
		fmt.Printf("\n%s (%d tasks)\n", label, len(tasks))
		fmt.Println(strings.Repeat("─", 70))

		if len(tasks) == 0 {
			fmt.Println("  (empty)")
			continue
		}

		for _, task := range tasks {
			priority := ""
			switch task.Priority {
			case 3:
				priority = "🔴"
			case 2:
				priority = "🟠"
			case 1:
				priority = "🟡"
			default:
				priority = "⚪"
			}

			title := task.Title
			if len(title) > 50 {
				title = title[:47] + "..."
			}

			assignee := ""
			if task.Assignee != "" {
				assignee = fmt.Sprintf(" @%s", task.Assignee)
			}

			fmt.Printf("  %s %s [%s]%s\n", priority, task.ID, title, assignee)
		}
	}

	return nil
}

func runKanbanStats(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	stats, err := mgr.GetStats(kanbanTenant)
	if err != nil {
		return fmt.Errorf("failed to get stats: %w", err)
	}

	fmt.Println("📊 Task Statistics")
	fmt.Println("══════════════════════════════════════")

	statusLabels := map[kanban.TaskStatus]string{
		kanban.StatusTriage:   "🔍 Triage",
		kanban.StatusTodo:     "📋 Todo",
		kanban.StatusReady:    "✅ Ready",
		kanban.StatusRunning:  "🔄 Running",
		kanban.StatusBlocked:  "🚫 Blocked",
		kanban.StatusDone:     "🎉 Done",
		kanban.StatusArchived: "📦 Archived",
	}

	total := 0
	for _, status := range []kanban.TaskStatus{
		kanban.StatusTriage, kanban.StatusTodo, kanban.StatusReady,
		kanban.StatusRunning, kanban.StatusBlocked, kanban.StatusDone,
	} {
		count := stats[status]
		total += count
		label := statusLabels[status]
		fmt.Printf("  %-15s : %d\n", label, count)
	}

	fmt.Println("──────────────────────────────────────")
	fmt.Printf("  %-15s : %d\n", "Total (active)", total)
	fmt.Printf("  %-15s : %d\n", "Archived", stats[kanban.StatusArchived])

	return nil
}

func runKanbanTriage(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	fmt.Println("🔍 Triage task with LLM...")
	fmt.Println("Note: Triage requires a provider configuration. Skipping LLM call in CLI mode.")

	task, err := mgr.GetTask(args[0])
	if err != nil {
		return fmt.Errorf("task not found: %s", args[0])
	}

	fmt.Printf("\nTask: %s\n", task.ID)
	fmt.Printf("Current Status: %s\n", task.Status)
	fmt.Printf("Title: %s\n", task.Title)
	fmt.Printf("Body: %s\n", task.Body)

	return nil
}

func runKanbanDelete(cmd *cobra.Command, args []string) error {
	mgr, err := getKanbanManager()
	if err != nil {
		return err
	}
	defer mgr.Close()

	if err := mgr.DeleteTask(args[0]); err != nil {
		return fmt.Errorf("failed to delete task: %w", err)
	}

	fmt.Printf("✓ Task %s deleted\n", args[0])
	return nil
}

func parseKanbanStatus(s string) kanban.TaskStatus {
	switch strings.ToLower(s) {
	case "triage":
		return kanban.StatusTriage
	case "todo":
		return kanban.StatusTodo
	case "ready":
		return kanban.StatusReady
	case "running":
		return kanban.StatusRunning
	case "blocked":
		return kanban.StatusBlocked
	case "done":
		return kanban.StatusDone
	case "archived":
		return kanban.StatusArchived
	default:
		return kanban.StatusTriage
	}
}
