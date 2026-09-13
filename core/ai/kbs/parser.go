package kbs

import (
	"context"

	"github.com/cloudwego/eino-ext/components/document/parser/docx"
	"github.com/cloudwego/eino-ext/components/document/parser/html"
	"github.com/cloudwego/eino-ext/components/document/parser/pdf"
	"github.com/cloudwego/eino/components/document/parser"
)

func DocxParser(config *docx.Config) (parser.Parser, error) {
	return docx.NewDocxParser(context.Background(), config)
}

func PDFParser(config *pdf.Config) (parser.Parser, error) {
	return pdf.NewPDFParser(context.Background(), config)
}

func HtmlParser(config *html.Config) (parser.Parser, error) {
	return html.NewParser(context.Background(), config)
}
