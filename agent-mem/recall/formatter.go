package recall

import (
	"fmt"
	"strings"

	"agentmem/types"
)

// FormatContext 将召回的记忆按类型分段格式化为可注入 prompt 的文本
func FormatContext(scored []types.ScoredMemory) string {
	if len(scored) == 0 {
		return ""
	}

	var short, long, ent, insight []string
	for _, s := range scored {
		if s.Memory == nil {
			continue
		}

		switch s.Memory.Type {
		case types.MemoryTypeShort:
			short = append(short, s.Memory.Content)
		case types.MemoryTypeLong:
			long = append(long, s.Memory.Content)
		case types.MemoryTypeEntity:
			ent = append(ent, s.Memory.Content)
		case types.MemoryTypeInsight:
			insight = append(insight, s.Memory.Content)
		}
	}

	var b strings.Builder
	b.WriteString("【 召回的记忆（供回答时参考）】\n")
	if len(short) > 0 {
		b.WriteString("[短期记忆]\n")
		writeList(&b, short)
	}
	if len(long) > 0 {
		b.WriteString("[长期记忆]\n")
		writeList(&b, long)
	}
	if len(insight) > 0 {
		b.WriteString("[洞察]\n")
		writeList(&b, insight)
	}
	if len(ent) > 0 {
		b.WriteString("[实体记忆]\n")
		writeList(&b, ent)
	}
	return b.String()
}

func writeList(b *strings.Builder, items []string) {
	for i, x := range items {
		fmt.Fprintf(b, "%d. %s\n", i+1, x)
	}
}
