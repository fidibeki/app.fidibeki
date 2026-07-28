package middleware

import "net/http"

const StrictTransportSecurityPolicy = "max-age=31536000"

func StrictTransportSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", StrictTransportSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}
