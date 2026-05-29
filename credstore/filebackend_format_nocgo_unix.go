//go:build !cgo && (linux || darwin)

package credstore

import (
	"encoding/json"

	jose "github.com/dvsekhvalnov/jose2go"
)

func decodeFileKeyringItem(token, password string) (keyringItem, error) {
	payload, _, err := jose.Decode(token, password)
	if err != nil {
		return keyringItem{}, err
	}
	var decoded persistedKeyringItem
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return keyringItem{}, err
	}
	return keyringItem{key: decoded.Key, data: decoded.Data}, nil
}
