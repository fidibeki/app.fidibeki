import 'errors.dart';

enum FidibekiPlatform {
  ios('ios'),
  android('android');

  const FidibekiPlatform(this.wireName);

  final String wireName;
}

enum FidibekiRecurrenceMode {
  session('session'),
  cooldown('cooldown'),
  never('never');

  const FidibekiRecurrenceMode(this.wireName);

  final String wireName;
}

enum FidibekiResponseBlockType {
  rating('rating'),
  reaction('reaction'),
  singleChoice('single_choice'),
  freeText('free_text');

  const FidibekiResponseBlockType(this.wireName);

  final String wireName;
}

Map<String, Object?> _object(Object? value, String label) {
  if (value is! Map) invalidCaptureSession('$label must be an object');
  try {
    return value.cast<String, Object?>();
  } on TypeError {
    invalidCaptureSession('$label must use string keys');
  }
}

List<Object?> _list(Object? value, String label) {
  if (value is! List) invalidCaptureSession('$label must be an array');
  return value.cast<Object?>();
}

String _string(Object? value, String label, {int? maxLength}) {
  if (value is! String || value.trim().isEmpty) {
    invalidCaptureSession('$label must be a non-empty string');
  }
  if (maxLength != null && value.length > maxLength) {
    invalidCaptureSession('$label exceeds its maximum length');
  }
  return value;
}

int _integer(Object? value, String label, {int? minimum}) {
  if (value is! int || (minimum != null && value < minimum)) {
    invalidCaptureSession('$label must be a valid integer');
  }
  return value;
}

bool _boolean(Object? value, String label) {
  if (value is! bool) invalidCaptureSession('$label must be a boolean');
  return value;
}

void _rejectUnknownKeys(
  Map<String, Object?> value,
  Set<String> allowed,
  String label,
) {
  if (value.keys.any((key) => !allowed.contains(key))) {
    invalidCaptureSession('$label contains unknown fields');
  }
}

Uri _httpsUri(Object? value, String label) {
  final uri = Uri.tryParse(_string(value, label));
  if (uri == null ||
      uri.scheme != 'https' ||
      uri.host.isEmpty ||
      uri.userInfo.isNotEmpty ||
      uri.fragment.isNotEmpty) {
    invalidCaptureSession('$label must be a safe HTTPS URL');
  }
  return uri;
}

Map<String, String> _stringMap(Object? value, String label) {
  if (value == null) return const <String, String>{};
  final object = _object(value, label);
  return <String, String>{
    for (final entry in object.entries)
      entry.key: _string(entry.value, '$label.${entry.key}'),
  };
}

final class FidibekiResponseOption {
  const FidibekiResponseOption({required this.value});

  factory FidibekiResponseOption.fromJson(Object? value) {
    final json = _object(value, 'response option');
    _rejectUnknownKeys(json, const <String>{'value'}, 'response option');
    return FidibekiResponseOption(
      value: _string(json['value'], 'response option value', maxLength: 64),
    );
  }

  final String value;
}

final class FidibekiResponseBlock {
  const FidibekiResponseBlock({
    required this.key,
    required this.type,
    required this.required,
    required this.options,
  });

  factory FidibekiResponseBlock.fromJson(Object? value) {
    final json = _object(value, 'response block');
    _rejectUnknownKeys(json, const <String>{
      'key',
      'type',
      'required',
      'options',
    }, 'response block');
    final typeValue = _string(json['type'], 'response block type');
    final type = FidibekiResponseBlockType.values
        .where((candidate) => candidate.wireName == typeValue)
        .firstOrNull;
    if (type == null) invalidCaptureSession('unsupported response block');

    final rawOptions = json['options'];
    final options = rawOptions == null
        ? const <FidibekiResponseOption>[]
        : _list(
            rawOptions,
            'response options',
          ).map(FidibekiResponseOption.fromJson).toList(growable: false);
    if ((type == FidibekiResponseBlockType.reaction ||
            type == FidibekiResponseBlockType.singleChoice) &&
        (options.length < 2 || options.length > 10)) {
      invalidCaptureSession('choice blocks require 2 to 10 options');
    }
    if (options.map((option) => option.value).toSet().length !=
        options.length) {
      invalidCaptureSession('response options must be unique');
    }

    return FidibekiResponseBlock(
      key: _string(json['key'], 'response block key', maxLength: 64),
      type: type,
      required: _boolean(json['required'], 'response block required'),
      options: options,
    );
  }

