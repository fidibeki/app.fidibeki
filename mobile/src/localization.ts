export interface SystemMessages {
  close: string
  dismiss: string
  optional: string
  required: string
  addMoreContext: string
  hideMoreContext: string
  getUpdates: string
  sending: string
  addingScreenshot: string
  addScreenshot: string
  removeScreenshot: string
  screenshotAdded: string
  screenshotCancelled: string
  screenshotInvalid: string
  screenshotProviderFailed: string
  emailPlaceholder: string
  consentRequired: string
  invalidEmail: string
  requiredResponse: string
  invalidResponse: string
  submitFailed: string
  attachmentFailed: string
  configurationChanged: string
  privacyPolicy: string
  aiProcessingDisclosure: string
  rating: string
  feedback: string
  reaction: string
  ratingValue: (value: number) => string
}

const english: SystemMessages = {
  close: 'Close',
  dismiss: 'Dismiss',
  optional: 'Optional',
  required: 'Required',
  addMoreContext: 'Add more context',
  hideMoreContext: 'Hide more context',
  getUpdates: 'Get updates',
  sending: 'Sending...',
  addingScreenshot: 'Adding screenshot...',
  addScreenshot: 'Add screenshot',
  removeScreenshot: 'Remove screenshot',
  screenshotAdded: 'Screenshot added',
  screenshotCancelled: 'Screenshot selection cancelled',
  screenshotInvalid: 'That screenshot cannot be attached',
  screenshotProviderFailed: 'The app could not provide a screenshot',
  emailPlaceholder: 'Email address',
  consentRequired: 'Confirm consent to receive feedback updates',
  invalidEmail: 'Enter a valid email address',
  requiredResponse: 'This response is required',
  invalidResponse: 'Select a valid response',
  submitFailed: 'Feedback could not be sent. Try again.',
  attachmentFailed: 'Feedback was sent, but the screenshot could not be uploaded.',
  configurationChanged: 'The feedback form changed. Review your answers and submit again.',
  privacyPolicy: 'Privacy policy',
  aiProcessingDisclosure:
    'Submitted text and screenshots may be processed to organize this feedback.',
  rating: 'Rating',
  feedback: 'Feedback',
  reaction: 'Reaction',
  ratingValue: (value) => `${value} out of 5`,
}

const portuguese: SystemMessages = {
  close: 'Fechar',
  dismiss: 'Dispensar',
  optional: 'Opcional',
  required: 'Obrigatório',
  addMoreContext: 'Adicionar mais contexto',
  hideMoreContext: 'Ocultar contexto adicional',
  getUpdates: 'Receber atualizações',
  sending: 'A enviar...',
  addingScreenshot: 'A adicionar captura de ecrã...',
  addScreenshot: 'Adicionar captura de ecrã',
  removeScreenshot: 'Remover captura de ecrã',
  screenshotAdded: 'Captura de ecrã adicionada',
  screenshotCancelled: 'Seleção da captura de ecrã cancelada',
  screenshotInvalid: 'Não é possível anexar essa captura de ecrã',
  screenshotProviderFailed: 'A aplicação não conseguiu fornecer uma captura de ecrã',
  emailPlaceholder: 'Endereço de email',
  consentRequired: 'Confirme o consentimento para receber atualizações',
  invalidEmail: 'Introduza um endereço de email válido',
  requiredResponse: 'Esta resposta é obrigatória',
  invalidResponse: 'Selecione uma resposta válida',
  submitFailed: 'Não foi possível enviar o feedback. Tente novamente.',
  attachmentFailed: 'O feedback foi enviado, mas não foi possível carregar a captura de ecrã.',
  configurationChanged: 'O formulário mudou. Reveja as respostas e envie novamente.',
  privacyPolicy: 'Política de privacidade',
  aiProcessingDisclosure:
    'O texto e as capturas de ecrã enviados podem ser processados para organizar este feedback.',
  rating: 'Classificação',
  feedback: 'Feedback',
  reaction: 'Reação',
  ratingValue: (value) => `${value} de 5`,
}

export function systemMessages(locale: string): SystemMessages {
  return locale.toLowerCase().split(/[-_]/)[0] === 'pt' ? portuguese : english
}
