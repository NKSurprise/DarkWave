package argon2stuff

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Argon2Params struct {
	Memory      uint32 //KiB
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// var param = &Argon2Params{
// 	Memory:      64 * 1024, // MiB
// 	Iterations:  3,
// 	Parallelism: 2,
// 	SaltLength:  16,
// 	KeyLength:   32,
// }

func HashPassword(password string, p *Argon2Params) (encodedHash string, err error) {
	salt := make([]byte, p.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, p.KeyLength)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encodedHash = fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", p.Memory, p.Iterations, p.Parallelism, b64Salt, b64Hash)
	return encodedHash, nil
}

func VerifyPassword(password, encodedHash string) (bool, error) {
	if password == "" {
		return false, errors.New("empty password")
	}
	if strings.TrimSpace(encodedHash) == "" {
		return false, errors.New("empty stored hash")
	}

	p, salt, expectedHash, err := DecodeHash(encodedHash)
	if err != nil {
		return false, err
	}
	if len(salt) < 8 {
		return false, errors.New("bad salt")
	}
	if len(expectedHash) == 0 {
		return false, errors.New("bad hash")
	}

	// Use the stored hash length as the key length to derive for verification
	keyLen := uint32(len(expectedHash))
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.Memory, p.Parallelism, keyLen)

	if subtle.ConstantTimeCompare(key, expectedHash) == 1 {
		return true, nil
	}
	return false, nil
}

func DecodeHash(encodedHash string) (Argon2Params, []byte, []byte, error) {
	// Expected: $argon2id$v=19$m=65536,t=3,p=2$<saltB64>$<hashB64>
	parts := strings.Split(strings.TrimSpace(encodedHash), "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Argon2Params{}, nil, nil, errors.New("invalid encoded hash format")
	}
	if parts[2] != "v=19" {
		return Argon2Params{}, nil, nil, errors.New("unsupported argon2 version")
	}

	// Parse m=...,t=...,p=...
	var p Argon2Params
	items := strings.Split(parts[3], ",")
	if len(items) != 3 {
		return Argon2Params{}, nil, nil, errors.New("invalid argon2 parameters")
	}
	var err error
	p.Memory, err = parseUint32KV(items[0], "m")
	if err != nil {
		return Argon2Params{}, nil, nil, err
	}
	p.Iterations, err = parseUint32KV(items[1], "t")
	if err != nil {
		return Argon2Params{}, nil, nil, err
	}
	par, err := parseUint32KV(items[2], "p")
	if err != nil {
		return Argon2Params{}, nil, nil, err
	}
	if par > 255 {
		return Argon2Params{}, nil, nil, errors.New("invalid parallelism value")
	}
	p.Parallelism = uint8(par)

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Argon2Params{}, nil, nil, errors.New("invalid salt b64")
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Argon2Params{}, nil, nil, errors.New("invalid hash b64")
	}

	// Set key length from stored hash length
	p.KeyLength = uint32(len(hash))

	return p, salt, hash, nil
}

func parseUint32KV(s, key string) (uint32, error) {
	kv := strings.SplitN(s, "=", 2)
	if len(kv) != 2 || kv[0] != key {
		return 0, errors.New("invalid format for " + key)
	}
	n, err := strconv.ParseUint(kv[1], 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(n), nil
}
