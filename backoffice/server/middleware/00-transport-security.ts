import { setResponseHeader } from 'h3'

const hstsPolicy = 'max-age=31536000'

export default defineEventHandler((event) => {
  setResponseHeader(event, 'Strict-Transport-Security', hstsPolicy)
})
