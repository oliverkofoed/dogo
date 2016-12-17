package vault

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"math"

	"golang.org/x/crypto/nacl/secretbox"
	"golang.org/x/crypto/scrypt"
)

// SCryptStrength represents the N, r, p values for scrypt. Read more: http://www.tarsnap.com/scrypt/scrypt-slides.pdf
type SCryptStrength int

const (
	// SCryptInteractive is suitiable for interactive use (<100ms) [N:2^14, r:8, p:1]
	SCryptInteractive SCryptStrength = 0
	// SCryptSensitive is suitiable for sensitive long-term storage (<100ms) [N:2^20, r:8, p:1]
	SCryptSensitive SCryptStrength = 1
)

func makeScryptArgs(strength SCryptStrength) ([]byte, error) {
	// find N, r, p
	var N, r, p int
	switch strength {
	case SCryptInteractive:
		N, r, p = int(math.Pow(2, 14)), 8, 1
	case SCryptSensitive:
		N, r, p = int(math.Pow(2, 20)), 8, 1
	default:
		return nil, errors.New("Unknown strength parameter")
	}

	// generate sal
	buf := bytes.NewBuffer(nil)
	if n, err := io.CopyN(buf, rand.Reader, saltSize); err != nil || n != saltSize {
		return nil, errors.New("Could not generate salt")
	}

	// write N, r, p
	for _, v := range []int32{int32(N), int32(r), int32(p)} {
		if err := binary.Write(buf, binary.LittleEndian, &v); err != nil {
			return nil, errors.New("Could not generate salt")
		}
	}

	return buf.Bytes(), nil
}

func makeScryptKey(passphrase string, scryptArgs []byte) *[keySize]byte {
	// read salt, N,r,p from scryptArgs
	salt := scryptArgs[:saltSize]
	buf := bytes.NewBuffer(scryptArgs[saltSize:])
	N := int32(0)
	r := int32(0)
	p := int32(0)
	for _, v := range []*int32{&N, &r, &p} {
		if err := binary.Read(buf, binary.LittleEndian, v); err != nil {
			panic(err)
		}
	}

	// generate key
	key, err := scrypt.Key([]byte(passphrase), salt, int(N), int(r), int(p), keySize)
	if err != nil {
		panic(err)
	}
	var s = new([keySize]byte)
	copy(s[:], key)
	return s
}

func encrypt(key *[keySize]byte, message []byte) ([]byte, error) {
	var nonce [nonceSize]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, err
	}

	return secretbox.Seal(nonce[:], message, &nonce, key), nil
}

func decrypt(key *[keySize]byte, message []byte) ([]byte, error) {
	var decryptNonce [nonceSize]byte
	copy(decryptNonce[:], message[:nonceSize])
	decrypted, ok := secretbox.Open([]byte{}, message[nonceSize:], &decryptNonce, key)
	if !ok {
		return nil, errors.New("Could not decrypt message")
	}
	return decrypted, nil
}

const nonceSize = 24
const keySize = 32
const saltSize = 32