  final String key;
  final FidibekiResponseBlockType type;
  final bool required;
  final List<FidibekiResponseOption> options;
}

final class FidibekiLocalization {
  const FidibekiLocalization({
    required this.launcherLabel,
    required this.prompt,
    required this.commentPlaceholder,
    required this.contactLabel,
    required this.consentLabel,
    required this.submitLabel,
    required this.successMessage,
    required this.blockLabels,
    required this.optionLabels,
    this.privacyPurposeSummary,
    this.privacyLawfulBasisSummary,
    this.privacyRetentionSummary,
  });

  factory FidibekiLocalization.fromJson(Object? value) {
    final json = _object(value, 'localization');
    _rejectUnknownKeys(json, const <String>{
      'launcher_label',
      'prompt',
      'comment_placeholder',
      'contact_label',
      'consent_label',
      'submit_label',
      'success_message',
      'block_labels',
      'option_labels',
      'privacy_purpose_summary',
      'privacy_lawful_basis_summary',
      'privacy_retention_summary',
    }, 'localization');
    final rawOptionLabels = json['option_labels'];
    final optionLabels = <String, Map<String, String>>{};
    if (rawOptionLabels != null) {
      for (final entry in _object(rawOptionLabels, 'option labels').entries) {
        optionLabels[entry.key] = _stringMap(
          entry.value,
          'option labels.${entry.key}',
        );
      }
    }
    return FidibekiLocalization(
      launcherLabel: _string(json['launcher_label'], 'launcher label'),
      prompt: _string(json['prompt'], 'prompt'),
      commentPlaceholder: _string(
        json['comment_placeholder'],
        'comment placeholder',
      ),
      contactLabel: _string(json['contact_label'], 'contact label'),
      consentLabel: _string(json['consent_label'], 'consent label'),
      submitLabel: _string(json['submit_label'], 'submit label'),
      successMessage: _string(json['success_message'], 'success message'),
      blockLabels: _stringMap(json['block_labels'], 'block labels'),
      optionLabels: Map.unmodifiable(optionLabels),
      privacyPurposeSummary: json['privacy_purpose_summary'] == null
          ? null
          : _string(json['privacy_purpose_summary'], 'privacy purpose summary'),
      privacyLawfulBasisSummary: json['privacy_lawful_basis_summary'] == null
          ? null
          : _string(
              json['privacy_lawful_basis_summary'],
              'privacy lawful basis summary',
            ),
      privacyRetentionSummary: json['privacy_retention_summary'] == null
          ? null
          : _string(
              json['privacy_retention_summary'],
              'privacy retention summary',
            ),
    );
  }

  final String launcherLabel;
  final String prompt;
  final String commentPlaceholder;
  final String contactLabel;
  final String consentLabel;
  final String submitLabel;
  final String successMessage;
  final Map<String, String> blockLabels;
  final Map<String, Map<String, String>> optionLabels;
  final String? privacyPurposeSummary;
  final String? privacyLawfulBasisSummary;
  final String? privacyRetentionSummary;
}

final class FidibekiAppearance {
  const FidibekiAppearance({
    this.position,
    this.theme,
    this.accentColor,
    this.accentForeground,
    this.surfaceColor,
    this.textColor,
    this.logoUrl,
  });

  factory FidibekiAppearance.fromJson(Object? value) {
    final json = _object(value, 'appearance');
    _rejectUnknownKeys(json, const <String>{
      'position',
      'theme',
      'accent_color',
      'accent_foreground',
      'surface_color',
      'text_color',
      'logo_url',
    }, 'appearance');
    final theme = json['theme'];
    if (theme != null &&
        (theme is! String ||
            !const <String>{'light', 'dark', 'auto'}.contains(theme))) {
      invalidCaptureSession('invalid appearance theme');
    }

    String? color(String key) {
      final value = json[key];
      if (value == null) return null;
      if (value is! String || !RegExp(r'^#[0-9a-fA-F]{6}$').hasMatch(value)) {
        invalidCaptureSession('invalid appearance color');
      }
      return value;
    }

    final logo = json['logo_url'];
    final logoUrl = logo == null ? null : _httpsUri(logo, 'logo URL');
    final position = json['position'];
    if (position != null &&
        (position is! String ||
            !const <String>{
              'bottom-left',
              'bottom-right',
            }.contains(position))) {
      invalidCaptureSession('invalid appearance position');
    }

    return FidibekiAppearance(
      position: position as String?,
      theme: theme as String?,
      accentColor: color('accent_color'),
      accentForeground: color('accent_foreground'),
      surfaceColor: color('surface_color'),
      textColor: color('text_color'),
      logoUrl: logoUrl,
    );
  }

