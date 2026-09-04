package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type params struct {
	time    uint32
	memory  uint32
	threads uint8
	keyLen  uint32
}

var defaultParams = params{time: 3, memory: 64 * 1024, threads: 4, keyLen: 32}

func Hash(password string) (string, error) {
	if len(password) < 8 {
		return "", fmt.Errorf("密码至少 8 位")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, defaultParams.time, defaultParams.memory, defaultParams.threads, defaultParams.keyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		defaultParams.memory, defaultParams.time, defaultParams.threads,
		enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

func Verify(password, encodedHash string) bool {
	p, salt, expected, err := parsePHC(encodedHash)
	if err != nil {
		return false
	}
	computed := argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, p.keyLen)
	return subtle.ConstantTimeCompare(computed, expected) == 1
}

func parsePHC(hash string) (params, []byte, []byte, error) {
	var p params
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return p, nil, nil, errors.New("invalid hash format")
	}
	var (
		version int
		time    uint32
		memory  uint32
		threads uint8
	)
	n, err := fmt.Sscanf(parts[2], "v=%d", &version)
	if err != nil || n != 1 || version != 19 {
		return p, nil, nil, errors.New("invalid version")
	}
	n, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads)
	if err != nil || n != 3 {
		return p, nil, nil, errors.New("invalid params")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return p, nil, nil, err
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return p, nil, nil, err
	}
	return params{time: time, memory: memory, threads: threads, keyLen: uint32(len(key))}, salt, key, nil
}
