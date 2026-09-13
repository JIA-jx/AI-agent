package utils

import (
	"fmt"
	"regexp"
	"strings"
)

type SkillMetadata struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Tags        []string `json:"tags"`
}

func ParseSkillMd(content string) *SkillMetadata {
	metadata := &SkillMetadata{
		Name:        "",
		Description: "",
		Version:     "1.0.0",
		Author:      "",
		Tags:        []string{},
	}

	frontmatterMatch := regexp.MustCompile(`^---\s*\n([\s\S]*?)\n---\s*\n`).FindStringSubmatch(content)
	if frontmatterMatch != nil && len(frontmatterMatch) > 1 {
		frontmatter := frontmatterMatch[1] // 内部内容
		lines := strings.Split(frontmatter, "\n")
		for _, line := range lines {
			match := regexp.MustCompile(`^([\w-]+):\s*(.*)$`).FindStringSubmatch(line)
			if match != nil && len(match) > 2 {
				key := strings.TrimSpace(match[1])
				value := strings.TrimSpace(match[2])

				value = strings.Trim(value, "\"'")

				switch key {
				case "name":
					metadata.Name = value
				case "description":
					metadata.Description = value
				case "version":
					metadata.Version = value
				case "author":
					metadata.Author = value
				case "tags":
					tags := strings.Split(value, ",")
					for _, tag := range tags {
						tag = strings.TrimSpace(tag)
						if tag != "" {
							metadata.Tags = append(metadata.Tags, tag)
						}
					}
				}
			}
		}
	}

	if metadata.Name == "" {
		titleMatch := regexp.MustCompile(`(?m)^#\s+(.+)$`).FindStringSubmatch(content)
		if titleMatch != nil && len(titleMatch) > 1 {
			metadata.Name = strings.TrimSpace(titleMatch[1])
		}
	}

	if metadata.Description == "" {
		contentWithoutFrontmatter := content
		if frontmatterMatch != nil {
			contentWithoutFrontmatter = content[len(frontmatterMatch[0]):]
		}
		lines := strings.Split(contentWithoutFrontmatter, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				if len(trimmed) > 200 {
					trimmed = trimmed[:200]
				}
				metadata.Description = trimmed
				break
			}
		}
	}

	return metadata
}

func ExtractTitle(content string, mark string) string {
	re := regexp.MustCompile(fmt.Sprintf(`(?m)^%s\s+(.*)`, mark))
	match := re.FindStringSubmatch(content)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func SplitByHeading(content string, mark string) []string {
	re := regexp.MustCompile(fmt.Sprintf(`(?m)^%s\s+`, mark))
	indices := re.FindAllStringIndex(content, -1)
	if len(indices) == 0 {
		return []string{content}
	}

	var chunks []string
	if indices[0][0] > 0 {
		preHeaderContent := strings.TrimSpace(content[:indices[0][0]])
		if preHeaderContent != "" {
			chunks = append(chunks, preHeaderContent)
		}
	}

	for i := 0; i < len(indices); i++ {
		start, end := indices[i][0], len(content)
		if i+1 < len(indices) {
			end = indices[i+1][0]
		}
		chunks = append(chunks, strings.TrimSpace(content[start:end]))
	}
	return chunks
}

func SplitTextByLength(content string, limit int, overlap int) []string {
	if len(content) <= limit {
		return []string{content}
	}

	return SplitByWindow(content, limit, overlap)
}

func SplitByWindow(content string, maxSize int, overlap int) []string {
	var chunks []string
	runes := []rune(content)
	if len(runes) <= maxSize {
		return []string{content}
	}

	step := maxSize - overlap
	for i := 0; i < len(runes); i += step {
		end := i + maxSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}
