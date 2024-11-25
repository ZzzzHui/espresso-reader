// (c) Cartesi and individual authors (see AUTHORS)
// SPDX-License-Identifier: Apache-2.0 (see LICENSE)

package inputs

import (
	"encoding/json"
	"fmt"

	cmdcommon "github.com/cartesi/rollups-node/cmd/cartesi-rollups-cli/root/common"

	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:     "inputs",
	Short:   "Reads inputs ordered by index",
	Example: examples,
	Run:     run,
}

const examples = `# Read inputs from GraphQL:
cartesi-rollups-cli read inputs -a 0x000000000000000000000000000000000`

var (
	index uint64
)

func init() {
	Cmd.Flags().Uint64Var(&index, "index", 0,
		"index of the input")
}

func run(cmd *cobra.Command, args []string) {
	ctx := cmd.Context()

	if cmdcommon.Database == nil {
		panic("Database was not initialized")
	}

	application := common.HexToAddress(cmdcommon.ApplicationAddress)

	var result []byte
	if cmd.Flags().Changed("index") {
		inputs, err := cmdcommon.Database.GetInput(ctx, application, index)
		cobra.CheckErr(err)
		result, err = json.MarshalIndent(inputs, "", "    ")
		cobra.CheckErr(err)
	} else {
		inputs, err := cmdcommon.Database.GetInputs(ctx, application)
		cobra.CheckErr(err)
		result, err = json.MarshalIndent(inputs, "", "    ")
		cobra.CheckErr(err)
	}

	fmt.Println(string(result))
}
