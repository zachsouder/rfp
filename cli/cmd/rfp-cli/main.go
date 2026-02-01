// Package main is the entry point for the RFP CLI tool.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "rfp-cli",
	Short: "RFP Intelligence Platform CLI",
	Long:  `CLI tools for managing and inspecting the RFP Intelligence Platform.`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(discoveryCmd)
}

// discoveryCmd is the parent command for discovery operations
var discoveryCmd = &cobra.Command{
	Use:   "discovery",
	Short: "Discovery service operations",
	Long:  `Commands for inspecting and managing the discovery pipeline.`,
}

func init() {
	discoveryCmd.AddCommand(statsCmd)
	discoveryCmd.AddCommand(inspectCmd)
	discoveryCmd.AddCommand(researchCmd)
	discoveryCmd.AddCommand(retryFailedCmd)
	discoveryCmd.AddCommand(recentCmd)
	discoveryCmd.AddCommand(exportCmd)
}

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show discovery statistics",
	Long:  `Display statistics about recent search and research activity.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("TODO: implement discovery stats")
	},
}

var inspectCmd = &cobra.Command{
	Use:   "inspect [result-id]",
	Short: "Inspect research steps for a result",
	Long:  `View the research steps taken for a specific search result.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("TODO: implement discovery inspect for result %s\n", args[0])
	},
}

var researchCmd = &cobra.Command{
	Use:   "research [url]",
	Short: "Manually research a URL",
	Long:  `Run the research agent on a specific URL.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("TODO: implement discovery research for URL %s\n", args[0])
	},
}

var retryFailedCmd = &cobra.Command{
	Use:   "retry-failed",
	Short: "Retry failed research",
	Long:  `Retry research on results that previously failed.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("TODO: implement discovery retry-failed")
	},
}

var recentDays int

var recentCmd = &cobra.Command{
	Use:   "recent",
	Short: "List recent discoveries",
	Long:  `Show RFPs discovered in the last N days.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("TODO: implement discovery recent --days=%d\n", recentDays)
	},
}

func init() {
	recentCmd.Flags().IntVar(&recentDays, "days", 7, "Number of days to look back")
}

var exportFormat string
var exportSince string

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export discoveries",
	Long:  `Export discovered RFPs to JSON or CSV format.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("TODO: implement discovery export --format=%s --since=%s\n", exportFormat, exportSince)
	},
}

func init() {
	exportCmd.Flags().StringVar(&exportFormat, "format", "json", "Output format (json, csv)")
	exportCmd.Flags().StringVar(&exportSince, "since", "", "Export RFPs discovered since date (YYYY-MM-DD)")
}
