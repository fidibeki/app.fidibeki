import type { DetailSection } from './types'
import type { photography } from './photography'

export interface Guide { slug: string; title: string; description: string; photo: keyof typeof photography; published: string; sections: DetailSection[]; related: {label: string; to: string}[] }

export const guides: Guide[] = [
  {
    "slug": "choose-website-feedback-tool",
    "title": "How to choose a website feedback tool for a small SaaS team",
    "description": "Compare website feedback tools by the work you need to do: capture context, talk to visitors, track issues, and reply. Includes a trial checklist.",
    "photo": "notebook",
    "published": "2026-10-02",
    "sections": [
      {
        "heading": "Start with the report you cannot act on",
        "body": "A website feedback tool collects reports where people use your product. The buying decision is what happens after collection. Take a recent report that stalled, such as “the button disappeared on my phone”. Was the missing piece a screenshot, a conversation with the visitor, or someone owning the fix? Use that gap to define your trial, before comparing feature lists."
      },
      {
        "heading": "Choose the workflow before the tool",
        "body": "Website bug reporting, live support, client design review, and feature voting are different jobs. A team reviewing a new site with clients may need precise visual annotations. A product team prioritising requests may need a voting board. A small SaaS team supporting people inside a live product may need private reports and conversations connected to issues. Write down your main job and the capabilities you cannot give up."
      },
      {
        "heading": "Run one complete trial journey",
        "body": "Install the widget on a test page and ask a teammate to act as the visitor on a phone. Use a harmless test report, not customer data. Follow it through the operator view and record where you need another tool or have to copy information manually. A useful trial ends with the visitor receiving an answer, not just a report appearing in an inbox.",
        "bullets": [
          "Can the visitor submit on mobile without covering the navigation or losing their draft?",
          "Can they choose which screenshot or recording to share, and can you explain the retention period?",
          "Can your team link two reports about the same problem without creating duplicate work?",
          "Can you return to the source conversation, and are internal notes kept out of the visitor reply?"
        ]
      },
      {
        "heading": "Check the limits your team will actually reach",
        "body": "Compare the total for your projects, teammates, monthly reports and required attachments. Check whether replay, integrations and retention depend on the plan. Separate a permanent free plan from a timed trial, and monthly billing from an annual price divided by twelve. Keep these numbers beside your trial notes so a low entry price does not decide the whole purchase."
      },
      {
        "heading": "Where Fidibeki fits",
        "body": "Fidibeki combines website feedback and visitor conversations with issue tracking. Your team can start a chat, choose whether visitors may start one too, and link a visitor message to an issue. You return to the chat to send a personal reply while that visitor session is valid. Issue notes are not sent automatically. If public voting, a public roadmap or specialist visual annotation is essential, include tools built for that workflow in your shortlist. The comparison pages explain the trade-offs using official vendor sources."
      }
    ],
    "related": [
      {
        "label": "Website feedback widget",
        "to": "/features/feedback-widget"
      },
      {
        "label": "Live chat for SaaS teams",
        "to": "/features/website-conversations"
      },
      {
        "label": "Compare",
        "to": "/compare"
      },
      {
        "label": "Pricing",
        "to": "/pricing"
      }
    ]
  },
  {
    "slug": "website-bug-report-template",
    "title": "How to collect a reproducible website bug report",
    "description": "Turn “it does not work” into a report your team can investigate. A practical website bug report template, a mobile example, and a follow-up checklist.",
    "photo": "laptop",
    "published": "2026-10-02",
    "sections": [
      {
        "heading": "Ask what they were trying to do",
        "body": "Start with the visitor's goal. “What were you trying to do just before this happened?” is easier to answer than “describe the bug”. Then ask what they expected and what they saw instead. Keep the first request short. If the visitor is still on the page, a brief conversation can fill a missing step before they leave."
      },
      {
        "heading": "A bug report template to reuse",
        "body": "Use these prompts in a form, an issue summary, or a conversation. Ask only for what is missing. The visitor should not have to reproduce a payment, delete data, or expose private information to help you investigate.",
        "bullets": [
          "Goal: what were you trying to do?",
          "Steps: which page and actions led to the problem?",
          "Expected and actual result: what should have happened, and what happened instead?",
          "Environment: which browser and device, and does it happen again?",
          "Evidence: an optional screenshot or recording with sensitive content removed."
        ]
      },
      {
        "heading": "A worked mobile example",
        "body": "Example report: “I wanted to save my filters. On the results page, I opened Filters, selected a category and rotated my phone to landscape. I expected to see Apply. The button was below the panel and I could not scroll to it. It happens again in Safari on my iPhone.” This gives the team a specific action sequence to test. It is an illustrative report, not a customer testimonial or a claim about a particular browser defect."
      },
      {
        "heading": "Use recordings to answer a specific question",
        "body": "A screenshot shows a state; a recording can show how someone reached it. Request one only when it helps with the missing context. Explain what will be captured and let the person choose what to share. In Fidibeki, attachment tools depend on the enabled rule and plan. Session replay also depends on consent, sampling and browser support. Do not assume every report includes a recording."
      },
      {
        "heading": "Keep the source, then close the conversation",
        "body": "Confirm whether your team can reproduce the problem before promising a fix. Link related reports to one issue and write a summary of what you have confirmed. In Fidibeki, a visitor message can also link to that issue without copying the whole chat. After checking the fix on the affected device, return to the conversation to reply while the visitor session is valid. Opted-in feedback reporters can receive a separate email update. Internal issue notes are not a visitor reply."
      }
    ],
    "related": [
      {
        "label": "Bug reports",
        "to": "/use-cases/bug-reports"
      },
      {
        "label": "Session replay",
        "to": "/features/session-replay"
      },
      {
        "label": "Issue tracking",
        "to": "/features/issue-tracking"
      },
      {
        "label": "Reporter updates",
        "to": "/features/reporter-loop"
      }
    ]
  }
]

export const guideSlugs = guides.map(guide => guide.slug)
