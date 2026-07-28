import { createServer } from 'node:http'

const port = 43136
const evidence = {
  session: 0,
  feedback: 0,
  uploadedBytes: 0,
  completed: 0,
  lastFeedback: null,
}
const png = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQI12P4z8DwHwAFAAH/iZk9HQAAAABJRU5ErkJggg==',
  'base64',
)

function json(response, status, body) {
  response.writeHead(status, { 'content-type': 'application/json' })
  response.end(JSON.stringify(body))
}

async function body(request) {
  const chunks = []
  for await (const chunk of request) chunks.push(chunk)
  return Buffer.concat(chunks)
}

const definition = {
  id: '33333333-3333-4333-8333-333333333333',
  definition_key: 'mobile-example',
  definition_version: 1,
  presentation: 'launcher',
  launcher_variant: 'floating_button',
  objective: 'continuous_feedback',
  response_blocks: [
    { key: 'rating', type: 'rating', required: true },
    {
      key: 'reaction',
      type: 'reaction',
      required: false,
      options: [{ value: 'happy' }, { value: 'sad' }],
    },
    {
      key: 'journey',
      type: 'single_choice',
      required: true,
      options: [{ value: 'easy' }, { value: 'blocked' }],
    },
    { key: 'feedback', type: 'free_text', required: true },
  ],
  localization: {
    launcher_label: 'Send feedback',
    prompt: 'How did checkout go?',
    comment_placeholder: 'Anything else?',
    contact_label: 'Email for updates',
    consent_label: 'I agree to receive updates',
    submit_label: 'Send feedback',
    success_message: 'Runtime smoke completed.',
    block_labels: {
      rating: 'Rating',
      reaction: 'Reaction',
      journey: 'Checkout journey',
      feedback: 'Feedback',
    },
    option_labels: {
      reaction: { happy: 'Happy', sad: 'Sad' },
      journey: { easy: 'Easy', blocked: 'Blocked' },
    },
  },
  allow_comment: true,
  collect_contact: false,
  can_attach_screenshot: true,
  recurrence_mode: 'never',
  recurrence_days: null,
  ai_processing: { enabled: false },
  appearance: {},
}

createServer(async (request, response) => {
  const url = new URL(request.url ?? '/', `http://${request.headers.host}`)
  if (request.method === 'POST' && url.pathname === '/session') {
    evidence.session += 1
    await body(request)
    return json(response, 200, {
      token: 'fmc_runtime_smoke_capture_token_123456789',
      submit_expires_at: '2099-07-28T22:00:00Z',
      configuration: {
        schema_version: 1,
        project_id: '44444444-4444-4444-8444-444444444444',
        display_locale: 'en',
        definition,
      },
    })
  }
  if (request.method === 'GET' && url.pathname === '/fixture.png') {
    response.writeHead(200, {
      'content-type': 'image/png',
      'content-length': String(png.length),
    })
    return response.end(png)
  }
  if (request.method === 'POST' && url.pathname === '/v1/mobile/feedback') {
    evidence.feedback += 1
    const parsed = JSON.parse((await body(request)).toString('utf8'))
    evidence.lastFeedback = parsed
    const allowed = new Set([
      'client_feedback_id',
      'responses',
      'comment',
      'reporter_email',
      'notify_reporter',
      'consent',
      'screenshot',
      'app_version',
      'os_version',
      'device_locale',
    ])
    if (Object.keys(parsed).some((key) => !allowed.has(key))) {
      return json(response, 422, {
        error: {
          code: 'validation_failed',
          message: 'unknown field',
          details: [{ field: 'request', message: 'contains an unknown field' }],
        },
      })
    }
    return json(response, 200, {
      feedback_id: '55555555-5555-4555-8555-555555555555',
      feedback_public_id: 'FDBK-SMOKE',
      attachments: parsed.screenshot
        ? [
            {
              attachment_id: '66666666-6666-4666-8666-666666666666',
              client_attachment_id: parsed.screenshot.client_attachment_id,
              presigned_url: `http://${request.headers.host}/upload`,
              method: 'PUT',
              headers: { 'x-smoke-upload': 'true' },
              complete_token: 'runtime-complete-token',
              expires_at: '2099-07-28T22:10:00Z',
            },
          ]
        : [],
    })
  }
  if (request.method === 'PUT' && url.pathname === '/upload') {
    evidence.uploadedBytes = (await body(request)).length
    response.writeHead(200)
    return response.end()
  }
  if (
    request.method === 'POST' &&
    url.pathname ===
      '/v1/mobile/attachments/66666666-6666-4666-8666-666666666666/complete'
  ) {
    const parsed = JSON.parse((await body(request)).toString('utf8'))
    if (parsed.complete_token !== 'runtime-complete-token') {
      return json(response, 403, {
        error: { code: 'access_denied', message: 'denied' },
      })
    }
    evidence.completed += 1
    return json(response, 200, { status: 'accepted' })
  }
  if (request.method === 'GET' && url.pathname === '/evidence') {
    return json(response, 200, evidence)
  }
  return json(response, 404, { error: { code: 'not_found', message: 'not found' } })
}).listen(port, '0.0.0.0', () => {
  process.stdout.write(`Fidibeki mobile smoke server listening on ${port}\n`)
})
