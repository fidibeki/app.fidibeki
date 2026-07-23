import { describe, expect, it } from 'vitest'
import { apiFieldErrors } from '../../utils/api-fetch'

describe('API field errors', () => {
  it('reads direct appearance validation maps', () => {
    expect(apiFieldErrors({
      data: {
        error: {
          code: 'validation_error',
          details: {
            logo_url: 'must be an HTTPS URL without credentials or fragments',
          },
        },
      },
    })).toEqual({
      logo_url: 'must be an HTTPS URL without credentials or fragments',
    })
  })

  it('unwraps the widget-rule details.fields envelope', () => {
    expect(apiFieldErrors({
      response: {
        _data: {
          error: {
            code: 'validation_error',
            details: {
              fields: {
                'localizations.pt.block_labels.feedback':
                  'must identify a block and contain 1-240 characters',
                recurrence_days: 'must be between 1 and 3650 for cooldown recurrence',
              },
            },
          },
        },
      },
    })).toEqual({
      'localizations.pt.block_labels.feedback':
        'must identify a block and contain 1-240 characters',
      recurrence_days: 'must be between 1 and 3650 for cooldown recurrence',
    })
  })

  it('supports field/message arrays and ignores malformed details', () => {
    expect(apiFieldErrors({
      data: {
        error: {
          code: 'validation_error',
          details: [
            { field: 'name', message: 'must be present' },
            { field: '', message: 'ignored' },
            { field: 'theme', message: 42 },
          ],
        },
      },
    })).toEqual({ name: 'must be present' })
    expect(apiFieldErrors({ data: { error: { details: 'private failure' } } })).toEqual({})
  })

  it('keeps localized fallbacks for stable field reasons without replacing messages', () => {
    const error = {
      data: {
        error: {
          details: [
            { field: 'name', reason: 'required' },
            { field: 'theme', message: 'must be light, dark, or auto' },
          ],
        },
      },
    }
    expect(apiFieldErrors(error, {
      name: 'This field is required.',
      theme: 'Check this field.',
    })).toEqual({
      name: 'This field is required.',
      theme: 'must be light, dark, or auto',
    })
  })
})
