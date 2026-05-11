package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage CI / publish tokens",
}

var tokenCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new bearer token",
	RunE: func(cmd *cobra.Command, args []string) error {
		label, _ := cmd.Flags().GetString("label")
		c, err := loadClient(true)
		if err != nil {
			return err
		}
		t, err := c.CreateToken(label)
		if err != nil {
			return err
		}
		fmt.Println(t.Token)
		info("(saved to registry as id=%s) — store this token securely; it will NOT be shown again", t.ID)
		return nil
	},
}

var tokenListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your tokens (raw bearers not shown)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := loadClient(true)
		if err != nil {
			return err
		}
		ts, err := c.ListTokens()
		if err != nil {
			return err
		}
		for _, t := range ts {
			label := t.Label
			if label == "" {
				label = "(login session)"
			}
			fmt.Printf("%s  %-32s  %s\n", t.ID, label, t.CreatedAt.Format("2006-01-02 15:04"))
		}
		return nil
	},
}

var tokenRevokeCmd = &cobra.Command{
	Use:   "revoke <id>",
	Short: "Revoke a token",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := loadClient(true)
		if err != nil {
			return err
		}
		if err := c.RevokeToken(args[0]); err != nil {
			return err
		}
		info("revoked %s", args[0])
		return nil
	},
}

func init() {
	tokenCreateCmd.Flags().String("label", "", "human-readable label (e.g. \"ci-prod\")")
	tokenCmd.AddCommand(tokenCreateCmd, tokenListCmd, tokenRevokeCmd)
	rootCmd.AddCommand(tokenCmd)
}
