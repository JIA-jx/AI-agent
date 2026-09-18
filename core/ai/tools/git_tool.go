package tools

import (
	"basemodel/ai/einos"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type GitTool struct {
	Name        string
	Description string
}

type GitToolArgs struct {
	Command    string `json:"command"`
	Args       string `json:"args"`
	WorkingDir string `json:"working_dir"`
}

func NewGitTool() einos.InvokeParamTool {
	return &GitTool{
		Name:        "git_command",
		Description: "执行 Git 命令的工具，支持所有 Git 子命令及其参数。例如：git status, git add, git commit, git diff 等。",
	}
}

func (g *GitTool) Params() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"command": {
			Type:     schema.String,
			Desc:     "Git 命令，如 status, add, commit, diff, log, checkout 等",
			Required: true,
		},
		"args": {
			Type:     schema.String,
			Desc:     "命令参数，如 -A, -m 'message', --cached 等",
			Required: false,
		},
		"working_dir": {
			Type:     schema.String,
			Desc:     "工作目录，指定git命令执行的目录路径，默认为当前目录",
			Required: false,
		},
	}
}

func (g *GitTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: g.Name,
		Desc: g.Description,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"command": {
				Type:     schema.String,
				Desc:     "Git 命令，如 status, add, commit, diff, log, checkout 等",
				Required: true,
			},
			"args": {
				Type:     schema.String,
				Desc:     "命令参数，如 -A, -m 'message', --cached 等",
				Required: false,
			},
			"working_dir": {
				Type:     schema.String,
				Desc:     "工作目录，指定git命令执行的目录路径，默认为当前目录",
				Required: false,
			},
		}),
	}, nil
}

func (g *GitTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args GitToolArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("解析参数失败: %w", err)
	}

	cmdParts := []string{"git", args.Command}
	if args.Args != "" {
		cmdParts = append(cmdParts, strings.Fields(args.Args)...)
	}

	var cmd *exec.Cmd
	if args.WorkingDir != "" {
		cmd = exec.CommandContext(ctx, cmdParts[0], cmdParts[1:]...)
		cmd.Dir = args.WorkingDir
	} else {
		gitDir, err := findGitRoot()
		if err != nil {
			cmd = exec.CommandContext(ctx, cmdParts[0], cmdParts[1:]...)
		} else {
			cmd = exec.CommandContext(ctx, cmdParts[0], cmdParts[1:]...)
			cmd.Dir = gitDir
		}
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("执行 Git 命令失败: %s\n错误输出: %s", strings.Join(cmdParts, " "), string(output)), nil
	}

	return string(output), nil
}

func findGitRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err == nil {
		root := strings.TrimSpace(string(output))
		if root != "" {
			if info, err := os.Stat(root); err == nil && info.IsDir() { // 极端情况下的解析文件本身而非目录
				return root, nil
			}
		}
	}

	currentDir, err := os.Getwd() // 找绝对路径
	if err != nil {
		return "", fmt.Errorf("获取当前目录失败: %w", err)
	}
	dir := currentDir
	for {
		gitPath := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitPath); err == nil && info.IsDir() {
			return dir, nil
		}

		parentDir := filepath.Dir(dir) // 上级目录
		if parentDir == dir {
			break
		}
		dir = parentDir
	}

	return "", fmt.Errorf("未找到 Git 仓库（从 %s 向上查找）", currentDir)
}
