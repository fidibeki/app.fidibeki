DROP INDEX IF EXISTS idx_testimonials_one_per_request;
ALTER TABLE testimonials DROP CONSTRAINT IF EXISTS testimonials_tenant_request_fkey;
ALTER TABLE testimonial_requests DROP CONSTRAINT IF EXISTS testimonial_requests_tenant_testimonial_fkey;
DROP TABLE IF EXISTS testimonial_forms;
DROP TABLE IF EXISTS testimonial_requests;
DROP TYPE IF EXISTS testimonial_request_kind;
DROP TYPE IF EXISTS testimonial_request_status;
