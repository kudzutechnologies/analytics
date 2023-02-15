package api

import (
	"encoding/hex"
	"fmt"

	"google.golang.org/protobuf/proto"
)

// Common interface for client sessions
type ServiceClientSession interface {
	CreatePushMetricsRequest(metrics *AnalyticsMetrics) (*PushMetricsRequest, error)
}

// Basic, un-encrypted client session
type BasicServiceClientSession struct {
	netId []byte
}

func (s *BasicServiceClientSession) CreatePushMetricsRequest(metrics *AnalyticsMetrics) (*PushMetricsRequest, error) {
	data, err := proto.Marshal(metrics)
	if err != nil {
		return nil, fmt.Errorf("Marshalling data: %w", err)
	}

	return &PushMetricsRequest{
		NetId:       s.netId,
		Data:        data,
		RequestType: ServiceRequestType_REQUEST_BASIC,
	}, nil
}

func CreateBasicServiceClientSession(netId string) (ServiceClientSession, error) {
	netIdBytes, err := hex.DecodeString(netId)
	if err != nil {
		return nil, fmt.Errorf("Could not parse network key: %w", err)
	}

	return &BasicServiceClientSession{
		netId: netIdBytes,
	}, nil
}

// // Encrypted client session
// type EncryptedServiceClientSession struct {
// 	netId       []byte
// 	ctr         int64
// 	blockCipher cipher.Block
// }

// func (s *EncryptedServiceClientSession) CreatePushMetricsRequest(metrics *AnalyticsMetrics) (*PushMetricsRequest, error) {
// 	return nil, nil
// }

// // Compute the CTR frame to use with the encryption routines
// func (s *EncryptedServiceClientSession) ctrFrame(ctr uint64, nonce uint32) []byte {
// 	var frame [16]byte
// 	frame[0] = 0xBE
// 	frame[1] = 0xEF
// 	frame[2] = 0xBA
// 	frame[3] = 0xBE
// 	binary.LittleEndian.PutUint32(frame[4:], nonce)
// 	binary.LittleEndian.PutUint64(frame[8:], ctr)
// 	return frame[:]
// }

// func CreateEncryptedServiceClientSession(netId string, netKey string) (ServiceClientSession, error) {
// 	netIdBytes, err := hex.DecodeString(netId)
// 	if err != nil {
// 		return nil, fmt.Errorf("Could not parse network key: %w", err)
// 	}
// 	key, err := hex.DecodeString(netKey)
// 	if err != nil {
// 		return nil, fmt.Errorf("Could not parse network key: %w", err)
// 	}

// 	// Require 256 bytes of key
// 	if len(key) != 32 {
// 		return nil, fmt.Errorf("Invalid network key length")
// 	}

// 	// Create AES cipher
// 	block, err := aes.NewCipher(key)
// 	if err != nil {
// 		return nil, fmt.Errorf("Could not create cipher: %s", err)
// 	}

// 	inst := &EncryptedServiceClientSession{
// 		netId:       netIdBytes,
// 		ctr:         0,
// 		blockCipher: block,
// 	}

// 	// // The initialization vector is well-known
// 	// plaintext := []byte("some plaintext")

// 	// // The IV needs to be unique, but not secure. Therefore it's common to
// 	// // include it at the beginning of the ciphertext.
// 	// ciphertext := make([]byte, aes.BlockSize+len(plaintext))
// 	// iv := ciphertext[:aes.BlockSize]
// 	// if _, err := io.ReadFull(rand.Reader, iv); err != nil {
// 	// 	panic(err)
// 	// }

// 	// stream := cipher.NewCTR(block, iv)
// 	// stream.XORKeyStream(ciphertext[aes.BlockSize:], plaintext)

// 	// // It's important to remember that ciphertexts must be authenticated
// 	// // (i.e. by using crypto/hmac) as well as being encrypted in order to
// 	// // be secure.

// 	// // CTR mode is the same for both encryption and decryption, so we can
// 	// // also decrypt that ciphertext with NewCTR.

// 	// plaintext2 := make([]byte, len(plaintext))
// 	// stream = cipher.NewCTR(block, iv)
// 	// stream.XORKeyStream(plaintext2, ciphertext[aes.BlockSize:])

// 	// fmt.Printf("%s\n", plaintext2)

// 	return inst, nil
// }
