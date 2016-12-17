package vault

import (
	"bytes"
	"fmt"
	"os/exec"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestEncryption(t *testing.T) {
	encryptionScryptArgs, err := makeScryptArgs(SCryptInteractive)
	if err != nil {
		t.Error("could not make salt")
		return
	}
	key := makeScryptKey("this is a my key", encryptionScryptArgs)

	message := []byte{1, 2, 3, 4}
	encrypted, err := encrypt(key, message)
	if err != nil {
		t.Error(err)
		return
	}

	if bytes.Equal(encrypted, message) {
		t.Error("no encryption happend :(")
		return
	}

	decrypted, err := decrypt(key, encrypted)
	if err != nil {
		t.Error(err)
		return
	}

	if !bytes.Equal(message, decrypted) {
		t.Error("not the same")
		return
	}
}

func TestVault(t *testing.T) {
	passphrase := "hullaballullah"
	path := ".testvault.vault"

	// create a vault
	vault, _, err := Create(path, passphrase, SCryptInteractive)
	if err != nil {
		t.Errorf("Could not create vault at path %v. Err: %v", path, err)
		t.FailNow()
	}
	m := make(map[string]interface{})

	// check
	check(t, vault, passphrase, m)

	// add a value
	m["akey"] = "12345"
	m["bkey"] = []byte{1, 2, 3, 4, 5}
	vault.SetString("akey", m["akey"].(string))
	vault.SetBytes("bkey", m["bkey"].([]byte))
	check(t, vault, passphrase, m)

	// add a value again
	m["greeting"] = "hello world"
	vault.SetString("greeting", m["greeting"].(string))
	check(t, vault, passphrase, m)

	// remove a value.
	delete(m, "bkey")
	vault.Remove("bkey")
	check(t, vault, passphrase, m)

	// Rekey
	newPassphrase := "thisisanotherkeyentirely"
	err = vault.Rekey(newPassphrase, SCryptInteractive)
	if err != nil {
		t.Errorf("Could not rekey err: %v", err)
		t.FailNow()
	}
	check(t, vault, newPassphrase, m)

	// make sure we get a nice error message if opening with wrong key.
	_, _, err = Open(path, "badkey", nil)
	if err == nil {
		t.Errorf("Expected an error, but didn't get any.")
		t.FailNow()
	}
	if !strings.Contains(err.Error(), "Probably wrong passphrase") {
		t.Errorf("Expected an error about wrong passphrase.")
		t.FailNow()
	}
}

func check(t *testing.T, vault *Vault, passphrase string, m map[string]interface{}) {
	// check that the value is there
	list := vault.List()
	if len(list) != len(m) {
		t.Errorf("Did not get correct value from List(): %v", list)
		t.FailNow()
	}
	for i, key := range sortKeys(m) {
		if list[i].Key != key {
			t.Errorf("Value at #%v should have key %v, but has key %v", i, key, list[i].Key)
			t.FailNow()
		}
		iface := m[key]
		switch v := iface.(type) {
		case string:
			rv, err := vault.GetString(key)
			if list[i].Type != EntryTypeString || err != nil || rv != v {
				t.Errorf("Value at %v should have been: %v (string), but was %v. (err: %v)", key, iface, rv, err)
				t.FailNow()
			}
		case []byte:
			rv, err := vault.GetBytes(key)
			if list[i].Type != EntryTypeBytes || err != nil || !bytes.Equal(rv, v) {
				t.Errorf("Value at %v should have been: %v ([]byte), but was %v. (err: %v)", key, iface, rv, err)
				t.FailNow()
			}
		}
	}

	for i := 0; i != 2; i++ {
		// write to a temporary file
		if err := vault.Save(); err != nil {
			t.Error(err)
			t.FailNow()
		}

		// read from temporary file with given key.
		tmp := ".testtemporaryfile"
		cpCmd := exec.Command("cp", "-rf", vault.path, tmp)
		err := cpCmd.Run()
		if err != nil {
			t.Error(err)
			t.FailNow()
		}

		read, _, err := Open(tmp, passphrase, nil)
		if err != nil {
			t.Error(err)
			t.FailNow()
		}

		// compare for equality
		listExpected := vault.List()
		listGot := read.List()
		if len(listExpected) != len(listGot) {
			t.Error("not equal (length comparison)")
			t.FailNow()
		}
		for n, e := range listExpected {
			g := listGot[n]

			if e.Key != g.Key || e.Type != g.Type {
				t.Error("not equal (key and type)")
				t.FailNow()
			}

			vE, eE := vault.get(e.Key)
			vG, eG := read.get(e.Key)
			if eE != nil || eG != nil {
				fmt.Println(eE, eG)
				t.Error("not equal (errors)")
				t.FailNow()
			}

			if !reflect.DeepEqual(vE, vG) {
				t.Error("not equal (value)")
				t.FailNow()
			}
		}
	}
}

func sortKeys(m map[string]interface{}) []string {
	arr := make([]string, 0, len(m))
	for k := range m {
		arr = append(arr, k)
	}
	sort.Strings(arr)
	return arr
}
