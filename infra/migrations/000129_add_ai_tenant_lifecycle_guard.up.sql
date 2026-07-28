CREATE FUNCTION fidibeki_ai_tenant_processing_eligible(
    tenant_status_value tenant_status,
    trial_ends_at_value timestamptz,
    deletion_requested_at_value timestamptz,
    evaluated_at timestamptz
) RETURNS boolean
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
    SELECT deletion_requested_at_value IS NULL
       AND (
           tenant_status_value = 'active'
           OR (
               tenant_status_value = 'trial'
               AND trial_ends_at_value IS NOT NULL
               AND trial_ends_at_value > evaluated_at
           )
       )
$$;

REVOKE ALL ON FUNCTION fidibeki_ai_tenant_processing_eligible(
    tenant_status,
    timestamptz,
    timestamptz,
    timestamptz
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION fidibeki_ai_tenant_processing_eligible(
    tenant_status,
    timestamptz,
    timestamptz,
    timestamptz
) TO fidibeki_app, fidibeki_admin;
