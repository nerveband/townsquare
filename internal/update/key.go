package update

// PublicKeys are the Ed25519 public keys (base64) that may sign releases. To
// rotate, add the new key here, ship a release signed with the old key, then
// sign later releases with the new one.
var PublicKeys = []string{
	"BHDE+x+uEeJIvu0UONP4GaqydgZokuW3I1rv74Y2bUY=",
}
