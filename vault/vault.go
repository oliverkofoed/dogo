package vault

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"sort"

	"github.com/oliverkofoed/dogo/snobgob"
)

// EntryType represents the type of a given entry in a vault
type EntryType int

func (e EntryType) String() string {
	switch e {
	case EntryTypeString:
		return "string"
	case EntryTypeBytes:
		return "bytes"
	default:
		return "unknown"
	}
}

const (
	// EntryTypeString is for entries of type string
	EntryTypeString EntryType = 0
	// EntryTypeBytes is for entries of type []byte
	EntryTypeBytes EntryType = 1
)

// Entry provides name and type information for an entry in a vault
type Entry struct {
	Key             string
	Type            EntryType
	file            *os.File
	fileValueLength int64
	fileValueOffset int64
	decrypted       interface{}
}

// Vault is an encrypted store of key/value pairs
type Vault struct {
	encryptionKey        *[keySize]byte
	encryptionScryptArgs []byte

	//encKey   string
	path     string
	file     *os.File
	entries  []*Entry
	entryMap map[string]*Entry
}

// List returns a list of entries in the vault
func (v *Vault) List() []*Entry {
	return v.entries
}

func (v *Vault) get(key string) (interface{}, error) {
	if e, found := v.entryMap[key]; found {
		// decrypt if required.
		if e.decrypted == nil {
			buf := make([]byte, e.fileValueLength)
			n, err := e.file.ReadAt(buf, e.fileValueOffset)
			if err != nil || int64(n) != e.fileValueLength {
				return nil, fmt.Errorf("Could not read from vault. Wanted: %vbytes, got %vbytes, err: %v", e.fileValueLength, n, err)
			}

			//fmt.Println("DECRYPT", v.encryptionKey, e.Key, buf)
			decrypted, err := decrypt(v.encryptionKey, buf)
			if err != nil {
				return nil, err
			}

			// deserialize value
			decoder := snobgob.NewDecoder(bytes.NewReader(decrypted))
			switch e.Type {
			case EntryTypeBytes:
				var v []byte
				if err = decoder.Decode(&v); err != nil {
					return nil, fmt.Errorf("Could not deserialize entry for key:%v. Err:%v", key, err)
				}
				e.decrypted = v
			case EntryTypeString:
				var v string
				if err = decoder.Decode(&v); err != nil {
					return nil, fmt.Errorf("Could not deserialize entry for key:%v. Err:%v", key, err)
				}
				e.decrypted = v
			}
		}
		return e.decrypted, nil
	}

	return nil, fmt.Errorf("No vault entry with key: %v in vault %v", key, v.path)
}

// GetString returns the string value for the given key and a found/not-found indicator
func (v *Vault) GetString(key string) (string, error) {
	x, err := v.get(key)
	if err == nil {
		if str, ok := x.(string); ok {
			return str, nil
		} else if b, ok := x.([]byte); ok {
			return string(b), nil
		}
	}
	return "", err
}

// GetBytes returns the []byte value for the given key and a found/not-found indicator
func (v *Vault) GetBytes(key string) ([]byte, error) {
	x, err := v.get(key)
	if err == nil {
		if b, ok := x.([]byte); ok {
			return b, nil
		} else if str, ok := x.(string); ok {
			return []byte(str), nil
		}
	}
	return nil, err
}

// SetString sets the value of a given key to the given string
func (v *Vault) SetString(key string, value string) {
	v.set(key, EntryTypeString, value)
}

// SetBytes sets the value of a given key to the given []byte
func (v *Vault) SetBytes(key string, value []byte) {
	v.set(key, EntryTypeBytes, value)
}

// Remove removes a given entry
func (v *Vault) Remove(key string) {
	delete(v.entryMap, key)
	v.updateEntries()
}

func (v *Vault) set(key string, t EntryType, value interface{}) {
	v.entryMap[key] = &Entry{
		Key:       key,
		Type:      t,
		decrypted: value,
	}
	v.updateEntries()
}

func (v *Vault) updateEntries() {
	entries := make([]*Entry, 0, len(v.entryMap))
	for _, e := range v.entryMap {
		entries = append(entries, e)
	}
	sort.Sort(byKey(entries))
	v.entries = entries
}

type byKey []*Entry

func (a byKey) Len() int           { return len(a) }
func (a byKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byKey) Less(i, j int) bool { return a[i].Key < a[j].Key }

