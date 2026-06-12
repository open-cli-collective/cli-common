package credstore

import (
	"encoding/json"
	"time"

	"github.com/byteness/percent"
	jose "github.com/dvsekhvalnov/jose2go"
)

type persistedKeyringItem struct {
	Key                         string
	Data                        []byte
	Label                       string
	Description                 string
	KeychainNotTrustApplication bool
	KeychainNotSynchronizable   bool
}

func encodeFileKeyringItem(it keyringItem, password string) (string, error) {
	bytes, err := json.Marshal(persistedKeyringItem{
		Key:                         it.key,
		Data:                        it.data,
		Label:                       it.label,
		Description:                 it.description,
		KeychainNotTrustApplication: it.keychainNotTrustApplication,
		KeychainNotSynchronizable:   it.keychainNotSynchronizable,
	})
	if err != nil {
		return "", err
	}
	return jose.Encrypt(string(bytes), jose.PBES2_HS256_A128KW, jose.A256GCM, password,
		jose.Headers(map[string]interface{}{
			"created": time.Now().String(),
		}))
}

func fileKeyringFilename(itemKey string) string {
	return percent.Encode(itemKey, "/")
}
