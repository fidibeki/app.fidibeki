import {
  expect,
  test,
  type Page,
  type Route,
  type TestInfo,
} from "@playwright/test";
import { allTenantPermissions } from "../../data/permissions";
import type {
  PrivacyCase,
  PrivacyCaseCommunication,
  PrivacyIntegrationAction,
  RetentionPolicy,
} from "../../types/api";

const tenantOrigin = `http://acme.localhost:${process.env.PLAYWRIGHT_PORT || "4321"}`;
const now = "2026-07-21T12:00:00.000Z";

function mockRetentionPolicy(
  category: string,
  label: string,
  duration: string,
  status: RetentionPolicy["status"],
  source: string,
  overrides: Partial<RetentionPolicy> = {},
): RetentionPolicy {
  return {
    category,
    label,
    duration,
    source,
    purpose: `${label} lifecycle`,
    role: "processor",
    status,
    trigger: "Resource lifecycle",
    action:
      status === "decision_required"
        ? "Approved policy is not yet implemented."
        : status === "external_control"
          ? "Provider configuration must be verified."
          : "Expire the resource through its maintained lifecycle.",
    cadence:
      status === "decision_required"
        ? "Not scheduled"
        : status === "external_control"
          ? "Provider controlled"
          : "Daily",
    hold_behavior:
      status === "decision_required" || status === "external_control"
        ? "decision_required"
        : "not_applicable",
    implementation: ["enforced", "configurable"].includes(status)
      ? "test.Maintainer"
      : undefined,
    resource_counts: [{ kind: "postgres_table", count: 1 }],
    ...overrides,
  };
}

const planCatalog = [
  {
    plan: "starter",
    monthly_price_cents: 999,
    yearly_price_cents: 11148,
    yearly_discount_percent: 7,
    limits: {
      projects: 1,
      feedback_per_month: 500,
      users: 1,
      storage_days: 30,
    },
    features: {
      ai: false,
      session_replay: false,
      slack: false,
      video: false,
      audio: false,
      dom_capture: false,
      custom_domain: false,
    },
  },
  {
    plan: "growth",
    monthly_price_cents: 2999,
    yearly_price_cents: 30948,
    yearly_discount_percent: 14,
    limits: {
      projects: 4,
      feedback_per_month: 2500,
      users: 3,
      storage_days: 90,
    },
    features: {
      ai: true,
      session_replay: true,
      slack: false,
      video: false,
      audio: true,
      dom_capture: true,
      custom_domain: false,
    },
  },
  {
    plan: "scale",
    monthly_price_cents: 6799,
    yearly_price_cents: 70164,
    yearly_discount_percent: 14,
    limits: {
      projects: 10,
      feedback_per_month: 10000,
      users: 10,
      storage_days: 365,
    },
    features: {
      ai: true,
      session_replay: true,
      slack: true,
      video: true,
      audio: true,
      dom_capture: true,
      custom_domain: false,
    },
  },
];

type MockState = ReturnType<typeof createMockState>;

function createMockState() {
  const tenant = {
    id: "tenant-1",
    name: "Acme Studio",
    slug: "acme",
    plan: "growth",
    status: "trial",
    trial_ends_at: "2026-08-04T12:00:00.000Z",
    deletion_requested_at: null as string | null,
    reporter_access_enabled: true,
    widget_appearance_defaults: {} as Record<string, string>,
    short_name: "ACME" as string | null,
    website_url: "https://acme.test" as string | null,
    support_email: "support@acme.test" as string | null,
    default_locale: "en" as string | null,
    default_timezone: "UTC" as string | null,
    privacy_policy_url: null as string | null,
    privacy_contact_email: null as string | null,
    privacy_rights_email: null as string | null,
    privacy_profile_version: 0,
    terms_url: null as string | null,
    logo_version: null as string | null,
  };
  const member = {
    id: "member-1",
    tenant_id: tenant.id,
    email: "alex@acme.test",
    name: "Alex Morgan",
    avatar_version: null as string | null,
    locale: null as string | null,
    timezone: null as string | null,
    permissions: [...allTenantPermissions],
    status: "active" as "active" | "deactivated" | "removed",
    role: "owner" as "owner" | "member",
    verified: true,
  };
  const members = [
    member,
    {
      id: "member-2",
      tenant_id: tenant.id,
      email: "jamie@acme.test",
      name: "Jamie Torres",
      avatar_version: null as string | null,
      locale: "en" as string | null,
      timezone: "America/Toronto" as string | null,
      permissions: [
        "feedback:read",
        "feedback:update",
        "issue:read",
        "issue:update",
      ],
      status: "active" as "active" | "deactivated" | "removed",
      role: "member" as "owner" | "member",
      verified: true,
    },
  ];
  const memberDomains = [
    {
      id: "member-domain-1",
      tenant_id: tenant.id,
      domain: "acme.test",
      verification_email: "security@acme.test",
      verification_expires_at: "2026-07-21T12:10:00.000Z",
      verified_at: "2026-07-21T12:00:00.000Z" as string | undefined,
      status: "verified" as "pending" | "verified",
      created_at: now,
      updated_at: now,
    },
  ];
  const tags = [
    { id: "tag-checkout", name: "Checkout", color: "#167D68" },
    {
      id: "tag-regression",
      name: "Regression",
      color: "#7C3AED",
    },
  ];
  const projects = [
    {
      id: "project-1",
      tenant_id: tenant.id,
      name: "Storefront",
      public_key: "pk_live_storefront",
      allowed_domains: ["shop.acme.test", "*.preview.acme.test"],
      default_locale: "en",
      reporter_access_enabled: null as boolean | null,
      widget_appearance_overrides: {} as Record<string, string>,
      widget_last_seen_at: null as string | null,
      created_at: now,
      updated_at: now,
    },
  ];
  const widgetRules = [
    {
      id: "rule-1",
      project_id: projects[0].id,
      name: "Checkout pulse",
      priority: 10,
      enabled: true,
      definition_key: "checkout-pulse",
      definition_version: 1,
      definition_status: "active" as "draft" | "active" | "paused" | "archived",
      presentation: "prompt" as "launcher" | "inline" | "reaction" | "prompt",
      objective: "continuous_feedback" as
        | "continuous_feedback"
        | "after_engagement"
        | "validate_content"
        | "detect_friction",
      matcher_type: "url_prefix",
      matcher_value: "https://shop.acme.test/checkout",
      include_conditions: [
        {
          id: "target-include-1",
          matcher_type: "url_prefix",
          matcher_value: "https://shop.acme.test/checkout",
          position: 0,
        },
      ],
      exclude_conditions: [] as Array<{
        id: string;
        matcher_type: string;
        matcher_value: string | null;
        position: number;
      }>,
      trigger_type: "delay",
      trigger_delay_seconds: 4,
      trigger_scroll_percent: null,
      can_rate: true,
      can_write_text: true,
      can_record_audio: true,
      can_record_video: false,
      can_attach_element: true,
      can_attach_screenshot: true,
      greeting: "How was checkout?",
      allow_comment: false,
      collect_contact: false,
      localizations: {
        en: {
          launcher_label: "Feedback",
          prompt: "How was checkout?",
          comment_placeholder: "Tell us more",
          contact_label: "Email address",
          consent_label: "Email me updates about this feedback",
          submit_label: "Send feedback",
          success_message: "Your feedback was sent.",
          privacy_purpose_summary: "",
          privacy_lawful_basis_summary: "",
          privacy_retention_summary: "",
          block_labels: {},
          option_labels: {},
        },
      },
      position: "bottom-right",
      theme: "light",
      accent_color: "#087f73",
      replay_mode: "full",
      replay_sampling_rate: 0,
      replay_max_duration_seconds: 1800,
      replay_max_events: 50000,
      replay_mask_all_inputs: true,
      replay_mask_text_selector: ".private",
      replay_record_canvas: false,
      replay_canvas_fps: 2,
      replay_consent: {
        mode: "customer_cmp" as "none" | "fidibeki_managed" | "customer_cmp",
        notice_version: "2026-07",
        controller_name: "",
        privacy_policy_url: "",
        supported_locales: ["en"],
        retention_days: 0,
        expiry_days: 30,
        notices: {},
      },
      recurrence_mode: "cooldown" as "cooldown" | "session" | "never",
      recurrence_days: 30 as number | null,
      appearance_overrides: {
        position: "bottom-right",
        theme: "light",
        accent_color: "#087f73",
      } as Record<string, string>,
      created_at: now,
      updated_at: now,
    },
  ];
  const feedback = [
    {
      id: "feedback-1",
      public_id: "FB-1048",
      project_id: projects[0].id,
      project_name: projects[0].name,
      widget_rule_id: widgetRules[0].id,
      session_id: "session-1",
      issue_id: "issue-1",
      issue_public_id: "ISS-101",
      issue_status: "open",
      rating: 2,
      title: "Checkout froze after I applied a promotion code.",
      description: "Checkout froze after I applied a promotion code.",
      text: "Checkout froze after I applied a promotion code.",
      text_cleaned: "Checkout froze after I applied a promotion code.",
      reporter_email: "customer@example.test",
      notify_reporter: true,
      source: "web" as "web" | "ios" | "android",
      mobile_application_id: null as string | null,
      mobile_screen: null as string | null,
      mobile_app_version: null as string | null,
      mobile_os_version: null as string | null,
      page_url: "https://shop.acme.test/checkout",
      referrer: "https://shop.acme.test/cart",
      user_agent: "Mozilla/5.0",
      os: "macOS",
      browser: "Chrome",
      device: "desktop",
      viewport_width: 1280,
      viewport_height: 720,
      language: "en-US",
      status: "open",
      ai_status: "completed",
      priority: "high" as "low" | "medium" | "high" | "urgent" | null,
      assignee: {
        id: member.id,
        name: member.name,
        email: member.email,
        avatar_version: member.avatar_version,
      } as {
        id: string;
        name?: string;
        email: string;
        avatar_version?: string | null;
      } | null,
      tags: [tags[0]],
      sentiment_score: -0.74,
      moderation_flags: [],
      attachment_kinds: ["screenshot", "replay"],
      has_replay: true,
      created_at: "2026-07-21T10:00:00.000Z",
      updated_at: now,
    },
    {
      id: "feedback-2",
      public_id: "FB-1047",
      project_id: projects[0].id,
      project_name: projects[0].name,
      rating: 5,
      title: "The new checkout is much clearer.",
      description: "The new checkout is much clearer.",
      text: "The new checkout is much clearer.",
      text_cleaned: "The new checkout is much clearer.",
      notify_reporter: false,
      source: "web" as "web" | "ios" | "android",
      mobile_application_id: null as string | null,
      mobile_screen: null as string | null,
      mobile_app_version: null as string | null,
      mobile_os_version: null as string | null,
      page_url: "https://shop.acme.test/checkout/success",
      status: "open",
      ai_status: "completed",
      priority: null as "low" | "medium" | "high" | "urgent" | null,
      assignee: null as {
        id: string;
        name?: string;
        email: string;
        avatar_version?: string | null;
      } | null,
      tags: [] as typeof tags,
      sentiment_score: 0.86,
      moderation_flags: [],
      attachment_kinds: [],
      has_replay: false,
      created_at: "2026-07-21T09:00:00.000Z",
      updated_at: now,
    },
  ];
  const issues = [
    {
      id: "issue-1",
      tenant_id: tenant.id,
      project_id: projects[0].id,
      public_id: "ISS-101",
      title: "Promotion code can freeze checkout",
      description:
        "Customers cannot complete payment after applying some promotion codes.",
      status: "open",
      priority: "medium" as "low" | "medium" | "high" | "urgent" | null,
      assignee: {
        id: member.id,
        name: member.name,
        email: member.email,
        avatar_version: member.avatar_version,
      } as {
        id: string;
        name?: string;
        email: string;
        avatar_version?: string | null;
      } | null,
      tags: [tags[0]],
      reporter_email: "customer@example.test",
      notify_reporter: true,
      reporter_access_revoked_at: null as string | null,
      feedback_count: 3,
      created_at: "2026-07-19T12:00:00.000Z",
      updated_at: now,
      merged_into_id: undefined as string | undefined,
    },
    {
      id: "issue-2",
      tenant_id: tenant.id,
      project_id: projects[0].id,
      public_id: "ISS-099",
      title: "Checkout stalls during promotion validation",
      description:
        "Promotion validation can leave the checkout waiting indefinitely.",
      status: "in_progress",
      priority: null as "low" | "medium" | "high" | "urgent" | null,
      assignee: null as {
        id: string;
        name?: string;
        email: string;
        avatar_version?: string | null;
      } | null,
      tags: [] as typeof tags,
      reporter_email: "ops@acme.test",
      notify_reporter: false,
      reporter_access_revoked_at: null as string | null,
      feedback_count: 7,
      created_at: "2026-07-16T12:00:00.000Z",
      updated_at: now,
      merged_into_id: undefined as string | undefined,
    },
  ];
  return {
    tenant,
    member,
    members,
    memberDomains,
    tags,
    projects,
    widgetRules,
    feedback,
    issues,
    issueUpdates: [] as Array<{
      id: string;
      issue_id: string;
      member_id?: string;
      old_status?: string;
      new_status: string;
      comment?: string;
      created_at: string;
    }>,
    issueComments: [] as Array<{
      id: string;
      issue_id: string;
      author: {
        id: string;
        name?: string;
        email: string;
        avatar_version?: string | null;
      };
      body: string;
      mentions: Array<{
        id: string;
        name?: string;
        email: string;
        avatar_version?: string | null;
      }>;
      created_at: string;
    }>,
    issueActivities: [] as Array<{
      id: string;
      issue_id: string;
      actor: {
        id: string;
        name?: string;
        email: string;
        avatar_version?: string | null;
      };
      kind: string;
      visibility: "internal" | "reporter";
      data: Record<string, unknown>;
      created_at: string;
    }>,
    memberNotifications: [] as Array<{
      id: string;
      recipient_member_id: string;
      kind: string;
      issue_id: string;
      public_id: string;
      issue_title: string;
      actor: {
        id: string;
        name?: string;
        email: string;
        avatar_version?: string | null;
      };
      body: string;
      read_at?: string;
      created_at: string;
    }>,
    apiKeys: [] as Array<Record<string, unknown>>,
    integrations: [] as Array<Record<string, unknown>>,
    destructiveHold: null as Promise<void> | null,
    destructiveRequests: [] as string[],
    privacyExports: [
      {
        id: "privacy-export-1",
        kind: "workspace_export" as const,
        status: "completed" as
          | "pending"
          | "processing"
          | "completed"
          | "failed"
          | "expired",
        attempts: 1,
        completed_at: "2026-07-21T12:04:00.000Z" as string | undefined,
        expires_at: "2026-07-28T12:04:00.000Z" as string | undefined,
        object_size_bytes: 1835008 as number | undefined,
        created_at: "2026-07-21T12:00:00.000Z",
        updated_at: "2026-07-21T12:04:00.000Z",
      },
    ],
    privacyRequests: [
      {
        id: "privacy-request-1",
        action: "anonymize" as "export" | "anonymize",
        status: "failed" as
          | "pending_verification"
          | "processing"
          | "awaiting_manual_review"
          | "completed"
          | "failed"
          | "expired",
        reporter_email: "former.customer@example.test" as string | undefined,
        verification_expires_at: "2026-07-23T12:00:00.000Z",
        verified_at: "2026-07-21T12:02:00.000Z" as string | undefined,
        access_expires_at: "2026-08-20T12:02:00.000Z" as string | undefined,
        job_id: "privacy-job-reporter-1" as string | undefined,
        job_status: "failed" as
          | "pending"
          | "processing"
          | "awaiting_manual_review"
          | "completed"
          | "failed"
          | "expired"
          | undefined,
        manual_review_count: 2 as number | undefined,
        created_at: "2026-07-21T12:00:00.000Z",
        updated_at: "2026-07-21T12:05:00.000Z",
      },
    ],
    privacyCases: [
      {
        id: "privacy-case-access",
        public_id: "PRV-ACCESS1",
        subject_type: "reporter" as "reporter" | "member",
        right_type: "access" as "access" | "erasure",
        controller_role: "customer_controller_assistance" as const,
        status: "in_progress" as
          | "pending_verification"
          | "in_progress"
          | "completed",
        subject_email: "access.requester@example.test" as string | undefined,
        subject_member_id: undefined as string | undefined,
        requested_by_member_id: "member-1",
        owner_member_id: "member-1" as string | undefined,
        verification_expires_at: "2026-07-23T12:00:00.000Z",
        verification_method: "email_link",
        verified_at: "2026-07-21T12:02:00.000Z",
        received_at: "2026-07-21T12:00:00.000Z",
        deadline_at: "2026-08-20T12:00:00.000Z",
        paused_seconds: 0,
        scope: { intake_channel: "backoffice" },
        legal_holds: [] as Array<{
          id: string;
          privacy_case_id: string;
          reason: string;
          legal_reference?: string;
          active: boolean;
          placed_by_member_id: string;
          placed_at: string;
          released_by_member_id?: string;
          released_at?: string;
          release_reason?: string;
          created_at: string;
          updated_at: string;
        }>,
        execution: {
          right_type: "access" as "access" | "erasure",
          request_status: "completed" as "processing" | "completed" | "failed",
          job_id: "privacy-case-access-job",
          job_status: "completed" as
            | "pending"
            | "processing"
            | "completed"
            | "failed",
          job_expires_at: "2026-07-28T12:04:00.000Z",
          record_count: 12,
          manual_review_count: 0,
          started_at: "2026-07-21T12:02:00.000Z",
          completed_at: "2026-07-21T12:04:00.000Z",
        } as
          | {
              right_type: "access" | "erasure";
              request_status: "processing" | "completed" | "failed";
              job_id?: string;
              job_status?: "pending" | "processing" | "completed" | "failed";
              job_expires_at?: string;
              record_count?: number;
              manual_review_count?: number;
              started_at: string;
              completed_at?: string;
            }
          | undefined,
        created_at: "2026-07-21T12:00:00.000Z",
        updated_at: "2026-07-21T12:04:00.000Z",
      },
      {
        id: "privacy-case-erasure",
        public_id: "PRV-ERASE1",
        subject_type: "reporter" as "reporter" | "member",
        right_type: "erasure" as "access" | "erasure",
        controller_role: "customer_controller_assistance" as const,
        status: "in_progress" as
          | "pending_verification"
          | "in_progress"
          | "completed",
        subject_email: "erase.requester@example.test" as string | undefined,
        subject_member_id: undefined as string | undefined,
        requested_by_member_id: "member-1",
        owner_member_id: "member-1" as string | undefined,
        verification_expires_at: "2026-07-23T12:00:00.000Z",
        verification_method: "email_link",
        verified_at: "2026-07-21T12:02:00.000Z",
        received_at: "2026-07-21T12:00:00.000Z",
        deadline_at: "2026-08-20T12:00:00.000Z",
        paused_seconds: 0,
        scope: { intake_channel: "backoffice" },
        legal_holds: [],
        execution: undefined,
        created_at: "2026-07-21T12:00:00.000Z",
        updated_at: "2026-07-21T12:00:00.000Z",
      },
      {
        id: "privacy-case-erasure-decision",
        public_id: "PRV-ERASE2",
        subject_type: "reporter",
        right_type: "erasure",
        controller_role: "customer_controller_assistance",
        status: "in_progress",
        subject_email: "partial.erase@example.test",
        requested_by_member_id: "member-1",
        owner_member_id: "member-1",
        verification_expires_at: "2026-07-23T12:00:00.000Z",
        verification_method: "email_link",
        verified_at: "2026-07-21T12:02:00.000Z",
        received_at: "2026-07-21T12:00:00.000Z",
        deadline_at: "2026-08-20T12:00:00.000Z",
        paused_seconds: 0,
        scope: { intake_channel: "backoffice" },
        legal_holds: [],
        execution: {
          right_type: "erasure",
          request_status: "completed",
          job_id: "privacy-job-erasure-decision",
          job_status: "completed",
          record_count: 7,
          manual_review_count: 0,
          started_at: "2026-07-21T12:02:00.000Z",
          completed_at: "2026-07-21T12:04:00.000Z",
          erasure_review: {
            pending: 0,
            redacted: 2,
            deleted: 4,
            not_personal_data: 0,
            retained_exception: 1,
            items: [
              {
                id: "privacy-review-erasure-exception",
                privacy_job_id: "privacy-job-erasure-decision",
                resource_type: "feedback",
                resource_id: "feedback-contract-dispute",
                fields: ["description"],
                status: "retained_exception",
                decision_reason:
                  "Required for the documented contractual dispute.",
                decided_by_member_id: "member-1",
                decided_at: "2026-07-21T12:04:00.000Z",
                created_at: "2026-07-21T12:03:00.000Z",
                updated_at: "2026-07-21T12:04:00.000Z",
              },
            ],
          },
        },
        created_at: "2026-07-21T12:00:00.000Z",
        updated_at: "2026-07-21T12:04:00.000Z",
      },
      {
        id: "privacy-case-portability",
        public_id: "PRV-PORT1",
        subject_type: "reporter",
        right_type: "portability_assessment",
        controller_role: "customer_controller_assistance",
        status: "in_progress",
        subject_email: "portable.requester@example.test",
        requested_by_member_id: "member-1",
        owner_member_id: "member-1",
        verification_expires_at: "2026-07-23T12:00:00.000Z",
        verification_method: "email_link",
        verified_at: "2026-07-21T12:02:00.000Z",
        received_at: "2026-07-21T12:00:00.000Z",
        deadline_at: "2026-08-20T12:00:00.000Z",
        paused_seconds: 0,
        scope: { intake_channel: "backoffice" },
        legal_holds: [],
        execution: undefined,
        portability_assessment: undefined,
        created_at: "2026-07-21T12:00:00.000Z",
        updated_at: "2026-07-21T12:00:00.000Z",
      },
      {
        id: "privacy-case-member-access",
        public_id: "PRV-MEMBER1",
        subject_type: "member",
        right_type: "access",
        controller_role: "controller",
        status: "in_progress",
        subject_member_id: "member-2",
        requested_by_member_id: "member-1",
        owner_member_id: "member-1",
        verification_method: "workspace_membership",
        verified_at: "2026-07-21T12:00:00.000Z",
        received_at: "2026-07-21T12:00:00.000Z",
        deadline_at: "2026-08-20T12:00:00.000Z",
        paused_seconds: 0,
        scope: { intake_channel: "backoffice" },
        legal_holds: [],
        execution: undefined,
        created_at: "2026-07-21T12:00:00.000Z",
        updated_at: "2026-07-21T12:00:00.000Z",
      },
      {
        id: "privacy-case-member-rectification",
        public_id: "PRV-RECTIFY",
        subject_type: "member",
        right_type: "rectification",
        controller_role: "controller",
        status: "in_progress",
        subject_member_id: "member-2",
        requested_by_member_id: "member-1",
        owner_member_id: "member-1",
        verification_method: "workspace_membership",
        verified_at: "2026-07-21T12:00:00.000Z",
        received_at: "2026-07-21T12:00:00.000Z",
        deadline_at: "2026-08-20T12:00:00.000Z",
        paused_seconds: 0,
        scope: { intake_channel: "backoffice" },
        legal_holds: [],
        rectifications: [],
        integration_actions: [],
        recipient_actions: [],
        created_at: "2026-07-21T12:00:00.000Z",
        updated_at: "2026-07-21T12:00:00.000Z",
      },
      {
        id: "privacy-case-feedback-rectification",
        public_id: "PRV-FEEDBACK",
        subject_type: "reporter",
        right_type: "rectification",
        controller_role: "customer_controller_assistance",
        status: "in_progress",
        subject_email: "customer@example.test",
        requested_by_member_id: "member-1",
        owner_member_id: "member-1",
        verification_expires_at: "2026-07-23T12:00:00.000Z",
        verification_method: "email_link",
        verified_at: "2026-07-21T12:02:00.000Z",
        received_at: "2026-07-21T12:00:00.000Z",
        deadline_at: "2026-08-20T12:00:00.000Z",
        paused_seconds: 0,
        scope: { intake_channel: "backoffice" },
        legal_holds: [],
        feedback_rectifications: [],
        created_at: "2026-07-21T12:00:00.000Z",
        updated_at: "2026-07-21T12:00:00.000Z",
      },
    ] as PrivacyCase[],
    reviewItems: [] as Array<{
      id: string;
      resource_type:
        | "feedback"
        | "issue"
        | "issue_comment"
        | "clarification_request"
        | "legacy_unlinked";
      resource_id: string;
      fields: string[];
      status:
        | "pending"
        | "redacted"
        | "deleted"
        | "not_personal_data"
        | "retained_exception";
      decision_reason?: string;
      decided_by_member_id?: string;
      decided_at?: string;
      disclosed_destinations?: Record<string, number>;
      created_at: string;
      updated_at: string;
    }>,
    publicPrivacyRequest: {
      action: "export" as "export" | "anonymize",
      status: "completed" as
        | "pending_verification"
        | "processing"
        | "awaiting_manual_review"
        | "completed"
        | "failed"
        | "expired",
      completed_at: "2026-07-21T12:04:00.000Z" as string | undefined,
      record_count: 14,
      manual_review_count: 0 as number | undefined,
      job_status: "completed" as
        | "pending"
        | "processing"
        | "awaiting_manual_review"
        | "completed"
        | "failed"
        | "expired",
      download_available: true,
      access_expires_at: "2026-08-20T12:02:00.000Z",
    },
    privacyDownloadCount: 0,
    privacyVerifyCount: 0,
    accessEvents: [
      {
        id: "access-event-1",
        kind: "feedback_attachment.accessed",
        actor_type: "human" as "human" | "api_key" | "platform_admin",
        actor_member_id: "member-owner",
        resource_type: "feedback_attachment",
        resource_id: "attachment-1",
        action: "preview",
        permission_path: "feedback:read",
        outcome: "success" as "success" | "denied" | "not_found",
        data: {},
        created_at: "2026-07-21T12:00:00.000Z",
      },
      {
        id: "access-event-2",
        kind: "replay_session.accessed",
        actor_type: "api_key" as "human" | "api_key" | "platform_admin",
        actor_api_key_id: "api-key-1",
        resource_type: "replay_session",
        resource_id: "session-1",
        action: "playback",
        permission_path: "session:read",
        outcome: "denied" as "success" | "denied" | "not_found",
        data: {},
        created_at: "2026-07-21T11:55:00.000Z",
      },
      {
        id: "access-event-3",
        kind: "feedback_attachment.support_accessed",
        actor_type: "platform_admin" as "human" | "api_key" | "platform_admin",
        actor_platform_admin_id: "platform-admin-1",
        resource_type: "feedback_attachment",
        resource_id: "attachment-2",
        action: "preview",
        permission_path: "platform_admin:support_access",
        outcome: "success" as "success" | "denied" | "not_found",
        data: {
          support_reason: "Investigating a reported checkout bug",
          support_case_reference: "CASE-42",
        },
        created_at: "2026-07-21T11:50:00.000Z",
      },
    ],
    accessEventSignals: [] as Array<{
      type:
        | "bulk_access"
        | "revoked_credential"
        | "unusual_api_key"
        | "support_access_volume";
      actor_type: "human" | "api_key" | "platform_admin";
      actor_id: string;
      event_count: number;
      distinct_resources?: number;
      denied_count?: number;
      detail?: string;
      last_access_at: string;
    }>,
    privacyPublicUnavailable: false,
    memberInviteFailure: null as null | {
      status: number;
      code: string;
      message: string;
    },
    billingDeletionRequestedAt: null as string | null,
    failedReplayFeedbackIDs: new Set<string>(),
    failNextBulkFeedbackUpdate: false,
    bulkFeedbackRequests: [] as Array<Record<string, unknown>>,
    analyticsMode: "data" as "data" | "empty" | "error",
    analyticsDelayMs: 0,
    analyticsPagesTotal: 2,
    analyticsRequests: [] as string[],
    submissionHealthMode: "data" as "data" | "empty" | "error",
    submissionHealthRequests: [] as string[],
    reporterResponse: null as string | null,
    lastReporterMessage: null as string | null,
    workspaceAccess: "available" as
      | "available"
      | "unknown"
      | "suspended"
      | "error",
    failNextIdentityUpload: false,
    failIdentityAssets: false,
    failNextAppearanceUpdate: false,
    failNextReporterAccessUpdate: false,
    nextWidgetRuleFieldErrors: null as Record<string, string> | null,
  };
}

function analyticsResponse(state: MockState, url: URL) {
  const empty = state.analyticsMode === "empty";
  const range = (url.searchParams.get("range") || "30d") as
    | "7d"
    | "30d"
    | "90d";
  const rangeDays = Number(range.slice(0, -1));
  const projectId = url.searchParams.get("project_id") || "";
  const ruleId = url.searchParams.get("rule_id") || "";
  const page = url.searchParams.get("page") || "";
  const dates = Array.from({ length: rangeDays }, (_, index) => {
    const date = new Date("2026-07-21T00:00:00.000Z");
    date.setUTCDate(date.getUTCDate() - (rangeDays - index - 1));
    return date.toISOString().slice(0, 10);
  });
  const counts = empty
    ? {
        matched: 0,
        impressions: 0,
        interactions: 0,
        submissions: 0,
        dismissals: 0,
        impression_rate: null,
        interaction_rate: null,
        completion_rate: null,
        submit_rate: null,
      }
    : {
        matched: 1840,
        impressions: 1240,
        interactions: 496,
        submissions: 186,
        dismissals: 94,
        impression_rate: 0.674,
        interaction_rate: 0.4,
        completion_rate: 0.375,
        submit_rate: 0.15,
      };
  const row = <T extends Record<string, unknown>>(overrides: T) => ({
    ...counts,
    ...overrides,
  });
  const projects = empty
    ? []
    : [row({ project_id: "project-1", project_name: "Storefront" })];
  const rules = empty
    ? []
    : [
        row({
          rule_id: "rule-1",
          rule_name: "Checkout pulse",
          project_id: "project-1",
          project_name: "Storefront",
          deleted: false,
        }),
        row({
          rule_id: "rule-deleted",
          rule_name: "Spring launch",
          project_id: "project-1",
          project_name: "Storefront",
          deleted: true,
          impressions: 310,
          interactions: 93,
          submissions: 21,
          interaction_rate: 0.3,
          completion_rate: 0.226,
          submit_rate: 0.068,
        }),
      ].filter((candidate) => !ruleId || candidate.rule_id === ruleId);
  const pages = empty
    ? []
    : [
        row({ page_url: "https://shop.acme.test/checkout" }),
        row({
          page_url: "https://shop.acme.test/checkout/success",
          impressions: 280,
          interactions: 168,
          submissions: 126,
          interaction_rate: 0.6,
          completion_rate: 0.75,
          submit_rate: 0.45,
        }),
      ].filter((candidate) => !page || candidate.page_url === page);
  return {
    range,
    period: { start_date: dates[0], end_date: dates.at(-1), timezone: "UTC" },
    filters: {
      project_id: projectId || undefined,
      rule_id: ruleId || undefined,
      page: page || undefined,
    },
    summary: counts,
    trend: dates.map((date, index) => ({
      date,
      impressions: empty ? 0 : 22 + (index % 6) * 9,
      interactions: empty ? 0 : 8 + (index % 5) * 4,
      submissions: empty ? 0 : 2 + (index % 4) * 2,
    })),
    projects,
    rules,
    pages,
    options: {
      projects: [{ id: "project-1", name: "Storefront" }],
      rules: [
        {
          id: "rule-1",
          project_id: "project-1",
          name: "Checkout pulse",
          deleted: false,
        },
        {
          id: "rule-deleted",
          project_id: "project-1",
          name: "Spring launch",
          deleted: true,
        },
      ],
      pages: [
        { url: "https://shop.acme.test/checkout" },
        { url: "https://shop.acme.test/checkout/success" },
      ],
    },
    metadata: {
      projects: {
        total: projects.length,
        returned: projects.length,
        limit: 100,
        truncated: false,
      },
      rules: {
        total: rules.length,
        returned: rules.length,
        limit: 100,
        truncated: false,
      },
      pages: {
        total: state.analyticsPagesTotal,
        returned: pages.length,
        limit: 100,
        truncated: state.analyticsPagesTotal > pages.length,
      },
      project_options: { total: 1, returned: 1, limit: 200, truncated: false },
      rule_options: { total: 2, returned: 2, limit: 200, truncated: false },
      page_options: { total: 2, returned: 2, limit: 200, truncated: false },
    },
  };
}

function submissionHealthResponse(state: MockState, url: URL) {
  const empty = state.submissionHealthMode === "empty";
  const range = (url.searchParams.get("range") || "7d") as "24h" | "7d" | "30d";
  const projectID = url.searchParams.get("project_id") || "";
  const startAt =
    range === "24h"
      ? "2026-07-22T13:00:00.000Z"
      : range === "30d"
        ? "2026-06-23T13:00:00.000Z"
        : "2026-07-16T13:00:00.000Z";
  const totals = [
    { category: "rate_limit", count: empty ? 0 : 17 },
    { category: "spam", count: empty ? 0 : 2 },
    { category: "plan_quota", count: 0 },
    { category: "upload_safety", count: empty ? 0 : 5 },
    { category: "ai_budget", count: empty ? 0 : 8 },
    { category: "provider", count: empty ? 0 : 3 },
  ];
  const trend = empty
    ? []
    : [
        {
          project_id: "project-1",
          bucket_start: "2026-07-22T14:00:00.000Z",
          category: "rate_limit",
          reason: "submission_rate_limited",
          count: 11,
        },
        {
          project_id: "project-1",
          bucket_start: "2026-07-22T17:00:00.000Z",
          category: "rate_limit",
          reason: "upload_rate_limited",
          count: 6,
        },
        {
          project_id: "project-1",
          bucket_start: "2026-07-22T19:00:00.000Z",
          category: "spam",
          reason: "spam_rejected",
          count: 2,
        },
        {
          project_id: "project-1",
          bucket_start: "2026-07-23T02:00:00.000Z",
          category: "upload_safety",
          reason: "signature_mismatch",
          count: 5,
        },
        {
          project_id: "project-1",
          bucket_start: "2026-07-23T08:00:00.000Z",
          category: "ai_budget",
          reason: "ai_daily_budget_exhausted",
          count: 8,
        },
        {
          project_id: "project-1",
          bucket_start: "2026-07-23T12:00:00.000Z",
          category: "provider",
          reason: "provider_timeout",
          count: 3,
        },
      ];
  return {
    range,
    period: {
      start_at: startAt,
      end_at: "2026-07-23T13:00:00.000Z",
      timezone: "UTC",
    },
    filters: { project_id: projectID || undefined },
    total: totals.reduce((sum, item) => sum + item.count, 0),
    totals,
    trend,
  };
}

function authResponse(state: MockState) {
  return {
    access_token: "test-access-token",
    token_type: "Bearer",
    expires_in: 900,
    member: state.member,
    tenant: state.tenant,
  };
}

function feedbackDetail(state: MockState, id: string) {
  const item =
    state.feedback.find((candidate) => candidate.id === id) ||
    state.feedback[0];
  const replayFailed = state.failedReplayFeedbackIDs.has(id);
  return {
    ...item,
    dom_snapshot_html: undefined,
    dom_snapshot_styles: undefined,
    attachments:
      id === "feedback-1"
        ? [
            {
              id: "attachment-1",
              kind: "screenshot",
              content_type: "image/png",
              size_bytes: 2120,
              processing_status: "failed",
              safety_status: "safe",
              error_message: "Screenshot analysis timed out",
              moderation_flags: [],
              processing_attempts: 1,
              completed_at: now,
              processed_at: now,
              created_at: now,
              content_url: `/v1/feedbacks/${id}/attachments/attachment-1/content`,
            },
            {
              id: "attachment-2",
              kind: "audio",
              content_type: "audio/webm",
              size_bytes: 4096,
              processing_status: "failed",
              safety_status: "expired",
              safety_error_reason: "upload_expired",
              error_message: "attachment upload expired before completion",
              moderation_flags: [],
              processing_attempts: 0,
              processed_at: now,
              created_at: now,
            },
          ]
        : [],
    session:
      id === "feedback-1"
        ? {
            id: "session-1",
            status: replayFailed ? "failed" : "ready",
            page_url: item.page_url,
            started_at: "2026-07-21T09:58:00.000Z",
            ended_at: "2026-07-21T10:00:00.000Z",
            events_count: replayFailed ? 0 : 3,
            batches_count: replayFailed ? 0 : 1,
            rrweb_version: "2.0",
            replay_ready: !replayFailed,
            replay_url: replayFailed
              ? undefined
              : "/v1/sessions/session-1/replay",
            merge_error: replayFailed
              ? "No replay events were uploaded before the recording ended."
              : undefined,
          }
        : undefined,
    ai_processed_at: now,
  };
}

