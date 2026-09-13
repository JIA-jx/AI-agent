package utils

import (
	"sync"

	"github.com/pkoukk/tiktoken-go"
)

var (
	tkc     *tiktoken.Tiktoken
	tkcOnce sync.Once
)

func GetTokenCount(text string) int {
	tkcOnce.Do(func() {
		encoding := "cl100k_base"
		tk, err := tiktoken.GetEncoding(encoding)
		if err != nil {
			return
		}
		tkc = tk
	})

	if tkc == nil {
		return len([]rune(text))
	}
	tokens := tkc.Encode(text, nil, nil)
	return len(tokens)
}
