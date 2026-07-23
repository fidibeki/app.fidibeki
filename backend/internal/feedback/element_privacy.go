package feedback

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	maximumElementNodes = 128
	maximumElementDepth = 12
	maximumStyleScalar  = 10000
)

var (
	allowedElementTags = map[string]struct{}{
		"address": {}, "article": {}, "aside": {}, "blockquote": {}, "dd": {},
		"div": {}, "dl": {}, "dt": {}, "figcaption": {}, "figure": {},
		"footer": {}, "h1": {}, "h2": {}, "h3": {}, "h4": {}, "h5": {},
		"h6": {}, "header": {}, "li": {}, "main": {}, "nav": {}, "ol": {},
		"p": {}, "section": {}, "span": {}, "table": {}, "tbody": {}, "td": {},
		"tfoot": {}, "th": {}, "thead": {}, "tr": {}, "ul": {},
	}
	allowedElementStyleProperties = map[string]struct{}{
		"display": {}, "position": {}, "color": {}, "background-color": {},
		"font-size": {}, "font-weight": {}, "line-height": {}, "text-align": {},
		"box-sizing": {}, "width": {}, "height": {}, "min-width": {},
		"min-height": {}, "max-width": {}, "max-height": {}, "padding-top": {},
		"padding-right": {}, "padding-bottom": {}, "padding-left": {},
		"margin-top": {}, "margin-right": {}, "margin-bottom": {},
		"margin-left": {}, "border-top-width": {}, "border-right-width": {},
		"border-bottom-width": {}, "border-left-width": {}, "border-top-style": {},
		"border-right-style": {}, "border-bottom-style": {},
		"border-left-style": {}, "border-top-color": {},
		"border-right-color": {}, "border-bottom-color": {},
		"border-left-color": {}, "border-top-left-radius": {},
		"border-top-right-radius": {}, "border-bottom-right-radius": {},
		"border-bottom-left-radius": {},
	}
	allowedElementStyleKeywords = map[string]struct{}{
		"absolute": {}, "auto": {}, "block": {}, "bold": {}, "bolder": {},
		"border-box": {}, "center": {}, "contents": {}, "dashed": {},
		"dotted": {}, "double": {}, "end": {}, "fixed": {}, "flex": {},
		"grid": {}, "groove": {}, "hidden": {}, "inherit": {}, "initial": {},
		"inline": {}, "inline-block": {}, "inline-flex": {}, "inline-grid": {},
		"inset": {}, "left": {}, "lighter": {}, "none": {}, "normal": {},
		"outset": {}, "relative": {}, "revert": {}, "ridge": {}, "right": {},
		"solid": {}, "start": {}, "static": {}, "sticky": {}, "table": {},
		"table-cell": {}, "table-row": {}, "transparent": {}, "unset": {},
	}
	elementStyleUnsafePattern   = regexp.MustCompile(`[\x00-\x1f\x7f"'` + "`" + `;:{}\[\]\\]`)
	elementStyleFunctionPattern = regexp.MustCompile(`(?:url|var|attr|calc|env|expression)\s*\(`)
	elementStyleHexPattern      = regexp.MustCompile(`^#[0-9a-f]{3,8}$`)
	elementStyleRGBPattern      = regexp.MustCompile(
		`^rgba?\(\s*\d{1,3}(?:\.\d+)?%?\s*,\s*\d{1,3}(?:\.\d+)?%?\s*,\s*` +
			`\d{1,3}(?:\.\d+)?%?(?:\s*,\s*(?:0(?:\.\d+)?|1(?:\.0+)?))?\s*\)$`,
	)
	elementStyleLengthPattern = regexp.MustCompile(
		`^-?(?:\d+(?:\.\d+)?|\.\d+)(?:px|%|em|rem|vh|vw)?` +
			`(?:\s+-?(?:\d+(?:\.\d+)?|\.\d+)(?:px|%|em|rem|vh|vw)?){0,3}$`,
	)
	elementStyleNumberPattern = regexp.MustCompile(`-?(?:\d+(?:\.\d+)?|\.\d+)`)
)

func sanitizeElementAttachment(input ElementAttachmentInput) (string, string, bool) {
	if strings.TrimSpace(input.Selector) == "" || len(input.Selector) > 2000 ||
		len(input.HTML) == 0 || len(input.HTML) > maxElementHTMLBytes {
		return "", "", false
	}
	encodedInputStyles, err := json.Marshal(input.Styles)
	if err != nil || len(encodedInputStyles) > maxElementCSSBytes {
		return "", "", false
	}
	contextNode := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := html.ParseFragment(strings.NewReader(input.HTML), contextNode)
	if err != nil {
		return "", "", false
	}
	var source *html.Node
	for _, node := range nodes {
		if node.Type == html.TextNode && strings.TrimSpace(node.Data) == "" {
			continue
		}
		if node.Type != html.ElementNode || source != nil {
			return "", "", false
		}
		source = node
	}
	if source == nil {
		return "", "", false
	}
	state := elementSanitizerState{}
	sanitized := sanitizeElementNode(source, &state, 1)
	if sanitized == nil || state.unsafe {
		return "", "", false
	}
	var rendered bytes.Buffer
	if err := html.Render(&rendered, sanitized); err != nil {
		return "", "", false
	}
	styles := make(map[string]string)
	for property, inputValue := range input.Styles {
		property = strings.ToLower(strings.TrimSpace(property))
		if _, allowed := allowedElementStyleProperties[property]; !allowed {
			continue
		}
		if value, ok := sanitizeElementStyleValue(inputValue); ok && value != "" {
			styles[property] = value
		}
	}
	encodedStyles, err := json.Marshal(styles)
	if err != nil {
		return "", "", false
	}
	return rendered.String(), string(encodedStyles), true
}

type elementSanitizerState struct {
	nodes  int
	unsafe bool
}

func sanitizeElementNode(
	source *html.Node,
	state *elementSanitizerState,
	depth int,
) *html.Node {
	tag := strings.ToLower(source.Data)
	if _, allowed := allowedElementTags[tag]; !allowed {
		return nil
	}
	state.nodes++
	if state.nodes > maximumElementNodes || depth > maximumElementDepth {
		state.unsafe = true
		return nil
	}
	output := &html.Node{Type: html.ElementNode, DataAtom: atom.Lookup([]byte(tag)), Data: tag}
	for child := source.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode {
			continue
		}
		sanitized := sanitizeElementNode(child, state, depth+1)
		if state.unsafe {
			return nil
		}
		if sanitized != nil {
			output.AppendChild(sanitized)
		}
	}
	return output
}

func sanitizeElementStyleValue(input string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(input))
	if value == "" {
		return "", true
	}
	if len(value) > 100 || elementStyleUnsafePattern.MatchString(value) ||
		elementStyleFunctionPattern.MatchString(value) {
		return "", false
	}
	if _, allowed := allowedElementStyleKeywords[value]; allowed {
		return value, true
	}
	if elementStyleHexPattern.MatchString(value) || elementStyleRGBPattern.MatchString(value) {
		return value, true
	}
	if elementStyleLengthPattern.MatchString(value) {
		for _, rawNumber := range elementStyleNumberPattern.FindAllString(value, -1) {
			number, err := strconv.ParseFloat(rawNumber, 64)
			if err != nil || math.Abs(number) > maximumStyleScalar {
				return "", false
			}
		}
		return value, true
	}
	return "", false
}