// Create creates a new vault at the given destination
func Create(path string, passphrase string, strength SCryptStrength) (*Vault, []byte, error) {
	encryptionScryptArgs, err := makeScryptArgs(strength)
	if err != nil {
		return nil, nil, fmt.Errorf("Could not generate encryption arguments: %v", err)
	}

	encryptionKey := makeScryptKey(passphrase, encryptionScryptArgs)
	v := &Vault{
		encryptionKey:        encryptionKey,
		encryptionScryptArgs: encryptionScryptArgs,
		path:                 path,
		entries:              []*Entry{},
		entryMap:             make(map[string]*Entry),
	}
	if err := v.Save(); err != nil {
		return nil, nil, err
	}
	return v, encryptionKey[:], nil
}

// Open decrypts and deserializes a vault from a file
func Open(path string, passphrase string, firstTryKey []byte) (*Vault, []byte, error) {
	// read the file
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("Could not open vault %v. Err:%v", path, err)
	}

	// read version and lengths
	version := int64(0)
	encryptionScryptArgsLength := int64(0)
	headerLength := int64(0)
	for _, v := range []*int64{&version, &encryptionScryptArgsLength, &headerLength} {
		if err := binary.Read(f, binary.LittleEndian, v); err != nil {
			return nil, nil, err
		}
	}

	// check version
	if version != 0 {
		return nil, nil, fmt.Errorf("Could not recognize file format of %v. Are you sure this is a vault file?", path)
	}

	// read scryptArgsth
	encryptionScryptArgs := make([]byte, encryptionScryptArgsLength)
	n, err := f.ReadAt(encryptionScryptArgs, 8+8+8)
	if err != nil || int64(n) != encryptionScryptArgsLength {
		return nil, nil, fmt.Errorf("Could not read scrypt args from %v. read:%v, expected:%v, err:%v", path, n, encryptionScryptArgsLength, err)
	}

	// read headerbytes
	headerBytes := make([]byte, headerLength)
	n, err = f.ReadAt(headerBytes, 8+8+8+encryptionScryptArgsLength)
	if err != nil || int64(n) != headerLength {
		return nil, nil, fmt.Errorf("Could not read header from %v. read:%v, expected:%v, err:%v", path, n, headerLength, err)
	}

	var encryptionKey *[keySize]byte
	var decryptedHeader []byte

	// in case the key is already know, let's try that.
	if firstTryKey != nil && len(firstTryKey) == keySize {
		var testkey [keySize]byte
		copy(testkey[:], firstTryKey)

		// decrypt header bytes.
		decryptedHeader, err = decrypt(&testkey, headerBytes)
		if err == nil {
			encryptionKey = &testkey
		}
	}

	// create key if we don't already have it.
	if encryptionKey == nil {
		encryptionKey = makeScryptKey(passphrase, encryptionScryptArgs)

		// decrypt header bytes.
		decryptedHeader, err = decrypt(encryptionKey, headerBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("Could not decrypt vault %v. Probably wrong passphrase. (err: %v)", path, err)
		}
	}

	// deserialize header
	header := fileHeader{}
	decoder := snobgob.NewDecoder(bytes.NewReader(decryptedHeader))
	if err = decoder.Decode(&header); err != nil {
		return nil, nil, fmt.Errorf("Could not read header at %v: %v", path, err)
	}

	// create the vault structure
	m := make(map[string]*Entry)
	offset := int64(8 + 8 + 8 + encryptionScryptArgsLength + headerLength)
	for _, e := range header.Entries {
		m[e.Key] = &Entry{
			Key:             e.Key,
			Type:            e.Type,
			file:            f,
			fileValueOffset: offset,
			fileValueLength: e.Length,
		}
		offset += e.Length
	}
	vault := &Vault{
		encryptionKey:        encryptionKey,
		encryptionScryptArgs: encryptionScryptArgs,
		file:                 f,
		entryMap:             m,
		path:                 path,
	}
	vault.updateEntries()
	return vault, encryptionKey[:], nil
}

