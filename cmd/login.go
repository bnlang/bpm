package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"bpm/internal/auth"
	"bpm/internal/registry"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in to the active registry",
	RunE: func(cmd *cobra.Command, args []string) error {
		reader := bufio.NewReader(os.Stdin)

		fmt.Print("username or email: ")
		ident, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		ident = strings.TrimSpace(ident)

		fmt.Print("password: ")
		pwBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return err
		}
		password := string(pwBytes)

		c := registry.New(flagRegistry, "")
		auth_, err := c.Login(ident, password)
		if err != nil {
			return err
		}

		store, err := auth.Load()
		if err != nil {
			return err
		}
		store.Set(flagRegistry, auth_.Token)
		if err := store.Save(); err != nil {
			return err
		}
		info("logged in to %s as %s", flagRegistry, auth_.User.Username)
		return nil
	},
}

var signupCmd = &cobra.Command{
	Use:   "signup",
	Short: "Create an account on the active registry",
	RunE: func(cmd *cobra.Command, args []string) error {
		reader := bufio.NewReader(os.Stdin)

		fmt.Print("username: ")
		username, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		username = strings.TrimSpace(username)

		fmt.Print("email: ")
		email, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		email = strings.TrimSpace(email)

		fmt.Print("password (min 8 chars): ")
		pwBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return err
		}

		c := registry.New(flagRegistry, "")
		auth_, err := c.Signup(username, email, string(pwBytes))
		if err != nil {
			return err
		}

		store, err := auth.Load()
		if err != nil {
			return err
		}
		store.Set(flagRegistry, auth_.Token)
		if err := store.Save(); err != nil {
			return err
		}
		info("signed up & logged in as %s", auth_.User.Username)
		return nil
	},
}

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Forget the stored token for the active registry",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := auth.Load()
		if err != nil {
			return err
		}
		store.Clear(flagRegistry)
		if err := store.Save(); err != nil {
			return err
		}
		info("logged out of %s", flagRegistry)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(signupCmd)
	rootCmd.AddCommand(logoutCmd)
}
