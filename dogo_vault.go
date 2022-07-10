package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"path/filepath"

	"encoding/hex"

	"sync"

	"github.com/docker/docker-credential-helpers/credentials"
	"github.com/oliverkofoed/dogo/commandtree"
	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/term"
	"github.com/oliverkofoed/dogo/vault"
	"golang.org/x/crypto/ssh/terminal"
)

func dogoVaultCreate(filename string, keyStrength string) error {
	passphrase := ""

	// check the file doesn't already exist
	if _, err := os.Stat(filename); err == nil {
		return fmt.Errorf("There is already a file called %v", flagVault)
	}

	// ask the user for a repeated passphrase
	for {
		fmt.Printf("Please enter passphrase for %v: \n", flagVault)
		firstBytes, err := terminal.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			fmt.Println(neaterror.String("", err, term.IsTerminal))
			continue
		}
		fmt.Printf("Please repeat passphrase for %v: \n", flagVault)
		secondBytes, err := terminal.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			fmt.Println(neaterror.String("", err, term.IsTerminal))
			continue
		}

		if !bytes.Equal(firstBytes, secondBytes) {
			fmt.Println(neaterror.String("", errors.New("That passwords didn't match. Please try again"), term.IsTerminal))
			continue
		}

		passphrase = string(firstBytes)
		break
	}

	// decide on the right scrypt key strength
	strength := vault.SCryptSensitive
	switch keyStrength {
	case "sensitive":
		strength = vault.SCryptSensitive
	case "interactive":
		strength = vault.SCryptInteractive
	default:
		return fmt.Errorf("Invalid key strength '%v'. Valid values are 'sensitive' (slow but more secure) and 'interactive' (faster, reasonably secure)", flagKeyStrength)
	}

	// create the vault
	_, key, err := vault.Create(filename, passphrase, strength)
	if err != nil {
		fmt.Println(neaterror.String("", err, term.IsTerminal))
	}

	// store passphrase in keychain.
	store := getCredStore(flagCredentialsStore)
	if store != nil {
		path, err := filepath.Abs(filename)
		url := "file://" + path
		if err == nil {
			store.Delete(url)
			err := store.Add(&credentials.Credentials{
				Secret:    fmt.Sprintf("%X: %v", key, passphrase),
				Username:  filepath.Base(path),
				ServerURL: url,
			})
			if err != nil {
				fmt.Println(neaterror.String("", err, term.IsTerminal))
				return nil
			}
		}
	}
	return nil
}

func vaultAction(filename string, action func(v *vault.Vault) error) error {
	v, err := getOpenVault(filename)
	if err != nil {
		fmt.Println(neaterror.String("", err, term.IsTerminal))
		return nil
	}

	err = action(v)
	if err != nil {
		fmt.Println(neaterror.String("", err, term.IsTerminal))
	}

	return nil
}

func dogVault(filename string) error {
	vault, err := getOpenVault(filename)
	if err != nil {
		return err
	}

	for _, e := range vault.List() {
		fmt.Println(e.Key)
	}

	return nil
}

func defaultCredStore() string {
	switch runtime.GOOS {
	case "darwin":
		return "osxkeychain"
	case "linux":
		return "secretservice"
	case "windows":
		return "wincred"
	default:
		return ""
	}
}

func getCredStore(store string) credentials.Helper {
	switch store {
	case "none":
		return nil
	default:
		return getPlatformCredStore(store)
	}
}

var openVaultsLock sync.RWMutex
var openVaults = make(map[string]*vault.Vault)

func getOpenVaultFile(filename string, key string) ([]byte, error) {
	v, err := getOpenVault(filename)
	if err != nil {
		return nil, err
	}
	return v.GetBytes(key)
}

func getOpenVaultString(filename string, key string) (string, error) {
	v, err := getOpenVault(filename)
	if err != nil {
		return "", err
	}
	return v.GetString(key)
}

func getOpenVault(filename string) (*vault.Vault, error) {
	path, err := filepath.Abs(filename)
	if err != nil {
		return nil, err
	}
	url := "https://stored2credentials.dogo" + path

	// only one thread at a time
	openVaultsLock.Lock()
	defer openVaultsLock.Unlock()

	// check if we already have this vault.
	if v, found := openVaults[path]; found {
		//openVaultsLock.Unlock()
		return v, nil
	}
	//openVaultsLock.Unlock()

	// check the file exists
	if _, err := os.Stat(filename); err != nil && os.IsNotExist(err) {
		return nil, fmt.Errorf("File does not exist: %v", filename)
	}

	// check if we have a credentials store we can use.
	store := getCredStore(flagCredentialsStore)
	if store != nil {
		_, pass, err := store.Get(url)
		if err == nil {
			parts := strings.Split(pass, ":")
			key, _ := hex.DecodeString(parts[0])
			passphrase := pass[len(parts[0])+2:]

			v, _, err := vault.Open(path, passphrase, key)
			if err != nil && !strings.Contains(err.Error(), "wrong passphrase") {
				return nil, err
			}
			if v != nil {
				openVaults[path] = v
				return v, nil
			}
		}
	}

	// did we get a passphrase from ENV?
	envPassphrase := strings.TrimSpace(os.Getenv(strings.ToUpper(fmt.Sprintf("%vPASS", strings.Replace(filename, ".", "", -1)))))
	if envPassphrase != "" {
		v, _, err := vault.Open(path, envPassphrase, nil)
		if err != nil && !strings.Contains(err.Error(), "wrong passphrase") {
			return nil, err
		}
		if v != nil {
			openVaults[path] = v
			return v, nil
		}
	}

	// check that we actually can ask for a pssword.
	if !term.IsTerminal {
		return nil, fmt.Errorf("can't ask for password for vault %v in non-terminal context", filename)
	}

	// ask for passphrase
	var v *vault.Vault
	var passphraseBytes []byte
	commandtree.ConsoleUIInterupt(func() {
		// check if we already have this vault.
		//openVaultsLock.Lock()
		if vx, found := openVaults[path]; found {
			v = vx
			//openVaultsLock.Unlock()
		} else {
			//openVaultsLock.Unlock()
			fmt.Printf("Please enter passphrase for %v: \n", filename)
			passphraseBytes, err = terminal.ReadPassword(int(os.Stdin.Fd()))
			term.MoveUp(1)
			term.EraseCurrentLine()
			if err == nil {
				fmt.Printf("Opening vault... \n")

				// open vault
				var key []byte
				v, key, err = vault.Open(path, string(passphraseBytes), nil)
				term.MoveUp(1)
				term.EraseCurrentLine()
				if err == nil {
					// store if a credstore is specified
					if store != nil {
						store.Delete(url)
						store.Add(&credentials.Credentials{
							Secret:    fmt.Sprintf("%X: %v", key, string(passphraseBytes)),
							Username:  filepath.Base(path),
							ServerURL: url,
						})
					}

					// save the vault in memory for future requests
					//openVaultsLock.Lock()
					openVaults[path] = v
					//openVaultsLock.Unlock()
				}
			}
		}
	})
	if err != nil {
		return nil, err
	}

	return v, nil
}