const replayEvents = [
  {
    type: 4,
    data: { href: "https://shop.acme.test/checkout", width: 1280, height: 720 },
    timestamp: 1_000,
  },
  {
    type: 2,
    data: {
      node: {
        type: 0,
        id: 1,
        childNodes: [
          { type: 1, name: "html", publicId: "", systemId: "", id: 2 },
          {
            type: 2,
            tagName: "html",
            attributes: {},
            id: 3,
            childNodes: [
              {
                type: 2,
                tagName: "head",
                attributes: {},
                id: 4,
                childNodes: [],
              },
              {
                type: 2,
                tagName: "body",
                attributes: {
                  style: "font-family:Arial;padding:32px;background:#fff",
                },
                id: 5,
                childNodes: [
                  {
                    type: 2,
                    tagName: "h1",
                    attributes: {},
                    id: 6,
                    childNodes: [{ type: 3, textContent: "Checkout", id: 7 }],
                  },
                  {
                    type: 2,
                    tagName: "p",
                    attributes: {},
                    id: 8,
                    childNodes: [
                      { type: 3, textContent: "Review your order", id: 9 },
                    ],
                  },
                  {
                    type: 2,
                    tagName: "canvas",
                    attributes: {
                      id: "replay-canvas",
                      width: "4",
                      height: "4",
                      rr_dataURL:
                        "data:image/svg+xml,%3Csvg xmlns=%22http://www.w3.org/2000/svg%22 width=%224%22 height=%224%22%3E%3Crect width=%224%22 height=%224%22 fill=%22%23e53935%22/%3E%3C/svg%3E",
                    },
                    id: 10,
                    childNodes: [],
                  },
                ],
              },
            ],
          },
        ],
      },
      initialOffset: { left: 0, top: 0 },
    },
    timestamp: 1_001,
  },
  { type: 1, data: {}, timestamp: 1_050 },
];

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  });
}

// Keeps every destructive request unanswered until the returned release runs, so
// a test can assert the pending UI without racing the response. A fixed delay
// cannot do that: a role-based lookup has to build the accessibility tree, and on
// a loaded CI runner that alone outlasts any window short enough to be worth
// waiting for.
function holdDestructiveRequests(state: MockState) {
  let release = () => {};
  state.destructiveHold = new Promise<void>((resolve) => {
    release = resolve;
  });
  return () => {
    state.destructiveHold = null;
    release();
  };
}

async function installMockAPI(page: Page, state: MockState) {
  await page.route("https://downloads.example.test/**", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/zip",
      headers: {
        "content-disposition": 'attachment; filename="fidibeki-export.zip"',
      },
      body: "PK",
    }),
  );
  await page.route(/^https?:\/\/[^/]+\/(?:auth|v1)\//, async (route) => {
    const request = route.request();
    if (request.resourceType() === "document") return route.continue();
    const url = new URL(request.url());
    const path = url.pathname;
    const method = request.method();
    const body = () => request.postDataJSON() as Record<string, unknown>;

    if (path === "/auth/login" && method === "POST")
      return json(route, {}, 202);
    if (
      (path === "/auth/verify" || path === "/auth/refresh") &&
      method === "POST"
    )
      return json(route, authResponse(state));
    if (path === "/auth/member-domains/verify" && method === "POST")
      return json(route, {
        member_domain: state.memberDomains[0],
      });
    if (path === "/auth/logout" && method === "POST") return json(route, {});

    const identityPNG = Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M/wHwAEAQH/2p7iWQAAAABJRU5ErkJggg==",
      "base64",
    );
    if (
      /^\/v1\/public\/workspace-logo\/[^/]+\/[^/]+\/(?:64|256)\.png$/.test(
        path,
      ) &&
      method === "GET"
    ) {
      if (state.failIdentityAssets)
        return route.fulfill({ status: 404, body: "" });
      return route.fulfill({
        status: 200,
        contentType: "image/png",
        body: identityPNG,
      });
    }
    if (
      /^\/v1\/members\/[^/]+\/avatar\/[^/]+\/(?:64|256)\.png$/.test(path) &&
      method === "GET"
    ) {
      if (state.failIdentityAssets)
        return route.fulfill({ status: 404, body: "" });
      return route.fulfill({
        status: 200,
        contentType: "image/png",
        body: identityPNG,
      });
    }
    if (path === "/v1/organization-profile" && method === "GET") {
      return json(route, {
        organization_profile: {
          name: state.tenant.name,
          short_name: state.tenant.short_name,
          website_url: state.tenant.website_url,
          support_email: state.tenant.support_email,
          accent_color:
            state.tenant.widget_appearance_defaults.accent_color || null,
          default_locale: state.tenant.default_locale,
          default_timezone: state.tenant.default_timezone,
          privacy_policy_url: state.tenant.privacy_policy_url,
          privacy_contact_email: state.tenant.privacy_contact_email,
          privacy_rights_email: state.tenant.privacy_rights_email,
          privacy_profile_version: state.tenant.privacy_profile_version,
          terms_url: state.tenant.terms_url,
          logo_version: state.tenant.logo_version,
        },
      });
    }
    if (path === "/v1/organization-profile" && method === "PATCH") {
      const input = body();
      const controllerChanged =
        state.tenant.name !== String(input.name || "") ||
        state.tenant.privacy_policy_url !==
          (input.privacy_policy_url as string | null) ||
        state.tenant.privacy_contact_email !==
          (input.privacy_contact_email as string | null) ||
        state.tenant.privacy_rights_email !==
          (input.privacy_rights_email as string | null);
      Object.assign(state.tenant, {
        name: String(input.name || ""),
        short_name: input.short_name as string | null,
        website_url: input.website_url as string | null,
        support_email: input.support_email as string | null,
        default_locale: input.default_locale as string | null,
        default_timezone: input.default_timezone as string | null,
        privacy_policy_url: input.privacy_policy_url as string | null,
        privacy_contact_email: input.privacy_contact_email as string | null,
        privacy_rights_email: input.privacy_rights_email as string | null,
        privacy_profile_version:
          state.tenant.privacy_profile_version + (controllerChanged ? 1 : 0),
        terms_url: input.terms_url as string | null,
      });
      if (input.accent_color)
        state.tenant.widget_appearance_defaults.accent_color = String(
          input.accent_color,
        );
      else delete state.tenant.widget_appearance_defaults.accent_color;
      return json(route, {
        organization_profile: {
          ...state.tenant,
          accent_color:
            state.tenant.widget_appearance_defaults.accent_color || null,
        },
      });
    }
    if (path === "/v1/organization-profile/logo" && method === "PUT") {
      if (state.failNextIdentityUpload) {
        state.failNextIdentityUpload = false;
        return json(
          route,
          {
            error: {
              code: "validation_error",
              message: "validation failed",
              details: {
                image:
                  "must be a valid PNG, JPEG, or WebP image between 32 and 4096 pixels and up to 2 MiB",
              },
            },
          },
          422,
        );
      }
      state.tenant.logo_version = `logo-version-${Date.now()}`;
      return json(route, {
        logo_version: state.tenant.logo_version,
        logo_url: "/mock-logo.png",
      });
    }
    if (path === "/v1/organization-profile/logo" && method === "DELETE") {
      state.tenant.logo_version = null;
      return json(route, { removed: true, logo_version: null, logo_url: null });
    }
    if (path === "/v1/profile" && method === "PATCH") {
      const input = body();
      if ("name" in input) {
        state.member.name =
          typeof input.name === "string" && input.name.trim()
            ? input.name.trim()
            : "";
      }
      if ("locale" in input) {
        state.member.locale =
          typeof input.locale === "string" ? input.locale : null;
      }
      if ("timezone" in input) {
        state.member.timezone =
          typeof input.timezone === "string" ? input.timezone : null;
      }
      return json(route, { member: state.member });
    }
    if (path === "/v1/profile/avatar" && method === "PUT") {
      if (state.failNextIdentityUpload) {
        state.failNextIdentityUpload = false;
        return json(
          route,
          {
            error: {
              code: "validation_error",
              message: "validation failed",
              details: { image: "must be a valid image" },
            },
          },
          422,
        );
      }
      state.member.avatar_version = `avatar-version-${Date.now()}`;
      return json(route, {
        avatar_version: state.member.avatar_version,
        member_id: state.member.id,
      });
    }
    if (path === "/v1/profile/avatar" && method === "DELETE") {
      state.member.avatar_version = null;
      return json(route, {
        removed: true,
        avatar_version: null,
        member_id: state.member.id,
      });
    }

    if (path === "/v1/dashboard" && method === "GET") {
      return json(route, {
        dashboard: {
          range_days: 30,
          feedback_count: 128,
          feedback_delta_percent: 12.4,
          open_issue_count: state.issues.filter(
            (issue) => issue.status !== "closed",
          ).length,
          satisfaction: 4.2,
          satisfaction_delta: 0.3,
          attention_count: 2,
          trend: [
            { date: "2026-07-17", feedback_count: 9, satisfaction: 4.0 },
            { date: "2026-07-18", feedback_count: 13, satisfaction: 4.1 },
            { date: "2026-07-19", feedback_count: 8, satisfaction: 4.3 },
            { date: "2026-07-20", feedback_count: 17, satisfaction: 4.2 },
            { date: "2026-07-21", feedback_count: 21, satisfaction: 4.4 },
          ],
        },
      });
    }

    if (path === "/v1/analytics/widget" && method === "GET") {
      state.analyticsRequests.push(url.search);
      if (state.analyticsDelayMs)
        await new Promise((resolve) =>
          setTimeout(resolve, state.analyticsDelayMs),
        );
      if (state.analyticsMode === "error")
        return json(
          route,
          {
            error: {
              code: "service_unavailable",
              message: "Analytics are temporarily unavailable.",
            },
          },
          503,
        );
      return json(route, analyticsResponse(state, url));
    }

    if (path === "/v1/abuse/diagnostics" && method === "GET") {
      state.submissionHealthRequests.push(url.search);
      if (state.submissionHealthMode === "error") {
        return json(
          route,
          {
            error: {
              code: "service_unavailable",
              message: "Diagnostics are temporarily unavailable.",
            },
          },
          503,
        );
      }
      return json(route, submissionHealthResponse(state, url));
    }

    if (path === "/v1/projects" && method === "GET")
      return json(route, { projects: state.projects });
    if (path === "/v1/projects" && method === "POST") {
      const input = body();
      const project = {
        id: `project-${state.projects.length + 1}`,
        tenant_id: state.tenant.id,
        name: String(input.name),
        public_key: `pk_live_project_${state.projects.length + 1}`,
        allowed_domains: input.allowed_domains as string[],
        default_locale: "en",
        reporter_access_enabled: null as boolean | null,
        widget_appearance_overrides: {} as Record<string, string>,
        widget_last_seen_at: null as string | null,
        created_at: now,
        updated_at: now,
      };
      state.projects.push(project);
      return json(route, { project }, 201);
    }
    const projectRulesMatch = path.match(
      /^\/v1\/projects\/([^/]+)\/widget-rules$/,
    );
    if (projectRulesMatch && method === "GET") {
      return json(route, {
        widget_rules: state.widgetRules.filter(
          (rule) => rule.project_id === projectRulesMatch[1],
        ),
      });
    }
    if (projectRulesMatch && method === "POST") {
      const input = body();
      const rule = {
        ...state.widgetRules[0],
        ...input,
        id: `rule-${state.widgetRules.length + 1}`,
        project_id: projectRulesMatch[1],
        include_conditions: (
          (input.include_conditions as Array<Record<string, unknown>>) || []
        ).map((condition, index) => ({
          ...condition,
          id: `target-include-${index + 10}`,
          position: index,
        })),
        exclude_conditions: (
          (input.exclude_conditions as Array<Record<string, unknown>>) || []
        ).map((condition, index) => ({
          ...condition,
          id: `target-exclude-${index + 10}`,
          position: index,
        })),
        created_at: now,
        updated_at: now,
      } as (typeof state.widgetRules)[number];
      state.widgetRules.push(rule);
      return json(route, { widget_rule: rule }, 201);
    }
    const projectRuleMatch = path.match(
      /^\/v1\/projects\/([^/]+)\/widget-rules\/([^/]+)$/,
    );
    if (projectRuleMatch && method === "PATCH") {
      if (state.nextWidgetRuleFieldErrors) {
        const fields = state.nextWidgetRuleFieldErrors;
        state.nextWidgetRuleFieldErrors = null;
        return json(
          route,
          {
            error: {
              code: "validation_error",
              message: "validation failed",
              details: { fields },
            },
          },
          422,
        );
      }
      const rule = state.widgetRules.find(
        (candidate) =>
          candidate.project_id === projectRuleMatch[1] &&
          candidate.id === projectRuleMatch[2],
      );
      if (rule) Object.assign(rule, body(), { updated_at: now });
      return json(route, { widget_rule: rule });
    }
    if (projectRuleMatch && method === "DELETE") {
      state.destructiveRequests.push(`rule:${projectRuleMatch[2]}`);
      if (state.destructiveHold) await state.destructiveHold;
      const index = state.widgetRules.findIndex(
        (candidate) =>
          candidate.project_id === projectRuleMatch[1] &&
          candidate.id === projectRuleMatch[2],
      );
      if (index >= 0) state.widgetRules.splice(index, 1);
      return route.fulfill({ status: 204 });
    }
    const replayConsentDiagnosticMatch = path.match(
      /^\/v1\/projects\/([^/]+)\/widget-rules\/([^/]+)\/replay-consent-diagnostic$/,
    );
    if (replayConsentDiagnosticMatch && method === "GET") {
      const rule = state.widgetRules.find(
        (candidate) =>
          candidate.project_id === replayConsentDiagnosticMatch[1] &&
          candidate.id === replayConsentDiagnosticMatch[2],
      );
      return json(route, {
        diagnostic: {
          widget_rule_id: replayConsentDiagnosticMatch[2],
          replay_mode: rule?.replay_mode || "none",
          consent_mode: rule?.replay_consent?.mode || "none",
          notice_version: rule?.replay_consent?.notice_version || "",
          state: "not_observed",
        },
      });
    }
    const analyticsConsentDiagnosticMatch = path.match(
      /^\/v1\/projects\/([^/]+)\/widget-rules\/([^/]+)\/analytics-consent-diagnostic$/,
    );
    if (analyticsConsentDiagnosticMatch && method === "GET") {
      const rule = state.widgetRules.find(
        (candidate) =>
          candidate.project_id === analyticsConsentDiagnosticMatch[1] &&
          candidate.id === analyticsConsentDiagnosticMatch[2],
      );
      const consent = (
        rule as unknown as
          | {
              analytics_consent?: { mode: string; notice_version: string };
            }
          | undefined
      )?.analytics_consent;
      return json(route, {
        diagnostic: {
          widget_rule_id: analyticsConsentDiagnosticMatch[2],
          consent_mode: consent?.mode || "none",
          notice_version: consent?.notice_version || "",
          active_count: 42,
          expiring_soon_count: 3,
          denied_count: 8,
          withdrawn_count: 5,
          expired_count: 11,
          recent_count: 27,
          next_expires_at: "2026-07-24T12:00:00.000Z",
          last_activity_at: "2026-07-21T11:45:00.000Z",
        },
      });
    }
    const projectMatch = path.match(/^\/v1\/projects\/([^/]+)$/);
    if (projectMatch && method === "GET") {
      return json(route, {
        project: state.projects.find(
          (project) => project.id === projectMatch[1],
        ),
      });
    }
    if (projectMatch && method === "PATCH") {
      const project = state.projects.find(
        (candidate) => candidate.id === projectMatch[1],
      );
      if (project) Object.assign(project, body(), { updated_at: now });
      return json(route, { project });
    }
    if (projectMatch && method === "DELETE") {
      state.destructiveRequests.push(`project:${projectMatch[1]}`);
      if (state.destructiveHold) await state.destructiveHold;
      const index = state.projects.findIndex(
        (candidate) => candidate.id === projectMatch[1],
      );
      if (index >= 0) state.projects.splice(index, 1);
      return route.fulfill({ status: 204 });
    }

    if (path === "/v1/feedbacks" && method === "GET") {
      const requestedStatus = url.searchParams.get("status");
      const requestedSource = url.searchParams.get("source");
      const requestedAttachment = url.searchParams.get("attachment");
      const requestedQuery = url.searchParams.get("q")?.toLowerCase();
      const items = state.feedback.filter((item) => {
        if (
          requestedQuery &&
          ![
            item.public_id,
            item.title,
            item.description,
            item.issue_public_id,
          ].some((value) => value?.toLowerCase().includes(requestedQuery))
        )
          return false;
        if (requestedStatus && item.status !== requestedStatus) return false;
        if (requestedSource && item.source !== requestedSource) return false;
        if (requestedAttachment === "replay") return item.has_replay;
        if (requestedAttachment === "none")
          return item.attachment_kinds.length === 0 && !item.has_replay;
        if (requestedAttachment)
          return (item.attachment_kinds as string[]).includes(
            requestedAttachment,
          );
        return true;
      });
      return json(route, {
        feedbacks: items,
        pagination: { next_cursor: null, has_more: false, limit: 50 },
      });
    }
    const attachmentMatch = path.match(
      /^\/v1\/feedbacks\/([^/]+)\/attachments\/([^/]+)\/content$/,
    );
    if (attachmentMatch && method === "GET") {
      return route.fulfill({
        status: 200,
        contentType: "image/png",
        body: Buffer.from(
          "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M/wHwAEAQH/2p7iWQAAAABJRU5ErkJggg==",
          "base64",
        ),
      });
    }
    if (path === "/v1/feedbacks/bulk" && method === "PATCH") {
      const input = body();
      state.bulkFeedbackRequests.push(input);
      if (state.failNextBulkFeedbackUpdate) {
        state.failNextBulkFeedbackUpdate = false;
        return json(
          route,
          { error: { message: "Bulk update rejected." } },
          422,
        );
      }
      const feedbackIDs = Array.isArray(input.feedback_ids)
        ? (input.feedback_ids as string[])
        : [];
      const updated = state.feedback.filter((item) =>
        feedbackIDs.includes(item.id),
      );
      for (const item of updated) {
        if (input.status) item.status = String(input.status);
        if (input.priority)
          item.priority =
            input.priority === "none"
              ? null
              : (String(input.priority) as typeof item.priority);
        if (input.unassign) item.assignee = null;
        if (input.assignee_id) {
          const assignee = state.members.find(
            (candidate) => candidate.id === input.assignee_id,
          );
          item.assignee = assignee
            ? { id: assignee.id, name: assignee.name, email: assignee.email }
            : null;
        }
        if (Array.isArray(input.remove_tag_ids)) {
          item.tags = item.tags.filter(
            (tag) => !(input.remove_tag_ids as string[]).includes(tag.id),
          );
        }
        if (Array.isArray(input.add_tag_ids)) {
          for (const tagID of input.add_tag_ids as string[]) {
            const tag = state.tags.find((candidate) => candidate.id === tagID);
            if (tag && !item.tags.some((candidate) => candidate.id === tag.id))
              item.tags.push(tag);
          }
          item.tags.sort((left, right) => left.name.localeCompare(right.name));
        }
      }
      return json(route, { updated_count: updated.length, feedbacks: updated });
    }
    if (path === "/v1/feedbacks/unlink" && method === "POST") {
      const input = body();
      const feedbackIDs = Array.isArray(input.feedback_ids)
        ? (input.feedback_ids as string[])
        : [];
      const updated = state.feedback.filter((item) =>
        feedbackIDs.includes(item.id),
      );
      for (const item of updated) {
        const previous = state.issues.find(
          (issue) => issue.id === item.issue_id,
        );
        if (previous)
          previous.feedback_count = Math.max(0, previous.feedback_count - 1);
        item.issue_id = undefined;
        item.issue_public_id = undefined;
        item.issue_status = undefined;
      }
      return json(route, { updated_count: updated.length, feedbacks: updated });
    }
    if (path === "/v1/feedbacks/link" && method === "POST") {
      const input = body();
      const feedbackIDs = Array.isArray(input.feedback_ids)
        ? (input.feedback_ids as string[])
        : [];
      const issue = state.issues.find(
        (candidate) => candidate.id === input.issue_id,
      );
      const updated = state.feedback.filter((item) =>
        feedbackIDs.includes(item.id),
      );
      for (const item of updated) {
        item.issue_id = issue?.id;
        item.issue_public_id = issue?.public_id;
        item.issue_status = issue?.status;
      }
      if (issue) issue.feedback_count += updated.length;
      return json(route, { updated_count: updated.length, feedbacks: updated });
    }
    const feedbackMatch = path.match(/^\/v1\/feedbacks\/([^/]+)$/);
    if (feedbackMatch && method === "GET")
      return json(route, { feedback: feedbackDetail(state, feedbackMatch[1]) });
    if (feedbackMatch && method === "PATCH") {
      const item = state.feedback.find(
        (candidate) => candidate.id === feedbackMatch[1],
      );
      const input = body();
      if (item && input.status) item.status = String(input.status);
      if (item && input.title) item.title = String(input.title);
      if (item && input.priority)
        item.priority =
          input.priority === "none"
            ? null
            : (String(input.priority) as typeof item.priority);
      if (item && input.unassign) item.assignee = null;
      if (item && input.assignee_id) {
        const assignee = state.members.find(
          (candidate) => candidate.id === input.assignee_id,
        );
        item.assignee = assignee
          ? { id: assignee.id, name: assignee.name, email: assignee.email }
          : null;
      }
      if (item && Array.isArray(input.remove_tag_ids)) {
        item.tags = item.tags.filter(
          (tag) => !(input.remove_tag_ids as string[]).includes(tag.id),
        );
      }
      if (item && Array.isArray(input.add_tag_ids)) {
        for (const tagID of input.add_tag_ids as string[]) {
          const tag = state.tags.find((candidate) => candidate.id === tagID);
          if (tag && !item.tags.some((candidate) => candidate.id === tag.id))
            item.tags.push(tag);
        }
        item.tags.sort((left, right) => left.name.localeCompare(right.name));
      }
      return json(route, { feedback: item });
    }
    const feedbackIssueMatch = path.match(/^\/v1\/feedbacks\/([^/]+)\/issue$/);
    if (feedbackIssueMatch && method === "POST") {
      const feedbackItem = state.feedback.find(
        (candidate) => candidate.id === feedbackIssueMatch[1],
      );
      const input = body();
      const issue = {
        id: "issue-created",
        tenant_id: state.tenant.id,
        project_id: feedbackItem?.project_id || "project-1",
        public_id: "ISS-102",
        title: String(input.title),
        description: String(input.description || ""),
        status: "open",
        priority: null as "low" | "medium" | "high" | "urgent" | null,
        assignee: null as { id: string; name?: string; email: string } | null,
        tags: [] as typeof state.tags,
        reporter_email: feedbackItem?.reporter_email || "",
        notify_reporter: feedbackItem?.notify_reporter || false,
        reporter_access_revoked_at: null as string | null,
        feedback_count: 1,
        created_at: now,
        updated_at: now,
        merged_into_id: undefined as string | undefined,
      };
      state.issues.push(issue);
      if (feedbackItem) {
        feedbackItem.issue_id = issue.id;
        feedbackItem.issue_public_id = issue.public_id;
        feedbackItem.issue_status = "open";
      }
      return json(route, { issue }, 201);
    }
    if (/^\/v1\/feedbacks\/[^/]+\/reprocess$/.test(path) && method === "POST")
      return json(route, {}, 202);
    if (/^\/v1\/attachments\/[^/]+\/retry$/.test(path) && method === "POST")
      return json(route, {}, 202);
    if (path === "/v1/sessions/session-1/replay" && method === "GET")
      return json(route, replayEvents);

    if (path === "/v1/issues/from-feedback" && method === "POST") {
      const input = body();
      const feedbackIDs = Array.isArray(input.feedback_ids)
        ? (input.feedback_ids as string[])
        : [];
      const selectedFeedback = state.feedback.filter((item) =>
        feedbackIDs.includes(item.id),
      );
      const issue = {
        id: `issue-${state.issues.length + 1}`,
        tenant_id: state.tenant.id,
        project_id: selectedFeedback[0]?.project_id || "project-1",
        public_id: `ISS-${100 + state.issues.length}`,
        title: String(input.title),
        description: String(input.description || ""),
        status: "open",
        priority: null as "low" | "medium" | "high" | "urgent" | null,
        assignee: null as { id: string; name?: string; email: string } | null,
        tags: [] as typeof state.tags,
        reporter_email: selectedFeedback[0]?.reporter_email || "",
        notify_reporter: selectedFeedback.some((item) => item.notify_reporter),
        reporter_access_revoked_at: null as string | null,
        feedback_count: selectedFeedback.length,
        created_at: now,
        updated_at: now,
        merged_into_id: undefined as string | undefined,
      };
      state.issues.push(issue);
      for (const item of selectedFeedback) {
        item.issue_id = issue.id;
        item.issue_public_id = issue.public_id;
        item.issue_status = "open";
      }
      return json(route, { issue }, 201);
    }
    if (path === "/v1/issues" && method === "GET")
      return json(route, { issues: state.issues });
    const issueMergeMatch = path.match(/^\/v1\/issues\/([^/]+)\/merge$/);
    if (issueMergeMatch && method === "POST") {
      const issue = state.issues.find(
        (candidate) => candidate.id === issueMergeMatch[1],
      );
      if (issue) issue.merged_into_id = String(body().target_issue_id);
      return json(route, { issue });
    }
    if (issueMergeMatch && method === "DELETE") {
      const issue = state.issues.find(
        (candidate) => candidate.id === issueMergeMatch[1],
      );
      if (issue) issue.merged_into_id = undefined;
      return json(route, { issue });
    }
    const issueCommentMatch = path.match(/^\/v1\/issues\/([^/]+)\/comments$/);
    if (issueCommentMatch && method === "POST") {
      const issue = state.issues.find(
        (candidate) => candidate.id === issueCommentMatch[1],
      );
      const input = body();
      const mentionIDs = Array.isArray(input.mention_member_ids)
        ? (input.mention_member_ids as string[])
        : [];
      const mentions = state.members.filter((member) =>
        mentionIDs.includes(member.id),
      );
      const comment = {
        id: `comment-${state.issueComments.length + 1}`,
        issue_id: issueCommentMatch[1],
        author: {
          id: state.member.id,
          name: state.member.name,
          email: state.member.email,
        },
        body: String(input.body),
        mentions: mentions.map((member) => ({
          id: member.id,
          name: member.name,
          email: member.email,
        })),
        created_at: now,
      };
      const activity = {
        id: `activity-${state.issueActivities.length + 1}`,
        issue_id: issueCommentMatch[1],
        actor: comment.author,
        kind: "issue.comment_created",
        visibility: "internal" as const,
        data: {
          comment_id: comment.id,
          body: comment.body,
          mention_member_ids: mentionIDs,
        },
        created_at: now,
      };
      state.issueComments.push(comment);
      state.issueActivities.push(activity);
      for (const member of mentions.filter(
        (member) => member.id !== state.member.id,
      )) {
        state.memberNotifications.push({
          id: `notification-${state.memberNotifications.length + 1}`,
          recipient_member_id: member.id,
          kind: "issue.mentioned",
          issue_id: issueCommentMatch[1],
          public_id: issue?.public_id || "",
          issue_title: issue?.title || "",
          actor: comment.author,
          body: comment.body,
          created_at: now,
        });
      }
      return json(route, { comment, activity }, 201);
    }
    const issueReporterAccessMatch = path.match(
      /^\/v1\/issues\/([^/]+)\/reporter-access$/,
    );
    if (issueReporterAccessMatch && method === "PATCH") {
      const issue = state.issues.find(
        (candidate) => candidate.id === issueReporterAccessMatch[1],
      );
      const enabled = Boolean(body().enabled);
      if (issue) issue.reporter_access_revoked_at = enabled ? null : now;
      return json(route, { issue });
    }
    const issueMatch = path.match(/^\/v1\/issues\/([^/]+)$/);
    if (issueMatch && method === "GET") {
      return json(route, {
        issue: state.issues.find((issue) => issue.id === issueMatch[1]),
        updates: state.issueUpdates.filter(
          (update) => update.issue_id === issueMatch[1],
        ),
        comments: state.issueComments.filter(
          (comment) => comment.issue_id === issueMatch[1],
        ),
        activities: state.issueActivities.filter(
          (activity) => activity.issue_id === issueMatch[1],
        ),
      });
    }
    if (issueMatch && method === "PATCH") {
      const issue = state.issues.find(
        (candidate) => candidate.id === issueMatch[1],
      );
      const input = body();
      if (issue && input.title) issue.title = String(input.title);
      if (issue && typeof input.description === "string")
        issue.description = input.description;
      if (issue && input.priority)
        issue.priority =
          input.priority === "none"
            ? null
            : (String(input.priority) as typeof issue.priority);
      if (issue && input.unassign) issue.assignee = null;
      if (issue && input.assignee_id) {
        const assignee = state.members.find(
          (candidate) => candidate.id === input.assignee_id,
        );
        issue.assignee = assignee
          ? { id: assignee.id, name: assignee.name, email: assignee.email }
          : null;
      }
      if (issue && Array.isArray(input.remove_tag_ids)) {
        issue.tags = issue.tags.filter(
          (tag) => !(input.remove_tag_ids as string[]).includes(tag.id),
        );
      }
      if (issue && Array.isArray(input.add_tag_ids)) {
        for (const tagID of input.add_tag_ids as string[]) {
          const tag = state.tags.find((candidate) => candidate.id === tagID);
          if (tag && !issue.tags.some((candidate) => candidate.id === tag.id))
            issue.tags.push(tag);
        }
        issue.tags.sort((left, right) => left.name.localeCompare(right.name));
      }
      if (issue && input.status && input.status !== issue.status) {
        const previousStatus = issue.status;
        state.issueUpdates.push({
          id: `update-${state.issueUpdates.length + 1}`,
          issue_id: issue.id,
          member_id: state.member.id,
          old_status: issue.status,
          new_status: String(input.status),
          comment:
            typeof input.comment === "string" ? input.comment : undefined,
          created_at: now,
        });
        issue.status = String(input.status);
        state.lastReporterMessage =
          typeof input.reporter_message === "string"
            ? input.reporter_message
            : null;
        state.issueActivities.push({
          id: `activity-${state.issueActivities.length + 1}`,
          issue_id: issue.id,
          actor: {
            id: state.member.id,
            name: state.member.name,
            email: state.member.email,
          },
          kind: "issue.status_changed",
          visibility: "internal",
          data: {
            changes: {
              status: { from: previousStatus, to: String(input.status) },
            },
            transition_comment:
              typeof input.comment === "string" ? input.comment : undefined,
          },
          created_at: now,
        });
      }
      return json(route, { issue });
    }

    if (path === "/v1/notifications" && method === "GET") {
      const notifications = state.memberNotifications.filter(
        (notification) => notification.recipient_member_id === state.member.id,
      );
      return json(route, {
        notifications,
        unread_count: notifications.filter(
          (notification) => !notification.read_at,
        ).length,
      });
    }
    const notificationReadMatch = path.match(
      /^\/v1\/notifications\/([^/]+)\/read$/,
    );
    if (notificationReadMatch && method === "PATCH") {
      const notification = state.memberNotifications.find(
        (candidate) =>
          candidate.id === notificationReadMatch[1] &&
          candidate.recipient_member_id === state.member.id,
      );
      if (!notification)
        return json(
          route,
          { error: { code: "not_found", message: "resource not found" } },
          404,
        );
      notification.read_at = now;
      return json(route, { read: true });
    }

    if (path === "/v1/plans" && method === "GET")
      return json(route, { plans: planCatalog });
    if (path === "/v1/config" && method === "GET") {
      if (state.workspaceAccess === "unknown")
        return json(
          route,
          { error: { code: "unknown_tenant", message: "unknown tenant" } },
          404,
        );
      if (state.workspaceAccess === "suspended")
        return json(
          route,
          {
            error: {
              code: "tenant_suspended",
              message: "tenant is not active",
            },
          },
          403,
        );
      if (state.workspaceAccess === "error")
        return json(
          route,
          {
            error: {
              code: "service_unavailable",
              message: "service unavailable",
            },
          },
          503,
        );
      return json(route, {
        tenant: state.tenant,
        plan: planCatalog.find((plan) => plan.plan === state.tenant.plan),
      });
    }
    if (path === "/v1/reporter-access" && method === "PATCH") {
      if (state.failNextReporterAccessUpdate) {
        state.failNextReporterAccessUpdate = false;
        return json(
          route,
          {
            error: {
              code: "service_unavailable",
              message: "reporter access unavailable",
            },
          },
          503,
        );
      }
      state.tenant.reporter_access_enabled = Boolean(body().enabled);
      return json(route, {
        reporter_access_enabled: state.tenant.reporter_access_enabled,
      });
    }
    if (path === "/v1/widget-appearance" && method === "PATCH") {
      if (state.failNextAppearanceUpdate) {
        state.failNextAppearanceUpdate = false;
        return json(
          route,
          {
            error: {
              code: "validation_error",
              message: "validation failed",
              details: {
                logo_url:
                  "must be an HTTPS URL without credentials or fragments",
              },
            },
          },
          422,
        );
      }
      state.tenant.widget_appearance_defaults = body() as Record<
        string,
        string
      >;
      return json(route, {
        widget_appearance_defaults: state.tenant.widget_appearance_defaults,
      });
    }
    if (path === "/v1/privacy/retention" && method === "GET") {
      return json(route, {
        retention: {
          plan: state.tenant.plan,
          registry_version: 1,
          summary: {
            enforced: 5,
            configurable: 2,
            decision_required: 1,
            external_control: 1,
          },
          policies: [
            mockRetentionPolicy(
              "workflow_metadata",
              "Feedback and issue workflow content",
              "Decision required",
              "decision_required",
              "Policy decision required",
            ),
            mockRetentionPolicy(
              "attachments",
              "Attachments",
              "90 days",
              "configurable",
              "Plan storage limit",
              { duration_days: 90 },
            ),
            mockRetentionPolicy(
              "session_replays",
              "Session replay objects",
              "90 days",
              "configurable",
              "Plan storage limit",
              { duration_days: 90 },
            ),
            mockRetentionPolicy(
              "widget_events",
              "Raw widget funnel events",
              "90 days",
              "enforced",
              "Platform default",
              { duration_days: 90 },
            ),
            mockRetentionPolicy(
              "widget_daily",
              "Daily widget funnel aggregates",
              "730 days",
              "enforced",
              "Platform default",
              { duration_days: 730 },
            ),
            mockRetentionPolicy(
              "export_archives",
              "Export archives",
              "7 days",
              "enforced",
              "Platform default",
              { duration_days: 7 },
            ),
            mockRetentionPolicy(
              "unverified_requests",
              "Unverified reporter requests",
              "48 hours",
              "enforced",
              "Platform default",
              { duration_days: 2 },
            ),
            mockRetentionPolicy(
              "verified_requests",
              "Verified reporter request access",
              "30 days",
              "enforced",
              "Platform default",
              { duration_days: 30 },
            ),
            mockRetentionPolicy(
              "external_infrastructure",
              "Infrastructure and telemetry boundaries",
              "External configuration",
              "external_control",
              "External configuration",
              {
                role: "controller_and_processor",
                resource_counts: [{ kind: "provider", count: 4 }],
              },
            ),
          ],
        },
        health: {
          generated_at: "2026-07-24T10:05:00Z",
          summary: {
            tracked: 3,
            healthy: 1,
            attention: 1,
            failed: 0,
            overdue: 0,
            not_observed: 1,
          },
          operations: [
            {
              operation: "tenant_magic_links",
              category: "auth_magic_links",
              state: "healthy",
              alert_reason: "none",
              registered_at: "2026-07-24T08:00:00Z",
              expected_interval_seconds: 3600,
              alert_grace_seconds: 900,
              last_attempt_at: "2026-07-24T10:00:00Z",
              last_success_at: "2026-07-24T10:00:00Z",
              next_expected_at: "2026-07-24T11:00:00Z",
              last_result: "succeeded",
              last_processed: 3,
              total_processed: 9,
              total_runs: 3,
              total_failures: 0,
              consecutive_failures: 0,
              budget_exhausted: false,
            },
            {
              operation: "platform_admin_magic_links",
              category: "auth_magic_links",
              state: "attention",
              alert_reason: "budget_exhausted",
              registered_at: "2026-07-24T08:00:00Z",
              expected_interval_seconds: 3600,
              alert_grace_seconds: 900,
              last_attempt_at: "2026-07-24T10:00:00Z",
              last_success_at: "2026-07-24T10:00:00Z",
              next_expected_at: "2026-07-24T11:00:00Z",
              last_result: "succeeded",
              last_processed: 4000,
              total_processed: 8000,
              total_runs: 2,
              total_failures: 0,
              consecutive_failures: 0,
              budget_exhausted: true,
            },
            {
              operation: "replay_sessions",
              category: "session_replays",
              state: "not_observed",
              alert_reason: "not_observed",
              registered_at: "2026-07-24T08:00:00Z",
              expected_interval_seconds: 86400,
              alert_grace_seconds: 900,
              next_expected_at: "2026-07-25T08:00:00Z",
              last_processed: 0,
              total_processed: 0,
              total_runs: 0,
              total_failures: 0,
              consecutive_failures: 0,
              budget_exhausted: false,
            },
          ],
        },
      });
    }
    if (path === "/v1/privacy/exports" && method === "GET")
      return json(route, { exports: state.privacyExports });
    if (path === "/v1/privacy/cases/dashboard" && method === "GET") {
      return json(route, {
        dashboard: {
          overdue: 0,
          due_soon: 1,
          awaiting_manual: 0,
          awaiting_legal_review: 0,
          failed_external: 0,
          completed: 0,
        },
      });
    }
    if (path === "/v1/privacy/cases" && method === "GET") {
      return json(route, { cases: state.privacyCases });
    }
    if (path === "/v1/privacy/cases" && method === "POST") {
      return json(
        route,
        { case: state.privacyCases[0], verification_email_queued: true },
        202,
      );
    }
    const caseDetailMatch = path.match(/^\/v1\/privacy\/cases\/([^/]+)$/);
    if (caseDetailMatch && method === "GET") {
      const item = state.privacyCases.find(
        (candidate) => candidate.id === caseDetailMatch[1],
      );
      if (!item) {
        return json(
          route,
          { error: { code: "not_found", message: "privacy case not found" } },
          404,
        );
      }
      const events = [
        {
          id: `${item.id}-event-created`,
          sequence: 1,
          privacy_case_id: item.id,
          event_type: "privacy_case.created",
          actor_member_id: item.requested_by_member_id,
          new_status: "pending_verification",
          occurred_at: item.created_at,
        },
        {
          id: `${item.id}-event-verified`,
          sequence: 2,
          privacy_case_id: item.id,
          event_type: "privacy_case.verified",
          previous_status: "pending_verification",
          new_status: item.status,
          occurred_at: item.verified_at || item.created_at,
        },
        ...(item.communications || []).map((communication, index) => ({
          id: `${communication.id}-event`,
          sequence: index + 3,
          privacy_case_id: item.id,
          event_type: "privacy_case.communication_created",
          actor_member_id: communication.created_by_member_id,
          occurred_at: communication.created_at,
        })),
      ];
      return json(route, { case: item, events });
    }
    const caseTransitionMatch = path.match(
      /^\/v1\/privacy\/cases\/([^/]+)\/transition$/,
    );
    if (caseTransitionMatch && method === "POST") {
      const item = state.privacyCases.find(
        (candidate) => candidate.id === caseTransitionMatch[1],
      );
      if (!item) {
        return json(
          route,
          { error: { code: "not_found", message: "privacy case not found" } },
          404,
        );
      }
      const input = body();
      item.status = String(input.status) as PrivacyCase["status"];
      item.decision_summary = String(input.decision_summary || "") || undefined;
      item.legal_review_notes =
        String(input.legal_review_notes || "") || undefined;
      item.exception_reason = String(input.exception_reason || "") || undefined;
      if (
        ["partially_fulfilled", "completed", "refused", "cancelled"].includes(
          item.status,
        )
      ) {
        item.completed_at = now;
      }
      item.updated_at = now;
      return json(route, { case: item });
    }
    const caseRectificationMatch = path.match(
      /^\/v1\/privacy\/cases\/([^/]+)\/rectifications$/,
    );
    if (caseRectificationMatch && method === "POST") {
      const item = state.privacyCases.find(
        (candidate) => candidate.id === caseRectificationMatch[1],
      );
      const subject = state.members.find(
        (candidate) => candidate.id === item?.subject_member_id,
      );
      if (!item || !subject) {
        return json(
          route,
          { error: { code: "not_found", message: "privacy case not found" } },
          404,
        );
      }
      const input = body();
      const changes = input.changes as Partial<
        Record<"name" | "locale" | "timezone", string | null>
      >;
      const previousValues: Partial<
        Record<"name" | "locale" | "timezone", string | null>
      > = {};
      for (const field of ["name", "locale", "timezone"] as const) {
        if (!Object.prototype.hasOwnProperty.call(changes, field)) continue;
        previousValues[field] = subject[field] ?? null;
        if (field === "name") subject.name = changes.name || "";
        if (field === "locale") subject.locale = changes.locale ?? null;
        if (field === "timezone") subject.timezone = changes.timezone ?? null;
      }
      const rectification = {
        id: `${item.id}-rectification-${(item.rectifications?.length || 0) + 1}`,
        privacy_case_id: item.id,
        subject_member_id: subject.id,
        changes,
        previous_values: previousValues,
        reason: String(input.reason),
        applied_by_member_id: "member-1",
        applied_at: now,
        created_at: now,
      };
      item.rectifications ||= [];
      item.rectifications.unshift(rectification);
      item.updated_at = now;
      return json(route, { rectification, created: true }, 201);
    }
    const rectifiableFeedbackMatch = path.match(
      /^\/v1\/privacy\/cases\/([^/]+)\/rectifiable-feedbacks$/,
    );
    if (rectifiableFeedbackMatch && method === "GET") {
      const item = state.privacyCases.find(
        (candidate) => candidate.id === rectifiableFeedbackMatch[1],
      );
      if (!item) {
        return json(
          route,
          { error: { code: "not_found", message: "privacy case not found" } },
          404,
        );
      }
      const feedbacks = state.feedback
        .filter((candidate) => candidate.reporter_email === item.subject_email)
        .map((candidate) => ({
          id: candidate.id,
          public_id: candidate.public_id,
          title: candidate.title,
          description: candidate.description,
          page_url: candidate.page_url,
          text_process_status: candidate.ai_status,
          created_at: candidate.created_at,
        }));
      return json(route, { feedbacks });
    }
    const feedbackRectificationMatch = path.match(
      /^\/v1\/privacy\/cases\/([^/]+)\/feedback-rectifications$/,
    );
    if (feedbackRectificationMatch && method === "POST") {
      const item = state.privacyCases.find(
        (candidate) => candidate.id === feedbackRectificationMatch[1],
      );
      const input = body();
      const feedbackItem = state.feedback.find(
        (candidate) => candidate.id === input.feedback_id,
      );
      if (
        !item ||
        !feedbackItem ||
        feedbackItem.reporter_email !== item.subject_email
      ) {
        return json(
          route,
          { error: { code: "not_found", message: "feedback not found" } },
          404,
        );
      }
      const previousDescription = feedbackItem.description;
      feedbackItem.description = String(input.description);
      feedbackItem.text = String(input.description);
      feedbackItem.text_cleaned = "";
      feedbackItem.ai_status = "pending";
      const rectification = {
        id: `${item.id}-feedback-rectification-${(item.feedback_rectifications?.length || 0) + 1}`,
        privacy_case_id: item.id,
        feedback_id: feedbackItem.id,
        previous_description: previousDescription,
        description: feedbackItem.description,
        reason: String(input.reason),
        derived_data_invalidated: true,
        applied_by_member_id: "member-1",
        applied_at: now,
        created_at: now,
      };
      item.feedback_rectifications ||= [];
      item.feedback_rectifications.unshift(rectification);
      item.updated_at = now;
      return json(
        route,
        { feedback_rectification: rectification, created: true },
        201,
      );
    }
    const portabilityAssessmentMatch = path.match(
      /^\/v1\/privacy\/cases\/([^/]+)\/portability-assessments$/,
    );
    if (portabilityAssessmentMatch && method === "POST") {
      const item = state.privacyCases.find(
        (candidate) => candidate.id === portabilityAssessmentMatch[1],
      );
      if (!item)
        return json(
          route,
          { error: { code: "not_found", message: "privacy case not found" } },
          404,
        );
      const input = body();
      item.portability_assessment = {
        id: `${item.id}-assessment`,
        privacy_case_id: item.id,
        decision: String(input.decision) as
          | "approved"
          | "partially_approved"
          | "not_applicable"
          | "refused",
        data_categories: input.data_categories as Array<
          | "feedback"
          | "issues"
          | "attachments"
          | "replay"
          | "ai_processing"
          | "consent"
          | "delivery"
          | "widget_events"
        >,
        export_format: input.export_format ? "jsonl_zip" : undefined,
        reason: String(input.reason),
        assessed_by_member_id: "member-1",
        assessed_at: now,
        created_at: now,
      };
      return json(
        route,
        { portability_assessment: item.portability_assessment, created: true },
        201,
      );
    }
    const integrationActionRefreshMatch = path.match(
      /^\/v1\/privacy\/cases\/([^/]+)\/integration-actions\/refresh$/,
    );
    if (integrationActionRefreshMatch && method === "POST") {
      const item = state.privacyCases.find(
        (candidate) => candidate.id === integrationActionRefreshMatch[1],
      );
      if (!item) {
        return json(
          route,
          { error: { code: "not_found", message: "privacy case not found" } },
          404,
        );
      }
      let created = 0;
      if (!item.integration_actions?.length) {
        const integrationAction: PrivacyIntegrationAction = {
          id: `${item.id}-integration-action-1`,
          privacy_case_id: item.id,
          integration_id: "integration-slack-1",
          destination_key: "slack:integration-slack-1",
          destination_kind: "slack",
          destination_name: "Slack · Product feedback",
          action_kind: item.right_type as "rectification" | "erasure",
          handling_mode: "manual",
          status: "manual_required",
          capability_snapshot: {
            message_update: false,
            message_delete: false,
            message_recall: false,
            fidibeki_links_require_authentication: true,
            fidibeki_links_access_controlled: true,
          },
          data_categories: ["member_identity", "workflow_context"],
          disclosure_count: 2,
          first_disclosed_at: "2026-07-20T10:00:00.000Z",
          last_disclosed_at: "2026-07-21T10:00:00.000Z",
          linked_recipient_action_ids: [],
          created_at: now,
          updated_at: now,
        };
        item.integration_actions = [integrationAction];
        created = 1;
      }
      return json(route, {
        integration_actions: item.integration_actions,
        created,
        reopened: 0,
      });
    }
    const integrationActionCompleteMatch = path.match(
      /^\/v1\/privacy\/cases\/([^/]+)\/integration-actions\/([^/]+)\/complete$/,
    );
    if (integrationActionCompleteMatch && method === "POST") {
      const item = state.privacyCases.find(
        (candidate) => candidate.id === integrationActionCompleteMatch[1],
      );
      const integrationAction = item?.integration_actions?.find(
        (candidate) => candidate.id === integrationActionCompleteMatch[2],
      );
      if (!item || !integrationAction) {
        return json(
          route,
          {
            error: {
              code: "not_found",
              message: "integration action not found",
            },
          },
          404,
        );
      }
      const input = body();
      integrationAction.status = "completed";
      integrationAction.completed_by_member_id = "member-1";
      integrationAction.completed_at = now;
      integrationAction.completion_note = String(input.completion_note);
      integrationAction.updated_at = now;
      return json(route, {
        integration_action: integrationAction,
        completed: true,
      });
    }
    const recipientActionMatch = path.match(
      /^\/v1\/privacy\/cases\/([^/]+)\/recipient-actions$/,
    );
    if (recipientActionMatch && method === "POST") {
      const item = state.privacyCases.find(
        (candidate) => candidate.id === recipientActionMatch[1],
      );
      if (!item) {
        return json(
          route,
          { error: { code: "not_found", message: "privacy case not found" } },
          404,
        );
      }
      const input = body();
      const action = {
        id: `${item.id}-recipient-${(item.recipient_actions?.length || 0) + 1}`,
        privacy_case_id: item.id,
        action_kind: String(input.action_kind) as "rectification" | "erasure",
        recipient_name: String(input.recipient_name),
        recipient_email: String(input.recipient_email),
        instruction_summary: String(input.instruction_summary),
        integration_action_id: input.integration_action_id
          ? String(input.integration_action_id)
          : undefined,
        status: "failed" as const,
        attempts: 1,
        last_error: "email provider delivery failed",
        last_attempt_at: now,
        created_by_member_id: "member-1",
        created_at: now,
        updated_at: now,
        delivery_attempts: [
          {
            id: `${item.id}-recipient-attempt-1`,
            attempt_number: 1,
            status: "failed" as const,
            error_message: "email provider delivery failed",
            started_at: now,
            completed_at: now,
          },
        ],
      };
      item.recipient_actions ||= [];
      item.recipient_actions.unshift(action);
      if (action.integration_action_id) {
        const integrationAction = item.integration_actions?.find(
          (candidate) => candidate.id === action.integration_action_id,
        );
        integrationAction?.linked_recipient_action_ids.unshift(action.id);
      }
      return json(route, { recipient_action: action, created: true }, 201);
    }
    const recipientActionRetryMatch = path.match(
      /^\/v1\/privacy\/cases\/([^/]+)\/recipient-actions\/([^/]+)\/retry$/,
    );
    if (recipientActionRetryMatch && method === "POST") {
      const item = state.privacyCases.find(
        (candidate) => candidate.id === recipientActionRetryMatch[1],
      );
      const action = item?.recipient_actions?.find(
        (candidate) => candidate.id === recipientActionRetryMatch[2],
      );
      if (!action) {
        return json(
          route,
          {
            error: { code: "not_found", message: "recipient action not found" },
          },
          404,
        );
      }
      action.status = "pending";
      action.last_error = undefined;
      action.updated_at = now;
      return json(route, { recipient_action: action, retried: true }, 202);
    }
    const caseCommunicationMatch = path.match(
      /^\/v1\/privacy\/cases\/([^/]+)\/communications$/,
    );
    if (caseCommunicationMatch && method === "POST") {
      const item = state.privacyCases.find(
        (candidate) => candidate.id === caseCommunicationMatch[1],
      );
      if (!item) {
        return json(
          route,
          { error: { code: "not_found", message: "privacy case not found" } },
          404,
        );
      }
      const input = body();
      const subjectMember = state.members.find(
        (candidate) => candidate.id === item.subject_member_id,
      );
      const communication: PrivacyCaseCommunication = {
        id: `${item.id}-communication-${(item.communications?.length || 0) + 1}`,
        privacy_case_id: item.id,
        communication_kind: String(
          input.communication_kind,
        ) as PrivacyCaseCommunication["communication_kind"],
        recipient_email: item.subject_email || subjectMember?.email || "",
        subject_line: String(input.subject_line),
        message_body: String(input.message_body),
        status: "failed",
        attempts: 1,
        last_error: "email provider delivery failed",
        last_attempt_at: now,
        created_by_member_id: "member-1",
        created_at: now,
        updated_at: now,
        delivery_attempts: [
          {
            id: `${item.id}-communication-attempt-1`,
            attempt_number: 1,
            status: "failed",
            error_message: "email provider delivery failed",
            started_at: now,
            completed_at: now,
          },
        ],
      };
      item.communications ||= [];
      item.communications.unshift(communication);
      return json(route, { communication, created: true }, 201);
    }
    const caseCommunicationRetryMatch = path.match(
      /^\/v1\/privacy\/cases\/([^/]+)\/communications\/([^/]+)\/retry$/,
    );
    if (caseCommunicationRetryMatch && method === "POST") {
      const item = state.privacyCases.find(
        (candidate) => candidate.id === caseCommunicationRetryMatch[1],
      );
      const communication = item?.communications?.find(
        (candidate) => candidate.id === caseCommunicationRetryMatch[2],
      );
      if (!communication) {
        return json(
          route,
          {
            error: {
              code: "not_found",
              message: "case communication not found",
            },
          },
          404,
        );
      }
      communication.status = "pending";
      communication.last_error = undefined;
      communication.updated_at = now;
      return json(route, { communication, retried: true }, 202);
    }
    const caseExecutionMatch = path.match(
      /^\/v1\/privacy\/cases\/([^/]+)\/execute$/,
    );
    if (caseExecutionMatch && method === "POST") {
      const item = state.privacyCases.find(
        (candidate) => candidate.id === caseExecutionMatch[1],
      );
      if (!item)
        return json(
          route,
          { error: { code: "not_found", message: "privacy case not found" } },
          404,
        );
      item.execution = {
        right_type: item.right_type,
        ...(item.subject_type === "reporter"
          ? { request_status: "processing" as const }
          : {}),
        job_id: `${item.id}-job`,
        job_status: "pending",
        started_at: now,
      };
      item.updated_at = now;
      return json(route, { execution: item.execution, created: true }, 202);
    }
    const caseExecutionDownloadMatch = path.match(
      /^\/v1\/privacy\/cases\/([^/]+)\/execution\/download$/,
    );
    if (caseExecutionDownloadMatch && method === "POST") {
      state.privacyDownloadCount += 1;
      return json(route, {
        url: "https://downloads.example.test/case-access-export.zip?signature=test",
        expires_at: "2026-07-21T12:10:00.000Z",
      });
    }
    const caseHoldMatch = path.match(
      /^\/v1\/privacy\/cases\/([^/]+)\/legal-holds$/,
    );
    if (caseHoldMatch && method === "POST") {
      const item = state.privacyCases.find(
        (candidate) => candidate.id === caseHoldMatch[1],
      );
      if (!item)
        return json(
          route,
          { error: { code: "not_found", message: "privacy case not found" } },
          404,
        );
      const input = body();
      const hold = {
        id: `privacy-hold-${item.legal_holds.length + 1}`,
        privacy_case_id: item.id,
        reason: String(input.reason),
        legal_reference: input.legal_reference
          ? String(input.legal_reference)
          : undefined,
        active: true,
        placed_by_member_id: "member-1",
        placed_at: now,
        created_at: now,
        updated_at: now,
      };
      item.legal_holds.unshift(hold);
      return json(route, { legal_hold: hold }, 201);
    }
    const caseHoldReleaseMatch = path.match(
      /^\/v1\/privacy\/cases\/([^/]+)\/legal-holds\/([^/]+)\/release$/,
    );
    if (caseHoldReleaseMatch && method === "POST") {
      const item = state.privacyCases.find(
        (candidate) => candidate.id === caseHoldReleaseMatch[1],
      );
      const hold = item?.legal_holds.find(
        (candidate) => candidate.id === caseHoldReleaseMatch[2],
      );
      if (!hold)
        return json(
          route,
          {
            error: {
              code: "not_found",
              message: "privacy legal hold not found",
            },
          },
          404,
        );
      hold.active = false;
      hold.released_by_member_id = "member-1";
      hold.released_at = now;
      hold.release_reason = String(body().reason);
      hold.updated_at = now;
      return json(route, { legal_hold: hold });
    }
    if (path === "/v1/privacy/exports" && method === "POST") {
      const active = state.privacyExports.find(
        (item) => item.status === "pending" || item.status === "processing",
      );
      if (active) return json(route, { export: active, created: false });
      const item = {
        id: `privacy-export-${state.privacyExports.length + 1}`,
        kind: "workspace_export" as const,
        status: "pending" as const,
        attempts: 0,
        completed_at: undefined,
        expires_at: undefined,
        object_size_bytes: undefined,
        created_at: now,
        updated_at: now,
      };
      state.privacyExports.unshift(item);
      return json(route, { export: item, created: true }, 202);
    }
    const privacyExportDownloadMatch = path.match(
      /^\/v1\/privacy\/exports\/([^/]+)\/download$/,
    );
    if (privacyExportDownloadMatch && method === "POST") {
      state.privacyDownloadCount += 1;
      return json(route, {
        url: "https://downloads.example.test/workspace-export.zip?signature=test",
        expires_at: "2026-07-21T12:10:00.000Z",
      });
    }
    const privacyExportRetryMatch = path.match(
      /^\/v1\/privacy\/exports\/([^/]+)\/retry$/,
    );
    if (privacyExportRetryMatch && method === "POST") {
      const exportItem = state.privacyExports.find(
        (item) => item.id === privacyExportRetryMatch[1],
      );
      if (exportItem) exportItem.status = "pending";
      const request = state.privacyRequests.find(
        (item) => item.job_id === privacyExportRetryMatch[1],
      );
      if (request) {
        request.status = "processing";
        request.job_status = "pending";
      }
      return json(
        route,
        {
          export: exportItem || {
            id: privacyExportRetryMatch[1],
            kind: "reporter_anonymize",
            status: "pending",
            attempts: 1,
            created_at: now,
            updated_at: now,
          },
        },
        202,
      );
    }
    if (path === "/v1/access-events" && method === "GET") {
      let events = state.accessEvents;
      const resourceType = url.searchParams.get("resource_type");
      const actorType = url.searchParams.get("actor_type");
      const outcome = url.searchParams.get("outcome");
      if (resourceType)
        events = events.filter((event) => event.resource_type === resourceType);
      if (actorType)
        events = events.filter((event) => event.actor_type === actorType);
      if (outcome) events = events.filter((event) => event.outcome === outcome);
      if (url.searchParams.get("format") === "csv") {
        const header =
          "id,created_at,kind,outcome,actor_type,actor_id,resource_type,resource_id,action,permission_path,request_id";
        const rows = events.map((event) =>
          [
            event.id,
            event.created_at,
            event.kind,
            event.outcome,
            event.actor_type,
            event.actor_member_id || event.actor_api_key_id || "",
            event.resource_type || "",
            event.resource_id || "",
            event.action || "",
            event.permission_path || "",
            "",
          ].join(","),
        );
        return route.fulfill({
          status: 200,
          contentType: "text/csv",
          headers: {
            "content-disposition": 'attachment; filename="access-events.csv"',
          },
          body: [header, ...rows].join("\n"),
        });
      }
      return json(route, {
        events,
        signals: state.accessEventSignals,
        pagination: { next_cursor: undefined, has_more: false, limit: 50 },
      });
    }
    if (path === "/v1/privacy/requests" && method === "GET")
      return json(route, { requests: state.privacyRequests });
    if (path === "/v1/privacy/requests" && method === "POST") {
      const input = body();
      const item = {
        id: `privacy-request-${state.privacyRequests.length + 1}`,
        action: String(input.action) as "export" | "anonymize",
        status: "pending_verification" as const,
        reporter_email: String(input.email),
        verification_expires_at: "2026-07-23T12:00:00.000Z",
        verified_at: undefined,
        access_expires_at: undefined,
        job_id: undefined,
        job_status: undefined,
        manual_review_count: undefined,
        created_at: now,
        updated_at: now,
      };
      state.privacyRequests.unshift(item);
      return json(
        route,
        { request: item, verification_email_queued: true },
        202,
      );
    }
    const privacyReviewItemsMatch = path.match(
      /^\/v1\/privacy\/requests\/([^/]+)\/review-items$/,
    );
    if (privacyReviewItemsMatch && method === "GET")
      return json(route, { review_items: state.reviewItems });
    const privacyDecideMatch = path.match(
      /^\/v1\/privacy\/requests\/([^/]+)\/review-items\/([^/]+)\/decide$/,
    );
    if (privacyDecideMatch && method === "POST") {
      const input = body();
      const item = state.reviewItems.find(
        (candidate) => candidate.id === privacyDecideMatch[2],
      );
      if (!item || item.status !== "pending")
        return json(
          route,
          {
            error: {
              code: "conflict",
              message: "privacy review item is already decided",
            },
          },
          409,
        );
      item.status = input.status as typeof item.status;
      item.decision_reason = input.reason ? String(input.reason) : undefined;
      item.decided_by_member_id = "member-1";
      item.decided_at = now;
      item.updated_at = now;
      const pending = state.reviewItems.filter(
        (candidate) => candidate.status === "pending",
      ).length;
      let completed = false;
      if (pending === 0) {
        const request = state.privacyRequests.find(
          (candidate) => candidate.id === privacyDecideMatch[1],
        );
        if (request && request.status === "awaiting_manual_review") {
          request.status = "completed";
          request.manual_review_count = 0;
          completed = true;
        }
      }
      return json(route, { review_item: item, completed, pending });
    }
    const privacyFinalizeMatch = path.match(
      /^\/v1\/privacy\/requests\/([^/]+)\/finalize$/,
    );
    if (privacyFinalizeMatch && method === "POST") {
      const pending = state.reviewItems.filter(
        (candidate) => candidate.status === "pending",
      ).length;
      const request = state.privacyRequests.find(
        (candidate) => candidate.id === privacyFinalizeMatch[1],
      );
      let completed = pending === 0;
      if (completed && request) {
        if (request.status === "awaiting_manual_review") {
          request.status = "completed";
          request.manual_review_count = 0;
        } else {
          completed = request.status === "completed";
        }
      }
      return json(route, { completed, pending });
    }
    const privacyPublicMatch = path.match(
      /^\/v1\/privacy\/requests\/([^/]+)(?:\/(verify|download))?$/,
    );
    if (
      privacyPublicMatch &&
      method === "POST" &&
      privacyPublicMatch[2] === "verify"
    ) {
      state.privacyVerifyCount += 1;
      if (state.privacyPublicUnavailable) {
        return json(
          route,
          {
            error: {
              code: "not_found",
              message: "data request is invalid or unavailable",
            },
          },
          404,
        );
      }
      return json(route, { request: state.publicPrivacyRequest });
    }
    if (privacyPublicMatch && method === "GET" && !privacyPublicMatch[2]) {
      return json(route, { request: state.publicPrivacyRequest });
    }
    if (
      privacyPublicMatch &&
      method === "POST" &&
      privacyPublicMatch[2] === "download"
    ) {
      state.privacyDownloadCount += 1;
      return json(route, {
        url: "https://downloads.example.test/reporter-export.zip?signature=test",
        expires_at: "2026-07-21T12:10:00.000Z",
      });
    }
    const reporterProgressMatch = path.match(
      /^\/v1\/reporter\/progress\/([^/]+)$/,
    );
    if (reporterProgressMatch && method === "GET")
      return json(route, {
        progress: {
          public_id: state.issues[0].public_id,
          title: state.issues[0].title,
          status: "needs_info",
          created_at: state.issues[0].created_at,
          events: [
            { from_status: "open", to_status: "needs_info", created_at: now },
          ],
          clarification: {
            prompt: "Which promotion code did you use?",
            responded: state.reporterResponse !== null,
            responded_at: state.reporterResponse === null ? null : now,
          },
        },
      });
    const reporterResponseMatch = path.match(
      /^\/v1\/reporter\/progress\/([^/]+)\/response$/,
    );
    if (reporterResponseMatch && method === "POST") {
      if (state.reporterResponse !== null)
        return json(
          route,
          {
            error: {
              code: "not_found",
              message: "progress link is invalid or unavailable",
            },
          },
          404,
        );
      state.reporterResponse = String(body().response);
      return json(route, { accepted: true });
    }
    if (path === "/v1/members" && method === "GET")
      return json(route, { members: state.members });
    if (path === "/v1/ownership/transfer" && method === "POST") {
      const input = body();
      if (input.confirmation !== state.tenant.slug) {
        return json(
          route,
          {
            error: {
              code: "validation_error",
              message: "workspace confirmation does not match",
            },
          },
          422,
        );
      }
      const target = state.members.find(
        (member) => member.id === input.new_owner_member_id,
      );
      if (!target)
        return json(
          route,
          {
            error: {
              code: "validation_error",
              message: "new workspace owner is invalid",
            },
          },
          422,
        );
      for (const member of state.members)
        member.role = member.id === target.id ? "owner" : "member";
      target.permissions = [...allTenantPermissions];
      return json(route, { owner: target });
    }
    const memberStatusMatch = path.match(/^\/v1\/members\/([^/]+)\/status$/);
    if (memberStatusMatch && method === "PATCH") {
      const target = state.members.find(
        (member) => member.id === memberStatusMatch[1],
      );
      if (!target)
        return json(
          route,
          { error: { code: "not_found", message: "member not found" } },
          404,
        );
      target.status = String(body().status) as typeof target.status;
      if (target.status === "deactivated") target.permissions = [];
      return json(route, { member: target });
    }
    const memberMatch = path.match(/^\/v1\/members\/([^/]+)$/);
    if (memberMatch && method === "DELETE") {
      const target = state.members.find(
        (member) => member.id === memberMatch[1],
      );
      if (!target)
        return json(
          route,
          { error: { code: "not_found", message: "member not found" } },
          404,
        );
      target.status = "removed";
      target.permissions = [];
      return json(route, { member: target });
    }
    if (path === "/v1/member-domains" && method === "GET")
      return json(route, { member_domains: state.memberDomains });
    if (path === "/v1/member-domains" && method === "POST") {
      const input = body();
      const domain = String(input.email).split("@").at(-1) || "";
      const memberDomain = {
        id: `member-domain-${state.memberDomains.length + 1}`,
        tenant_id: state.tenant.id,
        domain,
        verification_email: String(input.email),
        verification_expires_at: "2026-07-21T12:10:00.000Z",
        verified_at: undefined,
        status: "pending" as const,
        created_at: now,
        updated_at: now,
      };
      state.memberDomains.push(memberDomain);
      return json(route, { member_domain: memberDomain }, 202);
    }
    if (path === "/v1/tags" && method === "GET")
      return json(route, { tags: state.tags });
    if (path === "/auth/invite" && method === "POST") {
      if (state.memberInviteFailure) {
        return json(
          route,
          {
            error: {
              code: state.memberInviteFailure.code,
              message: state.memberInviteFailure.message,
            },
          },
          state.memberInviteFailure.status,
        );
      }
      return json(route, {}, 202);
    }
    if (path === "/v1/api-keys" && method === "GET")
      return json(route, { api_keys: state.apiKeys });
    if (path === "/v1/api-keys" && method === "POST") {
      const input = body();
      const key = {
        id: "key-1",
        tenant_id: state.tenant.id,
        name: input.name,
        token_prefix: "fk_live_demo...",
        permissions: input.permissions,
        rate_limit_per_min: input.rate_limit_per_min,
        expires_at: input.expires_at,
        last_used_at: null,
        created_at: now,
        updated_at: now,
      };
      state.apiKeys.push(key);
      return json(
        route,
        { api_key: key, token: "fk_live_0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ" },
        201,
      );
    }
    const apiKeyMatch = path.match(/^\/v1\/api-keys\/([^/]+)$/);
    if (apiKeyMatch && method === "DELETE") {
      state.destructiveRequests.push(`key:${apiKeyMatch[1]}`);
      if (state.destructiveHold) await state.destructiveHold;
      const index = state.apiKeys.findIndex((key) => key.id === apiKeyMatch[1]);
      if (index >= 0) state.apiKeys.splice(index, 1);
      return route.fulfill({ status: 204 });
    }
    if (path === "/v1/integrations" && method === "GET")
      return json(route, { integrations: state.integrations });
    // Settings loads its tabs in one Promise.all, so an unmocked route here fails
    // the whole page rather than just the tracker card.
    if (path === "/v1/issue-trackers" && method === "GET")
      return json(route, { issue_trackers: [] });
    if (path === "/v1/integrations" && method === "POST") {
      const input = body();
      const integration = {
        id: "integration-1",
        kind: input.kind,
        name: input.name,
        webhook_url: input.webhook_url,
        channel: input.channel,
        webhook_secret_fingerprint: "sha256:demo",
        custom_header_names: [],
        events: input.events,
        privacy_fields: input.privacy_fields || [],
        privacy_profile_version: 1,
        destination_capabilities: {
          message_update: false,
          message_delete: false,
          message_recall: false,
          fidibeki_links_require_authentication: true,
          fidibeki_links_access_controlled: true,
        },
        is_active: true,
        created_at: now,
        updated_at: now,
      };
      state.integrations.push(integration);
      return json(
        route,
        { integration, webhook_secret: "whsec_dispatch_demo" },
        201,
      );
    }
    const integrationPrivacyMatch = path.match(
      /^\/v1\/integrations\/([^/]+)\/privacy$/,
    );
    if (integrationPrivacyMatch && method === "PATCH") {
      const input = body();
      const integration = state.integrations.find(
        (candidate) => candidate.id === integrationPrivacyMatch[1],
      );
      if (!integration)
        return json(
          route,
          { error: { code: "not_found", message: "resource not found" } },
          404,
        );
      integration.privacy_fields = input.privacy_fields;
      integration.privacy_profile_version = 1;
      return json(route, { integration });
    }
    const integrationMatch = path.match(/^\/v1\/integrations\/([^/]+)$/);
    if (integrationMatch && method === "DELETE") {
      state.destructiveRequests.push(`integration:${integrationMatch[1]}`);
      if (state.destructiveHold) await state.destructiveHold;
      const index = state.integrations.findIndex(
        (integration) => integration.id === integrationMatch[1],
      );
      if (index >= 0) state.integrations.splice(index, 1);
      return route.fulfill({ status: 204 });
    }

    if (path === "/v1/billing" && method === "GET")
      return json(route, {
        billing: {
          plan: state.tenant.plan,
          status: state.tenant.status,
          billing_interval: null,
          trial_ends_at: state.tenant.trial_ends_at,
          has_customer: false,
          has_subscription: false,
          deletion_requested_at: state.billingDeletionRequestedAt,
        },
      });
    if (path === "/v1/billing/delete-account" && method === "POST") {
      state.billingDeletionRequestedAt = now;
      state.tenant.deletion_requested_at = now;
      return json(route, {}, 202);
    }
    if (path === "/v1/billing/cancel-deletion" && method === "POST") {
      state.billingDeletionRequestedAt = null;
      state.tenant.deletion_requested_at = null;
      return json(route, {});
    }

    return json(
      route,
      {
        error: {
          code: "mock_route_missing",
          message: `${method} ${path} is not mocked`,
        },
      },
      404,
    );
  });
}

