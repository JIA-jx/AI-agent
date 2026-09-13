package shortterm

import "github.com/pkoukk/tiktoken-go"

type Tokenizer struct {
	enc *tiktoken.Tiktoken
}

func NewTokenizer(model string) *Tokenizer {
	enc, err := tiktoken.EncodingForModel(model)
	if err != nil {
		return &Tokenizer{}
	}
	return &Tokenizer{enc: enc}
}

func (t *Tokenizer) Count(text string) int {
	if t == nil || t.enc == nil {
		if len(text) == 0 {
			return 0
		}
		return len(text) / 4
	}
	return len(t.enc.Encode(text, nil, nil))
}
