// (c) Cartesi and individual authors (see AUTHORS)
// SPDX-License-Identifier: Apache-2.0 (see LICENSE)

package reports

import (
	"encoding/json"
	"fmt"
	"os"

	cmdcommon "github.com/cartesi/rollups-node/cmd/cartesi-rollups-cli/root/common"

	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:     "reports",
	Short:   "Reads reports. If an input index is specified, reads all reports from that input",
	Example: examples,
	Run:     run,
}

const examples = `# Read all reports:
cartesi-rollups-cli read reports -a 0x000000000000000000000000000000000`

var (
	inputIndex  uint64
	reportIndex uint64
)

func init() {
	Cmd.Flags().Uint64Var(&inputIndex, "input-index", 0,
		"index of the input")

	Cmd.Flags().Uint64Var(&reportIndex, "report-index", 0,
		"index of the report")
}

func run(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	if cmdcommon.Database == nil {
		panic("Database was not initialized")
	}

	application := common.HexToAddress(cmdcommon.ApplicationAddress)

	var result []byte
	if cmd.Flags().Changed("report-index") {
		if cmd.Flags().Changed("input-index") {
			fmt.Fprintf(os.Stderr, "Error: Only one of 'output-index' or 'input-index' can be used at a time.\n")
			os.Exit(1)
		}
		reports, err := cmdcommon.Database.GetReport(ctx, application, reportIndex)
		cobra.CheckErr(err)
		result, err = json.MarshalIndent(reports, "", "    ")
		cobra.CheckErr(err)
	} else if cmd.Flags().Changed("input-index") {
		reports, err := cmdcommon.Database.GetReportsByInputIndex(ctx, application, inputIndex)
		cobra.CheckErr(err)
		result, err = json.MarshalIndent(reports, "", "    ")
		cobra.CheckErr(err)
	} else {
		reports, err := cmdcommon.Database.GetReports(ctx, application)
		cobra.CheckErr(err)
		result, err = json.MarshalIndent(reports, "", "    ")
		cobra.CheckErr(err)
	}

	fmt.Println(string(result))
}