async function authenticate(page: Page, state: MockState) {
  await installMockAPI(page, state);
  await page.goto(`${tenantOrigin}/auth/verify#token=test-token`);
  await expect(
    page.getByRole("heading", { name: /Good afternoon/ }),
  ).toBeVisible();
}

async function openPrimary(
  page: Page,
  label:
    | "Feedback"
    | "Issues"
    | "Notifications"
    | "Analytics"
    | "Projects"
    | "Settings",
) {
  const link = page
    .getByRole("navigation", { name: "Primary navigation" })
    .getByRole("link", { name: label, exact: true });
  if ((page.viewportSize()?.width || 0) <= 700)
    await page.getByTitle("Open navigation").click();
  await link.click();
  const path = {
    Feedback: "/feedback",
    Issues: "/issues",
    Notifications: "/notifications",
    Analytics: "/analytics",
    Projects: "/projects",
    Settings: "/settings",
  }[label];
  await expect(page).toHaveURL(new RegExp(`${path}(?:\\?|$)`));
  await expect(
    page.getByRole("heading", { name: label, exact: true }),
  ).toBeVisible();
}

async function expectNoPageOverflow(page: Page) {
  const dimensions = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
  }));
  expect(dimensions.scrollWidth).toBeLessThanOrEqual(
    dimensions.clientWidth + 1,
  );
}

async function capture(page: Page, testInfo: TestInfo, name: string) {
  await expect(page.locator("body")).not.toContainText("Internal Server Error");
  await expectNoPageOverflow(page);
  await page.screenshot({
    path: testInfo.outputPath(`${name}.png`),
    fullPage: false,
    animations: "disabled",
  });
}

test("magic-link login enters the Dispatch workspace", async ({ page }) => {
  const state = createMockState();
  await installMockAPI(page, state);
  await page.goto(`${tenantOrigin}/login`);
  await page.waitForLoadState("networkidle");
  await page.getByLabel("Work email").fill("alex@acme.test");
  await page.getByRole("button", { name: "Send sign-in link" }).click();
  const confirmation = page.getByRole("status");
  await expect(confirmation).toContainText("Check your inbox");
  const confirmationLayout = await confirmation.evaluate((element) => {
    const icon = element.querySelector("svg")?.getBoundingClientRect();
    const copy = element.querySelector("span")?.getBoundingClientRect();
    return {
      display: getComputedStyle(element).display,
      columnGap: Number.parseFloat(getComputedStyle(element).columnGap),
      iconCenter: icon ? icon.top + icon.height / 2 : null,
      copyCenter: copy ? copy.top + copy.height / 2 : null,
      copyLeft: copy?.left ?? null,
      iconRight: icon?.right ?? null,
    };
  });
  expect(confirmationLayout.display).toBe("grid");
  expect(confirmationLayout.columnGap).toBeGreaterThanOrEqual(7);
  expect(confirmationLayout.copyLeft).not.toBeNull();
  expect(confirmationLayout.iconRight).not.toBeNull();
  expect(confirmationLayout.copyLeft!).toBeGreaterThan(
    confirmationLayout.iconRight!,
  );
  expect(
    Math.abs(confirmationLayout.iconCenter! - confirmationLayout.copyCenter!),
  ).toBeLessThanOrEqual(1);

  await page.goto(`${tenantOrigin}/auth/verify#token=test-token`);
  await expect(page).toHaveURL(`${tenantOrigin}/`);
  await expect(
    page.getByRole("heading", { name: "Good afternoon, Alex" }),
  ).toBeVisible();
});

test("unknown standard workspace offers explicit signup with an editable slug", async ({
  page,
}) => {
  const state = createMockState();
  state.workspaceAccess = "unknown";
  await installMockAPI(page, state);
  const unknownOrigin = `http://missing.localhost:${process.env.PLAYWRIGHT_PORT || "4321"}`;

  await page.goto(`${unknownOrigin}/login`);

  await expect(page).toHaveURL(`${unknownOrigin}/login`);
  await expect(
    page.getByRole("heading", { name: "Workspace unavailable" }),
  ).toBeVisible();
  await expect(page.getByLabel("Work email")).toHaveCount(0);
  const signup = page.getByRole("link", { name: "Create missing workspace" });
  await expect(signup).toHaveAttribute("href", /\/signup\?slug=missing$/);
  await signup.click();

  await expect(page).toHaveURL(
    new RegExp(
      `http://localhost:${process.env.PLAYWRIGHT_PORT || "4321"}/signup\\?slug=missing$`,
    ),
  );
  await expect(page.getByLabel("Workspace URL")).toHaveValue("missing");
  await expect(page.locator(".signup-plan-grid")).toBeVisible();
  await page.getByLabel("Workspace URL").fill("another-workspace");
  await expect(page.getByLabel("Workspace URL")).toHaveValue(
    "another-workspace",
  );
});

