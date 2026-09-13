package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"thunder/ai/einos"
)

type GitCommitTool struct {
	Name        string
	Description string
}

func NewGitCommitTool() einos.InvokeParamTool {
	return &GitCommitTool{
		Name:        "git_commit_workflow",
		Description: "Git 提交流程专用工具，支持状态检查、差异查看、文件添加、提交和提交信息验证等操作。",
	}
}

type GitCommitToolArgs struct {
	Action     string `json:"action"`  // status, diff, add, commit, validate
	Message    string `json:"message"` // 提交信息
	Files      string `json:"files"`   // 指定文件
	WorkingDir string `json:"working_dir"`
}

func (g *GitCommitTool) Params() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"action": {
			Type:     schema.String,
			Desc:     "操作类型: status(获取状态), diff(查看差异), add(添加文件), commit(提交), validate(验证提交信息)",
			Required: true,
		},
		"message": {
			Type:     schema.String,
			Desc:     "提交信息（仅在 commit 操作时使用）",
			Required: false,
		},
		"files": {
			Type:     schema.String,
			Desc:     "指定文件路径（仅在 add 操作时使用）",
			Required: false,
		},
		"working_dir": {
			Type:     schema.String,
			Desc:     "工作目录，指定git命令执行的目录路径，默认为当前目录",
			Required: false,
		},
	}
}

func (g *GitCommitTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: g.Name,
		Desc: g.Description,
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action": {
				Type:     schema.String,
				Desc:     "操作类型: status(获取状态), diff(查看差异), add(添加文件), commit(提交), validate(验证提交信息)",
				Required: true,
			},
			"message": {
				Type:     schema.String,
				Desc:     "提交信息（仅在 commit 操作时使用）",
				Required: false,
			},
			"files": {
				Type:     schema.String,
				Desc:     "指定文件路径（仅在 add 操作时使用）",
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

func (g *GitCommitTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var args GitCommitToolArgs
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("解析参数失败: %w", err)
	}

	switch args.Action {
	case "status":
		return g.gitStatus(ctx, args.WorkingDir)
	case "diff":
		return g.gitDiff(ctx, args.WorkingDir)
	case "add":
		return g.gitAdd(ctx, args.Files, args.WorkingDir)
	case "commit":
		return g.gitCommit(ctx, args.Message, args.WorkingDir)
	case "validate":
		return g.validateCommitMessage(ctx, args.Message)
	default:
		return "", fmt.Errorf("不支持的操作: %s", args.Action)
	}
}

func (g *GitCommitTool) gitStatus(ctx context.Context, workingDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "status", "--short")
	if workingDir != "" {
		cmd.Dir = workingDir
	} else {
		gitDir, err := findGitRoot()
		if err != nil {
			wd, _ := os.Getwd()
			cmd.Dir = wd
		} else {
			cmd.Dir = gitDir
		}
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("获取 Git 状态失败: %s", string(output)), nil
	}
	if string(output) == "" {
		return "工作区干净，没有未提交的更改", nil
	}
	return string(output), nil
}

func (g *GitCommitTool) gitDiff(ctx context.Context, workingDir string) (string, error) {
	cmd1 := exec.CommandContext(ctx, "git", "diff", "--cached")
	if workingDir != "" {
		cmd1.Dir = workingDir
	} else {
		gitDir, err := findGitRoot()
		if err != nil {
			wd, _ := os.Getwd()
			cmd1.Dir = wd
		} else {
			cmd1.Dir = gitDir
		}
	}
	cachedOutput, _ := cmd1.CombinedOutput()

	cmd2 := exec.CommandContext(ctx, "git", "diff")
	if workingDir != "" {
		cmd2.Dir = workingDir
	} else {
		gitDir, err := findGitRoot()
		if err != nil {
			wd, _ := os.Getwd()
			cmd2.Dir = wd
		} else {
			cmd2.Dir = gitDir
		}
	}
	uncachedOutput, _ := cmd2.CombinedOutput()

	result := ""
	if string(cachedOutput) != "" {
		result += "=== 已暂存的更改 ===\n" + string(cachedOutput) + "\n"
	}
	if string(uncachedOutput) != "" {
		result += "=== 未暂存的更改 ===\n" + string(uncachedOutput)
	}
	if result == "" {
		result = "没有差异"
	}
	return result, nil
}

func (g *GitCommitTool) gitAdd(ctx context.Context, files string, workingDir string) (string, error) {
	cmdArgs := []string{"add"}
	if files == "" || strings.TrimSpace(files) == "-A" {
		cmdArgs = append(cmdArgs, "-A")
	} else {
		cmdArgs = append(cmdArgs, strings.Fields(files)...)
	}

	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	if workingDir != "" {
		cmd.Dir = workingDir
	} else {
		gitDir, err := findGitRoot()
		if err != nil {
			wd, _ := os.Getwd()
			cmd.Dir = wd
		} else {
			cmd.Dir = gitDir
		}
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("添加文件失败: %s", string(output)), nil
	}
	return fmt.Sprintf("成功添加文件: %s", strings.Join(cmdArgs[1:], " ")), nil
}

func (g *GitCommitTool) gitCommit(ctx context.Context, message string, workingDir string) (string, error) {
	if message == "" {
		return "提交信息不能为空", nil
	}

	cmd := exec.CommandContext(ctx, "git", "commit", "-m", message)
	if workingDir != "" {
		cmd.Dir = workingDir
	} else {
		gitDir, err := findGitRoot()
		if err != nil {
			wd, _ := os.Getwd()
			cmd.Dir = wd
		} else {
			cmd.Dir = gitDir
		}
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("提交失败: %s", string(output)), nil
	}
	return fmt.Sprintf("提交成功:\n%s", string(output)), nil
}

var commitMsgPattern = regexp.MustCompile(
	`^(feat|fix|docs|style|refactor|test|chore|perf|ci|revert)(\([^)]+\))?: .+$`,
	// 这里的正则匹配 .+ 会由于windows的原因，使得 \r\n  \n 处理不同从而匹配失败
)

func (g *GitCommitTool) validateCommitMessage(ctx context.Context, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "提交信息不能为空白", nil
	}

	firstLine := strings.SplitN(message, "\n", 2)[0]
	if len(firstLine) < 5 {
		return "提交信息至少5个字符", nil
	}
	if len(firstLine) > 100 {
		return "提交信息不能超过100个字符", nil
	}

	matched := commitMsgPattern.MatchString(firstLine)
	if !matched {
		return "格式错误，请使用: <type>(<scope>): <subject>", nil
	}

	parts := strings.SplitN(firstLine, ": ", 2)
	if len(parts) == 2 && len(parts[1]) > 0 {
		firstChar := parts[1][0]
		if firstChar >= 'a' && firstChar <= 'z' {
			return "subject 首字母应大写", nil
		}
	}

	if strings.HasSuffix(firstLine, ".") {
		return "subject 末尾不应有句号", nil
	}

	return message, nil
}
