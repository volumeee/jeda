package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"
)

// Generate creates an HMAC SHA-256 signature for the given payload using the secret key.
// It creates a signature resembling the QStash format.
func Generate(secretKey string, payload []byte) string {
	timestamp := time.Now().Unix()

	// message structure: "timestamp.payload"
	message := fmt.Sprintf("%d.%s", timestamp, string(payload))
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(message))
	signatureBase64 := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return fmt.Sprintf("t=%d,v1=%s", timestamp, signatureBase64)
}