test("platform login hosts the workspace locator instead of redirecting to signup", async ({
  page,
}) => {
  const state = createMockState();
  await installMockAPI(page, state);
  const platformOrigin = `http://localhost:${process.env.PLAYWRIGHT_PORT || "4321"}`;

  await page.goto(`${platformOrigin}/login`);
  await page.waitForLoadState("networkidle");

  await expect(page).toHaveURL(`${platformOrigin}/login`);
  await expect(
    page.getByRole("heading", { name: "Find your workspace" }),
  ).toBeVisible();
  await expect(page.getByLabel("Work email")).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: "Create a workspace" }),
  ).toHaveAttribute("href", /\/signup$/);
  await page.getByLabel("Workspace address").fill("acme");
  await page.getByRole("button", { name: "Continue" }).click();

  await expect(page).toHaveURL(`${tenantOrigin}/login`);
  await expect(page.getByLabel("Work email")).toBeVisible();
});

test("inaccessible workspace uses the same neutral unavailable state", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "The unavailable layout is responsive in the signup-prefill test",
  );
  const state = createMockState();
  state.workspaceAccess = "suspended";
  await installMockAPI(page, state);
  const suspendedOrigin = `http://suspended.localhost:${process.env.PLAYWRIGHT_PORT || "4321"}`;

  await page.goto(`${suspendedOrigin}/login`);

  await expect(
    page.getByRole("heading", { name: "Workspace unavailable" }),
  ).toBeVisible();
  await expect(page.locator("body")).not.toContainText("tenant is not active");
  await expect(
    page.getByRole("heading", { name: "Workspace suspended" }),
  ).toHaveCount(0);
  await expect(page.getByLabel("Work email")).toHaveCount(0);
});

test("temporary workspace lookup failure can be retried", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Retry behavior is viewport independent",
  );
  const state = createMockState();
  state.workspaceAccess = "error";
  await installMockAPI(page, state);
  const errorOrigin = `http://error.localhost:${process.env.PLAYWRIGHT_PORT || "4321"}`;

  await page.goto(`${errorOrigin}/login`);
  await expect(
    page.getByRole("heading", { name: "Workspace check unavailable" }),
  ).toBeVisible();

  state.workspaceAccess = "available";
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(
    page.getByRole("heading", { name: "Sign in to Fidibeki" }),
  ).toBeVisible();
  await expect(page.getByLabel("Work email")).toBeVisible();
});

test("member domain verification consumes the fragment before confirming access", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "The public verification flow is viewport independent",
  );
  const state = createMockState();
  await installMockAPI(page, state);

  await page.goto(
    `${tenantOrigin}/auth/domain-verify#token=member-domain-token`,
  );

  await expect(page).toHaveURL(`${tenantOrigin}/auth/domain-verify`);
  await expect(
    page.getByRole("heading", { name: "acme.test is verified" }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Continue to sign in" }),
  ).toBeVisible();
});

test("dashboard distinguishes unrated feedback from a zero rating", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  delete (state.feedback[1] as Partial<(typeof state.feedback)[number]>).rating;
  await authenticate(page, state);

  const ratedRow = page
    .locator(".dashboard-work-row")
    .filter({ hasText: "FB-1048" });
  const unratedRow = page
    .locator(".dashboard-work-row")
    .filter({ hasText: "FB-1047" });
  await expect(ratedRow.getByTitle("2 out of 5")).toBeVisible();
  await expect(unratedRow.getByTitle("Unrated")).toBeVisible();
  await expect(
    unratedRow.locator('.rating-mark > [aria-hidden="true"]'),
  ).toHaveText("—");
  await expect(page.getByTitle("0 out of 5")).toHaveCount(0);

  await openPrimary(page, "Feedback");
  const inboxRow = page
    .locator(".feedback-stream article")
    .filter({ hasText: "FB-1047" });
  await expect(inboxRow.getByTitle("Unrated")).toBeVisible();
  if (testInfo.project.name === "mobile") {
    await inboxRow.locator(".feedback-mobile-open").click();
  } else {
    await inboxRow.click();
    await expect(
      page.locator(".feedback-inspector-summary").getByTitle("Unrated"),
    ).toBeVisible();
    await page
      .locator(".feedback-inspector")
      .getByTitle("Open full detail")
      .click();
  }
  await expect(page).toHaveURL(/\/feedback\/feedback-2$/);
  await expect(
    page.locator(".feedback-detail-heading").getByTitle("Unrated"),
  ).toBeVisible();
  await expectNoPageOverflow(page);
  await capture(page, testInfo, "dispatch-dashboard-unrated");
});

test("dashboard attention rows use curated feedback titles with legacy fallbacks", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  state.feedback[0].title = "Promotion code blocks checkout";
  state.feedback[0].description =
    "The original reporter description contains the complete reproduction steps.";
  state.feedback[0].text_cleaned =
    "Cleaned reporter description that is not the operational title.";
  state.feedback[0].text =
    "Raw reporter description that is not the operational title.";
  state.feedback[1].title = "";
  state.feedback[1].text_cleaned = "Legacy cleaned feedback summary";
  state.feedback[1].text = "Legacy raw feedback summary";
  await authenticate(page, state);

  const titledRow = page
    .locator(".dashboard-work-row")
    .filter({ hasText: "FB-1048" });
  await expect(
    titledRow.getByText("Promotion code blocks checkout", { exact: true }),
  ).toBeVisible();
  await expect(titledRow).not.toContainText("Cleaned reporter description");
  await expect(titledRow).not.toContainText("Raw reporter description");

  const legacyRow = page
    .locator(".dashboard-work-row")
    .filter({ hasText: "FB-1047" });
  await expect(
    legacyRow.getByText("Legacy cleaned feedback summary", { exact: true }),
  ).toBeVisible();
  await expect(legacyRow).not.toContainText("Legacy raw feedback summary");
  await expectNoPageOverflow(page);
  await capture(
    page,
    testInfo,
    `dashboard-curated-feedback-title-${testInfo.project.name}`,
  );
});

test("analytics exposes the widget funnel across breakdowns and filters", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  state.analyticsDelayMs = 800;
  state.analyticsPagesTotal = 210;
  await authenticate(page, state);

  const navigation = page.getByRole("navigation", {
    name: "Primary navigation",
  });
  if (testInfo.project.name === "mobile")
    await page.getByTitle("Open navigation").click();
  await navigation
    .getByRole("link", { name: "Analytics", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "Loading analytics" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Analytics", exact: true }),
  ).toBeVisible();

  const impressionMetric = page
    .locator(".analytics-ledger article")
    .filter({ hasText: "Impressions" });
  await expect(impressionMetric).toContainText("1,240");
  await expect(
    page.getByRole("img", { name: "Daily widget funnel trend" }),
  ).toBeVisible();
  await expect(
    page.getByRole("rowheader", { name: "Storefront", exact: true }),
  ).toBeVisible();

  await page.getByRole("tab", { name: "Rules" }).click();
  await expect(
    page.getByRole("rowheader", { name: /Spring launch/ }),
  ).toContainText("deleted");
  await page.getByRole("tab", { name: "Pages" }).click();
  await expect(page.locator(".analytics-breakdown > header")).toContainText(
    "2 of 210 pages",
  );
  await expect(
    page.getByRole("rowheader", {
      name: "https://shop.acme.test/checkout",
      exact: true,
    }),
  ).toBeVisible();

  await page.getByLabel("Project").selectOption("project-1");
  await page.getByLabel("Rule").selectOption("rule-1");
  await page
    .getByRole("combobox", { name: "Page", exact: true })
    .selectOption("https://shop.acme.test/checkout");
  await page.getByRole("button", { name: "7d" }).click();
  await expect(page).toHaveURL(/range=7d/);
  await expect
    .poll(() => state.analyticsRequests.at(-1) || "")
    .toContain("range=7d");
  expect(state.analyticsRequests.at(-1)).toContain("project_id=project-1");
  expect(state.analyticsRequests.at(-1)).toContain("rule_id=rule-1");
  expect(state.analyticsRequests.at(-1)).toContain(
    "page=https%3A%2F%2Fshop.acme.test%2Fcheckout",
  );
  await capture(page, testInfo, "dispatch-widget-analytics");
});

test("analytics keeps empty and retry states operational", async ({ page }) => {
  const state = createMockState();
  state.analyticsMode = "empty";
  await authenticate(page, state);
  await openPrimary(page, "Analytics");
  await expect(
    page.getByText("No widget activity in this period"),
  ).toBeVisible();
  await expect(
    page
      .locator(".analytics-ledger article")
      .filter({ hasText: "Interaction rate" }),
  ).toContainText("—");

  state.analyticsMode = "error";
  await page.getByRole("button", { name: "7d" }).click();
  await expect(
    page.getByRole("heading", { name: "Analytics unavailable" }),
  ).toBeVisible();
  state.analyticsMode = "data";
  await page.getByRole("button", { name: "Try again" }).click();
  await expect(
    page
      .locator(".analytics-ledger article")
      .filter({ hasText: "Impressions" }),
  ).toContainText("1,240");
});

test("restricted members enter their first permitted workspace section", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Permission routing is viewport independent",
  );
  const state = createMockState();
  state.member.permissions = ["issue:read", "issue:update", "project:read"];
  await installMockAPI(page, state);

  await page.goto(`${tenantOrigin}/auth/verify#token=test-token`);

  await expect(page).toHaveURL(`${tenantOrigin}/issues`);
  const navigation = page.getByRole("navigation", {
    name: "Primary navigation",
  });
  await expect(
    navigation.getByRole("link", { name: "Issues", exact: true }),
  ).toBeVisible();
  await expect(
    navigation.getByRole("link", { name: "Projects", exact: true }),
  ).toBeVisible();
  await expect(
    navigation.getByRole("link", { name: "Overview", exact: true }),
  ).toHaveCount(0);
  await expect(
    navigation.getByRole("link", { name: "Feedback", exact: true }),
  ).toHaveCount(0);
  await expect(
    navigation.getByRole("link", { name: "Settings", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("navigation", { name: "Feedback queues" }),
  ).toHaveCount(0);
  await expect(page.getByRole("search")).toHaveCount(0);
});

test("project creation opens the new collection workspace", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Covered once as an interaction workflow",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  const newProjectButton = page.getByRole("button", { name: "New project" });
  await newProjectButton.click();
  const dialog = page.getByRole("dialog", { name: "Create project" });
  await expect(dialog.getByLabel("Project name")).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(dialog).not.toBeVisible();
  await expect(newProjectButton).toBeFocused();
  await newProjectButton.click();
  await dialog.getByLabel("Project name").fill("Documentation");
  await dialog
    .getByLabel("Allowed domains")
    .fill("docs.acme.test\n*.docs-preview.acme.test");
  await dialog.getByRole("button", { name: "Create project" }).click();
  await expect(page).toHaveURL(/\/projects\/project-2$/);
  await expect(
    page.getByText("Documentation", { exact: true }).first(),
  ).toBeVisible();
  expect(state.projects).toHaveLength(2);
});

test("starter plan exposes exhausted project capacity before form entry", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  state.tenant.plan = "starter";
  await authenticate(page, state);
  await openPrimary(page, "Projects");

  await expect(page.getByRole("button", { name: "New project" })).toHaveCount(
    0,
  );
  await expect(page.getByText("1 of 1 projects used")).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Upgrade plan" }),
  ).toHaveAttribute("href", "/billing");
  await capture(page, testInfo, "starter-project-limit");
});

test("starter plan shows unavailable features and euro-denominated prices", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  state.tenant.plan = "starter";
  await authenticate(page, state);
  await openPrimary(page, "Settings");

  await expect(page.getByText("€9.99 / month")).toBeVisible();
  const aiFeature = page
    .locator(".settings-features li")
    .filter({ hasText: /^ai$/i });
  await expect(aiFeature.locator("svg")).toHaveClass(/lucide-x/);
  await expect(page.locator(".settings-features .lucide-check")).toHaveCount(0);
  await capture(page, testInfo, "starter-entitlements");

  await page.getByRole("link", { name: "Manage billing" }).click();
  await expect(
    page.getByRole("heading", { name: "Billing", exact: true }),
  ).toBeVisible();
  await expect(
    page.locator(".billing-plan").filter({ hasText: "Starter" }),
  ).toContainText("€9.99");
  await expect(page.locator(".billing-plans")).not.toContainText("$");
  await capture(page, testInfo, "billing-euro-catalog");

  await page.getByRole("button", { name: "Yearly" }).click();
  const yearlyStarter = page
    .locator(".billing-plan")
    .filter({ hasText: "Starter" });
  await expect(yearlyStarter).toContainText("€9.29");
  await expect(yearlyStarter).toContainText("Save 7%");
  await expect(yearlyStarter).toContainText("€111.48 billed yearly");
  await capture(page, testInfo, "billing-euro-catalog-yearly");

  await page.getByRole("button", { name: "Monthly" }).click();
  await expect(
    page.locator(".billing-plan").filter({ hasText: "Starter" }),
  ).toContainText("€9.99");
});

test("starter feedback without evidence does not offer invalid AI reprocessing", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  state.tenant.plan = "starter";
  state.feedback.splice(0, 1);
  await authenticate(page, state);
  await openPrimary(page, "Feedback");
  if (testInfo.project.name === "mobile")
    await page.getByRole("link", { name: "Open FB-1047" }).click();
  else await page.getByTitle("Open full detail").click();

  await expect(
    page.getByText("No media was attached to this feedback."),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Reprocess all" })).toHaveCount(
    0,
  );
  await expect(
    page.getByRole("link", { name: "Upgrade for AI" }),
  ).toHaveAttribute("href", "/billing");
  await capture(page, testInfo, "starter-empty-evidence");
});

test("widget rule save enforces browser validation before sending an update", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Native form validation is viewport independent",
  );
  const state = createMockState();
  let updateRequests = 0;
  page.on("request", (request) => {
    if (
      request.method() === "PATCH" &&
      new URL(request.url()).pathname.endsWith("/widget-rules/rule-1")
    )
      updateRequests += 1;
  });
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  const contactCollection = page
    .locator(".project-capability-grid label")
    .filter({ hasText: "Optional email address" })
    .locator("input");
  await contactCollection.check();
  await expect(page.locator(".contact-controller-warning")).toContainText(
    "Controller profile required",
  );
  await expect(page.locator(".contact-controller-warning a")).toHaveAttribute(
    "href",
    "/settings#organization-profile",
  );
  await contactCollection.uncheck();

  const name = page.getByLabel("Name", { exact: true });
  await name.fill("");
  await page.getByRole("button", { name: "Save rule" }).click();

  await expect(name).toBeFocused();
  expect(updateRequests).toBe(0);

  await name.fill("Checkout feedback");
  const maxDuration = page.getByLabel("Max duration (seconds)");
  const maxEvents = page.getByLabel("Max events");
  await expect(maxDuration).toHaveAttribute("max", "1800");
  await expect(maxEvents).toHaveAttribute("max", "50000");

  await maxDuration.fill("1801");
  await page.getByRole("button", { name: "Save rule" }).click();
  await expect(maxDuration).toBeFocused();
  expect(updateRequests).toBe(0);

  await maxDuration.fill("1800");
  await maxEvents.fill("50001");
  await page.getByRole("button", { name: "Save rule" }).click();
  await expect(maxEvents).toBeFocused();
  expect(updateRequests).toBe(0);

  await maxEvents.fill("50000");
  await page.getByLabel("Include matcher 1").selectOption("url_regex");
  await page.getByLabel("Include value 1").fill("[");
  await page.getByRole("button", { name: "Save rule" }).click();
  await expect(page.locator("#widget-rule-form .targeting-error")).toHaveText(
    "Enter a valid safe regular expression.",
  );
  expect(updateRequests).toBe(0);
});

test("widget rule save submits a valid update", async ({ page }, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Rule submission is viewport independent",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  await page.getByLabel("Name", { exact: true }).fill("Checkout follow-up");
  await page.getByRole("button", { name: "Save rule" }).click();

  await expect(page.getByRole("status")).toHaveText("Widget rule saved.");
  expect(state.widgetRules[0].name).toBe("Checkout follow-up");
});

test("widget rule contact collection previews and saves the contextual controller notice", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  Object.assign(state.tenant, {
    privacy_policy_url: "https://acme.test/privacy",
    privacy_contact_email: "privacy@acme.test",
    privacy_rights_email: "rights@acme.test",
    privacy_profile_version: 1,
  });
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  await page
    .locator(".project-capability-grid label")
    .filter({ hasText: "Optional email address" })
    .locator("input")
    .check();
  await page
    .getByLabel("Purpose of contact")
    .fill("Send updates about this feedback.");
  await page
    .getByLabel("Lawful basis")
    .fill("Consent for optional status updates.");
  await page
    .getByLabel("Retention period")
    .fill("Retained until withdrawal or the approved case period.");

  const notice = page.locator(".widget-preview-privacy");
  await expect(notice).toContainText("How Acme Studio uses your data");
  await expect(notice).toContainText("Consent for optional status updates.");
  await expect(
    notice.locator('a[href="https://acme.test/privacy"]'),
  ).toHaveText("Privacy policy");
  await expect(page.locator(".contact-controller-warning")).toHaveCount(0);
  if (testInfo.project.name === "mobile") await expect(notice).toBeHidden();
  else await expect(notice).toBeVisible();
  await capture(
    page,
    testInfo,
    `widget-controller-notice-${testInfo.project.name}`,
  );

  await page.getByRole("button", { name: "Save rule" }).click();
  await expect(page.getByRole("status")).toHaveText("Widget rule saved.");
  expect(state.widgetRules[0].collect_contact).toBe(true);
  expect(state.widgetRules[0].localizations.en).toMatchObject({
    privacy_purpose_summary: "Send updates about this feedback.",
    privacy_lawful_basis_summary: "Consent for optional status updates.",
    privacy_retention_summary:
      "Retained until withdrawal or the approved case period.",
  });
});

test("widget rule validation maps nested fields, switches locale, and clears edited errors", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Structured validation behavior is viewport independent",
  );
  const state = createMockState();
  const localization = {
    launcher_label: "Feedback",
    prompt: "How was checkout?",
    comment_placeholder: "Tell us more",
    contact_label: "Email address",
    consent_label: "Email me about this feedback",
    submit_label: "Send feedback",
    success_message: "Your feedback was sent.",
    block_labels: { rating: "Rating", feedback: "Feedback" },
    option_labels: {},
  };
  Object.assign(state.widgetRules[0], {
    response_blocks: [
      { key: "rating", type: "rating", required: false },
      { key: "feedback", type: "free_text", required: false },
    ],
    localizations: {
      en: localization,
      pt: { ...localization, prompt: "Como foi o checkout?" },
    },
  });
  state.nextWidgetRuleFieldErrors = {
    greeting: "must not exceed 500 characters",
    "localizations.pt.block_labels.feedback":
      "must identify a block and contain 1-240 characters",
    "response_blocks[1].key":
      "must contain 1-32 lowercase letters, numbers, or underscores",
  };
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  await page.getByRole("button", { name: "Save rule" }).click();

  const summary = page.locator(".rule-validation-summary");
  await expect(summary).toContainText("greeting");
  await expect(summary).toContainText("localizations.pt.block_labels.feedback");
  await expect(summary).toContainText("response_blocks[1].key");
  await expect(page.getByLabel("Editing language")).toHaveValue("pt");
  const localizedFeedback = page.locator(
    '[data-field-path="localizations.pt.block_labels.feedback"]',
  );
  await expect(localizedFeedback).toBeFocused();
  await expect(localizedFeedback).toHaveAttribute("aria-invalid", "true");
  await expect(localizedFeedback).toHaveAttribute(
    "aria-describedby",
    /^rule-error-/,
  );
  await expect(
    page.locator(
      `#${await localizedFeedback.getAttribute("aria-describedby")}`,
    ),
  ).toHaveText("must identify a block and contain 1-240 characters");

  await localizedFeedback.fill("Conte-nos mais");
  await expect(localizedFeedback).not.toHaveAttribute("aria-invalid", "true");
  await expect(summary).not.toContainText(
    "localizations.pt.block_labels.feedback",
  );

  const blockKey = page.locator('[data-field-path="response_blocks[1].key"]');
  await expect(blockKey).toHaveAttribute("aria-invalid", "true");
  await blockKey.fill("feedback_text");
  await expect(summary).toContainText("greeting");
  await expect(summary).not.toContainText("response_blocks[1].key");

  await page.getByRole("button", { name: "Save rule" }).click();
  await expect(page.getByRole("status")).toHaveText("Widget rule saved.");
  await expect(summary).toHaveCount(0);
});

test("widget lifecycle exposes every status and hides archived rules by default", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Lifecycle editing is viewport independent",
  );
  const state = createMockState();
  Object.assign(state.widgetRules[0], {
    definition_status: "active",
    definition_key: "checkout-pulse",
  });
  state.widgetRules.push({
    ...state.widgetRules[0],
    id: "rule-archived",
    name: "Legacy survey",
    enabled: false,
    definition_status: "archived",
    definition_key: "legacy-survey",
  } as (typeof state.widgetRules)[number]);
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  await expect(page.getByRole("button", { name: /Legacy survey/ })).toHaveCount(
    0,
  );
  await page.getByTitle("Show archived rules").click();
  await page.getByRole("button", { name: /Legacy survey/ }).click();
  await expect(page.getByLabel("Lifecycle status")).toHaveValue("archived");
  await capture(page, testInfo, "widget-lifecycle");

  await page.getByLabel("Lifecycle status").selectOption("paused");
  await page.getByRole("button", { name: "Save rule" }).click();
  await expect(page.getByRole("status")).toHaveText("Widget rule saved.");
  expect(state.widgetRules[1]).toMatchObject({
    definition_status: "paused",
    enabled: false,
  });
});

test("widget appearance resolves workspace, project, and definition overrides", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Appearance configuration is viewport independent",
  );
  const state = createMockState();
  state.tenant.widget_appearance_defaults = {
    theme: "dark",
    accent_color: "#005f56",
  };
  state.projects[0].widget_appearance_overrides = {
    surface_color: "#101817",
    text_color: "#f7faf9",
    logo_url: "https://cdn.example.com/acme.svg",
  };
  state.widgetRules[0].appearance_overrides = {
    position: "bottom-left",
    logo_url: "",
  };
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  await expect(page.locator(".project-widget-preview > header")).toContainText(
    "Dark · Bottom left",
  );
  await expect(page.locator(".widget-preview-logo")).toHaveCount(0);
  await expect(page.locator(".widget-preview-modal")).toHaveCSS(
    "background-color",
    "rgb(16, 24, 23)",
  );
  await expect(page.locator(".widget-preview-modal")).toHaveCSS(
    "color",
    "rgb(247, 250, 249)",
  );
  await capture(page, testInfo, "widget-appearance-hierarchy");

  await page
    .locator(".project-rule-editor")
    .getByLabel("Accent color swatch")
    .fill("#004c45");
  await page.getByRole("button", { name: "Save rule" }).click();
  await expect(page.getByRole("status")).toHaveText("Widget rule saved.");
  expect(state.widgetRules[0].appearance_overrides).toMatchObject({
    position: "bottom-left",
    accent_color: "#004c45",
    logo_url: "",
  });
});

test("workspace widget defaults can be updated with controlled tokens", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Workspace appearance is viewport independent",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Settings");

  const appearance = page.locator(".settings-widget-appearance");
  await appearance.getByLabel("Accent color swatch").fill("#46c754");
  await appearance
    .getByLabel("Logo URL")
    .fill("https://cdn.example.com/acme.svg");
  const accentPreview = appearance.locator(".appearance-accent-preview > span");
  await expect(accentPreview).toHaveCSS("background-color", "rgb(70, 199, 84)");
  await expect(accentPreview).toHaveCSS("color", "rgb(23, 26, 25)");
  const heightBeforeSave = await appearance.evaluate(
    (element) => element.getBoundingClientRect().height,
  );
  await appearance.getByRole("button", { name: "Save defaults" }).click();
  const toast = page.locator(".app-toast");
  await expect(toast).toHaveRole("status");
  await expect(toast).toHaveText("Widget appearance saved.");
  await expect(appearance.getByRole("status")).toHaveCount(0);
  expect(
    await appearance.evaluate(
      (element) => element.getBoundingClientRect().height,
    ),
  ).toBe(heightBeforeSave);
  await appearance.getByRole("button", { name: "Save defaults" }).click();
  await expect(toast).toHaveCount(1);
  await expect(toast).toHaveText("Widget appearance saved.");
  expect(state.tenant.widget_appearance_defaults).toMatchObject({
    accent_color: "#46c754",
    logo_url: "https://cdn.example.com/acme.svg",
  });
});

test("workspace appearance and reporter policy keep validation in their own forms", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Form ownership is viewport independent",
  );
  const state = createMockState();
  state.failNextReporterAccessUpdate = true;
  state.failNextAppearanceUpdate = true;
  await authenticate(page, state);
  await openPrimary(page, "Settings");

  const reporterPolicy = page.locator(".settings-reporter-policy");
  await page.getByLabel("Reporter progress access").uncheck();
  await expect(reporterPolicy.getByRole("alert")).toBeVisible();

  const appearance = page.locator(".settings-widget-appearance");
  const logoURL = appearance.getByLabel("Logo URL");
  await logoURL.fill("http://example.test/logo.svg");
  await appearance.getByRole("button", { name: "Save defaults" }).click();

  await expect(logoURL).toHaveAttribute("aria-invalid", "true");
  const describedBy = await logoURL.getAttribute("aria-describedby");
  expect(describedBy).toBeTruthy();
  await expect(appearance.locator(`#${describedBy}`)).toHaveText(
    "must be an HTTPS URL without credentials or fragments",
  );
  await expect(reporterPolicy.getByRole("alert")).toBeVisible();
  await expect(
    page.locator(".settings-page > .settings-form-error"),
  ).toHaveCount(0);

  await logoURL.fill("https://cdn.example.test/logo.svg");
  await expect(logoURL).not.toHaveAttribute("aria-invalid", "true");
  await expect(
    appearance.getByText(
      "must be an HTTPS URL without credentials or fragments",
    ),
  ).toHaveCount(0);
  await appearance.getByRole("button", { name: "Save defaults" }).click();
  await expect(page.locator(".app-toast")).toHaveText(
    "Widget appearance saved.",
  );
});

test("organization profile validates identity assets and updates the workspace immediately", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  const organization = page.locator(".organization-profile");
  await expect(
    organization.getByRole("heading", { name: "Identity and public defaults" }),
  ).toBeVisible();

  state.failNextIdentityUpload = true;
  await organization.getByLabel("Workspace logo file").setInputFiles({
    name: "unsafe.svg",
    mimeType: "image/svg+xml",
    buffer: Buffer.from("<svg/>"),
  });
  await expect(organization.getByRole("alert")).toContainText(
    "PNG, JPEG, or WebP",
  );
  expect(state.tenant.logo_version).toBeNull();

  await organization.getByLabel("Workspace logo file").setInputFiles({
    name: "acme.png",
    mimeType: "image/png",
    buffer: Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M/wHwAEAQH/2p7iWQAAAABJRU5ErkJggg==",
      "base64",
    ),
  });
  await expect(organization.getByRole("status")).toHaveText(
    "Workspace logo updated.",
  );
  await expect(page.locator(".app-tenant-switcher img")).toBeVisible();

  await organization.getByLabel("Public name").fill("Acme Operations");
  await organization.getByLabel("Default language").selectOption("pt-AO");
  await organization.getByLabel("Default timezone").fill("Africa/Luanda");
  await organization
    .getByLabel("Privacy policy")
    .fill("https://acme.test/privacy");
  await organization
    .getByLabel("Privacy contact email")
    .fill("privacy@acme.test");
  await organization
    .getByLabel("Rights request email")
    .fill("rights@acme.test");
  await organization.getByRole("button", { name: "Save profile" }).click();
  await expect(organization.getByRole("status")).toHaveText(
    "Organization profile saved.",
  );
  await expect(page.locator(".app-tenant-switcher b")).toHaveText(
    "Acme Operations",
  );
  expect(state.tenant).toMatchObject({
    name: "Acme Operations",
    default_locale: "pt-AO",
    default_timezone: "Africa/Luanda",
    privacy_policy_url: "https://acme.test/privacy",
    privacy_contact_email: "privacy@acme.test",
    privacy_rights_email: "rights@acme.test",
    privacy_profile_version: 1,
  });
  await capture(
    page,
    testInfo,
    `organization-profile-${testInfo.project.name}`,
  );

  await organization
    .getByRole("button", {
      name: /(?:Remove workspace logo|Remover logótipo do workspace)/,
    })
    .click();
  await expect(organization.getByRole("status")).toHaveText(
    /(?:Workspace logo removed|Logótipo do workspace removido)\./,
  );
  await expect(page.locator(".app-tenant-switcher img")).toHaveCount(0);
  expect(state.tenant.logo_version).toBeNull();
});

test("organization identity controls are read-only without workspace update access", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Permission behavior is viewport independent",
  );
  const state = createMockState();
  state.member.permissions = state.member.permissions.filter(
    (permission) => permission !== "tenant:update",
  );
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  const organization = page.locator(".organization-profile");
  await expect(
    organization.getByRole("button", { name: "Save profile" }),
  ).toBeDisabled();
  await expect(
    organization.getByRole("button", { name: "Upload logo" }),
  ).toBeDisabled();
  await expect(organization).toContainText("read-only for this account");
});

test("workspace and member identity fall back to initials when assets are unavailable", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Fallback behavior is viewport independent",
  );
  const state = createMockState();
  state.tenant.logo_version = "missing-logo";
  state.member.avatar_version = "missing-avatar";
  state.failIdentityAssets = true;
  await authenticate(page, state);

  await expect(page.locator(".app-tenant-switcher img")).toHaveCount(0);
  await expect(
    page.locator(".app-tenant-switcher .workspace-mark > span"),
  ).toHaveText("AS");
  await expect(page.locator(".app-account-avatar img")).toHaveCount(0);
  await expect(
    page.locator(".app-account-avatar .member-avatar > span"),
  ).toHaveText("AM");
});

test("member profile updates display name and self avatar without a new session", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await page.locator(".app-account-menu summary").click();
  await page.getByRole("link", { name: "Profile", exact: true }).click();
  await expect(page).toHaveURL(/\/profile$/);
  await expect(
    page.getByRole("heading", { name: "Profile", exact: true }),
  ).toBeVisible();

  await page.getByLabel("Display name").fill("Alex Rivera");
  await page.getByRole("button", { name: "Save profile" }).click();
  await expect(page.getByRole("status")).toHaveText("Profile saved.");
  await expect(page.locator(".app-account-menu summary")).toContainText(
    "Alex Rivera",
  );

  await page.getByLabel("Profile photo file").setInputFiles({
    name: "alex.webp",
    mimeType: "image/webp",
    buffer: Buffer.from(
      "UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA",
      "base64",
    ),
  });
  await expect(page.getByRole("status")).toHaveText("Profile photo updated.");
  await expect(page.locator(".profile-avatar img")).toBeVisible();
  await expect(page.locator(".app-account-avatar img")).toBeVisible();
  expect(state.member).toMatchObject({ name: "Alex Rivera" });
  expect(state.member.avatar_version).toMatch(/^avatar-version-/);
  await capture(page, testInfo, `member-profile-${testInfo.project.name}`);

  await page.getByTitle("Remove profile photo").click();
  await expect(page.getByRole("status")).toHaveText("Profile photo removed.");
  await expect(page.locator(".profile-avatar img")).toHaveCount(0);
  expect(state.member.avatar_version).toBeNull();
});

test("member profile switches regional preferences immediately and persists inheritance", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await page.locator(".app-account-menu summary").click();
  await page.getByRole("link", { name: "Profile", exact: true }).click();

  const regionalPreferences = page.locator(".profile-regional-grid");
  const timezoneInput = page.getByLabel("Timezone", { exact: true });
  await expect(regionalPreferences).toBeVisible();
  await expect(timezoneInput).toBeDisabled();
  const preferenceGeometry = await regionalPreferences.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    const controls = [
      ...element.querySelectorAll("select, input.app-input"),
    ].map((control) => control.getBoundingClientRect());
    return {
      left: rect.left,
      right: rect.right,
      viewport: document.documentElement.clientWidth,
      controlHeights: controls.map((control) => control.height),
    };
  });
  expect(preferenceGeometry.left).toBeGreaterThanOrEqual(0);
  expect(preferenceGeometry.right).toBeLessThanOrEqual(
    preferenceGeometry.viewport + 1,
  );
  expect(
    Math.max(...preferenceGeometry.controlHeights) -
      Math.min(...preferenceGeometry.controlHeights),
  ).toBeLessThanOrEqual(1);

  await page.getByLabel("Language", { exact: true }).selectOption("pt-AO");
  await page.getByLabel("Inherit workspace/browser timezone").uncheck();
  await expect(timezoneInput).toBeEnabled();
  await timezoneInput.fill("Africa/Luanda");
  await page.getByRole("button", { name: "Save profile" }).click();

  await expect(page.locator("html")).toHaveAttribute("lang", "pt-AO");
  await expect(page).toHaveTitle("Perfil - Fidibeki");
  await expect(page.getByRole("status")).toHaveText("Perfil guardado.");
  await expect(
    page.getByRole("heading", { name: "Perfil", exact: true }),
  ).toBeVisible();
  expect(state.member).toMatchObject({
    locale: "pt-AO",
    timezone: "Africa/Luanda",
  });

  if (testInfo.project.name === "mobile") {
    await page.getByTitle("Abrir navegação").click();
  }
  const portugueseNavigation = page.getByRole("navigation", {
    name: "Navegação principal",
  });
  await expect(
    portugueseNavigation.getByRole("link", { name: "Projetos", exact: true }),
  ).toBeVisible();
  if (testInfo.project.name === "mobile") {
    await page.getByTitle("Fechar navegação").click();
  }
  await capture(page, testInfo, `regional-profile-${testInfo.project.name}`);

  await page.getByLabel("Idioma", { exact: true }).selectOption("");
  await page.getByLabel("Herdar o fuso do workspace/browser").check();
  await page.getByRole("button", { name: "Guardar perfil" }).click();

  await expect(page.locator("html")).toHaveAttribute("lang", "en");
  await expect(page).toHaveTitle("Profile - Fidibeki");
  await expect(page.getByRole("status")).toHaveText("Profile saved.");
  expect(state.member).toMatchObject({ locale: null, timezone: null });

  await page.goto(`${tenantOrigin}/auth/verify#token=fresh-session-token`);
  await expect(
    page.getByRole("heading", { name: /Good afternoon/ }),
  ).toBeVisible();
  await page.locator(".app-account-menu summary").click();
  await page.getByRole("link", { name: "Profile", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Profile", exact: true }),
  ).toBeVisible();
  await expect(page.getByLabel("Language", { exact: true })).toHaveValue("");
  await expect(
    page.getByLabel("Inherit workspace/browser timezone"),
  ).toBeChecked();
  await expect(page.getByLabel("Timezone", { exact: true })).toBeDisabled();
  await expect(page.locator("html")).toHaveAttribute("lang", "en");
});

test("localized navigation contains active labels and removes the static attention note", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  state.member.locale = "pt-AO";
  await installMockAPI(page, state);
  await page.goto(`${tenantOrigin}/auth/verify#token=test-token`);
  await expect(page.getByRole("heading", { name: /Boa tarde/ })).toBeVisible();

  if (testInfo.project.name === "mobile") {
    await page.getByTitle("Abrir navegação").click();
  }
  let navigation = page.getByRole("navigation", {
    name: "Navegação principal",
  });
  await navigation
    .getByRole("link", { name: "Notificações", exact: true })
    .click();
  await expect(page).toHaveURL(/\/notifications(?:\?|$)/);
  if (testInfo.project.name === "mobile") {
    await page.getByTitle("Abrir navegação").click();
  }
  navigation = page.getByRole("navigation", { name: "Navegação principal" });
  const notifications = navigation.getByRole("link", {
    name: "Notificações",
    exact: true,
  });
  await expect(notifications).toHaveAttribute("data-active", "true");
  const geometry = await notifications.evaluate((element) => {
    const item = element.getBoundingClientRect();
    const rail = element.closest(".app-global-rail")!.getBoundingClientRect();
    const icon = element.querySelector("svg")!.getBoundingClientRect();
    const label = element.querySelector("span")!.getBoundingClientRect();
    return {
      item: { left: item.left, right: item.right },
      rail: { left: rail.left, right: rail.right },
      icon: { left: icon.left, right: icon.right },
      label: { left: label.left, right: label.right },
    };
  });
  expect(geometry.item.left).toBeGreaterThanOrEqual(geometry.rail.left);
  expect(geometry.item.right).toBeLessThanOrEqual(geometry.rail.right + 1);
  expect(geometry.icon.left).toBeGreaterThanOrEqual(geometry.item.left);
  expect(geometry.icon.right).toBeLessThanOrEqual(geometry.item.right + 1);
  expect(geometry.label.left).toBeGreaterThanOrEqual(geometry.item.left);
  expect(geometry.label.right).toBeLessThanOrEqual(geometry.item.right + 1);
  await capture(
    page,
    testInfo,
    `localized-navigation-${testInfo.project.name}`,
  );

  await navigation.getByRole("link", { name: "Feedback", exact: true }).click();
  await expect(page).toHaveURL(/\/feedback(?:\?|$)/);
  await expect(page.locator(".app-queue-note")).toHaveCount(0);
  await expect(
    page.getByText("Os sinais da IA e de moderação permanecem visíveis aqui."),
  ).toHaveCount(0);
});

