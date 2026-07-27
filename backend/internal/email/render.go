package email

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"strings"
)

type Content struct {
	Preheader     string
	Heading       string
	Workspace     string
	Body          []string
	Metadata      []Metadata
	Action        *Link
	ShowActionURL bool
	Footer        string
	FooterLinks   []Link
	LogoURL       string
}

type Metadata struct {
	Label string
	Value string
}

type Link struct {
	Label string
	URL   string
}

type renderedContent struct {
	Content
	Text string
}

func Compose(message Message, content Content) (Message, error) {
	textBody, htmlBody, err := Render(content)
	if err != nil {
		return Message{}, err
	}
	message.Text = textBody
	message.HTML = htmlBody
	if err := message.Validate(); err != nil {
		return Message{}, fmt.Errorf("validate composed email: %w", err)
	}
	return message, nil
}

func Render(content Content) (string, string, error) {
	content.Preheader = strings.TrimSpace(content.Preheader)
	content.Heading = strings.TrimSpace(content.Heading)
	content.Workspace = strings.TrimSpace(content.Workspace)
	content.Footer = strings.TrimSpace(content.Footer)
	if content.Heading == "" {
		return "", "", errors.New("email heading is required")
	}
	if content.Preheader == "" {
		content.Preheader = content.Heading
	}
	if err := validateHTTPURL(content.LogoURL, "logo URL"); err != nil {
		return "", "", err
	}
	if content.Action != nil {
		if err := validateLink(*content.Action, false); err != nil {
			return "", "", fmt.Errorf("primary action: %w", err)
		}
	}
	for index, link := range content.FooterLinks {
		if err := validateLink(link, true); err != nil {
			return "", "", fmt.Errorf("footer link %d: %w", index+1, err)
		}
	}
	textBody := renderText(content)
	var htmlBody bytes.Buffer
	if err := transactionalTemplate.Execute(&htmlBody, renderedContent{Content: content, Text: textBody}); err != nil {
		return "", "", fmt.Errorf("render transactional email: %w", err)
	}
	return textBody, htmlBody.String(), nil
}

// BrandLogoURL points at the API's own copy of the wordmark. It keeps the host
// it is given rather than deriving a friendlier one, because the API is the only
// tier guaranteed to answer for BrandLogoPath in every environment.
func BrandLogoURL(publicBaseURL string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(publicBaseURL))
	if err != nil || !base.IsAbs() || base.Host == "" {
		return "", errors.New("public base URL is invalid")
	}
	base.Path = BrandLogoPath
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}

func validateLink(link Link, allowMailto bool) error {
	if strings.TrimSpace(link.Label) == "" {
		return errors.New("label is required")
	}
	if allowMailto {
		parsed, err := url.Parse(strings.TrimSpace(link.URL))
		if err == nil && parsed.Scheme == "mailto" && parsed.Opaque != "" {
			return nil
		}
	}
	return validateHTTPURL(link.URL, "URL")
}

func validateHTTPURL(value, field string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || !parsed.IsAbs() || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute HTTP URL", field)
	}
	return nil
}

func renderText(content Content) string {
	parts := []string{"Fidibeki", content.Heading}
	if content.Workspace != "" {
		parts = append(parts, "Workspace: "+content.Workspace)
	}
	for _, paragraph := range content.Body {
		if value := strings.TrimSpace(paragraph); value != "" {
			parts = append(parts, value)
		}
	}
	if len(content.Metadata) > 0 {
		lines := make([]string, 0, len(content.Metadata))
		for _, item := range content.Metadata {
			if strings.TrimSpace(item.Label) != "" && strings.TrimSpace(item.Value) != "" {
				lines = append(lines, strings.TrimSpace(item.Label)+": "+strings.TrimSpace(item.Value))
			}
		}
		if len(lines) > 0 {
			parts = append(parts, strings.Join(lines, "\n"))
		}
	}
	if content.Action != nil {
		parts = append(parts, content.Action.Label+": "+content.Action.URL)
	}
	footer := make([]string, 0, len(content.FooterLinks)+1)
	if content.Footer != "" {
		footer = append(footer, content.Footer)
	}
	for _, link := range content.FooterLinks {
		footer = append(footer, link.Label+": "+link.URL)
	}
	if len(footer) > 0 {
		parts = append(parts, strings.Join(footer, "\n"))
	}
	return strings.Join(parts, "\n\n")
}

var transactionalTemplate = template.Must(template.New("transactional").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="color-scheme" content="light"><meta name="supported-color-schemes" content="light"><title>{{.Heading}}</title></head>
<body style="margin:0;background:#f4f5f4;color:#161a17;font-family:Arial,Helvetica,sans-serif;color-scheme:light;supported-color-schemes:light;">
<div style="display:none;max-height:0;overflow:hidden;opacity:0;color:transparent;">{{.Preheader}}</div>
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background:#f4f5f4;padding:24px 12px;"><tr><td align="center">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="max-width:600px;background:#ffffff;border:1px solid #dfe3df;">
<tr><td style="padding:24px 28px 18px;border-bottom:1px solid #e8ebe8;">
<img src="{{.LogoURL}}" width="111" alt="Fidibeki" style="display:block;border:0;width:111px;max-width:111px;height:auto;font-family:Arial,Helvetica,sans-serif;font-size:18px;line-height:28px;font-weight:bold;color:#161a17;text-decoration:none;">
</td></tr>
<tr><td style="padding:28px;">
{{if .Workspace}}<p style="margin:0 0 10px;color:#55705f;font-size:13px;line-height:20px;font-weight:bold;">{{.Workspace}}</p>{{end}}
<h1 style="margin:0 0 18px;font-size:24px;line-height:32px;font-weight:700;color:#161a17;">{{.Heading}}</h1>
{{range .Body}}<p style="margin:0 0 16px;font-size:16px;line-height:25px;color:#343a35;">{{.}}</p>{{end}}
{{if .Metadata}}<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="margin:20px 0;border-top:1px solid #e8ebe8;">{{range .Metadata}}{{if and .Label .Value}}<tr><td style="padding:10px 12px 10px 0;color:#687169;font-size:13px;line-height:19px;vertical-align:top;">{{.Label}}</td><td style="padding:10px 0;color:#202622;font-size:14px;line-height:19px;vertical-align:top;">{{.Value}}</td></tr>{{end}}{{end}}</table>{{end}}
{{with .Action}}<p style="margin:24px 0 12px;"><a href="{{.URL}}" style="display:inline-block;background:#16794b;color:#ffffff;text-decoration:none;font-size:15px;line-height:20px;font-weight:bold;padding:12px 18px;border-radius:4px;">{{.Label}}</a></p>{{end}}
{{if and .Action .ShowActionURL}}<p style="margin:12px 0 0;color:#687169;font-size:12px;line-height:18px;word-break:break-all;">If the button does not work, open:<br><a href="{{.Action.URL}}" style="color:#176b47;">{{.Action.URL}}</a></p>{{end}}
</td></tr>
<tr><td style="padding:18px 28px;border-top:1px solid #e8ebe8;color:#687169;font-size:12px;line-height:18px;">
{{if .Footer}}<p style="margin:0 0 8px;">{{.Footer}}</p>{{end}}
{{if .FooterLinks}}<p style="margin:0;">{{range $index, $link := .FooterLinks}}{{if $index}} &middot; {{end}}<a href="{{$link.URL}}" style="color:#176b47;">{{$link.Label}}</a>{{end}}</p>{{end}}
</td></tr>
</table>
</td></tr></table>
</body></html>`))