func (v *Vault) Rekey(newPassphrase string, strength SCryptStrength) error {
	encryptionScryptArgs, err := makeScryptArgs(strength)
	if err != nil {
		return fmt.Errorf("Could not generate encryption arguments: %v", err)
	}

	for _, e := range v.entries {
		// ensure it's decrypted
		_, err := v.get(e.Key)
		if err != nil {
			return fmt.Errorf("Could not rekey because of error when trying to read %v: %v", e.Key, err)
		}

		// remove the file reference, so it'll get reencrypted later.
		e.file = nil
		e.fileValueLength = 0
		e.fileValueOffset = 0
	}

	v.encryptionKey = makeScryptKey(newPassphrase, encryptionScryptArgs)
	v.encryptionScryptArgs = encryptionScryptArgs
	return nil
}

func (v *Vault) Save() error {
	// create header
	header := fileHeader{
		Entries: make([]fileEntry, 0, len(v.entryMap)),
	}

	bodyBytes := bytes.NewBuffer(nil)
	for _, e := range v.entries {
		length := int64(0)

		if e.file != nil {
			// already encrypted, copy from source file.
			buf := make([]byte, e.fileValueLength)
			e.file.ReadAt(buf, e.fileValueOffset)
			bodyBytes.Write(buf)
			length = e.fileValueLength
		} else {
			// not encrypted yet. so, encrypt inline.
			buf := bytes.NewBuffer(nil)
			encoder := snobgob.NewEncoder(buf)
			err := encoder.Encode(e.decrypted)
			if err != nil {
				return fmt.Errorf("Could not encode value in vault at %v: %v", v.path, err)
			}
			encrypted, err := encrypt(v.encryptionKey, buf.Bytes())
			if err != nil {
				return fmt.Errorf("Could not encrypt vault for %v: %v", v.path, err)
			}
			bodyBytes.Write(encrypted)
			length = int64(len(encrypted))
			//fmt.Println("ENCRYPT", v.encryptionKey, e.Key, encrypted)
		}

		header.Entries = append(header.Entries, fileEntry{
			Key:    e.Key,
			Type:   e.Type,
			Length: length,
		})
	}

	// serialize the header
	headerBytes := bytes.NewBuffer(nil)
	encoder := snobgob.NewEncoder(headerBytes)
	err := encoder.Encode(header)
	if err != nil {
		return fmt.Errorf("Could not encode vault for %v: %v", v.path, err)
	}

	// encrypt the header
	encryptedHeaderBytes, err := encrypt(v.encryptionKey, headerBytes.Bytes())
	if err != nil {
		return fmt.Errorf("Could not encode vault for %v: %v", v.path, err)
	}

	// write vault to a temp file
	tmpFile := v.path + ".tmp"
	f, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("Could not open temporary file for writing: %v, %v", tmpFile, err)
	}
	for _, v := range []int64{int64(0), int64(len(v.encryptionScryptArgs)), int64(len(encryptedHeaderBytes))} {
		if err := binary.Write(f, binary.LittleEndian, &v); err != nil {
			return fmt.Errorf("Could not write file lengths: %v", err)
		}
	}
	f.Write(v.encryptionScryptArgs)
	f.Write(encryptedHeaderBytes)
	f.Write(bodyBytes.Bytes())

	// close pointer to currently open file
	if v.file != nil {
		if err := v.file.Close(); err != nil {
			return fmt.Errorf("Could not close existing open vault to %v to %v", v.path, err)
		}
	}

	// move the current file to a bak file, the temp file to the final position, and delete the back file
	// (this is so we're never in a state where we don't have a valid vault on disk)
	bakFile := v.path + ".bak"
	if v.file != nil {
		if err := os.Rename(v.path, bakFile); err != nil {
			return fmt.Errorf("Could not rename %v to %v", v.path, bakFile)
		}
	}
	if err := os.Rename(tmpFile, v.path); err != nil {
		return fmt.Errorf("Could not rename %v to %v", tmpFile, v.path)
	}
	if v.file != nil {
		if err := os.Remove(bakFile); err != nil {
			return fmt.Errorf("Could not delete %v", bakFile)
		}
	}

	// update in-memory vault data structure
	offset := int64(8 + 8 + 8 + len(v.encryptionScryptArgs) + len(encryptedHeaderBytes))
	for _, e := range header.Entries {
		other, found := v.entryMap[e.Key]
		if !found {
			panic("inconsistancy in vault datastructures!")
		}

		other.file = f
		other.fileValueLength = e.Length
		other.fileValueOffset = offset
		offset += e.Length
	}

	return nil
}

type fileHeader struct {
	Entries []fileEntry
}

type fileEntry struct {
	Key    string
	Type   EntryType
	Length int64
}
