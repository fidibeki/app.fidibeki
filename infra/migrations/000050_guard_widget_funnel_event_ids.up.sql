CREATE FUNCTION widget_funnel_event_id_exists(candidate_event_id uuid)
RETURNS boolean
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = pg_catalog, public
AS $$
    SELECT EXISTS (
        SELECT 1
        FROM public.widget_funnel_events
        WHERE event_id = candidate_event_id
    )
$$;

ALTER FUNCTION widget_funnel_event_id_exists(uuid) OWNER TO fidibeki_admin;
REVOKE ALL ON FUNCTION widget_funnel_event_id_exists(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION widget_funnel_event_id_exists(uuid) TO fidibeki_app;