test("widget appearance controls fit the mobile editor", async ({
  page,
}, testInfo) => {
  test.skip(testInfo.project.name !== "mobile", "Mobile layout check");
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  const editor = page.locator(".project-rule-editor .appearance-editor");
  await editor.scrollIntoViewIfNeeded();
  await expect(editor).toBeVisible();
  const geometry = await editor.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    return {
      left: rect.left,
      right: rect.right,
      viewport: document.documentElement.clientWidth,
    };
  });
  expect(geometry.left).toBeGreaterThanOrEqual(0);
  expect(geometry.right).toBeLessThanOrEqual(geometry.viewport + 1);
  await capture(page, testInfo, "widget-appearance-mobile");
});

test("widget canvas replay is disabled by the anonymous privacy contract", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  const captureCanvas = page.getByRole("checkbox", {
    name: /Capture canvas content/,
  });
  await expect(captureCanvas).toBeDisabled();
  await expect(captureCanvas).not.toBeChecked();
  await expect(page.getByLabel("Canvas capture rate")).toHaveCount(0);

  await expect(
    page
      .getByRole("note")
      .filter({ hasText: "Replay is anonymous by default." }),
  ).toContainText("Replay is anonymous by default.");

  const canvasNote = page
    .getByRole("note")
    .filter({ hasText: "Canvas pixels are always suppressed." });
  await expect(canvasNote).toContainText("pixel content is never recorded");
  await page.getByRole("button", { name: "Save rule" }).click();

  await expect(page.getByRole("status")).toHaveText("Widget rule saved.");
  expect(state.widgetRules[0]).toMatchObject({
    replay_record_canvas: false,
    replay_canvas_fps: 2,
  });
  await canvasNote.scrollIntoViewIfNeeded();
  await capture(
    page,
    testInfo,
    `widget-canvas-policy-${testInfo.project.name}`,
  );
});

test("replay and analytics notices use the full editor width with Portuguese copy", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  state.member.locale = "pt-AO";
  Object.assign(state.widgetRules[0], {
    analytics_consent: {
      mode: "customer_cmp",
      notice_version: "2026-07",
      controller_name: "",
      privacy_policy_url: "",
      supported_locales: ["pt-AO"],
      retention_days: 0,
      expiry_days: 30,
      notices: {},
    },
  });
  await installMockAPI(page, state);
  await page.goto(
    `${tenantOrigin}/auth/verify#token=settings-panel-width-token`,
  );
  await expect(page.getByRole("heading", { name: /Boa tarde/ })).toBeVisible();

  if (testInfo.project.name === "mobile")
    await page.getByTitle("Abrir navegação").click();
  await page
    .getByRole("navigation", { name: "Navegação principal" })
    .getByRole("link", { name: "Projetos", exact: true })
    .click();
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  const replaySection = page.locator(".project-rule-editor section").filter({
    has: page.getByRole("heading", { name: "Replay de sessão" }),
  });
  const analyticsSection = page.locator(".project-rule-editor section").filter({
    has: page.getByRole("heading", { name: "Análise do widget" }),
  });
  const replayPanel = replaySection.locator(".replay-consent-status");
  const replayNotices = replaySection.locator(".replay-privacy-note");
  const analyticsPanel = analyticsSection.locator(".replay-consent-status");
  await analyticsPanel.scrollIntoViewIfNeeded();
  await expect(replayPanel).toContainText(
    "Barreira de consentimento configurada",
  );
  await expect(replayNotices).toHaveCount(2);
  await expect(analyticsPanel).toContainText(
    "A análise do widget aguarda uma autorização estruturada do gestor de consentimento do cliente.",
  );

  for (const element of [
    replayPanel,
    replayNotices.first(),
    replayNotices.last(),
    analyticsPanel,
  ]) {
    const geometry = await element.evaluate((node) => {
      const item = node.getBoundingClientRect();
      const grid = node.parentElement!.getBoundingClientRect();
      return {
        item: { left: item.left, right: item.right, width: item.width },
        grid: { left: grid.left, right: grid.right, width: grid.width },
      };
    });
    expect(geometry.item.left).toBeGreaterThanOrEqual(geometry.grid.left - 1);
    expect(geometry.item.right).toBeLessThanOrEqual(geometry.grid.right + 1);
    expect(geometry.item.width).toBeGreaterThanOrEqual(geometry.grid.width - 1);
  }

  for (const panel of [replayPanel, analyticsPanel]) {
    const geometry = await panel.evaluate((node) => {
      const panelRect = node.getBoundingClientRect();
      const contentRect = node.firstElementChild!.getBoundingClientRect();
      const buttonRect = node.querySelector("button")!.getBoundingClientRect();
      return {
        panel: {
          left: panelRect.left,
          right: panelRect.right,
          width: panelRect.width,
        },
        content: {
          left: contentRect.left,
          right: contentRect.right,
          top: contentRect.top,
          bottom: contentRect.bottom,
          width: contentRect.width,
        },
        button: {
          left: buttonRect.left,
          right: buttonRect.right,
          top: buttonRect.top,
          bottom: buttonRect.bottom,
          width: buttonRect.width,
        },
      };
    });
    expect(geometry.content.left).toBeGreaterThanOrEqual(geometry.panel.left);
    expect(geometry.button.right).toBeLessThanOrEqual(geometry.panel.right + 1);
    if (testInfo.project.name === "mobile") {
      expect(geometry.button.top).toBeGreaterThanOrEqual(
        geometry.content.bottom,
      );
      expect(geometry.button.width).toBeGreaterThanOrEqual(
        geometry.panel.width - 26,
      );
    } else {
      expect(geometry.content.width).toBeGreaterThan(240);
      expect(geometry.button.left).toBeGreaterThanOrEqual(
        geometry.content.right,
      );
    }
  }
  await capture(
    page,
    testInfo,
    `settings-panel-width-portuguese-${testInfo.project.name}`,
  );
});

test("replay activation requires an explicit consent mode", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  Object.assign(state.widgetRules[0], {
    replay_mode: "none",
    replay_consent: {
      mode: "none",
      notice_version: "",
      controller_name: "",
      privacy_policy_url: "",
      supported_locales: [],
      retention_days: 0,
      expiry_days: 0,
      notices: {},
    },
  });
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  const replaySection = page.locator(".project-rule-editor section").filter({
    has: page.getByRole("heading", { name: "Session replay" }),
  });
  await page.getByLabel("Replay mode").selectOption("full");
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByRole("heading", { name: "Choose how consent is collected" }),
  ).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Cancel" })).toHaveCount(2);
  await expect(
    dialog.getByRole("button", { name: "Use this consent mode" }),
  ).toBeDisabled();

  const managedCopy = dialog
    .getByRole("button", { name: /Fidibeki-managed notice/ })
    .locator("span")
    .last();
  const customerCopy = dialog
    .getByRole("button", { name: /Customer CMP/ })
    .locator("span")
    .last();
  await expect
    .poll(async () => {
      const [managedBox, customerBox] = await Promise.all([
        managedCopy.boundingBox(),
        customerCopy.boundingBox(),
      ]);
      const managedWidth = managedBox?.width || 0;
      const customerWidth = customerBox?.width || 0;
      return (
        managedWidth > 100 &&
        customerWidth > 100 &&
        Math.abs(managedWidth - customerWidth) < 2
      );
    })
    .toBe(true);

  await dialog.getByRole("button", { name: /Customer CMP/ }).click();
  await expect(
    dialog.getByRole("button", { name: "Use this consent mode" }),
  ).toBeEnabled();
  await expect(dialog.locator("[data-consent-diagnostic]")).toContainText(
    "Blocked: save this consent mode and notice version before testing.",
  );
  await dialog.getByRole("button", { name: "Use this consent mode" }).click();

  await expect(page.getByText("Consent gate configured")).toBeVisible();
  await expect(
    page.getByText("Replay waits for a structured grant"),
  ).toBeVisible();
  await page.getByRole("button", { name: "Save rule" }).click();
  await expect(page.getByRole("status")).toHaveText("Widget rule saved.");
  expect(state.widgetRules[0]).toMatchObject({
    replay_mode: "full",
    replay_consent: {
      mode: "customer_cmp",
      notice_version: "2026-07",
      supported_locales: ["en"],
      expiry_days: 30,
    },
  });
  await replaySection
    .getByRole("button", { name: "Configure consent" })
    .click();
  const revisionDialog = page.getByRole("dialog");
  await revisionDialog.getByLabel("Notice version").fill("2026-08");
  await expect(revisionDialog.getByRole("alert")).toContainText(
    "Changing the consent mode or notice version invalidates existing grants.",
  );
  await revisionDialog.locator("footer").getByRole("button").first().click();
  await capture(
    page,
    testInfo,
    `replay-consent-configuration-${testInfo.project.name}`,
  );
});

test("analytics consent diagnostic summarizes the saved rule cohort", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  Object.assign(state.widgetRules[0], {
    analytics_consent: {
      mode: "customer_cmp",
      notice_version: "2026-07",
      controller_name: "",
      privacy_policy_url: "",
      supported_locales: ["en"],
      retention_days: 0,
      expiry_days: 30,
      notices: {},
    },
  });
  let diagnosticRequests = 0;
  page.on("request", (request) => {
    if (
      new URL(request.url()).pathname.endsWith("/analytics-consent-diagnostic")
    ) {
      diagnosticRequests += 1;
    }
  });

  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  const analyticsSection = page.locator(".project-rule-editor section").filter({
    has: page.getByRole("heading", { name: "Widget analytics" }),
  });
  await analyticsSection
    .getByRole("button", { name: "Configure consent" })
    .click();

  const dialog = page.getByRole("dialog");
  const diagnostic = dialog.locator("[data-consent-diagnostic]");
  const stats = dialog.locator("[data-diagnostic-stats]");
  await expect(diagnostic).toContainText("42 active · 3 expiring soon");
  await expect(stats).toContainText("Active42");
  await expect(stats).toContainText("Recent (30d)27");
  await expect(stats).toContainText("Denied8");
  await expect(stats).toContainText("Withdrawn5");
  await expect(stats).toContainText("Expired11");
  await expect(dialog).toContainText("Last activity");
  expect(diagnosticRequests).toBe(1);

  await dialog.getByLabel("Notice version").fill("2026-08");
  await expect(diagnostic).toContainText(
    "Blocked: save this consent mode and notice version before checking evidence.",
  );
  expect(diagnosticRequests).toBe(1);
  await dialog
    .getByRole("button", { name: "Refresh consent diagnostic" })
    .click();
  await expect.poll(() => diagnosticRequests).toBe(2);
  await expect(diagnostic).toContainText(
    "Blocked: save this consent mode and notice version before checking evidence.",
  );

  await dialog.getByLabel("Notice version").fill("2026-07");
  await expect(diagnostic).toContainText("42 active · 3 expiring soon");
  await capture(
    page,
    testInfo,
    `analytics-consent-diagnostic-${testInfo.project.name}`,
  );
});

test("widget definitions persist response blocks and localized content", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  await page.getByRole("checkbox", { name: /^Reaction/ }).check();
  const reactionRow = page
    .locator(".response-block-row")
    .filter({ hasText: "reaction" });
  await reactionRow.getByLabel("Required").check();
  await reactionRow.getByLabel("Label (en)").fill("Was this useful?");
  await page.getByLabel("New language").fill("pt-ao");
  await page.getByRole("button", { name: "Add", exact: true }).click();
  await expect(page.getByLabel("Editing language")).toHaveValue("pt-ao");
  await page.getByLabel("Prompt", { exact: true }).fill("Como foi o checkout?");
  await page.getByRole("button", { name: "Save rule" }).click();

  await expect(page.getByRole("status")).toHaveText("Widget rule saved.");
  const saved = state.widgetRules[0] as unknown as {
    response_blocks: Array<{ type: string; required: boolean }>;
    localizations: Record<string, { prompt: string }>;
  };
  expect(
    saved.response_blocks.find((block) => block.type === "reaction"),
  ).toMatchObject({ required: true });
  expect(saved.localizations["pt-ao"]?.prompt).toBe("Como foi o checkout?");
  await expect(page.getByText("Loading project")).toHaveCount(0);
  await expect(
    page.locator(".response-block-row").filter({ hasText: "reaction" }),
  ).toBeVisible();
  await capture(page, testInfo, "widget-definition-blocks");
});

test("widget targeting edits include and exclude conditions with a local URL test", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  await expect(
    page.getByRole("radio", { name: "Specific pages" }),
  ).toHaveAttribute("aria-checked", "true");
  await page.getByRole("button", { name: "Add include" }).click();
  await page.getByLabel("Include matcher 2").selectOption("url_exact");
  await page
    .getByLabel("Include value 2")
    .fill("https://shop.acme.test/pricing/?campaign=summer");
  await page.getByRole("button", { name: "Add exclude" }).click();
  await page.getByLabel("Exclude matcher 1").selectOption("url_prefix");
  await page
    .getByLabel("Exclude value 1")
    .fill("https://shop.acme.test/checkout/private/");

  const testURL = page.getByLabel("Test URL");
  await testURL.fill(
    "https://shop.acme.test/checkout/private/card?asset=BAI#details",
  );
  await page.getByRole("button", { name: "Test", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("Excluded by condition 1.");
  await expect(page.locator(".project-install-diagnostics")).toContainText(
    "Excluded by condition 1.",
  );
  await testURL.fill("https://shop.acme.test/pricing/?asset=BIC#plans");
  await page.getByRole("button", { name: "Test", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText(
    "This rule is eligible on the tested page.",
  );
  await expect(page.locator(".project-install-diagnostics")).toContainText(
    "This rule is eligible on the tested page.",
  );

  await page.getByRole("button", { name: "Save rule" }).click();
  await expect(page.getByText("Widget rule saved.")).toBeVisible();
  expect(state.widgetRules[0]).toMatchObject({
    matcher_type: "url_prefix",
    matcher_value: "https://shop.acme.test/checkout",
    include_conditions: [
      {
        matcher_type: "url_prefix",
        matcher_value: "https://shop.acme.test/checkout",
      },
      {
        matcher_type: "url_exact",
        matcher_value: "https://shop.acme.test/pricing",
      },
    ],
    exclude_conditions: [
      {
        matcher_type: "url_prefix",
        matcher_value: "https://shop.acme.test/checkout/private",
      },
    ],
  });
  await page
    .getByRole("heading", { name: "Page matching" })
    .scrollIntoViewIfNeeded();
  await capture(page, testInfo, "widget-targeting");
});

test("widget diagnostics explain installation and objective eligibility", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  const installation = page.locator(".project-install-diagnostics");
  await expect(installation).toContainText("Not detected");
  await page.getByLabel("Name", { exact: true }).fill("Unsaved checkout pulse");
  state.projects[0].widget_last_seen_at = now;
  await page.getByRole("button", { name: "Refresh diagnostics" }).click();
  await expect(installation).toContainText("Observed");
  await expect(page.getByLabel("Name", { exact: true })).toHaveValue(
    "Unsaved checkout pulse",
  );
  await expect(installation).toContainText("2 configured");
  await expect(installation).toContainText("1 active");
  await capture(
    page,
    testInfo,
    `widget-install-diagnostics-${testInfo.project.name}`,
  );

  await page.getByLabel("Presentation").selectOption("reaction");
  await page.getByLabel("Objective").selectOption("after_engagement");
  const diagnostics = page.locator(".decision-diagnostics");
  await diagnostics.scrollIntoViewIfNeeded();
  await expect(diagnostics).toContainText("Waiting");
  await expect(diagnostics).toContainText(
    "Reaction waits for meaningful engagement",
  );
  await diagnostics.getByLabel("Interactions").fill("1");
  await diagnostics.getByRole("checkbox", { name: "Engaged" }).check();
  await expect(diagnostics).toContainText("Eligible");
  await expect(diagnostics).toContainText("Explicit engagement signal");

  const geometry = await diagnostics.evaluate((element) => {
    const rect = element.getBoundingClientRect();
    return {
      left: rect.left,
      right: rect.right,
      viewport: document.documentElement.clientWidth,
    };
  });
  expect(geometry.left).toBeGreaterThanOrEqual(0);
  expect(geometry.right).toBeLessThanOrEqual(geometry.viewport + 1);
  await capture(page, testInfo, `widget-decision-${testInfo.project.name}`);
});

test("widget preview mirrors launcher and inline presentation states", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "The live preview is hidden on smaller viewports",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  await page.getByLabel("Presentation").selectOption("launcher");
  await expect(page.locator(".widget-preview-trigger")).toBeVisible();
  await expect(page.locator(".widget-preview-modal")).toBeHidden();
  await page
    .getByRole("group", { name: "Launcher preview state" })
    .getByRole("button", { name: "Open" })
    .click();
  await expect(page.locator(".widget-preview-trigger")).toHaveCount(0);
  await expect(page.locator(".widget-preview-modal")).toBeVisible();
  await expect(page.locator(".widget-preview-contact")).toHaveCount(0);
  await page.getByRole("checkbox", { name: "Optional email address" }).check();
  const previewContact = page.locator(".widget-preview-contact");
  await expect(previewContact).toBeVisible();
  await expect(previewContact.locator('input[type="email"]')).toHaveAttribute(
    "placeholder",
    "Email address",
  );
  await expect(previewContact).toContainText(
    "Email me updates about this feedback",
  );

  await page.getByLabel("Presentation").selectOption("inline");
  await expect(page.locator(".widget-preview-modal")).toBeVisible();
  await expect(page.locator(".widget-preview-modal")).toHaveCSS(
    "position",
    "relative",
  );
  await capture(page, testInfo, "widget-presentation-preview");
});

test("launcher variant selector is accessible and defaults to edge tab recommended", async ({
  page,
}) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  await page.getByLabel("Presentation").selectOption("launcher");
  const edgeTab = page.getByRole("radio", { name: "Edge tab" });
  const floatingButton = page.getByRole("radio", { name: "Floating button" });
  await expect(edgeTab).toBeVisible();
  await expect(floatingButton).toBeVisible();
  await expect(edgeTab).toBeChecked();
  await expect(page.getByText("Recommended")).toBeVisible();

  await floatingButton.check();
  await expect(floatingButton).toBeChecked();
  await expect(edgeTab).not.toBeChecked();

  await page.getByLabel("Presentation").selectOption("inline");
  await expect(edgeTab).toHaveCount(0);
  await expect(floatingButton).toHaveCount(0);
});

test("launcher variant preview reflects the selected style and side, and saves the exact payload", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "The live preview is hidden on smaller viewports",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  await page.getByLabel("Presentation").selectOption("launcher");
  const trigger = page.locator(".widget-preview-trigger");
  await expect(trigger).toHaveAttribute("data-launcher-variant", "edge_tab");
  await expect(trigger).toHaveAttribute("data-side", "right");

  await page.getByRole("radio", { name: "Floating button" }).check();
  await expect(trigger).toHaveAttribute(
    "data-launcher-variant",
    "floating_button",
  );

  await page.getByRole("radio", { name: "Edge tab" }).check();
  await expect(trigger).toHaveAttribute("data-launcher-variant", "edge_tab");
  await page.locator(".project-widget-preview").screenshot({
    path: testInfo.outputPath("widget-launcher-variant-preview-panel.png"),
    animations: "disabled",
  });
  await capture(page, testInfo, "widget-launcher-variant-preview");

  await page.getByRole("button", { name: "Save rule" }).click();
  await expect(page.getByRole("status")).toHaveText("Widget rule saved.");
  expect(state.widgetRules[0]).toMatchObject({
    presentation: "launcher",
    launcher_variant: "edge_tab",
  });
});

test("new widget rules require an explicit targeting choice", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Creation behavior is viewport independent",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  await page.getByTitle("New widget rule").click();
  await expect(page.getByLabel("Lifecycle status")).toHaveValue("draft");
  await expect(page.getByRole("radio", { name: "All pages" })).toHaveAttribute(
    "aria-checked",
    "false",
  );
  await expect(
    page.getByRole("radio", { name: "Specific pages" }),
  ).toHaveAttribute("aria-checked", "false");
  await page.getByRole("button", { name: "Save rule" }).click();
  await expect(
    page
      .locator("#widget-rule-form .targeting-error")
      .filter({ hasText: "Choose where this rule should appear." }),
  ).toBeVisible();
  expect(state.widgetRules).toHaveLength(1);

  await page.getByRole("radio", { name: "All pages" }).click();
  await page.getByRole("button", { name: "Save rule" }).click();
  await expect(page.getByText("Widget rule saved.")).toBeVisible();
  expect(state.widgetRules).toHaveLength(2);
  expect(state.widgetRules[1]).toMatchObject({
    matcher_type: "all_pages",
    matcher_value: null,
    include_conditions: [{ matcher_type: "all_pages", matcher_value: null }],
    exclude_conditions: [],
  });
});

test("widget recurrence controls persist each policy", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  const recurrence = page.getByLabel("Show again");
  const days = page.getByLabel("Cooldown days");
  await expect(recurrence).toHaveValue("cooldown");
  await expect(days).toHaveValue("30");

  await recurrence.selectOption("session");
  await expect(days).toHaveCount(0);
  await page.getByRole("button", { name: "Save rule" }).click();
  await expect(page.getByText("Widget rule saved.")).toBeVisible();
  expect(state.widgetRules[0]).toMatchObject({
    recurrence_mode: "session",
    recurrence_days: null,
  });

  await recurrence.selectOption("never");
  await page.getByRole("button", { name: "Save rule" }).click();
  await expect(page.getByText("Widget rule saved.")).toBeVisible();
  expect(state.widgetRules[0]).toMatchObject({
    recurrence_mode: "never",
    recurrence_days: null,
  });

  await recurrence.selectOption("cooldown");
  await page.getByLabel("Cooldown days").fill("45");
  await page.getByRole("button", { name: "Save rule" }).click();
  await expect(page.getByText("Widget rule saved.")).toBeVisible();
  expect(state.widgetRules[0]).toMatchObject({
    recurrence_mode: "cooldown",
    recurrence_days: 45,
  });

  await capture(page, testInfo, "widget-recurrence");
});

test("project settings enforce browser validation before sending an update", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Native form validation is viewport independent",
  );
  const state = createMockState();
  let updateRequests = 0;
  page.on("request", (request) => {
    if (
      request.method() === "PATCH" &&
      new URL(request.url()).pathname === "/v1/projects/project-1"
    )
      updateRequests += 1;
  });
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  const name = page.getByLabel("Project name");
  await name.fill("");
  await page.getByRole("button", { name: "Save project" }).click();

  await expect(name).toBeFocused();
  expect(updateRequests).toBe(0);
});

test("project settings submit a valid update", async ({ page }, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Project submission is viewport independent",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  await page.getByLabel("Project name").fill("European Storefront");
  await page.getByRole("button", { name: "Save project" }).click();

  await expect(page.getByRole("status")).toHaveText("Project settings saved.");
  expect(state.projects[0].name).toBe("European Storefront");
});

test("project deletion confirmation restores focus and guards the pending action", async ({
  page,
}) => {
  const state = createMockState();
  const releaseDelete = holdDestructiveRequests(state);
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  const deleteButton = page.getByRole("button", { name: "Delete project" });
  await deleteButton.click();
  const dialog = page.getByRole("dialog", { name: "Delete Storefront?" });
  await expect(dialog).toContainText(
    "feedback, issues, attachments, and replay data",
  );
  await expect(dialog.getByRole("button", { name: "Cancel" })).toBeFocused();

  await dialog.click({ position: { x: 4, y: 4 } });
  await expect(dialog).not.toBeVisible();
  await expect(deleteButton).toBeFocused();

  await deleteButton.click();
  await page.keyboard.press("Escape");
  await expect(dialog).not.toBeVisible();
  await expect(deleteButton).toBeFocused();

  await deleteButton.click();
  const confirm = dialog.locator(".app-confirmation__action");
  await expect(confirm).toHaveText("Delete project");
  await confirm.click();
  await expect(confirm).toBeDisabled();
  await expect(confirm).toHaveText("Deleting…");
  await expect(dialog.getByRole("button", { name: "Cancel" })).toBeDisabled();

  releaseDelete();
  await expect(page).toHaveURL(/\/projects$/);
  expect(state.destructiveRequests).toEqual(["project:project-1"]);
  expect(state.projects).toHaveLength(0);
});

test("widget preview controls stay out of keyboard and accessibility navigation", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "The preview is hidden on smaller viewports",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();

  const preview = page.locator(".widget-preview-page");
  const receivedFocus = await preview
    .locator("button, textarea")
    .first()
    .evaluate((control) => {
      control.focus();
      return document.activeElement === control;
    });

  expect(receivedFocus).toBe(false);
  await expect(preview.getByRole("button")).toHaveCount(0);
});

test("feedback can be inspected, previewed, and archived", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Covered once as an interaction workflow",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Feedback");
  await page.getByTitle("Open full detail").click();
  await expect(page).toHaveURL(/\/feedback\/feedback-1$/);
  await expect(
    page.getByRole("heading", {
      name: "Checkout froze after I applied a promotion code.",
    }),
  ).toBeVisible();

  await page.getByRole("button", { name: "View" }).click();
  await expect(
    page.getByRole("dialog", { name: "Attachment evidence" }),
  ).toBeVisible();
  await expect(page.getByAltText("Captured feedback screenshot")).toBeVisible();
  await page.getByTitle("Close evidence").click();

  await page.getByRole("button", { name: "Archive" }).click();
  await expect(page.getByText("Feedback marked archived.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Reopen" })).toBeVisible();
  expect(state.feedback[0].status).toBe("archived");
});

test("mobile feedback is identifiable, filterable, and never receives a fabricated page link", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  Object.assign(state.feedback[1], {
    source: "ios",
    page_url: "",
    mobile_application_id: "com.acme.storefront",
    mobile_screen: "/checkout/payment",
    mobile_app_version: "4.8.1",
    mobile_os_version: "18.5",
    os: "iOS",
    device: "phone",
  });

  await authenticate(page, state);
  await openPrimary(page, "Feedback");

  if (testInfo.project.name === "mobile") {
    await page.locator(".feedback-mobile-filters > summary").click();
    const panel = page.locator(".feedback-mobile-filters-panel");
    await panel.getByRole("combobox", { name: "Source filter" }).selectOption("ios");
    await panel.getByRole("button", { name: "Done", exact: true }).click();
  } else {
    await page
      .locator(".feedback-filter-bar-desktop")
      .getByLabel("Source filter")
      .selectOption("ios");
  }

  await expect(page).toHaveURL(/source=ios/);
  await expect(page.getByText("1 loaded")).toBeVisible();
  const row = page.getByRole("listitem").filter({ hasText: "FB-1047" });
  await expect(row.getByLabel("iOS source")).toBeVisible();
  await expect(row).toContainText("/checkout/payment");

  if (testInfo.project.name === "mobile") {
    await row.getByRole("link", { name: "Open FB-1047" }).click();
  } else {
    await row.click();
    const inspector = page.locator(".feedback-inspector");
    await expect(inspector.getByLabel("iOS source")).toBeVisible();
    await expect(inspector).toContainText("/checkout/payment");
    await expect(inspector.locator(".feedback-context-line > a")).toHaveCount(0);
    await inspector.getByTitle("Open full detail").click();
  }

  await expect(page).toHaveURL(/\/feedback\/feedback-2$/);
  await expect(page.locator(".feedback-detail-origin").getByLabel("iOS source")).toBeVisible();
  await expect(page.locator(".feedback-detail-origin")).toContainText("/checkout/payment");
  await expect(page.locator(".feedback-detail-origin > a")).toHaveCount(0);

  await page.getByRole("button", { name: "Environment", exact: true }).click();
  const environment = page.locator(".feedback-environment");
  await expect(environment).toContainText("com.acme.storefront");
  await expect(environment).toContainText("4.8.1");
  await expect(environment).toContainText("18.5");
  await expect(environment.locator("dd a")).toHaveCount(0);
  await expectNoPageOverflow(page);
  await capture(page, testInfo, `feedback-mobile-source-${testInfo.project.name}`);
});

test("feedback sentiment uses semantic labels with an exact-score disclosure", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Feedback");
  if (testInfo.project.name === "mobile") {
    await page.getByRole("link", { name: "Open FB-1048" }).click();
    await expect(page).toHaveURL(/\/feedback\/feedback-1$/);
  }

  const badge = page.locator(
    testInfo.project.name === "mobile"
      ? ".feedback-detail-page .sentiment-badge"
      : ".feedback-inspector .sentiment-badge",
  );
  await expect(badge).toHaveText("Negative");
  await expect(badge).toHaveAttribute(
    "aria-label",
    "Negative sentiment, exact score -0.74",
  );
  await badge.focus();
  await expect(badge).toBeFocused();
  await expect(badge).toHaveCSS("--sentiment-detail-visible", "1");
  await capture(page, testInfo, `feedback-sentiment-${testInfo.project.name}`);
});

test("moderated feedback stays redacted through issue creation", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  const moderated = state.feedback[1];
  Object.assign(moderated as unknown as Record<string, unknown>, {
    title: "Checkout feedback needs review",
    description: "[MODERATED]",
    text: "[MODERATED]",
    text_cleaned: "[MODERATED]",
    ai_suggested_title: "Checkout feedback needs review",
    moderation_flags: ["harassment"],
  });

  await authenticate(page, state);
  await openPrimary(page, "Feedback");
  const row = page.getByRole("listitem").filter({ hasText: "FB-1047" });
  if (testInfo.project.name === "mobile") {
    await row.getByRole("link", { name: "Open FB-1047" }).click();
  } else {
    await row.click();
    await page.getByTitle("Open full detail").click();
  }
  await expect(page).toHaveURL(/\/feedback\/feedback-2$/);
  await expect(
    page.getByRole("heading", { name: "Checkout feedback needs review" }),
  ).toBeVisible();
  await expect(page.locator(".feedback-description")).toHaveText("[MODERATED]");
  await page.getByRole("button", { name: "Create issue" }).click();
  const dialog = page.getByRole("dialog", {
    name: "Create issue from feedback",
  });
  await expect(dialog.getByLabel("Title")).toHaveValue(
    "Checkout feedback needs review",
  );
  await expect(dialog.getByLabel("Description")).toHaveValue("[MODERATED]");
  await capture(page, testInfo, `moderated-feedback-${testInfo.project.name}`);
});

test("feedback status pills remain readable in normal and selected rows", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "This test controls the viewport widths directly",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Feedback");

  for (const width of [1440, 1100, 768]) {
    await page.setViewportSize({ width, height: 900 });
    const pills = page.locator(".feedback-stream article > .status-pill", {
      hasText: "completed",
    });
    await expect(pills).toHaveCount(2);
    const measurements = await pills.evaluateAll((elements) =>
      elements.map((element) => {
        const pill = element as HTMLElement;
        const row = pill.closest("article")!.getBoundingClientRect();
        const bounds = pill.getBoundingClientRect();
        return {
          text: pill.textContent,
          contentFits: pill.scrollWidth <= pill.clientWidth,
          insideRow: bounds.left >= row.left && bounds.right <= row.right,
        };
      }),
    );
    expect(measurements).toEqual([
      { text: "Completed", contentFits: true, insideRow: true },
      { text: "Completed", contentFits: true, insideRow: true },
    ]);
    const overflow = await page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth,
      offenders: Array.from(document.querySelectorAll<HTMLElement>("body *"))
        .filter(
          (element) =>
            element.getBoundingClientRect().right >
            document.documentElement.clientWidth + 1,
        )
        .slice(0, 5)
        .map((element) => ({
          className: element.className,
          right: Math.round(element.getBoundingClientRect().right),
        })),
    }));
    expect(
      overflow.scrollWidth,
      `page should not overflow at ${width}px: ${JSON.stringify(overflow.offenders)}`,
    ).toBeLessThanOrEqual(overflow.clientWidth);
    await capture(page, testInfo, `feedback-status-${width}`);
  }
});

test("create-issue modal stays contained and usable in short viewports", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Feedback");
  await page.getByRole("link", { name: "Open FB-1047" }).click();
  await expect(page).toHaveURL(/\/feedback\/feedback-2$/);
  await page.getByRole("button", { name: "Create issue" }).click();

  const dialog = page.getByRole("dialog", {
    name: "Create issue from feedback",
  });
  const surface = dialog.locator(".app-modal");
  const body = surface.locator(".app-modal__body");
  await expect(surface).toBeVisible();
  await expect(body).toBeVisible();
  await expect(page.locator("body")).toHaveCSS("overflow", "hidden");

  const contained = await surface.evaluate((element) => {
    const bounds = element.getBoundingClientRect();
    const children = ["header", ".app-modal__body", "footer"].map((selector) =>
      element.querySelector(selector)!.getBoundingClientRect(),
    );
    return {
      sensibleWidth: bounds.width <= 580,
      inViewport: bounds.left >= 0 && bounds.right <= window.innerWidth,
      childrenInside: children.every(
        (child) => child.left >= bounds.left && child.right <= bounds.right,
      ),
    };
  });
  expect(contained).toEqual({
    sensibleWidth: true,
    inViewport: true,
    childrenInside: true,
  });
  await capture(
    page,
    testInfo,
    `feedback-create-issue-modal-${testInfo.project.name}`,
  );

  const viewport = page.viewportSize()!;
  await page.setViewportSize({ width: viewport.width, height: 320 });
  const shortGeometry = await surface.evaluate((element) => {
    const bounds = element.getBoundingClientRect();
    const content = element.querySelector<HTMLElement>(".app-modal__body")!;
    return {
      inViewport: bounds.top >= 0 && bounds.bottom <= window.innerHeight,
      bodyScrolls: content.scrollHeight > content.clientHeight,
    };
  });
  expect(shortGeometry).toEqual({ inViewport: true, bodyScrolls: true });
  await expect(dialog.getByRole("button", { name: "Close" })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Cancel" })).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "Create issue" }),
  ).toBeVisible();
});

test("unlinked feedback can be titled and promoted into an editable issue", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Feedback");
  if (testInfo.project.name === "mobile") {
    await page.getByRole("link", { name: "Open FB-1047" }).click();
  } else {
    await page.getByRole("listitem").filter({ hasText: "FB-1047" }).click();
    await page.getByTitle("Open full detail").click();
  }

  await expect(
    page.getByRole("heading", { name: "The new checkout is much clearer." }),
  ).toBeVisible();
  await expect(
    page.getByText("The new checkout is much clearer.", { exact: true }),
  ).toHaveCount(2);
  await page.getByTitle("Edit feedback title").click();
  await page
    .getByLabel("Feedback title")
    .fill("Checkout success page is clearer");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Checkout success page is clearer" }),
  ).toBeVisible();

  await page.getByRole("button", { name: "Create issue" }).click();
  const dialog = page.getByRole("dialog");
  await expect(
    dialog.getByRole("heading", { name: "Create issue from feedback" }),
  ).toBeVisible();
  await expect(dialog.getByLabel("Title")).toHaveValue(
    "Checkout success page is clearer",
  );
  await dialog
    .getByLabel("Description")
    .fill(
      "Customers confirm that the redesigned success page is easier to understand.",
    );
  await dialog.getByRole("button", { name: "Create issue" }).click();

  await expect(page).toHaveURL(/\/issues\/issue-created$/);
  await expect(
    page.getByRole("heading", { name: "Checkout success page is clearer" }),
  ).toBeVisible();
  await expect(
    page.getByText(
      "Customers confirm that the redesigned success page is easier to understand.",
    ),
  ).toBeVisible();
  expect(state.feedback[1].issue_id).toBe("issue-created");
  await capture(page, testInfo, "issue-created-from-feedback");
});

test("expired uploads are not offered an AI processing retry", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  const retryRequests: string[] = [];
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (
      request.method() === "POST" &&
      /^\/v1\/attachments\/[^/]+\/retry$/.test(path)
    )
      retryRequests.push(path);
  });
  await authenticate(page, state);
  await openPrimary(page, "Feedback");
  if (testInfo.project.name === "mobile") {
    await page.getByRole("link", { name: "Open FB-1048" }).click();
  } else {
    await page.getByTitle("Open full detail").click();
  }
  await expect(page).toHaveURL(/\/feedback\/feedback-1$/);
  await expect(page.locator("#attachments")).toContainText("Captured evidence");

  const expiredUpload = page
    .locator(".feedback-attachment-list article")
    .filter({
      has: page.locator(".status-pill", { hasText: "Expired" }),
    });
  await expect(expiredUpload).toBeVisible();
  await expect(expiredUpload.locator(".status-pill")).toHaveText("Expired");
  await expect(expiredUpload).toContainText("Attachment processing failed.");
  await expect(expiredUpload).not.toContainText(
    "attachment upload expired before completion",
  );
  await expect(
    expiredUpload.getByRole("button", { name: "Retry", exact: true }),
  ).toHaveCount(0);
  await expect(page.getByText("1 attachment can be retried.")).toBeVisible();
  await capture(page, testInfo, `attachment-safety-${testInfo.project.name}`);

  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByRole("status")).toHaveText("Attachment retry queued.");
  expect(retryRequests).toEqual(["/v1/attachments/attachment-1/retry"]);
});

test("terminal replay failures are shown as unavailable", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Replay terminal-state behavior is viewport independent",
  );
  const state = createMockState();
  state.failedReplayFeedbackIDs.add("feedback-1");
  await authenticate(page, state);
  await openPrimary(page, "Feedback");
  await page.getByTitle("Open full detail").click();
  await expect(
    page.getByRole("heading", {
      name: "Checkout froze after I applied a promotion code.",
    }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Replay", exact: true }).click();

  await expect(
    page.getByRole("heading", { name: "Session replay unavailable" }),
  ).toBeVisible();
  await expect(
    page.getByText("The replay will appear after its batches are merged."),
  ).toBeVisible();
  await expect(
    page.getByText(
      "No replay events were uploaded before the recording ended.",
    ),
  ).toHaveCount(0);
  await expect(
    page.locator(".feedback-replay-surface .status-pill"),
  ).toHaveText("Failed");
});

