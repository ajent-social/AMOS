package operation

import (
	"crypto/sha256"
	"errors"
)

// replayRequestHash consumes the frozen descriptor digest and already validated
// canonical input. It neither decodes transport input nor establishes authority.
func replayRequestHash(descriptorDigest [32]byte, canonicalInput []byte) [32]byte {
	hash := sha256.New()
	// Hash.Write always succeeds.
	_, _ = hash.Write([]byte("amos-operation-replay-v1\x00"))
	_, _ = hash.Write(descriptorDigest[:])
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(canonicalInput)
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	return digest
}

var errInvalidReplayResult = errors.New("invalid stored operation result")

// validateCachedResult checks only the bounded stored output. The caller must
// separately verify current authority and stored descriptor/revision/schema
// identity before replay disclosure. The checksum covers JSON bytes, not kind,
// and is an integrity check, not cryptographic authentication of storage.
func validateCachedResult(definition Definition, stored CachedResult, expectedSHA256 [32]byte) (CachedResult, error) {
	if definition.metadata.OperationID == "" || definition.descriptorDigest == ([32]byte{}) ||
		definition.decode == nil || definition.resolve == nil || definition.encode == nil ||
		definition.validateOutput == nil || definition.invoke == nil {
		return CachedResult{}, errInvalidReplayResult
	}
	metadata := definition.metadata
	if metadata.Idempotency != IdempotencyRequired || metadata.OutputClass != OutputReplaySafe ||
		(metadata.SideEffect != SideEffectWrite && metadata.SideEffect != SideEffectExternal) ||
		metadata.MaxReplayBytes < 1 || metadata.MaxReplayBytes > 65536 {
		return CachedResult{}, errInvalidReplayResult
	}
	if !validResultKind(stored.Kind) || len(stored.CanonicalJSON) == 0 || len(stored.CanonicalJSON) > metadata.MaxReplayBytes {
		return CachedResult{}, errInvalidReplayResult
	}
	if sha256.Sum256(stored.CanonicalJSON) != expectedSHA256 {
		return CachedResult{}, errInvalidReplayResult
	}
	// A validator may retain or mutate its argument. Neither it nor the caller
	// receives the buffer returned for subsequent replay composition.
	validationBytes := append([]byte(nil), stored.CanonicalJSON...)
	if err := definition.validateOutput(validationBytes); err != nil {
		return CachedResult{}, errInvalidReplayResult
	}
	return CachedResult{Kind: stored.Kind, CanonicalJSON: append([]byte(nil), stored.CanonicalJSON...)}, nil
}
