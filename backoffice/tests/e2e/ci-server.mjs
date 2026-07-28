process.env.HOST = '127.0.0.1'
process.env.PORT = process.env.PLAYWRIGHT_PORT || '4174'
process.env.BACKOFFICE_API_UPSTREAM = 'http://127.0.0.1:65535'
process.env.BACKOFFICE_INTERNAL_PROXY_SECRET = 'e2e-internal-proxy-secret'
process.env.BACKOFFICE_BASE_DOMAIN = 'localhost'

await import('../../.output/server/index.mjs')
