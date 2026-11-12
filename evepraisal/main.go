package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"

	"github.com/spf13/viper"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if err := readConfig(viper.GetViper(), "/etc/evepraisal/", "."); err != nil {
		log.Fatalf("Invalid config file: %s", err)
	}
	if err := configureEnv(viper.GetViper()); err != nil {
		log.Fatalf("Invalid environment: %s", err)
	}

	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		healthcheckMain()
		return
	}

	log.Println(buildSettingsLog(viper.GetViper()))

	// Check for our subcommands
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "restore":
			restoreMain()
		default:
			fmt.Printf("%q is not valid command.\n", os.Args[1])
			os.Exit(2)
		}
	} else {
		// Default to starting the main app
		appMain()
	}
}

// readConfig loads evepraisal.toml from the first of paths that has it. Only a
// missing file is tolerated: any other error would silently fall back to defaults.
func readConfig(v *viper.Viper, paths ...string) error {
	v.SetConfigType("toml")
	v.SetConfigName("evepraisal")
	for _, path := range paths {
		v.AddConfigPath(path)
	}
	err := v.ReadInConfig()
	var notFound viper.ConfigFileNotFoundError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &notFound), errors.Is(err, fs.ErrNotExist):
		log.Printf("No config file found, using defaults searched_paths=%q", paths)
		return nil
	default:
		if used := v.ConfigFileUsed(); used != "" {
			return fmt.Errorf("%s: %w", used, err)
		}
		return err
	}
}