test("session replay fits its surface across responsive viewports", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "This test exercises all target viewports explicitly",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Feedback");
  await page.getByTitle("Open full detail").click();
  await expect(
    page.getByRole("heading", {
      name: "Checkout froze after I applied a promotion code.",
    }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Replay", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Play or pause replay" }),
  ).toBeVisible();

  const replayGeometry = () =>
    page.locator(".feedback-replay-surface").evaluate((surface) => {
      const target = surface.querySelector<HTMLElement>(
        ".replay-player-target",
      );
      const player = surface.querySelector<HTMLElement>(".rr-player");
      const frame = surface.querySelector<HTMLElement>(".rr-player__frame");
      const controller = surface.querySelector<HTMLElement>(".rr-controller");
      if (!target || !player || !frame || !controller) return null;
      const targetRect = target.getBoundingClientRect();
      const playerRect = player.getBoundingClientRect();
      const frameRect = frame.getBoundingClientRect();
      const controllerRect = controller.getBoundingClientRect();
      return {
        documentOverflow:
          document.documentElement.scrollWidth -
          document.documentElement.clientWidth,
        playerLeft: playerRect.left,
        playerRight: playerRect.right,
        targetLeft: targetRect.left,
        targetRight: targetRect.right,
        controllerLeft: controllerRect.left,
        controllerRight: controllerRect.right,
        frameRatio: frameRect.width / frameRect.height,
      };
    });

  for (const viewport of [
    { width: 1440, height: 1000 },
    { width: 1024, height: 768 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport);
    await expect
      .poll(
        async () => {
          const geometry = await replayGeometry();
          return (
            geometry !== null &&
            geometry.playerRight <= geometry.targetRight + 1 &&
            geometry.controllerRight <= geometry.targetRight + 1
          );
        },
        { message: `${viewport.width}px replay resize` },
      )
      .toBe(true);
    const geometry = await replayGeometry();

    expect(geometry, `${viewport.width}px replay geometry`).not.toBeNull();
    expect(
      geometry!.documentOverflow,
      `${viewport.width}px document overflow`,
    ).toBeLessThanOrEqual(1);
    expect(
      geometry!.playerLeft,
      `${viewport.width}px player left edge`,
    ).toBeGreaterThanOrEqual(geometry!.targetLeft - 1);
    expect(
      geometry!.playerRight,
      `${viewport.width}px player right edge`,
    ).toBeLessThanOrEqual(geometry!.targetRight + 1);
    expect(
      geometry!.controllerLeft,
      `${viewport.width}px controls left edge`,
    ).toBeGreaterThanOrEqual(geometry!.targetLeft - 1);
    expect(
      geometry!.controllerRight,
      `${viewport.width}px controls right edge`,
    ).toBeLessThanOrEqual(geometry!.targetRight + 1);
    expect(
      geometry!.frameRatio,
      `${viewport.width}px recorded aspect ratio`,
    ).toBeCloseTo(16 / 9, 1);
  }
});

test("session replay renders captured canvas pixels inside the backoffice player", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Canvas replay rendering is viewport independent",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Feedback");
  await page.getByTitle("Open full detail").click();
  await expect(
    page.getByRole("heading", {
      name: "Checkout froze after I applied a promotion code.",
    }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Replay", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Play or pause replay" }),
  ).toBeVisible();

  const replayFrame = page.locator(".rr-player__frame iframe").contentFrame();
  const canvas = replayFrame.locator("#replay-canvas");
  await expect(canvas).toBeAttached();
  await expect
    .poll(async () =>
      canvas.evaluate((element) => {
        const context = (element as HTMLCanvasElement).getContext("2d");
        const pixel = context?.getImageData(0, 0, 1, 1).data;
        return pixel
          ? { red: pixel[0], green: pixel[1], blue: pixel[2], alpha: pixel[3] }
          : null;
      }),
    )
    .toMatchObject({ red: 229, green: 57, blue: 53, alpha: 255 });
});

test("replay fullscreen fills the viewport, preserves aspect ratio, and exits via Escape and the control", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Fullscreen geometry assertions run once on desktop",
  );
  const browserErrors: string[] = [];
  page.on("pageerror", (error) => browserErrors.push(error.message));
  page.on("console", (message) => {
    const text = message.text();
    const expectedSandboxBlock =
      text.startsWith("Blocked script execution in ") &&
      text.includes(
        "frame is sandboxed and the 'allow-scripts' permission is not set",
      );
    if (message.type() === "error" && !expectedSandboxBlock)
      browserErrors.push(text);
  });

  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Feedback");
  await page.getByTitle("Open full detail").click();
  await expect(
    page.getByRole("heading", {
      name: "Checkout froze after I applied a promotion code.",
    }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Replay", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Play or pause replay" }),
  ).toBeVisible();

  const fullscreenSupported = await page.evaluate(
    () => document.fullscreenEnabled,
  );
  test.skip(
    !fullscreenSupported,
    "Fullscreen API is unavailable in this browser context",
  );

  const fullscreenButton = page.getByRole("button", {
    name: "Toggle fullscreen replay",
  });

  const geometry = () =>
    page.locator(".replay-player").evaluate((root) => {
      const player = root.querySelector<HTMLElement>(".rr-player");
      const frame = root.querySelector<HTMLElement>(".rr-player__frame");
      const controller = root.querySelector<HTMLElement>(".rr-controller");
      if (!player || !frame || !controller) return null;
      const rootRect = root.getBoundingClientRect();
      const playerRect = player.getBoundingClientRect();
      const frameRect = frame.getBoundingClientRect();
      const controllerRect = controller.getBoundingClientRect();
      return {
        isFullscreenElement: document.fullscreenElement === root,
        ariaPressed: (
          root.querySelector(
            ".rr-controller__btns > button:last-child",
          ) as HTMLButtonElement | null
        )?.getAttribute("aria-pressed"),
        rootHeight: rootRect.height,
        frameRatio: frameRect.width / frameRect.height,
        controllerVisible:
          controllerRect.height > 0 &&
          controllerRect.bottom <= rootRect.bottom + 1 &&
          controllerRect.top >= rootRect.top - 1,
        playerFitsRoot:
          playerRect.left >= rootRect.left - 1 &&
          playerRect.right <= rootRect.right + 1 &&
          playerRect.top >= rootRect.top - 1 &&
          playerRect.bottom <= rootRect.bottom + 1,
        playerCenteredX:
          Math.abs(
            (playerRect.left + playerRect.right) / 2 -
              (rootRect.left + rootRect.right) / 2,
          ) <= 2,
      };
    });

  const inline = await geometry();
  expect(inline?.isFullscreenElement).toBe(false);
  expect(inline?.ariaPressed).toBe("false");

  async function assertEnteredFullscreen(cycle: number) {
    await expect
      .poll(async () => (await geometry())?.isFullscreenElement, {
        message: `cycle ${cycle} enters fullscreen`,
      })
      .toBe(true);
    await expect
      .poll(
        async () => {
          const g = await geometry();
          return (
            g !== null &&
            g.playerFitsRoot &&
            g.controllerVisible &&
            g.playerCenteredX
          );
        },
        {
          message: `cycle ${cycle} fullscreen layout fits, centers, and keeps controls visible`,
        },
      )
      .toBe(true);

    const fullscreen = await geometry();
    expect(fullscreen, `cycle ${cycle} fullscreen geometry`).not.toBeNull();
    expect(
      fullscreen!.frameRatio,
      `cycle ${cycle} recorded aspect ratio`,
    ).toBeCloseTo(16 / 9, 1);
    expect(
      fullscreen!.rootHeight,
      `cycle ${cycle} fullscreen uses the available viewport`,
    ).toBeGreaterThan(inline!.rootHeight * 1.5);
    expect(
      fullscreen!.ariaPressed,
      `cycle ${cycle} announces the pressed state`,
    ).toBe("true");
  }

  async function assertExitedFullscreen(cycle: number) {
    await expect
      .poll(async () => (await geometry())?.isFullscreenElement, {
        message: `cycle ${cycle} exits fullscreen`,
      })
      .toBe(false);
    await expect
      .poll(
        async () => {
          const g = await geometry();
          return g !== null && Math.abs(g.rootHeight - inline!.rootHeight) < 2;
        },
        { message: `cycle ${cycle} restores inline geometry` },
      )
      .toBe(true);
    const restored = await geometry();
    expect(
      restored?.ariaPressed,
      `cycle ${cycle} clears the pressed state`,
    ).toBe("false");
  }

  // Cycle 0: verify Escape, feature-detected rather than assumed. Playwright's CDP-simulated
  // Escape does not reach Chromium's browser-chrome fullscreen handler in every environment, so
  // this is probed rather than asserted blindly - a real user's Escape key always exits natively.
  await fullscreenButton.click();
  await assertEnteredFullscreen(0);
  await page.keyboard.press("Escape");
  const escapeExited = await page.evaluate(
    () =>
      new Promise<boolean>((resolve) => {
        if (!document.fullscreenElement) return resolve(true);
        const timer = setTimeout(() => {
          document.removeEventListener("fullscreenchange", onChange);
          resolve(!document.fullscreenElement);
        }, 1500);
        function onChange() {
          if (!document.fullscreenElement) {
            clearTimeout(timer);
            document.removeEventListener("fullscreenchange", onChange);
            resolve(true);
          }
        }
        document.addEventListener("fullscreenchange", onChange);
      }),
  );
  if (escapeExited) {
    await assertExitedFullscreen(0);
  } else {
    testInfo.annotations.push({
      type: "known-limitation",
      description:
        "Simulated Escape key presses do not trigger Chromium's native fullscreen exit under Playwright/CDP in this environment; exiting via the control instead. Real users' Escape key exits fullscreen natively per the Fullscreen API spec.",
    });
    await fullscreenButton.click();
    await assertExitedFullscreen(0);
  }

  // Cycle 1: exit via the control button, and prove the cycle leaves no leaks or playback reset.
  await fullscreenButton.click();
  await assertEnteredFullscreen(1);
  await fullscreenButton.click();
  await assertExitedFullscreen(1);

  await expect(
    page.getByRole("button", { name: "Play or pause replay" }),
  ).toBeVisible();
  await expect(page.locator(".replay-player-count")).toHaveText(/3 events/);
  expect(browserErrors).toEqual([]);
});

test("feedback queue links apply their filters without a document reload", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Queue routing is viewport independent",
  );
  const state = createMockState();
  state.feedback[1].status = "archived";
  let documentRequests = 0;
  page.on("request", (request) => {
    if (request.resourceType() === "document") documentRequests += 1;
  });
  await authenticate(page, state);
  await openPrimary(page, "Feedback");
  const documentRequestsBeforeQueueNavigation = documentRequests;
  const desktopFilters = page.locator(".feedback-filter-bar-desktop");

  const queues = page.getByRole("navigation", { name: "Feedback queues" });
  await queues.getByRole("link", { name: "Has replay" }).click();

  await expect(page).toHaveURL(/\/feedback\?.*attachment=replay/);
  await expect(desktopFilters.getByLabel("Evidence filter")).toHaveValue(
    "replay",
  );
  await expect(desktopFilters.getByLabel("Status filter")).toHaveValue("open");
  await expect(page.getByText("1 loaded")).toBeVisible();
  await expect(queues.locator('a[data-active="true"]')).toHaveCount(1);
  await expect(queues.locator('a[data-active="true"]')).toHaveText(
    "Has replay",
  );
  await expect(
    queues.getByRole("link", { name: "Has replay" }),
  ).toHaveAttribute("aria-current", "page");

  await queues.getByRole("link", { name: "Archived" }).click();

  await expect(page).toHaveURL(`${tenantOrigin}/feedback?status=archived`);
  await expect(desktopFilters.getByLabel("Status filter")).toHaveValue(
    "archived",
  );
  await expect(desktopFilters.getByLabel("Evidence filter")).toHaveValue("");
  await expect(page.getByText("1 loaded")).toBeVisible();
  await expect(page.locator(".feedback-stream")).toContainText("FB-1047");
  await expect(queues.locator('a[data-active="true"]')).toHaveCount(1);
  await expect(queues.locator('a[data-active="true"]')).toHaveText("Archived");
  expect(documentRequests).toBe(documentRequestsBeforeQueueNavigation);
});

test("feedback inspector persists operational triage metadata", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "The inspector is a desktop workflow",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Feedback");

  const inspector = page.locator(".feedback-inspector");
  await expect(
    inspector.getByRole("group", { name: "Priority" }),
  ).toBeVisible();
  await inspector.getByRole("button", { name: "Urgent priority" }).click();
  await inspector.getByLabel("Assignee").selectOption("member-2");
  await inspector.getByLabel("Regression tag").check();
  await inspector.getByLabel("Checkout tag").uncheck();

  expect(state.feedback[0].priority).toBe("urgent");
  expect(state.feedback[0].assignee?.id).toBe("member-2");
  expect(state.feedback[0].tags.map((tag) => tag.id)).toEqual([
    "tag-regression",
  ]);

  await page.reload();
  await page.goto(`${tenantOrigin}/auth/verify#token=test-token`);
  await openPrimary(page, "Feedback");
  await expect(
    inspector.getByRole("button", { name: "Urgent priority" }),
  ).toHaveAttribute("aria-pressed", "true");
  await expect(inspector.getByLabel("Assignee")).toHaveValue("member-2");
  await expect(inspector.getByLabel("Regression tag")).toBeChecked();
  const row = page.getByRole("listitem").filter({ hasText: "FB-1048" });
  await expect(row.locator(".feedback-priority-mark")).toHaveText("Urgent");
  await expect(row.locator(".feedback-assignee-mark")).toHaveText("JT");
  await capture(page, testInfo, "feedback-operational-triage");
});

test("bulk feedback triage is atomic and responsive", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Feedback");

  const rows = page.getByRole("listitem");
  const selectBoth = async () => {
    await page.getByLabel("Select FB-1048").check();
    await page.getByLabel("Select FB-1047").check();
    await expect(page.locator(".feedback-bulk-bar")).toContainText(
      "2 selected",
    );
  };
  const openCompactActions = async () => {
    if (testInfo.project.name === "mobile") {
      const actions = page.getByRole("button", { name: "Bulk actions" });
      if ((await actions.getAttribute("aria-expanded")) !== "true")
        await actions.click();
    }
  };
  const selectBulkOption = async (label: string, value: string) => {
    await openCompactActions();
    await page
      .locator(".feedback-bulk-bar")
      .getByLabel(label)
      .selectOption(value);
  };

  const initialRowHeight = (await rows.first().boundingBox())?.height;
  await selectBoth();
  expect((await rows.first().boundingBox())?.height).toBe(initialRowHeight);
  if (testInfo.project.name === "mobile") {
    await openCompactActions();
    await capture(page, testInfo, "dispatch-bulk-actions-menu-mobile");
    await page.getByRole("button", { name: "Bulk actions" }).click();
  }

  state.failNextBulkFeedbackUpdate = true;
  await selectBulkOption("Bulk priority", "urgent");
  await expect(page.getByRole("alert")).toContainText(
    "Feedback could not be updated.",
  );
  await expect(page.getByRole("alert")).not.toContainText(
    "Bulk update rejected.",
  );
  await expect(page.getByLabel("Select FB-1048")).toBeChecked();
  await expect(page.getByLabel("Select FB-1047")).toBeChecked();
  await expect(page.locator(".feedback-bulk-bar")).toBeVisible();
  expect(state.feedback.map((item) => item.priority)).toEqual(["high", null]);
  await capture(
    page,
    testInfo,
    `dispatch-bulk-triage-${testInfo.project.name}`,
  );

  await selectBulkOption("Bulk priority", "urgent");
  await expect(page.locator(".feedback-bulk-bar")).toHaveCount(0);
  expect(state.feedback.map((item) => item.priority)).toEqual([
    "urgent",
    "urgent",
  ]);

  await selectBoth();
  await selectBulkOption("Bulk assignee", "member-2");
  expect(state.feedback.map((item) => item.assignee?.id)).toEqual([
    "member-2",
    "member-2",
  ]);

  await selectBoth();
  await openCompactActions();
  if (testInfo.project.name === "desktop")
    await page.locator(".bulk-tag-popover summary").click();
  await page
    .locator(".feedback-bulk-bar")
    .getByRole("button", { name: "Add Regression tag" })
    .click();
  expect(
    state.feedback.map((item) =>
      item.tags.some((tag) => tag.id === "tag-regression"),
    ),
  ).toEqual([true, true]);

  await selectBoth();
  await openCompactActions();
  await page
    .locator(".feedback-bulk-bar")
    .getByRole("button", { name: "Archive", exact: true })
    .click();
  if (testInfo.project.name === "mobile") {
    await page.locator(".feedback-mobile-filters > summary").click();
    await page
      .locator(".feedback-mobile-filters-panel")
      .getByLabel("Status filter")
      .selectOption("");
    await page.getByRole("button", { name: "Done", exact: true }).click();
  } else {
    await page
      .locator(".feedback-filter-bar-desktop")
      .getByLabel("Status filter")
      .selectOption("");
  }
  await expect(rows).toHaveCount(2);
  expect(state.feedback.map((item) => item.status)).toEqual([
    "archived",
    "archived",
  ]);

  await selectBoth();
  await openCompactActions();
  await page
    .locator(".feedback-bulk-bar")
    .getByRole("button", { name: "Spam", exact: true })
    .click();
  expect(state.feedback.map((item) => item.status)).toEqual(["spam", "spam"]);
  expect(state.bulkFeedbackRequests).toHaveLength(6);
  expect(
    state.bulkFeedbackRequests.every(
      (request) => (request.feedback_ids as string[]).length === 2,
    ),
  ).toBe(true);
});

test("selected feedback can create, unlink, and link issues within one project", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "The complete dialog journey is viewport independent",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Feedback");

  const selectBoth = async () => {
    await page.getByLabel("Select FB-1048").check();
    await page.getByLabel("Select FB-1047").check();
    await expect(page.locator(".feedback-bulk-bar")).toContainText(
      "2 selected",
    );
  };
  const openIssueActions = async () => {
    await page.locator(".bulk-issue-popover summary").click();
  };

  await selectBoth();
  await openIssueActions();
  await page
    .locator(".feedback-bulk-bar")
    .getByRole("button", { name: "Unlink", exact: true })
    .click();
  expect(state.feedback.map((item) => item.issue_id)).toEqual([
    undefined,
    undefined,
  ]);

  await selectBoth();
  await openIssueActions();
  await page
    .locator(".feedback-bulk-bar")
    .getByRole("button", { name: "Create issue", exact: true })
    .click();
  const createDialog = page.getByRole("dialog", {
    name: "Create issue from 2 feedback records",
  });
  await expect(createDialog.locator(".app-modal")).toBeVisible();
  await expect(createDialog.locator(".app-modal__body")).toBeVisible();
  await expect(createDialog.getByLabel("Issue title")).toHaveValue(
    "Checkout froze after I applied a promotion code.",
  );
  await capture(page, testInfo, "feedback-create-issue-dialog");
  await createDialog
    .getByLabel("Issue title")
    .fill("Checkout promotion failures");
  await createDialog
    .getByLabel("Issue description")
    .fill("Two reports confirm the checkout promotion failure.");
  await createDialog
    .getByRole("button", { name: "Create issue", exact: true })
    .click();
  await expect(page).toHaveURL(`${tenantOrigin}/issues/issue-3`);
  expect(state.issues.at(-1)).toMatchObject({
    title: "Checkout promotion failures",
    feedback_count: 2,
  });
  expect(state.feedback.map((item) => item.issue_id)).toEqual([
    "issue-3",
    "issue-3",
  ]);

  await openPrimary(page, "Feedback");
  await selectBoth();
  await openIssueActions();
  await page
    .locator(".feedback-bulk-bar")
    .getByRole("button", { name: "Unlink", exact: true })
    .click();

  await selectBoth();
  await openIssueActions();
  await page
    .locator(".feedback-bulk-bar")
    .getByRole("button", { name: "Link issue", exact: true })
    .click();
  const linkDialog = page.getByRole("dialog", {
    name: "Link 2 feedback records to an issue",
  });
  await linkDialog.getByLabel("Issue").selectOption("issue-2");
  await linkDialog
    .getByRole("button", { name: "Link feedback", exact: true })
    .click();
  await expect(page).toHaveURL(`${tenantOrigin}/issues/issue-2`);
  expect(state.feedback.map((item) => item.issue_id)).toEqual([
    "issue-2",
    "issue-2",
  ]);

  state.projects.push({
    ...state.projects[0],
    id: "project-2",
    name: "Documentation",
    public_key: "pk_live_docs",
  });
  state.feedback[0].issue_id = undefined;
  state.feedback[0].issue_public_id = undefined;
  state.feedback[0].issue_status = undefined;
  state.feedback[1].issue_id = undefined;
  state.feedback[1].issue_public_id = undefined;
  state.feedback[1].issue_status = undefined;
  state.feedback[1].project_id = "project-2";
  state.feedback[1].project_name = "Documentation";
  await openPrimary(page, "Feedback");
  await selectBoth();
  await openIssueActions();
  await expect(page.locator(".bulk-issue-popover")).toContainText(
    "Create and link require feedback from one project.",
  );
  await expect(
    page
      .locator(".feedback-bulk-bar")
      .getByRole("button", { name: "Create issue", exact: true }),
  ).toBeDisabled();
  await expect(
    page
      .locator(".feedback-bulk-bar")
      .getByRole("button", { name: "Link issue", exact: true }),
  ).toBeDisabled();
  await capture(page, testInfo, "feedback-mixed-project-actions");
});

test("Portuguese feedback operations preserve customer content and exact mutations", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  state.member.locale = "pt-AO";
  state.member.timezone = "Africa/Luanda";
  await installMockAPI(page, state);
  await page.goto(
    `${tenantOrigin}/auth/verify#token=feedback-portuguese-token`,
  );
  await expect(
    page.getByRole("heading", { name: "Boa tarde, Alex" }),
  ).toBeVisible();

  const openPortugueseFeedback = async () => {
    if (testInfo.project.name === "mobile") {
      await page.getByTitle("Abrir navegação").click();
    }
    await page
      .getByRole("navigation", { name: "Navegação principal" })
      .getByRole("link", { name: "Feedback", exact: true })
      .click();
    await expect(page).toHaveURL(`${tenantOrigin}/feedback`);
  };

  await openPortugueseFeedback();
  await expect(page).toHaveTitle("Feedback · Fidibeki");
  await expect(page.getByText("2 carregados")).toBeVisible();
  const negativeFeedbackRow = page
    .getByRole("listitem")
    .filter({ hasText: "FB-1048" });
  const positiveFeedbackRow = page
    .getByRole("listitem")
    .filter({ hasText: "FB-1047" });
  await expect(
    negativeFeedbackRow.getByText(
      "Checkout froze after I applied a promotion code.",
      { exact: true },
    ),
  ).toBeVisible();
  await expect(
    positiveFeedbackRow.getByText("The new checkout is much clearer.", {
      exact: true,
    }),
  ).toBeVisible();
  await expect(negativeFeedbackRow).toContainText("Storefront");

  if (testInfo.project.name === "mobile") {
    const summary = page.locator(".feedback-mobile-filters > summary");
    await expect(summary).toHaveAttribute("aria-label", "Filtros, 1 ativo");
    await summary.click();
    const panel = page.locator(".feedback-mobile-filters-panel");
    await expect(
      panel.getByRole("combobox", { name: "Filtro de rating" }),
    ).toBeVisible();
    await expect(
      panel.getByRole("combobox", { name: "Sentimento" }),
    ).toBeVisible();
    await capture(page, testInfo, "feedback-filters-mobile-portuguese");
    await panel.getByRole("button", { name: "Concluído", exact: true }).click();
    await expect(summary).toBeFocused();
  }

  const openBulkActions = async () => {
    if (testInfo.project.name === "mobile") {
      const trigger = page.getByRole("button", { name: "Ações em bulk" });
      if ((await trigger.getAttribute("aria-expanded")) !== "true")
        await trigger.click();
    }
  };
  const openIssueActions = async () => {
    await openBulkActions();
    if (testInfo.project.name === "desktop")
      await page.locator(".bulk-issue-popover summary").click();
  };

  await page.getByLabel("Selecionar FB-1048").check();
  await page.getByLabel("Selecionar FB-1047").check();
  await openBulkActions();
  await page.getByLabel("Prioridade em bulk").selectOption("urgent");
  expect(state.bulkFeedbackRequests.at(-1)).toEqual({
    feedback_ids: ["feedback-1", "feedback-2"],
    priority: "urgent",
  });
  expect(state.feedback.map((item) => item.priority)).toEqual([
    "urgent",
    "urgent",
  ]);

  await page.getByLabel("Selecionar FB-1048").check();
  await openIssueActions();
  await page
    .locator(".feedback-bulk-bar")
    .getByRole("button", { name: "Desligar", exact: true })
    .click();
  expect(state.feedback[0].issue_id).toBeUndefined();

  await page.getByLabel("Selecionar FB-1047").check();
  await openIssueActions();
  await page
    .locator(".feedback-bulk-bar")
    .getByRole("button", { name: "Ligar issue", exact: true })
    .click();
  const linkDialog = page.getByRole("dialog", {
    name: "Ligar 1 feedback a uma issue",
  });
  await linkDialog.getByLabel("Issue").selectOption("issue-2");
  await linkDialog
    .getByRole("button", { name: "Ligar feedback", exact: true })
    .click();
  await expect(page).toHaveURL(`${tenantOrigin}/issues/issue-2`);
  expect(state.feedback[1]).toMatchObject({
    issue_id: "issue-2",
    issue_public_id: "ISS-099",
  });

  await openPortugueseFeedback();
  await page.getByRole("link", { name: "Abrir FB-1048" }).click();
  await expect(page).toHaveURL(`${tenantOrigin}/feedback/feedback-1`);
  await expect(
    page.getByRole("heading", {
      name: "Checkout froze after I applied a promotion code.",
    }),
  ).toBeVisible();
  await expect(
    page.getByText("Checkout froze after I applied a promotion code.", {
      exact: true,
    }),
  ).toHaveCount(2);
  await expect(
    page.locator(".feedback-detail-page .sentiment-badge"),
  ).toHaveAttribute("aria-label", "Sentimento Negativo, valor exato -0,74");
  await expect(page.getByText("Screenshot analysis timed out")).toHaveCount(0);
  await expect(
    page.getByText("attachment upload expired before completion"),
  ).toHaveCount(0);
  await expect(page.getByText("O processamento do anexo falhou.")).toHaveCount(
    2,
  );

  await page.getByRole("button", { name: "Arquivar", exact: true }).click();
  await expect(page.getByRole("status")).toContainText(
    "Feedback marcado como arquivado.",
  );
  expect(state.feedback[0].status).toBe("archived");

  await page.getByRole("button", { name: "Replay", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Reproduzir ou pausar replay" }),
  ).toBeVisible();
  await expect(page.getByText("3 eventos", { exact: true })).toBeVisible();
  await capture(page, testInfo, `feedback-portuguese-${testInfo.project.name}`);
});

test("all-status and clear-filters choices survive route synchronization", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Filter routing is viewport independent",
  );
  const state = createMockState();
  state.feedback[1].status = "archived";
  await authenticate(page, state);
  await openPrimary(page, "Feedback");

  const statusFilter = page
    .locator(".feedback-filter-bar-desktop")
    .getByLabel("Status filter");
  await statusFilter.selectOption("");

  await expect(statusFilter).toHaveValue("");
  await expect(page.getByText("2 loaded")).toBeVisible();
  expect(new URL(page.url()).searchParams.has("status")).toBe(false);
  await page
    .locator(".feedback-stream article")
    .filter({ hasText: "FB-1047" })
    .click();
  await expect(statusFilter).toHaveValue("");
  await expect(page.getByText("2 loaded")).toBeVisible();
  expect(new URL(page.url()).searchParams.has("status")).toBe(false);

  await statusFilter.selectOption("archived");
  await expect(page.getByText("1 loaded")).toBeVisible();
  await page.getByText("More filters", { exact: true }).click();
  await page.getByRole("button", { name: "Clear filters" }).click();

  await expect(statusFilter).toHaveValue("");
  await expect(page.getByText("2 loaded")).toBeVisible();
  expect(new URL(page.url()).searchParams.has("status")).toBe(false);
});

test("desktop feedback toolbar keeps every structured filter directly available", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Desktop toolbar preservation is asserted once",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Feedback");

  const desktopFilters = page.locator(".feedback-filter-bar-desktop");
  await expect(page.locator(".feedback-mobile-filters")).toBeHidden();
  await expect(desktopFilters.getByLabel("Project filter")).toBeVisible();
  await expect(desktopFilters.getByLabel("Status filter")).toBeVisible();
  await expect(desktopFilters.getByLabel("Source filter")).toBeVisible();
  await expect(desktopFilters.getByLabel("Rating filter")).toBeVisible();
  await expect(desktopFilters.getByLabel("Evidence filter")).toBeVisible();
  await expect(page.getByText("More filters", { exact: true })).toBeVisible();
  await capture(page, testInfo, "feedback-filters-desktop-toolbar");

  await desktopFilters.getByLabel("Evidence filter").selectOption("replay");
  await expect(page).toHaveURL(/attachment=replay/);
  await expect(page.getByText("1 loaded")).toBeVisible();

  await desktopFilters.getByLabel("Rating filter").selectOption("2");
  await expect(page).toHaveURL(/rating=2/);
  await expect(page.getByText("1 loaded")).toBeVisible();

  await page.getByText("More filters", { exact: true }).click();
  await desktopFilters
    .getByRole("combobox", { name: "Sentiment" })
    .selectOption("negative");
  await expect(page).toHaveURL(/sentiment=negative/);

  await page.getByRole("button", { name: "Clear filters" }).click();
  // Clearing writes the query through an async router.replace, so the URL
  // settles a tick after the click.
  await expect.poll(() => new URL(page.url()).search).toBe("?id=feedback-1");
  await expect(page.getByText("2 loaded")).toBeVisible();
});

test("mobile Filters disclosure exposes every structured filter and restores focus", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "mobile",
    "Mobile disclosure geometry is asserted once",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Feedback");
  await expectNoPageOverflow(page);

  await expect(
    page.locator(".feedback-search").getByLabel("Search feedback"),
  ).toBeVisible();
  await expect(page.locator(".feedback-filter-bar-desktop")).toBeHidden();
  await expect(page.locator(".feedback-mobile-filters-panel")).toBeHidden();
  await expect(page.getByText("More filters", { exact: true })).toBeHidden();

  const summary = page.locator(".feedback-mobile-filters > summary");
  await expect(summary).toBeVisible();
  await expect(summary).toHaveAttribute("aria-label", "Filters, 1 active");
  await capture(page, testInfo, "feedback-filters-mobile-closed");

  await summary.click();
  await expectNoPageOverflow(page);
  const panel = page.locator(".feedback-mobile-filters-panel");
  await expect(
    panel.getByRole("combobox", { name: "Project filter" }),
  ).toBeVisible();
  await expect(
    panel.getByRole("combobox", { name: "Status filter" }),
  ).toBeVisible();
  await expect(
    panel.getByRole("combobox", { name: "Source filter" }),
  ).toBeVisible();
  await expect(
    panel.getByRole("combobox", { name: "Rating filter" }),
  ).toBeVisible();
  await expect(
    panel.getByRole("combobox", { name: "Evidence filter" }),
  ).toBeVisible();
  await expect(
    panel.getByRole("combobox", { name: "Moderation" }),
  ).toBeVisible();
  await expect(panel.getByRole("combobox", { name: "Issue" })).toBeVisible();
  await expect(
    panel.getByRole("combobox", { name: "AI status" }),
  ).toBeVisible();
  await expect(
    panel.getByRole("combobox", { name: "Sentiment" }),
  ).toBeVisible();
  await expect(panel.getByRole("textbox", { name: "From" })).toBeVisible();
  await expect(panel.getByRole("textbox", { name: "To" })).toBeVisible();

  await panel
    .getByRole("combobox", { name: "Rating filter" })
    .selectOption("2");
  await expect(page).toHaveURL(/rating=2/);
  await expect(summary).toHaveAttribute("aria-label", "Filters, 2 active");

  await panel
    .getByRole("combobox", { name: "Sentiment" })
    .selectOption("negative");
  await expect(page).toHaveURL(/sentiment=negative/);
  await expect(summary).toHaveAttribute("aria-label", "Filters, 3 active");

  await panel
    .getByRole("combobox", { name: "Evidence filter" })
    .selectOption("replay");
  await expect(page).toHaveURL(/attachment=replay/);
  await expect(page.getByText("1 loaded")).toBeVisible();
  await expect(summary).toHaveAttribute("aria-label", "Filters, 4 active");
  await capture(page, testInfo, "feedback-filters-mobile-open");

  await panel.getByRole("button", { name: "Clear filters" }).click();
  await expect.poll(() => new URL(page.url()).search).toBe("?id=feedback-1");
  await expect(page.getByText("2 loaded")).toBeVisible();
  await expect(summary).toHaveAttribute("aria-label", "Filters");

  await panel.getByRole("button", { name: "Done", exact: true }).click();
  await expect(page.locator(".feedback-mobile-filters-panel")).toBeHidden();
  await expect(summary).toBeFocused();
  const focusStyle = await summary.evaluate((element) => ({
    outlineStyle: getComputedStyle(element).outlineStyle,
    outlineWidth: getComputedStyle(element).outlineWidth,
  }));
  expect(focusStyle).toEqual({ outlineStyle: "solid", outlineWidth: "2px" });
});

test("mobile filter panel stays within the viewport and scrolls internally on short screens", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "mobile",
    "Mobile panel geometry is asserted once",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Feedback");

  await page.locator(".feedback-mobile-filters > summary").click();
  const panel = page.locator(".feedback-mobile-filters-panel");
  await expect(panel).toBeVisible();
  await expectNoPageOverflow(page);
  const normalGeometry = await panel.evaluate((element) => ({
    bottom: element.getBoundingClientRect().bottom,
    height: element.getBoundingClientRect().height,
    columns: getComputedStyle(element).gridTemplateColumns.trim().split(/\s+/)
      .length,
  }));
  expect(normalGeometry.bottom).toBeLessThanOrEqual(844);
  expect(normalGeometry.height).toBeLessThan(650);
  expect(normalGeometry.columns).toBe(2);

  await page.setViewportSize({ width: 390, height: 380 });
  await expectNoPageOverflow(page);
  const shortGeometry = await panel.evaluate((element) => ({
    bottom: element.getBoundingClientRect().bottom,
    scrollHeight: element.scrollHeight,
    clientHeight: element.clientHeight,
  }));
  expect(shortGeometry.bottom).toBeLessThanOrEqual(380);
  expect(shortGeometry.scrollHeight).toBeGreaterThan(
    shortGeometry.clientHeight,
  );
  await capture(page, testInfo, "feedback-filters-mobile-short-viewport");

  await page.setViewportSize({ width: 340, height: 844 });
  await expectNoPageOverflow(page);
  const narrowColumns = await panel.evaluate(
    (element) =>
      getComputedStyle(element).gridTemplateColumns.trim().split(/\s+/).length,
  );
  expect(narrowColumns).toBe(1);
  await capture(page, testInfo, "feedback-filters-mobile-narrow-single-column");
});

test("read-only feedback rows preserve grid placement", async ({ page }) => {
  const state = createMockState();
  state.member.permissions = ["feedback:read", "issue:read"];
  await authenticate(page, state);
  await openPrimary(page, "Feedback");

  const row = page.getByRole("listitem").first();
  await expect(row).toBeVisible();
  const placement = await row.evaluate((element) => {
    const rating = element.querySelector<HTMLElement>(".rating-mark")!;
    const style = getComputedStyle(element);
    return {
      actual:
        rating.getBoundingClientRect().left -
        element.getBoundingClientRect().left,
      expected:
        parseFloat(style.paddingLeft) +
        parseFloat(style.gridTemplateColumns.split(/\s+/)[0]!) +
        parseFloat(style.columnGap),
    };
  });
  expect(placement.actual).toBeCloseTo(placement.expected, 0);
});

test("read-only members can inspect feedback without mutation controls", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Permission behavior is viewport independent",
  );
  const state = createMockState();
  state.member.permissions = ["feedback:read", "issue:read"];
  await authenticate(page, state);
  await openPrimary(page, "Feedback");

  await expect(page.getByRole("checkbox", { name: /^Select FB-/ })).toHaveCount(
    0,
  );
  await expect(
    page.getByRole("button", { name: "Archive", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Spam", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Reprocess AI", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("link", { name: "Open issue", exact: true }),
  ).toBeVisible();
  await expect(
    page.locator(".feedback-inspector").getByText("High", { exact: true }),
  ).toBeVisible();
  await expect(
    page
      .locator(".feedback-inspector")
      .getByText("Alex Morgan", { exact: true }),
  ).toBeVisible();
  await expect(
    page
      .locator(".feedback-inspector")
      .getByRole("group", { name: "Priority" }),
  ).toHaveCount(0);
  await expect(
    page.locator(".feedback-inspector").getByLabel("Assignee"),
  ).toHaveCount(0);

  await page.getByTitle("Open full detail").click();
  await expect(page).toHaveURL(/\/feedback\/feedback-1$/);
  await expect(
    page.getByRole("button", { name: "Archive", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Spam", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Reprocess all", exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "View", exact: true }),
  ).toBeVisible();
});

