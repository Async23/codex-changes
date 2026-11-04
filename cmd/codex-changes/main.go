package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	"github.com/Async23/codex-changes/internal/changes"
	"github.com/Async23/codex-changes/internal/codex"
	"github.com/Async23/codex-changes/internal/preferences"
	"github.com/Async23/codex-changes/internal/tui"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "codex-changes:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	check := len(args) > 0 && args[0] == "check"
	if check {
		args = args[1:]
	}
	flags := flag.NewFlagSet("codex-changes", flag.ContinueOnError)
	flags.SetOutput(out)
	var opts tui.Options
	var noState, showVersion bool
	flags.StringVar(&opts.Binary, "codex-bin", "codex", "Codex CLI 可执行文件")
	flags.StringVar(&opts.Thread, "thread", "", "直接打开指定会话 ID")
	flags.StringVar(&opts.File, "file", "", "直接选中完整路径或文件名")
	flags.BoolVar(&opts.Demo, "demo", false, "使用合成演示数据，无需安装 Codex")
	flags.BoolVar(&opts.Children, "children", true, "汇总子会话的修改记录")
	flags.BoolVar(&opts.Archived, "archived", false, "会话列表同时显示已归档会话")
	flags.BoolVar(&noState, "no-state", false, "不读取或保存界面偏好")
	flags.BoolVar(&showVersion, "version", false, "显示版本")
	flags.Usage = func() {
		fmt.Fprintln(out, "Codex Changes — 按文件查看 Codex 会话修改，无需 Git。\n\n用法：\n  codex-changes [选项]\n  codex-changes check --thread ID [--file 路径]\n\n选项：")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("未知参数 %q；使用 --help 查看用法", flags.Arg(0))
	}
	if showVersion {
		label := version
		if label == "dev" {
			if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
				label = info.Main.Version
			}
		}
		fmt.Fprintln(out, "codex-changes "+label)
		return nil
	}
	if opts.Demo && opts.Thread == "" {
		opts.Thread = "demo-camera"
	}
	if check {
		return checkHistory(ctx, opts, out)
	}
	opts.Preferences = preferences.Defaults()
	if !noState && !opts.Demo {
		var err error
		opts.StatePath, err = preferences.Path()
		if err != nil {
			return err
		}
		opts.Preferences, err = preferences.Load(opts.StatePath)
		if err != nil {
			return fmt.Errorf("界面偏好读取失败（可用 --no-state 启动）：%w", err)
		}
	}
	return tui.Run(ctx, opts)
}

func checkHistory(ctx context.Context, opts tui.Options, out io.Writer) error {
	if opts.Thread == "" {
		return fmt.Errorf("check 需要 --thread ID")
	}
	var reader changes.Reader
	if opts.Demo {
		reader = codex.NewDemo()
	} else {
		client, err := codex.Start(ctx, opts.Binary)
		if err != nil {
			return err
		}
		defer client.Close()
		reader = client
	}
	var result changes.Progress
	changes.Scan(ctx, reader, opts.Thread, opts.Children, func(progress changes.Progress) {
		if progress.Done {
			result = progress
		}
	})
	if result.Err != nil {
		return result.Err
	}
	output := struct {
		ThreadID string   `json:"threadId"`
		Files    int      `json:"files"`
		Edits    int      `json:"edits"`
		Turns    int      `json:"turns"`
		Children int      `json:"children"`
		Ignored  int      `json:"ignored"`
		Warnings []string `json:"warnings,omitempty"`
		File     *struct {
			Path    string `json:"path"`
			Edits   int    `json:"edits"`
			Added   int    `json:"added"`
			Removed int    `json:"removed"`
		} `json:"selectedFile,omitempty"`
	}{ThreadID: result.Session.Thread.ID, Files: len(result.Session.Files), Edits: result.Session.EditCount(), Turns: result.Session.TurnsRead, Children: result.Session.Children, Ignored: result.Session.Ignored, Warnings: result.Session.Warnings}
	if opts.File != "" {
		for _, file := range result.Session.Files {
			if file.Path == opts.File || filepath.Base(file.Path) == opts.File {
				output.File = &struct {
					Path    string `json:"path"`
					Edits   int    `json:"edits"`
					Added   int    `json:"added"`
					Removed int    `json:"removed"`
				}{file.Path, len(file.Edits), file.Added, file.Removed}
				break
			}
		}
		if output.File == nil {
			return fmt.Errorf("会话中找不到指定文件 %q", opts.File)
		}
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}