  final String? position;
  final String? theme;
  final String? accentColor;
  final String? accentForeground;
  final String? surfaceColor;
  final String? textColor;
  final Uri? logoUrl;
}

final class FidibekiControllerNotice {
  const FidibekiControllerNotice({
    required this.noticeVersion,
    required this.controllerProfileVersion,
    required this.widgetDefinitionVersion,
    required this.locale,
    required this.controllerName,
    required this.privacyContactEmail,
    required this.privacyRightsEmail,
    required this.privacyPolicyUrl,
    required this.purposeSummary,
    required this.lawfulBasisSummary,
    required this.retentionSummary,
  });

  factory FidibekiControllerNotice.fromJson(Object? value) {
    final json = _object(value, 'controller notice');
    _rejectUnknownKeys(json, const <String>{
      'notice_version',
      'controller_profile_version',
      'widget_definition_version',
      'locale',
      'controller_name',
      'privacy_contact_email',
      'privacy_rights_email',
      'privacy_policy_url',
      'purpose_summary',
      'lawful_basis_summary',
      'retention_summary',
    }, 'controller notice');
    return FidibekiControllerNotice(
      noticeVersion: _string(json['notice_version'], 'notice version'),
      controllerProfileVersion: _integer(
        json['controller_profile_version'],
        'controller profile version',
        minimum: 1,
      ),
      widgetDefinitionVersion: _integer(
        json['widget_definition_version'],
        'widget definition version',
        minimum: 1,
      ),
      locale: _string(json['locale'], 'notice locale'),
      controllerName: _string(json['controller_name'], 'controller name'),
      privacyContactEmail: _string(
        json['privacy_contact_email'],
        'privacy contact email',
      ),
      privacyRightsEmail: _string(
        json['privacy_rights_email'],
        'privacy rights email',
      ),
      privacyPolicyUrl: _httpsUri(
        json['privacy_policy_url'],
        'privacy policy URL',
      ),
      purposeSummary: _string(json['purpose_summary'], 'purpose summary'),
      lawfulBasisSummary: _string(
        json['lawful_basis_summary'],
        'lawful basis summary',
      ),
      retentionSummary: _string(json['retention_summary'], 'retention summary'),
    );
  }

  final String noticeVersion;
  final int controllerProfileVersion;
  final int widgetDefinitionVersion;
  final String locale;
  final String controllerName;
  final String privacyContactEmail;
  final String privacyRightsEmail;
  final Uri privacyPolicyUrl;
  final String purposeSummary;
  final String lawfulBasisSummary;
  final String retentionSummary;
}

final class FidibekiControllerProfile {
  const FidibekiControllerProfile({
    required this.version,
    required this.name,
    required this.privacyContactEmail,
    required this.privacyRightsEmail,
    required this.privacyPolicyUrl,
  });

  factory FidibekiControllerProfile.fromJson(Object? value) {
    final json = _object(value, 'controller profile');
    _rejectUnknownKeys(json, const <String>{
      'version',
      'name',
      'privacy_contact_email',
      'privacy_rights_email',
      'privacy_policy_url',
    }, 'controller profile');
    return FidibekiControllerProfile(
      version: _integer(
        json['version'],
        'controller profile version',
        minimum: 1,
      ),
      name: _string(json['name'], 'controller profile name'),
      privacyContactEmail: _string(
        json['privacy_contact_email'],
        'controller privacy contact email',
      ),
      privacyRightsEmail: _string(
        json['privacy_rights_email'],
        'controller privacy rights email',
      ),
      privacyPolicyUrl: _httpsUri(
        json['privacy_policy_url'],
        'controller privacy policy URL',
      ),
    );
  }

  final int version;
  final String name;
  final String privacyContactEmail;
  final String privacyRightsEmail;
  final Uri privacyPolicyUrl;
}

final class FidibekiAiProcessing {
  const FidibekiAiProcessing({
    required this.enabled,
    required this.inputCategories,
    this.provider,
    this.privacyPolicyUrl,
    this.policyVersion,
  });

