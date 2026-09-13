package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"thunder/ai/einos"
)

type HTMLToPPTTool struct {
	outputDir string
}

type HTMLToPPTConfig struct {
	OutputDir string
}

func NewHTMLToPPTTool(c *HTMLToPPTConfig) einos.InvokeParamTool {
	if c == nil {
		c = &HTMLToPPTConfig{}
	}
	return &HTMLToPPTTool{outputDir: c.OutputDir}
}

func (h *HTMLToPPTTool) Params() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"html_content": {
			Desc:     "HTML 内容字符串",
			Type:     schema.String,
			Required: true,
		},
		"output_path": {
			Desc:     "输出的 PPTX 文件路径（包含文件名和.pptx 扩展名）",
			Type:     schema.String,
			Required: true,
		},
		"title": {
			Desc: "PPT 标题，如果不提供则从 HTML 标题提取",
			Type: schema.String,
		},
		"slides_per_page": {
			Desc: "每页幻灯片的内容数量（默认 5）",
			Type: schema.Integer,
		},
	}
}

func (h *HTMLToPPTTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "html_to_ppt",
		Desc: "将 HTML 内容转换为 PPTX 演示文稿，自动解析 HTML 结构并生成幻灯片（需要安装 Python 和 python-pptx 库）",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"html_content": {
				Desc:     "HTML 内容字符串",
				Type:     schema.String,
				Required: true,
			},
			"output_path": {
				Desc:     "输出的 PPTX 文件路径（包含文件名和.pptx 扩展名）",
				Type:     schema.String,
				Required: true,
			},
			"title": {
				Desc: "PPT 标题，如果不提供则从 HTML 标题提取",
				Type: schema.String,
			},
			"slides_per_page": {
				Desc: "每页幻灯片的内容数量（默认 5）",
				Type: schema.Integer,
			},
		}),
	}, nil
}

func (h *HTMLToPPTTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var params map[string]any
	if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
		return "", fmt.Errorf("解析参数失败：%w", err)
	}

	htmlContent, ok := params["html_content"].(string)
	if !ok || htmlContent == "" {
		return "", fmt.Errorf("html_content 参数为空")
	}

	outputPath, ok := params["output_path"].(string)
	if !ok || outputPath == "" {
		return "", fmt.Errorf("output_path 参数为空")
	}
	if !strings.HasSuffix(outputPath, ".pptx") {
		outputPath = outputPath + ".pptx"
	}
	if h.outputDir != "" && !filepath.IsAbs(outputPath) { // 相对路径
		outputPath = filepath.Join(h.outputDir, outputPath)
	}

	err := createPPTXWithPython(outputPath, htmlContent)
	if err != nil {
		return "", fmt.Errorf("创建 PPTX 失败：%w", err)
	}

	result := map[string]string{
		"status":      "success",
		"message":     fmt.Sprintf("PPT 生成成功：%s", outputPath),
		"output_path": outputPath,
	}

	resultJSON, _ := json.Marshal(result)
	return string(resultJSON), nil
}

func createPPTXWithPython(outputPath string, htmlContent string) error {
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建目录失败：%w", err)
	}

	pythonCmd, err := getPythonCommand()
	if err != nil {
		return err
	}

	tempFiles := make([]string, 0, 2)
	defer func() {
		for _, f := range tempFiles {
			os.Remove(f)
		}
	}()

	tempHTML, err := writeTempHTML(dir, htmlContent)
	if err != nil {
		return err
	}
	tempFiles = append(tempFiles, tempHTML)

	tempScript, err := writeTempScript(dir)
	if err != nil {
		return err
	}
	tempFiles = append(tempFiles, tempScript)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, pythonCmd, tempScript, tempHTML, outputPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("执行 Python 脚本失败：%w", err)
	}

	return nil
}

var (
	pythonCmdCache string
	pythonCmdOnce  sync.Once
	pythonCmdErr   error
)

func getPythonCommand() (string, error) {
	pythonCmdOnce.Do(func() {
		for _, cmd := range []string{"python3", "python"} {
			if err := exec.Command(cmd, "--version").Run(); err == nil {
				pythonCmdCache = cmd
				return
			}
		}
		pythonCmdErr = fmt.Errorf("Python 未安装或不可用。请先安装 Python，然后执行：pip install python-pptx beautifulsoup4")
	})
	return pythonCmdCache, pythonCmdErr
}

