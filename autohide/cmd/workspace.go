package cmd

import (
	"fmt"
	"sort"
	"strconv"

	"autohide/config"

	"github.com/spf13/cobra"
)

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Manage per-workspace labels shown in the menu bar",
}

var workspaceSetCmd = &cobra.Command{
	Use:   "set <number> <label>",
	Short: "Set a label for a workspace",
	Long:  "Assign a label to a workspace number (1-based). The label shows in the menu bar when on that workspace.",
	Args:  cobra.ExactArgs(2),
	RunE:  runWorkspaceSet,
}

var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all workspace labels",
	RunE:  runWorkspaceList,
}

var workspaceClearCmd = &cobra.Command{
	Use:   "clear <number>",
	Short: "Remove the label for a workspace",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkspaceClear,
}

func init() {
	workspaceCmd.AddCommand(workspaceSetCmd)
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceClearCmd)
	rootCmd.AddCommand(workspaceCmd)
}

func runWorkspaceSet(cmd *cobra.Command, args []string) error {
	num, err := strconv.Atoi(args[0])
	if err != nil || num < 1 {
		return fmt.Errorf("invalid workspace number: %s (must be a positive integer)", args[0])
	}
	label := args[1]

	cfg, p := loadConfig()
	if cfg.Workspaces == nil {
		cfg.Workspaces = make(map[string]string)
	}
	cfg.Workspaces[strconv.Itoa(num)] = label

	if err := config.Save(cfg, p); err != nil {
		return err
	}
	fmt.Printf("Workspace %d = %q\n", num, label)
	return nil
}

func runWorkspaceList(cmd *cobra.Command, args []string) error {
	cfg, _ := loadConfig()

	wsMap := cfg.WorkspaceMap()
	if len(wsMap) == 0 {
		fmt.Println("No workspace labels configured.")
		fmt.Println("Use: autohide workspace set <number> <label>")
		return nil
	}

	// Sort by workspace number
	nums := make([]int, 0, len(wsMap))
	for n := range wsMap {
		nums = append(nums, n)
	}
	sort.Ints(nums)

	for _, n := range nums {
		fmt.Printf("  %d: %s\n", n, wsMap[n])
	}
	return nil
}

func runWorkspaceClear(cmd *cobra.Command, args []string) error {
	num, err := strconv.Atoi(args[0])
	if err != nil || num < 1 {
		return fmt.Errorf("invalid workspace number: %s (must be a positive integer)", args[0])
	}

	key := strconv.Itoa(num)
	cfg, p := loadConfig()
	if _, ok := cfg.Workspaces[key]; !ok {
		fmt.Printf("Workspace %d has no label.\n", num)
		return nil
	}

	delete(cfg.Workspaces, key)
	if err := config.Save(cfg, p); err != nil {
		return err
	}
	fmt.Printf("Cleared label for workspace %d.\n", num)
	return nil
}