  factory FidibekiAiProcessing.fromJson(Object? value) {
    final json = _object(value, 'AI disclosure');
    _rejectUnknownKeys(json, const <String>{
      'enabled',
      'input_categories',
      'provider',
      'privacy_policy_url',
      'policy_version',
    }, 'AI disclosure');
    final enabled = _boolean(json['enabled'], 'AI disclosure enabled');
    final rawCategories = json['input_categories'];
    final categories = rawCategories == null
        ? const <String>[]
        : _list(rawCategories, 'AI input categories')
              .map((category) => _string(category, 'AI input category'))
              .toList(growable: false);
    if (categories.any(
      (category) => !const <String>{
        'feedback_text',
        'screenshot',
        'audio',
        'video',
      }.contains(category),
    )) {
      invalidCaptureSession('invalid AI input category');
    }
    final provider = json['provider'];
    final privacyPolicyUrl = json['privacy_policy_url'];
    final policyVersion = json['policy_version'];
    if (enabled &&
        (provider == null ||
            privacyPolicyUrl == null ||
            policyVersion == null ||
            rawCategories == null)) {
      invalidCaptureSession('incomplete AI disclosure');
    }
    return FidibekiAiProcessing(
      enabled: enabled,
      provider: provider == null ? null : _string(provider, 'AI provider'),
      privacyPolicyUrl: privacyPolicyUrl == null
          ? null
          : _httpsUri(privacyPolicyUrl, 'AI privacy policy URL'),
      policyVersion: policyVersion == null
          ? null
          : _string(policyVersion, 'AI policy version'),
      inputCategories: List.unmodifiable(categories),
    );
  }

  final bool enabled;
  final String? provider;
  final Uri? privacyPolicyUrl;
  final String? policyVersion;
  final List<String> inputCategories;
}

final class FidibekiWidgetDefinition {
  const FidibekiWidgetDefinition({
    required this.id,
    required this.definitionKey,
    required this.definitionVersion,
    required this.recurrenceMode,
    required this.recurrenceDays,
    required this.responseBlocks,
    required this.localization,
    required this.allowComment,
    required this.collectContact,
    required this.canAttachScreenshot,
    required this.appearance,
    this.aiProcessing,
  });

  factory FidibekiWidgetDefinition.fromJson(Object? value) {
    final json = _object(value, 'widget definition');
    _rejectUnknownKeys(json, const <String>{
      'id',
      'definition_key',
      'definition_version',
      'presentation',
      'launcher_variant',
      'objective',
      'recurrence_mode',
      'recurrence_days',
      'response_blocks',
      'localization',
      'allow_comment',
      'collect_contact',
      'can_attach_screenshot',
      'appearance',
      'ai_processing',
    }, 'widget definition');
    final recurrenceValue = _string(json['recurrence_mode'], 'recurrence mode');
    final recurrenceMode = FidibekiRecurrenceMode.values
        .where((candidate) => candidate.wireName == recurrenceValue)
        .firstOrNull;
    if (recurrenceMode == null) invalidCaptureSession('invalid recurrence');

    for (final entry in const <String, Set<String>>{
      'presentation': {'launcher', 'prompt', 'inline', 'reaction'},
      'launcher_variant': {'edge_tab', 'floating_button'},
      'objective': {
        'continuous_feedback',
        'after_engagement',
        'validate_content',
        'detect_friction',
      },
    }.entries) {
      final actual = _string(json[entry.key], entry.key);
      if (!entry.value.contains(actual)) {
        invalidCaptureSession('invalid ${entry.key}');
      }
    }

    final blocks = _list(
      json['response_blocks'],
      'response blocks',
    ).map(FidibekiResponseBlock.fromJson).toList(growable: false);
    if (blocks.isEmpty || blocks.length > 8) {
      invalidCaptureSession('response blocks must contain 1 to 8 items');
    }
    if (blocks.map((block) => block.key).toSet().length != blocks.length) {
      invalidCaptureSession('response block keys must be unique');
    }

    final recurrenceDays = json['recurrence_days'];
    if (recurrenceMode == FidibekiRecurrenceMode.cooldown) {
      if (recurrenceDays is! int ||
          recurrenceDays < 1 ||
          recurrenceDays > 3650) {
        invalidCaptureSession('invalid recurrence cooldown');
      }
    } else if (recurrenceDays != null) {
      invalidCaptureSession('recurrence days are only valid for cooldown mode');
    }

    final ai = json['ai_processing'];

    return FidibekiWidgetDefinition(
      id: _string(json['id'], 'definition ID'),
      definitionKey: _string(
        json['definition_key'],
        'definition key',
        maxLength: 128,
      ),
      definitionVersion: _integer(
        json['definition_version'],
        'definition version',
        minimum: 1,
      ),
      recurrenceMode: recurrenceMode,
      recurrenceDays: recurrenceDays as int?,
      responseBlocks: blocks,
      localization: FidibekiLocalization.fromJson(json['localization']),
      allowComment: _boolean(json['allow_comment'], 'allow comment'),
      collectContact: _boolean(json['collect_contact'], 'collect contact'),
      canAttachScreenshot: _boolean(
        json['can_attach_screenshot'],
        'can attach screenshot',
      ),
      appearance: FidibekiAppearance.fromJson(json['appearance']),
      aiProcessing: ai == null ? null : FidibekiAiProcessing.fromJson(ai),
    );
  }

