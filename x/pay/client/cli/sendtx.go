package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/context"
	"github.com/cosmos/cosmos-sdk/client/utils"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtxb "github.com/cosmos/cosmos-sdk/x/auth/client/txbuilder"
	"github.com/cosmos/cosmos-sdk/x/bank"

	"github.com/terra-project/core/client/tx"

	"github.com/spf13/cobra"
)

const (
	flagTo      = "to"
	flagCoins   = "coins"
	flagOffline = "offline"
)

// SendTxCmd will create a send tx and sign it with the given key.
func SendTxCmd(cdc *codec.Codec) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send --to [to_address] --coins [amount] --from [from_address or key_name]",
		Args:  cobra.NoArgs,
		Short: "Create and sign a send tx",
		Long: strings.TrimSpace(`
Create, sign and broadcast send tx.

For generate-only, --from should be specified as address not key name.
$ terracli tx send --to [to_address] --coins [amount] --from [from_address or key_name]
`),
		RunE: func(cmd *cobra.Command, args []string) error {
			txBldr := authtxb.NewTxBuilderFromCLI().WithTxEncoder(utils.GetTxEncoder(cdc))
			cliCtx := context.NewCLIContext().
				WithCodec(cdc).
				WithAccountDecoder(cdc)

			toStr := viper.GetString(flagTo)

			to, err := sdk.AccAddressFromBech32(toStr)
			if err != nil {
				return err
			}

			coinsStr := viper.GetString(flagCoins)

			// parse coins trying to be sent
			coins, err := sdk.ParseCoins(coinsStr)
			if err != nil {
				return err
			}

			from := cliCtx.GetFromAddress()

			msg := bank.NewMsgSend(from, to, coins)

			offline := viper.GetBool(flagOffline)
			if !offline {

				if err := cliCtx.EnsureAccountExists(); err != nil {
					return err
				}

				account, err := cliCtx.GetAccount(from)
				if err != nil {
					return err
				}

				if txBldr.Fees().Empty() {
					fees, gas, err := tx.ComputeFees(cliCtx, cdc, tx.ComputeReqParams{
						Memo:          txBldr.Memo(),
						ChainID:       txBldr.ChainID(),
						AccountNumber: txBldr.AccountNumber(),
						Sequence:      txBldr.Sequence(),
						GasPrices:     txBldr.GasPrices(),
						Gas:           fmt.Sprintf("%d", txBldr.Gas()),
						GasAdjustment: fmt.Sprintf("%f", txBldr.GasAdjustment()),

						Msgs: []sdk.Msg{msg},
					})

					if err != nil {
						return err
					}

					// Reset txBlder with computed values
					client.GasFlagVar.Gas = gas
					client.GasFlagVar.Simulate = false
					viper.Set(client.FlagFees, fees.String())
					viper.Set(client.FlagGasPrices, "")

					txBldr = authtxb.NewTxBuilderFromCLI().WithTxEncoder(utils.GetTxEncoder(cdc))
				}

				totalAmount := coins.Add(txBldr.Fees())

				// ensure account has enough coins
				if !account.GetCoins().IsAllGTE(totalAmount) {
					return fmt.Errorf("address %s doesn't have enough coins to pay for %s", from, totalAmount.String())
				}
			}

			// build and sign the transaction, then broadcast to Tendermint
			return utils.GenerateOrBroadcastMsgs(cliCtx, txBldr, []sdk.Msg{msg}, offline)
		},
	}

	cmd = client.PostCommands(cmd)[0]

	cmd.Flags().String(flagTo, "", "Destination address for sending")
	cmd.Flags().String(flagCoins, "", "Amount of coins (e.g. 1000uluna,100usdr)")
	cmd.Flags().Bool(flagOffline, false, " Offline mode; Without full node connection the node can still build and sign tx")

	cmd.MarkFlagRequired(client.FlagFrom)
	cmd.MarkFlagRequired(flagTo)
	cmd.MarkFlagRequired(flagCoins)

	return cmd
}
