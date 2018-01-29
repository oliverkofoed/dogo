package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"io/ioutil"

	"github.com/oliverkofoed/dogo/neaterror"
	"github.com/oliverkofoed/dogo/registry"
	"github.com/oliverkofoed/dogo/schema"
	"github.com/oliverkofoed/dogo/term"
	"github.com/oliverkofoed/dogo/vault"
	"github.com/spf13/cobra"
)

var config *schema.Config

var flagAllowDecommission = false
var flagVault = ""
var flagKeyStrength = ""
var flagCredentialsStore = ""

func main() {
	// required for serilization
	registry.GobRegister()

	// read config from current folder
	var errs []error
	config, errs = buildConfig("")
	if errs != nil {
		printErrors(errs)
		return
	}

	// configure command flags
	DogoCmd.PersistentFlags().StringVar(&flagCredentialsStore, "credentials", defaultCredStore(), "the credentials store to read/store the passphrase in so you don't have to re-enter it every time.")
	DogoDeployCommand.PersistentFlags().BoolVar(&flagAllowDecommission, "allowdecommission", false, "if true, will remove unused resources/servers from the target environment")
	DogoVaultCommand.PersistentFlags().StringVarP(&flagVault, "vault", "v", "secrets.vault", "vault filename")
	DogoVaultCreateCommand.PersistentFlags().StringVar(&flagKeyStrength, "keystrength", "sensitive", "the strength used to scrypt the passphrase. (interactive:fast, sensitive:slower, more secure)")

	// build corbra-command tree
	DogoCmd.AddCommand(DogoBuildCmd)
	DogoCmd.AddCommand(DogoDeployCommand)
	DogoCmd.AddCommand(DogoSSHCommand)
	DogoCmd.AddCommand(DogoTunnelCommand)
	DogoCmd.AddCommand(DogoVaultCommand)
	DogoVaultCommand.AddCommand(DogoVaultCreateCommand)
	DogoVaultCommand.AddCommand(DogoVaultListCommand)
	DogoVaultCommand.AddCommand(DogoVaultRemoveCommand)
	DogoVaultCommand.AddCommand(DogoVaultSetCommand)
	DogoVaultCommand.AddCommand(DogoVaultGetCommand)
	DogoVaultCommand.AddCommand(DogoVaultSetFileCommand)
	DogoVaultCommand.AddCommand(DogoVaultGetFileCommand)
	for packageName, pack := range config.Packages {
		for name, c := range pack.Commands {
			DogoCmd.AddCommand(createDogoCommand(name, c, packageName))
		}
	}

	// run dogo
	DogoCmd.SilenceErrors = true
	DogoCmd.SilenceUsage = true
	if err := DogoCmd.Execute(); err != nil {
		fmt.Println(neaterror.String("", err, term.IsTerminal))
		os.Exit(-1)
	}
}

func createDogoCommand(name string, c *schema.Command, packageName string) *cobra.Command {
	return &cobra.Command{
		Use:   name + " ENVIRONMENT [target]",
		Short: "[command found in package '" + packageName + "']",
		Long:  "[command found in package '" + packageName + "']",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("requires argument: ENVIRONMENT")
			}
			environment, found := config.Environments[args[0]]
			if !found {
				return fmt.Errorf("unknown environment: %v", args[0])
			}
			forceTarget := ""
			if len(args) >= 2 {
				forceTarget = args[1]
			}
			dogoCommand(config, environment, name, c, packageName, forceTarget, []string{})
			return nil
		},
	}
}

// DogoCmd represents the base command when called without any subcommands
var DogoCmd = &cobra.Command{
	Use:   "dogo",
	Short: "A simple and fast development and deployment tool",
	Long: `Dogo is an opinionated and focused development and deployment 
tool that tries to make the development and deployment for 
multi-component projects easy as pie.`,
}

// DogoBuildCmd represents the 'dogo build' command
var DogoBuildCmd = &cobra.Command{
	Use:     "build",
	Short:   "Build components",
	Long:    `Finds all components that are buildable and builds them.`,
	Example: "dogo build",
	Run: func(cmd *cobra.Command, args []string) {
		dogoBuild(config)
	},
}

