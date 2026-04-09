package cmd

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"autohide/config"
	"autohide/daemon"

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

var workspaceCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the current workspace",
	RunE:  runWorkspaceCurrent,
}

var workspaceNameCmd = &cobra.Command{
	Use:   "name <label>",
	Short: "Name the current workspace",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkspaceName,
}

var workspaceSwitchCmd = &cobra.Command{
	Use:   "switch [workspace number or label]",
	Short: "Switch to another workspace",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceSwitch,
}

var workspaceSwitchFuzzy bool

func init() {
	workspaceCmd.AddCommand(workspaceSetCmd)
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceClearCmd)
	workspaceCmd.AddCommand(workspaceCurrentCmd)
	workspaceCmd.AddCommand(workspaceNameCmd)
	workspaceCmd.AddCommand(workspaceSwitchCmd)
	workspaceSwitchCmd.Flags().BoolVar(&workspaceSwitchFuzzy, "fuzzy", false, "open the fuzzy workspace picker")
	rootCmd.AddCommand(workspaceCmd)
}

func runWorkspaceSet(cmd *cobra.Command, args []string) error {
	num, err := strconv.Atoi(args[0])
	if err != nil || num < 1 {
		return fmt.Errorf("invalid workspace number: %s (must be a positive integer)", args[0])
	}
	label := daemon.NormalizeWorkspaceLabel(args[1])
	if label == "" {
		return fmt.Errorf("label cannot be empty")
	}

	if err := saveWorkspaceLabel(num, label); err != nil {
		return err
	}
	fmt.Printf("Workspace %d = %q\n", num, label)
	return nil
}

func runWorkspaceList(cmd *cobra.Command, args []string) error {
	cfg, _ := loadConfig()
	if entries, current, err := daemon.ListWorkspaceEntries(cfg); err == nil {
		for _, entry := range entries {
			marker := " "
			if entry.Number == current {
				marker = "*"
			}

			text := fmt.Sprintf("Workspace %d", entry.Number)
			if entry.Label != "" {
				text = fmt.Sprintf("%s: %s", text, entry.Label)
			} else {
				text += " (no label)"
			}
			fmt.Printf("%s %s\n", marker, text)
		}
		return nil
	}

	wsMap := cfg.WorkspaceMap()
	if len(wsMap) == 0 {
		fmt.Println("No workspace labels configured.")
		fmt.Println("Use: autohide workspace set <number> <label> or autohide workspace name <label>")
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

func runWorkspaceCurrent(cmd *cobra.Command, args []string) error {
	cfg, _ := loadConfig()
	current, total, err := daemon.GetWorkspaceInfo()
	if err != nil {
		return err
	}

	label := cfg.WorkspaceLabel(current)
	if label == "" {
		fmt.Printf("Workspace %d of %d (no label)\n", current, total)
		return nil
	}

	fmt.Printf("Workspace %d of %d: %s\n", current, total, label)
	return nil
}

func runWorkspaceName(cmd *cobra.Command, args []string) error {
	label := daemon.NormalizeWorkspaceLabel(args[0])
	if label == "" {
		return fmt.Errorf("label cannot be empty")
	}

	current, err := daemon.GetCurrentWorkspaceNumber()
	if err != nil {
		return err
	}

	if err := saveWorkspaceLabel(current, label); err != nil {
		return err
	}

	fmt.Printf("Workspace %d = %q\n", current, label)
	return nil
}

func runWorkspaceSwitch(cmd *cobra.Command, args []string) error {
	cfg, _ := loadConfig()

	var (
		target int
		err    error
	)

	if workspaceSwitchFuzzy || len(args) == 0 {
		target, err = daemon.PickWorkspace(cfg, "Switch Workspace")
		if err != nil {
			if err == daemon.ErrWorkspacePickerCanceled {
				return nil
			}
			return err
		}
	} else {
		target, err = resolveWorkspaceTarget(cfg, args[0])
		if err != nil {
			return err
		}
	}

	current, _, err := daemon.GetWorkspaceInfo()
	if err != nil {
		return err
	}
	if target == current {
		fmt.Printf("Already on workspace %d.\n", target)
		return nil
	}

	if err := daemon.SwitchToWorkspace(target); err != nil {
		return err
	}

	fmt.Printf("Switched to workspace %d.\n", target)
	return nil
}

func saveWorkspaceLabel(num int, label string) error {
	cfg, p := loadConfig()
	if cfg.Workspaces == nil {
		cfg.Workspaces = make(map[string]string)
	}
	cfg.Workspaces[strconv.Itoa(num)] = label
	return config.Save(cfg, p)
}

func resolveWorkspaceTarget(cfg *config.Config, raw string) (int, error) {
	if num, err := strconv.Atoi(raw); err == nil && num > 0 {
		return num, nil
	}

	target := daemon.NormalizeWorkspaceLabel(raw)
	if target == "" {
		return 0, fmt.Errorf("workspace target cannot be empty")
	}

	lower := strings.ToLower(target)
	for num, label := range cfg.WorkspaceMap() {
		if strings.ToLower(daemon.NormalizeWorkspaceLabel(label)) == lower {
			return num, nil
		}
	}

	return 0, fmt.Errorf("no workspace matches %q", raw)
}
