package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/gofrs/uuid"
	"github.com/kairos-io/enki/pkg/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func NewGenkeyCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "genkey NAME",
		Short: "Generate secureboot keys under the uuid generated by NAME",
		Args:  cobra.ExactArgs(1),
		RunE: func(cobraCmd *cobra.Command, args []string) error {
			// Set this after parsing of the flags, so it fails on parsing and prints usage properly
			cobraCmd.SilenceUsage = true
			cobraCmd.SilenceErrors = true // Do not propagate errors down the line, we control them

			cfg, err := config.ReadConfigBuild(viper.GetString("config-dir"), cobraCmd.Flags())
			if err != nil {
				return err
			}
			l := cfg.Logger
			name := args[0]
			uid := uuid.NewV5(uuid.NamespaceDNS, name)
			output, _ := cobraCmd.Flags().GetString("output")

			err = os.MkdirAll(output, 0700)
			if err != nil {
				l.Errorf("Error creating output directory: %s", err)
				return err
			}

			for _, keyType := range []string{"PK", "KEK", "DB"} {
				l.Infof("Generating %s", keyType)
				key := filepath.Join(output, fmt.Sprintf("%s.key", keyType))
				pem := filepath.Join(output, fmt.Sprintf("%s.pem", keyType))
				der := filepath.Join(output, fmt.Sprintf("%s.der", keyType))
				auth := filepath.Join(output, fmt.Sprintf("%s.auth", keyType))
				esl := filepath.Join(output, fmt.Sprintf("%s.esl", keyType))
				args := []string{
					"req", "-nodes", "-x509", "-subj", fmt.Sprintf("/CN=%s/", name),
					"-keyout", key,
					"-out", pem,
				}
				if viper.GetString("expiration-in-days") != "" {
					args = append(args, "-days", viper.GetString("expiration-in-days"))
				}
				cmd := exec.Command("openssl", args...)
				out, err := cmd.CombinedOutput()
				if err != nil {
					l.Errorf("Error generating %s: %s", keyType, string(out))
					return err
				}
				l.Infof("%s generated at %s and %s", keyType, key, pem)

				l.Infof("Converting %s.pem to DER", keyType)
				cmd = exec.Command(
					"openssl", "x509", "-outform", "DER", "-in", pem, "-out", der,
				)
				out, err = cmd.CombinedOutput()
				if err != nil {
					l.Errorf("Error generating %s: %s", keyType, string(out))
					return err
				}
				l.Infof("%s generated at %s", keyType, der)

				l.Infof("Generating %s.esl", keyType)
				cmd = exec.Command(
					"sbsiglist", "--owner", uid.String(), "--type", "x509", "--output", esl, der,
				)
				out, err = cmd.CombinedOutput()
				if err != nil {
					l.Errorf("Error generating %s: %s\n%s", keyType, string(out), err.Error())
					return err
				}
				l.Infof("%s generated at %s", keyType, esl)

				// For PK and KEK we use PK.key and PK.pem to sign it
				// For DB we use KEK.key and KEK.pem to sign it
				var signKey string
				if keyType == "PK" || keyType == "KEK" {
					signKey = filepath.Join(output, "PK")
				} else {
					signKey = filepath.Join(output, "KEK")
				}
				l.Infof("Signing %s with %s", keyType, signKey)
				cmd = exec.Command(
					"sbvarsign",
					"--attr", "NON_VOLATILE,RUNTIME_ACCESS,BOOTSERVICE_ACCESS,TIME_BASED_AUTHENTICATED_WRITE_ACCESS",
					"--key", fmt.Sprintf("%s.key", signKey),
					"--cert", fmt.Sprintf("%s.pem", signKey),
					"--output", auth,
					fmt.Sprintf("%s", keyType),
					esl,
				)
				out, err = cmd.CombinedOutput()
				if err != nil {
					l.Errorf("Error generating %s: %s", keyType, string(out))
					return err
				}
				l.Infof("%s generated at %s", keyType, auth)

			}

			// Generate the policy encryption key
			l.Infof("Generating policy encryption key")
			tpmPrivate := filepath.Join(output, "tpm2-pcr-private.pem")
			cmd := exec.Command(
				"openssl", "genrsa", "-out", tpmPrivate, "2048",
			)
			out, err := cmd.CombinedOutput()
			if err != nil {
				l.Errorf("Error generating tpm2-pcr-private.pem: %s", string(out))
				return err
			}
			return nil
		},
	}
	c.Flags().StringP("output", "o", "keys/", "Output directory for the keys")
	c.Flags().StringP("expiration-in-days", "e", "365", "In how many days from today should the certificates expire")

	viper.BindPFlag("expiration-in-days", c.Flags().Lookup("expiration-in-days"))
	return c
}

func init() {
	rootCmd.AddCommand(NewGenkeyCmd())
}
