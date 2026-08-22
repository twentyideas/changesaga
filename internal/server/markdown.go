package server

import (
	"bytes"
	"fmt"
	"html"
	"html/template"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// markdownWithAnchors renders the author-facing Markdown dialect as safe GFM.
// Raw HTML and dangerous URLs remain disabled by goldmark's default renderer.
// A small heading renderer adds the namespaced permalinks used by the review UI.
func markdownWithAnchors(source, namespace string) template.HTML {
	var out bytes.Buffer
	engine := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.NewFootnote(
				extension.WithFootnoteIDPrefix(namespace+"--"),
				extension.WithFootnoteLinkTitle("Open citation %%"),
				extension.WithFootnoteBacklinkTitle("Return to citation"),
			),
		),
		goldmark.WithParserOptions(
			parser.WithAttribute(),
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(
			util.Prioritized(&headingRenderer{namespace: namespace}, 100),
		)),
	)
	if err := engine.Convert([]byte(source), &out); err != nil {
		// bytes.Buffer does not return write errors, so this is defensive. Never
		// fall back to emitting unescaped authored content.
		return template.HTML("<p>Unable to render Markdown.</p>") // #nosec G203 -- constant HTML.
	}
	return template.HTML(out.String()) // #nosec G203 -- goldmark's safe renderer escaped authored HTML.
}

type headingRenderer struct{ namespace string }

func (r *headingRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(ast.KindHeading, r.render)
	register.Register(ast.KindHTMLBlock, r.renderHTMLBlock)
	register.Register(ast.KindRawHTML, r.renderRawHTML)
}

func (r *headingRenderer) render(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	heading := node.(*ast.Heading)
	level := min(heading.Level+2, 6)
	anchor := []byte("heading")
	if value, ok := heading.AttributeString("id"); ok {
		switch parsed := value.(type) {
		case []byte:
			if len(parsed) > 0 {
				anchor = parsed
			}
		case string:
			if parsed != "" {
				anchor = []byte(parsed)
			}
		}
	}
	id := r.namespace + "--" + string(anchor)
	if entering {
		_, err := fmt.Fprintf(writer, `<h%d id="%s" class="fragment-heading"><span>`, level, html.EscapeString(id))
		return ast.WalkContinue, err
	}
	label := string(heading.Text(source))
	_, err := fmt.Fprintf(writer, `</span><a class="permalink heading-permalink" href="#%s" data-copy-link="#%s" aria-label="Copy link to %s">#</a></h%d>`+"\n", html.EscapeString(id), html.EscapeString(id), html.EscapeString(label), level)
	return ast.WalkContinue, err
}

func (r *headingRenderer) renderHTMLBlock(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	block := node.(*ast.HTMLBlock)
	if entering {
		for index := 0; index < block.Lines().Len(); index++ {
			segment := block.Lines().At(index)
			if _, err := writer.Write(util.EscapeHTML(segment.Value(source))); err != nil {
				return ast.WalkStop, err
			}
		}
	} else if block.HasClosure() {
		if _, err := writer.Write(util.EscapeHTML(block.ClosureLine.Value(source))); err != nil {
			return ast.WalkStop, err
		}
	}
	return ast.WalkContinue, nil
}

func (r *headingRenderer) renderRawHTML(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	raw := node.(*ast.RawHTML)
	for index := 0; index < raw.Segments.Len(); index++ {
		segment := raw.Segments.At(index)
		if _, err := writer.Write(util.EscapeHTML(segment.Value(source))); err != nil {
			return ast.WalkStop, err
		}
	}
	return ast.WalkSkipChildren, nil
}
