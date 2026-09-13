package utils

import (
	"regexp"
	"strconv"
	"strings"
)

type StandardizedTitle struct {
	VolumeNum  int    // 卷号
	ChapterNum int    // 章节
	VolumeName string // 卷名
	RawTitle   string // 标题
}

func ParseComplexTitle(title string) StandardizedTitle {
	result := StandardizedTitle{
		RawTitle: title,
	}

	volRe := regexp.MustCompile(`第?\s*([0-9零一二三四五六七八九十百]+)\s*[卷部]`)
	volMatch := volRe.FindStringSubmatch(title)
	if len(volMatch) > 1 {
		result.VolumeNum = ChineseToArabic(volMatch[1])
	}

	chapterRe := regexp.MustCompile(`第?\s*([0-9零一二三四五六七八九十百千万]+)\s*[章回节]`)
	chapterMatch := chapterRe.FindStringSubmatch(title)
	if len(chapterMatch) > 1 {
		result.ChapterNum = ChineseToArabic(chapterMatch[1])
	}

	parts := regexp.MustCompile(`\s+`).Split(title, -1)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if !volRe.MatchString(p) && !chapterRe.MatchString(p) && p != "" {
			if result.VolumeName == "" {
				result.VolumeName = p
			} else {
				result.RawTitle = p
			}
		}
	}
	return result
}

var chineseNumValues = map[rune]int{
	'零': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9,
}

var chineseUnits = map[rune]int{
	'十': 10, '百': 100, '千': 1000, '万': 10000,
}

func ChineseToArabic(cn string) int {
	cn = strings.TrimSpace(cn)
	if cn == "" {
		return 0
	}

	if n, err := strconv.Atoi(cn); err == nil {
		return n
	}

	runes := []rune(cn)
	total := 0
	section := 0
	number := 0

	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if val, ok := chineseNumValues[r]; ok {
			number = val
			if i == len(runes)-1 {
				section += number
			}
		} else if unit, ok := chineseUnits[r]; ok {
			if unit == 10 && number == 0 {
				number = 1
			}
			section += number * unit
			number = 0
		} else if r == '万' {
			total += (section + number) * 10000
			section = 0
			number = 0
		}
	}
	return total + section
}
