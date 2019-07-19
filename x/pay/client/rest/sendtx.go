package rest

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/cosmos/cosmos-sdk/client/context"
	clientrest "github.com/cosmos/cosmos-sdk/client/rest"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/rest"
	"github.com/cosmos/cosmos-sdk/x/bank"

	"github.com/terra-project/core/client/tx"
)

// RegisterRoutes - Central function to define routes that get registered by the main application
func RegisterRoutes(cliCtx context.CLIContext, r *mux.Router, cdc *codec.Codec, kb keys.Keybase) {
	r.HandleFunc("/bank/accounts/{address}/transfers", SendRequestHandlerFn(cdc, kb, cliCtx)).Methods("POST")
}

// SendReq defines the properties of a send request's body.
type SendReq struct {
	BaseReq rest.BaseReq `json:"base_req"`
	Coins   sdk.Coins    `json:"coins"`
}

// SendRequestHandlerFn - http request handler to send coins to a address.
func SendRequestHandlerFn(cdc *codec.Codec, kb keys.Keybase, cliCtx context.CLIContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		bech32Addr := vars["address"]

		toAddr, err := sdk.AccAddressFromBech32(bech32Addr)
		if err != nil {
			rest.WriteErrorResponse(w, http.StatusBadRequest, err.Error())
			return
		}

		var req SendReq
		if !rest.ReadRESTReq(w, r, cdc, &req) {
			return
		}

		req.BaseReq = req.BaseReq.Sanitize()
		if !req.BaseReq.ValidateBasic(w) {
			return
		}

		fromAddr, err := sdk.AccAddressFromBech32(req.BaseReq.From)
		if err != nil {
			rest.WriteErrorResponse(w, http.StatusBadRequest, err.Error())
			return
		}

		account, err := cliCtx.GetAccount(fromAddr.Bytes())
		if err != nil {
			rest.WriteErrorResponse(w, http.StatusBadRequest, err.Error())
			return
		}

		req.BaseReq.AccountNumber = account.GetAccountNumber()

		if req.BaseReq.Sequence == 0 {
			req.BaseReq.Sequence = account.GetSequence()
		}

		msg := bank.NewMsgSend(fromAddr, toAddr, req.Coins)

		if req.BaseReq.Fees.Empty() {
			fees, gas, err := tx.ComputeFees(cliCtx, cdc, tx.ComputeReqParams{
				Memo:          req.BaseReq.Memo,
				ChainID:       req.BaseReq.ChainID,
				AccountNumber: req.BaseReq.AccountNumber,
				Sequence:      req.BaseReq.Sequence,
				GasPrices:     req.BaseReq.GasPrices,
				Gas:           req.BaseReq.Gas,
				GasAdjustment: req.BaseReq.GasAdjustment,

				Msgs: []sdk.Msg{msg},
			})

			if err != nil {
				rest.WriteErrorResponse(w, http.StatusInternalServerError, err.Error())
			}

			req.BaseReq.Fees = fees
			req.BaseReq.Gas = fmt.Sprintf("%d", gas)
			req.BaseReq.GasPrices = sdk.DecCoins{}
		}

		totalAmount := req.Coins.Add(req.BaseReq.Fees)

		// ensure account has enough coins
		if !account.GetCoins().IsAllGTE(totalAmount) {
			err = fmt.Errorf("address %s doesn't have enough coins to pay for %s", req.BaseReq.From, totalAmount.String())
			rest.WriteErrorResponse(w, http.StatusBadRequest, err.Error())
		}

		clientrest.WriteGenerateStdTxResponse(w, cdc, cliCtx, req.BaseReq, []sdk.Msg{msg})
	}
}
