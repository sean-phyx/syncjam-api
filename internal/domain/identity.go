package domain

// AuthedIdentity is the result of a successful auth handshake.
// IdentityKey is backend-native (Subsonic username or API-key prefix);
// DisplayName is what other members see.
type AuthedIdentity struct {
	IdentityKey string
	DisplayName string
}