test("issues merge into an existing project issue", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Covered once as an interaction workflow",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Issues");
  await page
    .locator(".issues-table")
    .getByRole("link", { name: /ISS-101/ })
    .click();
  await expect(page).toHaveURL(/\/issues\/issue-1$/);
  await page
    .getByRole("combobox")
    .filter({ has: page.locator("option", { hasText: "Choose target issue" }) })
    .selectOption("issue-2");
  await page.getByRole("button", { name: "Merge" }).click();
  await expect(
    page.getByText("This issue is merged into another issue."),
  ).toBeVisible();
  expect(state.issues[0].merged_into_id).toBe("issue-2");
});

test("issue detail uses singular feedback copy for one linked record", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Copy behavior is viewport independent",
  );
  const state = createMockState();
  state.issues[0].feedback_count = 1;
  state.feedback.splice(1);
  await authenticate(page, state);
  await openPrimary(page, "Issues");
  await page
    .locator(".issues-table")
    .getByRole("link", { name: /ISS-101/ })
    .click();

  await expect(page.locator(".issue-detail-heading")).toContainText(
    "1 feedback record · created",
  );
  await expect(page.locator(".issue-feedback > header > span")).toHaveText(
    "1 record",
  );
});

test("issue comments mention members without requiring a status change", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "The collaboration workflow is viewport independent",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Issues");
  await page
    .locator(".issues-table")
    .getByRole("link", { name: /ISS-101/ })
    .click();

  const comments = page.locator(".issue-comments");
  await comments
    .getByLabel("Internal comment")
    .fill("Please verify this with the checkout team.");
  await comments.getByRole("checkbox", { name: "Jamie Torres" }).check();
  await comments.getByRole("button", { name: "Add comment" }).click();

  await expect(comments).toContainText(
    "Please verify this with the checkout team.",
  );
  await expect(comments).toContainText("Jamie Torres");
  await expect(page.locator(".issue-history")).toContainText(
    "Alex Morgan added a comment",
  );
  expect(state.issueUpdates).toHaveLength(0);
  expect(state.memberNotifications).toHaveLength(1);
  expect(state.memberNotifications[0].recipient_member_id).toBe("member-2");
});

test("issue requests reporter clarification and controls progress access", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "The complete clarification workflow is viewport independent",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Issues");
  await page
    .locator(".issues-table")
    .getByRole("link", { name: /ISS-101/ })
    .click();

  const updatePanel = page.locator(".issue-update-panel");
  await updatePanel.getByRole("combobox").selectOption("needs_info");
  await expect(
    updatePanel.getByRole("button", { name: "Update issue" }),
  ).toBeDisabled();
  await updatePanel
    .getByLabel("Message for reporter")
    .fill("Which promotion code did you use?");
  await updatePanel.getByRole("button", { name: "Update issue" }).click();

  expect(state.issues[0].status).toBe("needs_info");
  expect(state.lastReporterMessage).toBe("Which promotion code did you use?");
  await expect(page.locator(".issue-detail-heading .status-pill")).toHaveText(
    "Needs info",
  );

  await page.getByRole("button", { name: "Revoke progress access" }).click();
  expect(state.issues[0].reporter_access_revoked_at).toBe(now);
  await expect(
    page.getByRole("button", { name: "Enable progress access" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Enable progress access" }).click();
  expect(state.issues[0].reporter_access_revoked_at).toBeNull();
});

test("reporter progress accepts one clarification response without exposing internal context", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "The public response workflow is viewport independent",
  );
  const state = createMockState();
  await authenticate(page, state);
  await page.evaluate(() => {
    window.history.pushState({}, "", "/reporter/test-token");
    window.dispatchEvent(new PopStateEvent("popstate"));
  });

  await expect(
    page.getByRole("heading", { name: state.issues[0].title }),
  ).toBeVisible();
  await expect(
    page.getByText("Which promotion code did you use?"),
  ).toBeVisible();
  await expect(page.getByText("Internal comments")).toHaveCount(0);
  await page.getByLabel("Your response").fill("SUMMER20");
  await page.getByRole("button", { name: "Send response" }).click();

  await expect(page.getByText("Response sent.")).toBeVisible();
  expect(state.reporterResponse).toBe("SUMMER20");
  await expect(page.getByLabel("Your response")).toHaveCount(0);
  await capture(page, testInfo, "reporter-progress");
});

test("member notifications open the related issue and become read", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "The notification workflow is viewport independent",
  );
  const state = createMockState();
  state.memberNotifications.push({
    id: "notification-1",
    recipient_member_id: state.member.id,
    kind: "issue.mentioned",
    issue_id: state.issues[0].id,
    public_id: state.issues[0].public_id,
    issue_title: state.issues[0].title,
    actor: {
      id: state.members[1].id,
      name: state.members[1].name,
      email: state.members[1].email,
    },
    body: "Can you confirm the expected promotion behavior?",
    created_at: now,
  });
  await authenticate(page, state);
  await openPrimary(page, "Notifications");

  await expect(page.getByText("1 unread")).toBeVisible();
  await page
    .getByRole("button", { name: /Jamie Torres mentioned you/ })
    .click();
  await expect(page).toHaveURL(/\/issues\/issue-1$/);
  expect(state.memberNotifications[0].read_at).toBe(now);
});

test("notification empty state shares one centered alignment axis", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Notifications");

  const empty = page.locator(".notifications-empty");
  await expect(
    empty.getByRole("heading", { name: "No notifications" }),
  ).toBeVisible();
  const geometry = await empty.evaluate((element) => {
    const icon = element.querySelector("svg")!.getBoundingClientRect();
    const heading = element.querySelector("h2")!.getBoundingClientRect();
    const copy = element.querySelector("p")!.getBoundingClientRect();
    return {
      iconCenter: icon.left + icon.width / 2,
      headingCenter: heading.left + heading.width / 2,
      copyCenter: copy.left + copy.width / 2,
    };
  });
  expect(
    Math.abs(geometry.iconCenter - geometry.headingCenter),
  ).toBeLessThanOrEqual(1);
  expect(
    Math.abs(geometry.iconCenter - geometry.copyCenter),
  ).toBeLessThanOrEqual(1);
  await expectNoPageOverflow(page);
  await capture(page, testInfo, `notifications-empty-${testInfo.project.name}`);
});

test("issue status can change without nesting the control inside its detail link", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Issues");

  await expect(page.locator(".issues-table a select")).toHaveCount(0);
  await capture(page, testInfo, "dispatch-issues");
  await page.getByLabel("Status for ISS-101").selectOption("resolved");

  await expect(page).toHaveURL(`${tenantOrigin}/issues`);
  await expect(page.getByLabel("Status for ISS-101")).toHaveValue("resolved");
  expect(state.issues[0].status).toBe("resolved");
});

test("issue operational metadata stays independent from linked feedback", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "The complete issue editor is covered once",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Issues");

  const issueRow = page.locator(".issues-row").filter({ hasText: "ISS-101" });
  await expect(issueRow.locator(".issue-priority-mark")).toHaveText("Medium");
  await expect(issueRow.locator(".issue-assignee-mark")).toHaveText("AM");
  await issueRow.getByRole("link").click();

  await page.getByRole("button", { name: "Edit issue details" }).click();
  await page.getByLabel("Issue title").fill("Promotion code blocks checkout");
  await page
    .getByLabel("Issue description")
    .fill("The checkout blocks while promotion validation is running.");
  await page.getByRole("button", { name: "Save details" }).click();

  const triage = page.locator(".issue-operational-triage");
  await triage.getByRole("button", { name: "Urgent priority" }).click();
  await triage.getByLabel("Assignee").selectOption("member-2");
  await triage.getByLabel("Regression tag").check();
  await triage.getByLabel("Checkout tag").uncheck();

  expect(state.issues[0]).toMatchObject({
    title: "Promotion code blocks checkout",
    description: "The checkout blocks while promotion validation is running.",
    priority: "urgent",
    assignee: { id: "member-2" },
  });
  expect(state.issues[0].tags.map((tag) => tag.id)).toEqual(["tag-regression"]);
  expect(state.feedback[0].priority).toBe("high");
  expect(state.feedback[0].assignee?.id).toBe("member-1");
  expect(state.feedback[0].tags.map((tag) => tag.id)).toEqual(["tag-checkout"]);
  expect(state.issueUpdates).toHaveLength(0);

  await page
    .locator(".issue-update-panel")
    .getByRole("combobox")
    .selectOption("in_progress");
  await page
    .locator(".issue-update-panel")
    .getByRole("textbox")
    .fill("Jamie is investigating the checkout path.");
  await page.getByRole("button", { name: "Update issue" }).click();
  await expect(
    page.locator(".issue-history li:not(.issue-created)"),
  ).toHaveCount(1);
  expect(state.issueUpdates).toHaveLength(1);
  await capture(page, testInfo, "issue-operational-triage");

  await openPrimary(page, "Issues");
  const updatedRow = page.locator(".issues-row").filter({ hasText: "ISS-101" });
  await expect(updatedRow).toContainText("Promotion code blocks checkout");
  await expect(updatedRow.locator(".issue-priority-mark")).toHaveText("Urgent");
  await expect(updatedRow.locator(".issue-assignee-mark")).toHaveText("JT");
  await capture(page, testInfo, "issues-operational-markers");
});

test("settings creates one-time credentials and delivery secrets", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Covered once as an interaction workflow",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Settings");

  await page.getByRole("link", { name: "API keys" }).click();
  await expect(page).toHaveURL(/\/settings\?tab=keys$/);
  await expect(
    page.getByRole("heading", { name: "API keys", exact: true }),
  ).toBeVisible();
  const createKeyButton = page.getByRole("button", { name: "Create key" });
  await createKeyButton.click();
  const keyDialog = page.getByRole("dialog", { name: "Create API key" });
  await expect(keyDialog.getByLabel("Name")).toBeFocused();
  await page.keyboard.press("Escape");
  await expect(keyDialog).not.toBeVisible();
  await expect(createKeyButton).toBeFocused();
  await createKeyButton.click();
  await keyDialog.getByLabel("Name").fill("Warehouse exporter");
  await page
    .getByRole("dialog", { name: "Create API key" })
    .getByRole("button", { name: "Create key" })
    .click();
  await expect(
    page.getByRole("heading", { name: "API key created" }),
  ).toBeVisible();
  expect(state.apiKeys).toHaveLength(1);
  await page.getByRole("button", { name: "Done" }).click();

  await page.getByRole("link", { name: "Integrations" }).click();
  await expect(page).toHaveURL(/\/settings\?tab=integrations$/);
  await expect(
    page.getByRole("heading", { name: "Integrations", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Add integration" }).click();
  const dialog = page.getByRole("dialog", { name: "Add integration" });
  await dialog.getByLabel("Name", { exact: true }).fill("Product operations");
  await dialog
    .getByLabel("Webhook URL")
    .fill("https://hooks.acme.test/fidibeki");
  await expect(dialog).toContainText(
    "Fidibeki cannot update, delete, or recall a delivered message",
  );
  await expect(dialog).toContainText("Synthetic preview");
  await dialog.getByRole("checkbox", { name: /^Description/ }).check();
  await dialog.getByRole("checkbox", { name: /^Source page/ }).check();
  await dialog.getByRole("button", { name: "Add integration" }).click();
  await expect(
    page.getByRole("heading", { name: "Signing secret" }),
  ).toBeVisible();
  expect(state.integrations).toHaveLength(1);
  expect(state.integrations[0]?.privacy_fields).toEqual([
    "description",
    "page",
  ]);
  await dialog.getByRole("button", { name: "Done" }).click();

  await page.getByTitle("Configure data disclosure").click();
  const privacyDialog = page.getByRole("dialog", { name: "Data disclosure" });
  await expect(
    privacyDialog.getByRole("checkbox", { name: /^Description/ }),
  ).toBeChecked();
  await privacyDialog.getByRole("checkbox", { name: /^Source page/ }).uncheck();
  await privacyDialog.getByRole("checkbox", { name: /^Rating/ }).check();
  await privacyDialog
    .getByRole("button", { name: "Save disclosure profile" })
    .click();
  await expect(privacyDialog).not.toBeVisible();
  expect(state.integrations[0]?.privacy_fields).toEqual([
    "description",
    "rating",
  ]);
});

test("integration disclosure profile remains usable on responsive layouts", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  state.integrations.push({
    id: "integration-privacy",
    kind: "slack",
    name: "Feedback operations",
    webhook_url: "https://hooks.slack.test/fidibeki",
    channel: "#feedback",
    webhook_secret_fingerprint: null,
    custom_header_names: [],
    events: ["feedback.created", "issue.updated"],
    privacy_fields: [],
    privacy_profile_version: 1,
    destination_capabilities: {
      message_update: false,
      message_delete: false,
      message_recall: false,
      fidibeki_links_require_authentication: true,
      fidibeki_links_access_controlled: true,
    },
    is_active: true,
    created_at: now,
    updated_at: now,
  });
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Integrations" }).click();

  await page.getByTitle("Configure data disclosure").click();
  const dialog = page.getByRole("dialog", { name: "Data disclosure" });
  await expect(dialog).toContainText(
    "Always included: public ID, sanitized title",
  );
  await expect(
    dialog.getByRole("checkbox", { name: /^Description/ }),
  ).not.toBeChecked();
  await dialog.getByRole("checkbox", { name: /^Attachment metadata/ }).check();
  await capture(page, testInfo, "integration-disclosure-profile");
  await dialog.getByRole("button", { name: "Save disclosure profile" }).click();
  await expect(dialog).not.toBeVisible();
  expect(state.integrations[0]?.privacy_fields).toEqual(["attachments"]);
});

test("shared confirmations cover rule, key, and integration removal", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Shared confirmation behavior is covered once",
  );
  const state = createMockState();
  state.apiKeys.push({
    id: "key-1",
    tenant_id: state.tenant.id,
    name: "Reporting",
    token_prefix: "fk_live_report",
    permissions: ["feedback:read"],
    rate_limit_per_min: 100,
    expires_at: null,
    last_used_at: null,
    created_at: now,
    updated_at: now,
  });
  state.integrations.push({
    id: "integration-1",
    kind: "webhook",
    name: "Operations",
    webhook_url: "https://hooks.acme.test/fidibeki",
    channel: null,
    webhook_secret_fingerprint: "sha256:demo",
    custom_header_names: [],
    events: ["feedback.created"],
    is_active: true,
    created_at: now,
    updated_at: now,
  });
  await authenticate(page, state);

  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();
  const deleteRuleButton = page.getByRole("button", { name: "Delete rule" });
  await deleteRuleButton.click();
  const ruleDialog = page.getByRole("dialog", {
    name: "Delete “Checkout pulse”?",
  });
  await expect(
    ruleDialog.getByRole("button", { name: "Cancel" }),
  ).toBeFocused();
  await ruleDialog.getByRole("button", { name: "Delete rule" }).click();
  await expect(ruleDialog).not.toBeVisible();
  expect(state.widgetRules).toHaveLength(0);

  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "API keys" }).click();
  const revokeButton = page.getByTitle("Revoke API key");
  await revokeButton.click();
  const keyDialog = page.getByRole("dialog", { name: "Revoke Reporting?" });
  await expect(keyDialog).toContainText(
    "Requests using this key will fail immediately.",
  );
  await keyDialog.getByRole("button", { name: "Cancel" }).click();
  await expect(revokeButton).toBeFocused();
  await revokeButton.click();
  await keyDialog.getByRole("button", { name: "Revoke API key" }).click();
  await expect(page.getByText("Reporting")).toHaveCount(0);

  await page.getByRole("link", { name: "Integrations" }).click();
  await page.getByTitle("Remove integration").click();
  const integrationDialog = page.getByRole("dialog", {
    name: "Remove Operations?",
  });
  await expect(integrationDialog).toContainText(
    "stop sending events to this destination",
  );
  await integrationDialog
    .getByRole("button", { name: "Remove integration" })
    .click();
  await expect(page.getByText("Operations")).toHaveCount(0);

  expect(state.destructiveRequests).toEqual([
    "rule:rule-1",
    "key:key-1",
    "integration:integration-1",
  ]);
});

test("submission health explains pressure and applies period and project filters", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Submission health" }).click();
  await expect(page).toHaveURL(/\/settings\?tab=health$/);

  const health = page.locator(".submission-health");
  await expect(
    health.getByRole("heading", { name: "Submission health" }),
  ).toBeVisible();
  await expect(health.locator(".submission-health-summary strong")).toHaveText(
    "35",
  );
  await expect(health.locator(".submission-health-categories")).toContainText(
    "Rate limits",
  );
  await expect(health.locator(".submission-health-categories")).toContainText(
    "Provider pressure",
  );
  await expect(health.locator(".submission-health-reasons")).toContainText(
    "AI daily budget exhausted",
  );

  await health.getByLabel("Submission health period").selectOption("24h");
  await expect
    .poll(() => state.submissionHealthRequests.at(-1))
    .toContain("range=24h");
  await health
    .getByLabel("Submission health project")
    .selectOption("project-1");
  await expect
    .poll(() => state.submissionHealthRequests.at(-1))
    .toContain("project_id=project-1");

  const geometry = await health.evaluate((element) => ({
    left: element.getBoundingClientRect().left,
    right: element.getBoundingClientRect().right,
    viewport: document.documentElement.clientWidth,
    pageWidth: document.documentElement.scrollWidth,
  }));
  expect(geometry.left).toBeGreaterThanOrEqual(0);
  expect(geometry.right).toBeLessThanOrEqual(geometry.viewport);
  expect(geometry.pageWidth).toBeLessThanOrEqual(geometry.viewport);
  await capture(
    page,
    testInfo,
    `settings-submission-health-${testInfo.project.name}`,
  );
});

test("submission health distinguishes empty and recoverable error states", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "State transitions are viewport independent",
  );
  const state = createMockState();
  state.submissionHealthMode = "empty";
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Submission health" }).click();
  await expect(
    page.getByText("No pressure recorded in this period."),
  ).toBeVisible();
  await expect(
    page.locator('.submission-health-categories article[data-state=\"quiet\"]'),
  ).toHaveCount(6);

  state.submissionHealthMode = "error";
  await page.getByRole("button", { name: "Refresh submission health" }).click();
  await expect(
    page.getByRole("heading", { name: "Submission health unavailable" }),
  ).toBeVisible();
  state.submissionHealthMode = "data";
  await page.getByRole("button", { name: "Try again" }).click();
  await expect(page.locator(".submission-health-summary strong")).toHaveText(
    "35",
  );
});

test("submission health does not request diagnostics without workspace read access", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Permission behavior is viewport independent",
  );
  const state = createMockState();
  state.member.permissions = state.member.permissions.filter(
    (permission) => permission !== "tenant:read",
  );
  await authenticate(page, state);
  await page.goto(`${tenantOrigin}/settings?tab=health`);

  await expect(
    page.getByRole("heading", { name: "Sign in to Fidibeki" }),
  ).toBeVisible();
  expect(state.submissionHealthRequests).toHaveLength(0);
});

test("data privacy controls cover retention, exports, reporter verification, and retries", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Data & privacy" }).click();
  await expect(page).toHaveURL(/\/settings\?tab=privacy$/);

  await expect(
    page.getByRole("heading", { name: "Retention schedule" }),
  ).toBeVisible();
  await expect(page.locator(".privacy-retention-table")).toContainText(
    "Attachments",
  );
  await expect(page.locator(".privacy-retention-table")).toContainText(
    "90 days",
  );
  await expect(page.locator(".privacy-retention-summary")).toContainText(
    "Enforced",
  );
  await expect(page.locator(".privacy-retention-summary")).toContainText(
    "Decision required",
  );
  await expect(page.locator(".privacy-retention-health")).toContainText(
    "Cleanup health",
  );
  await expect(page.locator(".privacy-retention-health")).toContainText(
    "Workspace magic links",
  );
  await expect(page.locator(".privacy-retention-health")).toContainText(
    "Healthy",
  );
  await expect(page.locator(".privacy-retention-health")).toContainText(
    "Platform admin magic links",
  );
  await expect(page.locator(".privacy-retention-health")).toContainText(
    "Attention",
  );
  await expect(page.locator(".privacy-retention-health")).toContainText(
    "4,000 / 8,000",
  );
  await expect(page.locator(".privacy-retention-health")).toContainText(
    "Session replays",
  );
  await expect(page.locator(".privacy-retention-health")).toContainText(
    "Not observed",
  );
  await expect(page.locator(".privacy-retention-table")).toContainText(
    "External control",
  );
  await expect(page.locator(".privacy-retention-table")).not.toContainText(
    "Workspace lifetime",
  );
  await page
    .locator(".privacy-retention-table .privacy-row", {
      hasText: "Feedback and issue workflow content",
    })
    .getByTitle("View retention details")
    .click();
  await expect(page.locator(".privacy-retention-detail")).toContainText(
    "Approved policy is not yet implemented",
  );
  expect(
    await page.evaluate(
      () =>
        document.documentElement.scrollWidth <=
        document.documentElement.clientWidth,
    ),
  ).toBe(true);
  await expect(page.locator(".privacy-export-table")).toContainText("1.8 MB");
  await expect(page.locator(".privacy-request-table")).toContainText(
    "former.customer@example.test",
  );
  await expect(page.locator(".privacy-request-table")).toContainText(
    "Manual review: 2",
  );
  await capture(
    page,
    testInfo,
    `settings-data-privacy-${testInfo.project.name}`,
  );

  await page.getByTitle("Download workspace export").click();
  expect(state.privacyDownloadCount).toBe(1);

  await page.getByTitle("Retry reporter request").click();
  await expect(page.locator(".privacy-request-table")).toContainText(
    "Processing",
  );
  expect(state.privacyRequests[0].job_status).toBe("pending");

  await page.getByRole("button", { name: "Generate export" }).click();
  await expect(page.getByRole("status")).toContainText(
    "Workspace export queued",
  );
  expect(state.privacyExports[0].status).toBe("pending");

  const requestForm = page.locator(".privacy-request-form");
  await requestForm.getByLabel("Reporter email").fill("requester@example.test");
  await requestForm.getByLabel("Request type").selectOption("export");
  await requestForm.getByRole("button", { name: "Send verification" }).click();
  await expect(page.getByRole("status")).toContainText(
    "Verification email queued",
  );
  expect(state.privacyRequests[0].action).toBe("export");

  await requestForm.getByLabel("Reporter email").fill("remove.me@example.test");
  await requestForm.getByLabel("Request type").selectOption("anonymize");
  await requestForm
    .getByRole("button", { name: "Review anonymization" })
    .click();
  const dialog = page.getByRole("dialog", {
    name: "Confirm reporter anonymization",
  });
  await expect(dialog).toBeVisible();
  const confirmButton = dialog.getByRole("button", {
    name: "Send anonymization request",
  });
  await expect(confirmButton).toBeDisabled();
  await dialog
    .getByLabel(/Type remove\.me@example\.test to confirm/)
    .fill("remove.me@example.test");
  await confirmButton.hover();
  const contrast = await confirmButton.evaluate((element) => {
    function luminance(color: string) {
      const channels = color
        .match(/\d+/g)!
        .slice(0, 3)
        .map(Number)
        .map((value) => {
          const normalized = value / 255;
          return normalized <= 0.04045
            ? normalized / 12.92
            : ((normalized + 0.055) / 1.055) ** 2.4;
        });
      return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722;
    }
    const style = getComputedStyle(element);
    const foreground = luminance(style.color);
    const background = luminance(style.backgroundColor);
    return {
      background: style.backgroundColor,
      ratio:
        (Math.max(foreground, background) + 0.05) /
        (Math.min(foreground, background) + 0.05),
    };
  });
  expect(contrast.background).not.toBe("rgb(247, 250, 249)");
  expect(contrast.ratio).toBeGreaterThanOrEqual(4.5);
  await confirmButton.click();
  await expect(dialog).not.toBeVisible();
  expect(state.privacyRequests[0].action).toBe("anonymize");
});

test("data-rights cases execute access and erasure around legal holds", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Data & privacy" }).click();
  await expect(page).toHaveURL(/\/settings\?tab=privacy$/);

  await expect(
    page.getByRole("heading", { name: "Data-rights cases" }),
  ).toBeVisible();
  const accessCase = page.locator(".privacy-case-entry", {
    hasText: "PRV-ACCESS1",
  });
  await accessCase.getByTitle("Toggle case details").click();
  await expect(accessCase).toContainText("Access");
  await expect(accessCase).toContainText("12 records in scope");
  await expect(accessCase).toContainText("Identity and ownership");
  await expect(accessCase).toContainText("access.requester@example.test");
  await expect(accessCase).toContainText("Request scope");
  await expect(accessCase).toContainText("intake_channel");
  await expect(accessCase).toContainText("Case timeline");
  await expect(accessCase).toContainText("Case created");
  const communicationSection = accessCase.locator(
    ".privacy-case-communications",
  );
  await expect(communicationSection.getByLabel("Subject")).toHaveValue(
    "Update on data-rights case PRV-ACCESS1",
  );
  await communicationSection
    .getByRole("textbox", { name: "Message", exact: true })
    .fill(
      "The requested archive is ready and remains available through the secure case workflow.",
    );
  await communicationSection
    .getByRole("button", { name: "Send message" })
    .click();
  await expect(page.getByRole("status")).toContainText(
    "case communication was queued",
  );
  await expect(communicationSection).toContainText(
    "email provider delivery failed",
  );
  await communicationSection
    .getByRole("button", { name: "Retry delivery" })
    .click();
  await expect(page.getByRole("status")).toContainText(
    "communication retry was queued",
  );
  await expect(communicationSection).toContainText("Pending");
  expect(
    state.privacyCases.find((item) => item.id === "privacy-case-access")
      ?.communications?.[0],
  ).toMatchObject({
    recipient_email: "access.requester@example.test",
    status: "pending",
    attempts: 1,
  });
  await accessCase
    .getByRole("button", { name: "Download access export" })
    .click();
  expect(state.privacyDownloadCount).toBe(1);

  const erasureCase = page.locator(".privacy-case-entry", {
    hasText: "PRV-ERASE1",
  });
  await erasureCase.getByTitle("Toggle case details").click();
  await erasureCase
    .getByPlaceholder("Preservation reason")
    .fill("Preserve while the dispute is reviewed.");
  await erasureCase
    .getByPlaceholder("Legal or case reference (optional)")
    .fill("CASE-2026-17");
  await erasureCase.getByRole("button", { name: "Place hold" }).click();
  await expect(erasureCase).toContainText(
    "Preserve while the dispute is reviewed.",
  );
  await expect(erasureCase).toContainText("1 active hold");
  await expect(
    erasureCase.getByRole("button", { name: "Start erasure" }),
  ).toBeDisabled();

  await erasureCase
    .getByPlaceholder("Reason for releasing the hold")
    .fill("Preservation duty reviewed and ended.");
  await erasureCase.getByRole("button", { name: "Release hold" }).click();
  await expect(erasureCase).toContainText("Released");
  await expect(
    erasureCase.getByRole("button", { name: "Start erasure" }),
  ).toBeEnabled();

  page.once("dialog", (dialog) => dialog.accept());
  await erasureCase.getByRole("button", { name: "Start erasure" }).click();
  await expect(page.getByRole("status")).toContainText(
    "erasure operation was queued",
  );
  await expect(erasureCase).toContainText("Erasure");
  await expect(erasureCase).toContainText("Pending");
  expect(
    state.privacyCases.find((item) => item.id === "privacy-case-erasure")
      ?.execution,
  ).toMatchObject({
    right_type: "erasure",
    job_status: "pending",
  });

  await erasureCase.scrollIntoViewIfNeeded();
  await capture(
    page,
    testInfo,
    `settings-data-rights-cases-${testInfo.project.name}`,
  );
});

test("erasure case records itemized exceptions and a reasoned partial outcome", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Data & privacy" }).click();

  const erasureCase = page.locator(".privacy-case-entry", {
    hasText: "PRV-ERASE2",
  });
  await erasureCase.getByTitle("Toggle case details").click();
  await expect(erasureCase).toContainText(
    "Itemized outcome of the assessed data set",
  );
  await expect(erasureCase).toContainText(
    "Required for the documented contractual dispute.",
  );
  await expect(erasureCase).toContainText("1Retained exceptions");

  const status = erasureCase.getByLabel("Case status");
  await expect(status.locator('option[value="completed"]')).toHaveCount(0);
  await status.selectOption("partially_fulfilled");
  await expect(
    erasureCase.getByRole("button", { name: "Record decision" }),
  ).toBeDisabled();

  await erasureCase
    .getByLabel("Decision summary")
    .fill("The assessed erasure completed except for one documented record.");
  await erasureCase
    .getByLabel("Legal review notes")
    .fill(
      "An operator reviewed the recorded exception; the product made no legal conclusion.",
    );
  await erasureCase
    .getByLabel("Exception or refusal basis")
    .fill(
      "The feedback description remains preserved for the contractual dispute.",
    );
  await erasureCase.getByRole("button", { name: "Record decision" }).click();

  await expect(page.getByRole("status")).toContainText(
    "Case decision recorded.",
  );
  await expect(erasureCase).toContainText("Partially fulfilled");
  await expect(erasureCase).toContainText(
    "The assessed erasure completed except for one documented record.",
  );
  expect(
    state.privacyCases.find(
      (item) => item.id === "privacy-case-erasure-decision",
    ),
  ).toMatchObject({
    status: "partially_fulfilled",
    legal_review_notes:
      "An operator reviewed the recorded exception; the product made no legal conclusion.",
    exception_reason:
      "The feedback description remains preserved for the contractual dispute.",
  });
  await erasureCase.scrollIntoViewIfNeeded();
  await capture(
    page,
    testInfo,
    `settings-erasure-decision-${testInfo.project.name}`,
  );
});

test("portability assessment gates the reporter archive", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Data & privacy" }).click();

  const portabilityCase = page.locator(".privacy-case-entry", {
    hasText: "PRV-PORT1",
  });
  await portabilityCase.getByTitle("Toggle case details").click();
  await expect(portabilityCase).toContainText(
    "No portability decision has been recorded.",
  );
  await expect(
    portabilityCase.getByRole("button", { name: "Generate portable archive" }),
  ).toHaveCount(0);

  await portabilityCase
    .locator(".privacy-case-portability select")
    .first()
    .selectOption("partially_approved");
  await portabilityCase
    .getByRole("checkbox", { name: "Session replay manifests" })
    .uncheck();
  await portabilityCase
    .getByLabel("Assessment reason")
    .fill(
      "Reporter-provided records and linked operational evidence are portable.",
    );
  await portabilityCase
    .getByRole("button", { name: "Record assessment" })
    .click();
  await expect(portabilityCase).toContainText("Partially approved");
  await expect(portabilityCase).toContainText("JSONL files in a ZIP archive");
  await expect(
    portabilityCase.getByRole("button", { name: "Generate portable archive" }),
  ).toBeEnabled();

  await portabilityCase
    .getByRole("button", { name: "Generate portable archive" })
    .click();
  await expect(page.getByRole("status")).toContainText(
    "portable archive was queued",
  );
  expect(
    state.privacyCases.find((item) => item.id === "privacy-case-portability")
      ?.execution,
  ).toMatchObject({
    right_type: "portability_assessment",
    job_status: "pending",
  });
  await portabilityCase.scrollIntoViewIfNeeded();
  await capture(
    page,
    testInfo,
    `settings-portability-assessment-${testInfo.project.name}`,
  );
});

test("member access case queues a member-scoped archive", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Data & privacy" }).click();

  const memberCase = page.locator(".privacy-case-entry", {
    hasText: "PRV-MEMBER1",
  });
  await expect(memberCase).toContainText("Jamie Torres");
  await memberCase.getByTitle("Toggle case details").click();
  await expect(
    memberCase.getByRole("button", { name: "Start access export" }),
  ).toBeEnabled();

  await memberCase.getByRole("button", { name: "Start access export" }).click();
  await expect(page.getByRole("status")).toContainText(
    "access export was queued",
  );
  expect(
    state.privacyCases.find((item) => item.id === "privacy-case-member-access")
      ?.execution,
  ).toMatchObject({
    right_type: "access",
    job_status: "pending",
  });
  expect(
    state.privacyCases.find((item) => item.id === "privacy-case-member-access")
      ?.execution?.request_status,
  ).toBeUndefined();
  await memberCase.scrollIntoViewIfNeeded();
  await capture(
    page,
    testInfo,
    `settings-member-access-${testInfo.project.name}`,
  );
});

test("member rectification applies selected profile changes and preserves evidence", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Data & privacy" }).click();

  const rectificationCase = page.locator(".privacy-case-entry", {
    hasText: "PRV-RECTIFY",
  });
  await expect(rectificationCase).toContainText("Jamie Torres");
  await rectificationCase.getByTitle("Toggle case details").click();
  await expect(rectificationCase).toContainText(
    "No profile corrections have been applied.",
  );

  const integrationSection = rectificationCase.locator(
    ".privacy-integration-actions",
  );
  await integrationSection
    .getByRole("button", { name: "Refresh checklist" })
    .click();
  await expect(page.getByRole("status")).toContainText("1 destinations added");
  await expect(integrationSection).toContainText("Slack · Product feedback");
  await expect(integrationSection).toContainText("Manual action required");
  await expect(integrationSection).toContainText(
    "Apply the verified correction or complete the assessed erasure",
  );
  await expect(
    integrationSection.getByRole("button", { name: "Record completion" }),
  ).toHaveCount(0);

  await rectificationCase.getByRole("checkbox", { name: "Name" }).check();
  await rectificationCase
    .getByRole("textbox", { name: "Name" })
    .fill("Jamie Corrected");
  await rectificationCase.getByRole("checkbox", { name: "Locale" }).check();
  await rectificationCase
    .getByRole("combobox", { name: "Locale" })
    .selectOption("pt-PT");
  await rectificationCase
    .getByLabel("Decision reason")
    .fill("Identity evidence confirmed the corrected member profile.");
  await rectificationCase
    .getByRole("button", { name: "Apply correction" })
    .click();

  await expect(page.getByRole("status")).toContainText(
    "Member profile correction applied.",
  );
  await expect(rectificationCase).toContainText("Jamie Corrected");
  await expect(rectificationCase).toContainText("Jamie Torres");
  await expect(rectificationCase).toContainText("en");
  await expect(rectificationCase).toContainText("pt-PT");
  await expect(rectificationCase).toContainText(
    "Identity evidence confirmed the corrected member profile.",
  );
  expect(
    state.members.find((member) => member.id === "member-2"),
  ).toMatchObject({
    name: "Jamie Corrected",
    locale: "pt-PT",
    timezone: "America/Toronto",
  });
  await integrationSection
    .getByLabel("Completion evidence")
    .fill("Slack retention owner confirmed ticket EXT-1842.");
  await integrationSection
    .getByRole("button", { name: "Record completion" })
    .click();
  await expect(page.getByRole("status")).toContainText(
    "Manual destination evidence recorded.",
  );
  await expect(integrationSection).toContainText("Completed");
  expect(
    state.privacyCases.find(
      (item) => item.id === "privacy-case-member-rectification",
    )?.integration_actions?.[0],
  ).toMatchObject({
    status: "completed",
    completion_note: "Slack retention owner confirmed ticket EXT-1842.",
  });

  await integrationSection
    .getByRole("button", { name: "Prepare recipient notice" })
    .click();
  const recipientSection = rectificationCase.locator(
    ".privacy-recipient-actions",
  );
  await expect(
    recipientSection.getByLabel("Destination checklist"),
  ).toHaveValue("privacy-case-member-rectification-integration-action-1");
  await expect(
    recipientSection.getByLabel("Recipient", { exact: true }),
  ).toHaveValue("Slack · Product feedback");
  await recipientSection
    .getByLabel("Recipient", { exact: true })
    .fill("Payroll processor");
  await recipientSection
    .getByLabel("Recipient email")
    .fill("privacy@processor.example");
  await recipientSection
    .getByLabel("Approved instruction")
    .fill(
      "Correct the approved profile fields in the downstream payroll record.",
    );
  await recipientSection
    .getByRole("button", { name: "Send instruction" })
    .click();
  await expect(page.getByRole("status")).toContainText(
    "recipient action was queued",
  );
  await expect(recipientSection).toContainText("Payroll processor");
  await expect(recipientSection).toContainText(
    "email provider delivery failed",
  );
  await recipientSection
    .getByRole("button", { name: "Retry delivery" })
    .click();
  await expect(page.getByRole("status")).toContainText(
    "recipient action retry was queued",
  );
  await expect(recipientSection).toContainText("Pending");
  expect(
    state.privacyCases.find(
      (item) => item.id === "privacy-case-member-rectification",
    )?.recipient_actions?.[0],
  ).toMatchObject({
    recipient_email: "privacy@processor.example",
    integration_action_id:
      "privacy-case-member-rectification-integration-action-1",
    status: "pending",
    attempts: 1,
  });
  await expect(integrationSection).toContainText("1 linked notice");
  await rectificationCase.scrollIntoViewIfNeeded();
  await capture(
    page,
    testInfo,
    `settings-member-rectification-${testInfo.project.name}`,
  );
});

