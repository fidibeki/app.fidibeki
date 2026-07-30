import 'dart:typed_data';

import 'package:uuid/uuid.dart';

import 'errors.dart';

const int maxFidibekiScreenshotBytes = 10 * 1024 * 1024;

const Set<String> _supportedScreenshotTypes = <String>{
  'image/png',
  'image/jpeg',
  'image/webp',
};

final class FidibekiScreenshotAsset {
  const FidibekiScreenshotAsset({
    required this.contentType,
    required this.sizeBytes,
    required this.read,
    this.dispose,
  });

  final String contentType;
  final int sizeBytes;
  final Future<Uint8List> Function(int maximumBytes) read;
  final Future<void> Function()? dispose;
}

typedef FidibekiScreenshotProvider =
    Future<FidibekiScreenshotAsset?> Function();

enum FidibekiAttachmentStatus { uploaded, failed }

enum FidibekiAttachmentStage { upload, complete }

enum FidibekiScreenshotNotice { cancelled, invalid, providerFailed }

final class FidibekiAttachmentOutcome {
  const FidibekiAttachmentOutcome({
    required this.attachmentId,
    required this.status,
    this.stage,
    this.error,
  });

  final String attachmentId;
  final FidibekiAttachmentStatus status;
  final FidibekiAttachmentStage? stage;
  final String? error;
}

final class FidibekiPreparedScreenshot {
  FidibekiPreparedScreenshot._({
    required this.clientAttachmentId,
    required this.contentType,
    required this.sizeBytes,
    required Uint8List bytes,
    required this._asset,
  }) : _bytes = Uint8List.fromList(bytes).asUnmodifiableView();

  final String clientAttachmentId;
  final String contentType;
  final int sizeBytes;
  final Uint8List _bytes;
  final FidibekiScreenshotAsset _asset;
  bool _disposed = false;

  Uint8List get bytes => _bytes;

  Future<void> dispose() async {
    if (_disposed) return;
    _disposed = true;
    try {
      await _asset.dispose?.call();
    } on Object {
      // Host cleanup failures must not expose private provider details.
    }
  }

  @override
  String toString() {
    return 'FidibekiPreparedScreenshot('
        'contentType: $contentType, sizeBytes: $sizeBytes)';
  }
}

bool _hasPrefix(Uint8List bytes, List<int> signature) {
  if (bytes.length < signature.length) return false;
  for (var index = 0; index < signature.length; index += 1) {
    if (bytes[index] != signature[index]) return false;
  }
  return true;
}

bool _matchesDeclaredType(Uint8List bytes, String contentType) {
  return switch (contentType) {
    'image/png' => _hasPrefix(bytes, const <int>[
      0x89,
      0x50,
      0x4e,
      0x47,
      0x0d,
      0x0a,
      0x1a,
      0x0a,
    ]),
    'image/jpeg' =>
      bytes.length >= 4 &&
          _hasPrefix(bytes, const <int>[0xff, 0xd8, 0xff]) &&
          bytes[bytes.length - 2] == 0xff &&
          bytes.last == 0xd9,
    'image/webp' =>
      bytes.length >= 12 &&
          _hasPrefix(bytes, const <int>[0x52, 0x49, 0x46, 0x46]) &&
          bytes[8] == 0x57 &&
          bytes[9] == 0x45 &&
          bytes[10] == 0x42 &&
          bytes[11] == 0x50 &&
          ByteData.sublistView(bytes, 4, 8).getUint32(0, Endian.little) ==
              bytes.length - 8,
    _ => false,
  };
}

Future<FidibekiPreparedScreenshot> prepareFidibekiScreenshot(
  FidibekiScreenshotAsset asset, {
  String Function()? clientAttachmentIdGenerator,
}) async {
  Future<void> disposeAsset() async {
    try {
      await asset.dispose?.call();
    } on Object {
      // Preserve the SDK-owned safe error.
    }
  }

  if (!_supportedScreenshotTypes.contains(asset.contentType) ||
      asset.sizeBytes < 1 ||
      asset.sizeBytes > maxFidibekiScreenshotBytes) {
    await disposeAsset();
    throw const FidibekiException(
      FidibekiErrorCode.attachment,
      'Invalid screenshot declaration.',
    );
  }

  try {
    final bytes = await asset.read(maxFidibekiScreenshotBytes);
    if (bytes.length != asset.sizeBytes ||
        bytes.isEmpty ||
        bytes.length > maxFidibekiScreenshotBytes) {
      await disposeAsset();
      throw const FidibekiException(
        FidibekiErrorCode.attachment,
        'Screenshot data does not match its declaration.',
      );
    }
    if (!_matchesDeclaredType(bytes, asset.contentType)) {
      await disposeAsset();
      throw const FidibekiException(
        FidibekiErrorCode.attachment,
        'Screenshot data does not match its declaration.',
      );
    }
    return FidibekiPreparedScreenshot._(
      clientAttachmentId: (clientAttachmentIdGenerator ?? const Uuid().v4)
          .call(),
      contentType: asset.contentType,
      sizeBytes: asset.sizeBytes,
      bytes: bytes,
      asset: asset,
    );
  } on FidibekiException {
    rethrow;
  } on Object {
    await disposeAsset();
    throw const FidibekiException(
      FidibekiErrorCode.attachment,
      'Screenshot could not be read.',
    );
  }
}
