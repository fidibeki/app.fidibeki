final class FidibekiSystemMessages {
  const FidibekiSystemMessages({
    required this.close,
    required this.optional,
    required this.required,
    required this.addMoreContext,
    required this.hideMoreContext,
    required this.getUpdates,
    required this.sending,
    required this.addingScreenshot,
    required this.addScreenshot,
    required this.removeScreenshot,
    required this.screenshotAdded,
    required this.screenshotCancelled,
    required this.screenshotInvalid,
    required this.screenshotProviderFailed,
    required this.emailPlaceholder,
    required this.consentRequired,
    required this.invalidEmail,
    required this.requiredResponse,
    required this.invalidResponse,
    required this.submitFailed,
    required this.attachmentFailed,
    required this.configurationChanged,
    required this.privacyPolicy,
    required this.aiProcessingDisclosure,
    required this.rating,
    required this.feedback,
    required this.reaction,
    required this.ratingValue,
  });

  final String close;
  final String optional;
  final String required;
  final String addMoreContext;
  final String hideMoreContext;
  final String getUpdates;
  final String sending;
  final String addingScreenshot;
  final String addScreenshot;
  final String removeScreenshot;
  final String screenshotAdded;
  final String screenshotCancelled;
  final String screenshotInvalid;
  final String screenshotProviderFailed;
  final String emailPlaceholder;
  final String consentRequired;
  final String invalidEmail;
  final String requiredResponse;
  final String invalidResponse;
  final String submitFailed;
  final String attachmentFailed;
  final String configurationChanged;
  final String privacyPolicy;
  final String aiProcessingDisclosure;
  final String rating;
  final String feedback;
  final String reaction;
  final String Function(int value) ratingValue;
}

final FidibekiSystemMessages _englishMessages = FidibekiSystemMessages(
  close: 'Close',
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
  attachmentFailed:
      'Feedback was sent, but the screenshot could not be uploaded.',
  configurationChanged:
      'The feedback form changed. Review your answers and submit again.',
  privacyPolicy: 'Privacy policy',
  aiProcessingDisclosure:
      'Submitted text and screenshots may be processed to organize this feedback.',
  rating: 'Rating',
  feedback: 'Feedback',
  reaction: 'Reaction',
  ratingValue: (value) => '$value out of 5',
);

final FidibekiSystemMessages _portugueseMessages = FidibekiSystemMessages(
  close: 'Fechar',
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
  screenshotProviderFailed:
      'A aplicação não conseguiu fornecer uma captura de ecrã',
  emailPlaceholder: 'Endereço de email',
  consentRequired: 'Confirme o consentimento para receber atualizações',
  invalidEmail: 'Introduza um endereço de email válido',
  requiredResponse: 'Esta resposta é obrigatória',
  invalidResponse: 'Selecione uma resposta válida',
  submitFailed: 'Não foi possível enviar o feedback. Tente novamente.',
  attachmentFailed:
      'O feedback foi enviado, mas não foi possível carregar a captura de ecrã.',
  configurationChanged:
      'O formulário mudou. Reveja as respostas e envie novamente.',
  privacyPolicy: 'Política de privacidade',
  aiProcessingDisclosure:
      'O texto e as capturas de ecrã enviados podem ser processados para organizar este feedback.',
  rating: 'Classificação',
  feedback: 'Feedback',
  reaction: 'Reação',
  ratingValue: (value) => '$value de 5',
);

FidibekiSystemMessages fidibekiSystemMessages(String locale) {
  final language = locale.trim().toLowerCase().split(RegExp('[-_]')).first;
  return language == 'pt' ? _portugueseMessages : _englishMessages;
}
