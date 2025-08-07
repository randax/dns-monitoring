package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var appVersion = "0.1.0"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long:  `Print detailed version information about the DNS monitoring tool.`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("DNS Monitoring Tool\n")
			fmt.Printf("Version: %s\n", appVersion)
			fmt.Printf("Build Date: %s\n", getBuildDate())
			fmt.Printf("Git Commit: %s\n", getGitCommit())
			fmt.Printf("Go Version: %s\n", getGoVersion())
			fmt.Printf("OS/Arch: %s\n", getOSArch())
		},
	}
}

func getBuildDate() string {
	if buildDate := os.Getenv("BUILD_DATE"); buildDate != "" {
		return buildDate
	}
	return "unknown"
}

func getGitCommit() string {
	if gitCommit := os.Getenv("GIT_COMMIT"); gitCommit != "" {
		return gitCommit
	}
	return "unknown"
}

func getGoVersion() string {
	if goVersion := os.Getenv("GO_VERSION"); goVersion != "" {
		return goVersion
	}
	return "unknown"
}

func getOSArch() string {
	if osArch := os.Getenv("OS_ARCH"); osArch != "" {
		return osArch
	}
	return "unknown"
}