  final String id;
  final String definitionKey;
  final int definitionVersion;
  final FidibekiRecurrenceMode recurrenceMode;
  final int? recurrenceDays;
  final List<FidibekiResponseBlock> responseBlocks;
  final FidibekiLocalization localization;
  final bool allowComment;
  final bool collectContact;
  final bool canAttachScreenshot;
  final FidibekiAppearance appearance;
  final FidibekiAiProcessing? aiProcessing;
}

final class FidibekiCaptureConfiguration {
  const FidibekiCaptureConfiguration({
    required this.schemaVersion,
    required this.projectId,
    required this.displayLocale,
    required this.definition,
    this.controllerProfile,
    this.contactNotice,
  });

  factory FidibekiCaptureConfiguration.fromJson(Object? value) {
    final json = _object(value, 'configuration');
    _rejectUnknownKeys(json, const <String>{
      'schema_version',
      'project_id',
      'display_locale',
      'definition',
      'controller_profile',
      'contact_notice',
    }, 'configuration');
    final schemaVersion = json['schema_version'];
    if (schemaVersion != 1) {
      invalidCaptureSession('unsupported schema version');
    }
    final displayLocale = _string(json['display_locale'], 'display locale');
    final definition = FidibekiWidgetDefinition.fromJson(json['definition']);
    final profile = json['controller_profile'] == null
        ? null
        : FidibekiControllerProfile.fromJson(json['controller_profile']);
    final notice = json['contact_notice'] == null
        ? null
        : FidibekiControllerNotice.fromJson(json['contact_notice']);
    if (definition.collectContact) {
      if (notice == null ||
          notice.locale != displayLocale ||
          (profile != null &&
              notice.controllerProfileVersion != profile.version) ||
          notice.widgetDefinitionVersion != definition.definitionVersion) {
        invalidCaptureSession(
          'contact collection requires the matching controller notice',
        );
      }
    }
    return FidibekiCaptureConfiguration(
      schemaVersion: 1,
      projectId: _string(json['project_id'], 'project ID'),
      displayLocale: displayLocale,
      definition: definition,
      controllerProfile: profile,
      contactNotice: notice,
    );
  }

  final int schemaVersion;
  final String projectId;
  final String displayLocale;
  final FidibekiWidgetDefinition definition;
  final FidibekiControllerProfile? controllerProfile;
  final FidibekiControllerNotice? contactNotice;
}

final class FidibekiCaptureSession {
  const FidibekiCaptureSession({
    required this.token,
    required this.submitExpiresAt,
    required this.configuration,
  });

  factory FidibekiCaptureSession.fromJson(Map<String, Object?> json) {
    _rejectUnknownKeys(json, const <String>{
      'token',
      'submit_expires_at',
      'configuration',
    }, 'capture session');
    final token = _string(json['token'], 'capture token');
    if (!token.startsWith('fmc_')) {
      invalidCaptureSession('invalid capture token');
    }
    final rawExpiry = _string(json['submit_expires_at'], 'submit expiry');
    final expiry = DateTime.tryParse(rawExpiry);
    if (expiry == null) invalidCaptureSession('invalid submit expiry');
    return FidibekiCaptureSession(
      token: token,
      submitExpiresAt: expiry.toUtc(),
      configuration: FidibekiCaptureConfiguration.fromJson(
        json['configuration'],
      ),
    );
  }

  final String token;
  final DateTime submitExpiresAt;
  final FidibekiCaptureConfiguration configuration;
}

final class FidibekiSessionRequest {
  const FidibekiSessionRequest({
    required this.platform,
    required this.screen,
    required this.displayLocale,
  });

  final FidibekiPlatform platform;
  final String screen;
  final String displayLocale;
}

typedef FidibekiSessionProvider =
    Future<FidibekiCaptureSession> Function(FidibekiSessionRequest request);
