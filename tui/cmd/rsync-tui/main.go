// Command rsync-tui is the terminal front-end of rsync-gui.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"

	"github.com/lunatickochiya/rsync-gui/tui/internal/config"
	"github.com/lunatickochiya/rsync-gui/tui/internal/rsync"
	"github.com/lunatickochiya/rsync-gui/tui/internal/tui"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usageText = `rsync-tui - 终端里的 rsync 传输面板

用法：
  rsync-tui [选项] [源 ...]

选项：
  -d, --destination 路径   目标目录（本地、user@host:/path 或 sftp://host/path）
  -p, --preset 名称        使用预设（ID 或 JSON 文件路径）
  -c, --config 文件        载入 rsync-gui 设置文件
      --run                填好后立即开始（删除文件时仍会确认）
      --print-command      只输出 rsync 命令，不启动界面
      --list-presets       列出可用预设
  -V, --version            显示版本
  -h, --help               显示帮助

示例：
  rsync-tui ~/Photos -d /mnt/d/backup
  rsync-tui --preset rsync-copy-verify ~/Photos -d nas:/volume1/photos
  rsync-tui --config ~/rsync-mirror.json -d /mnt/d/backup ~/src
  rsync-tui --print-command ~/Photos -d /mnt/d/backup
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return
		}
		fmt.Fprintf(os.Stderr, "rsync-tui: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr *os.File) error {
	fs := pflag.NewFlagSet("rsync-tui", pflag.ContinueOnError)
	fs.SetInterspersed(true)
	fs.SetOutput(stderr)
	fs.SortFlags = false

	var (
		destination  string
		presetID     string
		configPath   string
		autoRun      bool
		printCommand bool
		listPresets  bool
		showVersion  bool
		showHelp     bool
	)
	fs.StringVarP(&destination, "destination", "d", "", "目标目录")
	fs.StringVarP(&presetID, "preset", "p", "", "预设名称或文件")
	fs.StringVarP(&configPath, "config", "c", "", "设置文件")
	fs.BoolVar(&autoRun, "run", false, "立即开始")
	fs.BoolVar(&printCommand, "print-command", false, "只输出命令")
	fs.BoolVar(&listPresets, "list-presets", false, "列出预设")
	fs.BoolVarP(&showVersion, "version", "V", false, "显示版本")
	fs.BoolVarP(&showHelp, "help", "h", false, "显示帮助")
	fs.Usage = func() { fmt.Fprint(stderr, usageText) }

	if err := fs.Parse(args); err != nil {
		return err
	}
	if showHelp {
		fs.Usage()
		return nil
	}
	if showVersion {
		fmt.Fprintf(stdout, "rsync-tui %s\n", version)
		return nil
	}
	if listPresets {
		return printPresets(stdout)
	}
	cfg := config.LoadConfig()
	opts := cfg.Options

	// A preset sets the transfer mode; an explicit settings file can then
	// override individual keys while missing keys keep the preset values.
	if presetID != "" {
		preset, ok := config.FindPreset(presetID)
		if !ok {
			return fmt.Errorf("找不到预设 %q（用 --list-presets 查看）", presetID)
		}
		if preset.Error != "" {
			return fmt.Errorf("预设 %q 无效：%s", presetID, preset.Error)
		}
		opts = config.MergePreset(opts, preset.Options)
	}
	if configPath != "" {
		loaded, _, err := config.LoadSettingsFile(configPath, opts)
		if err != nil {
			return err
		}
		opts = loaded
	}

	sources := append([]string(nil), fs.Args()...)
	if len(sources) == 0 {
		sources = append([]string(nil), cfg.Sources...)
	}
	if destination == "" {
		destination = cfg.Destination
	}

	if printCommand {
		return printPlan(stdout, opts, sources, destination)
	}

	model := tui.New(opts, sources, destination, cfg, presetID, autoRun)
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("界面退出：%w", err)
	}
	return nil
}

func printPresets(stdout *os.File) error {
	for _, preset := range config.AvailablePresets() {
		source := "内置"
		if !preset.Builtin {
			source = preset.Path
		}
		label := preset.Label
		if preset.Error != "" {
			label += "（无效：" + preset.Error + "）"
		}
		fmt.Fprintf(stdout, "%-24s %s\t%s\n", preset.ID, label, source)
	}
	return nil
}

func printPlan(stdout *os.File, opts config.Options, sources []string, destination string) error {
	plan, err := rsync.PreviewPlan(opts, sources, destination)
	if err != nil {
		return err
	}
	defer plan.Cleanup()

	var lines []string
	for _, transfer := range plan.Transfers {
		cmd := plan.CommandFor(transfer, rsync.PhaseCopy)
		lines = append(lines, config.JoinCommand(cmd[0], cmd[1:]))
	}
	if len(lines) == 0 {
		return fmt.Errorf("没有可执行的传输")
	}
	fmt.Fprintln(stdout, strings.Join(lines, "\n"))
	return nil
}