func writeTempHTML(dir, content string) (string, error) {
	tempHTML := filepath.Join(dir, fmt.Sprintf("temp_html_%d.html", time.Now().UnixNano()))

	fullHTML := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Temp Presentation</title>
</head>
<body class="bg-black text-white overflow-hidden font-sans">
<div id="presentation" class="relative w-full h-screen glow-bg">
` + content + `
</div>
</body>
</html>`

	if err := os.WriteFile(tempHTML, []byte(fullHTML), 0644); err != nil {
		return "", fmt.Errorf("写入临时HTML文件失败：%w", err)
	}
	return tempHTML, nil
}

func writeTempScript(dir string) (string, error) {
	tempScript := filepath.Join(dir, fmt.Sprintf("generate_ppt_bs_%d.py", time.Now().UnixNano()))

	pythonCode := `#!/usr/bin/env python3
"""HTML 转 PPT - 使用 BeautifulSoup 解析 Tailwind 样式"""

import re
import sys
from pathlib import Path

from bs4 import BeautifulSoup, NavigableString
from pptx import Presentation
from pptx.dml.color import RGBColor
from pptx.enum.shapes import MSO_SHAPE
from pptx.enum.text import PP_ALIGN
from pptx.util import Inches, Pt

# Tailwind 字体大小映射
FONT_SIZE_MAP = {
    "text-7xl": Pt(70),
    "text-6xl": Pt(60),
    "text-5xl": Pt(48),
    "text-4xl": Pt(36),
    "text-3xl": Pt(30),
    "text-2xl": Pt(24),
    "text-xl": Pt(20),
    "text-lg": Pt(18),
}

# 颜色映射
COLOR_MAP = {
    "text-white": RGBColor(255, 255, 255),
    "text-gray-300": RGBColor(209, 213, 219),
    "text-gray-400": RGBColor(156, 163, 175),
    "text-gray-500": RGBColor(107, 114, 128),
}


def get_font_size(class_list):
    for cls in class_list:
        if cls in FONT_SIZE_MAP:
            return FONT_SIZE_MAP[cls]
    return Pt(24)


def get_color(class_list):
    for cls in class_list:
        if cls in COLOR_MAP:
            return COLOR_MAP[cls]
    return RGBColor(255, 255, 255)


def is_bold(class_list):
    return "font-bold" in class_list or "font-black" in class_list


def is_grid_layout(class_list):
    return "grid-cols-2" in class_list or "md:grid-cols-2" in class_list


def is_flex_row(class_list):
    return "flex-row" in class_list or ("flex" in class_list and "flex-col" not in class_list)


def clean_text(text):
    if not text:
        return ""
    text = text.replace("&nbsp;", " ")
    text = " ".join(text.split())
    return text.strip()


class SlideExtractor:
    """幻灯片内容提取器"""
    
    def __init__(self, slide_html):
        self.soup = BeautifulSoup(slide_html, "html.parser")
        self.slide_div = self.soup.find("div", class_=re.compile("slide"))
        self.elements = []
        self.seen_texts = set()
        self.has_two_column = False
    
    def extract(self):
        if not self.slide_div:
            return [], False
        self._process_element(self.slide_div)
        return self.elements, self.has_two_column
    
    def _process_element(self, elem, depth=0, in_grid=False):
        if elem is None or isinstance(elem, NavigableString):
            return
        if elem.name in ["script", "style"]:
            return
        
        class_list = elem.get("class", [])
        
        # 处理 grid 两列布局
        if elem.name == "div" and is_grid_layout(class_list):
            self._handle_grid_layout(elem)
            return
        
        # 处理水平 flex 容器
        if elem.name == "div" and is_flex_row(class_list) and not in_grid:
            self._handle_flex_row(elem, class_list)
            return
        
        # 处理文本元素
        if elem.name in ["h1", "h2", "h3", "p"]:
            self._handle_text_element(elem, class_list)
            return
        
        # 递归处理子元素
        for child in elem.children:
            self._process_element(child, depth + 1, in_grid or is_grid_layout(class_list))
    
    def _handle_grid_layout(self, elem):
        self.has_two_column = True
        cols = elem.find_all("div", recursive=False)[:2]
        left_items, right_items = [], []
        
        for i, col in enumerate(cols):
            items = self._extract_column_items(col)
            if i == 0:
                left_items = items
            else:
                right_items = items
        
        if left_items or right_items:
            self.elements.append({"type": "two_column", "left": left_items, "right": right_items})
    
    def _extract_column_items(self, col):
        items = []
        for child in col.find_all(["p", "h1", "h2", "h3", "div"], recursive=True):
            text = clean_text(child.get_text())
            if text and text not in self.seen_texts:
                items.append({
                    "text": text,
                    "classes": child.get("class", []),
                    "is_horizontal": False,
                    "is_list": False,
                })
                self.seen_texts.add(text)
        return items
    
    def _handle_flex_row(self, elem, class_list):
        parts = []
        for child in elem.descendants:
            if isinstance(child, NavigableString):
                text = clean_text(str(child))
                if text:
                    parts.append(text)
            elif child.name in ["span", "p"]:
                text = clean_text(child.get_text())
                if text:
                    parts.append(text)
        
        full_text = " ".join(parts)
        if full_text and full_text not in self.seen_texts and len(full_text) > 1:
            self.elements.append({
                "type": "text",
                "text": full_text,
                "classes": class_list,
                "is_horizontal": True,
                "is_list": False,
            })
            self.seen_texts.add(full_text)
    
    def _handle_text_element(self, elem, class_list):
        text = clean_text(elem.get_text())
        if text and text not in self.seen_texts:
            is_list = text.startswith("•") or elem.find_parent("li") is not None
            self.elements.append({
                "type": "text",
                "text": text,
                "classes": class_list,
                "is_horizontal": False,
                "is_list": is_list,
            })
            self.seen_texts.add(text)


def create_slide(prs, elements, is_first=False):
    """创建单个 slide"""
    slide_layout = prs.slide_layouts[6]
    slide = prs.slides.add_slide(slide_layout)

    # 黑色背景
    bg = slide.shapes.add_shape(MSO_SHAPE.RECTANGLE, 0, 0, prs.slide_width, prs.slide_height)
    bg.fill.solid()
    bg.fill.fore_color.rgb = RGBColor(10, 10, 10)
    bg.line.fill.background()

    if not elements:
        return slide

    # 计算起始位置（垂直居中）
    total_items = len([e for e in elements if e.get("type") == "text"])
    total_height = total_items * 0.5 + sum(1.0 for e in elements if e.get("type") == "two_column")
    y_pos = Inches((7.5 - total_height) / 2)
    if y_pos < Inches(0.8):
        y_pos = Inches(0.8)

    for elem_data in elements:
        if elem_data.get("type") == "two_column":
            y_pos = _render_two_column(slide, elem_data, y_pos)
        else:
            y_pos = _render_text_element(slide, elem_data, y_pos)

    return slide


def _render_two_column(slide, elem_data, y_pos):
    left_items = elem_data.get("left", [])
    right_items = elem_data.get("right", [])
    col_y = y_pos
    max_height = Inches(0)

    # 左列
    for item in left_items:
        _add_textbox(slide, Inches(0.8), col_y, Inches(5.5), Inches(1.0), item)
        col_y += Inches(0.6)
    if left_items:
        max_height = max(max_height, col_y - y_pos)

    # 右列
    col_y = y_pos
    for item in right_items:
        _add_textbox(slide, Inches(7.0), col_y, Inches(5.5), Inches(1.0), item)
        col_y += Inches(0.6)
    if right_items:
        max_height = max(max_height, col_y - y_pos)

    return y_pos + max_height + Inches(0.3)


def _render_text_element(slide, elem_data, y_pos):
    text = elem_data.get("text", "")
    classes = elem_data.get("classes", [])
    is_list = elem_data.get("is_list", False)
    
    if not text:
        return y_pos
    
    box_height = Inches(0.5) if is_list else Inches(0.8)
    display_text = ("• " + text) if is_list and not text.startswith("•") else text
    
    _add_textbox(slide, Inches(0.8), y_pos, Inches(11.733), box_height, {
        "text": display_text,
        "classes": classes,
    })
    
    return y_pos + (Inches(0.5) if is_list else Inches(0.8))


def _add_textbox(slide, left, top, width, height, item):
    txBox = slide.shapes.add_textbox(left, top, width, height)
    tf = txBox.text_frame
    tf.word_wrap = True
    
    p = tf.paragraphs[0]
    p.text = item["text"]
    p.alignment = PP_ALIGN.CENTER
    p.font.size = get_font_size(item.get("classes", []))
    p.font.color.rgb = get_color(item.get("classes", []))
    p.font.bold = is_bold(item.get("classes", []))


def main():
    if len(sys.argv) < 3:
        print("Usage: python script.py <input.html> <output.pptx>")
        sys.exit(1)

    input_file = sys.argv[1]
    output_file = sys.argv[2]

    with open(input_file, "r", encoding="utf-8") as f:
        html_content = f.read()

    soup = BeautifulSoup(html_content, "html.parser")
    slides = soup.find_all("div", class_=re.compile("slide"))

    print(f"Found {len(slides)} slides")

    prs = Presentation()
    prs.slide_width = Inches(13.333)
    prs.slide_height = Inches(7.5)

    for i, slide_div in enumerate(slides):
        print(f"Processing slide {i + 1}...")
        extractor = SlideExtractor(str(slide_div))
        elements, has_two_col = extractor.extract()
        print(f"  - Found {len(elements)} elements, two_column={has_two_col}")
        create_slide(prs, elements, is_first=(i == 0))

    # 确保输出目录存在
    Path(output_file).parent.mkdir(parents=True, exist_ok=True)
    prs.save(output_file)
    print(f"PPT saved: {output_file}")


if __name__ == "__main__":
    main()
`

	if err := os.WriteFile(tempScript, []byte(pythonCode), 0755); err != nil {
		return "", fmt.Errorf("写入临时脚本文件失败：%w", err)
	}
	return tempScript, nil
}
