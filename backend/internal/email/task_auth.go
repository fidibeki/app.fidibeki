package email

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const emailTaskEnvelopeVersion = 1

type TaskAuthenticator struct {
	currentKey  []byte
	previousKey []byte
}

type emailTaskEnvelope struct {
	Version   int             `json:"version"`
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}

func NewTaskAuthenticator(currentEncoded, previousEncoded string) (TaskAuthenticator, error) {
	current, err := decodeTaskSigningKey("current", currentEncoded, true)
	if err != nil {
		return TaskAuthenticator{}, err
	}
	previous, err := decodeTaskSigningKey("previous", previousEncoded, false)
	if err != nil {
		return TaskAuthenticator{}, err
	}
	if len(previous) > 0 && hmac.Equal(current, previous) {
		return TaskAuthenticator{}, errors.New("previous signing key must differ from current signing key")
	}
	return TaskAuthenticator{currentKey: current, previousKey: previous}, nil
}

func (a TaskAuthenticator) validate() error {
	if len(a.currentKey) != sha256.Size {
		return errors.New("email task authenticator is not configured")
	}
	if len(a.previousKey) != 0 && len(a.previousKey) != sha256.Size {
		return errors.New("email task previous signing key is invalid")
	}
	return nil
}

func (a TaskAuthenticator) Encode(message Message) ([]byte, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	if err := message.Validate(); err != nil {
		return nil, fmt.Errorf("validate email task payload: %w", err)
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("encode email task payload: %w", err)
	}
	envelope := emailTaskEnvelope{
		Version: emailTaskEnvelopeVersion,
		Payload: payload,
		Signature: base64.RawURLEncoding.EncodeToString(
			emailTaskMAC(a.currentKey, emailTaskEnvelopeVersion, payload),
		),
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("encode signed email task: %w", err)
	}
	return encoded, nil
}

func (a TaskAuthenticator) Decode(encoded []byte) (Message, error) {
	if err := a.validate(); err != nil {
		return Message{}, err
	}
	var envelope emailTaskEnvelope
	if err := decodeStrictJSON(encoded, &envelope); err != nil {
		return Message{}, fmt.Errorf("decode signed email task: %w", err)
	}
	if envelope.Version != emailTaskEnvelopeVersion {
		return Message{}, fmt.Errorf("unsupported email task envelope version %d", envelope.Version)
	}
	if len(envelope.Payload) == 0 || envelope.Signature == "" {
		return Message{}, errors.New("signed email task payload and signature are required")
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(envelope.Signature)
	if err != nil || len(signature) != sha256.Size {
		return Message{}, errors.New("email task signature is invalid")
	}
	if !a.verify(envelope.Version, envelope.Payload, signature) {
		return Message{}, errors.New("email task signature verification failed")
	}

	var message Message
	if err := decodeStrictJSON(envelope.Payload, &message); err != nil {
		return Message{}, fmt.Errorf("decode email task payload: %w", err)
	}
	if err := message.Validate(); err != nil {
		return Message{}, fmt.Errorf("validate email task payload: %w", err)
	}
	return message, nil
}

func (a TaskAuthenticator) verify(version int, payload, signature []byte) bool {
	currentMatch := hmac.Equal(signature, emailTaskMAC(a.currentKey, version, payload))
	previousMatch := false
	if len(a.previousKey) == sha256.Size {
		previousMatch = hmac.Equal(signature, emailTaskMAC(a.previousKey, version, payload))
	}
	return currentMatch || previousMatch
}

func emailTaskMAC(key []byte, version int, payload []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(TaskSendEmail))
	_, _ = mac.Write([]byte{0})
	var encodedVersion [8]byte
	binary.BigEndian.PutUint64(encodedVersion[:], uint64(version))
	_, _ = mac.Write(encodedVersion[:])
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func decodeTaskSigningKey(name, encoded string, required bool) ([]byte, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		if required {
			return nil, fmt.Errorf("%s signing key is required", name)
		}
		return nil, nil
	}
	key, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(key) != sha256.Size {
		return nil, fmt.Errorf("%s signing key must be base64 encoding of exactly 32 bytes", name)
	}
	return key, nil
}

func decodeStrictJSON(encoded []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}