// DogoDeployCommand represents the 'dogo deploy [env]' command
var DogoDeployCommand = &cobra.Command{
	Use:     "deploy ENVIRONMENT",
	Short:   "Deploy the given environment",
	Example: "dogo deploy dev",
	//Long:  `TODO`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("requires argument: ENVIRONMENT")
		}
		environment, found := config.Environments[args[0]]
		if !found {
			return fmt.Errorf("unknown environment: %v", args[0])
		}

		dogoDeploy(config, environment, flagAllowDecommission)
		return nil
	},
}

// DogoSSHCommand represents the 'dogo ssh [server]' command
var DogoSSHCommand = &cobra.Command{
	Use:     "ssh SERVER",
	Short:   "Connect and start an shell session via SSH on the given serve",
	Example: "dogo ssh prod.web_1",
	//Long: `Connect and start an shell session via SSH on the given server Example: xsdogo ssh prod.web_1`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("requires argument: SERVER, which must be in the form 'environment.name'. Examples: 'dev.web', 'prod.sql03'")
		}
		parts := strings.Split(args[0], ".")
		if len(parts) != 2 {
			return fmt.Errorf("argument SERVER must be in the form 'environment.name'. Examples: 'dev.web', 'prod.sql03'")
		}

		environment, found := config.Environments[parts[0]]
		if !found {
			return fmt.Errorf("unknown environment: %v", args[0])
		}

		return dogoSSH(config, environment, parts[1])
	},
}

// DogoTunnelCommand represents the 'dogo tunnel [query]' command
var DogoTunnelCommand = &cobra.Command{
	Use:     "tunnel TUNNELQUERY",
	Short:   "Start tunnels matching the given query.",
	Example: "dogo tunnel prod.web_1",
	Long: `Start one or more tunnels from the local machine to machines in the remote environment.

Valid TUNNELQUERY values:
"env"                   -> Start one of each tunnel. Useful for just reaching one of each component type
"env.*"                 -> Start all tunnels to the environment
"env.server"            -> Start all tunnels on the given server
"env.tunnelname"        -> Start the tunnels with the given name across all servers
"env.server.tunnelname" -> Start the given tunnel on the specific server`,

	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("requires argument: TUNNELQUERY")
		}
		parts := strings.Split(args[0], ".")
		enviroment, found := config.Environments[parts[0]]
		if !found {
			return fmt.Errorf("Unknown environment: %v", parts[0])
		}

		query := ""
		if len(parts) > 1 {
			query = args[0][len(parts[0])+1:]
		}

		dogoTunnel(config, enviroment, query)
		return nil
	},
}

// DogoVaultCommand represents the 'dogo vault' command
var DogoVaultCommand = &cobra.Command{
	Use:   "vault [command]",
	Short: "Use the dogo vault subcommands to manage secure vaults",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Usage()
	},
	Example: "dogo vault list",
	Long:    `Manage a secret values vault. If no vault is given, defaults to 'secrets.vault'`,
}

// DogoVaultCreateCommand represents the 'dogo vault create' command
var DogoVaultCreateCommand = &cobra.Command{
	Use:     "create",
	Short:   "Create a new secure vault",
	Example: "dogo vault create --vault=secure.vault --keystrength=sensitive",
	RunE: func(cmd *cobra.Command, args []string) error {
		return dogoVaultCreate(flagVault, flagKeyStrength)
	},
}