test("reporter feedback rectification corrects content without rewriting the linked issue", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  const originalTitle = state.feedback[0].title;
  const originalIssueDescription = state.issues[0].description;
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Data & privacy" }).click();

  const rectificationCase = page.locator(".privacy-case-entry", {
    hasText: "PRV-FEEDBACK",
  });
  await rectificationCase.getByTitle("Toggle case details").click();
  await expect(
    rectificationCase.getByRole("combobox", { name: "Feedback" }),
  ).toHaveValue("feedback-1");
  await expect(
    rectificationCase.getByLabel("Corrected description"),
  ).toHaveValue("Checkout froze after I applied a promotion code.");

  await rectificationCase
    .getByLabel("Corrected description")
    .fill(
      "Checkout became unresponsive after I applied the SUMMER promotion code.",
    );
  await rectificationCase
    .getByLabel("Decision reason")
    .fill("The verified reporter clarified the original sequence of events.");
  await rectificationCase
    .getByRole("button", { name: "Apply content correction" })
    .click();

  await expect(page.getByRole("status")).toContainText(
    "Feedback content correction applied.",
  );
  await expect(rectificationCase).toContainText(
    "Checkout froze after I applied a promotion code.",
  );
  await expect(rectificationCase).toContainText(
    "Checkout became unresponsive after I applied the SUMMER promotion code.",
  );
  await expect(rectificationCase).toContainText(
    "The verified reporter clarified the original sequence of events.",
  );
  await expect(rectificationCase).toContainText(
    "Text-derived AI recommendations were invalidated.",
  );
  expect(state.feedback[0]).toMatchObject({
    title: originalTitle,
    description:
      "Checkout became unresponsive after I applied the SUMMER promotion code.",
    text: "Checkout became unresponsive after I applied the SUMMER promotion code.",
    text_cleaned: "",
    ai_status: "pending",
  });
  expect(state.issues[0].description).toBe(originalIssueDescription);
  await rectificationCase.scrollIntoViewIfNeeded();
  await capture(
    page,
    testInfo,
    `settings-feedback-rectification-${testInfo.project.name}`,
  );
});

test("access audit log lists events, applies filters, and exports CSV", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Filter interactions are viewport independent",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Access audit" }).click();
  await expect(page).toHaveURL(/\/settings\?tab=audit$/);

  await expect(
    page.getByRole("heading", { name: "Access audit log" }),
  ).toBeVisible();
  await expect(page.locator(".access-audit-table")).toContainText(
    "attachment-1",
  );
  await expect(page.locator(".access-audit-table")).toContainText("session-1");
  await expect(page.locator(".access-audit-table")).toContainText(
    "Support access · CASE-42",
  );
  await expect(
    page.getByRole("button", { name: "Export CSV · max. 5,000" }),
  ).toBeVisible();
  await capture(
    page,
    testInfo,
    `settings-access-audit-${testInfo.project.name}`,
  );

  await page
    .locator(".access-audit-filters select")
    .nth(2)
    .selectOption("denied");
  await expect(page.locator(".access-audit-table")).not.toContainText(
    "attachment-1",
  );
  await expect(page.locator(".access-audit-table")).toContainText("session-1");
  await page.locator(".access-audit-filters select").nth(2).selectOption("");

  await page.getByLabel("Support access only").check();
  await expect(page.locator(".access-audit-table")).not.toContainText(
    "session-1",
  );
  await expect(page.locator(".access-audit-table")).toContainText(
    "Support access · CASE-42",
  );
  await page.getByLabel("Support access only").uncheck();
  await expect(page.locator(".access-audit-table")).toContainText("session-1");

  const downloadPromise = page.waitForEvent("download");
  await page.getByRole("button", { name: "Export CSV · max. 5,000" }).click();
  const download = await downloadPromise;
  expect(download.suggestedFilename()).toBe("fidibeki-access-events.csv");
});

test("access audit surfaces governance signals", async ({ page }, testInfo) => {
  const state = createMockState();
  state.accessEventSignals = [
    {
      type: "bulk_access",
      actor_type: "human",
      actor_id: "member-owner",
      event_count: 86,
      distinct_resources: 24,
      denied_count: 3,
      last_access_at: "2026-07-21T12:00:00.000Z",
    },
    {
      type: "revoked_credential",
      actor_type: "human",
      actor_id: "member-removed",
      event_count: 12,
      detail: "removed",
      last_access_at: "2026-07-20T10:00:00.000Z",
    },
    {
      type: "unusual_api_key",
      actor_type: "api_key",
      actor_id: "api-key-durable",
      event_count: 31,
      detail: "no_expiration",
      last_access_at: "2026-07-19T09:00:00.000Z",
    },
    {
      type: "support_access_volume",
      actor_type: "platform_admin",
      actor_id: "platform-admin-1",
      event_count: 7,
      distinct_resources: 7,
      last_access_at: "2026-07-22T08:00:00.000Z",
    },
  ];

  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Access audit" }).click();

  const signals = page.locator(".access-audit-signals-table");
  await expect(signals).toContainText("Bulk access");
  await expect(signals).toContainText("24 resources · 3 denied");
  await expect(signals).toContainText("Access by inactive member");
  await expect(signals).toContainText("Removed");
  await expect(signals).toContainText("Non-expiring API key");
  await expect(signals).toContainText("No expiration");
  await expect(signals).toContainText("Unusual platform-support access volume");
  await expect(signals).toContainText("7 resources accessed");
  await signals.scrollIntoViewIfNeeded();
  await capture(
    page,
    testInfo,
    `settings-access-audit-signals-${testInfo.project.name}`,
  );
});

test("access audit tab requires audit:read and is hidden without it", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Permission behavior is viewport independent",
  );
  const state = createMockState();
  state.member.permissions = state.member.permissions.filter(
    (permission) => permission !== "audit:read",
  );
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Access audit" }).click();
  await expect(page).toHaveURL(/\/settings\?tab=audit$/);
  await expect(page.getByText("Audit read access required")).toBeVisible();
  await expect(page.locator(".access-audit-table")).toHaveCount(0);
});

test("admin resolves a manual-review queue and the request completes only once every item is decided", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  state.privacyRequests[0].status = "awaiting_manual_review";
  state.privacyRequests[0].job_status = "awaiting_manual_review";
  state.privacyRequests[0].manual_review_count = 2;
  state.reviewItems = [
    {
      id: "review-item-1",
      resource_type: "feedback",
      resource_id: "feedback-1",
      fields: ["description"],
      status: "pending",
      disclosed_destinations: { slack: 1 },
      created_at: "2026-07-21T12:05:00.000Z",
      updated_at: "2026-07-21T12:05:00.000Z",
    },
    {
      id: "review-item-2",
      resource_type: "issue_comment",
      resource_id: "comment-1",
      fields: ["body"],
      status: "pending",
      created_at: "2026-07-21T12:05:00.000Z",
      updated_at: "2026-07-21T12:05:00.000Z",
    },
  ];
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Data & privacy" }).click();
  await expect(page).toHaveURL(/\/settings\?tab=privacy$/);

  const requestRow = page.locator(".privacy-request-table .privacy-row", {
    hasText: "former.customer@example.test",
  });
  await expect(requestRow).toContainText("Awaiting review");
  await expect(requestRow).toContainText("Manual review: 2");

  await requestRow.getByTitle("Show review queue").click();
  const queue = page.locator(".privacy-review-queue");
  await expect(queue).toBeVisible();
  await expect(queue).toContainText("Feedback");
  await expect(queue).toContainText("description");
  await expect(queue).toContainText("Already disclosed to: Slack (1)");
  await expect(queue).toContainText("Issue comment");
  await capture(
    page,
    testInfo,
    `settings-data-privacy-review-queue-${testInfo.project.name}`,
  );

  // Deciding the second item first (while the first is still pending) keeps the queue open so
  // the confirmed decision is visible before the request completes and the panel unmounts.
  const items = queue.locator(".privacy-review-item");
  const secondItem = items.nth(1);
  await secondItem.getByRole("button", { name: "Retain (exception)" }).click();
  await expect(page.getByRole("alert")).toContainText(
    "A decision reason is required",
  );
  expect(state.reviewItems[1].status).toBe("pending");

  await secondItem
    .getByPlaceholder("Decision reason (required to retain)")
    .fill("Needed for an open support ticket");
  await secondItem.getByRole("button", { name: "Retain (exception)" }).click();
  await expect(secondItem).toContainText("Retained (exception)");
  await expect(secondItem).toContainText("Needed for an open support ticket");
  expect(state.reviewItems[1].status).toBe("retained_exception");
  await expect(requestRow).toContainText("Awaiting review");

  // Deciding the last pending item completes the request; the queue panel unmounts with it.
  await items.nth(0).getByRole("button", { name: "Redact" }).click();
  expect(state.reviewItems[0].status).toBe("redacted");
  await expect(requestRow).toContainText("Completed");
  await expect(page.locator(".privacy-review-queue")).toHaveCount(0);
  expect(state.privacyRequests[0].status).toBe("completed");
  expect(state.privacyRequests[0].manual_review_count).toBe(0);
});

test("Portuguese administration preserves permissions and operational mutations", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  state.member.locale = "pt-AO";
  state.member.timezone = "Africa/Luanda";
  await installMockAPI(page, state);
  await page.goto(
    `${tenantOrigin}/auth/verify#token=administration-portuguese-token`,
  );
  await expect(
    page.getByRole("heading", { name: "Boa tarde, Alex" }),
  ).toBeVisible();

  if (testInfo.project.name === "mobile")
    await page.getByTitle("Abrir navegação").click();
  await page
    .getByRole("navigation", { name: "Navegação principal" })
    .getByRole("link", { name: "Definições", exact: true })
    .click();
  await expect(page).toHaveURL(`${tenantOrigin}/settings`);
  await expect(page).toHaveTitle("Definições - Fidibeki");
  await expect(
    page.getByRole("heading", { name: "Identidade e predefinições públicas" }),
  ).toBeVisible();
  await expect(page.getByLabel("Nome público")).toHaveValue("Acme Studio");

  await page.getByRole("link", { name: "Chaves de API" }).click();
  await page.getByRole("button", { name: "Criar chave" }).click();
  const keyDialog = page.getByRole("dialog", { name: "Criar chave de API" });
  await expect(keyDialog).toContainText("Predefinição Leitor");
  await keyDialog.getByLabel("Nome").fill("Warehouse exporter");
  await keyDialog.getByRole("button", { name: "Criar chave" }).click();
  await expect(
    keyDialog.getByRole("heading", { name: "Chave de API criada" }),
  ).toBeVisible();
  expect(state.apiKeys.at(-1)).toMatchObject({
    name: "Warehouse exporter",
    permissions: [
      "project:read",
      "widget_rule:read",
      "feedback:read",
      "issue:read",
      "session:read",
      "member:read",
      "integration:read",
      "billing:read",
      "tenant:read",
    ],
    rate_limit_per_min: 1000,
  });
  await keyDialog.getByRole("button", { name: "Concluído" }).click();

  await page.getByRole("link", { name: "Dados e privacidade" }).click();
  await expect(
    page.getByRole("heading", { name: "Calendário de retenção" }),
  ).toBeVisible();
  await expect(page.locator(".privacy-retention-table")).toContainText(
    "90 dias",
  );
  await expect(page.locator(".privacy-retention-summary")).toContainText(
    "Decisão necessária",
  );
  await expect(page.locator(".privacy-retention-health")).toContainText(
    "Saúde da limpeza",
  );
  await expect(page.locator(".privacy-retention-health")).toContainText(
    "Magic links do workspace",
  );
  await expect(page.locator(".privacy-retention-health")).toContainText(
    "Saudável",
  );
  await expect(page.locator(".privacy-retention-health")).toContainText(
    "Magic links de administradores da plataforma",
  );
  await expect(page.locator(".privacy-retention-health")).toContainText(
    "Atenção",
  );
  await expect(page.locator(".privacy-retention-health")).toContainText(
    "Replays de sessão",
  );
  await expect(page.locator(".privacy-retention-health")).toContainText(
    "Ainda não observada",
  );
  await expect(page.locator(".privacy-retention-table")).toContainText(
    "Controlo externo",
  );
  await expect(page.locator(".privacy-retention-table")).not.toContainText(
    "Vida útil do workspace",
  );
  await page
    .locator(".privacy-retention-table .privacy-row", {
      hasText: "Conteúdo do fluxo de feedback e issues",
    })
    .getByTitle("Ver detalhes de retenção")
    .click();
  await expect(page.locator(".privacy-retention-detail")).toContainText(
    "Finalidade",
  );
  await expect(page.locator(".privacy-request-table")).toContainText(
    "former.customer@example.test",
  );
  const requestForm = page.locator(".privacy-request-form");
  await requestForm
    .getByLabel("Email do reporter")
    .fill("requester@example.test");
  await requestForm.getByLabel("Tipo de pedido").selectOption("export");
  await requestForm.getByRole("button", { name: "Enviar verificação" }).click();
  await expect(page.getByRole("status")).toContainText(
    "requester@example.test",
  );
  expect(state.privacyRequests[0]).toMatchObject({
    action: "export",
    reporter_email: "requester@example.test",
  });
  await capture(
    page,
    testInfo,
    `settings-portuguese-privacy-${testInfo.project.name}`,
  );

  await page.getByRole("link", { name: "Saúde das submissões" }).click();
  const health = page.locator(".submission-health");
  await expect(health).toContainText("Orçamento diário da IA esgotado");
  await health
    .getByLabel("Período da saúde das submissões")
    .selectOption("24h");
  await health
    .getByLabel("Projeto da saúde das submissões")
    .selectOption("project-1");
  await expect
    .poll(() => state.submissionHealthRequests.at(-1))
    .toContain("range=24h");
  await expect
    .poll(() => state.submissionHealthRequests.at(-1))
    .toContain("project_id=project-1");

  state.tenant.deletion_requested_at = now;
  state.billingDeletionRequestedAt = now;
  await page.getByRole("link", { name: "Workspace", exact: true }).click();
  await page.getByRole("link", { name: "Gerir faturação" }).click();
  await expect(
    page.getByRole("heading", { name: "Faturação", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Eliminação agendada", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Cancelar eliminação" }).click();
  await expect(
    page.getByText("Eliminação agendada", { exact: true }),
  ).toHaveCount(0);
  expect(state.billingDeletionRequestedAt).toBeNull();

  await page.context().addCookies([
    { name: "fidibeki_browser_locale", value: "pt-AO", url: tenantOrigin },
    {
      name: "fidibeki_browser_timezone",
      value: "Africa/Luanda",
      url: tenantOrigin,
    },
  ]);
  await page.addInitScript(() => {
    Object.defineProperty(navigator, "language", { get: () => "pt-AO" });
    Object.defineProperty(navigator, "languages", {
      get: () => ["pt-AO", "pt"],
    });
  });
  await page.goto(`${tenantOrigin}/privacy/privacy-token`);
  await expect(
    page.getByRole("heading", { name: "Pedido de exportação de dados" }),
  ).toBeVisible();
  await expect(page.getByText("14 registos correspondentes")).toBeVisible();
  await page
    .getByRole("button", { name: "Descarregar arquivo de dados" })
    .click();
  expect(state.privacyDownloadCount).toBe(1);
});

test("verified reporter can follow and download a completed data export", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await installMockAPI(page, state);
  await page.goto(`${tenantOrigin}/privacy/privacy-token`);

  await expect(
    page.getByRole("heading", { name: "Data export request" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Your data archive is ready" }),
  ).toBeVisible();
  await expect(page.getByText("14 matching records")).toBeVisible();
  expect(state.privacyVerifyCount).toBe(1);
  await capture(
    page,
    testInfo,
    `reporter-data-export-${testInfo.project.name}`,
  );

  await page.getByRole("button", { name: "Download data archive" }).click();
  expect(state.privacyDownloadCount).toBe(1);
});

test("reporter data request keeps failures and invalid links neutral", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "State semantics are viewport independent",
  );
  const failed = createMockState();
  failed.publicPrivacyRequest.status = "failed";
  failed.publicPrivacyRequest.job_status = "failed";
  failed.publicPrivacyRequest.download_available = false;
  await installMockAPI(page, failed);
  await page.goto(`${tenantOrigin}/privacy/failed-token`);

  await expect(
    page.getByRole("heading", { name: "The request needs attention" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Download data archive" }),
  ).toHaveCount(0);

  const awaitingReview = createMockState();
  awaitingReview.publicPrivacyRequest.action = "anonymize";
  awaitingReview.publicPrivacyRequest.status = "awaiting_manual_review";
  awaitingReview.publicPrivacyRequest.job_status = "awaiting_manual_review";
  awaitingReview.publicPrivacyRequest.completed_at = undefined;
  awaitingReview.publicPrivacyRequest.manual_review_count = 2;
  awaitingReview.publicPrivacyRequest.download_available = false;
  await page.unrouteAll({ behavior: "wait" });
  await installMockAPI(page, awaitingReview);
  await page.goto(`${tenantOrigin}/privacy/awaiting-review-token`);

  await expect(
    page.getByRole("heading", { name: "Your request is processing" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Anonymization completed" }),
  ).toHaveCount(0);
  await expect(page.locator("body")).not.toContainText("Data request expired");

  const unavailable = createMockState();
  unavailable.privacyPublicUnavailable = true;
  await page.unrouteAll({ behavior: "wait" });
  await installMockAPI(page, unavailable);
  await page.goto(`${tenantOrigin}/privacy/invalid-token`);
  await expect(
    page.getByRole("heading", { name: "Data request unavailable" }),
  ).toBeVisible();
  await expect(page.locator("body")).not.toContainText(
    "data request is invalid or unavailable",
  );
});

test("member and API key permissions share a compact custom-aware editor", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Members" }).click();
  await page.getByRole("button", { name: "Invite member" }).click();

  const memberDialog = page.getByRole("dialog", { name: "Invite member" });
  const memberEditor = memberDialog.locator(".permission-editor");
  await expect(memberEditor).toBeVisible();
  await expect(memberEditor.locator("fieldset")).toHaveCount(11);
  await memberEditor.getByRole("button", { name: "Admin" }).click();
  await expect(
    memberEditor.getByRole("button", { name: "Admin" }),
  ).toHaveAttribute("data-active", "true");
  await memberEditor
    .getByRole("checkbox", { name: "Delete project" })
    .uncheck();
  await expect(memberEditor).toContainText("Custom selection");
  await expect(memberEditor.locator('[data-active="true"]')).toHaveCount(0);
  await expectNoPageOverflow(page);
  await capture(
    page,
    testInfo,
    `settings-permissions-${testInfo.project.name}`,
  );

  await memberDialog.getByRole("button", { name: "Cancel" }).click();
  await page.getByRole("link", { name: "API keys" }).click();
  await page.getByRole("button", { name: "Create key" }).click();
  const keyDialog = page.getByRole("dialog", { name: "Create API key" });
  const keyEditor = keyDialog.locator(".permission-editor");
  await expect(keyEditor).toBeVisible();
  await expect(keyEditor.locator("fieldset")).toHaveCount(11);
  await expect(
    keyEditor.getByRole("button", { name: "Viewer" }),
  ).toHaveAttribute("data-active", "true");
  await expectNoPageOverflow(page);
});

test("settings identifies and transfers workspace ownership", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  state.members.push({
    id: "member-unverified",
    tenant_id: state.tenant.id,
    email: "pending@acme.test",
    name: "Pending Member",
    avatar_version: null,
    locale: null,
    timezone: null,
    permissions: ["project:read"],
    status: "active",
    role: "member",
    verified: false,
  });
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Members" }).click();

  const currentOwner = page
    .locator(".settings-row")
    .filter({ hasText: "alex@acme.test" });
  const ownerBadge = currentOwner.getByText("Owner", { exact: true });
  await expect(ownerBadge).toBeVisible();
  const ownerGeometry = await ownerBadge.evaluate((element) => {
    const box = element.getBoundingClientRect();
    const range = document.createRange();
    range.selectNodeContents(element);
    const text = range.getBoundingClientRect();
    return {
      horizontalOffset: Math.abs(
        box.left + box.width / 2 - (text.left + text.width / 2),
      ),
      verticalOffset: Math.abs(
        box.top + box.height / 2 - (text.top + text.height / 2),
      ),
      lineHeight: getComputedStyle(element).lineHeight,
    };
  });
  expect(ownerGeometry.horizontalOffset).toBeLessThanOrEqual(1);
  expect(ownerGeometry.verticalOffset).toBeLessThanOrEqual(1);
  expect(ownerGeometry.lineHeight).not.toBe("normal");
  await currentOwner.getByTitle("Transfer ownership").click();

  const dialog = page.getByRole("dialog", { name: "Transfer ownership" });
  await expect(dialog.getByLabel("New owner")).toBeFocused();
  await expect(
    dialog.getByRole("option", { name: /Pending Member/ }),
  ).toHaveCount(0);
  await dialog.getByLabel("New owner").selectOption("member-2");
  await dialog.getByLabel("Type acme to confirm").fill("acme");
  await expectNoPageOverflow(page);
  await capture(
    page,
    testInfo,
    `settings-transfer-ownership-${testInfo.project.name}`,
  );
  await dialog.getByRole("button", { name: "Transfer ownership" }).click();

  await expect(dialog).not.toBeVisible();
  const newOwner = page
    .locator(".settings-row")
    .filter({ hasText: "jamie@acme.test" });
  await expect(newOwner.getByText("Owner", { exact: true })).toBeVisible();
  await expect(page.getByTitle("Transfer ownership")).toHaveCount(0);
  expect(state.members.find((member) => member.id === "member-2")?.role).toBe(
    "owner",
  );
});

test("settings manages non-owner member lifecycle", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Members" }).click();

  let memberRow = page
    .locator(".settings-row")
    .filter({ hasText: "jamie@acme.test" });
  await memberRow.getByTitle("Deactivate member").click();
  let dialog = page.getByRole("dialog", { name: "Deactivate member" });
  await expect(dialog).toContainText(
    "every active session will end immediately",
  );
  await dialog.getByRole("button", { name: "Deactivate member" }).click();

  memberRow = page
    .locator(".settings-row")
    .filter({ hasText: "jamie@acme.test" });
  await expect(
    memberRow.getByText("Deactivated", { exact: true }),
  ).toBeVisible();
  expect(state.members[1].permissions).toEqual([]);
  await memberRow.getByTitle("Reactivate member").click();
  dialog = page.getByRole("dialog", { name: "Reactivate member" });
  await dialog.getByRole("button", { name: "Reactivate member" }).click();

  memberRow = page
    .locator(".settings-row")
    .filter({ hasText: "jamie@acme.test" });
  await expect(memberRow.getByText("Active", { exact: true })).toBeVisible();
  await memberRow.getByTitle("Remove member").click();
  dialog = page.getByRole("dialog", { name: "Remove member" });
  await expect(dialog).toContainText("Removal is permanent");
  await capture(
    page,
    testInfo,
    `settings-remove-member-${testInfo.project.name}`,
  );
  await dialog.getByRole("button", { name: "Remove member" }).click();

  memberRow = page
    .locator(".settings-row")
    .filter({ hasText: "jamie@acme.test" });
  await expect(memberRow.getByText("Removed", { exact: true })).toBeVisible();
  await expect(memberRow.locator("button")).toHaveCount(0);
});

test("member invitation keeps email errors beside the field and general errors separate", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  state.memberInviteFailure = {
    status: 422,
    code: "validation_error",
    message: "email domain is not verified for this workspace",
  };
  await authenticate(page, state);
  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Members" }).click();
  await page.getByRole("button", { name: "Invite member" }).click();

  const dialog = page.getByRole("dialog", { name: "Invite member" });
  const email = dialog.getByLabel("Email");
  await email.fill("blocked@other.test");
  await dialog.getByRole("button", { name: "Send invitation" }).click();

  const emailError = dialog.locator("#member-email-error");
  await expect(emailError).toHaveText(
    "Enter an email from a verified workspace domain.",
  );
  await expect(email).toBeFocused();
  await expect(email).toHaveAttribute("aria-invalid", "true");
  await expect(email).toHaveAttribute("aria-describedby", "member-email-error");
  const placement = await dialog
    .locator(".member-email-field")
    .evaluate((field) => {
      const error = field
        .querySelector("#member-email-error")!
        .getBoundingClientRect();
      const permissions = field
        .parentElement!.querySelector(".permission-editor")!
        .getBoundingClientRect();
      return { adjacent: error.bottom <= permissions.top };
    });
  expect(placement.adjacent).toBe(true);
  await capture(
    page,
    testInfo,
    `settings-invite-email-error-${testInfo.project.name}`,
  );

  state.memberInviteFailure = {
    status: 503,
    code: "service_unavailable",
    message: "service unavailable",
  };
  await email.fill("member@acme.test");
  await dialog.getByRole("button", { name: "Send invitation" }).click();
  await expect(emailError).toHaveCount(0);
  await expect(email).not.toHaveAttribute("aria-invalid", "true");
  await expect(dialog.locator('.settings-form-error[role="alert"]')).toHaveText(
    "This service is temporarily unavailable.",
  );
});

test("workspace and project reporter access policies remain explicit", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Policy persistence is viewport independent",
  );
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Settings");

  const workspacePolicy = page.getByLabel("Reporter progress access");
  await expect(workspacePolicy).toBeChecked();
  await workspacePolicy.uncheck();
  expect(state.tenant.reporter_access_enabled).toBe(false);

  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();
  const projectPolicy = page.getByLabel("Reporter progress access");
  await expect(projectPolicy).toHaveValue("inherit");
  await projectPolicy.selectOption("disabled");
  await page.getByRole("button", { name: "Save project" }).click();

  expect(state.projects[0].reporter_access_enabled).toBe(false);
  await expect(page.getByRole("status")).toHaveText("Project settings saved.");
});

test("settings verifies additional member email domains before invitations", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  await authenticate(page, state);
  await openPrimary(page, "Settings");

  await page.getByRole("link", { name: "Members" }).click();
  await expect(
    page.getByRole("heading", { name: "Allowed email domains" }),
  ).toBeVisible();
  await expect(page.getByText("acme.test", { exact: true })).toBeVisible();
  await expect(page.getByText("Enforced", { exact: true })).toBeVisible();

  await page.getByLabel("Verification email").fill("security@other.test");
  await page.getByRole("button", { name: "Verify domain" }).click();

  await expect(page.getByRole("status")).toContainText(
    "Verification sent to security@other.test",
  );
  await expect(page.getByText("other.test", { exact: true })).toBeVisible();
  await expect(page.getByText("Pending", { exact: true })).toBeVisible();
  await capture(page, testInfo, "settings-member-domains");
});

test("workspace deletion supports the full recovery path", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Covered once as an interaction workflow",
  );
  const state = createMockState();
  await authenticate(page, state);
  await page.locator(".app-account-menu summary").click();
  await page.getByRole("link", { name: "Billing", exact: true }).click();
  await page.getByRole("button", { name: "Schedule deletion" }).click();
  const dialog = page.getByRole("dialog", { name: "Schedule deletion" });
  await expect(dialog.getByLabel(/Type acme to confirm/)).toBeFocused();
  await dialog.getByLabel(/Type acme to confirm/).fill("acme");
  await dialog.getByRole("button", { name: "Schedule deletion" }).click();
  await expect(
    page.getByText("Deletion scheduled", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("This workspace is pending deletion"),
  ).toBeVisible();

  await page.getByRole("button", { name: "Cancel deletion" }).click();
  await expect(
    page.getByText("Deletion scheduled", { exact: true }),
  ).toHaveCount(0);
  expect(state.billingDeletionRequestedAt).toBeNull();
});

test("pending deletion keeps feedback readable and cancellation available", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Workspace lifecycle behavior is viewport independent",
  );
  const state = createMockState();
  state.tenant.deletion_requested_at = now;
  state.billingDeletionRequestedAt = now;
  const mutationRequests: string[] = [];
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (
      (request.method() === "PATCH" && /^\/v1\/feedbacks\/[^/]+$/.test(path)) ||
      (request.method() === "POST" && /\/reprocess$/.test(path))
    ) {
      mutationRequests.push(`${request.method()} ${path}`);
    }
  });
  await authenticate(page, state);

  await openPrimary(page, "Feedback");
  await expect(page.getByLabel(/Select FB-/)).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Archive", exact: true }),
  ).toHaveCount(0);
  await page.getByTitle("Open full detail").click();
  await expect(
    page.getByRole("heading", {
      name: "Checkout froze after I applied a promotion code.",
    }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Reprocess all" })).toHaveCount(
    0,
  );
  await expect(page.getByRole("button", { name: "Spam" })).toHaveCount(0);
  expect(mutationRequests).toEqual([]);

  await page.getByRole("link", { name: "Review deletion" }).click();
  await expect(
    page.getByRole("button", { name: "Billing portal" }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Cancel deletion" }).click();
  await expect(
    page.getByText("This workspace is pending deletion"),
  ).toHaveCount(0);
  expect(state.billingDeletionRequestedAt).toBeNull();
});

test("manage-only member can reach billing and cancel pending deletion", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Permission routing is viewport independent",
  );
  const state = createMockState();
  state.member.permissions = ["billing:manage"];
  state.tenant.deletion_requested_at = now;
  state.billingDeletionRequestedAt = now;
  await installMockAPI(page, state);

  await page.goto(`${tenantOrigin}/auth/verify#token=test-token`);

  await expect(page).toHaveURL(`${tenantOrigin}/billing`);
  await expect(
    page.getByRole("heading", { name: "Billing", exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Cancel deletion" }).click();
  await expect(
    page.getByText("This workspace is pending deletion"),
  ).toHaveCount(0);
  expect(state.billingDeletionRequestedAt).toBeNull();
});

test("pending deletion disables mutations across workspace administration", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Workspace lifecycle behavior is viewport independent",
  );
  const state = createMockState();
  state.tenant.deletion_requested_at = now;
  state.billingDeletionRequestedAt = now;
  state.apiKeys.push({
    id: "key-1",
    tenant_id: state.tenant.id,
    name: "Reporting",
    token_prefix: "fk_live_report",
    permissions: ["feedback:read"],
    rate_limit_per_min: 100,
    expires_at: null,
    last_used_at: null,
    created_at: now,
    updated_at: now,
  });
  state.integrations.push({
    id: "integration-1",
    kind: "webhook",
    name: "Operations",
    webhook_url: "https://hooks.acme.test/fidibeki",
    channel: null,
    webhook_secret_fingerprint: "sha256:demo",
    custom_header_names: [],
    events: ["feedback.created"],
    is_active: true,
    created_at: now,
    updated_at: now,
  });
  await authenticate(page, state);

  await openPrimary(page, "Issues");
  await expect(page.getByLabel("Status for ISS-101")).toBeDisabled();
  await page.getByRole("link", { name: /Open ISS-101:/ }).click();
  await expect(
    page.getByRole("button", { name: "Update issue" }),
  ).toBeDisabled();
  await expect(page.locator(".issue-update-panel textarea")).toBeDisabled();
  await expect(page.locator(".issue-merge select")).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "Merge", exact: true }),
  ).toBeDisabled();

  await openPrimary(page, "Projects");
  await expect(page.getByRole("button", { name: "New project" })).toHaveCount(
    0,
  );
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();
  await expect(page.getByTitle("New widget rule")).toHaveCount(0);
  await expect(page.getByTitle("Delete rule")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Save rule" })).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "Save project" }),
  ).toBeDisabled();
  await expect(
    page.getByRole("button", { name: "Delete project" }),
  ).toBeDisabled();
  await expect(page.getByLabel("Name", { exact: true })).toBeDisabled();
  await expect(page.getByLabel("Project name")).toBeDisabled();

  await openPrimary(page, "Settings");
  await page.getByRole("link", { name: "Members" }).click();
  await expect(page.getByRole("button", { name: "Invite member" })).toHaveCount(
    0,
  );
  await expect(page.getByTitle("Edit permissions")).toHaveCount(0);
  await page.getByRole("link", { name: "API keys" }).click();
  await expect(page.getByRole("button", { name: "Create key" })).toHaveCount(0);
  await expect(page.getByTitle("Revoke API key")).toHaveCount(0);
  await page.getByRole("link", { name: "Integrations" }).click();
  await expect(
    page.getByRole("button", { name: "Add integration" }),
  ).toHaveCount(0);
  await expect(page.getByTitle("Remove integration")).toHaveCount(0);
});

test("pending deletion disables first-project activation", async ({
  page,
}, testInfo) => {
  test.skip(
    testInfo.project.name !== "desktop",
    "Workspace lifecycle behavior is viewport independent",
  );
  const state = createMockState();
  state.projects.splice(0);
  state.widgetRules.splice(0);
  state.tenant.deletion_requested_at = now;
  state.billingDeletionRequestedAt = now;
  await installMockAPI(page, state);

  await page.goto(`${tenantOrigin}/auth/verify#token=test-token`);
  await expect(page).toHaveURL(`${tenantOrigin}/getting-started`);
  await expect(
    page.getByRole("heading", { name: "Connect your first project" }),
  ).toBeVisible();
  const form = page.locator(".activation-surface form");
  await expect(form.getByLabel("Project name")).toBeDisabled();
  await expect(form.locator('button[type="submit"]')).toBeDisabled();
});

test("Dispatch production surfaces remain responsive and error-free", async ({
  page,
}, testInfo) => {
  const state = createMockState();
  const browserErrors: string[] = [];
  page.on("pageerror", (error) => browserErrors.push(error.message));
  page.on("console", (message) => {
    const text = message.text();
    const expectedSandboxBlock =
      text.startsWith("Blocked script execution in ") &&
      text.includes(
        "frame is sandboxed and the 'allow-scripts' permission is not set",
      );
    if (message.type() === "error" && !expectedSandboxBlock)
      browserErrors.push(text);
  });
  await authenticate(page, state);
  if (testInfo.project.name === "desktop") {
    await expect(page.getByTitle("Workspace settings")).toHaveAttribute(
      "href",
      "/settings",
    );
    await expect(page.getByTitle("Notifications")).toHaveCount(0);
  }
  if (testInfo.project.name === "mobile") {
    const primaryNavigation = page.getByRole("navigation", {
      name: "Primary navigation",
    });
    const openNavigation = page.getByTitle("Open navigation");
    await expect(primaryNavigation).not.toBeVisible();
    await openNavigation.click();
    await expect(primaryNavigation).toBeVisible();
    await expect(page.getByTitle("Close navigation")).toBeFocused();
    await expect(page.locator(".app-main")).toHaveAttribute("inert", "");
    await page.keyboard.press("Escape");
    await expect(primaryNavigation).not.toBeVisible();
    await expect(openNavigation).toBeFocused();
  }
  await capture(page, testInfo, "dispatch-overview");

  await openPrimary(page, "Feedback");
  await expect(page.locator(".feedback-stream article").first()).toContainText(
    "FB-1048",
  );
  await capture(page, testInfo, "dispatch-feedback");
  if (testInfo.project.name === "mobile") {
    await page.getByRole("link", { name: "Open FB-1048" }).click();
  } else {
    await page.getByTitle("Open full detail").click();
  }
  await expect(page).toHaveURL(/\/feedback\/feedback-1$/);
  await expect(
    page
      .locator(".feedback-detail-page")
      .getByRole("heading", {
        name: "Checkout froze after I applied a promotion code.",
      }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Replay" }).click();
  await expect(page.getByText("3 events", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Play or pause replay" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Toggle fullscreen replay" }),
  ).toBeVisible();
  await expect(
    page.getByRole("checkbox", { name: "Skip inactive periods" }),
  ).toBeVisible();
  await capture(page, testInfo, "dispatch-feedback-replay");

  await page.getByRole("link", { name: "Back to queue" }).click();
  await expect(page).toHaveURL(/\/feedback(?:\?|$)/);
  await expect(
    page.getByRole("heading", { name: "Feedback", exact: true }),
  ).toBeVisible();
  await openPrimary(page, "Projects");
  await page
    .locator(".project-list")
    .getByRole("link", { name: /Storefront/ })
    .click();
  await expect(page).toHaveURL(/\/projects\/project-1$/);
  await expect(
    page.getByRole("heading", { name: "Checkout pulse", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Delete rule" })).toBeVisible();
  await capture(page, testInfo, "dispatch-project");

  await openPrimary(page, "Settings");
  await expect(page.getByRole("heading", { name: "Settings" })).toBeVisible();
  await capture(page, testInfo, "dispatch-settings");

  await page.locator(".app-account-menu summary").click();
  await page.getByRole("link", { name: "Billing", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Billing", exact: true }),
  ).toBeVisible();
  if (testInfo.project.name === "mobile") {
    const rowAlignment = await page
      .locator(".billing-plan")
      .first()
      .locator("dl > div")
      .first()
      .evaluate((row) => {
        const value = row.querySelector("dd");
        const rowBox = row.getBoundingClientRect();
        const valueBox = value?.getBoundingClientRect();
        return {
          rowLeft: rowBox.left,
          rowWidth: rowBox.width,
          valueLeft: valueBox?.left || 0,
        };
      });
    expect(rowAlignment.valueLeft).toBeGreaterThan(
      rowAlignment.rowLeft + rowAlignment.rowWidth * 0.7,
    );
  }
  await capture(page, testInfo, "dispatch-billing");
  expect(browserErrors).toEqual([]);
});
