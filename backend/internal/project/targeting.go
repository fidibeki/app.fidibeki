package project

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
)

type CanonicalPage struct {
	Target   string
	Pathname string
}

func CanonicalizePage(rawURL string) (CanonicalPage, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || !parsed.IsAbs() {
		return CanonicalPage{}, fmt.Errorf("must be an absolute HTTP(S) URL")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return CanonicalPage{}, fmt.Errorf("must be an absolute HTTP(S) URL")
	}
	hostname, err := idna.Lookup.ToASCII(strings.ToLower(parsed.Hostname()))
	if err != nil || hostname == "" {
		return CanonicalPage{}, fmt.Errorf("must contain a valid hostname")
	}
	port := parsed.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	host := hostname
	if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	}
	pathname := parsed.EscapedPath()
	if pathname == "" {
		pathname = "/"
	}
	for strings.Contains(pathname, "//") {
		pathname = strings.ReplaceAll(pathname, "//", "/")
	}
	if pathname != "/" {
		pathname = strings.TrimRight(pathname, "/")
		if pathname == "" {
			pathname = "/"
		}
	}
	return CanonicalPage{Target: scheme + "://" + host + pathname, Pathname: pathname}, nil
}

type PrivacyLocationSource string

const (
	PrivacyLocationExact  PrivacyLocationSource = "exact"
	PrivacyLocationPrefix PrivacyLocationSource = "prefix"
	PrivacyLocationOrigin PrivacyLocationSource = "origin"
)

type PrivacySafeLocation struct {
	Target string
	Source PrivacyLocationSource
}

func ProjectPrivacySafeLocation(rawURL string, rule WidgetRule) (PrivacySafeLocation, error) {
	page, err := CanonicalizePage(rawURL)
	if err != nil {
		return PrivacySafeLocation{}, err
	}
	parsedPage, err := url.Parse(page.Target)
	if err != nil {
		return PrivacySafeLocation{}, fmt.Errorf("parse canonical page: %w", err)
	}
	origin := parsedPage.Scheme + "://" + parsedPage.Host
	conditions := rule.IncludeConditions
	if len(conditions) == 0 {
		conditions = []WidgetRuleTargetCondition{{
			MatcherType: rule.MatcherType, MatcherValue: rule.MatcherValue,
		}}
	}
	var exact, prefix string
	for _, condition := range conditions {
		if condition.MatcherType != MatcherURLExact && condition.MatcherType != MatcherURLPrefix ||
			condition.MatcherValue == nil {
			continue
		}
		candidate, candidateErr := CanonicalizePage(*condition.MatcherValue)
		if candidateErr != nil {
			continue
		}
		parsedCandidate, candidateErr := url.Parse(candidate.Target)
		if candidateErr != nil ||
			parsedCandidate.Scheme+"://"+parsedCandidate.Host != origin {
			continue
		}
		switch {
		case condition.MatcherType == MatcherURLExact && candidate.Target == page.Target:
			exact = candidate.Target
		case condition.MatcherType == MatcherURLPrefix &&
			strings.HasPrefix(page.Target, candidate.Target) &&
			len(candidate.Target) > len(prefix):
			prefix = candidate.Target
		}
	}
	if exact != "" {
		return PrivacySafeLocation{Target: exact, Source: PrivacyLocationExact}, nil
	}
	if prefix != "" {
		return PrivacySafeLocation{Target: prefix, Source: PrivacyLocationPrefix}, nil
	}
	return PrivacySafeLocation{Target: origin + "/", Source: PrivacyLocationOrigin}, nil
}
