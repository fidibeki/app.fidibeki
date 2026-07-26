package privacy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"

	"github.com/acidiney/app.fidibeki/backend/internal/email"
)

func reporterVerificationMessage(
	request ReporterRequest,
	workspaceName string,
	workspaceSlug string,
	token string,
	options HTTPOptions,
) (email.Message, error) {
	verificationURL, err := reporterPrivacyURL(
		options.PublicBaseURL, options.BaseDomain, workspaceSlug, token,
	)
	if err != nil {
		return email.Message{}, err
	}
	logoURL, err := email.BrandLogoURL(options.PublicBaseURL)
	if err != nil {
		return email.Message{}, err
	}
	if request.ReporterEmail == nil {
		return email.Message{}, errors.New("reporter privacy email is unavailable")
	}
	action := "export your data"
	button := "Confirm data export"
	if request.Action == ReporterAnonymize {
		action = "anonymize your data"
		button = "Confirm anonymization"
	}
	digest := sha256.Sum256([]byte(token))
	return email.Compose(email.Message{
		From: options.From, To: *request.ReporterEmail,
		Subject:        "Confirm your Fidibeki data request",
		IdempotencyKey: "privacy-verification:" + request.ID + ":" + hex.EncodeToString(digest[:8]),
	}, email.Content{
		Preheader: "Confirm your data request",
		Heading:   "Confirm your data request",
		Workspace: workspaceName,
		Body: []string{
			workspaceName + " received a request to " + action + ".",
			"Use the link below within 48 hours. If you did not request this, you can ignore this email.",
		},
		Action:        &email.Link{Label: button, URL: verificationURL},
		ShowActionURL: true,
		Footer:        "The request will not run until control of this email address is confirmed.",
		LogoURL:       logoURL,
	})
}

func reporterPrivacyURL(publicBaseURL, baseDomain, slug, token string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(publicBaseURL))
	if err != nil || !base.IsAbs() || base.Host == "" || baseDomain == "" || slug == "" || token == "" {
		return "", errors.New("privacy request URL configuration is invalid")
	}
	port := base.Port()
	base.Host = slug + "." + baseDomain
	if port != "" {
		base.Host += ":" + port
	}
	base.Path = "/privacy/" + url.PathEscape(token)
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}

func privacyCaseURL(publicBaseURL, baseDomain, slug, token string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(publicBaseURL))
	if err != nil || !base.IsAbs() || base.Host == "" || baseDomain == "" || slug == "" || token == "" {
		return "", errors.New("privacy case URL configuration is invalid")
	}
	port := base.Port()
	base.Host = slug + "." + baseDomain
	if port != "" {
		base.Host += ":" + port
	}
	base.Path = "/privacy/cases/" + url.PathEscape(token)
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = ""
	return base.String(), nil
}
