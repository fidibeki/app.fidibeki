package tenant

import "strconv"

// OutboundDeliveryEligibleSQL returns the canonical tenant lifecycle predicate for SQL queries
// that send tenant data to an external provider. Callers must alias the tenants table as tenant.
func OutboundDeliveryEligibleSQL(nowParameter int) string {
	if nowParameter < 1 {
		panic("outbound delivery clock parameter must be positive")
	}
	return `(
	tenant.deletion_requested_at IS NULL
	AND (
		tenant.status = 'active'
		OR (tenant.status = 'trial' AND tenant.trial_ends_at > $` +
		strconv.Itoa(nowParameter) + `)
	)
)`
}
