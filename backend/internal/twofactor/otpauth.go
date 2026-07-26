package twofactor

import (
	"net/url"
	"strconv"
)

// ProvisioningURI builds the otpauth URI defined by the Key Uri Format that
// every mainstream authenticator app reads from a QR code. The algorithm,
// digits, and period are stated explicitly so an app that defaults differently
// still enrols correctly.
func ProvisioningURI(issuer, account, secret string) string {
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", strconv.Itoa(Digits))
	query.Set("period", strconv.Itoa(PeriodSeconds))
	// The label is escaped per segment so a workspace name containing a slash
	// or a question mark cannot break out of it.
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	return "otpauth://totp/" + label + "?" + query.Encode()
}