// DogoVaultList represents the 'dogo vault list' command
var DogoVaultListCommand = &cobra.Command{
	Use:     "list",
	Short:   "List the contents of a secure vault",
	Example: "dogo vault list",
	RunE: func(cmd *cobra.Command, args []string) error {
		return vaultAction(flagVault, func(v *vault.Vault) error {
			for _, e := range v.List() {
				excerpt := ""
				switch e.Type {
				case vault.EntryTypeString:
					orgStr, err := v.GetString(e.Key)
					if err != nil {
						return err
					}
					str := strings.Replace(orgStr, "\n", "\\n", -1)
					if len(str) > 30 {
						str = str[:27] + "..."
					}
					excerpt = "\"" + str + "\""

					// add sha256
					hasher := sha256.New()
					hasher.Write([]byte(orgStr))
					excerpt = excerpt + ", sha256:" + hex.EncodeToString(hasher.Sum(nil))
				case vault.EntryTypeBytes:
					bytes, err := v.GetBytes(e.Key)
					if err != nil {
						return err
					}
					if len(bytes) > 30 {
						excerpt = fmt.Sprintf("%X...", bytes[:27])
					} else {
						excerpt = fmt.Sprintf("%X", bytes)
					}

					// add sha256
					hasher := sha256.New()
					hasher.Write(bytes)
					excerpt = excerpt + ", sha256:" + hex.EncodeToString(hasher.Sum(nil))
				}
				fmt.Println(e.Key + " (" + e.Type.String() + ": " + excerpt + ")")
			}
			return nil
		})
	},
}

// DogoVaultRemoveCommand represents the 'dogo vault remove' command
var DogoVaultRemoveCommand = &cobra.Command{
	Use:     "remove KEY",
	Short:   "Remove a specific key from the vault",
	Example: "dogo vault remove somekey",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("requires argument: KEY (the key to remove)")
		}
		return vaultAction(flagVault, func(vault *vault.Vault) error {
			vault.Remove(args[0])
			return vault.Save()
		})
	},
}

// DogoVaultSetCommand represents the 'dogo vault set' command
var DogoVaultSetCommand = &cobra.Command{
	Use:     "set KEY VALUE",
	Short:   "Set a key in the vault",
	Example: "dogo vault set somekey somevalue",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 2 {
			return fmt.Errorf("requires two arguments: KEY VALUE")
		}
		return vaultAction(flagVault, func(vault *vault.Vault) error {
			vault.SetString(args[0], args[1])
			return vault.Save()
		})
	},
}

// DogoVaultGetCommand represents the 'dogo vault set' command
var DogoVaultGetCommand = &cobra.Command{
	Use:     "get KEY",
	Short:   "Get the value of the key in the vault",
	Example: "dogo vault get somekey",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("requires argument: KEY (the key to get)")
		}
		return vaultAction(flagVault, func(vault *vault.Vault) error {
			v, err := vault.GetString(args[0])
			if err != nil {
				return err
			}
			fmt.Println(v)
			return nil
		})
	},
}

// DogoVaultSetFileCommand represents the 'dogo vault setfile' command
var DogoVaultSetFileCommand = &cobra.Command{
	Use:     "setfile KEY FILENAME",
	Short:   "Set a key in the vault to the contents of a given file",
	Example: "dogo vault setfile certificate maincert.cert",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 2 {
			return fmt.Errorf("requires two arguments: KEY FILENAME")
		}
		bytes, err := ioutil.ReadFile(args[1])
		if err != nil {
			return err
		}

		return vaultAction(flagVault, func(vault *vault.Vault) error {
			vault.SetBytes(args[0], bytes)
			return vault.Save()
		})
	},
}

// DogoVaultGetFileCommand represents the 'dogo vault getfile' command
var DogoVaultGetFileCommand = &cobra.Command{
	Use:     "getfile KEY",
	Short:   "Write the contents file store in key to stdout",
	Example: "dogo vault getfile somekey",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("requires argument: KEY (the key to get)")
		}
		return vaultAction(flagVault, func(vault *vault.Vault) error {
			v, err := vault.GetBytes(args[0])
			if err != nil {
				return err
			}
			os.Stdout.Write(v)
			return nil
		})
	},
}

// DogoVaultRenameCommand represents the 'dogo vault rename' command
var DogoVaultRenameCommand = &cobra.Command{
	Use:     "rename OLDKEY NEWKEY",
	Short:   "Rename an entry in the vault",
	Example: "dogo vault rename oldkey newkey",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 2 {
			return fmt.Errorf("requires TWO arguments: OldKey (the key to rename) NewKey (the new name for the key)")
		}
		return vaultAction(flagVault, func(vault *vault.Vault) error {
			v, err := vault.GetBytes(args[0])
			if err != nil {
				return err
			}

			vault.SetBytes(args[1], v)
			vault.Remove(args[0])
			return vault.Save()
		})
	},
}
