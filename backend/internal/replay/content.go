package replay

import (
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

// Match the client policy in widget/src/modules/replay-content.ts. Names and other
// application-specific personal content require a private-region marker at capture.
var (
	replayEmail           = regexp.MustCompile("(?i)[A-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[A-Z0-9-]+(?:\\.[A-Z0-9-]+)+")
	replayPhone           = regexp.MustCompile(`\+?[0-9][0-9 ().-]{7,}[0-9]`)
	replayAssetExtension  = regexp.MustCompile(`(?i)\.(css|woff2?|ttf|otf|png|jpe?g|gif|webp|avif|svg|ico)$`)
	replayPrivateAsset    = regexp.MustCompile(`(?i)avatar|profile|user|account|private|upload|attachment|reporter|customer`)
	replayFragment        = regexp.MustCompile(`^#[a-zA-Z_][\w-]*$`)
	replayScopeAttribute  = regexp.MustCompile(`^data-v-[\da-f]+(?:-s)?$`)
	replayPrivateVariable = regexp.MustCompile(`(?i)name|email|user|account|token|secret|address|phone|reporter`)
)

var replayCalendarDate = regexp.MustCompile(`^(?:[0-9]{4}-[0-9]{2}-[0-9]{2}|[0-9]{2}-[0-9]{2}-[0-9]{4})$`)

func validReplayText(value string) bool {
	if replayEmail.MatchString(value) {
		return false
	}
	for _, match := range replayPhone.FindAllString(value, -1) {
		if replayCalendarDate.MatchString(match) {
			continue
		}
		digits := 0
		for _, char := range match {
			if char >= '0' && char <= '9' {
				digits++
			}
		}
		if digits >= 8 {
			return false
		}
	}
	return true
}

func safeReplayAssetURL(value string) bool {
	if replayFragment.MatchString(value) {
		return true
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return false
	}
	return validReplayText(parsed.Path) && !replayPrivateAsset.MatchString(parsed.Path) && replayAssetExtension.MatchString(parsed.Path)
}

// Inspect parsed CSS tokens rather than looking for literal "url(" strings. CSS
// escapes and comments cannot bypass the resource and generated-content policy.
func validReplayCSS(value string, inline bool) bool {
	if len(value) > 1024*1024 || replayEmail.MatchString(value) {
		return false
	}
	parser := css.NewParser(parse.NewInputString(value), inline)
	for {
		grammar, _, data := parser.Next()
		if grammar == css.ErrorGrammar {
			return parser.Err() == io.EOF
		}
		name := strings.ToLower(string(data))
		if grammar == css.CommentGrammar {
			return false
		}
		if strings.Contains(name, `\`) {
			return false
		}
		if grammar == css.AtRuleGrammar && name != "@layer" {
			return false
		}
		if grammar == css.DeclarationGrammar || grammar == css.CustomPropertyGrammar {
			if name == "behavior" || name == "-moz-binding" ||
				(strings.HasPrefix(name, "--") && replayPrivateVariable.MatchString(name)) {
				return false
			}
		}
		var values strings.Builder
		for _, token := range parser.Values() {
			values.Write(token.Data)
		}
		if name == "content" && !replayEmptyContent.MatchString(strings.TrimSpace(values.String())) {
			return false
		}
		selectors := grammar == css.BeginRulesetGrammar || grammar == css.QualifiedRuleGrammar
		if !validReplayCSSTokens(values.String(), selectors) {
			return false
		}

	}
}

var replayEmptyContent = regexp.MustCompile(`^(?:none|normal|"[ \t]*"|'[ \t]*')(?:\s*!important)?$`)

func validReplayCSSTokens(value string, selectors bool) bool {
	if strings.Contains(strings.ToLower(value), "javascript:") {
		return false
	}
	lexer := css.NewLexer(parse.NewInputString(value))
	for {
		token, data := lexer.Next()
		if token == css.ErrorToken {
			return lexer.Err() == io.EOF
		}
		text := string(data)
		if !selectors && strings.Contains(text, `\`) {
			return false
		}
		switch token {
		case css.BadStringToken, css.BadURLToken, css.CommentToken:
			return false
		case css.URLToken:
			resource := strings.TrimSpace(text[4 : len(text)-1])
			resource = strings.Trim(resource, `"'`)
			if resource != "" && !safeReplayAssetURL(resource) {
				return false
			}
		case css.FunctionToken:
			function := strings.ToLower(text)
			if function == "url(" || function == "expression(" || function == "attr(" || strings.Contains(function, "image-set(") {
				return false
			}
		}
	}
}

var replayPresentationAttributes = strings.Fields(`
id class type role dir lang hidden open disabled checked selected
width height colspan rowspan span size rows cols wrap slot
viewbox d fill fill-rule fill-opacity stroke stroke-width stroke-linecap
stroke-linejoin stroke-dasharray stroke-dashoffset stroke-opacity clip-rule
cx cy r rx ry x y x1 x2 y1 y2 points transform preserveaspectratio xmlns
offset stop-color stop-opacity gradientunits gradienttransform patternunits patterntransform clip-path mask
aria-expanded aria-hidden aria-checked aria-selected aria-disabled
data-theme data-state data-side data-orientation data-active data-selected aria-current
rr_width rr_height rr_scrollleft rr_scrolltop rr_open_mode rr_mediastate rr_mediacurrenttime
rr_mediaplaybackrate rr_mediamuted rr_medialoop rr_mediavolume`)

func validReplayContentAttributes(value any) (bool, bool) {
	attributes, ok := replayObject(value)
	if !ok {
		return false, false
	}
	privateRegion := false
	for key, raw := range attributes {
		name := strings.ToLower(key)
		text, isText := raw.(string)
		switch name {
		case "data-fidibeki-mask", "data-fidibeki-safe-text":
			if raw != nil && raw != "" {
				return false, false
			}
			if name == "data-fidibeki-mask" && raw != nil {
				privateRegion = true
			}
		case "style", "_csstext":
			if !isText || !validReplayCSS(text, name == "style") {
				return false, false
			}
		case "value", "alt", "title", "aria-label", "aria-description":
			if raw != nil && (!isText || !maskedReplayText(text)) {
				return false, false
			}
		case "placeholder":
			if raw != nil && (!isText || !validReplayText(text)) {
				return false, false
			}
		case "src", "href", "xlink:href", "poster":
			if raw != nil && (!isText || !safeReplayAssetURL(text)) {
				return false, false
			}
		default:
			allowed := replayScopeAttribute.MatchString(name)
			for _, attr := range replayPresentationAttributes {
				allowed = allowed || name == attr
			}
			if !allowed {
				return false, false
			}
			if raw == nil || replayBoolean(raw) {
				continue
			}
			if _, numeric := replayNumber(raw); numeric {
				continue
			}
			if !isText || replayEmail.MatchString(text) {
				return false, false
			}
			// SVG paints can contain local fragment references, but no private URLs.
			if strings.Contains(strings.ToLower(text), "url") && !validReplayCSS("fill:"+text, true) {
				return false, false
			}
		}
	}
	return privateRegion, true
}

func validReplayStyleMutation(data map[string]any, source int) bool {
	index := func(value any) bool {
		if replayID(value, false) {
			return true
		}
		list, ok := value.([]any)
		if !ok || len(list) == 0 {
			return false
		}
		for _, entry := range list {
			if !replayID(entry, false) {
				return false
			}
		}
		return true
	}
	rules := func(value any, additions bool) bool {
		list, ok := value.([]any)
		if !ok {
			return false
		}
		for _, entry := range list {
			rule, ok := replayObject(entry)
			if !ok || !hasOnlyKeys(rule, "index", "rule") || !index(rule["index"]) {
				return false
			}
			if additions {
				text, ok := rule["rule"].(string)
				if !ok || !validReplayCSS(text, false) {
					return false
				}
			} else if _, exists := rule["rule"]; exists {
				return false
			}
		}
		return true
	}
	if !replayID(data["id"], false) && !replayID(data["styleId"], false) {
		return false
	}
	for _, key := range []string{"id", "styleId"} {
		if value, exists := data[key]; exists && !replayID(value, false) {
			return false
		}
	}
	switch source {
	case 8:
		if !hasOnlyKeys(data, "source", "id", "styleId", "adds", "removes", "replace", "replaceSync") {
			return false
		}
		for _, key := range []string{"replace", "replaceSync"} {
			if value, exists := data[key]; exists {
				text, ok := value.(string)
				if !ok || !validReplayCSS(text, false) {
					return false
				}
			}
		}
		if value, exists := data["adds"]; exists && !rules(value, true) {
			return false
		}
		if value, exists := data["removes"]; exists && !rules(value, false) {
			return false
		}
	case 13:
		if !hasOnlyKeys(data, "source", "id", "styleId", "index", "set", "remove") || !index(data["index"]) {
			return false
		}
		if value, exists := data["set"]; exists {
			set, ok := replayObject(value)
			if !ok || !hasOnlyKeys(set, "property", "value", "priority") {
				return false
			}
			property, propOK := set["property"].(string)
			text, textOK := set["value"].(string)
			if !propOK || !textOK || !replayCSSProperty.MatchString(property) || !validReplayCSS(property+":"+text, true) {
				return false
			}
			if priority, exists := set["priority"]; exists && priority != "" && priority != "important" {
				return false
			}
		} else if value, exists := data["remove"]; exists {
			remove, ok := replayObject(value)
			if !ok || !hasOnlyKeys(remove, "property") {
				return false
			}
			property, ok := remove["property"].(string)
			if !ok || !replayCSSProperty.MatchString(property) {
				return false
			}
		} else {
			return false
		}
	case 16:
		if !hasOnlyKeys(data, "source", "id", "styleIds", "styles") {
			return false
		}
		ids, ok := data["styleIds"].([]any)
		if !ok {
			return false
		}
		for _, id := range ids {
			if !replayID(id, false) {
				return false
			}
		}
		if value, exists := data["styles"]; exists {
			styles, ok := value.([]any)
			if !ok {
				return false
			}
			for _, value := range styles {
				style, ok := replayObject(value)
				if !ok || !hasOnlyKeys(style, "styleId", "rules") || !replayID(style["styleId"], false) || !rules(style["rules"], true) {
					return false
				}
			}
		}
	default:
		return false
	}
	return true
}

var replayCSSProperty = regexp.MustCompile(`^-{0,2}[a-zA-Z][\w-]*$`)
