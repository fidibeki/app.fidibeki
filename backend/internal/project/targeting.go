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
	if pathname != "/" {
		pathname = strings.TrimRight(pathname, "/")
		if pathname == "" {
			pathname = "/"
		}
	}
	return CanonicalPage{Target: scheme + "://" + host + pathname, Pathname: pathname}, nil
}
